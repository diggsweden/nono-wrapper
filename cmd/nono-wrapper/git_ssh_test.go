// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testHostKey(t *testing.T) (string, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "generic test key")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))), pem.EncodeToMemory(block)
}

func TestPublicHostKeysNeverCopyPrivateDataOrComments(t *testing.T) {
	key, private := testHostKey(t)
	data := append(private, []byte("\ngit.example.com "+key+" confidential-comment\n@revoked revoked.example.com "+key+"\n")...)
	got := string(publicHostKeys(data))
	want := "git.example.com " + key + "\n@revoked revoked.example.com " + key + "\n"
	if got != want {
		t.Fatalf("unexpected public host keys: %q", got)
	}
	if len(publicHostKeys(private)) != 0 {
		t.Fatal("private key data was accepted as host keys")
	}
}

func TestPrepareGitSSHQuotesPathsAndHonorsExplicitCommands(t *testing.T) {
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("requires OpenSSH")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home with space's")
	profiles := filepath.Join(home, "config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	key, private := testHostKey(t)
	if err := os.WriteFile(filepath.Join(home, ".ssh/known_hosts"), []byte("git.example.com "+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("", "git-ssh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	listener, err := net.Listen("unix", filepath.Join(socketDir, "agent.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SSH_AUTH_SOCK", listener.Addr().String())
	t.Setenv("NONO_WRAPPER_SSH_AGENT", "1")
	t.Setenv("PATH", filepath.Dir(sshPath))
	for _, variable := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		t.Setenv(variable, "")
		if err := os.Unsetenv(variable); err != nil {
			t.Fatal(err)
		}
	}
	session, err := prepareGitSSH(t.Context(), "")
	if err != nil || session == nil {
		t.Fatalf("prepare SSH: %v", err)
	}
	t.Cleanup(session.cleanup)
	output, err := exec.CommandContext(t.Context(), session.driver, "-G", "git.example.com").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "userknownhostsfile "+session.files[1]) ||
		!strings.Contains(string(output), "stricthostkeychecking true") || !strings.Contains(string(output), "ssh-proxy") {
		t.Fatalf("SSH options did not survive quoting: %v\n%s", err, output)
	}
	session.cleanup()
	if _, err := os.Stat(filepath.Dir(session.files[1])); !os.IsNotExist(err) {
		t.Fatalf("temporary SSH files remain: %v", err)
	}
	t.Setenv("GIT_SSH_COMMAND", "custom-ssh")
	if session, err := prepareGitSSH(t.Context(), ""); err != nil || session != nil {
		t.Fatalf("explicit SSH command was replaced: %v", err)
	}
	if err := os.Unsetenv("GIT_SSH_COMMAND"); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(profiles, "codex-user.jsonc")
	if err := os.WriteFile(user, []byte(`{"environment":{"set_vars":{"GIT_SSH_COMMAND":"custom-ssh"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if session, err := prepareGitSSH(t.Context(), user); err != nil || session != nil {
		t.Fatalf("explicit profile SSH command was replaced: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh/known_hosts"), private, 0o600); err != nil {
		t.Fatal(err)
	}
	if session, err := prepareGitSSH(t.Context(), ""); err != nil || session != nil {
		t.Fatalf("private data was exposed as SSH host keys: %v", err)
	}
}

func TestSSHProxyRelaysBufferedBytesAndKeepsCredentialsPrivate(t *testing.T) {
	for _, status := range []int{200, 403, 407} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "CONNECT" || r.Host != "git.example.com:443" ||
					r.Header.Get("Proxy-Authorization") != "Basic bm9ubzpzZXNzaW9uLXNlY3JldA==" || r.Header.Get("Authorization") != "" {
					t.Error("invalid CONNECT target or proxy authentication")
					w.WriteHeader(400)
					return
				}
				if status != 200 {
					w.WriteHeader(status)
					return
				}
				conn, stream, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = stream.WriteString("HTTP/1.1 200 Connection Established\r\n\r\nSSH-test\n")
				_ = stream.Flush()
				line, _ := stream.ReadString('\n')
				_, _ = io.WriteString(conn, line)
			}))
			defer server.Close()
			t.Setenv("NONO_SESSION_ID", "generic-session")
			t.Setenv("HTTPS_PROXY", strings.Replace(server.URL, "http://", "http://nono:session-secret@", 1))
			var output bytes.Buffer
			err := relaySSH(context.Background(), "git.example.com", "443", strings.NewReader("client\n"), &output)
			if status == 200 {
				if err != nil || output.String() != "SSH-test\nclient\n" {
					t.Fatalf("relay failed: %v output=%q", err, output.String())
				}
			} else if err == nil || strings.Contains(err.Error(), "session-secret") || !strings.Contains(err.Error(), fmt.Sprint(status)) {
				t.Fatalf("unsafe or missing proxy error: %v", err)
			}
		})
	}
}

func TestSSHProxyRejectsAnExternalProxyWithoutConnecting(t *testing.T) {
	t.Setenv("NONO_SESSION_ID", "generic-session")
	t.Setenv("HTTPS_PROXY", "http://user:private-password@proxy.example.com:3128")
	err := relaySSH(t.Context(), "git.example.com", "22", bufio.NewReader(strings.NewReader("")), io.Discard)
	if err == nil || strings.Contains(err.Error(), "private-password") || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("unexpected proxy validation: %v", err)
	}
}

func TestSSHProxyRequiresAnActiveSession(t *testing.T) {
	t.Setenv("NONO_SESSION_ID", "")
	t.Setenv("NONO_CAP_FILE", "")
	t.Setenv("HTTPS_PROXY", "http://nono:session-secret@127.0.0.1:1")
	err := relaySSH(t.Context(), "git.example.com", "22", strings.NewReader(""), io.Discard)
	if err == nil || exitCode(t.Context(), err) != exitNoPerm || strings.Contains(err.Error(), "session-secret") {
		t.Fatalf("unexpected inactive-session result: %v", err)
	}
}
