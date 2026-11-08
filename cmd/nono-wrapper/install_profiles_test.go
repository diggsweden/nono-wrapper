// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/diggsweden/nono-wrapper/profiles"
)

func TestChangedProfilesNamesMissingAndEditedCopies(t *testing.T) {
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	directory := filepath.Join(config, "nono", "profiles")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	current, err := profiles.Files.ReadFile("codex-base.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"codex-base.jsonc": current, "codex-chatgpt.jsonc": []byte("{}\n")} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changed := changedProfiles()
	if slices.Contains(changed, "codex-base.jsonc") || !slices.Contains(changed, "codex-chatgpt.jsonc") || !slices.Contains(changed, "opencode-offline.jsonc") {
		t.Fatalf("changed=%q", changed)
	}
}

func TestBundledPacksReadsTheManifest(t *testing.T) {
	packs, err := bundledPacks()
	if err != nil || len(packs) == 0 {
		t.Fatalf("packs=%q err=%v", packs, err)
	}
}
