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
				"opencode-chatgpt": `{"extends":"opencode-base","environment":{"allow_vars":["KEEP"]},"network":{"tls_intercept":{"ca_env_vars":["CUSTOM_CA"]},"allow_domain":[{"domain":"api.github.com","endpoints":[{"method":"GET","path":"/old"}]},"retained.example"]}}`,
				"opencode-base":    `{"extends":["nolabs-ai/opencode","agent-policy"],"linux":{"af_unix_mediation":"pathname"}}`,
				"agent-policy":     `{"filesystem":{"read":["$HOME/reference"]},"unsafe_macos_seatbelt_rules":["(deny appleevent-send)","(deny mach-lookup (global-name \"com.apple.coreservices.appleevents\"))"],"platform_overrides":{"linux":{"network":{"allow_domain":["API.GITHUB.COM.","platform.example"]}}}}`,
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
			path, cleanup, err := composeUserOverrides(context.Background(), nono, "opencode-chatgpt", "opencode-user", "", base, user, false)
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
				if err != nil || len(entries) != 4 {
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

func TestHostOverridesLeaveExternalProfilesToNono(t *testing.T) {
	path, cleanup, err := composeUserOverrides(context.Background(), "missing-nono", "custom", "opencode-user", "", nil, nil, false)
	defer cleanup()
	if err != nil || path != "" {
		t.Fatalf("external profile was rewritten: path=%q err=%v", path, err)
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
			path, cleanup, err := composeUserOverrides(context.Background(), nono, "opencode-offline", "", "", nil, nil, true)
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
