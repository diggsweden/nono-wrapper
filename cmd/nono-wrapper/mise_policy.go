// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Mise's installed code, configuration and trust inputs are control-plane state.
// Its writable cache is an explicitly accepted exception. Discover
// documented locations and filesystem aliases without evaluating any config.
func miseProtectedPaths() ([]pathRule, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	var paths []pathRule
	seen := map[pathRule]bool{}
	add := func(path string, file bool) error {
		if !filepath.IsAbs(path) {
			return invalidConfig{fmt.Errorf("Mise control path must be absolute: %q", path)}
		}
		entries, err := executableEntries(path)
		if err != nil {
			return err
		}
		actual, err := canonicalConfigPath(path)
		if err != nil {
			return err
		}
		for _, value := range append(entries, actual) {
			// Protect alias entries, not every child of a symlinked ancestor.
			rule := pathRule{value, pathRead, true}
			if value == actual {
				rule.file = file
			}
			if !seen[rule] {
				seen[rule] = true
				paths = append(paths, rule)
			}
		}
		return nil
	}
	visited := map[string]bool{}
	var inspectConfig func(string) error
	inspectConfig = func(path string) error {
		if err := add(path, false); err != nil {
			return err
		}
		actual, err := canonicalConfigPath(path)
		if err != nil || visited[actual] {
			return err
		}
		if pathWithin(home, actual) {
			return invalidConfig{fmt.Errorf("Mise configuration path %q contains HOME; use a dedicated configuration directory", path)}
		}
		visited[actual] = true
		entries, err := os.ReadDir(actual)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect Mise configuration aliases: %w", quotePathError(err))
		}
		for _, entry := range entries {
			path := filepath.Join(actual, entry.Name())
			info, err := os.Stat(path)
			if os.IsNotExist(err) {
				// A dangling link can become an active control input later.
				if err := add(path, false); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return quotePathError(err)
			}
			if info.IsDir() && entry.Name() != ".git" {
				if err := inspectConfig(path); err != nil {
					return err
				}
			} else if err := add(path, !info.IsDir()); err != nil {
				return err
			}
		}
		return nil
	}
	for _, location := range []struct{ xdg, fallback, override string }{
		{"XDG_CONFIG_HOME", ".config", "MISE_CONFIG_DIR"},
		{"XDG_DATA_HOME", ".local/share", "MISE_DATA_DIR"},
		{"XDG_STATE_HOME", ".local/state", "MISE_STATE_DIR"},
	} {
		base := os.Getenv(location.xdg)
		if base == "" {
			base = filepath.Join(home, location.fallback)
		}
		roots := []string{filepath.Join(home, location.fallback, "mise"), filepath.Join(base, "mise"), os.Getenv(location.override)}
		for _, root := range roots {
			if root == "" {
				continue
			}
			if location.override == "MISE_CONFIG_DIR" {
				if err := inspectConfig(root); err != nil {
					return nil, err
				}
			} else {
				if err := add(root, false); err != nil {
					return nil, err
				}
				// Capture relocated children without traversing large caches.
				for _, child := range []string{"installs", "shims", "command-wrappers", "plugins", "downloads", "trusted-configs", "tracked-configs"} {
					if err := add(filepath.Join(root, child), false); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for _, variable := range []string{"MISE_INSTALLS_DIR", "MISE_INSTALL_STORE_DIR", "MISE_SHIMS_DIR", "MISE_PLUGINS_DIR", "MISE_DOWNLOADS_DIR", "MISE_SYSTEM_DATA_DIR", "MISE_SYSTEM_INSTALLS_DIR", "MISE_SYSTEM_CONFIG_DIR", "MISE_SYSTEM_DIR"} {
		if path := os.Getenv(variable); path != "" {
			if variable == "MISE_SYSTEM_CONFIG_DIR" || variable == "MISE_SYSTEM_DIR" {
				if err := inspectConfig(path); err != nil {
					return nil, err
				}
			} else if err := add(path, false); err != nil {
				return nil, err
			}
		}
	}
	if err := inspectConfig("/etc/mise"); err != nil {
		return nil, err
	}
	if err := add("/usr/local/share/mise", false); err != nil {
		return nil, err
	}
	for _, path := range filepath.SplitList(os.Getenv("MISE_SHARED_INSTALL_DIRS")) {
		if err := add(path, false); err != nil {
			return nil, err
		}
	}
	for _, variable := range []string{"MISE_GLOBAL_CONFIG_FILE", "MISE_CONFIG_FILE", "MISE_SYSTEM_CONFIG_FILE"} {
		if path := os.Getenv(variable); path != "" {
			if err := add(path, true); err != nil {
				return nil, err
			}
		}
	}
	// HOME config also applies to projects below it, outside global config.
	for _, name := range []string{".tool-versions", ".mise.toml", "mise.toml", ".mise.local.toml", "mise.local.toml", ".config/mise.toml", ".config/mise.local.toml"} {
		if err := add(filepath.Join(home, name), true); err != nil {
			return nil, err
		}
	}
	for _, pattern := range []string{".mise.*.toml", "mise.*.toml", ".config/mise.*.toml"} {
		matches, err := filepath.Glob(filepath.Join(home, pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			if err := add(path, true); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range []string{".mise", "mise"} {
		if err := inspectConfig(filepath.Join(home, name)); err != nil {
			return nil, err
		}
	}
	globalRoot := os.Getenv("MISE_GLOBAL_CONFIG_ROOT")
	if globalRoot == "" {
		globalRoot = home
	}
	if !filepath.IsAbs(globalRoot) {
		return nil, invalidConfig{fmt.Errorf("Mise global configuration root must be absolute: %q", globalRoot)}
	}
	// These are Mise's user-global task entry points, not every project's
	// task directories. Do not protect the whole global root (normally HOME).
	for _, name := range []string{"mise-tasks", ".mise-tasks", ".mise/tasks", ".config/mise/tasks", "mise/tasks"} {
		if err := inspectConfig(filepath.Join(globalRoot, name)); err != nil {
			return nil, err
		}
	}
	if name := os.Getenv("MISE_DEFAULT_TOOL_VERSIONS_FILENAME"); name != "" {
		if !filepath.IsAbs(name) {
			name = filepath.Join(home, name)
		}
		if err := add(name, true); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
