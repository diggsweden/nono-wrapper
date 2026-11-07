// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestDeveloperEnvironmentPreservesReleaseAge(t *testing.T) {
	t.Setenv("NPM_CONFIG_MIN_RELEASE_AGE", "14")
	t.Setenv("PIPX_COOLDOWN", "30")
	environment, err := developerSessionEnvironment("codex-offline")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "NPM_CONFIG_MIN_RELEASE_AGE=") || strings.HasPrefix(entry, "PIPX_COOLDOWN=") {
			t.Fatalf("overrode explicit release-age policy: %s", entry)
		}
	}
	if got, err := developerSessionEnvironment("external-profile"); err != nil || len(got) != 0 {
		t.Fatalf("external profile changed: %v (%v)", got, err)
	}
}

func TestDeveloperEnvironmentDefaultsEmptyCacheRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("GOCACHE", "")
	got, err := developerSessionEnvironment("codex-offline")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "GOCACHE="+filepath.Join(root, "go-build")) {
		t.Fatalf("cache default differs from its prepared path: %v", got)
	}
	t.Setenv("GOCACHE", filepath.Join(root, "explicit-cache"))
	got, err = developerSessionEnvironment("codex-offline")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "GOCACHE=") {
			t.Fatalf("explicit cache root overridden: %s", entry)
		}
	}
}

func TestDeveloperEnvironmentDefaults(t *testing.T) {
	// t.Setenv restores each original value even though the fixture unsets it.
	for _, key := range []string{"NPM_CONFIG_MIN_RELEASE_AGE", "PIPX_COOLDOWN"} {
		t.Setenv(key, "fixture")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	got, err := developerSessionEnvironment("opencode-chatgpt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NPM_CONFIG_MIN_RELEASE_AGE=7", "PIPX_COOLDOWN=7"} {
		if !slices.Contains(got, want) {
			t.Fatalf("missing default %s: %v", want, got)
		}
	}
}

func TestDeveloperTemporaryDirectoryDefaultsAndOverrides(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux private socket roots")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "custom cache"))
	t.Setenv("TMPDIR", "")
	for _, unset := range []bool{false, true} {
		if unset {
			if err := os.Unsetenv("TMPDIR"); err != nil {
				t.Fatal(err)
			}
		}
		got, err := developerSessionEnvironment("codex-offline")
		want := "TMPDIR=" + filepath.Join(root, "custom cache/opencode/tmp")
		if err != nil || !slices.Contains(got, want) {
			t.Fatalf("missing private temp default (unset=%v): %v (%v)", unset, got, err)
		}
	}
	t.Setenv("TMPDIR", filepath.Join(root, "explicit temp"))
	got, err := developerSessionEnvironment("opencode-offline")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "TMPDIR=") {
			t.Fatalf("explicit TMPDIR overridden: %s", entry)
		}
	}
}
