// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type pathAccess uint8

const (
	pathRead pathAccess = 1 << iota
	pathWrite
)

func (access pathAccess) String() string {
	switch access {
	case pathRead:
		return "read-only"
	case pathWrite:
		return "write-only"
	default:
		return "read/write"
	}
}

var filesystemFields = []struct {
	name   string
	access pathAccess
	file   bool
}{
	{"read", pathRead, false}, {"write", pathWrite, false}, {"allow", pathRead | pathWrite, false},
	{"read_file", pathRead, true}, {"write_file", pathWrite, true}, {"allow_file", pathRead | pathWrite, true},
}

type pathRule struct {
	path   string
	access pathAccess
	file   bool
}

// Match nono 0.79's profile path variables before comparing resource identities.
// Resolve symlinks (and existing ancestors for absent paths) so spelling a path
// through $HOME, XDG variables or a symlink cannot bypass precedence/protection.
func policyPathKey(path string) (string, error) {
	home := os.Getenv("HOME")
	workdir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") {
		path = home + path[1:]
	}
	path = strings.ReplaceAll(path, "$WORKDIR", workdir)
	path = strings.ReplaceAll(path, "$TMPDIR", strings.TrimRight(os.TempDir(), "/"))
	path = strings.ReplaceAll(path, "$UID", strconv.Itoa(os.Getuid()))
	path = strings.ReplaceAll(path, "$HOME", home)
	for _, variable := range []struct{ name, fallback string }{
		{"XDG_CONFIG_HOME", filepath.Join(home, ".config")},
		{"XDG_STATE_HOME", filepath.Join(home, ".local/state")},
		{"XDG_CACHE_HOME", filepath.Join(home, ".cache")},
		{"XDG_DATA_HOME", filepath.Join(home, ".local/share")},
		{"XDG_RUNTIME_DIR", ""},
	} {
		value, set := os.LookupEnv(variable.name)
		if !set {
			value = variable.fallback
		}
		if !strings.Contains(path, "$"+variable.name) {
			continue
		}
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("%s must be set to an absolute path for this filesystem rule", variable.name)
		}
		path = strings.ReplaceAll(path, "$"+variable.name, value)
	}
	if strings.Contains(path, "$NONO_CONFIG") || strings.Contains(path, "$NONO_PACKAGES") {
		profile, err := configPath("opencode")
		if err != nil {
			return "", err
		}
		root := filepath.Dir(filepath.Dir(profile))
		path = strings.ReplaceAll(path, "$NONO_CONFIG", root)
		path = strings.ReplaceAll(path, "$NONO_PACKAGES", filepath.Join(root, "packages"))
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", quotePathError(err)
	}
	return canonicalConfigPath(path)
}

func profilePathRules(data []byte) ([]pathRule, error) {
	var shown struct {
		Filesystem map[string][]string `json:"filesystem"`
	}
	if err := json.Unmarshal(data, &shown); err != nil {
		return nil, fmt.Errorf("invalid resolved filesystem rules: %w", err)
	}
	byPath := map[string]pathRule{}
	for _, field := range filesystemFields {
		for _, raw := range shown.Filesystem[field.name] {
			if raw == "" || !field.file && strings.Contains(raw, "*") {
				return nil, fmt.Errorf("user filesystem overrides require literal, non-empty paths: %q", raw)
			}
			path, err := policyPathKey(raw)
			if err != nil {
				return nil, err
			}
			rule := pathRule{path, field.access, field.file}
			if info, err := os.Stat(path); err == nil {
				rule.file = !info.IsDir()
			}
			for existing, previous := range byPath {
				if !samePolicyPath(path, existing) {
					continue
				}
				if previous.access != rule.access {
					return nil, fmt.Errorf("user profile gives %q multiple access modes; choose one mode for this path", path)
				}
				path, rule.path = existing, existing
				break
			}
			if rule.access&pathWrite != 0 {
				if err := protectRuleWrite(path); err != nil {
					return nil, err
				}
			}
			byPath[path] = rule
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	rules := make([]pathRule, 0, len(paths))
	for _, path := range paths {
		rules = append(rules, byPath[path])
	}
	return rules, nil
}

func stripPathRules(profile map[string]json.RawMessage, overrides []pathRule) error {
	data, exists := profile["filesystem"]
	if !exists || len(overrides) == 0 {
		return nil
	}
	var filesystem map[string]json.RawMessage
	if err := json.Unmarshal(data, &filesystem); err != nil {
		return err
	}
	for _, field := range filesystemFields {
		data, exists := filesystem[field.name]
		if !exists {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil {
			return err
		}
		kept := make([]json.RawMessage, 0, len(entries))
		for _, entry := range entries {
			var raw string
			if json.Unmarshal(entry, &raw) != nil {
				var conditional struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(entry, &conditional); err != nil || conditional.Path == "" {
					return fmt.Errorf("invalid path entry in filesystem.%s", field.name)
				}
				raw = conditional.Path
			}
			if !field.file && strings.Contains(raw, "*") {
				// A glob is a set of resources, even if a literal file happens
				// to have that spelling. Exact-path overrides cannot replace it.
				kept = append(kept, entry)
				continue
			}
			path, err := policyPathKey(raw)
			if err != nil {
				return err
			}
			overridden := false
			for _, rule := range overrides {
				overridden = overridden || samePolicyPath(path, rule.path)
			}
			if !overridden {
				kept = append(kept, entry)
			}
		}
		filesystem[field.name], _ = json.Marshal(kept)
	}
	profile["filesystem"], _ = json.Marshal(filesystem)
	return nil
}

// Use the manifest only to inspect effective filesystem grants (including
// groups), never to recreate a launch policy: its other sections are lossy.
func verifyPathOverrides(ctx context.Context, nono, profile, program string, shown []byte, rules []pathRule) error {
	if len(rules) == 0 {
		return nil
	}
	check := exec.CommandContext(ctx, nono, "profile", "show", "--format", "manifest", profile)
	var diagnostic strings.Builder
	check.Stderr = &diagnostic
	data, err := check.Output()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("inspect effective filesystem grants: %w\n%s", err, diagnostic.String())
	}
	grants, err := manifestPathGrants(data)
	if err != nil {
		return err
	}
	// Account for launch-time grants that are not part of the profile export.
	if program != "" {
		grants = append(grants, pathRule{program, pathRead, true})
	}
	if socket := sshAgentArgs(); len(socket) == 2 {
		path, err := policyPathKey(socket[1])
		if err != nil {
			return err
		}
		grants = append(grants, pathRule{path, pathRead, true})
	}
	for _, raw := range strings.Split(os.Getenv("NONO_ALLOW"), ",") {
		if raw == "" {
			continue
		}
		path, err := policyPathKey(raw)
		if err != nil {
			return err
		}
		grants = append(grants, pathRule{path, pathRead | pathWrite, false})
	}
	// --allow-cwd defaults to read when workdir.access is none.
	var resolved struct {
		Workdir struct {
			Access string `json:"access"`
		} `json:"workdir"`
		Filesystem map[string][]string `json:"filesystem"`
	}
	if err := json.Unmarshal(shown, &resolved); err != nil {
		return fmt.Errorf("invalid resolved filesystem policy: %w", err)
	}
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	workdirAccess := pathRead
	if resolved.Workdir.Access == "readwrite" {
		workdirAccess |= pathWrite
	} else if resolved.Workdir.Access == "write" {
		workdirAccess = pathWrite
	}
	grants = append(grants, pathRule{workdir, workdirAccess, false})
	// Bind grants imply filesystem writes that the manifest does not export.
	for _, field := range []string{"unix_socket_bind", "unix_socket_dir_bind", "unix_socket_subtree_bind"} {
		for _, raw := range resolved.Filesystem[field] {
			path, err := policyPathKey(raw)
			if err != nil {
				return err
			}
			file := field == "unix_socket_bind"
			if _, err := os.Stat(path); file && os.IsNotExist(err) {
				path, file = filepath.Dir(path), false
			}
			grants = append(grants, pathRule{path, pathRead | pathWrite, file})
		}
	}
	return checkPathOverrides(rules, grants)
}

func manifestPathGrants(data []byte) ([]pathRule, error) {
	var manifest struct {
		Version    string `json:"version"`
		Filesystem *struct {
			Grants []struct {
				Path   string `json:"path"`
				Access string `json:"access"`
				Type   string `json:"type"`
			} `json:"grants"`
		} `json:"filesystem"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != "0.1.0" || manifest.Filesystem == nil {
		return nil, fmt.Errorf("nono returned an invalid filesystem manifest")
	}
	grants := make([]pathRule, 0, len(manifest.Filesystem.Grants))
	for _, grant := range manifest.Filesystem.Grants {
		var access pathAccess
		switch grant.Access {
		case "read":
			access = pathRead
		case "write":
			access = pathWrite
		case "readwrite":
			access = pathRead | pathWrite
		default:
			return nil, fmt.Errorf("nono returned invalid filesystem access %q", grant.Access)
		}
		if !filepath.IsAbs(grant.Path) || (grant.Type != "file" && grant.Type != "directory") {
			return nil, fmt.Errorf("nono returned an invalid filesystem grant")
		}
		path, err := canonicalConfigPath(grant.Path)
		if err != nil {
			return nil, err
		}
		grants = append(grants, pathRule{path, access, grant.Type == "file"})
	}
	return grants, nil
}

func checkPathOverrides(rules, grants []pathRule) error {
	present := make([]pathAccess, len(rules))
	for _, grant := range grants {
		path := grant.path
		// A surviving directory glob may cover the requested path. Treat its
		// fixed prefix conservatively instead of claiming a restriction worked.
		if index := strings.IndexByte(path, '*'); index >= 0 && !grant.file {
			path = filepath.Dir(path[:index] + "glob")
		}
		if grant.access&pathWrite != 0 {
			if err := protectRuleWrite(path); err != nil {
				return err
			}
		}
		for i, rule := range rules {
			covers := samePolicyPath(path, rule.path) || !grant.file && pathWithin(rule.path, path)
			inside := !rule.file && pathWithin(path, rule.path)
			if covers {
				present[i] |= grant.access
			}
			if (covers || inside) && grant.access & ^rule.access != 0 {
				return fmt.Errorf("cannot enforce %s user access to %q: overlapping %s grant for %q remains; narrow or remove that grant", rule.access, rule.path, grant.access, grant.path)
			}
		}
	}
	for i, rule := range rules {
		if present[i]&rule.access != rule.access {
			return fmt.Errorf("resolved filesystem policy is missing the %s user allowance for %q", rule.access, rule.path)
		}
	}
	return nil
}
