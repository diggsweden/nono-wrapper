// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDeveloperPathsRespectCustomRootsAndCredentialBoundary(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local/share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	gradle, cargo, rustup := filepath.Join(root, "gradle"), filepath.Join(root, "cargo"), filepath.Join(root, "rustup")
	t.Setenv("GRADLE_USER_HOME", gradle)
	t.Setenv("CARGO_HOME", cargo)
	t.Setenv("RUSTUP_HOME", rustup)
	t.Setenv("GOPATH", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOCACHE", "")
	rules, err := prepareDeveloperPaths(nil)
	if err != nil {
		t.Fatal(err)
	}
	foundNative, foundDistributions, foundConfig, foundToolchain := false, false, false, false
	writable := []string{filepath.Join(gradle, "native"), filepath.Join(gradle, "notifications"), filepath.Join(gradle, ".tmp"), filepath.Join(gradle, "wrapper/dists"), filepath.Join(gradle, "caches"), filepath.Join(gradle, "daemon"), filepath.Join(cargo, "registry"), filepath.Join(cargo, "git")}
	writableFiles := []string{filepath.Join(cargo, ".package-cache"), filepath.Join(cargo, ".package-cache-mutate"), filepath.Join(cargo, ".global-cache")}
	for _, rule := range rules {
		if rule.access&pathWrite != 0 {
			if (!rule.file && !slices.Contains(writable, rule.path)) || (rule.file && !slices.Contains(writableFiles, rule.path)) {
				t.Fatalf("unexpected writable developer path: %+v", rule)
			}
			foundNative = foundNative || rule.path == filepath.Join(gradle, "native")
			foundDistributions = foundDistributions || rule.path == filepath.Join(gradle, "wrapper/dists")
		}
		foundConfig = foundConfig || rule.path == filepath.Join(cargo, "config.toml") && rule.file && rule.access == pathRead
		foundToolchain = foundToolchain || rule.path == rustup && !rule.file && rule.access == pathRead
		for _, credential := range []string{filepath.Join(cargo, "credentials"), filepath.Join(cargo, "credentials.toml")} {
			if samePolicyPath(rule.path, credential) || !rule.file && pathWithin(credential, rule.path) {
				t.Fatalf("dedicated credential store covered by runtime convenience: %+v", rule)
			}
		}
	}
	if !foundNative || !foundDistributions || !foundConfig || !foundToolchain {
		t.Fatalf("missing developer paths: %+v", rules)
	}
	if info, err := os.Stat(filepath.Join(gradle, "native")); err != nil || !info.IsDir() {
		t.Fatalf("Gradle's granted native directory must exist before launch: %v", err)
	}
	if info, err := os.Stat(filepath.Join(gradle, "wrapper/dists")); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("Gradle's granted distributions directory must be prepared privately: %v", err)
	}
	if _, err := prepareDeveloperPaths([]pathRule{{gradle, pathRead, false}}); err == nil {
		t.Fatal("Gradle runtime escaped a protected install location")
	}
	t.Setenv("GRADLE_USER_HOME", "relative")
	if _, err := prepareDeveloperPaths(nil); err == nil {
		t.Fatal("relative Gradle root accepted")
	}
}

func TestGoCachePathsAreScopedAndUseFirstGOPATHEntry(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local/share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	first, second := filepath.Join(root, "first go"), filepath.Join(root, "second go")
	modules, build := filepath.Join(root, "module cache"), filepath.Join(root, "build cache")
	t.Setenv("GOPATH", first+string(os.PathListSeparator)+second)
	t.Setenv("GOMODCACHE", modules)
	t.Setenv("GOCACHE", build)
	t.Setenv("GRADLE_USER_HOME", "")
	rules, err := prepareDeveloperPaths(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(first, "pkg/mod"), modules, build} {
		if !slices.Contains(rules, pathRule{path, pathRead | pathWrite, false}) {
			t.Errorf("missing scoped cache: %s", path)
		}
	}
	for _, rule := range rules {
		if pathWithin(rule.path, second) || rule.access&pathWrite != 0 && pathWithin(filepath.Join(first, "bin"), rule.path) {
			t.Errorf("overbroad Go cache grant: %+v", rule)
		}
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("secondary GOPATH was prepared: %v", err)
	}
	if _, err := prepareDeveloperPaths([]pathRule{{build, pathRead, false}}); err == nil {
		t.Fatal("cache path bypassed protected launch inputs")
	}
}

func TestPrepareRuntimeFilePreservesContentAndRejectsAliases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "semgrep.log")
	if err := prepareRuntimeFile(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected private regular log: %v (%v)", info, err)
	}
	const content = "existing scanner output\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareRuntimeFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("existing log changed: %q (%v)", data, err)
	}
	for _, kind := range []string{"symlink", "hardlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			alias := filepath.Join(root, kind)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(path, alias)
			case "hardlink":
				err = os.Link(path, alias)
			case "directory":
				err = os.Mkdir(alias, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareRuntimeFile(alias); err == nil {
				t.Fatal("prepared log alias accepted")
			}
		})
	}
}

func TestUserDataPreparationRejectsProtectedAliases(t *testing.T) {
	for _, name := range []string{"manuals", "opengrep cache"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			data, cache := filepath.Join(root, "data"), filepath.Join(root, "cache")
			t.Setenv("XDG_DATA_HOME", data)
			t.Setenv("XDG_CACHE_HOME", cache)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			target := filepath.Join(root, "protected")
			alias := filepath.Join(data, "man")
			if name == "opengrep cache" {
				alias = filepath.Join(cache, "opengrep")
			}
			for _, path := range []string{target, filepath.Dir(alias)} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := prepareDeveloperPaths(nil); err != nil {
				t.Fatalf("valid user-data roots failed: %v", err)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			if _, err := prepareDeveloperPaths([]pathRule{{target, pathRead, false}}); err == nil {
				t.Fatal("writable user data exposed a protected directory")
			}
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != 0 {
				t.Fatalf("protected directory changed: %v (%v)", entries, err)
			}
		})
	}
}
