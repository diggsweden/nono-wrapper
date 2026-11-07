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

func testNativeToolCaches(t *testing.T, root, project, wrapper string, env []string) {
	// A broad grant to the checkout cache or fixture root must not hide missing
	// runtime grants. This file is outside every declared application subtree.
	scratchPrivate := filepath.Join(root, "unrelated-private-state")
	if err := os.WriteFile(scratchPrivate, []byte("dummy-private-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	opengrep := "-"
	if program, err := exec.LookPath("opengrep"); err == nil {
		program, err = filepath.EvalSymlinks(program)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(program) != "mise" {
			opengrep = program
		} else {
			t.Log("OpenGrep resolves to a Mise shim; cache permissions are tested without executing host configuration")
		}
	}
	for _, mode := range []string{"home", "xdg", "overrides"} {
		t.Run(mode, func(t *testing.T) {
			bin := filepath.Join(root, "tool cache probes "+mode)
			cache := filepath.Join(root, "home/.cache")
			if mode != "home" {
				cache = filepath.Join(root, "tool caches "+mode)
			}
			for _, dir := range []string{bin, cache} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			private := filepath.Join(cache, "unrelated-private-cache")
			if err := os.WriteFile(private, []byte("dummy-private-cache"), 0o600); err != nil {
				t.Fatal(err)
			}
			probeEnv := append([]string{}, env...)
			for i := len(env) - 1; i >= 0; i-- {
				if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
					probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
					break
				}
			}
			probeEnv = append(probeEnv, "XDG_CACHE_HOME="+cache)
			if mode == "home" {
				probeEnv = append(probeEnv, "XDG_DATA_HOME="+filepath.Join(root, "home/.local/share"))
			}
			for _, data := range []string{filepath.Join(root, "home/.local/share"), filepath.Join(root, "xdg data")} {
				completion := filepath.Join(data, "bash-completion/completions/private-fixture")
				if err := os.MkdirAll(filepath.Dir(completion), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(completion, []byte("unchanged\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(root, "xdg config")
			for _, base := range []string{filepath.Join(root, "home"), config} {
				directory := filepath.Join(base, ".opengrep")
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "settings.yml"), []byte("dummy-credential-store\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			expected := make([]string, 0, 5)
			for _, spec := range []struct{ variable, directory string }{
				{"MISE_CACHE_DIR", "mise"}, {"GOLANGCI_LINT_CACHE", "golangci-lint"},
				{"STATICCHECK_CACHE", "staticcheck"}, {"TRIVY_CACHE_DIR", "trivy"},
			} {
				path, override := filepath.Join(cache, spec.directory), ""
				if mode == "overrides" {
					path = filepath.Join(root, "custom "+spec.directory)
					override = path
				}
				probeEnv = append(probeEnv, spec.variable+"="+override)
				expected = append(expected, path)
			}
			expected = append(expected, filepath.Join(cache, "opengrep"))
			work := filepath.Join(project, "opengrep fixture "+mode)
			if err := os.MkdirAll(work, 0o700); err != nil {
				t.Fatal(err)
			}
			for file, data := range map[string]string{
				"rules.yml": "rules:\n  - id: fixture\n    languages: [python]\n    message: fixture\n    severity: INFO\n    pattern: print(...)\n",
				"sample.py": "print('fixture')\n",
			} {
				if err := os.WriteFile(filepath.Join(work, file), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
while [ "${1:-}" != --fixture ]; do shift; done
shift
private=$1
shift
scratch_private=$1
shift
for actual in "$MISE_CACHE_DIR" "$GOLANGCI_LINT_CACHE" "$STATICCHECK_CACHE" "$TRIVY_CACHE_DIR" "$XDG_CACHE_HOME/opengrep"; do
  [ "$actual" = "$1" ]
  shift
  mkdir -p "$actual/nono-cache-probe"
  printf fixture > "$actual/nono-cache-probe/entry"
  [ "$(cat "$actual/nono-cache-probe/entry")" = fixture ]
  rm "$actual/nono-cache-probe/entry"
  rmdir "$actual/nono-cache-probe"
done
if cat "$private" >/dev/null 2>&1; then exit 91; fi
if (printf changed > "$private") 2>/dev/null; then exit 92; fi
if cat "$scratch_private" >/dev/null 2>&1; then exit 97; fi
if (printf changed > "$scratch_private") 2>/dev/null; then exit 98; fi
base=$XDG_DATA_HOME
mkdir -p "$base/man/man1"
printf 'fixture manual\n' > "$base/man/man1/private-fixture.1"
cat "$base/man/man1/private-fixture.1" >/dev/null
rm "$base/man/man1/private-fixture.1"
if (printf changed > "$base/bash-completion/completions/private-fixture") 2>/dev/null; then exit 95; fi
if [ -e /etc/dpkg/origins/default ]; then cat /etc/dpkg/origins/default >/dev/null; fi
if [ -e /etc/dpkg/dpkg.cfg ]; then cat /etc/dpkg/dpkg.cfg >/dev/null; fi
if [ -d /etc/dpkg/dpkg.cfg.d ]; then ls /etc/dpkg/dpkg.cfg.d >/dev/null; fi
mkdir -p "$GRADLE_USER_HOME/wrapper/dists/nono-cache-probe"
printf fixture > "$GRADLE_USER_HOME/wrapper/dists/nono-cache-probe/fixture.zip.lck"
rm "$GRADLE_USER_HOME/wrapper/dists/nono-cache-probe/fixture.zip.lck"
rmdir "$GRADLE_USER_HOME/wrapper/dists/nono-cache-probe"
if (printf changed > "$GRADLE_USER_HOME/outside-scoped-state") 2>/dev/null; then exit 96; fi
for directory in "$HOME/.opengrep" "$XDG_CONFIG_HOME/.opengrep"; do
  printf 'fixture log entry\n' >> "$directory/semgrep.log"
  if cat "$directory/settings.yml" >/dev/null 2>&1; then exit 93; fi
  if (printf changed > "$directory/settings.yml") 2>/dev/null; then exit 94; fi
done
if [ "$1" != - ]; then
  # Account settings stay project-local; exercise the actual default log path.
  export SEMGREP_SETTINGS_FILE="$PWD/scanner-settings.yml"
  "$1" scan --config rules.yml --disable-version-check sample.py
  printf 'OPENGREP_LOCAL_SCAN_OK\n'
fi
printf 'TOOL_CACHES_SCOPED_OK\n'
`
			for _, agent := range []string{"opencode", "codex"} {
				if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				args := []string{"run", "--block-net"}
				if opengrep != "-" {
					args = append(args, "--read", filepath.Dir(opengrep))
				}
				args = append(args, agent, "--fixture", private, scratchPrivate)
				args = append(args, expected...)
				args = append(args, opengrep)
				cmd := exec.CommandContext(ctx, wrapper, args...)
				cmd.Dir, cmd.Env, cmd.WaitDelay = work, probeEnv, time.Second
				output, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(output), "TOOL_CACHES_SCOPED_OK") || opengrep != "-" && !strings.Contains(string(output), "OPENGREP_LOCAL_SCAN_OK") {
					t.Fatalf("tool caches through %s: %v\n%s", agent, err, output)
				}
			}
		})
	}
}
