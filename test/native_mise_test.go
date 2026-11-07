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

func testNativeMiseTrust(t *testing.T, root, project, wrapper string, env []string) {
	mise, err := exec.LookPath("mise")
	if err != nil {
		t.Skip("mise is not installed")
	}
	mise, err = filepath.EvalSymlinks(mise)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"home", "xdg", "override"} {
		t.Run(mode, func(t *testing.T) {
			work := filepath.Join(project, "mise trust "+mode)
			bin := filepath.Join(root, "mise probes "+mode)
			state := filepath.Join(root, "home/.local/state/mise")
			xdg := filepath.Join(root, "home/.local/state")
			if mode != "home" {
				xdg = filepath.Join(root, "xdg state")
				state = filepath.Join(xdg, "mise")
			}
			override := ""
			if mode == "override" {
				state = filepath.Join(root, "custom mise state")
				override = state
			}
			for _, dir := range []string{work, bin, state} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			config := filepath.Join(work, ".mise.toml")
			if err := os.WriteFile(config, []byte("[env]\nNONO_MISE_TRUST_FIXTURE = 'trusted'\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			probeEnv := append([]string{}, env...)
			for i := len(env) - 1; i >= 0; i-- {
				if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
					probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
					break
				}
			}
			probeEnv = append(probeEnv, "XDG_STATE_HOME="+xdg, "MISE_STATE_DIR="+override,
				"MISE_TRUSTED_CONFIG_PATHS=", "MISE_PARANOID=1", "MISE_AUTO_INSTALL=false",
				"MISE_GLOBAL_CONFIG_FILE="+filepath.Join(root, "mise config "+mode, "absent-global.toml"),
				"MISE_CONFIG_DIR="+filepath.Join(root, "mise config "+mode), "MISE_CEILING_PATHS="+root)
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			// Trust only this disposable fixture, never the checkout or host config.
			trust := exec.CommandContext(ctx, mise, "trust", config)
			trust.Dir, trust.Env, trust.WaitDelay = work, probeEnv, time.Second
			if output, err := trust.CombinedOutput(); err != nil {
				t.Fatalf("trust fixture: %v\n%s", err, output)
			}
			hashes, err := filepath.Glob(filepath.Join(state, "trusted-configs", "*.hash"))
			if err != nil || len(hashes) == 0 {
				t.Fatalf("missing trust hashes: %v %v", hashes, err)
			}
			if err := os.WriteFile(filepath.Join(state, "unrelated-private-state"), []byte("private"), 0o600); err != nil {
				t.Fatal(err)
			}
			body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
while [ "${1:-}" != --fixture ]; do shift; done
shift
mise_program=$1
state=$2
hash=$3
export MISE_PARANOID=1 MISE_TRUSTED_CONFIG_PATHS= MISE_AUTO_INSTALL=false
export MISE_GLOBAL_CONFIG_FILE="$PWD/absent-global.toml" MISE_CONFIG_DIR="$PWD/absent-config"
export MISE_CEILING_PATHS="$(dirname "$PWD")"
"$mise_program" exec -- /bin/sh -c '[ "$NONO_MISE_TRUST_FIXTURE" = trusted ]'
cat "$hash" >/dev/null
if (printf changed > "$hash") 2>/dev/null; then exit 91; fi
if (printf changed > "$state/trusted-configs/new-trust-record") 2>/dev/null; then exit 92; fi
if cat "$state/unrelated-private-state" >/dev/null 2>&1; then exit 93; fi
printf 'MISE_TRUST_READ_ONLY_OK\n'
`
			for _, agent := range []string{"opencode", "codex"} {
				if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
				cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", "--read-file", mise, agent, "--fixture", mise, state, hashes[0])
				cmd.Dir, cmd.Env, cmd.WaitDelay = work, probeEnv, time.Second
				output, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(output), "MISE_TRUST_READ_ONLY_OK") {
					t.Fatalf("Mise through %s: %v\n%s", agent, err, output)
				}
			}
		})
	}
}
