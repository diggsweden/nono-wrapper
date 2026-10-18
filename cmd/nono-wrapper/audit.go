// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

func audit(ctx context.Context, marker, directory string, output io.Writer) error {
	info, err := os.Stat(marker)
	if err != nil {
		return fmt.Errorf("audit marker: %w", quotePathError(err))
	}
	if !info.Mode().IsRegular() {
		return cli.Exit(fmt.Sprintf("audit marker must be a regular file: %q", marker), exitNoInput)
	}
	changed, err := changedFiles(ctx, directory, info.ModTime())
	if err != nil {
		return quotePathError(err)
	}
	if len(changed) == 0 {
		return nil
	}
	fmt.Fprintln(output, "Changed files that this computer may run by itself. Read them before you")
	fmt.Fprintln(output, "commit, enter the directory with direnv or mise, or open it in an editor:")
	for _, path := range changed {
		fmt.Fprintf(output, "  %q\n", path)
	}
	return cli.Exit("", exitFinding)
}

// Keep filesystem error identity while rendering untrusted paths safely.
type quotedPathError struct{ *fs.PathError }

func (err quotedPathError) Error() string {
	return fmt.Sprintf("%s %q: %v", err.Op, err.Path, err.Err)
}

func (err quotedPathError) Unwrap() error { return err.PathError }

func quotePathError(err error) error {
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		return quotedPathError{pathError}
	}
	return err
}

func changedFiles(ctx context.Context, directory string, since time.Time) ([]string, error) {
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("audit directory is not a directory: %q", directory)
	}
	var changed []string
	prune := false
	visit := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if prune && path != root {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", "target", "dist", "build", ".cache", ".venv", "venv":
					return filepath.SkipDir
				}
			}
			return nil
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if !auditPath(name) {
			return nil
		}
		info, err := entry.Info() // lstat: inspect links, never traverse their targets.
		if err != nil {
			return err
		}
		if (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) && changeTime(info).After(since) {
			changed = append(changed, name)
		}
		return nil
	}
	// Inspect local Git metadata only. Worktree files and symlinks are reported
	// by the project walk below; their targets may lie outside the audited tree.
	if git, err := os.Lstat(filepath.Join(root, ".git")); err == nil && git.IsDir() {
		for _, name := range []string{".git/config", ".git/hooks"} {
			path := filepath.Join(root, name)
			if _, err := os.Lstat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			if err := filepath.WalkDir(path, visit); err != nil {
				return nil, err
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	prune = true
	if err := filepath.WalkDir(root, visit); err != nil {
		return nil, err
	}
	return changed, nil
}

func auditPath(path string) bool {
	switch path {
	case ".git", ".git/config", ".git/hooks", "lefthook.yml", ".lefthook.yml", ".pre-commit-config.yaml", "opencode.json", "opencode.jsonc", ".gitlab-ci.yml":
		return true
	}
	for _, prefix := range []string{".git/hooks/", ".githooks/", ".husky/", ".opencode/", ".codex/", ".vscode/", ".idea/", ".devcontainer/", ".github/workflows/", ".forgejo/workflows/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	switch filepath.Base(path) {
	case ".envrc", "flake.nix", "shell.nix", "default.nix", "package.json",
		".mise.toml", "mise.toml", ".mise.local.toml", "mise.local.toml", "mise.lock", "mise.local.lock":
		return true
	}
	return false
}
