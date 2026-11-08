// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparedRuntimePathsMatchConfiguredLocations(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			for name, suffix := range map[string]string{"XDG_CONFIG_HOME": ".config", "XDG_DATA_HOME": ".local/share", "XDG_CACHE_HOME": ".cache", "XDG_STATE_HOME": ".local/state", "CODEX_HOME": ".codex"} {
				t.Setenv(name, filepath.Join(home, suffix))
			}
			paths, err := prepareAgentState(agent, nil)
			if err != nil || len(paths) != 0 {
				t.Fatalf("default paths should need no session additions: %v (%v)", paths, err)
			}
			want := map[string]bool{}
			if agent == "opencode" {
				for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
					base := filepath.Join(root, name)
					t.Setenv(name, base)
					want[filepath.Join(base, "opencode")] = true
					if name == "XDG_DATA_HOME" {
						want[filepath.Join(base, "opentui")] = true
					}
				}
			} else {
				path := filepath.Join(root, "custom codex")
				t.Setenv("CODEX_HOME", path)
				want[path] = true
			}
			paths, err = prepareAgentState(agent, nil)
			if err != nil || len(paths) != len(want) {
				t.Fatalf("configured paths missing from runtime policy: %v (%v)", paths, err)
			}
			for _, rule := range paths {
				if !want[rule.path] || rule.access != pathRead|pathWrite || rule.file {
					t.Fatalf("runtime rule is broader than the configured state directory: %#v", rule)
				}
				info, err := os.Stat(rule.path)
				if err != nil || !info.IsDir() {
					t.Fatalf("policy and state preparation disagree: %s (%v)", rule.path, err)
				}
				delete(want, rule.path)
			}
		})
	}
}

func TestRuntimeDirectoriesAreNotReinterpretedAsProfileExpressions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	path := filepath.Join(root, "literal-$HOME")
	t.Setenv("XDG_DATA_HOME", path)
	if rules, err := prepareAgentState("opencode", nil); err == nil || len(rules) != 0 || !strings.Contains(err.Error(), "profile variable") {
		t.Fatalf("resolved path would be reinterpreted as a different permission: %#v (%v)", rules, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("rejected runtime expression created a state directory: %v", err)
	}
}

func TestCanonicalRuntimeTargetsMustBeLiteral(t *testing.T) {
	for _, name := range []string{"literal-$HOME", "literal-$WORKDIR", "literal-$NONO_CONFIG", "cache*", "cache?", "cache[1]"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
			data, target := filepath.Join(root, "data"), filepath.Join(root, name)
			for _, path := range []string{data, target} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, filepath.Join(data, "opencode")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_DATA_HOME", data)
			if paths, err := prepareAgentState("opencode", nil); err == nil || len(paths) != 0 {
				t.Fatalf("accepted an interpreted canonical state path: %v (%v)", paths, err)
			}
			if _, err := os.Stat(filepath.Join(data, "opentui")); !os.IsNotExist(err) {
				t.Fatalf("invalid canonical state path did not stop preparation: %v", err)
			}
		})
	}
}
