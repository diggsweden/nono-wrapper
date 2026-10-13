// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Only these profiles belong to the wrapper. Pack and external profile
// inheritance remains nono's responsibility, including hook provenance.
func bundledProfile(name string) bool {
	switch name {
	case "agent-policy", "opencode-base", "opencode-chatgpt", "opencode-offline",
		"codex-base", "codex-chatgpt", "codex-offline":
		return true
	}
	return false
}

type hostRule struct {
	Domain    string `json:"domain"`
	Endpoints []struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	} `json:"endpoints"`
}

func hostKey(domain string) string {
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

func decodeHostRule(data json.RawMessage) (hostRule, error) {
	var rule hostRule
	if json.Unmarshal(data, &rule.Domain) != nil {
		if err := json.Unmarshal(data, &rule); err != nil {
			return rule, fmt.Errorf("invalid host rule: %w", err)
		}
	}
	if rule.Domain == "" {
		return rule, fmt.Errorf("host rule has no domain")
	}
	rule.Domain = hostKey(rule.Domain)
	if len(rule.Endpoints) == 0 {
		rule.Endpoints = nil
	}
	return rule, nil
}

func profileHostRules(data []byte) (map[string]hostRule, error) {
	var shown struct {
		Network struct {
			AllowDomain []json.RawMessage `json:"allow_domain"`
		} `json:"network"`
	}
	if err := json.Unmarshal(data, &shown); err != nil {
		return nil, fmt.Errorf("invalid resolved host rules: %w", err)
	}
	rules := map[string]hostRule{}
	for _, entry := range shown.Network.AllowDomain {
		rule, err := decodeHostRule(entry)
		if err != nil {
			return nil, err
		}
		rules[rule.Domain] = rule
	}
	return rules, nil
}

// Remove matching bundled definitions before native inheritance runs.
// Reordering --extends alone cannot replace nono's additive host/path rules.
// Never rebuild policy from profile show's JSON: it omits environment and TLS
// settings. These private copies preserve the complete original JSONC instead.
// The same copies drop socket mediation where nono cannot enforce it; user is
// empty when that is the only change.
func composeUserOverrides(ctx context.Context, nono, base, user, program string, baseJSON, userJSON []byte, dropSocketMediation bool) (string, func(), error) {
	noop := func() {}
	if !bundledProfile(base) {
		return "", noop, nil
	}
	overrides := map[string]hostRule{}
	var paths []pathRule
	if user != "" {
		baseRules, err := profileHostRules(baseJSON)
		if err != nil {
			return "", noop, err
		}
		userRules, err := profileHostRules(userJSON)
		if err != nil {
			return "", noop, err
		}
		for host, rule := range userRules {
			if _, present := baseRules[host]; present {
				overrides[host] = rule
			}
		}
		paths, err = profilePathRules(userJSON)
		if err != nil {
			return "", noop, err
		}
	}
	if len(overrides) == 0 && len(paths) == 0 && !dropSocketMediation {
		return "", noop, nil
	}
	userPath, err := configPath(strings.TrimSuffix(user, "-user"))
	if err != nil {
		return "", noop, err
	}
	profiles := filepath.Dir(userPath)
	directory, err := os.MkdirTemp(profiles, ".nono-wrapper-policy-")
	if err != nil {
		return "", noop, quotePathError(err)
	}
	cleanup := func() {
		if err := os.RemoveAll(directory); err != nil {
			fmt.Fprintf(os.Stderr, "nono-wrapper: could not remove temporary policy directory %q: %s\n", directory, quotePathError(err))
		}
	}
	complete := false
	defer func() {
		if !complete {
			cleanup()
		}
	}()

	// Use sibling .json references, supported by nono, inside the private
	// directory. Only wrapper-owned sources are copied; the original files
	// and package references stay untouched.
	copied := map[string]string{}
	var copyProfile func(string, string, int) (string, error)
	copyProfile = func(name, source string, depth int) (string, error) {
		if depth >= 10 {
			return "", fmt.Errorf("bundled profile inheritance is too deep")
		}
		if source == "" {
			source = filepath.Join(profiles, name+".jsonc")
			if _, err := os.Lstat(source); os.IsNotExist(err) {
				source = filepath.Join(profiles, name+".json")
			}
		}
		source, err := filepath.EvalSymlinks(source)
		if err != nil {
			return "", quotePathError(err)
		}
		if alias, ok := copied[source]; ok {
			return alias, nil
		}
		doc, original, _, err := readUserConfig(source)
		if err != nil {
			return "", err
		}
		if original == nil {
			return "", fmt.Errorf("bundled profile disappeared: %q", source)
		}
		doc.Standardize()
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(doc.Pack(), &raw); err != nil {
			return "", err
		}
		if err := stripBundledRules(raw, overrides, paths, dropSocketMediation); err != nil {
			return "", err
		}
		alias := fmt.Sprintf("bundled%d", len(copied))
		copied[source] = alias
		var parents []string
		if value, ok := raw["extends"]; ok && string(value) != "null" {
			var single string
			if json.Unmarshal(value, &single) == nil {
				parents = []string{single}
			} else if err := json.Unmarshal(value, &parents); err != nil {
				return "", fmt.Errorf("invalid extends in %q: %w", source, err)
			}
			for i, parent := range parents {
				if !bundledProfile(parent) {
					continue
				}
				// nono checks canonical sibling .json files before user profiles.
				sibling := filepath.Join(filepath.Dir(source), parent+".json")
				if info, err := os.Stat(sibling); err != nil || !info.Mode().IsRegular() || sibling == source {
					sibling = ""
				}
				parents[i], err = copyProfile(parent, sibling, depth+1)
				if err != nil {
					return "", err
				}
			}
			raw["extends"], _ = json.Marshal(parents)
		}
		data, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(directory, alias+".json"), data, 0o600); err != nil {
			return "", quotePathError(err)
		}
		return alias, nil
	}
	alias, err := copyProfile(base, "", 0)
	if err != nil {
		return "", noop, err
	}
	parents := []string{alias}
	if user != "" {
		parents = append(parents, user)
	}
	data, _ := json.Marshal(map[string]any{"extends": parents})
	path := filepath.Join(directory, "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", noop, quotePathError(err)
	}
	shown, diagnostic, err := showProfile(ctx, nono, path)
	if err != nil {
		return "", noop, fmt.Errorf("resolve user overrides: %w\n%s", err, diagnostic)
	}
	if err := ctx.Err(); err != nil {
		return "", noop, err
	}
	actual, err := profileHostRules(shown)
	if err != nil {
		return "", noop, err
	}
	for host, want := range overrides {
		if !reflect.DeepEqual(actual[host], want) {
			return "", noop, fmt.Errorf("cannot override host %q: a rule outside the bundled profiles still conflicts", host)
		}
	}
	if err := verifyPathOverrides(ctx, nono, path, program, shown, paths); err != nil {
		return "", noop, err
	}
	if dropSocketMediation && mediatesSockets(shown) {
		return "", noop, fmt.Errorf("cannot start on WSL2: a profile outside the bundled profiles still sets linux.af_unix_mediation, which nono cannot enforce there")
	}
	complete = true
	return path, cleanup, nil
}

func stripBundledRules(profile map[string]json.RawMessage, overrides map[string]hostRule, paths []pathRule, dropSocketMediation bool) error {
	if err := stripPathRules(profile, paths); err != nil {
		return err
	}
	if data, ok := profile["linux"]; ok && dropSocketMediation {
		var linux map[string]json.RawMessage
		if err := json.Unmarshal(data, &linux); err != nil {
			return err
		}
		delete(linux, "af_unix_mediation")
		profile["linux"], _ = json.Marshal(linux)
	}
	if data, ok := profile["network"]; ok {
		var network map[string]json.RawMessage
		if err := json.Unmarshal(data, &network); err != nil {
			return err
		}
		if data, ok := network["allow_domain"]; ok {
			var entries []json.RawMessage
			if err := json.Unmarshal(data, &entries); err != nil {
				return err
			}
			kept := make([]json.RawMessage, 0, len(entries))
			for _, entry := range entries {
				rule, err := decodeHostRule(entry)
				if err != nil {
					return err
				}
				if _, overridden := overrides[rule.Domain]; !overridden {
					kept = append(kept, entry)
				}
			}
			network["allow_domain"], _ = json.Marshal(kept)
		}
		profile["network"], _ = json.Marshal(network)
	}
	if data, ok := profile["platform_overrides"]; ok {
		var platforms map[string]map[string]json.RawMessage
		if err := json.Unmarshal(data, &platforms); err != nil {
			return err
		}
		for _, platform := range platforms {
			if err := stripBundledRules(platform, overrides, paths, dropSocketMediation); err != nil {
				return err
			}
		}
		profile["platform_overrides"], _ = json.Marshal(platforms)
	}
	return nil
}
