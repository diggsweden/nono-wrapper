// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testNativeDevBaseDefaults(t *testing.T, root, project, wrapper string, env []string) {
	if runtime.GOOS != "linux" {
		t.Skip("compiler-helper and PTY validation currently covers Linux")
	}
	config, data, state := filepath.Join(root, "xdg config"), filepath.Join(root, "xdg data"), filepath.Join(root, "xdg state")
	for path, contents := range map[string]string{
		filepath.Join(root, "home/.docker/cli-plugins/docker-compose"):           "#!/bin/sh\nprintf 'COMPOSE_PLUGIN_WRAPPER_OK\\n'\n",
		filepath.Join(root, "home/.docker/config.json"):                          "{\"auths\":{}}\n",
		filepath.Join(root, "home/.ssh/fixture-private-key"):                     "dummy-private-key\n",
		filepath.Join(root, "home/.m2/settings-security.xml"):                    "dummy-master-password-material\n",
		filepath.Join(root, "home/.npmrc"):                                       "ignore-scripts=true\n",
		filepath.Join(config, "pip/pip.conf"):                                    "[global]\ndisable-pip-version-check = true\n",
		filepath.Join(config, "containers/registries.conf"):                      "unqualified-search-registries = ['docker.io']\n",
		filepath.Join(config, "containers/auth.json"):                            "{\"auths\":{\"fixture\":\"dummy-token\"}}\n",
		filepath.Join(state, "unrelated/private-state"):                          "dummy-unrelated-state\n",
		filepath.Join(data, "nanolinter/linter-defaults/rumdl.toml"):             "fixture-rumdl\n",
		filepath.Join(data, "nanolinter/linter-defaults/shellcheckrc"):           "fixture-shellcheck\n",
		filepath.Join(data, "nanolinter/linter-defaults/.yamlfmt"):               "fixture-yamlfmt\n",
		filepath.Join(data, "nanolinter/linter-defaults/lychee.toml"):            "fixture-lychee\n",
		filepath.Join(data, "nanolinter/private-state"):                          "dummy-private-state\n",
		filepath.Join(data, "unrelated/private-state"):                           "dummy-unrelated-state\n",
		filepath.Join(root, "home/.bundle/config"):                               "---\nBUNDLE_FROZEN: true\n",
		filepath.Join(root, "home/.gradle/gradle.properties"):                    "fixture=ordinary-config\n",
		filepath.Join(root, "home/.local/share/pipx/venvs/fixture/bin/probe"):    "#!/bin/sh\nprintf 'PIPX_RUNTIME_OK\\n'\n",
		filepath.Join(root, "custom-pipx/venvs/fixture/bin/probe"):               "#!/bin/sh\nprintf 'CUSTOM_PIPX_RUNTIME_OK\\n'\n",
		filepath.Join(root, "custom-cargo/config.toml"):                          "custom-cargo-config\n",
		filepath.Join(root, "custom-cargo/credentials.toml"):                     "dummy-custom-cargo-credential\n",
		filepath.Join(config, "ripgrep/config"):                                  "--hidden\n",
		filepath.Join(root, "home/.cargo/config.toml"):                           "cargo-config\n",
		filepath.Join(root, "home/.cargo/bin/marker"):                            "cargo-tool\n",
		filepath.Join(root, "home/.cargo/registry/marker"):                       "cargo-cache\n",
		filepath.Join(root, "home/.cargo/credentials.toml"):                      "dummy-cargo-credential\n",
		filepath.Join(root, "home/.cargo/credentials"):                           "dummy-legacy-cargo-credential\n",
		filepath.Join(root, "home/.local/share/uv/python/marker"):                "python-runtime\n",
		filepath.Join(root, "home/.local/share/uv/tools/marker"):                 "uv-tool\n",
		filepath.Join(root, "home/.local/share/uv/credentials/credentials.toml"): "dummy-uv-credential\n",
		filepath.Join(config, "devtools/linting/marker"):                         "lint-config\n",
		filepath.Join(config, "devtools/formatting/marker"):                      "format-config\n",
		filepath.Join(data, "devbase-check/check.sh"):                            "#!/bin/sh\n[ \"$DEVBASE_CHECK_SKIP_UPDATES\" = 1 ]\nprintf 'DEVBASE_HELPER_OK\\n'\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "devbase probes")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
			break
		}
	}
	probeEnv = append(probeEnv, "RIPGREP_CONFIG_PATH="+filepath.Join(config, "ripgrep/config"),
		"CARGO_HOME="+filepath.Join(root, "custom-cargo"), "PIPX_HOME="+filepath.Join(root, "custom-pipx"),
		"NODE_USE_SYSTEM_CA=1", "GOTOOLCHAIN=auto", "DEVBASE_CHECK_SKIP_UPDATES=0",
		"NPM_CONFIG_MIN_RELEASE_AGE=14", "PIPX_COOLDOWN=30")
	body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
[ "$GOTOOLCHAIN" = local ]
[ "$DEVBASE_CHECK_SKIP_UPDATES" = 1 ]
[ "$NPM_CONFIG_MIN_RELEASE_AGE" = 14 ]
[ "$PIPX_COOLDOWN" = 30 ]
[ "$NODE_USE_SYSTEM_CA" = 1 ]
[ "$(cat "$RIPGREP_CONFIG_PATH")" = --hidden ]
[ "$(cat "$XDG_CONFIG_HOME/devtools/linting/marker")" = lint-config ]
[ "$(cat "$XDG_CONFIG_HOME/devtools/formatting/marker")" = format-config ]
[ "$(cat "$HOME/.cargo/config.toml")" = cargo-config ]
[ "$(cat "$HOME/.cargo/bin/marker")" = cargo-tool ]
[ "$(cat "$HOME/.cargo/registry/marker")" = cargo-cache ]
[ "$(cat "$CARGO_HOME/config.toml")" = custom-cargo-config ]
[ "$(cat "$HOME/.npmrc")" = ignore-scripts=true ]
grep -q 'fixture=ordinary-config' "$HOME/.gradle/gradle.properties"
cat "$XDG_CONFIG_HOME/pip/pip.conf" "$HOME/.bundle/config" >/dev/null
grep -q docker.io "$XDG_CONFIG_HOME/containers/registries.conf"
for base in "$XDG_DATA_HOME" "$XDG_STATE_HOME"; do
  if cat "$base/unrelated/private-state" >/dev/null 2>&1; then exit 94; fi
done
if cat "$XDG_CONFIG_HOME/containers/auth.json" >/dev/null 2>&1; then exit 94; fi
for file in rumdl.toml shellcheckrc .yamlfmt lychee.toml; do
  cat "$XDG_DATA_HOME/nanolinter/linter-defaults/$file" >/dev/null
  if (printf changed > "$XDG_DATA_HOME/nanolinter/linter-defaults/$file") 2>/dev/null; then exit 93; fi
done
cat "$XDG_DATA_HOME/nanolinter/private-state" >/dev/null
if (printf changed > "$XDG_DATA_HOME/nanolinter/private-state") 2>/dev/null; then exit 93; fi
if cat "$XDG_DATA_HOME/unrelated/private-state" >/dev/null 2>&1; then exit 94; fi
"$HOME/.local/share/pipx/venvs/fixture/bin/probe"
"$PIPX_HOME/venvs/fixture/bin/probe"
"$HOME/.docker/cli-plugins/docker-compose"
[ "$(cat "$HOME/.local/share/uv/python/marker")" = python-runtime ]
[ "$(cat "$HOME/.local/share/uv/tools/marker")" = uv-tool ]
for path in "$HOME/.cargo/credentials.toml" "$HOME/.cargo/credentials" "$CARGO_HOME/credentials.toml" "$HOME/.local/share/uv/credentials/credentials.toml" "$HOME/.docker/config.json" "$HOME/.ssh/fixture-private-key" "$HOME/.m2/settings-security.xml"; do
  if cat "$path" >/dev/null 2>&1; then exit 94; fi
done
"$XDG_DATA_HOME/devbase-check/check.sh"
for path in "$RIPGREP_CONFIG_PATH" "$XDG_CONFIG_HOME/devtools/linting/marker" "$XDG_DATA_HOME/devbase-check/check.sh" "$PIPX_HOME/venvs/fixture/bin/probe" "$HOME/.npmrc" "$XDG_CONFIG_HOME/containers/registries.conf"; do
  if (printf changed > "$path") 2>/dev/null; then exit 93; fi
done
`
	if _, err := exec.LookPath("cc"); err == nil {
		body += `printf '#include <stdio.h>\nint main(void) { puts("C_COMPILER_OK"); return 0; }\n' > devbase-probe.c
cc devbase-probe.c -o devbase-c-probe
./devbase-c-probe
`
	}
	if _, err := exec.LookPath("script"); err == nil {
		body += "script -q -c 'printf PTY_DEVICE_OK' /dev/null\n"
	}
	// Give the isolated HOME a read-only view through the existing Rust group.
	// No rustup installation/update command is run and no host toolchain is changed.
	if rustup, err := exec.LookPath("rustup"); err == nil {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(ctx, rustup, "which", "rustc").Output(); err == nil {
			toolchain := strings.TrimSpace(string(output))
			rustupHome := os.Getenv("RUSTUP_HOME")
			if rustupHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				rustupHome = filepath.Join(home, ".rustup")
			}
			if err := os.Symlink(rustupHome, filepath.Join(root, "home/.rustup")); err != nil {
				t.Fatal(err)
			}
			body += "printf 'fn main() { println!(\"RUST_LINK_OK\"); }\\n' > devbase-probe.rs\n" +
				"'" + strings.ReplaceAll(toolchain, "'", "'\\''") + "' devbase-probe.rs -o devbase-rust-probe\n./devbase-rust-probe\n"
		}
	}
	body += "printf '\nDEVBASE_DEFAULTS_OK\n'\n"
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", agent)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, probeEnv, time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "DEVBASE_DEFAULTS_OK") {
				t.Fatalf("DevBase defaults through %s: %v\n%s", agent, err, output)
			}
			t.Logf("%s", output)
		})
	}
	t.Run("optional bypasses cannot expose private keys through symlinks", func(t *testing.T) {
		secret := filepath.Join(root, "home/.ssh/fixture-private-key")
		for _, relative := range []string{".npmrc", ".docker/cli-plugins/docker-compose"} {
			file := filepath.Join(root, "home", relative)
			original, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(secret, file); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Remove(file); err != nil {
					t.Error(err)
					return
				}
				if err := os.WriteFile(file, original, 0o700); err != nil {
					t.Error(err)
				}
			}()
		}
		probe := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
for path in "$HOME/.npmrc" "$HOME/.docker/cli-plugins/docker-compose" "$HOME/.ssh/fixture-private-key"; do
  if cat "$path" >/dev/null 2>&1; then exit 94; fi
done
printf 'REDIRECTED_BYPASS_DENIED\n'
`
		for _, agent := range []string{"opencode", "codex"} {
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(probe), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			// An additive read grant must not defeat the inherited private-key
			// deny via an unrelated automatic npmrc/plugin exception.
			cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", "--read-file", secret, agent)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, probeEnv, time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "REDIRECTED_BYPASS_DENIED") {
				t.Fatalf("redirected bypass through %s: %v\n%s", agent, err, output)
			}
		}
	})
}
