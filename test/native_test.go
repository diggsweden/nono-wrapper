// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Real-agent smoke coverage is explicit: installation downloads signed packs.
// The ordinary unit suite continues to use fake agents and work offline.
func TestNativeIntegration(t *testing.T) {
	if os.Getenv("NONO_NATIVE_TEST") != "1" {
		t.Skip("use just test-native for real-agent integration")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Fatal("native integration requires macOS or Linux")
	}
	for _, tool := range []string{"nono", "opencode", "codex", "just", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required; run just install-dev-tools: %v", tool, err)
		}
	}

	// macOS grants system access under /private/var, which cannot also contain
	// protected nono state. Keep the isolated home on the user's filesystem.
	hostHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(hostHome, ".nono-wrapper-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove native test home %s: %v", root, err)
		}
	})
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "projects", "project with spaces")
	// The official macOS Codex profile grants OS temporary locations, not
	// arbitrary directories chosen as TMPDIR. Keep temporary files there.
	temp := canonicalTempDir(t)
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(home, ".local", "bin")
	env := append([]string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		"SHELL=/bin/sh",
		"TMPDIR=" + temp,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local/share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local/state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"CODEX_HOME=" + filepath.Join(home, ".codex"),
		"NONO_WRAPPER_SSH_AGENT=0",
		"NONO_WRAPPER_ROOT=" + filepath.Dir(project),
		"NONO_NO_UPDATE_CHECK=1",
		"NONO_NO_PACK_UPDATE_HINTS=1",
		"NO_COLOR=1",
	}, buildEnv...)
	run := func(directory string, extra []string, command string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = directory
		cmd.Env = append(append([]string{}, env...), extra...)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", command, args, err, output)
		}
		t.Logf("passed: %s %s", filepath.Base(command), strings.Join(args, " "))
		return string(output)
	}

	// A real Git root bounds OpenCode's project configuration discovery.
	run(project, nil, "git", "init", "-q")
	run(testRepository(t), nil, "just", "install-local")
	wrapper := filepath.Join(bin, "nono-wrapper")
	if output := run(project, nil, wrapper, "man"); !strings.Contains(output, "## SYNOPSIS") {
		t.Fatal("installed manual is missing")
	}
	run(project, nil, wrapper, "verify")
	run(project, []string{"NONO_PROFILE=opencode-offline"}, wrapper, "opencode", "session", "list")
	run(project, []string{"NONO_PROFILE=codex-offline"}, wrapper, "codex", "features", "list")

	// Reuse the container suite's real command-execution probe with this fresh
	// home and project. No login or model is needed by the Codex app server.
	if err := os.WriteFile(filepath.Join(home, "private"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	if err := probeCodex(wrapper, append(env, "NONO_PROFILE=codex-offline")); err != nil {
		t.Fatal(err)
	}
	t.Log("passed: real Codex command execution and confinement")
}
