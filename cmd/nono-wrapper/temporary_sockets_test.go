// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateSocketDirectoryPreparation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	show := func(path string) []byte {
		data, err := json.Marshal(map[string]any{"filesystem": map[string]any{"unix_socket_subtree_bind": []string{path}}})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	path := filepath.Join(root, "private/tmp")
	for range 2 {
		if err := preparePrivateSocketDirectories(show(path), nil); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("private directory not prepared: %v (%v)", info, err)
	}
	if err := preparePrivateSocketDirectories(show(path), []pathRule{{root, pathRead, false}}); err == nil {
		t.Fatal("protected root was accepted")
	}
	if err := os.Chmod(path, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateSocketDirectories(show(path), nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSticky == 0 {
		t.Fatalf("owned root is not private or lost special mode bits: %v (%v)", info, err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateSocketDirectories(show(alias), nil); err == nil {
		t.Fatal("socket root symlink was accepted")
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preparePrivateSocketDirectories(show(file), nil); err == nil {
		t.Fatal("regular file accepted as a socket directory")
	}
}

func TestReadOnlyOverrideRemovesSocketBindDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	data, err := json.Marshal(map[string][]string{"unix_socket_subtree_bind": {root}})
	if err != nil {
		t.Fatal(err)
	}
	profile := map[string]json.RawMessage{"filesystem": data}
	if err := stripPathRules(profile, []pathRule{{root, pathRead, false}}); err != nil {
		t.Fatal(err)
	}
	var filesystem map[string][]string
	if err := json.Unmarshal(profile["filesystem"], &filesystem); err != nil {
		t.Fatal(err)
	}
	if len(filesystem["unix_socket_subtree_bind"]) != 0 {
		t.Fatal("socket capability defeated read-only override")
	}
}
