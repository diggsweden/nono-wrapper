// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfigReposFindsNamedRepositories(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		"sdg-config/.git", "group/team_config/.git", "Config/.git",
		"config-not-a-repo", "app/.git", "app/config/.git", ".config/.git", "a/b/deep-config/.git",
	} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got, err := configRepos(root, configRepoDepth)
	if err != nil {
		t.Fatal(err)
	}
	// Projects are searched as well: app/config is a nested repository.
	want := []string{filepath.Join(root, "Config"), filepath.Join(root, "app", "config"), filepath.Join(root, "group", "team_config"), filepath.Join(root, "sdg-config")}
	if !slices.Equal(got, want) {
		t.Fatalf("config repositories:\n got: %q\nwant: %q", got, want)
	}
}

func TestConfigReposSkipsUnreadableDirectories(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	root := t.TempDir()
	for _, path := range []string{"volume/data", "team-config/.git"} {
		if err := os.MkdirAll(filepath.Join(root, path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "volume"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "volume"), 0o700) })
	got, err := configRepos(root, configRepoDepth)
	if err != nil || !slices.Equal(got, []string{filepath.Join(root, "team-config")}) {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestConfirmConfigRepos(t *testing.T) {
	repos := []string{"/projects/sdg/sdg-config"}
	for _, test := range []struct {
		answer      string
		interactive bool
		ok          bool
	}{
		{"y\n", true, true},
		{"YES\n", true, true},
		{"n\n", true, false},
		{"\n", true, false},
		{"", true, false},
		{"y\n", false, false},
	} {
		var prompt strings.Builder
		scope := configScope{subject: "the start directory", access: "read and write", alternative: "Start elsewhere"}
		err := confirmConfigRepos(strings.NewReader(test.answer), &prompt, test.interactive, scope, repos)
		if (err == nil) != test.ok {
			t.Errorf("answer %q, interactive %v: err=%v", test.answer, test.interactive, err)
		}
		if test.interactive != strings.Contains(prompt.String(), "Include them? [y/N]") {
			t.Errorf("answer %q, interactive %v: prompt %q", test.answer, test.interactive, prompt.String())
		}
		if err != nil && (!strings.Contains(err.Error(), "NONO_WRAPPER_ALLOW_CONFIG=1") || !strings.Contains(err.Error(), "landlock-lsm/linux/issues/28")) {
			t.Errorf("refusal does not name the override: %v", err)
		}
	}
}
