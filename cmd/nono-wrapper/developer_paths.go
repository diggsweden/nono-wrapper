// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// Runtime roots, subpaths and access modes come from runtime-defaults.jsonc.
// Go supplies path resolution, validation and protected-input checks only.
func prepareDeveloperPaths(protected []pathRule) ([]pathRule, error) {
	defaults, err := loadRuntimeDefaults()
	if err != nil {
		return nil, err
	}
	var rules []pathRule
	for _, app := range defaults.Applications {
		if app.Platform != "" && app.Platform != runtime.GOOS {
			continue
		}
		fallback, err := expandPolicyPath(app.DefaultRoot)
		if err != nil {
			return nil, err
		}
		root := os.Getenv(app.Variable)
		if root == "" {
			root = fallback
		}
		if app.FirstPath {
			paths := filepath.SplitList(root)
			root = fallback
			for _, path := range paths {
				if path != "" {
					root = path
					break
				}
			}
		}
		if !filepath.IsAbs(root) {
			return nil, invalidConfig{fmt.Errorf("%s must be absolute", app.Variable)}
		}
		if app.RequireExistingRoot {
			info, err := os.Stat(root)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !info.IsDir() {
				return nil, invalidConfig{fmt.Errorf("%s must be a directory", app.Variable)}
			}
		}
		prepared := map[string]bool{}
		for _, path := range app.Prepare {
			prepared[path] = true
		}
		for _, field := range filesystemFields {
			for _, declared := range app.Filesystem[field.name] {
				relative := declared.Path
				if root == fallback && !prepared[relative] {
					continue // The native profile already supplies default reads.
				}
				path := filepath.Join(root, relative)
				actual, err := literalRuntimePath(path)
				if err != nil {
					return nil, err
				}
				rule := pathRule{actual, field.access, field.file}
				if field.access&pathWrite != 0 {
					if err := protectRuleWrite(actual); err != nil {
						return nil, err
					}
					if err := checkProtectedWrites([]pathRule{rule}, protected); err != nil {
						return nil, err
					}
				}
				if prepared[relative] {
					var err error
					if field.file {
						err = prepareRuntimeFile(path)
					} else {
						err = os.MkdirAll(path, 0o700)
					}
					if err != nil {
						return nil, fmt.Errorf("prepare developer state: %w", quotePathError(err))
					}
					if app.RejectPreparedSymlinks {
						info, err := os.Lstat(path)
						if err != nil {
							return nil, err
						}
						if !info.IsDir() {
							return nil, invalidConfig{fmt.Errorf("runtime directory must not be a file or symlink: %s", path)}
						}
					}
				}
				if root != fallback {
					rules = append(rules, rule)
				}
			}
		}
	}
	return rules, nil
}

// Create a declared writable file without truncating an existing log or
// following an alias into credentials or code. No content is written here.
func prepareRuntimeFile(path string) error {
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return invalidConfig{fmt.Errorf("prepared file must be regular and not a symlink: %q", path)}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		return invalidConfig{fmt.Errorf("prepared file must be regular with a single link: %q", path)}
	}
	return nil
}
