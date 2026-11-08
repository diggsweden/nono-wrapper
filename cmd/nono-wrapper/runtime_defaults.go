// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

//go:embed runtime-defaults.jsonc
var runtimeDefaultsSource []byte

type runtimeApplication struct {
	Description            string                   `json:"description,omitempty"`
	Variable               string                   `json:"variable"`
	FirstPath              bool                     `json:"first_path,omitempty"`
	DefaultRoot            string                   `json:"default_root"`
	SetEnvironmentDefault  bool                     `json:"set_environment_default"`
	Platform               string                   `json:"platform"`
	RequireExistingRoot    bool                     `json:"require_existing_root"`
	RejectPreparedSymlinks bool                     `json:"reject_prepared_symlinks"`
	Prepare                []string                 `json:"prepare"`
	Filesystem             map[string][]runtimePath `json:"filesystem"`
}

// Rule descriptions are optional documentation, never input to path resolution.
// Plain strings stay supported for concise rules and backwards compatibility.
type runtimePath struct {
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

func (rule *runtimePath) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &rule.Path)
	}
	type plain runtimePath
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(rule))
}

type runtimeDefaults struct {
	PublicKeyFiles   []runtimePath        `json:"public_key_files"`
	OptionalBypasses []runtimePath        `json:"optional_bypass_protection"`
	Description      string               `json:"description,omitempty"`
	Version          int                  `json:"version"`
	Environment      map[string]string    `json:"environment_defaults"`
	Applications     []runtimeApplication `json:"applications"`
	Podman           struct {
		Description       string   `json:"description,omitempty"`
		Platform          string   `json:"platform"`
		Socket            string   `json:"socket"`
		TransportVariable string   `json:"transport_variable"`
		OverrideVariables []string `json:"override_variables"`
	} `json:"podman"`
}

// nono warns for nonexistent bypass entries. Apply configured optional exact
// files only when present; this preserves its deny behavior for absent files.
func configuredBypassPaths(defaults runtimeDefaults) ([]string, error) {
	var paths []string
	for _, rule := range defaults.OptionalBypasses {
		expanded, err := expandPolicyPath(rule.Path)
		if err != nil {
			return nil, err
		}
		path, err := literalRuntimePath(expanded)
		if err != nil {
			return nil, err
		}
		// An automatic exception must not turn a config/plugin symlink into
		// permission to read a denied credential. Check parent redirects too.
		if path != filepath.Clean(expanded) {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || os.IsPermission(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func loadRuntimeDefaults() (runtimeDefaults, error) {
	return parseRuntimeDefaults(runtimeDefaultsSource)
}

func parseRuntimeDefaults(source []byte) (runtimeDefaults, error) {
	var defaults runtimeDefaults
	data, err := hujson.Standardize(source)
	if err != nil {
		return defaults, fmt.Errorf("parse bundled runtime defaults: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&defaults); err != nil {
		return defaults, fmt.Errorf("decode bundled runtime defaults: %w", err)
	}
	if defaults.Version != 1 {
		return defaults, fmt.Errorf("unsupported bundled runtime defaults version %d", defaults.Version)
	}
	for _, rule := range defaults.OptionalBypasses {
		if rule.Path == "" || strings.ContainsAny(rule.Path, "*?[") {
			return defaults, fmt.Errorf("optional bypass must name an exact file: %q", rule.Path)
		}
	}
	for _, rule := range defaults.PublicKeyFiles {
		if !strings.HasSuffix(rule.Path, ".pub") || strings.ContainsAny(filepath.Dir(rule.Path), "*?[") {
			return defaults, fmt.Errorf("public-key rule must select .pub files in a literal directory: %q", rule.Path)
		}
		if _, err := filepath.Match(filepath.Base(rule.Path), "key.pub"); err != nil {
			return defaults, fmt.Errorf("invalid public-key pattern: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, app := range defaults.Applications {
		if app.Variable == "" || app.DefaultRoot == "" || seen[app.Variable] {
			return defaults, fmt.Errorf("invalid or duplicate runtime variable %q", app.Variable)
		}
		seen[app.Variable] = true
		writable := map[string]bool{}
		for name, paths := range app.Filesystem {
			known := false
			for _, field := range filesystemFields {
				if field.name == name {
					known = true
					for _, rule := range paths {
						path := rule.Path
						if path == "" || filepath.IsAbs(path) || filepath.Clean(path) == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator)) || strings.ContainsAny(path, "$*?[") {
							return defaults, fmt.Errorf("runtime path must be a literal relative path: %q", path)
						}
						if field.access&pathWrite != 0 {
							writable[path] = true
						}
					}
					break
				}
			}
			if !known {
				return defaults, fmt.Errorf("unknown runtime filesystem field %q", name)
			}
		}
		for _, path := range app.Prepare {
			if !writable[path] {
				return defaults, fmt.Errorf("prepared runtime path %q must have an explicit writable grant", path)
			}
		}
	}
	return defaults, nil
}
