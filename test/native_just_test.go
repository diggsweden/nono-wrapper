// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testNativeJustRecipes(t *testing.T, root, project, wrapper string, env []string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Just's /run/user runtime directory is Linux-only")
	}
	runtimeDir := filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	if info, err := os.Stat(runtimeDir); err != nil || !info.IsDir() {
		t.Skip("requires the user's /run/user runtime directory")
	}
	// Use a harmless sibling, never read or modify a real runtime socket.
	sibling, err := os.CreateTemp(runtimeDir, "nono-wrapper-just-denied-")
	if err != nil {
		t.Fatal(err)
	}
	siblingPath := sibling.Name()
	if err := sibling.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(siblingPath) })
	bin := filepath.Join(root, "just probes")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	// The isolated HOME cannot read the host user's Mise installs. Put a real
	// Just binary under this home's default Mise tree, exercising mise_manager.
	just, err := exec.LookPath("just")
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(just)
	if err != nil {
		t.Fatal(err)
	}
	toolBin := filepath.Join(root, "home", ".local/share/mise/installs/just/test/bin")
	if err := os.MkdirAll(toolBin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolBin, "just"), contents, 0o700); err != nil {
		t.Fatal(err)
	}
	justfile := filepath.Join(project, "runtime.just")
	// Checking $0 prevents a fallback to /tmp from hiding the missing grant.
	recipe := "probe:\n    #!/bin/sh\n    set -eu\n    case \"$0\" in \"$XDG_RUNTIME_DIR\"/just/*) ;; *) exit 91 ;; esac\n    printf 'JUST_SCRIPT_RAN\\n'\n"
	if err := os.WriteFile(justfile, []byte(recipe), 0o600); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+toolBin+string(os.PathListSeparator)+path)
			break
		}
	}
	probeEnv = append(probeEnv, "XDG_RUNTIME_DIR="+runtimeDir)
	// Stand-ins run real Just through the real wrapper and nono, without a model.
	// Version checks must still succeed for the OpenCode launcher.
	body := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf 'opencode v2.0.0\\n'; exit 0; fi\n" +
		"for arg do if [ \"$arg\" = --fail-probe ]; then printf 'Permission denied\\n' >&2; exit 7; fi; done\n" +
		"if cat '" + siblingPath + "' >/dev/null 2>&1; then exit 92; fi\n" +
		"if (printf probe > '" + siblingPath + "') 2>/dev/null; then exit 93; fi\n" +
		"exec just --justfile runtime.just probe\n"
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{agent}, {"run", "--block-net", agent}, {agent, "--fail-probe"}} {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, wrapper, args...)
				cmd.Dir, cmd.Env, cmd.WaitDelay = project, probeEnv, time.Second
				output, err := cmd.CombinedOutput()
				if args[len(args)-1] == "--fail-probe" {
					exit, ok := err.(*exec.ExitError)
					if !ok || exit.ExitCode() != 7 || !strings.Contains(string(output), "session ended (exit 7)") ||
						strings.Contains(string(output), "Next steps:") || strings.Contains(string(output), "Review denied paths") || strings.Contains(string(output), "refuse_version()") {
						t.Fatalf("native footer leaked or exit/report lost: %v\n%s", err, output)
					}
				} else if err != nil || !strings.Contains(string(output), "JUST_SCRIPT_RAN") {
					t.Fatalf("Just script recipe through %v: %v\n%s", args, err, output)
				}
			}
			customRuntime := filepath.Join(root, "runtime-"+agent)
			if err := os.Mkdir(customRuntime, 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, wrapper, agent)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, append(append([]string{}, probeEnv...), "XDG_RUNTIME_DIR="+customRuntime), time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "JUST_SCRIPT_RAN") {
				t.Fatalf("Just with custom XDG_RUNTIME_DIR through %s: %v\n%s", agent, err, output)
			}
		})
	}
}
