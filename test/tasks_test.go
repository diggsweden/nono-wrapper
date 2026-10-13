// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type taskFixture struct {
	root, home, tools, log, nonoLog, just string
}

func fakeLinuxController(t *testing.T, f taskFixture) {
	t.Helper()
	path := filepath.Join(f.tools, "uname")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, path, "#!/bin/sh\ncase \"$1\" in -s) printf '%s\\n' \"${TEST_CONTROLLER_OS:-Linux}\" ;; -m) printf 'x86_64\\n' ;; *) exit 99 ;; esac\n")
}

func checksumTool(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"sha256sum", "shasum"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	t.Fatal("tests require sha256sum or shasum")
	return ""
}

func newTaskFixture(t *testing.T) taskFixture {
	t.Helper()
	base := canonicalTempDir(t)
	f := taskFixture{
		root:    filepath.Join(base, "checkout with spaces"),
		home:    filepath.Join(base, "user home"),
		tools:   filepath.Join(base, "commands"),
		log:     filepath.Join(base, "podman.log"),
		nonoLog: filepath.Join(base, "nono.log"),
	}
	for _, path := range []string{f.root, f.home, f.tools} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := []string{"justfile", "test/tools", "test/packs", "go.mod", "go.sum"}
	for _, directory := range []string{"cmd/nono-wrapper", "docs/manpages", "profiles"} {
		entries, err := os.ReadDir(filepath.Join(testRepository(t), directory))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				t.Fatalf("unexpected source directory: %s/%s", directory, entry.Name())
			}
			files = append(files, filepath.Join(directory, entry.Name()))
		}
	}
	for _, file := range files {
		contents, err := os.ReadFile(filepath.Join(testRepository(t), file))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeLauncherFixture(t, path, string(contents))
	}
	// Distinct fixture versions prove setup reads the shared manifest.
	writeLauncherFixture(t, filepath.Join(f.root, "test/packs"), "# fixture pins\n\nnolabs-ai/opencode 1.2.3\nnolabs-ai/codex 4.5.6\n")
	// Only these utilities and fake nono/podman are exposed to the recipes.
	for _, tool := range []string{"just", "go", "bash", "uname", "mkdir", "install", "rm", "rmdir"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("task tests require %s: %v", tool, err)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		if tool == "just" {
			f.just = path
			continue
		}
		if err := os.Symlink(path, filepath.Join(f.tools, tool)); err != nil {
			t.Fatal(err)
		}
	}
	writeLauncherFixture(t, filepath.Join(f.tools, "podman"), `#!/bin/sh
case "$1" in
images)
  printf '%s\n' 'localhost/nono-agent-profiles:fixture' 'localhost/unrelated:keep'
  ;;
rmi) printf '%s\n' "$*" >> "$TASK_PODMAN_LOG" ;;
*) exit 99 ;;
esac
`)
	writeLauncherFixture(t, filepath.Join(f.tools, "nono"), `#!/bin/sh
printf '%s\n' "$*" >> "$TASK_NONO_LOG"
if [ "${TASK_NONO_FAIL:-}" = "$1" ]; then
  printf 'fixture nono failure: %s\n' "$1" >&2
  exit 1
fi
case "$1" in
pull|pin) [ "$#" -eq 2 ] || exit 99 ;;
profile)
  [ "$#" -eq 3 ] && [ "$2" = show ] || exit 99
  case "$3" in opencode-chatgpt|codex-chatgpt) ;; *) exit 99 ;; esac
  [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/nono/profiles/$3.jsonc" ] || exit 1
  ;;
*) exit 99 ;;
esac
`)
	return f
}

func runTask(t *testing.T, f taskFixture, task string, env ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Deliberately run elsewhere: cleanup must use the justfile's directory.
	cmd := exec.CommandContext(ctx, f.just, "--justfile", filepath.Join(f.root, "justfile"),
		"--working-directory", f.home, task)
	cmd.Dir = f.home
	cmd.Env = append([]string{"PATH=" + f.tools, "HOME=" + f.home, "TASK_PODMAN_LOG=" + f.log, "TASK_NONO_LOG=" + f.nonoLog}, buildEnv...)
	cmd.Env = append(cmd.Env, env...)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out: %s", task, output)
	}
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		t.Fatal(err)
	}
	return string(output), cmd.ProcessState.ExitCode()
}

func TestInstallLocal(t *testing.T) {
	for _, mode := range []string{"default", "xdg", "install-dir precedence"} {
		t.Run(mode, func(t *testing.T) {
			f := newTaskFixture(t)
			bin := filepath.Join(f.home, ".local/bin")
			config := filepath.Join(f.home, ".config")
			var env []string
			if mode != "default" {
				bin = filepath.Join(f.home, "xdg bin")
				config = filepath.Join(f.home, "xdg config")
				env = []string{"XDG_BIN_HOME=" + bin, "XDG_CONFIG_HOME=" + config}
			}
			if mode == "install-dir precedence" {
				bin = filepath.Join(f.home, "explicit bin")
				env = append(env, "INSTALL_DIR="+bin)
			}
			profiles := filepath.Join(config, "nono/profiles")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			userConfig := "{\"network\":{\"allow_domain\":[\"api.github.com\"]},\"filesystem\":{\"read\":[\"$HOME/reference\"],\"allow\":[\"$HOME/work\"]}}\n"
			for _, agent := range []string{"opencode", "codex"} {
				writeLauncherFixture(t, filepath.Join(profiles, agent+"-user.jsonc"), userConfig)
			}
			for attempt := 0; attempt < 2; attempt++ {
				output, status := runTask(t, f, "install-local", env...)
				if status != 0 {
					t.Fatalf("install failed: %s", output)
				}
				for _, agent := range []string{"opencode", "codex"} {
					data, err := os.ReadFile(filepath.Join(profiles, agent+"-user.jsonc"))
					if err != nil || string(data) != userConfig {
						t.Fatalf("install overwrote %s's user configuration: %q (%v)", agent, data, err)
					}
				}
				wantCalls := "pull nolabs-ai/opencode@1.2.3\npin nolabs-ai/opencode\n" +
					"pull nolabs-ai/codex@4.5.6\npin nolabs-ai/codex\n" +
					"profile show opencode-chatgpt\nprofile show codex-chatgpt\n"
				calls, err := os.ReadFile(f.nonoLog)
				if err != nil || string(calls) != strings.Repeat(wantCalls, attempt+1) {
					t.Fatalf("incorrect pack installation or profile checks: %q (%v)", calls, err)
				}
				if !strings.Contains(output, "Default profiles verified: opencode-chatgpt, codex-chatgpt") {
					t.Fatalf("installation did not report the selected profiles: %s", output)
				}
				for _, source := range []struct {
					directory, destination string
					mode                   os.FileMode
				}{
					{".cache/bin", bin, 0o755},
					{"profiles", profiles, 0o644},
				} {
					entries, err := os.ReadDir(filepath.Join(f.root, source.directory))
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						want, err := os.ReadFile(filepath.Join(f.root, source.directory, entry.Name()))
						if err != nil {
							t.Fatal(err)
						}
						path := filepath.Join(source.destination, entry.Name())
						got, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(got, want) {
							t.Fatalf("installed content differs for %s: %v", path, err)
						}
						info, err := os.Stat(path)
						if err != nil || info.Mode().Perm() != source.mode {
							t.Fatalf("wrong permissions for %s: %v", path, err)
						}
					}
				}
				installed := filepath.Join(bin, "nono-wrapper")
				cmd := exec.Command(installed, "--help")
				cmd.Env = []string{"PATH=" + f.tools, "HOME=" + f.home}
				if output, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(output), "Run OpenCode and Codex") {
					t.Fatalf("installed CLI does not run: %v (%s)", err, output)
				}
				if attempt == 0 {
					entries, err := os.ReadDir(bin)
					if err != nil || len(entries) != 1 || entries[0].Name() != "nono-wrapper" {
						t.Fatalf("expected one installed executable: %v (%v)", entries, err)
					}
					if err := os.Remove(installed); err != nil {
						t.Fatal(err)
					}
					writeLauncherFixture(t, installed, "outdated\n")
					writeLauncherFixture(t, filepath.Join(bin, "unrelated-command"), "keep\n")
					writeLauncherFixture(t, filepath.Join(profiles, "custom.jsonc"), "keep\n")
				}
			}
			for _, path := range []string{filepath.Join(bin, "unrelated-command"), filepath.Join(profiles, "custom.jsonc")} {
				contents, err := os.ReadFile(path)
				if err != nil || string(contents) != "keep\n" {
					t.Fatalf("unrelated file changed: %s (%v)", path, err)
				}
			}
		})
	}
}

func TestInstallLocalRejectsUnsafeDestinations(t *testing.T) {
	for _, env := range []string{"INSTALL_DIR=relative", "XDG_CONFIG_HOME=relative", "HOME="} {
		t.Run(env, func(t *testing.T) {
			f := newTaskFixture(t)
			output, status := runTask(t, f, "install-local", env)
			if status == 0 {
				t.Fatalf("accepted unsafe destination: %s", output)
			}
			if _, err := os.Stat(filepath.Join(f.home, ".local")); !os.IsNotExist(err) {
				t.Fatalf("installation wrote files before rejecting the destination: %v", err)
			}
			if _, err := os.Stat(f.nonoLog); !os.IsNotExist(err) {
				t.Fatalf("installation ran nono before rejecting the destination: %v", err)
			}
		})
	}
}

func TestInstallLocalRequiresNono(t *testing.T) {
	f := newTaskFixture(t)
	if err := os.Remove(filepath.Join(f.tools, "nono")); err != nil {
		t.Fatal(err)
	}
	output, status := runTask(t, f, "install-local")
	if status == 0 || !strings.Contains(output, "nono is required; run just install-dev-tools") {
		t.Fatalf("missing nono was not reported: status=%d (%s)", status, output)
	}
	if _, err := os.Stat(filepath.Join(f.home, ".local")); !os.IsNotExist(err) {
		t.Fatalf("installation copied files with nono missing: %v", err)
	}
}

func TestInstallLocalStopsOnNonoFailure(t *testing.T) {
	for _, stage := range []string{"pull", "pin", "profile"} {
		t.Run(stage, func(t *testing.T) {
			f := newTaskFixture(t)
			output, status := runTask(t, f, "install-local", "TASK_NONO_FAIL="+stage)
			if status == 0 || !strings.Contains(output, "fixture nono failure: "+stage) || strings.Contains(output, "Default profiles verified:") {
				t.Fatalf("failed setup reported success: status=%d (%s)", status, output)
			}
			calls, err := os.ReadFile(f.nonoLog)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(calls)), "\n")
			if !strings.HasPrefix(lines[len(lines)-1], stage+" ") {
				t.Fatalf("installation continued after a failed %s: %s", stage, calls)
			}
			if stage != "profile" {
				if _, err := os.Stat(filepath.Join(f.home, ".local/bin/nono-wrapper")); !os.IsNotExist(err) {
					t.Fatalf("installation copied the binary after a pack failure: %v", err)
				}
			}
		})
	}
}

func TestCleanRemovesOnlyOwnedArtifacts(t *testing.T) {
	for _, extra := range []bool{false, true} {
		name := "empty cache removed"
		if extra {
			name = "extra files preserved"
		}
		t.Run(name, func(t *testing.T) {
			f := newTaskFixture(t)
			cache := filepath.Join(f.root, ".cache")
			tools := filepath.Join(cache, "tools")
			if err := os.MkdirAll(tools, 0o700); err != nil {
				t.Fatal(err)
			}
			generated := []string{"nono.tar.gz", "codex.tar.gz", "opencode.tar.gz", "nono", "codex", "opencode", "agent-probe", "nono-wrapper", "nono-wrapper.sources", "nono.sha256", "codex.sha256", "opencode.sha256"}
			for _, name := range generated {
				writeLauncherFixture(t, filepath.Join(tools, name), "generated\n")
			}
			bin := filepath.Join(cache, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, filepath.Join(bin, "nono-wrapper"), "generated\n")
			outside := filepath.Join(f.home, ".cache", "keep")
			if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, outside, "keep\n")
			if extra {
				writeLauncherFixture(t, filepath.Join(tools, "unrelated"), "keep\n")
				writeLauncherFixture(t, filepath.Join(bin, "unrelated"), "keep\n")
			}
			output, status := runTask(t, f, "test-clean")
			if status != 0 {
				t.Fatalf("cleanup failed: %s", output)
			}
			for _, name := range generated {
				if _, err := os.Lstat(filepath.Join(tools, name)); !os.IsNotExist(err) {
					t.Fatalf("generated file remains: %s (%v)", name, err)
				}
			}
			if _, err := os.Lstat(filepath.Join(bin, "nono-wrapper")); !os.IsNotExist(err) {
				t.Fatalf("native build remains: %v", err)
			}
			if !extra {
				if _, err := os.Stat(cache); !os.IsNotExist(err) {
					t.Fatalf("empty cache remains: %v", err)
				}
			} else if contents, err := os.ReadFile(filepath.Join(tools, "unrelated")); err != nil || string(contents) != "keep\n" {
				t.Fatalf("unrelated cache file changed: %v", err)
			}
			if extra {
				if contents, err := os.ReadFile(filepath.Join(bin, "unrelated")); err != nil || string(contents) != "keep\n" {
					t.Fatalf("unrelated build file changed: %v", err)
				}
			}
			if contents, err := os.ReadFile(outside); err != nil || string(contents) != "keep\n" {
				t.Fatalf("cleanup touched the working directory instead of the checkout: %v", err)
			}
			log, err := os.ReadFile(f.log)
			if err != nil || string(log) != "rmi --ignore --no-prune localhost/nono-agent-profiles:fixture\n" {
				t.Fatalf("unexpected image deletion: %q (%v)", log, err)
			}
		})
	}
}

func TestCleanRefusesSymlinkedCacheDirectories(t *testing.T) {
	for _, directory := range []string{".cache", ".cache/tools", ".cache/bin"} {
		t.Run(directory, func(t *testing.T) {
			f := newTaskFixture(t)
			outside := filepath.Join(f.home, "outside")
			link := filepath.Join(f.root, directory)
			if err := os.MkdirAll(filepath.Join(outside, "tools"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(outside, "nono"), filepath.Join(outside, "tools/nono"), filepath.Join(outside, "nono-wrapper")} {
				writeLauncherFixture(t, path, "keep\n")
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			output, status := runTask(t, f, "test-clean")
			if status == 0 || !strings.Contains(output, "Refusing cleanup of symlinked") {
				t.Fatalf("symlinked cache was accepted: %s", output)
			}
			for _, path := range []string{filepath.Join(outside, "nono"), filepath.Join(outside, "tools/nono"), filepath.Join(outside, "nono-wrapper")} {
				if contents, err := os.ReadFile(path); err != nil || string(contents) != "keep\n" {
					t.Fatalf("symlink target changed: %s (%v)", path, err)
				}
			}
			if _, err := os.Stat(f.log); !os.IsNotExist(err) {
				t.Fatalf("image removal ran after cache refusal: %v", err)
			}
		})
	}
}

func TestCleanDoesNotFollowArtifactSymlinks(t *testing.T) {
	f := newTaskFixture(t)
	tools := filepath.Join(f.root, ".cache/tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(f.home, "keep")
	writeLauncherFixture(t, outside, "keep\n")
	if err := os.Symlink(outside, filepath.Join(tools, "nono")); err != nil {
		t.Fatal(err)
	}
	output, status := runTask(t, f, "test-clean")
	if status != 0 {
		t.Fatalf("cleanup failed: %s", output)
	}
	if contents, err := os.ReadFile(outside); err != nil || string(contents) != "keep\n" {
		t.Fatalf("artifact symlink target changed: %v", err)
	}
}

func TestCleanDoesNotRecursivelyRemoveArtifactDirectories(t *testing.T) {
	f := newTaskFixture(t)
	directory := filepath.Join(f.root, ".cache/tools/nono")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "keep")
	writeLauncherFixture(t, path, "keep\n")
	output, status := runTask(t, f, "test-clean")
	if status == 0 {
		t.Fatalf("cleanup accepted a directory in place of an artifact: %s", output)
	}
	if contents, err := os.ReadFile(path); err != nil || string(contents) != "keep\n" {
		t.Fatalf("cleanup recursively removed data: %v", err)
	}
	if _, err := os.Stat(f.log); !os.IsNotExist(err) {
		t.Fatalf("image removal ran after cache failure: %v", err)
	}
}

func TestCleanRefusesUnrecognizedCheckout(t *testing.T) {
	f := newTaskFixture(t)
	if err := os.Remove(filepath.Join(f.root, "profiles/agent-policy.jsonc")); err != nil {
		t.Fatal(err)
	}
	output, status := runTask(t, f, "test-clean")
	if status == 0 || !strings.Contains(output, "Refusing cleanup outside") {
		t.Fatalf("cleanup accepted an unrecognized checkout: %s", output)
	}
	if _, err := os.Stat(f.log); !os.IsNotExist(err) {
		t.Fatalf("image removal ran for an unrecognized checkout: %v", err)
	}
}
