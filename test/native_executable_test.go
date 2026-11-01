// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testNativeExecutableProtection(t *testing.T, root, project, wrapper string, env []string) {
	t.Helper()
	install := filepath.Join(root, "guard projects", "managed agent stand-in")
	home, data := filepath.Join(root, "guard home"), filepath.Join(root, "guard data")
	for _, path := range []string{install, home, data} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(install, "opencode")
	const original = "#!/bin/sh\nprintf 'GUARD_AGENT_STARTED\\n'\n"
	if err := os.WriteFile(program, []byte(original), 0o700); err != nil {
		t.Fatal(err)
	}
	nono, err := exec.LookPath("nono")
	if err != nil {
		t.Fatal(err)
	}
	nono, err = filepath.EvalSymlinks(nono)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(nono, filepath.Join(install, "nono")); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+install+string(os.PathListSeparator)+path)
			break
		}
	}
	probeEnv = append(probeEnv, "HOME="+home, "XDG_DATA_HOME="+data)
	invoke := func(directory string, extra []string, args ...string) (string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, wrapper, args...)
		cmd.Dir = directory
		cmd.Env = append(append([]string{}, probeEnv...), extra...)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return string(output), exit.ExitCode()
			}
			t.Fatalf("guard probe: %v\n%s", err, output)
		}
		return string(output), 0
	}
	if output, status := invoke(project, nil, "opencode", "--version"); status != 0 || !strings.Contains(output, "GUARD_AGENT_STARTED") {
		t.Fatalf("safe native control failed: status=%d\n%s", status, output)
	}
	for _, mode := range []string{"user grant", "dispatcher namespace", "ambient grant", "workspace", "state alias", "default state alias", "canonical expression", "Mise config", "Mise state", "Mise cache", "linked global config", "future global config"} {
		t.Run(mode, func(t *testing.T) {
			directory, want := project, "protected launch location"
			var extra []string
			switch mode {
			case "user grant", "dispatcher namespace":
				grant := install
				if mode == "dispatcher namespace" {
					managed := filepath.Join(root, "fake managed data")
					grant = filepath.Join(managed, "command-wrappers/bin")
					if err := os.MkdirAll(grant, 0o700); err != nil {
						t.Fatal(err)
					}
					extra = []string{"MISE_DATA_DIR=" + managed}
				}
				if output, status := invoke(project, nil, "config", "opencode", "add-dir", "--read-write", grant); status != 0 {
					t.Fatalf("configure guard probe: status=%d\n%s", status, output)
				}
				defer func() {
					if output, status := invoke(project, nil, "config", "opencode", "remove-dir", grant); status != 0 {
						t.Errorf("restore guard probe configuration: status=%d\n%s", status, output)
					}
				}()
			case "ambient grant":
				extra = []string{"NONO_ALLOW=" + install}
			case "workspace":
				directory = install
				extra = []string{"NONO_WRAPPER_ROOT=" + filepath.Dir(install)}
			case "state alias", "canonical expression":
				aliasData := filepath.Join(root, "alias "+mode)
				if err := os.Mkdir(aliasData, 0o700); err != nil {
					t.Fatal(err)
				}
				target := install
				if mode == "canonical expression" {
					target, want = filepath.Join(root, "literal-$HOME"), "profile variable"
					if err := os.Mkdir(target, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, filepath.Join(aliasData, "opencode")); err != nil {
					t.Fatal(err)
				}
				extra = []string{"XDG_DATA_HOME=" + aliasData}
			case "default state alias":
				aliasHome := filepath.Join(root, "alias home")
				if err := os.Mkdir(aliasHome, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(install, filepath.Join(aliasHome, ".npm")); err != nil {
					t.Fatal(err)
				}
				extra = []string{"HOME=" + aliasHome}
			case "Mise config", "Mise state", "Mise cache", "linked global config", "future global config":
				aliasData := filepath.Join(root, "control alias "+mode)
				if err := os.Mkdir(aliasData, 0o700); err != nil {
					t.Fatal(err)
				}
				value := func(name string) string {
					for i := len(probeEnv) - 1; i >= 0; i-- {
						if value, ok := strings.CutPrefix(probeEnv[i], name+"="); ok {
							return value
						}
					}
					return ""
				}
				target := value("MISE_CONFIG_DIR")
				if target == "" {
					target = filepath.Join(value("XDG_CONFIG_HOME"), "mise")
				}
				if mode == "Mise state" {
					target = value("MISE_STATE_DIR")
					if target == "" {
						target = filepath.Join(value("XDG_STATE_HOME"), "mise")
					}
				}
				if mode == "Mise cache" {
					target = value("MISE_CACHE_DIR")
					if target == "" {
						target = filepath.Join(value("XDG_CACHE_HOME"), "mise")
					}
				}
				if mode == "linked global config" || mode == "future global config" {
					config := filepath.Join(root, "separate config "+mode)
					target = filepath.Join(root, "backing source "+mode)
					for _, path := range []string{config, target} {
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					backing := filepath.Join(target, "config.toml")
					if mode == "linked global config" {
						if err := os.WriteFile(backing, []byte("[settings]\n"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					file := filepath.Join(config, "config.toml")
					if err := os.Symlink(backing, file); err != nil {
						t.Fatal(err)
					}
					extra = append(extra, "MISE_CONFIG_DIR="+config, "MISE_GLOBAL_CONFIG_FILE="+file)
				}
				if err := os.MkdirAll(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(aliasData, "opencode")); err != nil {
					t.Fatal(err)
				}
				extra = append(extra, "XDG_DATA_HOME="+aliasData)
			}
			output, status := invoke(directory, extra, "opencode", "--version")
			if status != 125 || !strings.Contains(output, want) || strings.Contains(output, "GUARD_AGENT_STARTED") || strings.Contains(output, "Applying sandbox") {
				t.Fatalf("agent was not refused before sandbox execution: status=%d\n%s", status, output)
			}
			if content, err := os.ReadFile(program); err != nil || string(content) != original {
				t.Fatalf("guard modified the executable stand-in: %q (%v)", content, err)
			}
		})
	}
}
