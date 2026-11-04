// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestReproducibleGenerationRefusesPrivateToolDownloads(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile(filepath.Join(testRepository(t), "test", "verify-reproducible.sh"))
	if err != nil {
		t.Fatal(err)
	}
	exports := regexp.MustCompile(`(?m)^export [^\n]+`).FindAllString(string(source), -1)
	if len(exports) == 0 {
		t.Fatal("reproducibility script must declare its offline environment")
	}
	goRoot := ""
	for _, entry := range buildEnv {
		if value, ok := strings.CutPrefix(entry, "GOROOT="); ok {
			goRoot = value
		}
	}
	if goRoot == "" {
		t.Fatal("test setup did not resolve the Go toolchain")
	}
	for _, private := range []string{"implicit", "explicit"} {
		t.Run(private, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for name, contents := range map[string]string{
				"go.mod": "module example.invalid/offline-tool-review\n\ngo 1.24.0\n\nrequire example.invalid/review.git v1.0.0\n\ntool example.invalid/review.git/cmd/probe\n",
				"go.sum": "example.invalid/review.git v1.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
					"example.invalid/review.git v1.0.0/go.mod h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n",
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/bash", "-euo", "pipefail", "-c",
				strings.Join(exports, "\n")+"\nexec \"$OFFLINE_GO\" tool probe")
			command.Dir = root
			// .git avoids HTTP discovery; GOVCS blocks VCS before any network access.
			command.Env = []string{
				"PATH=" + os.Getenv("PATH"), "HOME=" + root, "OFFLINE_GO=" + filepath.Join(goRoot, "bin", "go"),
				"GOMODCACHE=" + filepath.Join(root, "modules"), "GOCACHE=" + filepath.Join(root, "build-cache"),
				"GOPRIVATE=example.invalid", "GOVCS=*:off",
			}
			if private == "explicit" {
				command.Env = append(command.Env, "GONOPROXY=*")
			}
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "module lookup disabled by GOPROXY=off") {
				t.Fatalf("missing tool must fail at the offline resolver, not use VCS: %v\n%s", err, output)
			}
		})
	}
}

// The helper compares inert fixtures; no wrapper or agent is executed.
func TestReproducibleFiles(t *testing.T) {
	script, err := filepath.Abs("verify-reproducible.sh")
	if err != nil {
		t.Fatal(err)
	}
	files := []string{".cache/bin/nono-wrapper", ".cache/man/nono-wrapper.1"}
	for _, profile := range []string{"agent-policy", "developer-network", "codex-base", "codex-chatgpt", "codex-offline", "opencode-base", "opencode-chatgpt", "opencode-offline"} {
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
