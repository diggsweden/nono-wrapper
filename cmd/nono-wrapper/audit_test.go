// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

func auditFixture(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAuditQuotesFilenames(t *testing.T) {
	root := t.TempDir()
	marker := auditFixture(t, t.TempDir(), "started")
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(marker, old, old); err != nil {
		t.Fatal(err)
	}
	name := ".vscode/\x1b[2J\x1b[Hforged\n  extra-line"
	auditFixture(t, root, name)
	var output bytes.Buffer
	err := audit(context.Background(), marker, root, &output)
	var exit cli.ExitCoder
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("expected changed-file status: %v", err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "\n  extra-line\n") ||
		!strings.Contains(output.String(), "  "+strconv.Quote(name)+"\n") {
		t.Fatalf("unsafe filename rendering: %q", output.String())
	}
}

func TestAuditQuotesPathErrors(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "missing\x1b[2J\nmarker")
	var output bytes.Buffer
	err := audit(context.Background(), marker, t.TempDir(), &output)
	var pathError *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pathError) || pathError.Path != marker {
		t.Fatalf("filesystem error identity lost: %v", err)
	}
	if strings.ContainsAny(err.Error(), "\x1b\n") || !strings.Contains(err.Error(), strconv.Quote(marker)) {
		t.Fatalf("unsafe path diagnostic: %q", err.Error())
	}
}

func TestAuditGitPointer(t *testing.T) {
	for _, mode := range []string{"file", "symlink", "hooks symlink"} {
		t.Run(mode, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			auditFixture(t, root, "package.json")
			auditFixture(t, outside, "config")
			auditFixture(t, outside, "hooks/pre-commit")
			name := ".git"
			if mode == "hooks symlink" {
				if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				name = ".git/hooks"
			}
			pointer := filepath.Join(root, name)
			if mode == "file" {
				if err := os.WriteFile(pointer, []byte("gitdir: "+outside+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(outside, pointer); err != nil {
				t.Fatal(err)
			}
			got, err := changedFiles(context.Background(), root, time.Now().Add(-time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, []string{name, "package.json"}) {
				t.Fatalf("expected the pointer and project changes, without traversing external Git state: %q", got)
			}
		})
	}
}

func TestAuditSelectsChangedFiles(t *testing.T) {
	root := t.TempDir()
	want := []string{
		".git/config", ".git/hooks/pre-commit", ".git/hooks/vendor/helper",
		".githooks/pre-push", ".husky/prepare", "lefthook.yml", ".lefthook.yml",
		".pre-commit-config.yaml", "opencode.json", "opencode.jsonc",
		".opencode/agent/review.md", ".codex/config.toml", ".vscode/tasks.json",
		".idea/project.xml", ".devcontainer/devcontainer.json",
		".github/workflows/ci.yml", ".forgejo/workflows/ci.yml", ".gitlab-ci.yml",
		".envrc", "nested/.envrc", "nix/flake.nix", "shell.nix", "default.nix",
		".mise.toml", "nested/mise.toml", ".mise.local.toml", "nested/mise.local.toml",
		"mise.lock", "nested/mise.local.lock", "web/package.json",
	}
	for _, name := range want {
		auditFixture(t, root, name)
	}
	for _, name := range []string{
		"notes.txt", "src/main.go", ".git/objects/package.json",
		"node_modules/package.json", "vendor/package.json", "target/package.json",
		"dist/package.json", "build/package.json", ".cache/package.json",
		".venv/package.json", "venv/package.json", "nested/node_modules/package.json",
	} {
		auditFixture(t, root, name)
	}
	// Restoring mtime must not hide a changed script/configuration file.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "web/package.json"), old, old); err != nil {
		t.Fatal(err)
	}
	// A selected symlink is reported, but its target is never traversed.
	outside := t.TempDir()
	auditFixture(t, outside, "package.json")
	if err := os.Symlink(outside, filepath.Join(root, ".vscode/linked")); err != nil {
		t.Fatal(err)
	}
	want = append(want, ".vscode/linked")
	got, err := changedFiles(context.Background(), root, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("reported files:\n got %q\nwant %q", got, want)
	}
	unchanged, err := changedFiles(context.Background(), root, time.Now().Add(time.Hour))
	if err != nil || len(unchanged) != 0 {
		t.Fatalf("files older than the cutoff were reported: %q (%v)", unchanged, err)
	}
}

func TestAuditStatusAndMarker(t *testing.T) {
	root := t.TempDir()
	marker := auditFixture(t, t.TempDir(), "started")
	auditFixture(t, root, "package.json")
	for _, test := range []struct {
		name    string
		cutoff  time.Time
		changed bool
	}{
		{"changed", time.Now().Add(-time.Hour), true},
		{"unchanged", time.Now().Add(time.Hour), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Chtimes(marker, test.cutoff, test.cutoff); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := audit(context.Background(), marker, root, &output)
			if test.changed {
				var exit cli.ExitCoder
				if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(output.String(), "  \"package.json\"\n") {
					t.Fatalf("expected a changed-file report and exit 1: %v (%s)", err, &output)
				}
			} else if err != nil || output.Len() != 0 {
				t.Fatalf("expected silent success: %v (%s)", err, &output)
			}
		})
	}
	for _, test := range []struct{ name, marker, root string }{
		{"missing marker", filepath.Join(root, "missing"), root},
		{"directory marker", root, root},
		{"missing root", marker, filepath.Join(root, "missing")},
		{"file root", marker, marker},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := audit(context.Background(), test.marker, test.root, &output); err == nil || output.Len() != 0 {
				t.Fatalf("invalid input accepted: %v (%s)", err, &output)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := changedFiles(ctx, root, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("audit ignored cancellation: %v", err)
	}
}
