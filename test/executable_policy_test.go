// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchChecksEffectiveExecutableWritesBeforeRunningAgent(t *testing.T) {
	for _, mode := range []string{"inherited directory", "external profile", "explicit file", "standalone manager", "read-only", "ambient allow", "literal ambient alias", "workspace", "managed cached version", "runtime alias", "Mise config alias", "Mise trust alias", "Mise cache alias", "global config override", "invalid manifest"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			home := filepath.Dir(f.bin)
			path, access, kind := filepath.Dir(f.target), "readwrite", "directory"
			var env []string
			if mode == "external profile" {
				env = append(env, "NONO_PROFILE=custom")
			}
			switch mode {
			case "explicit file":
				path, kind = f.target, "file"
			case "standalone manager":
				path, kind = filepath.Join(f.bin, "mise"), "file"
				writeLauncherFixture(t, path, "manager stand-in\n")
			case "read-only":
				access = "read"
			case "ambient allow", "literal ambient alias":
				if mode == "literal ambient alias" {
					path = filepath.Join(home, "literal-$HOME")
					if err := os.Symlink(filepath.Dir(f.target), path); err != nil {
						t.Fatal(err)
					}
				}
				env = append(env, "NONO_ALLOW="+path)
				path = ""
			case "workspace":
				target := filepath.Join(f.project, "opencode")
				writeLauncherFixture(t, target, "agent stand-in\n")
				if err := os.Remove(filepath.Join(f.bin, "opencode")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(f.bin, "opencode")); err != nil {
					t.Fatal(err)
				}
				path = ""
			case "managed cached version":
				data := filepath.Join(home, "managed data")
				path = filepath.Join(data, "installs/other-tool/1")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				env = append(env, "MISE_DATA_DIR="+data)
			case "runtime alias":
				data := filepath.Join(home, "custom data")
				if err := os.Mkdir(data, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(f.target), filepath.Join(data, "opencode")); err != nil {
					t.Fatal(err)
				}
				env = append(env, "XDG_DATA_HOME="+data)
				path = ""
			case "Mise config alias", "Mise trust alias", "Mise cache alias", "global config override":
				data := filepath.Join(home, "relocated state")
				target := filepath.Join(home, ".config/mise")
				if mode == "Mise trust alias" {
					target = filepath.Join(home, ".local/state/mise")
				}
				if mode == "Mise cache alias" {
					target = filepath.Join(home, ".cache/mise")
				}
				if mode == "global config override" {
					target = filepath.Join(home, "global source")
					env = append(env, "MISE_GLOBAL_CONFIG_FILE="+filepath.Join(target, "config.toml"))
				}
				for _, directory := range []string{data, target} {
					if err := os.MkdirAll(directory, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, filepath.Join(data, "opencode")); err != nil {
					t.Fatal(err)
				}
				env = append(env, "XDG_DATA_HOME="+data)
				path = ""
			}
			grants := []map[string]string{}
			if path != "" {
				grants = append(grants, map[string]string{"path": path, "access": access, "type": kind})
			}
			manifest, err := json.Marshal(map[string]any{"version": "0.1.0", "filesystem": map[string]any{"grants": grants}})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "invalid manifest" {
				manifest = []byte("{broken")
			}
			env = append(env, "LAUNCHER_TEST_MANIFEST_JSON="+string(manifest))
			stdout, stderr, status := runLauncher(t, f, "opencode", []string{"--version"}, env)
			if mode == "read-only" {
				if status != 0 || len(stdout) == 0 {
					t.Fatalf("read-only executable access was refused: status=%d stderr=%q", status, stderr)
				}
				return
			}
			want := "protected launch location"
			if mode == "invalid manifest" {
				want = "invalid filesystem manifest"
			}
			if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, want) {
				t.Fatalf("unsafe agent launch was not refused: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}
