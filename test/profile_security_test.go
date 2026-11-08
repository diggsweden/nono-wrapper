// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

func TestOpenCodeDefaultWholeHostGrants(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles", "opencode-chatgpt.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	data, err = hujson.Standardize(data)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Network struct {
			AllowDomain []json.RawMessage `json:"allow_domain"`
		} `json:"network"`
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"opencode.ai", "chatgpt.com"} {
		t.Run(host, func(t *testing.T) {
			found := false
			for _, rule := range profile.Network.AllowDomain {
				var domain string
				if json.Unmarshal(rule, &domain) == nil {
					found = found || domain == host
					continue
				}
				var endpoint struct {
					Domain string `json:"domain"`
				}
				if err := json.Unmarshal(rule, &endpoint); err != nil {
					t.Fatal(err)
				}
				if endpoint.Domain == host {
					t.Errorf("%s must not have endpoint rules that block WebSocket upgrades", host)
				}
			}
			if !found {
				t.Errorf("missing whole-host grant for %s", host)
			}
		})
	}
}

// Check every shipped entry point, not just agent-policy: changing the
// inheritance graph must not silently restore the terminal escape route.
func TestBundledProfilesDenyAppleEvents(t *testing.T) {
	var rules func(string) []string
	rules = func(name string) []string {
		t.Helper()
		if strings.HasPrefix(name, "nolabs-ai/") {
			return nil // The security floor must come from our own profiles.
		}
		data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles", name+".jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		data, err = hujson.Standardize(data)
		if err != nil {
			t.Fatal(err)
		}
		var profile struct {
			Extends json.RawMessage `json:"extends"`
			Rules   []string        `json:"unsafe_macos_seatbelt_rules"`
		}
		if err := json.Unmarshal(data, &profile); err != nil {
			t.Fatal(err)
		}
		var parents []string
		if len(profile.Extends) != 0 {
			var parent string
			if json.Unmarshal(profile.Extends, &parent) == nil {
				parents = []string{parent}
			} else if err := json.Unmarshal(profile.Extends, &parents); err != nil {
				t.Fatal(err)
			}
		}
		for _, parent := range parents {
			profile.Rules = append(profile.Rules, rules(parent)...)
		}
		return profile.Rules
	}
	for _, agent := range []string{"codex", "opencode"} {
		for _, suffix := range []string{"base", "chatgpt", "offline"} {
			name := agent + "-" + suffix
			t.Run(name, func(t *testing.T) {
				got := rules(name)
				for _, required := range []string{
					"(deny appleevent-send)",
					`(deny mach-lookup (global-name "com.apple.coreservices.appleevents"))`,
				} {
					if !slices.Contains(got, required) {
						t.Errorf("%s does not inherit %s", name, required)
					}
				}
			})
		}
	}
}
