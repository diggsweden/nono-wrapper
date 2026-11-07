// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRuntimeDirectoryPreparation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Just's runtime directory is Linux-only")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GRADLE_USER_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("RUSTUP_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", root)
	for range 2 {
		rules, err := prepareDeveloperPaths(nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, rule := range rules {
			found = found || rule.path == filepath.Join(root, "just") && rule.access == pathRead|pathWrite && !rule.file
		}
		if !found {
			t.Fatalf("missing custom runtime grant: %v", rules)
		}
	}
	info, err := os.Stat(filepath.Join(root, "just"))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("expected private prepared runtime directory: %v, %v", info, err)
	}
	if _, err := prepareDeveloperPaths([]pathRule{{root, pathRead, false}}); err == nil {
		t.Fatal("runtime directory bypassed protected launch inputs")
	}
	missing := filepath.Join(root, "missing")
	t.Setenv("XDG_RUNTIME_DIR", missing)
	if _, err := prepareDeveloperPaths(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("runtime parent must not be created: %v", err)
	}
	t.Setenv("XDG_RUNTIME_DIR", "relative")
	if _, err := prepareDeveloperPaths(nil); err == nil {
		t.Fatal("relative runtime directory accepted")
	}
}

func TestRuntimeDirectoryRejectsSymlink(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Just's runtime directory is Linux-only")
	}
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local/share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GRADLE_USER_HOME", "")
	t.Setenv("CARGO_HOME", "")
	t.Setenv("RUSTUP_HOME", "")
	t.Setenv("XDG_RUNTIME_DIR", root)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "just")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareDeveloperPaths(nil); err == nil {
		t.Fatal("prepared runtime symlink accepted")
	}
}
