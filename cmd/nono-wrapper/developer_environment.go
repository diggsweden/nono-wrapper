// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"runtime"
	"sort"
)

// Preserve explicit policy, including stricter release-age settings. These
// environment conveniences are not a boundary against project-controlled code.
func developerSessionEnvironment(profile string) ([]string, error) {
	if !bundledProfile(profile) {
		return nil, nil
	}
	defaults, err := loadRuntimeDefaults()
	if err != nil {
		return nil, err
	}
	var environment []string
	keys := make([]string, 0, len(defaults.Environment))
	for key := range defaults.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, set := os.LookupEnv(key); !set {
			environment = append(environment, key+"="+os.ExpandEnv(defaults.Environment[key]))
		}
	}
	for _, app := range defaults.Applications {
		if app.Platform != "" && app.Platform != runtime.GOOS {
			continue
		}
		if app.SetEnvironmentDefault {
			// Match runtime-root resolution: empty path overrides use defaults.
			if os.Getenv(app.Variable) == "" {
				root, err := expandPolicyPath(app.DefaultRoot)
				if err != nil {
					return nil, err
				}
				environment = append(environment, app.Variable+"="+root)
			}
		}
	}
	return append(environment, podmanSessionEnvironment(profile, defaults)...), nil
}
