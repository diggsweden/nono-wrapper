// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// People copy install commands from the docs, so those must follow the pins.
func TestInstallDocsSelectThePinnedNono(t *testing.T) {
	root := testRepository(t)
	pins, err := os.ReadFile(filepath.Join(root, ".mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	pinned := regexp.MustCompile(`(?m)^nono = "([^"]+)"$`).FindSubmatch(pins)
	if pinned == nil {
		t.Fatal(".mise.toml does not pin nono")
	}
	for _, doc := range []string{"README.md", "docs/install.md"} {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		uses := regexp.MustCompile(`mise use -g nono@([0-9][0-9A-Za-z.+-]*[0-9A-Za-z])`).FindAllSubmatch(data, -1)
		if len(uses) == 0 {
			t.Fatalf("%s no longer shows 'mise use -g nono@VERSION'", doc)
		}
		for _, use := range uses {
			if string(use[1]) != string(pinned[1]) {
				t.Errorf("%s selects nono %s; .mise.toml pins %s", doc, use[1], pinned[1])
			}
		}
	}
}
