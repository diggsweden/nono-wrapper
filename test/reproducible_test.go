// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The helper compares inert fixtures; no wrapper or agent is executed.
func TestReproducibleFiles(t *testing.T) {
	script, err := filepath.Abs("verify-reproducible.sh")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{".cache/bin/nono-wrapper", ".cache/man/nono-wrapper.1"}
	for _, profile := range []string{"agent-policy", "codex-base", "codex-chatgpt", "codex-offline", "opencode-base", "opencode-chatgpt", "opencode-offline"} {
		files = append(files, "profiles/"+profile+".jsonc")
	}
	for _, scenario := range []string{"equal", "binary-diff", "profile-diff", "missing-both", "empty-manpage", "extra-profile"} {
		t.Run(scenario, func(t *testing.T) {
			first, second := t.TempDir(), t.TempDir()
			for _, dir := range []string{first, second} {
				for i, file := range files {
					if scenario == "missing-both" && i == 2 {
						continue
					}
					content := "payload"
					if (scenario == "binary-diff" && i == 0 || scenario == "profile-diff" && i == 2) && dir == second {
						content = "changed"
					}
					if scenario == "empty-manpage" && i == 1 {
						content = ""
					}
					path := filepath.Join(dir, filepath.FromSlash(file))
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "extra-profile" {
					if err := os.WriteFile(filepath.Join(dir, "profiles", "unexpected.jsonc"), []byte("{}"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			output, err := exec.CommandContext(t.Context(), "bash", script, "--compare", first, second).CombinedOutput()
			if (err == nil) != (scenario == "equal") {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}
