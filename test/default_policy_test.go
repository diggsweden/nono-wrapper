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

// Keep common developer-tool grants narrow across the shipped defaults.
func TestJustRuntimeGrantIsLinuxOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles", "agent-policy.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	data, err = hujson.Standardize(data)
	if err != nil {
		t.Fatal(err)
	}
	type filesystem struct {
		Allow                 []string `json:"allow"`
		UnixSocket            []string `json:"unix_socket"`
		UnixSocketDir         []string `json:"unix_socket_dir"`
		UnixSocketSubtree     []string `json:"unix_socket_subtree"`
		UnixSocketSubtreeBind []string `json:"unix_socket_subtree_bind"`
	}
	var profile struct {
		Filesystem filesystem `json:"filesystem"`
		Platforms  map[string]struct {
			Filesystem filesystem `json:"filesystem"`
		} `json:"platform_overrides"`
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	const grant = "/run/user/$UID/just"
	if got := profile.Platforms["linux"].Filesystem.Allow; !slices.Equal(got, []string{grant, "/dev/shm"}) {
		t.Fatalf("Linux runtime grant must be narrowly scoped: %v", got)
	}
	const podmanSocket = "/run/user/$UID/podman/podman.sock"
	linux := profile.Platforms["linux"].Filesystem
	if !slices.Equal(linux.UnixSocketSubtreeBind, []string{"$HOME/.cache/opencode/tmp", "$XDG_CACHE_HOME/opencode/tmp"}) {
		t.Fatalf("temporary sockets must stay in exact private roots: %v", linux.UnixSocketSubtreeBind)
	}
	wantSockets := []string{podmanSocket, "/run/nscd/socket", "/var/run/nscd/socket"}
	if !slices.Equal(linux.UnixSocket, wantSockets) || len(linux.UnixSocketDir) != 0 || len(linux.UnixSocketSubtree) != 0 {
		t.Fatalf("Linux must grant only the exact Podman/nscd connect sockets: %+v", linux)
	}
	if len(profile.Filesystem.UnixSocket) != 0 {
		t.Fatal("Linux socket defaults must not apply globally")
	}
	if slices.Contains(profile.Filesystem.Allow, grant) {
		t.Fatal("Just's Linux runtime grant must not apply to other platforms")
	}
	for platform, override := range profile.Platforms {
		if platform != "linux" && len(override.Filesystem.UnixSocketSubtreeBind) != 0 {
			t.Errorf("Linux temporary socket grants apply to %s", platform)
		}
		if platform != "linux" && slices.Contains(override.Filesystem.Allow, grant) {
			t.Errorf("Just's Linux runtime grant also applies to %s", platform)
		}
		if platform != "linux" && slices.Contains(override.Filesystem.Allow, "/dev/shm") {
			t.Errorf("Linux shared-memory grant also applies to %s", platform)
		}
		if platform != "linux" && slices.Contains(override.Filesystem.UnixSocket, podmanSocket) {
			t.Errorf("Podman's Linux runtime grant also applies to %s", platform)
		}
		for _, socket := range []string{"/run/nscd/socket", "/var/run/nscd/socket"} {
			if platform != "linux" && slices.Contains(override.Filesystem.UnixSocket, socket) {
				t.Errorf("nscd's Linux socket grant also applies to %s", platform)
			}
		}
	}
}

func TestDevBaseDefaultsKeepCodeReadOnly(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(testRepository(t), "profiles/agent-policy.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	data, err = hujson.Standardize(data)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Groups struct {
			Exclude []string `json:"exclude"`
		} `json:"groups"`
		Environment struct {
			Allow []string          `json:"allow_vars"`
			Set   map[string]string `json:"set_vars"`
		} `json:"environment"`
		Filesystem map[string][]string `json:"filesystem"`
		Platforms  map[string]struct {
			Filesystem map[string][]string `json:"filesystem"`
		} `json:"platform_overrides"`
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"$XDG_CONFIG_HOME/devtools/formatting", "$XDG_CONFIG_HOME/devtools/linting", "$XDG_DATA_HOME/devbase-check"} {
		if !slices.Contains(profile.Filesystem["read"], path) || slices.Contains(profile.Filesystem["allow"], path) {
			t.Errorf("shared code/config must be readable, not writable: %s", path)
		}
	}
	linux := profile.Platforms["linux"].Filesystem
	for _, path := range []string{"/usr/libexec/gcc", "/usr/include", "/usr/local/include"} {
		if !slices.Contains(linux["read"], path) || slices.Contains(linux["allow"], path) {
			t.Errorf("compiler inputs must be readable, not writable: %s", path)
		}
	}
	for _, path := range []string{"/etc/dpkg/origins", "/etc/dpkg/dpkg.cfg.d"} {
		if !slices.Contains(linux["read"], path) || slices.Contains(linux["read"], "/etc/dpkg") {
			t.Fatal("dpkg reads must stay in the public origins and config-snippet directories")
		}
		for _, field := range []string{"allow", "write", "allow_file", "write_file"} {
			if slices.Contains(linux[field], path) || slices.Contains(linux[field], "/etc/dpkg") {
				t.Fatalf("dpkg metadata must not be writable: %s %s", field, path)
			}
		}
	}
	if !slices.Contains(linux["read_file"], "/etc/dpkg/dpkg.cfg") {
		t.Fatal("missing exact public dpkg configuration file")
	}
	if !slices.Contains(profile.Filesystem["allow"], "$HOME/.gradle/wrapper/dists") || slices.Contains(profile.Filesystem["allow"], "$HOME/.gradle") || slices.Contains(profile.Filesystem["allow"], "$HOME/.gradle/wrapper") {
		t.Fatal("Gradle wrapper writes must stay within wrapper/dists")
	}
	for _, path := range []string{"$HOME/.local/share/man", "$XDG_DATA_HOME/man", "$HOME/.cache/itiquette-test-vm", "$XDG_CACHE_HOME/itiquette-test-vm"} {
		if !slices.Contains(profile.Filesystem["allow"], path) {
			t.Errorf("missing scoped user-data write: %s", path)
		}
	}
	if !slices.Equal(linux["allow_file"], []string{"/dev/ptmx"}) {
		t.Fatalf("PTY grant must be an exact device: %v", linux["allow_file"])
	}
	for _, key := range []string{"RIPGREP_CONFIG_PATH", "NODE_USE_SYSTEM_CA", "NODE_EXTRA_CA_CERTS", "NPM_CONFIG_MIN_RELEASE_AGE", "PIPX_COOLDOWN", "DOCKER_HOST"} {
		if !slices.Contains(profile.Environment.Allow, key) {
			t.Errorf("missing safe environment setting: %s", key)
		}
	}
	for key, want := range map[string]string{"GOTOOLCHAIN": "local", "DEVBASE_CHECK_SKIP_UPDATES": "1"} {
		if profile.Environment.Set[key] != want {
			t.Errorf("%s = %q, want %q", key, profile.Environment.Set[key], want)
		}
	}
	if _, set := profile.Environment.Set["NPM_CONFIG_MIN_RELEASE_AGE"]; set {
		t.Fatal("profile must not override a stricter inherited npm release-age setting")
	}
	for _, group := range []string{"rust_runtime", "python_runtime"} {
		if !slices.Contains(profile.Groups.Exclude, group) {
			t.Errorf("broad upstream credential-bearing group restored: %s", group)
		}
	}
	for _, root := range []string{"$HOME/.cargo", "$HOME/.local/share/uv", "$XDG_DATA_HOME/uv"} {
		for _, field := range []string{"read", "allow", "write"} {
			if slices.Contains(profile.Filesystem[field], root) {
				t.Errorf("whole credential-bearing runtime root granted in %s: %s", field, root)
			}
		}
	}
	if !slices.Contains(linux["read_file"], "/etc/debian_version") {
		t.Fatal("missing exact distro metadata read for pip")
	}
	for _, path := range []string{"$HOME/.local/state/mise/trusted-configs", "$XDG_STATE_HOME/mise/trusted-configs"} {
		if !slices.Contains(profile.Filesystem["read"], path) {
			t.Errorf("missing Mise trust read: %s", path)
		}
	}
	for _, cache := range []string{"mise", "golangci-lint", "staticcheck", "trivy", "opengrep"} {
		for _, root := range []string{"$HOME/.cache/", "$XDG_CACHE_HOME/"} {
			if !slices.Contains(profile.Filesystem["allow"], root+cache) {
				t.Errorf("missing writable cache: %s%s", root, cache)
			}
		}
	}
	for _, root := range []string{"$HOME/.local/share/", "$XDG_DATA_HOME/"} {
		if !slices.Contains(profile.Filesystem["read"], root+"nanolinter") {
			t.Errorf("missing read-only Nanolinter data: %s", root)
		}
		for _, field := range []string{"allow", "write", "allow_file", "write_file"} {
			for _, path := range profile.Filesystem[field] {
				if path == root+"nanolinter" || strings.HasPrefix(path, root+"nanolinter/") {
					t.Errorf("Nanolinter data must not be writable: %s %s", field, path)
				}
			}
		}
	}
	if slices.Contains(profile.Filesystem["allow"], "$HOME/.cache") || slices.Contains(profile.Filesystem["allow"], "$XDG_CACHE_HOME") {
		t.Error("whole cache parent was granted")
	}
	if !slices.Contains(profile.Filesystem["read_file"], "$XDG_CONFIG_HOME/containers/registries.conf") {
		t.Error("missing exact registry config read")
	}
	whdkit := map[string]string{
		"$HOME/.config/whdkit": "read", "$XDG_CONFIG_HOME/whdkit": "read",
		"$HOME/.local/share/whdkit": "allow", "$XDG_DATA_HOME/whdkit": "allow",
		"$HOME/.local/state/whdkit": "allow", "$XDG_STATE_HOME/whdkit": "allow",
	}
	for path, field := range whdkit {
		if !slices.Contains(profile.Filesystem[field], path) {
			t.Errorf("missing Whdkit %s grant: %s", field, path)
		}
	}
	for field, paths := range profile.Filesystem {
		for _, path := range paths {
			if strings.Contains(path, "/whdkit") && whdkit[path] != field {
				t.Errorf("unexpected Whdkit grant: %s %s", field, path)
			}
		}
	}
	for _, filesystem := range []map[string][]string{profile.Filesystem, linux} {
		for field, paths := range filesystem {
			for _, path := range paths {
				if path == "/" || (field == "allow" || field == "write" || field == "allow_file" || field == "write_file") && (strings.HasPrefix(path, "/proc/") || strings.Contains(path, "mise/trusted-configs")) {
					t.Errorf("overbroad default grant: %s %s", field, path)
				}
			}
		}
	}
}
