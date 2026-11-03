// SPDX-FileCopyrightText: The nono-wrapper Authors
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

func TestDeveloperNetworkDefaultsStayOnlineAndPortFree(t *testing.T) {
	type profile struct {
		Extends json.RawMessage `json:"extends"`
		Network struct {
			Block  bool              `json:"block"`
			Hosts  []json.RawMessage `json:"allow_domain"`
			Open   []int             `json:"open_port"`
			Listen []int             `json:"listen_port"`
		} `json:"network"`
	}
	read := func(name string) profile {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles", name+".jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		data, err = hujson.Standardize(data)
		if err != nil {
			t.Fatal(err)
		}
		var result profile
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	var hosts func(string) []string
	hosts = func(name string) []string {
		t.Helper()
		if strings.HasPrefix(name, "nolabs-ai/") {
			return nil
		}
		p := read(name)
		var parents []string
		var single string
		if len(p.Extends) > 0 {
			if json.Unmarshal(p.Extends, &single) == nil {
				parents = []string{single}
			} else if err := json.Unmarshal(p.Extends, &parents); err != nil {
				t.Fatal(err)
			}
		}
		var result []string
		for _, parent := range parents {
			result = append(result, hosts(parent)...)
		}
		for _, entry := range p.Network.Hosts {
			var host string
			if json.Unmarshal(entry, &host) != nil {
				var rule struct{ Domain string }
				if err := json.Unmarshal(entry, &rule); err != nil {
					t.Fatal(err)
				}
				host = rule.Domain
			}
			result = append(result, host)
		}
		return result
	}
	defaults := read("developer-network")
	if defaults.Network.Block || len(defaults.Network.Open) > 0 || len(defaults.Network.Listen) > 0 {
		t.Fatal("shared domain defaults must not add raw TCP port exceptions")
	}
	for _, agent := range []string{"opencode", "codex"} {
		online := hosts(agent + "-chatgpt")
		for _, required := range []string{"github.com", "gitlab.com", "codefloe.com", "codeberg.org", "en.wikipedia.org", "stackoverflow.com", "api.osv.dev", "vuln.go.dev", "ghcr.io"} {
			if !slices.Contains(online, required) {
				t.Errorf("%s does not inherit %s", agent, required)
			}
		}
		if offline := hosts(agent + "-offline"); len(offline) > 0 || !read(agent+"-offline").Network.Block {
			t.Errorf("%s offline profile inherits external host grants: %v", agent, offline)
		}
		for _, unwanted := range []string{"*", "*.github.com", "*.gitlab.com", "registry.npmjs.org", "pypi.org"} {
			if slices.Contains(online, unwanted) {
				t.Errorf("%s unexpectedly inherits %s", agent, unwanted)
			}
		}
	}
}
