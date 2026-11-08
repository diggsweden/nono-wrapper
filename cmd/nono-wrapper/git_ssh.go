// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
	"golang.org/x/crypto/ssh"
)

type gitSSHSession struct {
	driver  string
	files   []string
	ssh     executable
	source  string
	cleanup func()
}

func shellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// Defaults are session-local. User-provided SSH commands take precedence;
// private keys and the user's SSH configuration are never copied.
func prepareGitSSH(ctx context.Context, userProfile string) (*gitSSHSession, error) {
	for _, variable := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		if _, set := os.LookupEnv(variable); set {
			return nil, nil
		}
	}
	if userProfile != "" {
		doc, _, _, err := readUserConfig(userProfile)
		if err != nil {
			return nil, err
		}
		for _, variable := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
			if doc.Find("/environment/set_vars/"+variable) != nil {
				return nil, nil
			}
		}
	}
	if len(sshAgentArgs()) == 0 {
		return nil, nil
	}
	program, err := exec.LookPath("ssh")
	if err != nil {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	source := filepath.Join(home, ".ssh", "known_hosts")
	file, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Git SSH host keys: %w", quotePathError(err))
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, quotePathError(err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	const maxHostKeys = 4 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxHostKeys+1))
	if err != nil {
		return nil, quotePathError(err)
	}
	if len(data) > maxHostKeys {
		return nil, fmt.Errorf("Git SSH known_hosts exceeds 4 MiB; set GIT_SSH_COMMAND to use your own SSH setup")
	}
	public := publicHostKeys(data)
	if len(public) == 0 {
		return nil, nil
	}
	resolved, err := resolveExecutable(ctx, "ssh", program)
	if err != nil {
		return nil, err
	}
	own, err := os.Executable()
	if err != nil {
		return nil, err
	}
	own, err = literalRuntimePath(own)
	if err != nil {
		return nil, err
	}
	profile, err := configPath("opencode")
	if err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(filepath.Dir(profile), ".nono-wrapper-ssh-")
	if err != nil {
		return nil, quotePathError(err)
	}
	cleanup := func() {
		if err := os.RemoveAll(directory); err != nil {
			fmt.Fprintf(os.Stderr, "nono-wrapper: remove temporary SSH host keys: %s\n", quotePathError(err))
		}
	}
	knownHosts, err := literalRuntimePath(filepath.Join(directory, "known_hosts"))
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.WriteFile(knownHosts, public, 0o600); err != nil {
		cleanup()
		return nil, quotePathError(err)
	}
	proxyCommand := shellArg(strings.ReplaceAll(own, "%", "%%")) + " ssh-proxy '%h' '%p'"
	hostKeyPath := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(knownHosts)
	command := shellArg(resolved.path) + " -F /dev/null -o BatchMode=yes -o IdentityFile=none" +
		" -o StrictHostKeyChecking=yes -o UpdateHostKeys=no" +
		" -o " + shellArg(`UserKnownHostsFile="`+hostKeyPath+`"`) +
		" -o " + shellArg("ProxyCommand="+proxyCommand)
	driver, err := literalRuntimePath(filepath.Join(directory, "git-ssh"))
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := os.WriteFile(driver, []byte("#!/bin/sh\nexec "+command+" \"$@\"\n"), 0o700); err != nil {
		cleanup()
		return nil, quotePathError(err)
	}
	return &gitSSHSession{
		driver:  driver,
		files:   []string{own, knownHosts, resolved.path, driver},
		ssh:     resolved,
		source:  source,
		cleanup: cleanup,
	}, nil
}

// Parse and re-encode public entries, discarding comments and invalid lines.
// A symlink to a private key or credentials file must not expose its bytes.
func publicHostKeys(data []byte) []byte {
	var result bytes.Buffer
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		marker, hosts, key, _, _, err := ssh.ParseKnownHosts(append(bytes.Clone(line), '\n'))
		if err != nil {
			continue
		}
		if marker != "" {
			fmt.Fprintf(&result, "@%s ", marker)
		}
		fmt.Fprintf(&result, "%s %s", strings.Join(hosts, ","), ssh.MarshalAuthorizedKey(key))
	}
	return result.Bytes()
}

func sshProxyCommand() *cli.Command {
	return &cli.Command{
		Name: "ssh-proxy", Usage: "Relay SSH through the active nono session proxy",
		ArgsUsage: "HOST PORT", OnUsageError: usageError,
		Description: "Git transport helper used by the bundled online profiles. Requires an active nono session. The proxy's host policy still applies; this command grants no network access itself.",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 2 {
				return cli.Exit("usage: nono-wrapper ssh-proxy HOST PORT", exitUsage)
			}
			return relaySSH(ctx, cmd.Args().Get(0), cmd.Args().Get(1), os.Stdin, cmd.Root().Writer)
		},
	}
}

func relaySSH(ctx context.Context, host, port string, input io.Reader, output io.Writer) error {
	if os.Getenv("NONO_SESSION_ID") == "" && os.Getenv("NONO_CAP_FILE") == "" {
		return cli.Exit("ssh-proxy requires an active nono session", exitNoPerm)
	}
	if !validHostname(hostKey(host)) && net.ParseIP(host) == nil {
		return cli.Exit("ssh-proxy requires a hostname or IP address", exitDataErr)
	}
	if _, err := parsePort(port); err != nil {
		return cli.Exit(err.Error(), exitDataErr)
	}
	// Never print this URL: its userinfo holds the session proxy credential.
	proxy, err := url.Parse(os.Getenv("HTTPS_PROXY"))
	if err != nil || proxy.Scheme != "http" || proxy.User == nil ||
		(proxy.Hostname() != "127.0.0.1" && proxy.Hostname() != "::1") ||
		(proxy.Path != "" && proxy.Path != "/") || proxy.RawQuery != "" || proxy.Fragment != "" {
		return cli.Exit("ssh-proxy requires nono's authenticated loopback HTTPS_PROXY; restart the agent", exitConfig)
	}
	if _, err := parsePort(proxy.Port()); err != nil {
		return cli.Exit("ssh-proxy received an invalid session proxy port", exitConfig)
	}
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", proxy.Host)
	if err != nil {
		return fmt.Errorf("connect to the session SSH proxy: %w", err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	authority := net.JoinHostPort(host, port)
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: authority}, Host: authority, Header: make(http.Header)}
	password, _ := proxy.User.Password()
	request.SetBasicAuth(proxy.User.Username(), password)
	request.Header.Set("Proxy-Authorization", request.Header.Get("Authorization"))
	request.Header.Del("Authorization")
	if err := request.Write(connection); err != nil {
		return fmt.Errorf("request SSH proxy connection: %w", err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return fmt.Errorf("read SSH proxy response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("SSH proxy refused %q (HTTP %d); check the domain rules and destination", authority, response.StatusCode)
	}
	_ = connection.SetDeadline(time.Time{})
	go func() {
		_, _ = io.Copy(connection, input)
		if tcp, ok := connection.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	_, err = io.Copy(output, reader)
	if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
