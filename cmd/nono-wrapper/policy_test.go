// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeHostOverridesPreservesPolicy(t *testing.T) {
	for _, mode := range []string{"whole host", "restricted host", "conflicting external rule", "invalid resolution"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			profiles := filepath.Join(home, ".config/nono/profiles")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			sources := map[string]string{
				"opencode-chatgpt":  `{"extends":["opencode-base","developer-network"],"environment":{"allow_vars":["KEEP"]},"network":{"tls_intercept":{"ca_env_vars":["CUSTOM_CA"]},"allow_domain":["retained.example"]}}`,
				"developer-network": `{"network":{"allow_domain":["api.github.com","developer-kept.example"]}}`,
				"opencode-base":     `{"extends":["nolabs-ai/opencode","agent-policy"],"linux":{"af_unix_mediation":"pathname"}}`,
				"agent-policy":      `{"filesystem":{"read":["$HOME/reference"]},"unsafe_macos_seatbelt_rules":["(deny appleevent-send)","(deny mach-lookup (global-name \"com.apple.coreservices.appleevents\"))"],"platform_overrides":{"linux":{"network":{"allow_domain":["API.GITHUB.COM.","platform.example"]}}}}`,
			}
			for name, data := range sources {
				if err := os.WriteFile(filepath.Join(profiles, name+".jsonc"), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			base := []byte(`{"network":{"block":false,"allow_domain":[{"domain":"api.github.com","endpoints":[{"method":"GET","path":"/old"}]},"retained.example"]}}`)
			user := []byte(`{"network":{"block":false,"allow_domain":["api.github.com"]}}`)
			if mode == "restricted host" {
				user = []byte(`{"network":{"block":false,"allow_domain":[{"domain":"api.github.com","endpoints":[{"method":"POST","path":"/new"}]}]}}`)
			}
			resolved := string(user)
			if mode == "conflicting external rule" {
				resolved = string(base)
			} else if mode == "invalid resolution" {
				resolved = "invalid JSON"
			}
			t.Setenv("POLICY_RESULT", resolved)
			nono := filepath.Join(home, "nono")
			if err := os.WriteFile(nono, []byte("#!/bin/sh\nprintf '%s\\n' \"$POLICY_RESULT\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			path, cleanup, err := composeSessionPolicy(context.Background(), nono, "opencode-chatgpt", "opencode-user", "", base, user, false, nil, nil)
			if strings.Contains(mode, "conflicting") || strings.Contains(mode, "invalid") {
				if err == nil {
					cleanup()
					t.Fatal("accepted conflicting or invalid resolved policy")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(cleanup)
				if !strings.HasPrefix(path, profiles+string(filepath.Separator)) {
					t.Fatalf("generated policy is outside the protected profiles area: %s", path)
				}
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil || len(entries) != 5 {
					t.Fatalf("missing profile snapshots: %v (%v)", entries, err)
				}
				var all strings.Builder
				for _, entry := range entries {
					data, err := os.ReadFile(filepath.Join(filepath.Dir(path), entry.Name()))
					if err != nil || !json.Valid(data) {
						t.Fatalf("invalid generated profile: %s (%v)", data, err)
					}
					all.Write(data)
				}
				for _, retained := range []string{"KEEP", "CUSTOM_CA", "pathname", "$HOME/reference", "retained.example", "platform.example", "nolabs-ai/opencode", "opencode-user", "(deny appleevent-send)", "com.apple.coreservices.appleevents"} {
					if !strings.Contains(all.String(), retained) {
						t.Fatalf("lost policy field %q: %s", retained, all.String())
					}
				}
				if strings.Contains(strings.ToLower(all.String()), "api.github.com") || strings.Contains(all.String(), "/old") {
					t.Fatalf("bundled host rules survived the override: %s", all.String())
				}
				cleanup()
			}
			entries, err := os.ReadDir(profiles)
			if err != nil || len(entries) != len(sources) {
				t.Fatalf("generated policy residue remains: %v (%v)", entries, err)
			}
			for name, original := range sources {
				data, err := os.ReadFile(filepath.Join(profiles, name+".jsonc"))
				if err != nil || string(data) != original {
					t.Fatalf("changed original profile %q: %v", name, err)
				}
			}
		})
	}
}

func TestGitSSHCompositionAcceptsNullEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	profiles := filepath.Join(home, "config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	const original = `{"network":{"block":false},"environment":null}`
	source := filepath.Join(profiles, "codex-chatgpt.jsonc")
	if err := os.WriteFile(source, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	nono := filepath.Join(home, "nono")
	if err := os.WriteFile(nono, []byte("#!/bin/sh\nprintf '{\"network\":{\"block\":false}}\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	session, cleanup, err := composeSessionPolicy(t.Context(), nono, "codex-chatgpt", "", "", []byte(original), nil, false, nil,
		&gitSSHSession{driver: "generic-driver", files: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(session), "bundled0.json"))
	if err != nil {
		t.Fatal(err)
	}
	var copied struct {
		Environment struct {
			Variables map[string]string `json:"set_vars"`
		} `json:"environment"`
	}
	if err := json.Unmarshal(data, &copied); err != nil || copied.Environment.Variables["GIT_SSH"] != "generic-driver" {
		t.Fatalf("SSH default was not composed with null environment: %s (%v)", data, err)
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != original {
		t.Fatalf("composition changed the persistent profile: %s (%v)", data, err)
	}
	cleanup()
	if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 1 {
		t.Fatalf("composition left temporary policy files: %v (%v)", entries, err)
	}
}

func TestHostOverridesLeaveExternalProfilesToNono(t *testing.T) {
	path, cleanup, err := composeSessionPolicy(context.Background(), "missing-nono", "custom", "", "", nil, nil, false,
		[]pathRule{{"/custom/runtime", pathRead | pathWrite, false}}, nil)
	defer cleanup()
	if err != nil || path != "" {
		t.Fatalf("external profile was rewritten: path=%q err=%v", path, err)
	}
}

func TestExternalProfileAndUserAreInspectedAsOneNativePolicy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	profiles := filepath.Join(root, "config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	nono := filepath.Join(root, "nono")
	if err := os.WriteFile(nono, []byte("#!/bin/sh\nprintf '{\"network\":{\"block\":true}}\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := composeSessionPolicy(context.Background(), nono, "external-profile", "opencode-user", "", nil, nil, false,
		[]pathRule{{filepath.Join(root, "not-an-external-grant"), pathRead | pathWrite, false}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Extends    []string        `json:"extends"`
		Filesystem json.RawMessage `json:"filesystem"`
	}
	if err := json.Unmarshal(data, &session); err != nil || len(session.Extends) != 2 || session.Extends[0] != "external-profile" || session.Extends[1] != "opencode-user" || session.Filesystem != nil {
		t.Fatalf("external policy was rewritten or runtime grants were added: %s (%v)", data, err)
	}
	cleanup()
	if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 0 {
		t.Fatalf("inspection carrier left residue: %v (%v)", entries, err)
	}
}

func TestRuntimePathsRespectUserRestrictions(t *testing.T) {
	for _, mode := range []string{"runtime only", "exact read-only", "read-only parent"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			profiles := filepath.Join(home, "config/nono/profiles")
			runtime := filepath.Join(home, "custom-data/opencode")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			const source = `{"network":{"block":true},"filesystem":{"deny":["/retained-deny"]},"environment":{"allow_vars":["KEEP"]}}`
			if err := os.WriteFile(filepath.Join(profiles, "opencode-offline.jsonc"), []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			user, readPath := "", ""
			if mode != "runtime only" {
				user, readPath = "opencode-user", runtime
				if mode == "read-only parent" {
					readPath = filepath.Dir(runtime)
				}
			}
			userJSON, _ := json.Marshal(map[string]any{"network": map[string]any{"block": false}, "filesystem": map[string]any{"read": []string{readPath}}})
			grants := []map[string]string{}
			if readPath != "" {
				grants = append(grants, map[string]string{"path": readPath, "access": "read", "type": "directory"})
			}
			if mode != "exact read-only" {
				grants = append(grants, map[string]string{"path": runtime, "access": "readwrite", "type": "directory"})
			}
			manifest, _ := json.Marshal(map[string]any{"version": "0.1.0", "filesystem": map[string]any{"grants": grants}})
			t.Setenv("RUNTIME_MANIFEST", string(manifest))
			nono := filepath.Join(home, "nono")
			if err := os.WriteFile(nono, []byte("#!/bin/sh\nif [ \"$4\" = manifest ]; then printf '%s\\n' \"$RUNTIME_MANIFEST\"; else printf '{\"network\":{\"block\":true}}\\n'; fi\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			path, cleanup, err := composeSessionPolicy(context.Background(), nono, "opencode-offline", user, "", []byte(source), userJSON, false,
				[]pathRule{{runtime, pathRead | pathWrite, false}}, nil)
			defer cleanup()
			if mode == "read-only parent" {
				if err == nil || !strings.Contains(err.Error(), "cannot enforce") {
					t.Fatalf("runtime grant bypassed a broader user restriction: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var session struct {
					Filesystem struct{ Allow []string } `json:"filesystem"`
				}
				if err := json.Unmarshal(data, &session); err != nil {
					t.Fatal(err)
				}
				if mode == "exact read-only" && len(session.Filesystem.Allow) != 0 || mode == "runtime only" && (len(session.Filesystem.Allow) != 1 || session.Filesystem.Allow[0] != runtime) {
					t.Fatalf("unexpected runtime defaults in session: %s", data)
				}
				copied, err := os.ReadFile(filepath.Join(filepath.Dir(path), "bundled0.json"))
				if err != nil || !strings.Contains(string(copied), "/retained-deny") || !strings.Contains(string(copied), "KEEP") {
					t.Fatalf("lost inherited restrictions or environment: %s (%v)", copied, err)
				}
			}
			cleanup()
			if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 1 {
				t.Fatalf("runtime composition left residue: %v (%v)", entries, err)
			}
		})
	}
}

func TestComposeDropsSocketMediation(t *testing.T) {
	for _, mode := range []string{"bundled setting", "setting from another profile"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			profiles := filepath.Join(home, ".config/nono/profiles")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{
				"opencode-offline": `{"extends":"agent-policy","network":{"block":true}}`,
				"agent-policy":     `{"environment":{"allow_vars":["KEEP"]},"linux":{"af_unix_mediation":"pathname"}}`,
			} {
				if err := os.WriteFile(filepath.Join(profiles, name+".jsonc"), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			resolved := `{"network":{"block":true}}`
			if mode != "bundled setting" {
				resolved = `{"network":{"block":true},"linux":{"af_unix_mediation":"pathname"}}`
			}
			t.Setenv("POLICY_RESULT", resolved)
			nono := filepath.Join(home, "nono")
			if err := os.WriteFile(nono, []byte("#!/bin/sh\nprintf '%s\\n' \"$POLICY_RESULT\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			path, cleanup, err := composeSessionPolicy(context.Background(), nono, "opencode-offline", "", "", nil, nil, true, nil, nil)
			if mode != "bundled setting" {
				if err == nil {
					cleanup()
					t.Fatal("accepted a policy that still mediates sockets")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(filepath.Dir(path))
				if err != nil || len(entries) != 3 {
					t.Fatalf("missing profile snapshots: %v (%v)", entries, err)
				}
				var all strings.Builder
				for _, entry := range entries {
					data, err := os.ReadFile(filepath.Join(filepath.Dir(path), entry.Name()))
					if err != nil || !json.Valid(data) {
						t.Fatalf("invalid generated profile: %s (%v)", data, err)
					}
					all.Write(data)
				}
				if strings.Contains(all.String(), "pathname") || !strings.Contains(all.String(), "KEEP") || !strings.Contains(all.String(), `"block":true`) {
					t.Fatalf("unexpected generated policy: %s", all.String())
				}
				cleanup()
			}
			if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 2 {
				t.Fatalf("generated policy residue remains: %v (%v)", entries, err)
			}
		})
	}
}
