// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testNativeTemporarySockets(t *testing.T, root, project, wrapper string, env []string) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux pathname socket mediation")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("requires Python multiprocessing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	paths, err := exec.CommandContext(ctx, python, "-I", "-c", "import sys; print(sys.executable); print(sys.prefix)").Output()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(paths)), "\n")
	if len(parts) != 2 {
		t.Fatalf("unexpected Python paths: %q", paths)
	}
	// Keep the pathname below Linux's sockaddr_un limit, including test names.
	outside, err := os.MkdirTemp("/tmp", "nono-socket-deny-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	// Python may fall back to /tmp for Unix sockets when its private temp path
	// exceeds sockaddr_un. Keep the fixture cache short; the unrelated sibling
	// socket below still proves that the parent receives no socket capability.
	cache := filepath.Join(outside, "cache")
	// The official OpenCode pack also sets TMPDIR relative to HOME. Keep this
	// home outside /tmp: inherited temp reads cannot contain protected HOME files.
	hostHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp(hostHome, ".nono-sockets-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	socket := filepath.Join(outside, "listener.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	control, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	control.Close()
	script := filepath.Join(project, "private_socket_probe.py")
	if err := os.WriteFile(script, []byte(`import errno
import multiprocessing
import os
from pathlib import Path
import socket
import sys
import tempfile

def main():
    assert tempfile.gettempdir() == os.environ["TMPDIR"]
    with multiprocessing.Manager() as manager:
        value = manager.list(["working"])
        assert value[0] == "working"
    outside = Path(sys.argv[1])
    # Prove filesystem access separately from the denied socket operations.
    probe = outside.parent / "filesystem-probe"
    probe.write_text("fixture")
    probe.unlink()
    for operation, path in [("connect", outside), ("bind", outside.parent / "blocked.sock")]:
        with socket.socket(socket.AF_UNIX) as sock:
            sock.settimeout(2)
            try:
                getattr(sock, operation)(str(path))
            except OSError as error:
                assert error.errno in (errno.EACCES, errno.EPERM), error
            else:
                raise AssertionError("unrelated socket operation permitted: " + operation)
    print("PRIVATE_MULTIPROCESSING_OK")

if __name__ == "__main__":
    main()
`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "temporary socket probes")
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
	body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
while [ "${1:-}" != --fixture ]; do shift; done
shift
exec "$1" -I "$2" "$3"
`
	for _, agent := range []string{"opencode", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"unset", "empty", "explicit"} {
			environment := make([]string, 0, len(probeEnv)+1)
			for _, entry := range probeEnv {
				if !strings.HasPrefix(entry, "TMPDIR=") && !strings.HasPrefix(entry, "XDG_CACHE_HOME=") && !strings.HasPrefix(entry, "HOME=") {
					environment = append(environment, entry)
				}
			}
			environment = append(environment, "XDG_CACHE_HOME="+cache, "HOME="+home)
			switch mode {
			case "empty":
				environment = append(environment, "TMPDIR=")
			case "explicit":
				environment = append(environment, "TMPDIR="+filepath.Join(cache, "opencode/tmp"))
			}
			cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", "--read", parts[1], agent, "--fixture", parts[0], script, socket)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, environment, time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "PRIVATE_MULTIPROCESSING_OK") {
				t.Fatalf("multiprocessing through %s (TMPDIR %s): %v\n%s", agent, mode, err, output)
			}
		}
	}
}
