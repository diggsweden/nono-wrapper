// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathRuleIdentityAndProtection(t *testing.T) {
	home := t.TempDir()
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	directory := filepath.Join(home, "reference")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(directory, filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	rules, err := profilePathRules([]byte(`{"filesystem":{"read":["$HOME/reference","~/alias"]}}`))
	if err != nil || len(rules) != 1 || rules[0].path != directory || rules[0].access != pathRead {
		t.Fatalf("path aliases did not identify one resource: %+v (%v)", rules, err)
	}
	for _, input := range []string{
		`{"filesystem":{"read":["$HOME/reference"],"allow":["~/alias"]}}`,
		`{"filesystem":{"allow":["$XDG_CONFIG_HOME/nono/profiles"]}}`,
		`{"filesystem":{"write_file":["$NONO_CONFIG/config.toml"]}}`,
		`{"filesystem":{"read":["$HOME/reference/**"]}}`,
	} {
		if _, err := profilePathRules([]byte(input)); err == nil {
			t.Fatalf("accepted ambiguous, protected or nonliteral user path: %s", input)
		}
	}
}

func TestPathRulesUseFilesystemIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	file, alias := filepath.Join(home, "document"), filepath.Join(home, "alias")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(file, alias); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"filesystem": map[string]any{
		"read_file": []string{file}, "allow_file": []string{alias},
	}})
	if _, err := profilePathRules(data); err == nil {
		t.Fatal("accepted conflicting user modes for two names of the same file")
	}
	read := pathRule{file, pathRead, true}
	if err := checkPathOverrides([]pathRule{read}, []pathRule{read, {alias, pathRead | pathWrite, true}}); err == nil {
		t.Fatal("a writable file alias silently defeated the read-only user rule")
	}
}

func TestPathIdentityRespectsVolumeCaseSensitivity(t *testing.T) {
	root := t.TempDir()
	upper, lower := filepath.Join(root, "Reference"), filepath.Join(root, "reference")
	if err := os.Mkdir(upper, 0o700); err != nil {
		t.Fatal(err)
	}
	err := os.Mkdir(lower, 0o700)
	if os.IsExist(err) {
		if !samePolicyPath(upper, lower) || !pathWithin(filepath.Join(lower, "future-file"), upper) {
			t.Fatal("case-insensitive aliases were treated as distinct paths")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if samePolicyPath(upper, lower) || pathWithin(lower, upper) {
		t.Fatal("distinct paths on a case-sensitive volume were conflated")
	}
}

func TestEffectivePathModeConflicts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	path := filepath.Join(home, "data", "reference")
	for _, test := range []struct {
		name    string
		rule    pathRule
		extra   pathRule
		allowed bool
	}{
		{"same path write", pathRule{path, pathRead, false}, pathRule{path, pathRead | pathWrite, false}, false},
		{"parent write", pathRule{path, pathRead, false}, pathRule{filepath.Dir(path), pathWrite, false}, false},
		{"child directory write", pathRule{path, pathRead, false}, pathRule{filepath.Join(path, "child"), pathWrite, false}, false},
		{"child file write", pathRule{path, pathRead, false}, pathRule{filepath.Join(path, "file"), pathWrite, true}, false},
		{"directory grants file write", pathRule{path, pathRead, true}, pathRule{filepath.Dir(path), pathWrite, false}, false},
		{"different file", pathRule{path, pathRead, true}, pathRule{path + "-other", pathWrite, true}, true},
		{"path boundary", pathRule{path, pathRead, false}, pathRule{path + "-other", pathWrite, false}, true},
		{"parent read", pathRule{path, pathRead, false}, pathRule{filepath.Dir(path), pathRead, false}, true},
		{"readwrite preserves reads", pathRule{path, pathRead | pathWrite, false}, pathRule{path, pathRead, false}, true},
		{"write-only conflicts with read", pathRule{path, pathWrite, false}, pathRule{path, pathRead, false}, false},
		{"directory glob", pathRule{path, pathRead, false}, pathRule{filepath.Join(filepath.Dir(path), "*"), pathWrite, false}, false},
		{"unrelated protected write", pathRule{path, pathRead, false}, pathRule{filepath.Join(home, "config/nono/profiles"), pathWrite, false}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := checkPathOverrides([]pathRule{test.rule}, []pathRule{test.rule, test.extra})
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%t: %v", test.allowed, err)
			}
		})
	}
	if err := checkPathOverrides([]pathRule{{path, pathRead, false}}, nil); err == nil {
		t.Fatal("accepted a missing user allowance")
	}
}

func TestComposeFilesystemOverrides(t *testing.T) {
	for _, mode := range []string{"directory read-only", "file read-only", "directory readwrite", "XDG alias", "broader inherited write", "invalid manifest", "cancelled inspection"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			profiles := filepath.Join(home, "config/nono/profiles")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			path, rawPath := filepath.Join(home, "data/reference"), "$HOME/data/reference"
			if mode == "XDG alias" {
				path, rawPath = filepath.Join(home, "config/reference"), "$XDG_CONFIG_HOME/reference"
			}
			baseField, userField, access, kind := "allow", "read", "read", "directory"
			if mode == "file read-only" {
				baseField, userField, kind = "allow_file", "read_file", "file"
			} else if mode == "directory readwrite" {
				baseField, userField, access = "read", "allow", "readwrite"
			}
			baseObject := map[string]any{
				"extends":     "agent-policy",
				"environment": map[string]any{"allow_vars": []string{"KEEP"}},
				"network":     map[string]any{"allow_domain": []string{"retained.example"}},
				"filesystem":  map[string]any{baseField: []string{rawPath}},
			}
			base, _ := json.Marshal(baseObject)
			parent, _ := json.Marshal(map[string]any{
				"filesystem": map[string]any{"read": []string{"$HOME/unrelated"}},
				"platform_overrides": map[string]any{"linux": map[string]any{
					"filesystem": map[string]any{baseField: []any{map[string]any{"path": rawPath, "when": "linux"}}},
				}},
			})
			for name, data := range map[string][]byte{"opencode-chatgpt": base, "agent-policy": parent} {
				if err := os.WriteFile(filepath.Join(profiles, name+".jsonc"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			user, _ := json.Marshal(map[string]any{"filesystem": map[string]any{userField: []string{path}}})
			grants := []map[string]string{{"path": path, "access": access, "type": kind}}
			if mode == "broader inherited write" {
				grants = append(grants, map[string]string{"path": filepath.Dir(path), "access": "readwrite", "type": "directory"})
			}
			manifest, _ := json.Marshal(map[string]any{"version": "0.1.0", "filesystem": map[string]any{"grants": grants}})
			if mode == "invalid manifest" {
				manifest = []byte(`{"filesystem":{"grants":[]}}`)
			}
			t.Setenv("FS_RESOLVED", string(user))
			t.Setenv("FS_MANIFEST", string(manifest))
			t.Setenv("FS_TEST_MODE", mode)
			t.Setenv("NONO_ALLOW", "")
			nono := filepath.Join(home, "nono")
			if err := os.WriteFile(nono, []byte(`#!/bin/sh
if [ "$3" = --format ]; then
  [ "$FS_TEST_MODE" != 'cancelled inspection' ] || exec /bin/sleep 5
  printf '%s\n' "$FS_MANIFEST"
else
  printf '%s\n' "$FS_RESOLVED"
fi
`), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if mode == "cancelled inspection" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			composed, cleanup, err := composeUserOverrides(ctx, nono, "opencode-chatgpt", "opencode-user", "", base, user, false)
			fails := mode == "broader inherited write" || mode == "invalid manifest" || mode == "cancelled inspection"
			if fails {
				if err == nil {
					cleanup()
					t.Fatal("unsafe or unverified override was accepted")
				}
			} else {
				if err != nil || composed == "" {
					t.Fatalf("could not compose path override: %v", err)
				}
				t.Cleanup(cleanup)
				entries, err := os.ReadDir(filepath.Dir(composed))
				if err != nil {
					t.Fatal(err)
				}
				var copied strings.Builder
				for _, entry := range entries {
					data, err := os.ReadFile(filepath.Join(filepath.Dir(composed), entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					copied.Write(data)
				}
				if strings.Contains(copied.String(), rawPath) || !strings.Contains(copied.String(), "KEEP") || !strings.Contains(copied.String(), "$HOME/unrelated") {
					t.Fatalf("failed to replace only the matching bundled path: %s", copied.String())
				}
				cleanup()
			}
			entries, err := os.ReadDir(profiles)
			if err != nil || len(entries) != 2 {
				t.Fatalf("composition left generated files: %v (%v)", entries, err)
			}
			for name, original := range map[string][]byte{"opencode-chatgpt": base, "agent-policy": parent} {
				data, err := os.ReadFile(filepath.Join(profiles, name+".jsonc"))
				if err != nil || string(data) != string(original) {
					t.Fatalf("rewrote original profile %s: %v", name, err)
				}
			}
		})
	}
}

func TestManifestRejectsUnexpectedFilesystemShapes(t *testing.T) {
	for _, input := range []string{
		`null`, `{"version":"0.1.0"}`, `{"version":"future","filesystem":{"grants":[]}}`,
		`{"version":"0.1.0","filesystem":{"grants":[{"path":"relative","type":"directory","access":"read"}]}}`,
		`{"version":"0.1.0","filesystem":{"grants":[{"path":"/usr","type":"other","access":"read"}]}}`,
		`{"version":"0.1.0","filesystem":{"grants":[{"path":"/usr","type":"directory","access":"other"}]}}`,
	} {
		if _, err := manifestPathGrants([]byte(input)); err == nil {
			t.Fatalf("accepted unexpected manifest: %s", input)
		}
	}
}
