// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testNativePodman(t *testing.T, root, project, wrapper string, env []string) {
	if runtime.GOOS != "linux" || os.Getuid() == 0 {
		t.Skip("requires Linux rootless Podman")
	}
	runtimeDir := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	socket := filepath.Join(runtimeDir, "podman/podman.sock")
	if info, err := os.Lstat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Skip("rootless Podman socket is not running; test never starts it")
	}
	for _, tool := range []string{"podman", "curl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("requires %s", tool)
		}
	}
	unrelated, err := os.MkdirTemp(runtimeDir, "nono-podman-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(unrelated) })
	other := filepath.Join(unrelated, "unrelated.sock")
	listener, err := net.Listen("unix", other)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("UNRELATED_SOCKET_REACHED"))
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return net.Dial("unix", other)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	response, err := client.Get("http://localhost/_ping")
	if err != nil {
		t.Fatalf("unrelated socket positive control: %v", err)
	}
	response.Body.Close()
	bin := filepath.Join(root, "podman probes")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
			break
		}
	}
	// Only read service metadata. Never create containers or modify real storage.
	body := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf 'opencode v2.0.0\\n'; exit 0; fi\n" +
		"set -eu\n[ \"${CONTAINER_HOST:-}\" = 'unix://" + socket + "' ]\n" +
		"if curl --silent --max-time 2 --unix-socket '" + other + "' http://localhost/_ping >/dev/null; then exit 92; fi\n" +
		"[ \"$(podman info --format '{{.Host.Security.Rootless}}')\" = true ]\nprintf 'PODMAN_ROOTLESS_REACHED\\n'\n"
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", agent)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, probeEnv, time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "PODMAN_ROOTLESS_REACHED") {
				t.Fatalf("rootless Podman through %s: %v\n%s", agent, err, output)
			}
		})
	}
}
