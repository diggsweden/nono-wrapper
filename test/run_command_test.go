// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunCommandArgumentBoundary(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		for _, separator := range []bool{false, true} {
			t.Run(agent+"/separator="+map[bool]string{true: "yes", false: "no"}[separator], func(t *testing.T) {
				f := newLauncherFixture(t, agent)
				args := []string{"--profile", "custom-offline", agent}
				if separator {
					args = append(args, "--")
				}
				forwarded := []string{"--help", "--profile", "agent-profile", "", "literal*argument", "line\nbreak"}
				args = append(args, forwarded...)
				stdout, stderr, status := runLauncher(t, f, "run", args, []string{"NONO_PROFILE=ignored"})
				if status != 0 || stderr != emptySessionReport(agent, 0) {
					t.Fatalf("status=%d stderr=%s", status, stderr)
				}
				got := capturedLauncherArgs(t, stdout, agent)
				want := append(expectedRunProfile("custom-offline", agent == "opencode"), "--allow-cwd", "--read-file", f.target, "--", f.target)
				if agent == "opencode" {
					want = append(want, "--standalone")
				} else {
					want = append(want, launcherSpecs[1].agentArgs...)
				}
				want = append(want, forwarded...)
				if !slices.Equal(got, want) {
					t.Fatalf("got %q\nwant %q", got, want)
				}
			})
		}
	}
}

func TestRunCommandTemporaryFlags(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	directory := filepath.Join(f.project, "shared directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "file")
	writeLauncherFixture(t, file, "fixture\n")
	args := []string{"--allow-domain", "example.com", "--allow-domain", "other.example", "--open-port", "3000",
		"--read", directory, "--write", directory, "--allow", directory, "--read-file", file, "--write-file", file, "--allow-file", file, "codex"}
	stdout, stderr, status := runLauncher(t, f, "run", args, nil)
	if status != 0 || stderr != emptySessionReport("codex", 0) {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
	got := capturedLauncherArgs(t, stdout, "codex")
	for _, flag := range []string{"--allow-domain", "--open-port", "--read", "--write", "--allow", "--read-file", "--write-file", "--allow-file", "--no-diagnostics"} {
		if !slices.Contains(got, flag) {
			t.Errorf("missing temporary flag %s: %q", flag, got)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles/codex-user.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("temporary flags changed persistent configuration: %v", err)
	}
}

func TestRunDirectoryFlagsSuggestFileFlags(t *testing.T) {
	for _, flag := range []string{"--read", "--write", "--allow"} {
		t.Run(flag, func(t *testing.T) {
			f := newLauncherFixture(t, "codex")
			file := filepath.Join(f.project, "reference.txt")
			writeLauncherFixture(t, file, "fixture\n")
			stdout, stderr, status := runLauncher(t, f, "run", []string{flag, file, "codex"}, nil)
			if status == 0 || len(stdout) != 0 || !strings.Contains(stderr, "use "+flag+"-file") {
				t.Fatalf("status=%d stdout=%q stderr=%s", status, stdout, stderr)
			}
		})
	}
}

func TestRunMissingDirectoryUsesNoInputStatus(t *testing.T) {
	for _, flag := range []string{"--read", "--write", "--allow"} {
		t.Run(flag, func(t *testing.T) {
			f := newLauncherFixture(t, "codex")
			missing := filepath.Join(f.project, "missing-directory")
			stdout, stderr, status := runLauncher(t, f, "run", []string{flag, missing, "codex"}, nil)
			if status != 66 || len(stdout) != 0 || !strings.Contains(stderr, missing) {
				t.Fatalf("status=%d stdout=%q stderr=%s", status, stdout, stderr)
			}
		})
	}
}

func TestRunBlockNetSelectsOfflineDefaults(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		f := newLauncherFixture(t, agent)
		stdout, stderr, status := runLauncher(t, f, "run", []string{"--block-net", agent}, nil)
		if status != 0 {
			t.Fatalf("%s: status=%d stderr=%s", agent, status, stderr)
		}
		args := capturedLauncherArgs(t, stdout, agent)
		if !slices.Contains(args, "--block-net") || !slices.Contains(args, agent+"-offline") {
			t.Fatalf("block-net inherited online defaults: %q", args)
		}
	}
}

func TestRunPrivateLoopbackUsesNativeIsolation(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	stdout, stderr, status := runLauncher(t, f, "run", []string{"--block-net", "--isolate", "net,loopback", "codex", "--", "features", "list"}, nil)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
	args := capturedLauncherArgs(t, stdout, "codex")
	index := slices.Index(args, "--isolate")
	if index < 0 || index+1 >= len(args) || args[index+1] != "net,loopback" || !slices.Contains(args, "codex-offline") {
		t.Fatalf("private loopback must preserve native namespace syntax and offline policy: %q", args)
	}
	if slices.Contains(args, "--open-port") || slices.Contains(args, "--listen-port") || slices.Contains(args, "--allow-connect-port") {
		t.Fatalf("private loopback added host TCP grants: %q", args)
	}
}

func TestRunCannotWidenReadOnlyUserAllowance(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	shared := filepath.Join(filepath.Dir(f.bin), "read-only reference")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	profiles := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	user, err := json.Marshal(map[string]any{"network": map[string]any{"block": false}, "filesystem": map[string]any{"read": []string{shared}}})
	if err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, filepath.Join(profiles, "codex-user.jsonc"), string(user))
	writeLauncherFixture(t, filepath.Join(profiles, "codex-chatgpt.jsonc"), `{}`)
	manifest, err := json.Marshal(map[string]any{"version": "0.1.0", "filesystem": map[string]any{"grants": []map[string]string{{"path": shared, "access": "read", "type": "directory"}}}})
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runLauncher(t, f, "run", []string{"--allow", shared, "codex"}, []string{
		"LAUNCHER_TEST_USER_JSON=" + string(user), "LAUNCHER_TEST_MANIFEST_JSON=" + string(manifest),
	})
	if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, "cannot enforce read-only user access") {
		t.Fatalf("temporary grant silently widened a user restriction: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestRunKeepsCommaValuesLiteral(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	path := filepath.Join(f.project, "comma,directory")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	url := "https://models.example.com/catalog?type=chat,decision,image"
	stdout, stderr, status := runLauncher(t, f, "run", []string{"--read", path, "--allow-domain", url, "codex"}, nil)
	if status != 0 {
		t.Fatalf("status=%d stderr=%s", status, stderr)
	}
	args := capturedLauncherArgs(t, stdout, "codex")
	if !slices.Contains(args, path) || !slices.Contains(args, url) {
		t.Fatalf("comma-separated data was split into grants: %q", args)
	}
}

func TestRunCommandDiagnosticsAndSilent(t *testing.T) {
	for _, mode := range []string{"default", "native", "silent"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "codex")
			var args []string
			if mode == "native" {
				args = append(args, "--no-diagnostics=false")
			} else if mode == "silent" {
				args = append(args, "--silent")
			}
			stdout, stderr, status := runLauncher(t, f, "run", append(args, "codex"), []string{"LAUNCHER_TEST_EXIT=7"})
			if status != 7 {
				t.Fatalf("status=%d stderr=%s", status, stderr)
			}
			got := capturedLauncherArgs(t, stdout, "codex")
			if slices.Contains(got, "--no-diagnostics") != (mode != "native") || slices.Contains(got, "--silent") != (mode == "silent") {
				t.Fatalf("incorrect diagnostic flags: %q", got)
			}
			if (mode == "silent" && stderr != "") || (mode != "silent" && stderr != emptySessionReport("codex", 7)) {
				t.Fatalf("unexpected report: %q", stderr)
			}
		})
	}
}

func TestRunCommandRejectsUnsafeGrantsAndUsage(t *testing.T) {
	for _, mode := range []string{"executable directory", "executable file", "policy", "unknown agent", "unknown flag", "invalid port"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "codex")
			var args []string
			switch mode {
			case "executable directory":
				args = []string{"--allow", filepath.Dir(f.target), "codex"}
			case "executable file":
				args = []string{"--allow-file", f.target, "codex"}
			case "policy":
				args = []string{"--write", filepath.Join(filepath.Dir(f.bin), ".config/nono"), "codex"}
			case "unknown agent":
				args = []string{"other"}
			case "unknown flag":
				args = []string{"--not-a-flag", "codex"}
			case "invalid port":
				args = []string{"--open-port", "65536", "codex"}
			}
			stdout, stderr, status := runLauncher(t, f, "run", args, nil)
			if status == 0 || len(stdout) != 0 || stderr == "" {
				t.Fatalf("unsafe or invalid launch accepted: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestConfigNonoNamesPreserveAccessModes(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			file := filepath.Join(f.project, "file")
			writeLauncherFixture(t, file, "fixture\n")
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles", agent+"-user.jsonc")
			for _, spec := range []struct{ command, field, value string }{
				{"allow", "allow", f.project}, {"read", "read", f.project},
				{"allow-file", "allow_file", file}, {"read-file", "read_file", file},
				{"add-dir", "read", f.project},
			} {
				stdout, stderr, status := runLauncher(t, f, "config", []string{agent, spec.command, spec.value}, nil)
				if status != 0 || stderr != "" {
					t.Fatalf("%s: status=%d stdout=%q stderr=%q", spec.command, status, stdout, stderr)
				}
				if !slices.Contains(configContents(t, path)["filesystem"][spec.field], spec.value) {
					t.Fatalf("%s did not save %s access", spec.command, spec.field)
				}
			}
			for _, name := range []string{"open-port", "remove-open-port", "allow-tcp-port", "remove-tcp-port"} {
				_, stderr, status := runLauncher(t, f, "config", []string{agent, name, "3000"}, nil)
				if status != 0 || stderr != "" {
					t.Fatalf("%s: status=%d stderr=%q", name, status, stderr)
				}
			}
		})
	}
}

func TestVersionAliases(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	var want string
	for _, flag := range []string{"--version", "-V", "-v"} {
		stdout, stderr, status := runLauncher(t, f, "", []string{flag}, nil)
		if status != 0 || stderr != "" || !strings.Contains(string(stdout), "nono-wrapper version") {
			t.Fatalf("%s: status=%d stdout=%q stderr=%q", flag, status, stdout, stderr)
		}
		if want != "" && want != string(stdout) {
			t.Fatalf("%s version output differs", flag)
		}
		want = string(stdout)
	}
}
