// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tailscale/hujson"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Explicit native coverage: no model, account, external host or real user keys.
func TestNativeGitSSH(t *testing.T) {
	if os.Getenv("NONO_SSH_TEST") != "1" {
		t.Skip("set NONO_SSH_TEST=1 for real nono/Git/OpenSSH integration")
	}
	if runtime.GOOS != "linux" {
		t.Skip("native Git SSH confinement coverage currently targets Linux")
	}
	nono := os.Getenv("NONO_TEST_NONO")
	if nono == "" {
		var err error
		nono, err = exec.LookPath("nono")
		if err != nil {
			t.Fatal(err)
		}
	}
	nono, err := filepath.EvalSymlinks(nono)
	if err != nil || !filepath.IsAbs(nono) || filepath.Base(nono) == "mise" {
		t.Fatal("set NONO_TEST_NONO to the absolute native nono executable, not a Mise shim")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	sshClient, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("", "nono-git-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove native Git fixture: %v", err)
		}
	})
	home, project, bin := filepath.Join(root, "home"), filepath.Join(root, "workspace"), filepath.Join(root, "bin")
	configHome := filepath.Join(root, "config with spaces")
	profiles := filepath.Join(configHome, "nono/profiles")
	for _, dir := range []string{profiles, project, bin, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"nono-wrapper": wrapperBinary, "nono": nono, "ssh": sshClient} {
		if err := os.Symlink(target, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(bin, "codex")
	writeLauncherFixture(t, program, "#!/bin/sh\nshift 3\nif [ \"$1\" = __query ]; then shift; exec nono why --self --host \"$1\" --json; fi\nif [ \"$1\" = __socket ]; then exec nono why --self --unix-socket \"$SSH_AUTH_SOCK\" --json; fi\nexec "+git+" \"$@\"\n")
	data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles/developer-network.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "developer-network.jsonc"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	base, _ := json.Marshal(map[string]any{
		"extends":     "developer-network",
		"groups":      map[string]any{"exclude": []string{"system_write_linux", "user_caches_linux"}},
		"workdir":     map[string]any{"access": "readwrite"},
		"filesystem":  map[string]any{"allow": []string{"$HOME/.codex"}, "allow_file": []string{"/dev/null"}, "read": []string{"$NONO_CONFIG/profiles"}, "read_file": []string{nono}},
		"environment": map[string]any{"allow_vars": []string{"HOME", "PATH", "SSH_AUTH_SOCK", "XDG_CONFIG_HOME", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG_GLOBAL"}},
		"linux":       map[string]any{"af_unix_mediation": "pathname"},
	})
	if err := os.WriteFile(filepath.Join(profiles, "codex-chatgpt.jsonc"), base, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + home, "XDG_CONFIG_HOME=" + configHome,
		"NONO_WRAPPER_ROOT=" + project, "NONO_NO_UPDATE_CHECK=1", "NONO_NO_PACK_UPDATE_HINTS=1",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LANG=C"}
	hostGit := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), git, args...)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("host Git fixture %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	bare, source := filepath.Join(root, "remote.git"), filepath.Join(root, "source")
	hostGit("init", "--bare", "-q", bare)
	hostGit("init", "-q", "-b", "main", source)
	commit := func(dir, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		hostGit("-C", dir, "add", "example.txt")
		hostGit("-C", dir, "-c", "user.name=Example", "-c", "user.email=example@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "Generic change")
	}
	commit(source, "first\n")
	hostGit("-C", source, "push", "-q", bare, "HEAD:main")
	hostGit("-C", bare, "symbolic-ref", "HEAD", "refs/heads/main")
	_, clientKey, _ := ed25519.GenerateKey(rand.Reader)
	clientPublic, err := ssh.NewPublicKey(clientKey.Public())
	if err != nil {
		t.Fatal(err)
	}
	_, hostPrivate, _ := ed25519.GenerateKey(rand.Reader)
	hostKey, err := ssh.NewSignerFromKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() == "git" && bytes.Equal(key.Marshal(), clientPublic.Marshal()) {
			return nil, nil
		}
		return nil, fmt.Errorf("unrecognized test key")
	}}
	config.AddHostKey(hostKey)
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	// Cover both loopback families without exposing the test server externally.
	v4, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v4.Close() })
	for _, server := range []net.Listener{listener, v4} {
		go serveGitSSH(t, server, config, git, bare, env)
	}
	knownHosts := filepath.Join(home, ".ssh/known_hosts")
	writeHostKey := func(key ssh.PublicKey) {
		t.Helper()
		line := fmt.Sprintf("[localhost.localdomain]:%d %s", port, ssh.MarshalAuthorizedKey(key))
		if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeHostKey(hostKey.PublicKey())
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: clientKey}); err != nil {
		t.Fatal(err)
	}
	agentSocket, err := net.Listen("unix", filepath.Join(root, "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agentSocket.Close() })
	go func() {
		for {
			conn, err := agentSocket.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				stop := context.AfterFunc(t.Context(), func() { _ = conn.Close() })
				defer stop()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	env = append(env, "SSH_AUTH_SOCK="+agentSocket.Addr().String())
	invoke := func(extra []string, args ...string) (string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, wrapperBinary, args...)
		cmd.Env = append(append([]string{}, env...), extra...)
		cmd.Dir, cmd.WaitDelay = project, time.Second
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
	if output, err := invoke(nil, "config", "codex", "allow-domain", "localhost.localdomain"); err != nil {
		t.Fatalf("configure native SSH target: %v\n%s", err, output)
	}
	remote := fmt.Sprintf("ssh://git@localhost.localdomain:%d/project.git", port)
	checkout := filepath.Join(project, "checkout")
	if output, err := invoke(nil, "codex", "clone", remote, checkout); err != nil {
		t.Fatalf("sandboxed Git clone: %v\n%s", err, output)
	}
	commit(source, "second\n")
	hostGit("-C", source, "push", "-q", bare, "HEAD:main")
	if output, err := invoke(nil, "codex", "-C", checkout, "fetch", "origin"); err != nil {
		t.Fatalf("sandboxed Git fetch: %v\n%s", err, output)
	}
	commit(checkout, "client change\n")
	if output, err := invoke(nil, "codex", "-C", checkout, "push", "origin", "HEAD:client-branch"); err != nil {
		t.Fatalf("sandboxed Git push: %v\n%s", err, output)
	}
	if got, want := hostGit("-C", bare, "rev-parse", "refs/heads/client-branch"), hostGit("-C", checkout, "rev-parse", "HEAD"); got != want {
		t.Fatal("sandboxed push did not update the test remote")
	}
	checkPolicy := func(url, status string) {
		t.Helper()
		output, err := invoke(nil, "codex", "__query", url)
		if err != nil || !strings.Contains(output, `"status": "`+status+`"`) {
			t.Fatalf("session policy for %s: want %s, got %v\n%s", url, status, err, output)
		}
	}
	checkPolicy("https://github.com", "allowed")
	checkPolicy("https://unlisted.invalid", "denied")
	for _, test := range []struct{ setting, reason string }{
		{"NONO_WRAPPER_SSH_AGENT=1", "unix_socket_granted"},
		{"NONO_WRAPPER_SSH_AGENT=0", "unix_socket_not_granted"},
	} {
		output, err := invoke([]string{test.setting}, "codex", "__socket")
		if err != nil || !strings.Contains(output, `"reason": "`+test.reason+`"`) {
			t.Fatalf("session socket diagnostics for %s: %v\n%s", test.setting, err, output)
		}
	}
	if output, err := invoke(nil, "config", "codex", "allow-domain", "--method", "GET", "https://api.github.com/repos/project-a/**"); err != nil {
		t.Fatalf("restrict shared default: %v\n%s", err, output)
	}
	checkPolicy("https://api.github.com/repos/project-a/issues", "allowed")
	checkPolicy("https://api.github.com/repos/project-b/issues", "denied")
	if output, err := invoke(nil, "codex", "clone", fmt.Sprintf("ssh://git@unlisted.invalid:%d/project.git", port), filepath.Join(project, "denied")); err == nil || !strings.Contains(output, "HTTP 403") {
		t.Fatalf("unlisted SSH destination was not refused: %v\n%s", err, output)
	}
	// This isolated negative probe removes trust/auth obstacles so a failed
	// fetch must come from the direct TCP restriction, not a missing host key.
	direct := "ssh -F /dev/null -o Hostname=::1 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
	if output, err := invoke([]string{"GIT_SSH_COMMAND=" + direct, "GIT_SSH_VARIANT=ssh"}, "codex", "-C", checkout, "fetch"); err == nil || !strings.Contains(output, "Permission denied") {
		t.Fatalf("direct SSH bypass was not refused: %v\n%s", err, output)
	}
	if output, err := invoke([]string{"GIT_SSH_COMMAND=sh -c 'echo EXPLICIT_SSH >&2; exit 1' --", "GIT_SSH_VARIANT=ssh"}, "codex", "-C", checkout, "fetch"); err == nil || !strings.Contains(output, "EXPLICIT_SSH") {
		t.Fatalf("explicit SSH environment was overridden: %v\n%s", err, output)
	}
	hostGit("-C", checkout, "config", "core.sshCommand", "sh -c 'echo CONFIG_SSH >&2; exit 1' --")
	hostGit("-C", checkout, "config", "ssh.variant", "ssh")
	if output, err := invoke(nil, "codex", "-C", checkout, "fetch"); err == nil || !strings.Contains(output, "CONFIG_SSH") {
		t.Fatalf("explicit Git configuration was overridden: %v\n%s", err, output)
	}
	hostGit("-C", checkout, "config", "--unset", "core.sshCommand")
	hostGit("-C", checkout, "config", "--unset", "ssh.variant")
	userFile := filepath.Join(profiles, "codex-user.jsonc")
	original, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatal(err)
	}
	standard, _ := hujson.Standardize(original)
	var user map[string]any
	_ = json.Unmarshal(standard, &user)
	network := user["network"].(map[string]any)
	network["deny_domain"] = []string{"github.com"}
	data, _ = json.Marshal(user)
	if err := os.WriteFile(userFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	checkPolicy("https://github.com", "denied")
	user["environment"] = map[string]any{"set_vars": map[string]string{"GIT_SSH_COMMAND": "sh -c 'echo PROFILE_SSH >&2; exit 1' --", "GIT_SSH_VARIANT": "ssh"}}
	data, _ = json.Marshal(user)
	if err := os.WriteFile(userFile, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := invoke(nil, "codex", "-C", checkout, "fetch"); err == nil || !strings.Contains(output, "PROFILE_SSH") {
		t.Fatalf("explicit SSH profile setting was overridden: %v\n%s", err, output)
	}
	if err := os.WriteFile(userFile, original, 0o600); err != nil {
		t.Fatal(err)
	}
	_, wrongPrivate, _ := ed25519.GenerateKey(rand.Reader)
	wrongKey, _ := ssh.NewPublicKey(wrongPrivate.Public())
	writeHostKey(wrongKey)
	if output, err := invoke(nil, "codex", "-C", checkout, "fetch"); err == nil || !strings.Contains(output, "Host key verification failed") {
		t.Fatalf("incorrect host key was not refused: %v\n%s", err, output)
	}
	for _, pattern := range []string{".nono-wrapper-ssh-*", ".nono-wrapper-policy-*"} {
		files, err := filepath.Glob(filepath.Join(profiles, pattern))
		if err != nil || len(files) > 0 {
			t.Fatalf("SSH session left temporary files: %v (%v)", files, err)
		}
	}
}

func serveGitSSH(t *testing.T, listener net.Listener, config *ssh.ServerConfig, git, repository string, env []string) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer connection.Close()
			stop := context.AfterFunc(t.Context(), func() { _ = connection.Close() })
			defer stop()
			server, channels, requests, err := ssh.NewServerConn(connection, config)
			if err != nil {
				return
			}
			defer server.Close()
			go ssh.DiscardRequests(requests)
			for incoming := range channels {
				channel, requests, err := incoming.Accept()
				if err != nil {
					return
				}
				func() {
					defer channel.Close()
					for request := range requests {
						var payload struct{ Command string }
						if request.Type != "exec" || ssh.Unmarshal(request.Payload, &payload) != nil {
							_ = request.Reply(false, nil)
							continue
						}
						operation := ""
						for _, allowed := range []string{"upload-pack", "receive-pack"} {
							if payload.Command == "git-"+allowed+" '/project.git'" {
								operation = allowed
							}
						}
						if operation == "" {
							_ = request.Reply(false, nil)
							return
						}
						_ = request.Reply(true, nil)
						cmd := exec.CommandContext(t.Context(), git, operation, repository)
						cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, channel, channel, channel.Stderr()
						status := uint32(0)
						if err := cmd.Run(); err != nil {
							status = 1
						}
						_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
						return
					}
				}()
			}
		}()
	}
}
