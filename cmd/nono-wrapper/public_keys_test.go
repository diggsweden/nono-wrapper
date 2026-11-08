// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfiguredPublicKeysExcludePrivateAndRedirectedFiles(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	directory := filepath.Join(home, ".ssh")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	public, private := testHostKey(t)
	for name, data := range map[string][]byte{
		"first.pub":            []byte(public + " fixture-comment\n"),
		"second.pub":           []byte(public + "\r\n"),
		"private":              private,
		"renamed-private.pub":  private,
		"trailing-private.pub": append([]byte(public+"\n"), private...),
		"leading-private.pub":  append(slices.Clone(private), []byte(public+"\n")...),
		"options.pub":          []byte("command=\"unexpected\" " + public + "\n"),
		"malformed.pub":        []byte("not a public key\n"),
		"literal[glob].pub":    []byte(public + "\n"),
		"literal-$HOME.pub":    []byte(public + "\n"),
		"oversized.pub":        []byte(public + " " + strings.Repeat("x", 1<<20)),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "directory.pub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"linked-private.pub": "private", "linked-public.pub": "first.pub", "dangling.pub": "missing"} {
		if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	defaults := runtimeDefaults{PublicKeyFiles: []runtimePath{{Path: "$HOME/.ssh/*.pub"}}}
	paths, err := configuredPublicKeyPaths(defaults)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(directory, "first.pub"), filepath.Join(directory, "second.pub")}
	if !slices.Equal(paths, want) {
		t.Fatalf("unexpected public-key exceptions: %q", paths)
	}
	alias := filepath.Join(home, "ssh-alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	defaults.PublicKeyFiles[0].Path = filepath.Join(alias, "*.pub")
	paths, err = configuredPublicKeyPaths(defaults)
	if err != nil || len(paths) != 0 {
		t.Fatalf("redirected parent accepted: %q (%v)", paths, err)
	}
}
