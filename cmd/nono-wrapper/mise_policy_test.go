// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMiseControlPlanePathsAndOverridesAreReadOnly(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	for _, name := range []string{"MISE_GLOBAL_CONFIG_FILE", "MISE_CONFIG_FILE", "MISE_SYSTEM_CONFIG_FILE", "MISE_SYSTEM_CONFIG_DIR", "MISE_SYSTEM_DIR"} {
		t.Setenv(name, "")
	}
	var directories []string
	for _, spec := range []struct{ xdg, fallback, override string }{
		{"XDG_CONFIG_HOME", ".config", "MISE_CONFIG_DIR"},
		{"XDG_DATA_HOME", ".local/share", "MISE_DATA_DIR"},
		{"XDG_STATE_HOME", ".local/state", "MISE_STATE_DIR"},
	} {
		xdg, override := filepath.Join(root, spec.xdg), filepath.Join(root, spec.override)
		t.Setenv(spec.xdg, xdg)
		t.Setenv(spec.override, override)
		directories = append(directories, filepath.Join(home, spec.fallback, "mise"), filepath.Join(xdg, "mise"), override)
	}
	configFile := filepath.Join(root, "separate-global/config.toml")
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", configFile)
	paths, err := miseProtectedPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range directories {
		file := filepath.Join(directory, "control-input")
		if err := checkProtectedWrites([]pathRule{{file, pathWrite, true}}, paths); err == nil {
			t.Fatalf("control plane was writable: %s", file)
		}
		if err := checkProtectedWrites([]pathRule{{directory, pathRead, false}}, paths); err != nil {
			t.Fatalf("read-only tool lookup was blocked: %v", err)
		}
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Dir(configFile), pathWrite, false}}, paths); err == nil {
		t.Fatal("an explicit global config file outside the config directory was writable")
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Join(home, "mise-tasks/run"), pathWrite, true}}, paths); err == nil {
		t.Fatal("user-global task code was left writable")
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Join(root, "ordinary-state"), pathWrite, false}}, paths); err != nil {
		t.Fatalf("unrelated application state was blocked: %v", err)
	}
}

func TestMiseCacheIsWritableButControlPlaneAliasesAreNot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local/share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg cache"))
	t.Setenv("MISE_CACHE_DIR", filepath.Join(root, "custom cache"))
	paths, err := miseProtectedPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, cache := range []string{filepath.Join(root, ".cache/mise"), filepath.Join(root, "xdg cache/mise"), filepath.Join(root, "custom cache")} {
		if err := checkProtectedWrites([]pathRule{{cache, pathRead | pathWrite, false}}, paths); err != nil {
			t.Fatalf("accepted Mise cache was blocked: %v", err)
		}
	}
	for _, target := range []string{".config/mise", ".local/state/mise", ".local/share/mise"} {
		t.Setenv("MISE_CACHE_DIR", filepath.Join(root, target))
		if _, err := prepareDeveloperPaths(paths); err == nil {
			t.Fatalf("cache override exposed control input: %s", target)
		}
		if err := os.MkdirAll(filepath.Join(root, target), 0o700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(root, filepath.Base(filepath.Dir(target))+"-cache-alias")
		if err := os.Symlink(filepath.Join(root, target), alias); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MISE_CACHE_DIR", alias)
		if _, err := prepareDeveloperPaths(paths); err == nil {
			t.Fatalf("cache symlink exposed control input: %s", target)
		}
	}
}

func TestMiseConfigurationAliasesIncludeBackingAndFuturePaths(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, config := filepath.Join(root, "home"), filepath.Join(root, "config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("MISE_CONFIG_DIR", filepath.Join(config, "mise"))
	t.Setenv("MISE_GLOBAL_CONFIG_FILE", "")
	t.Setenv("MISE_CONFIG_FILE", "")
	for _, path := range []string{filepath.Join(config, "mise/conf.d"), filepath.Join(root, "backing"), filepath.Join(root, "future")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	backing := filepath.Join(root, "backing/config.toml")
	if err := os.WriteFile(backing, []byte("[settings]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(backing, filepath.Join(config, "mise/config.toml")); err != nil {
		t.Fatal(err)
	}
	future := filepath.Join(root, "future/config.toml")
	if err := os.Symlink(future, filepath.Join(config, "mise/conf.d/future.toml")); err != nil {
		t.Fatal(err)
	}
	// A directory alias cycle must terminate without losing protection.
	if err := os.Symlink(filepath.Join(config, "mise"), filepath.Join(config, "mise/conf.d/again")); err != nil {
		t.Fatal(err)
	}
	paths, err := miseProtectedPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{backing, future} {
		if err := checkProtectedWrites([]pathRule{{filepath.Dir(path), pathWrite, false}}, paths); err == nil {
			t.Fatalf("configuration symlink target was writable: %s", path)
		}
	}
}

func TestMiseConfigurationProtectsIntermediateSymlinks(t *testing.T) {
	for _, mode := range []string{"directory", "file", "missing file", "parent directory"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			home, config := filepath.Join(root, "home"), filepath.Join(root, "home/.config")
			dispatch, backing := filepath.Join(root, "dispatch"), filepath.Join(root, "backing")
			for _, path := range []string{config, dispatch, backing} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", config)
			t.Setenv("MISE_CONFIG_DIR", "")
			t.Setenv("MISE_GLOBAL_CONFIG_FILE", "")
			t.Setenv("MISE_CONFIG_FILE", "")
			entry, target := filepath.Join(config, "mise"), backing
			if mode == "file" || mode == "missing file" {
				if err := os.MkdirAll(entry, 0o700); err != nil {
					t.Fatal(err)
				}
				entry, target = filepath.Join(entry, "config.toml"), filepath.Join(backing, "config.toml")
				if mode == "file" {
					if err := os.WriteFile(target, []byte("[settings]\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			} else if mode == "parent directory" {
				entry = filepath.Join(config, "parent")
				t.Setenv("MISE_CONFIG_DIR", filepath.Join(entry, "mise"))
			}
			intermediate := filepath.Join(dispatch, "alias")
			relative, err := filepath.Rel(dispatch, target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(relative, intermediate); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(intermediate, entry); err != nil {
				t.Fatal(err)
			}
			protected, err := miseProtectedPaths()
			if err != nil {
				t.Fatal(err)
			}
			if err := checkProtectedWrites([]pathRule{{dispatch, pathWrite, false}}, protected); err == nil {
				t.Fatal("a writable directory could replace an intermediate configuration symlink")
			}
			if err := checkProtectedWrites([]pathRule{{filepath.Join(dispatch, "unrelated"), pathWrite, false}}, protected); err != nil {
				t.Fatalf("an unrelated sibling was blocked: %v", err)
			}
		})
	}
}

func TestSymlinkedHomeAllowsWorkspaceButProtectsMise(t *testing.T) {
	root := t.TempDir()
	actual, home := filepath.Join(root, "actual-home"), filepath.Join(root, "home")
	workspace := filepath.Join(actual, "development/project")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MISE_CONFIG_DIR", "")
	protected, err := miseProtectedPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProtectedWrites([]pathRule{{workspace, pathWrite, false}}, protected); err != nil {
		t.Fatalf("ordinary workspace under symlinked HOME was refused: %v", err)
	}
	for _, path := range []string{root, filepath.Join(actual, ".config/mise"), filepath.Join(actual, ".config/mise/config.toml")} {
		if err := checkProtectedWrites([]pathRule{{path, pathWrite, false}}, protected); err == nil {
			t.Fatalf("write access could replace HOME or alter Mise configuration: %s", path)
		}
	}
}

func TestHomeMiseConfigurationProtectsLinkedTargets(t *testing.T) {
	for _, name := range []string{".mise.toml", "mise.toml", ".mise.local.toml", "mise.local.toml", ".mise.production.toml", "mise.production.local.toml", ".config/mise.toml", ".config/mise.production.local.toml", ".mise/conf.d/tools.toml", "mise/config.toml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			home, workspace := filepath.Join(root, "home"), filepath.Join(root, "workspace")
			config, target := filepath.Join(home, name), filepath.Join(workspace, "settings.toml")
			for _, path := range []string{filepath.Dir(config), workspace} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte("[settings]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, config); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("MISE_CONFIG_DIR", "")
			protected, err := miseProtectedPaths()
			if err != nil {
				t.Fatal(err)
			}
			for _, grant := range []pathRule{{config, pathWrite, true}, {target, pathWrite, true}, {workspace, pathWrite, false}} {
				if err := checkProtectedWrites([]pathRule{grant}, protected); err == nil {
					t.Fatalf("HOME configuration remained writable through %q", grant.path)
				}
			}
		})
	}
}

func TestControlPathDiscoveryDoesNotEvaluateMiseConfiguration(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("MISE_CONFIG_DIR", filepath.Join(root, "config/mise"))
	bin, marker := filepath.Join(root, "bin"), filepath.Join(root, "executed")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "config/mise"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mise"), []byte("#!/bin/sh\nprintf executed > \"$HOME/executed\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config/mise/config.toml"), []byte("[env]\nREVIEW = \"{{ exec(command='false') }}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if _, err := protectedLaunchPaths(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("path discovery ran a tool/config: %v", err)
	}
}
