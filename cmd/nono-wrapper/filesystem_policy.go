// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
	"syscall"
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

// Match nono's profile path variables before comparing resource identities.
// Resolve symlinks (and existing ancestors for absent paths) so spelling a path
// through $HOME, XDG variables or a symlink cannot bypass precedence/protection.
func policyPathKey(path string) (string, error) {
	path, err := expandPolicyPath(path)
	if err != nil {
		return "", err
	}
	return canonicalConfigPath(path)
}

// Expand profile variables without following symlinks, so edits can match
// the user's selected source path even if its link target changes.
func expandPolicyPath(path string) (string, error) {
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
	return path, nil
}

// A generated state grant is a literal directory, not a profile expression
// or glob. Validate exactly the canonical string that will be serialized.
func literalRuntimePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", invalidConfig{fmt.Errorf("runtime path must be absolute: %q", path)}
	}
	actual, err := canonicalConfigPath(path)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(actual, "*?[") {
		return "", invalidConfig{fmt.Errorf("runtime path must not contain profile glob patterns: %q", actual)}
	}
	expanded, err := policyPathKey(actual)
	if err != nil {
		return "", err
	}
	if actual != expanded {
		return "", invalidConfig{fmt.Errorf("runtime path must not contain profile variable references: %q", actual)}
	}
	return actual, nil
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
	// Socket bind capabilities imply filesystem writes too; an exact user
	// override must remove the capability as well as ordinary path grants.
	fields := []string{"unix_socket_subtree_bind"}
	for _, field := range filesystemFields {
		fields = append(fields, field.name)
	}
	for _, name := range fields {
		data, exists := filesystem[name]
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
					return fmt.Errorf("invalid path entry in filesystem.%s", name)
				}
				raw = conditional.Path
			}
			if !strings.HasSuffix(name, "_file") && strings.Contains(raw, "*") {
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
		filesystem[name], _ = json.Marshal(kept)
	}
	profile["filesystem"], _ = json.Marshal(filesystem)
	return nil
}

// Use the manifest only to inspect effective filesystem grants (including
// groups), never to recreate a launch policy: its other sections are lossy.
func verifyPathOverrides(ctx context.Context, nono, profile, program string, shown []byte, rules []pathRule, environment []string) error {
	if len(rules) == 0 {
		return nil
	}
	grants, err := effectivePathGrants(ctx, nono, profile, program, shown, environment)
	if err != nil {
		return err
	}
	return checkPathOverrides(rules, grants)
}

// Inspect native inheritance and groups, then account for launch-time grants
// absent from the export. All permission checks share this effective view.
func effectivePathGrants(ctx context.Context, nono, profile, program string, shown []byte, environment []string) ([]pathRule, error) {
	check := exec.CommandContext(ctx, nono, "profile", "show", "--format", "manifest", profile)
	check.Env = append(os.Environ(), environment...)
	var diagnostic strings.Builder
	check.Stderr = &diagnostic
	data, err := check.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("inspect effective filesystem grants: %w\n%s", err, diagnostic.String())
	}
	grants, err := manifestPathGrants(data)
	if err != nil {
		return nil, err
	}
	// Account for launch-time grants that are not part of the profile export.
	if program != "" {
		grants = append(grants, pathRule{program, pathRead, true})
	}
	if socket := sshAgentArgs(); len(socket) == 2 {
		path, err := canonicalConfigPath(socket[1])
		if err != nil {
			return nil, err
		}
		grants = append(grants, pathRule{path, pathRead, true})
	}
	for _, raw := range strings.Split(os.Getenv("NONO_ALLOW"), ",") {
		if raw == "" {
			continue
		}
		// CLI paths are filesystem paths, not profile-variable expressions.
		path, err := filepath.Abs(raw)
		if err == nil {
			path, err = canonicalConfigPath(path)
		}
		if err != nil {
			return nil, err
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
		return nil, fmt.Errorf("invalid resolved filesystem policy: %w", err)
	}
	workdir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	workdir, err = canonicalConfigPath(workdir)
	if err != nil {
		return nil, err
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
				return nil, err
			}
			file := field == "unix_socket_bind"
			if _, err := os.Stat(path); file && os.IsNotExist(err) {
				path, file = filepath.Dir(path), false
			}
			grants = append(grants, pathRule{path, pathRead | pathWrite, file})
		}
	}
	return grants, nil
}

// Protect executable files, invocation entries and known Mise data trees. A file's
// containing directory is not itself a protected tree: standalone binaries
// may live in HOME without making every sibling state directory unwritable.
func protectedLaunchPaths(executables ...executable) ([]pathRule, error) {
	own, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var paths []string
	var protected []pathRule
	seen := map[pathRule]bool{}
	add := func(path string, file bool) {
		rule := pathRule{path, pathRead, file}
		if !seen[rule] {
			seen[rule] = true
			protected = append(protected, rule)
		}
	}
	paths = append(paths, own)
	if entry, err := exec.LookPath(os.Args[0]); err == nil {
		paths = append(paths, entry)
	}
	// Activated Mise shells may put raw tool bins on PATH instead of shims.
	// The manager still needs protection even when resolution did not invoke it.
	if manager, err := exec.LookPath("mise"); err == nil {
		paths = append(paths, manager)
	}
	for _, executable := range executables {
		paths = append(paths, executable.path, executable.manager, executable.entry)
	}
	for _, path := range paths {
		if path == "" {
			continue
		}
		entries, err := executableEntries(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			add(entry, true)
		}
		actual, err := canonicalConfigPath(path)
		if err != nil {
			return nil, err
		}
		add(actual, true)
	}
	mise, err := miseProtectedPaths()
	if err != nil {
		return nil, err
	}
	for _, rule := range mise {
		add(rule.path, rule.file)
	}
	return protected, nil
}

func grantPathPrefix(grant pathRule) string {
	path := grant.path
	if index := strings.IndexAny(path, "*?["); index >= 0 && !grant.file {
		path = filepath.Dir(path[:index] + "glob")
	}
	return path
}

func checkProtectedWrites(grants, protected []pathRule) error {
	for _, grant := range grants {
		if grant.access&pathWrite == 0 {
			continue
		}
		path := grantPathPrefix(grant)
		if err := protectRuleWrite(path); err != nil {
			return err
		}
		for _, target := range protected {
			overlaps := samePolicyPath(path, target.path) || !grant.file && pathWithin(target.path, path)
			if !target.file {
				overlaps = overlaps || pathWithin(path, target.path)
			}
			if overlaps {
				return fmt.Errorf("writable grant %q overlaps protected launch location %q; move state or workspace, or narrow the grant", grant.path, target.path)
			}
			// A directory grant could contain an unseen hard-link alias. Reject
			// multiply linked protected files rather than scan entire workspaces
			// and caches to try to locate every alias.
			if !grant.file && target.file {
				info, err := os.Stat(target.path)
				if err != nil && !os.IsNotExist(err) {
					return quotePathError(err)
				}
				if err == nil && info.Mode().IsRegular() && info.Sys().(*syscall.Stat_t).Nlink > 1 {
					return fmt.Errorf("protected launch file %q has multiple hard links; use a single-link copy before granting writable directory %q", target.path, grant.path)
				}
			}
		}
	}
	return nil
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
		path := grantPathPrefix(grant)
		// A surviving directory glob may cover the requested path. Treat its
		// fixed prefix conservatively instead of claiming a restriction worked.
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
			extra := grant.access & ^rule.access
			if (covers || inside) && extra != 0 && !pathGrantAuthorized(path, extra, rules) {
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

// A more specific user rule may intentionally widen access beneath a more
// restrictive directory. Only accept an effective grant when its entire scope
// is contained by a user rule that permits the otherwise conflicting access.
func pathGrantAuthorized(path string, access pathAccess, rules []pathRule) bool {
	for _, rule := range rules {
		covers := samePolicyPath(path, rule.path) || !rule.file && pathWithin(path, rule.path)
		if covers && rule.access&access == access {
			return true
		}
	}
	return false
}
