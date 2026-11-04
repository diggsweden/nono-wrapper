// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProbeTimeoutStopsChildrenHoldingOutput(t *testing.T) {
	ps, err := exec.LookPath("ps")
	if err != nil {
		t.Skip("process cleanup check requires ps")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	started := time.Now()
	cmd := probeCommand(ctx, "/bin/sh", "-c", "sleep 60 & printf '%s\\n' $!; wait")
	output, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() == nil || time.Since(started) > 3*time.Second {
		t.Fatalf("timeout did not bound subprocess cleanup: %v (%s)", err, output)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("child never started: %q (%v)", output, err)
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		state, err := exec.Command(ps, "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		// A killed child may be a zombie until container PID 1 reaps it.
		if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child %d survived the timeout", pid)
}

func TestHostIsolationRequiresAConnectionRefusal(t *testing.T) {
	for _, code := range []int{-1, 0, 7, 28, 35, 127} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			bin := t.TempDir()
			if code >= 0 {
				if err := os.WriteFile(filepath.Join(bin, "curl"), []byte("#!/bin/sh\nexit "+strconv.Itoa(code)+"\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("/bin/sh", "-c", hostIsolationScript, "probe", "http://127.0.0.1:12345")
			cmd.Env = []string{"PATH=" + bin}
			output, err := cmd.CombinedOutput()
			if (err == nil) != (code == 7) || strings.Contains(string(output), "host-isolated") != (code == 7) {
				t.Fatalf("curl %d: %v (%s)", code, err, output)
			}
		})
	}
}

func TestOpenCodeCancelURLOnlyUsesItsLoopbackCallback(t *testing.T) {
	for _, test := range []struct {
		redirect string
		allow    bool
	}{
		{"http://127.0.0.1:45678/auth/callback", true},
		{"http://example.com:45678/auth/callback", false},
		{"https://127.0.0.1:45678/auth/callback", false},
		{"http://127.0.0.1/auth/callback", false},
		{"http://127.0.0.1:0/auth/callback", false},
		{"http://127.0.0.1:65536/auth/callback", false},
		{"http://user@127.0.0.1:45678/auth/callback", false},
		{"http://127.0.0.1:45678/other", false},
	} {
		t.Run(test.redirect, func(t *testing.T) {
			output := "Open: https://auth.openai.com/api/accounts/authorize?redirect_uri=" + url.QueryEscape(test.redirect) + "\x1b[0m\n"
			got := openCodeCancelURL(output)
			if (got != "") != test.allow {
				t.Fatalf("unexpected callback from %q: %q", test.redirect, got)
			}
			if test.allow && got != "http://127.0.0.1:45678/auth/callback?error=access_denied&error_description=Login+cancelled" {
				t.Fatalf("unexpected cancellation request: %q", got)
			}
		})
	}
}

func TestPackValidationChecksInstalledVersionAndPin(t *testing.T) {
	const manifest = "# official packs\n\nnolabs-ai/opencode 0.2.0\n"
	for _, test := range []struct {
		name  string
		lock  string
		valid bool
	}{
		{"pinned version", `{"packages":{"nolabs-ai/opencode":{"version":"0.2.0","pinned":true}}}`, true},
		{"wrong version", `{"packages":{"nolabs-ai/opencode":{"version":"0.3.0","pinned":true}}}`, false},
		{"unpinned", `{"packages":{"nolabs-ai/opencode":{"version":"0.2.0","pinned":false}}}`, false},
		{"missing", `{"packages":{}}`, false},
		{"invalid JSON", "not JSON", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkPacks(strings.NewReader(manifest), strings.NewReader(test.lock), io.Discard)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%t, error=%v", test.valid, err)
			}
		})
	}
}

func TestRPCMatchesResponseRatherThanNotifications(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := readResponses(ctx, strings.NewReader(`nono banner
{"method":"notification"}
{"id":99,"result":"wrong response"}
{"id":2,"result":{"exitCode":0,"stdout":"confined\n"}}
`))
	result, err := awaitResponse(ctx, messages, 2)
	if err != nil || string(result) != `{"exitCode":0,"stdout":"confined\n"}` {
		t.Fatalf("result=%s, error=%v", result, err)
	}
}

func TestRPCRejectsErrorsAndEarlyEOF(t *testing.T) {
	for _, input := range []string{
		`{"id":2,"error":{"code":-1,"message":"refused"}}`,
		`{"id":2}`,
		`{"id":2,broken}`,
		`{"id":1,"result":{}}`,
		"",
	} {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := awaitResponse(ctx, readResponses(ctx, strings.NewReader(input)), 2)
		cancel()
		if err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestRPCWaitHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := awaitResponse(ctx, make(chan rpcMessage), 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestSocketProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.sock")
	if err := run([]string{"socket-inside", path}); err == nil {
		t.Fatal("a missing socket is not a permission denial")
	}
	server, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := connectSocket(path); err != nil {
		t.Fatalf("positive control: %v", err)
	}
	err = run([]string{"socket-inside", path})
	if err == nil || !strings.Contains(err.Error(), "connected to a host socket without a socket grant") {
		t.Fatalf("an unrestricted connection must fail the probe, got %v", err)
	}
}
