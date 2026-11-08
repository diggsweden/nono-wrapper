// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// OpenCode sign-in gets its port grants on the command line, for one session.
func TestLoginOpenCodeGrantsOnlyThatSession(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	stdout, stderr, status := runLauncher(t, f, "login", []string{"opencode"}, nil)
	if status != 0 {
		t.Fatalf("login failed: status=%d stderr=%q", status, stderr)
	}
	got := capturedLauncherArgs(t, stdout, "opencode")
	index := slices.Index(got, "--open-port")
	if index < 0 || index+1 >= len(got) {
		t.Fatalf("login has no server port grant: %q", got)
	}
	port := got[index+1]
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		t.Fatalf("invalid server port %q", port)
	}
	// No private loopback: the host browser must reach the sign-in callback.
	want := append(expectedRunProfile("opencode-chatgpt", false),
		"--open-port", port, "--listen-port", "0", "--allow-cwd", "--read-file", f.target, "--", f.target, port)
	if !slices.Equal(got, want) {
		t.Fatalf("got=%q want=%q", got, want)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.bin), ".config", "nono", "profiles", "opencode-user.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("login wrote a user configuration: %v", err)
	}
}

func TestLoginOpenCodeSignsInThroughATemporaryServer(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	for _, tool := range []string{"od", "tr", "sleep"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("test requires %s: %v", tool, err)
		}
		if err := os.Symlink(path, filepath.Join(f.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Dir(f.bin)
	log, pid := filepath.Join(root, "calls"), filepath.Join(root, "server.pid")
	writeLauncherFixture(t, f.target, `#!/bin/sh
case "$1" in
--version) printf 'opencode v2.2.5\n' ;;
serve)
  printf 'serve %s %s\n' "$5" "$OPENCODE_PASSWORD" >>"$LOGIN_LOG"
  printf '%s\n' "$$" >"$LOGIN_PID"
  exec sleep 30 ;;
--server)
  case "$3" in
  session) [ -f "$LOGIN_PID" ] || exit 1 ;;
  auth) shift 2; printf 'login %s %s\n' "$*" "$OPENCODE_PASSWORD" >>"$LOGIN_LOG"; exit 7 ;;
  esac ;;
*) exit 99 ;;
esac
`)
	writeNonoFixture(t, f, `
case "$1" in
profile) printf '{"network":{"block":false},"workdir":{"access":"readwrite"}}\n' ;;
run)
  while [ "$1" != -- ]; do shift; done
  shift
  exec "$@" ;;
*) exit 99 ;;
esac
`)
	_, stderr, status := runLauncher(t, f, "login", []string{"opencode"}, []string{"LOGIN_LOG=" + log, "LOGIN_PID=" + pid})
	if status != 7 {
		t.Fatalf("login status was not the sign-in status: status=%d stderr=%q", status, stderr)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`^serve ([0-9]+) ([0-9a-f]{64})\nlogin auth login openai --method chatgpt-token-sharing ([0-9a-f]{64})\n$`).FindStringSubmatch(string(calls))
	if match == nil || match[2] != match[3] {
		t.Fatalf("server and sign-in did not share one generated password: %q", calls)
	}
	data, err := os.ReadFile(pid)
	if err != nil {
		t.Fatal(err)
	}
	server, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); syscall.Kill(server, 0) == nil; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the temporary server is still running after sign-in")
		}
	}
}

func TestLoginCodexForwardsToCodexLogin(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	stdout, stderr, status := runLauncher(t, f, "login", []string{"codex", "--device-auth"}, nil)
	if status != 0 {
		t.Fatalf("login failed: status=%d stderr=%q", status, stderr)
	}
	got := capturedLauncherArgs(t, stdout, "codex")
	if !slices.Equal(got[len(got)-2:], []string{"login", "--device-auth"}) {
		t.Fatalf("codex login arguments were not forwarded: %q", got)
	}
}
