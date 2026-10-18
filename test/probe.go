// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

// The container test helper uses only the standard library. Build it on the
// host with CGO_ENABLED=0; neither Go nor an interpreter is needed in the images.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Set by prepare.sh so an image built from older probe source is refused.
var sourceSHA string

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "agent-probe:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected codex, login, tcp-bind, socket, socket-inside, packs or source-sha256")
	}
	switch args[0] {
	case "codex":
		if len(args) == 1 {
			if err := requireContainer(); err != nil {
				return err
			}
			return probeCodex("nono-wrapper", nil)
		}
	case "socket":
		if len(args) == 2 {
			if err := requireContainer(); err != nil {
				return err
			}
			return probeSocket(args[1])
		}
	case "login":
		if len(args) == 2 {
			if err := requireContainer(); err != nil {
				return err
			}
			return probeLogin(args[1])
		}
	case "tcp-bind":
		if len(args) == 2 {
			listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", args[1]))
			if err != nil {
				return err
			}
			return listener.Close()
		}
	case "listen":
		// Stand in for a host service: note in the marker that a peer connected.
		if len(args) == 4 {
			listener, err := net.Listen(args[1], args[2])
			if err != nil {
				return err
			}
			for {
				connection, err := listener.Accept()
				if err != nil {
					return err
				}
				connection.Close()
				if err := os.WriteFile(args[3], nil, 0o600); err != nil {
					return err
				}
			}
		}
	case "socket-inside":
		if len(args) == 2 {
			err := connectSocket(args[1])
			if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
				return nil
			}
			if err == nil {
				return errors.New("connected to a host socket without a socket grant")
			}
			return fmt.Errorf("expected a socket permission denial, got: %w", err)
		}
	case "packs":
		if len(args) == 3 {
			manifest, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer manifest.Close()
			lock, err := os.Open(args[2])
			if err != nil {
				return err
			}
			defer lock.Close()
			return checkPacks(manifest, lock, os.Stdout)
		}
	case "source-sha256":
		if len(args) == 1 && sourceSHA != "" {
			fmt.Println(sourceSHA)
			return nil
		}
	}
	return fmt.Errorf("invalid arguments for %q", args[0])
}

func requireContainer() error {
	if _, err := os.Stat("/run/.containerenv"); err != nil || os.Getenv("HOME") != "/home/tester" {
		return errors.New("tests require the disposable Podman image; use just test-containers")
	}
	return nil
}

func lineScanner(input io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(input)
	// Agent RPC responses can include sizeable output in a single JSON line.
	scanner.Buffer(make([]byte, 4096), 8*1024*1024)
	return scanner
}

func checkPacks(manifest, lock io.Reader, output io.Writer) error {
	var installed struct {
		Packages map[string]struct {
			Version string
			Pinned  bool
		}
	}
	if err := json.NewDecoder(lock).Decode(&installed); err != nil {
		return err
	}
	scanner := lineScanner(manifest)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return fmt.Errorf("invalid pack manifest entry: %q", line)
		}
		name, version := fields[0], fields[1]
		pack, exists := installed.Packages[name]
		if !exists || pack.Version != version || !pack.Pinned {
			return fmt.Errorf("expected pinned pack %s@%s; found version %q, pinned=%t", name, version, pack.Version, pack.Pinned)
		}
		fmt.Fprintf(output, "  installed pack: %s@%s (pinned)\n", name, version)
	}
	return scanner.Err()
}

type rpcResponse struct {
	ID     json.RawMessage
	Result json.RawMessage
	Error  json.RawMessage
}

type rpcMessage struct {
	response rpcResponse
	err      error
}

func readResponses(ctx context.Context, input io.Reader) <-chan rpcMessage {
	messages := make(chan rpcMessage)
	go func() {
		defer close(messages)
		scanner := lineScanner(input)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "{") {
				continue // nono banners are outside the JSON-RPC stream.
			}
			var response rpcResponse
			err := json.Unmarshal([]byte(line), &response)
			select {
			case messages <- rpcMessage{response, err}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case messages <- rpcMessage{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return messages
}

func awaitResponse(ctx context.Context, messages <-chan rpcMessage, id int) (json.RawMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case message, ok := <-messages:
			if !ok {
				return nil, errors.New("Codex exited before responding")
			}
			if message.err != nil {
				return nil, message.err
			}
			response := message.response
			if string(response.ID) != strconv.Itoa(id) {
				continue
			}
			if len(response.Error) != 0 {
				return nil, fmt.Errorf("Codex RPC error: %s", response.Error)
			}
			if len(response.Result) == 0 {
				return nil, errors.New("Codex response has no result")
			}
			return response.Result, nil
		}
	}
}

func probeCodex(wrapper string, env []string) (err error) {
	diagnostics, err := os.CreateTemp("", "codex-probe-errors-")
	if err != nil {
		return err
	}
	defer os.Remove(diagnostics.Name())
	defer diagnostics.Close()
	cmd := exec.Command(wrapper, "codex", "app-server")
	cmd.Env = env // nil inherits the container environment; native tests pass an isolated one.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stderr = diagnostics
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer func() {
		cancel()
		stopProcess(cmd, input)
		if err != nil {
			diagnostics.Seek(0, io.SeekStart)
			io.Copy(os.Stderr, diagnostics)
		}
	}()
	messages := readResponses(ctx, output)
	encoder := json.NewEncoder(input)
	request := func(id int, method string, params any) (json.RawMessage, error) {
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		return awaitResponse(ctx, messages, id)
	}
	if _, err = request(1, "initialize", map[string]any{
		"clientInfo":   map[string]string{"name": "nono-profile-test", "version": "1"},
		"capabilities": map[string]bool{"experimentalApi": true},
	}); err != nil {
		return err
	}
	if err := encoder.Encode(map[string]string{"method": "initialized"}); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	result, err := request(2, "command/exec", map[string]any{
		"command": []string{"/bin/sh", "-c", `
set -eu
touch codex-written .git/codex-written "$TMPDIR/codex-written"
if cat "$HOME/private" >/dev/null 2>&1; then exit 11; fi
if touch "$HOME/escaped" 2>/dev/null; then exit 12; fi
curl --version >/dev/null
if curl -s -m 5 -o /dev/null https://example.com; then exit 13; fi
echo confined
`},
		"cwd": cwd,
		// Match the launcher: nono must enforce confinement without Codex's sandbox.
		"sandboxPolicy": map[string]string{"type": "dangerFullAccess"},
		"timeoutMs":     15000,
	})
	if err != nil {
		return err
	}
	var execution struct {
		ExitCode *int
		Stdout   string
	}
	if err := json.Unmarshal(result, &execution); err != nil {
		return err
	}
	if execution.ExitCode == nil || *execution.ExitCode != 0 || strings.TrimSpace(execution.Stdout) != "confined" {
		return fmt.Errorf("unexpected command result: %s", result)
	}
	for _, path := range []string{"codex-written", ".git/codex-written"} {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Codex did not create a regular file: %s", path)
		}
	}
	return nil
}

// Start the real browser-login flow and cancel it through its local callback.
// No browser, account credentials or completed authentication are required.
func probeLogin(agent string) (err error) {
	var cmd *exec.Cmd
	switch agent {
	case "opencode":
		cmd = exec.Command("nono-wrapper", "opencode", "auth", "login", "--provider", "openai", "--method", "ChatGPT Pro/Plus (browser)")
	case "codex":
		cmd = exec.Command("nono-wrapper", "codex", "login")
	default:
		return fmt.Errorf("unknown login agent: %s", agent)
	}

	// Refuse an occupied port rather than mistake another service for our child.
	listener, err := net.Listen("tcp4", "127.0.0.1:1455")
	if err != nil {
		return fmt.Errorf("login callback port is unavailable: %w", err)
	}
	listener.Close()
	diagnostics, err := os.CreateTemp("", "login-probe-errors-")
	if err != nil {
		return err
	}
	defer os.Remove(diagnostics.Name())
	defer diagnostics.Close()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = diagnostics, diagnostics
	input, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		stopProcess(cmd, input)
		if err != nil {
			diagnostics.Seek(0, io.SeekStart)
			io.Copy(os.Stderr, diagnostics)
		}
	}()
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for {
		output, readErr := os.ReadFile(diagnostics.Name())
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(output), "https://auth.openai.com/oauth/authorize") {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1455/cancel", nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err == nil {
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					return err
				}
				if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "Login cancelled") {
					return fmt.Errorf("unexpected login callback response: status=%d body=%q", response.StatusCode, body)
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s browser login did not start: %w", agent, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func stopProcess(cmd *exec.Cmd, input io.Closer) {
	input.Close()
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(10 * time.Second):
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	<-done
}

func connectSocket(path string) error {
	connection, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return err
	}
	return connection.Close()
}

func probeSocket(profile string) error {
	// The relative name avoids AF_UNIX path limits in long checkout paths.
	// It is inside the writable project, so only socket policy may refuse it.
	const path = "host.sock"
	server, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer server.Close() // UnixListener also removes its own pathname.
	if err := connectSocket(path); err != nil {
		return fmt.Errorf("host socket positive control: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nono", "run", "--silent", "--profile", profile,
		"--allow-cwd", "--read-file", executable, "--", executable, "socket-inside", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
