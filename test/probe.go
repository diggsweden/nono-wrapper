// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

// The container test helper uses only the standard library. Build it on the
// host with CGO_ENABLED=0; neither Go nor an interpreter is needed in the images.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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
	case "opencode":
		if len(args) == 1 {
			if err := requireContainer(); err != nil {
				return err
			}
			config := os.Getenv("XDG_CONFIG_HOME")
			if config == "" {
				config = filepath.Join(os.Getenv("HOME"), ".config")
			}
			return probeOpenCode("nono-wrapper", filepath.Join(config, "nono/profiles/opencode-user.jsonc"), nil)
		}
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
			return probeLogin(args[1], "nono-wrapper", nil, os.Getenv("OPENCODE_TEST_SERVER"))
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
			return socketDenialResult(connectSocket(args[1]))
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

func probeCommand(ctx context.Context, program string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// A timed-out wrapper can leave nono and its children holding our output
	// pipes. Kill the test command's group and bound any remaining pipe wait.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	return cmd
}

// Exercise OpenCode 2's automatic standalone server and explicit local-server
// mode. The temporary user profile belongs to this test fixture.
func probeOpenCode(wrapper, userProfile string, env []string) error {
	for _, profile := range []string{"opencode-offline", "opencode-chatgpt"} {
		if err := probeOpenCodeProfile(wrapper, userProfile, env, profile); err != nil {
			return fmt.Errorf("%s local-server probe: %w", profile, err)
		}
	}
	return nil
}

func probeOpenCodeProfile(wrapper, userProfile string, env []string, base string) (err error) {
	original, readErr := os.ReadFile(userProfile)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	profile, _ := json.Marshal(map[string]any{
		"environment": map[string]any{"allow_vars": []string{"OPENCODE_PASSWORD", "OPENCODE_DISABLE_MODELS_FETCH"}},
	})
	if err := os.WriteFile(userProfile, profile, 0o600); err != nil {
		return err
	}
	defer func() {
		var restore error
		if original == nil {
			restore = os.Remove(userProfile)
		} else {
			restore = os.WriteFile(userProfile, original, 0o600)
		}
		if err == nil {
			err = restore
		}
	}()
	if env == nil {
		env = os.Environ()
	}
	password := rand.Text()
	env = append(append([]string{}, env...), "NONO_PROFILE="+base, "OPENCODE_PASSWORD="+password, "OPENCODE_DISABLE_MODELS_FETCH=1")
	if runtime.GOOS == "linux" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		// No server URL or port grants: the wrapper must contain both the
		// client and OpenCode's own dynamic-port server in private loopback.
		client := probeCommand(ctx, wrapper, "opencode", "session", "list", "--format", "json")
		client.Env = env
		output, err := client.Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return fmt.Errorf("OpenCode 2 standalone startup: %w\n%s\n%s", err, output, exit.Stderr)
			}
			return err
		}
		var sessions []json.RawMessage
		if err := json.Unmarshal(output, &sessions); err != nil {
			return fmt.Errorf("OpenCode 2 standalone session list is not JSON: %w\n%s", err, output)
		}
		// A host localhost service must remain unreachable even though the
		// standalone client's own loopback connection above succeeded.
		host, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		defer host.Close()
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, "host-service")
		}), ReadHeaderTimeout: time.Second}
		defer server.Close()
		go server.Serve(host)
		endpoint := "http://" + host.Addr().String()
		response, err := http.Get(endpoint)
		if err != nil {
			return fmt.Errorf("host-loopback positive control: %w", err)
		}
		response.Body.Close()
		isolated := probeCommand(ctx, "nono", "run", "--profile", base, "--isolate", "net,loopback", "--", "/bin/sh", "-c",
			hostIsolationScript, "probe", endpoint)
		isolated.Env = env
		output, err = isolated.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "host-isolated") || strings.Contains(string(output), "host-service") {
			return fmt.Errorf("private loopback did not isolate the host service: %v\n%s", err, output)
		}
		if base == "opencode-chatgpt" && os.Getenv("AGENT_TEST_LOCAL_HTTP") == "1" {
			if err := requireContainer(); err != nil {
				return err
			}
			if err := probeLocalHTTP(ctx, wrapper, base, env); err != nil {
				return err
			}
		}
	}
	// Explicit servers use host loopback and deliberately keep their separate,
	// narrow port grants. Do not apply namespace isolation to this mode.
	profile, _ = json.Marshal(map[string]any{
		"environment": map[string]any{"allow_vars": []string{"OPENCODE_PASSWORD", "OPENCODE_DISABLE_MODELS_FETCH"}},
		"network":     map[string]any{"open_port": []int{port}, "listen_port": []int{0}},
	})
	if err := os.WriteFile(userProfile, profile, 0o600); err != nil {
		return err
	}
	diagnostics, err := os.CreateTemp("", "opencode-server-probe-")
	if err != nil {
		return err
	}
	defer os.Remove(diagnostics.Name())
	defer diagnostics.Close()
	server := exec.Command(wrapper, "opencode", "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	server.Env = env
	server.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	server.Stdout, server.Stderr = diagnostics, diagnostics
	input, err := server.StdinPipe()
	if err != nil {
		return err
	}
	if err := server.Start(); err != nil {
		input.Close()
		return err
	}
	defer func() {
		_ = server.Process.Signal(syscall.SIGTERM)
		stopProcess(server, input)
		if err != nil {
			diagnostics.Seek(0, io.SeekStart)
			io.Copy(os.Stderr, diagnostics)
		}
	}()
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	for {
		output, readErr := os.ReadFile(diagnostics.Name())
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(output), "server listening on "+endpoint) {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("OpenCode 2 foreground server did not become ready: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
	for _, valid := range []bool{true, false} {
		client := probeCommand(ctx, wrapper, "opencode", "--server", endpoint, "session", "list")
		client.Env = append([]string{}, env...)
		if !valid {
			client.Env = append(client.Env, "OPENCODE_PASSWORD=incorrect-test-password")
		}
		output, clientErr := client.CombinedOutput()
		if (clientErr == nil) != valid {
			return fmt.Errorf("OpenCode 2 server authentication valid=%t: %v\n%s", valid, clientErr, output)
		}
		if !valid && !strings.Contains(strings.ToLower(string(output)), "password") && !strings.Contains(strings.ToLower(string(output)), "unauthorized") {
			return fmt.Errorf("wrong server password failed for an unexpected reason: %v\n%s", clientErr, output)
		}
	}
	if base == "opencode-chatgpt" {
		return probeLogin("opencode", wrapper, env, endpoint)
	}
	return nil
}

// curl 7 is a failed connection to the positively controlled host listener.
// Missing tools, timeouts and other transport failures are not isolation proof.
const hostIsolationScript = `
command -v curl >/dev/null || exit 127
status=0
curl --noproxy '*' -sS --max-time 2 "$1" || status=$?
[ "$status" -eq 7 ] || exit 13
printf 'host-isolated\n'
`

// Separate host-side loopback addresses can carry separate services. This
// deliberately tests the same port on both IPs; it is not a per-port grant.
func probeLocalHTTP(ctx context.Context, wrapper, base string, env []string) error {
	const allowed = "local-service.nono.test"
	const blocked = "blocked.local-service.nono.test"
	first, err := net.Listen("tcp4", "127.0.0.2:0")
	if err != nil {
		return err
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port
	second, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.3", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	defer second.Close()
	extra, err := net.Listen("tcp4", "127.0.0.2:0")
	if err != nil {
		return err
	}
	defer extra.Close()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for _, fixture := range []struct {
		listener net.Listener
		body     string
	}{{first, "allowed-local-service"}, {second, "other-local-service"}, {extra, "same-ip-extra-port"}} {
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, fixture.body)
		}), ReadHeaderTimeout: time.Second}
		defer server.Close()
		go server.Serve(fixture.listener)
		response, err := client.Get("http://" + fixture.listener.Addr().String())
		if err != nil {
			return fmt.Errorf("local-service positive control: %w", err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || string(body) != fixture.body {
			return fmt.Errorf("local-service positive control: status=%d body=%q error=%v", response.StatusCode, body, err)
		}
	}
	request := func(host string, targetPort, want int, marker string) error {
		endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(targetPort)) + "/"
		cmd := probeCommand(ctx, "nono", "run", "--silent", "--profile", base, "--extends", "opencode-user",
			"--isolate", "net,loopback", "--allow-cwd", "--", "curl", "--noproxy", "", "--silent", "--show-error",
			"--max-time", "5", "--max-filesize", "1024", "--write-out", "\nHTTP:%{http_code}\n", endpoint)
		cmd.Env = env
		output, err := cmd.Output()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return fmt.Errorf("local HTTP request %s: %w\n%s\n%s", endpoint, err, output, exit.Stderr)
			}
			return fmt.Errorf("local HTTP request %s: %w\n%s", endpoint, err, output)
		}
		suffix := fmt.Sprintf("\nHTTP:%d\n", want)
		if !strings.HasSuffix(string(output), suffix) ||
			(want == http.StatusOK && strings.TrimSuffix(string(output), suffix) != marker) {
			return fmt.Errorf("local HTTP request %s: expected %d, got %s", endpoint, want, output)
		}
		return nil
	}
	configure := func(verb string) error {
		cmd := probeCommand(ctx, wrapper, "config", "opencode", verb, allowed)
		cmd.Env = env
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("configure local HTTP allowance: %w\n%s", err, output)
		}
		return nil
	}
	if err := request(allowed, port, http.StatusForbidden, ""); err != nil {
		return err
	}
	if err := configure("allow-domain"); err != nil {
		return err
	}
	if err := request(allowed, port, http.StatusOK, "allowed-local-service"); err != nil {
		return err
	}
	if err := request(allowed, extra.Addr().(*net.TCPAddr).Port, http.StatusOK, "same-ip-extra-port"); err != nil {
		return err
	}
	if err := request(blocked, port, http.StatusForbidden, ""); err != nil {
		return err
	}
	if err := configure("remove-domain"); err != nil {
		return err
	}
	if err := request(allowed, port, http.StatusForbidden, ""); err != nil {
		return err
	}
	fmt.Println("local HTTP proxy: exact hostname works on both host ports, separate-IP service refused, grant removal restores refusal")
	return nil
}

// Start the real browser-login flow and cancel it through its local callback.
// No browser, account credentials or completed authentication are required.
func probeLogin(agent, wrapper string, env []string, server string) (err error) {
	var cmd *exec.Cmd
	switch agent {
	case "opencode":
		cmd = exec.Command(wrapper, "opencode", "auth", "login", "openai", "--method", "chatgpt-token-sharing")
		if server != "" {
			cmd.Args = append(cmd.Args, "--server", server)
		}
	case "codex":
		cmd = exec.Command(wrapper, "codex", "login")
	default:
		return fmt.Errorf("unknown login agent: %s", agent)
	}

	// Refuse an occupied port rather than mistake another service for our child.
	cmd.Env = env
	if agent != "opencode" {
		listener, err := net.Listen("tcp4", "127.0.0.1:1455")
		if err != nil {
			return fmt.Errorf("login callback port is unavailable: %w", err)
		}
		listener.Close()
	}
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
	client := &http.Client{
		Timeout: time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	for {
		output, readErr := os.ReadFile(diagnostics.Name())
		if readErr != nil {
			return readErr
		}
		callback := ""
		wantStatus := http.StatusOK
		if agent == "opencode" {
			callback = openCodeCancelURL(string(output))
			wantStatus = http.StatusBadRequest
		} else if strings.Contains(string(output), "https://auth.openai.com/oauth/authorize") {
			callback = "http://127.0.0.1:1455/cancel"
		}
		if callback != "" {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, callback, nil)
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
				if response.StatusCode != wantStatus || !strings.Contains(string(body), "Login cancelled") {
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

// Only contact the explicit loopback callback in OpenCode 2's authorization
// URL. Never turn an arbitrary URL printed by a child into a host request.
func openCodeCancelURL(output string) string {
	const authorize = "https://auth.openai.com/api/accounts/authorize?"
	for _, field := range strings.FieldsFunc(output, func(r rune) bool { return r <= ' ' || r == '\x1b' }) {
		index := strings.Index(field, authorize)
		if index < 0 {
			continue
		}
		request, err := url.Parse(field[index:])
		if err != nil {
			continue
		}
		callback, err := url.Parse(request.Query().Get("redirect_uri"))
		if err != nil || callback.Scheme != "http" || callback.Hostname() != "127.0.0.1" ||
			callback.Path != "/auth/callback" || callback.User != nil || callback.RawQuery != "" || callback.Fragment != "" {
			continue
		}
		port, err := strconv.ParseUint(callback.Port(), 10, 16)
		if err != nil || port == 0 {
			continue
		}
		callback.RawQuery = url.Values{"error": {"access_denied"}, "error_description": {"Login cancelled"}}.Encode()
		return callback.String()
	}
	return ""
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

// socketDenialResult accepts only an actual permission denial, never a missing
// socket, refused connection or timeout. Integration supplies the live socket.
func socketDenialResult(err error) error {
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return nil
	}
	if err == nil {
		return errors.New("connected to a host socket without a socket grant")
	}
	return fmt.Errorf("expected a socket permission denial, got: %w", err)
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
