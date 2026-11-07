// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A local Git dependency exercises Cargo's real cache locks, checkout and
// compiler without a registry account or external download.
func testNativeCargo(t *testing.T, root, project, wrapper string, env []string) {
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("requires an installed Rust toolchain")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, "rustc", "--print", "sysroot").Output()
	if err != nil {
		t.Fatal(err)
	}
	toolchain := strings.TrimSpace(string(output))
	work, dependency, bin := filepath.Join(project, "cargo-fixture"), filepath.Join(project, "cargo-dependency"), filepath.Join(root, "cargo probes")
	for _, path := range []string{work, dependency, bin} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(dependency, "Cargo.toml"): "[package]\nname = 'fixture-dependency'\nversion = '0.1.0'\nedition = '2021'\n[lib]\npath = 'lib.rs'\n",
		filepath.Join(dependency, "lib.rs"):     "pub fn value() -> u8 { 42 }\n",
		filepath.Join(work, "Cargo.toml"):       fmt.Sprintf("[package]\nname = 'fixture'\nversion = '0.1.0'\nedition = '2021'\n[lib]\npath = 'lib.rs'\n[dependencies]\nfixture-dependency = { git = 'file://%s' }\n", dependency),
		filepath.Join(work, "lib.rs"):           "#[test] fn dependency_works() { assert_eq!(fixture_dependency::value(), 42); }\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture"}} {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = dependency, env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare local dependency: %v\n%s", err, output)
		}
	}
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			cargoHome := filepath.Join(root, "cargo cache "+agent)
			if err := os.Mkdir(cargoHome, 0o700); err != nil {
				t.Fatal(err)
			}
			credential := filepath.Join(cargoHome, "credentials.toml")
			if err := os.WriteFile(credential, []byte("PRIVATE_FIXTURE\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
while [ "${1:-}" != --fixture ]; do shift; done
shift
export RUSTC="$1/bin/rustc" RUSTDOC="$1/bin/rustdoc"
if cat "$CARGO_HOME/credentials.toml" 2>/dev/null; then exit 91; fi
"$1/bin/cargo" test
"$1/bin/cargo" test --offline --locked
printf 'CARGO_DEPENDENCY_OK\n'
`
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			probeEnv := append([]string{}, env...)
			for i := len(env) - 1; i >= 0; i-- {
				if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
					probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
					break
				}
			}
			probeEnv = append(probeEnv, "CARGO_HOME="+cargoHome)
			cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", "--read", toolchain, "--read", dependency, agent, "--fixture", toolchain)
			cmd.Dir, cmd.Env, cmd.WaitDelay = work, probeEnv, time.Second
			if output, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(output), "CARGO_DEPENDENCY_OK") {
				t.Fatalf("Cargo dependency build: %v\n%s", err, output)
			}
		})
	}
}
