// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestColdLaunchPreparesPrivateAgentState(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			home := filepath.Dir(f.bin)
			paths := []string{".codex"}
			if agent == "opencode" {
				paths = []string{".npm", ".config/opencode", ".cache/opencode", ".local/share/opencode", ".local/share/opentui", ".local/state/opencode"}
			}
			for attempt := 0; attempt < 2; attempt++ {
				_, stderr, status := runLauncher(t, f, agent, []string{"--version"}, nil)
				if status != 0 {
					t.Fatalf("cold/repeated launch: status=%d stderr=%q", status, stderr)
				}
				for _, path := range paths {
					info, err := os.Stat(filepath.Join(home, path))
					if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
						t.Fatalf("state directory %s is missing or not private: %v (%v)", path, info, err)
					}
				}
			}
		})
	}
}

func TestAgentStateCannotExposePolicy(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			policy := filepath.Join(filepath.Dir(f.bin), ".config/nono")
			variable, value := "XDG_DATA_HOME", filepath.Join(policy, "profiles")
			if agent == "codex" {
				variable, value = "CODEX_HOME", filepath.Join(policy, "profiles")
			}
			stdout, stderr, status := runLauncher(t, f, agent, nil, []string{variable + "=" + value})
			if status == 0 || len(stdout) != 0 || !strings.Contains(stderr, "active rules") {
				t.Fatalf("policy-overlapping agent state was allowed: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Stat(policy); !os.IsNotExist(err) {
				t.Fatalf("refused state setup created policy directories: %v", err)
			}
		})
	}
}

func TestOpenCodeInstallationIsNotPreparedAsWritableState(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	installed := filepath.Join(filepath.Dir(f.bin), ".opencode/bin/opencode")
	if err := os.MkdirAll(filepath.Dir(installed), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, installed, "#!/bin/sh\nexit 99\n")
	if err := os.Remove(filepath.Join(f.bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(installed, filepath.Join(f.bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	_, stderr, status := runLauncher(t, f, "opencode", []string{"--version"}, nil)
	if status != 0 {
		t.Fatalf("read-only installation was treated as writable state: status=%d stderr=%q", status, stderr)
	}
}

func TestAgentStateRejectsLinkedPolicyAndRelativeRoots(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	home := filepath.Dir(f.bin)
	policy := filepath.Join(home, ".config/nono/profiles")
	data := filepath.Join(home, ".local/share")
	for _, path := range []string{policy, data} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(policy, filepath.Join(data, "opencode")); err != nil {
		t.Fatal(err)
	}
	_, stderr, status := runLauncher(t, f, "opencode", nil, nil)
	if status == 0 || !strings.Contains(stderr, "active rules") {
		t.Fatalf("linked state exposed policy: status=%d stderr=%q", status, stderr)
	}
	_, stderr, status = runLauncher(t, f, "opencode", nil, []string{"XDG_CONFIG_HOME=relative"})
	if status == 0 || !strings.Contains(stderr, "absolute") {
		t.Fatalf("relative state root was accepted: status=%d stderr=%q", status, stderr)
	}
}
