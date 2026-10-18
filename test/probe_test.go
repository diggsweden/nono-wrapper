// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

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
