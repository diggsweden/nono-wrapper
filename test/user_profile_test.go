// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLauncherUserProfile(t *testing.T) {
	for _, spec := range launcherSpecs {
		for _, mode := range []string{"home", "xdg", "custom base", "selected user profile", "official fallback", "symlink", "proxy", "base blocks network", "user blocks network"} {
			t.Run(spec.agent+"/"+mode, func(t *testing.T) {
				f := newLauncherFixture(t, spec.agent)
				home := filepath.Dir(f.bin)
				config := filepath.Join(home, ".config")
				profile := spec.profile
				var env []string
				switch mode {
				case "xdg":
					config = filepath.Join(home, "xdg config")
					env = append(env, "XDG_CONFIG_HOME="+config)
				case "custom base":
					profile = "custom-profile"
					env = append(env, "NONO_PROFILE="+profile)
				case "selected user profile":
					profile = spec.agent + "-user"
					env = append(env, "NONO_PROFILE="+profile)
				case "official fallback":
					profile = spec.agent
					env = append(env, "LAUNCHER_TEST_MISSING_PROFILE="+spec.profile)
				case "proxy":
					env = append(env, "HTTPS_PROXY=http://proxy.example:3128")
				case "base blocks network":
					env = append(env, "HTTPS_PROXY=https://unsupported.example", "LAUNCHER_TEST_BASE_BLOCK=true")
				case "user blocks network":
					env = append(env, "HTTPS_PROXY=https://unsupported.example", "LAUNCHER_TEST_USER_BLOCK=true")
				}
				user := spec.agent + "-user"
				path := filepath.Join(config, "nono", "profiles", user+".jsonc")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				contents := "{\"network\":{\"allow_domain\":[\"api.github.com\"]},\"filesystem\":{\"read\":[\"$HOME/reference\"],\"allow\":[\"$HOME/work\"]}}\n"
				if mode == "symlink" {
					target := filepath.Join(home, "user.jsonc")
					writeLauncherFixture(t, target, contents)
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				} else {
					writeLauncherFixture(t, path, contents)
				}
				log := filepath.Join(home, "profile.log")
				env = append(env, "LAUNCHER_PROFILE_LOG="+log)
				stdout, stderr, status := runLauncher(t, f, spec.agent, []string{"--model", "test"}, env)
				if status != 0 {
					t.Fatalf("status=%d, stderr=%q", status, stderr)
				}
				got := capturedLauncherArgs(t, stdout, spec.agent)
				want := []string{"run", "--profile", profile}
				if profile != user {
					want = append(want, "--extends", user)
				}
				want = append(want, "--allow-cwd", "--read-file", f.target)
				if mode == "proxy" {
					want = append(want, "--upstream-proxy", "proxy.example:3128")
				}
				want = append(want, "--", f.target)
				want = append(want, spec.agentArgs...)
				want = append(want, "--model", "test")
				if !slices.Equal(got, want) {
					t.Fatalf("nono arguments:\n got: %q\nwant: %q", got, want)
				}
				calls, err := os.ReadFile(log)
				if err != nil || strings.Count(string(calls), "profile show --json "+user+"\n") != 1 {
					t.Fatalf("user profile was not validated once: %q (%v)", calls, err)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != contents {
					t.Fatalf("user configuration changed: %q (%v)", data, err)
				}
			})
		}
	}
}

func TestLauncherUserProfileErrors(t *testing.T) {
	for _, mode := range []string{"directory", "dangling symlink", "invalid profile", "invalid resolved JSON", "null resolved policy", "relative XDG", "relative HOME"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			path := filepath.Join(filepath.Dir(f.bin), ".config", "nono", "profiles", "opencode-user.jsonc")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			var env []string
			switch mode {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				if err := os.Symlink("missing.jsonc", path); err != nil {
					t.Fatal(err)
				}
			default:
				writeLauncherFixture(t, path, "{}\n")
				switch mode {
				case "invalid profile":
					env = []string{"LAUNCHER_TEST_MISSING_PROFILE=opencode-user"}
				case "invalid resolved JSON":
					env = []string{"LAUNCHER_TEST_USER_JSON=invalid"}
				case "null resolved policy":
					env = []string{"LAUNCHER_TEST_USER_JSON=null"}
				case "relative XDG":
					env = []string{"XDG_CONFIG_HOME=relative"}
				case "relative HOME":
					env = []string{"HOME=relative"}
				}
			}
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, env)
			if status != 125 || len(stdout) != 0 || stderr == "" || strings.Contains(stderr, "using nono's official") {
				t.Fatalf("invalid user configuration did not stop launch: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestLauncherUserProfileDiscovery(t *testing.T) {
	for _, mode := range []string{"project file", "other agent", "XDG replaces HOME"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			home := filepath.Dir(f.bin)
			path := filepath.Join(home, ".config", "nono", "profiles", "opencode-user.jsonc")
			var env []string
			switch mode {
			case "project file":
				path = filepath.Join(f.project, "opencode-user.jsonc")
			case "other agent":
				path = filepath.Join(filepath.Dir(path), "codex-user.jsonc")
			case "XDG replaces HOME":
				env = []string{"XDG_CONFIG_HOME=" + filepath.Join(home, "other config")}
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, path, "{invalid}\n")
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, env)
			if status != 0 || slices.Contains(capturedLauncherArgs(t, stdout, "opencode"), "--extends") {
				t.Fatalf("loaded a user profile outside its configured location: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestLauncherRefusesPolicyDirectoryAsWorkspace(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	stdout, stderr, status := runLauncher(t, f, "opencode", nil, []string{"XDG_CONFIG_HOME=" + filepath.Join(f.project, "config")})
	if status == 0 || len(stdout) != 0 || !strings.Contains(stderr, "cannot share this working directory") {
		t.Fatalf("shared a writable workspace containing active rules: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestLauncherRefusesWritableProfileBackingFile(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	profiles := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	backing := filepath.Join(f.project, "live-profile.jsonc")
	writeLauncherFixture(t, backing, "{}\n")
	if err := os.Symlink(backing, filepath.Join(profiles, "opencode-user.jsonc")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runLauncher(t, f, "opencode", nil, nil)
	if status == 0 || len(stdout) != 0 || !strings.Contains(stderr, "cannot share this working directory") {
		t.Fatalf("shared the live profile's writable backing directory: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}
