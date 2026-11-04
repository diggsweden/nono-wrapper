// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/tailscale/hujson"
)

func TestBundledRuntimeDefaultsValidate(t *testing.T) {
	defaults, err := loadRuntimeDefaults()
	if err != nil || len(defaults.Applications) == 0 {
		t.Fatalf("bundled runtime configuration: %+v, %v", defaults, err)
	}
}

func TestOptionalBypassesDoNotFollowRedirectedFiles(t *testing.T) {
	// Some hosts put the system temp directory behind a symlink. Start from
	// its canonical spelling so each case controls its own redirects.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	secret := filepath.Join(home, "private-key")
	if err := os.WriteFile(secret, []byte("dummy secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"regular", "symlink", "parent symlink", "missing", "directory"} {
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(home, mode)
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, ".npmrc")
			switch mode {
			case "regular":
				if err := os.WriteFile(file, []byte("ordinary config"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(secret, file); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				if err := os.Symlink(home, filepath.Join(root, "alias")); err != nil {
					t.Fatal(err)
				}
				file = filepath.Join(root, "alias", "private-key")
			case "directory":
				if err := os.Mkdir(file, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			paths, err := configuredBypassPaths(runtimeDefaults{OptionalBypasses: []runtimePath{{Path: file}}})
			if err != nil {
				t.Fatal(err)
			}
			if (len(paths) == 1) != (mode == "regular") {
				t.Fatalf("unexpected bypasses for %s: %q", mode, paths)
			}
		})
	}
}

func TestRuntimeDescriptionsAreOptionalAndDoNotChangePaths(t *testing.T) {
	bare := `{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","filesystem":{"read":["bin"]}}]}`
	documented := `{"version":1,"description":"policy rationale","applications":[{"variable":"APP_HOME","default_root":"$HOME/app","description":"installed tools only","filesystem":{"read":[{"path":"bin","description":"run installed tools without write access"}]}}]}`
	first, err := parseRuntimeDefaults([]byte(bare))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseRuntimeDefaults([]byte(documented))
	if err != nil {
		t.Fatal(err)
	}
	a, b := first.Applications[0].Filesystem["read"][0], second.Applications[0].Filesystem["read"][0]
	if a.Path != b.Path || a.Description != "" || b.Description == "" || second.Description == "" {
		t.Fatalf("description changed a grant or was not preserved: %+v, %+v", a, b)
	}
}

func TestRuntimeDefaultsRejectUnsafeDeclarations(t *testing.T) {
	for _, source := range []string{
		`{"version":2}`,
		`{"version":1,"public_key_files":["$HOME/.ssh/*"]}`,
		`{"version":1,"public_key_files":["$HOME/*/*.pub"]}`,
		`{"version":1,"public_key_files":["$HOME/.ssh/[.pub"]}`,
		`{"version":1,"unknown":true}`,
		`{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","filesystem":{"typo":["path"]}}]}`,
		`{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","filesystem":{"read":["../credentials"]}}]}`,
		`{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","filesystem":{"read":["/absolute"]}}]}`,
		`{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","filesystem":{"read":["$HOME"]}}]}`,
		`{"version":1,"applications":[{"variable":"APP_HOME","default_root":"$HOME/app","prepare":["config"],"filesystem":{"read":["config"]}}]}`,
	} {
		if _, err := parseRuntimeDefaults([]byte(source)); err == nil {
			t.Errorf("invalid runtime configuration accepted: %s", source)
		}
	}
}

func TestRuntimeDeclarationsMatchNativePolicy(t *testing.T) {
	defaults, err := loadRuntimeDefaults()
	if err != nil {
		t.Fatal(err)
	}
	_, source, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "../../profiles/agent-policy.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	data, err = hujson.Standardize(data)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Filesystem  map[string][]string `json:"filesystem"`
		Environment struct {
			Allow []string `json:"allow_vars"`
		} `json:"environment"`
		Platforms map[string]struct {
			Filesystem map[string][]string `json:"filesystem"`
		} `json:"platform_overrides"`
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	for _, app := range defaults.Applications {
		if !slices.Contains(profile.Environment.Allow, app.Variable) {
			t.Errorf("runtime variable is filtered out of the child: %s", app.Variable)
		}
		for field, paths := range app.Filesystem {
			for _, rule := range paths {
				grant := path.Join(app.DefaultRoot, rule.Path)
				if !slices.Contains(profile.Filesystem[field], grant) && !slices.Contains(profile.Platforms[app.Platform].Filesystem[field], grant) {
					t.Errorf("runtime declaration has no matching native default: %s %s", field, grant)
				}
			}
		}
	}
	for key := range defaults.Environment {
		if !slices.Contains(profile.Environment.Allow, key) {
			t.Errorf("environment default is filtered out of the child: %s", key)
		}
	}
	for _, rule := range defaults.OptionalBypasses {
		if !slices.Contains(profile.Filesystem["read_file"], rule.Path) {
			t.Errorf("optional bypass must be paired with an exact native read-file grant: %s", rule.Path)
		}
	}
	if !slices.Contains(profile.Platforms[defaults.Podman.Platform].Filesystem["unix_socket"], defaults.Podman.Socket) {
		t.Fatal("Podman discovery endpoint differs from the declared socket grant")
	}
}
