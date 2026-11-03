// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tailscale/hujson"
)

func TestFileEditUsesSelectedTargetWhenLinkChanges(t *testing.T) {
	for _, field := range []string{"read_file", "allow_file"} {
		t.Run(field, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			first, second := filepath.Join(home, "project-a.md"), filepath.Join(home, "project-b.md")
			alias := filepath.Join(home, "AGENTS.md")
			for _, path := range []string{first, second} {
				if err := os.WriteFile(path, []byte("Generic instructions.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(first, alias); err != nil {
				t.Fatal(err)
			}
			rule, err := parseAllowance("file", alias, "*", true)
			if err != nil {
				t.Fatal(err)
			}
			// A valid, manually authored profile can use HOME expressions.
			data, err := json.Marshal(map[string]any{
				"filesystem":  map[string]any{field: []string{"$HOME/AGENTS.md"}},
				"environment": map[string]any{"allow_vars": []string{"EDITOR"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			doc, err := hujson.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(second, alias); err != nil {
				t.Fatal(err)
			}
			changed, err := changeAllowance(&doc, rule, true)
			if err != nil || !changed {
				t.Fatalf("update allowance: changed=%v err=%v", changed, err)
			}
			doc.Standardize()
			var saved map[string]map[string][]string
			if err := json.Unmarshal(doc.Pack(), &saved); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(saved["filesystem"]["read_file"], []string{first}) || len(saved["filesystem"]["allow_file"]) != 0 {
				t.Fatalf("grant did not stay on the selected read-only target: %#v", saved["filesystem"])
			}
			if !slices.Equal(saved["environment"]["allow_vars"], []string{"EDITOR"}) {
				t.Fatalf("editing a file changed unrelated settings: %#v", saved)
			}
		})
	}
}
