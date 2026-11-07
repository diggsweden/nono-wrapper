// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavedLoginLooksInTheAgentStateDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	for agent, want := range map[string]string{
		"codex":    filepath.Join(root, "codex", "auth.json"),
		"opencode": filepath.Join(root, "data", "opencode", "auth.json"),
	} {
		if path, ok := savedLogin(agent); ok || path != want {
			t.Fatalf("%s before sign-in: %q, %v; want %q, false", agent, path, ok, want)
		}
		if err := os.MkdirAll(filepath.Dir(want), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(want, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := savedLogin(agent); !ok {
			t.Fatalf("%s sign-in at %s not found", agent, want)
		}
	}
}
