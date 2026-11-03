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

func configContents(t *testing.T, path string) map[string]map[string][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err = hujson.Standardize(data)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]map[string][]string
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestConfigHostAndPathEditing(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles", agent+"-user.jsonc")
			run := func(args ...string) string {
				t.Helper()
				stdout, stderr, status := runLauncher(t, f, "", append([]string{"config", agent}, args...), nil)
				if status != 0 || stderr != "" {
					t.Fatalf("%q: status=%d, stdout=%q, stderr=%q", args, status, stdout, stderr)
				}
				return string(stdout)
			}
			if output := run("show"); !strings.Contains(output, "(none)") {
				t.Fatalf("empty configuration: %s", output)
			}
			run("remove-domain", "absent.example")
			if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
				t.Fatalf("read/no-op created a configuration directory: %v", err)
			}
			run("allow-domain", "API.GITHUB.COM.")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("new user configuration should be owner-only: %v", err)
			}
			if output := run("allow-domain", "api.github.com"); !strings.Contains(output, "No change") {
				t.Fatalf("duplicate addition was not a no-op: %s", output)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(data) != string(after) {
				t.Fatalf("duplicate addition rewrote the file: %v", err)
			}
			directory := filepath.Join(f.project, "reference with spaces")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(f.project, "single file")
			writeLauncherFixture(t, file, "example\n")
			run("add-dir", "reference with spaces", "--read-only")
			run("add-file", file)
			document := configContents(t, path)
			if !slices.Equal(document["network"]["allow_domain"], []string{"api.github.com"}) ||
				!slices.Equal(document["filesystem"]["read"], []string{directory}) ||
				!slices.Equal(document["filesystem"]["read_file"], []string{file}) {
				t.Fatalf("wrong user allowances: %#v", document)
			}
			run("add-dir", "--read-write", directory)
			run("add-file", file, "--read-write")
			document = configContents(t, path)
			if len(document["filesystem"]["read"]) != 0 || len(document["filesystem"]["read_file"]) != 0 ||
				!slices.Equal(document["filesystem"]["allow"], []string{directory}) ||
				!slices.Equal(document["filesystem"]["allow_file"], []string{file}) {
				t.Fatalf("updating access left the old allowances: %#v", document)
			}
			if output := run("show"); !strings.Contains(output, "api.github.com") || !strings.Contains(output, "directory (read/write)") || !strings.Contains(output, "file (read/write)") {
				t.Fatalf("missing readable listing: %s", output)
			}
			// Adding read-only also supports downgrading a user read/write grant.
			run("add-dir", directory)
			document = configContents(t, path)
			if len(document["filesystem"]["allow"]) != 0 || !slices.Equal(document["filesystem"]["read"], []string{directory}) {
				t.Fatalf("read-only update retained write access: %#v", document)
			}
			alias := filepath.Join(f.project, "reference-link")
			if err := os.Symlink(directory, alias); err != nil {
				t.Fatal(err)
			}
			run("add-dir", "--read-write", alias)
			document = configContents(t, path)
			if len(document["filesystem"]["read"]) != 0 || !slices.Equal(document["filesystem"]["allow"], []string{alias}) {
				t.Fatalf("path alias left conflicting user access modes: %#v", document)
			}
			run("add-dir", directory)
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			run("remove-file", file) // Deleted files must still be removable from policy.
			run("remove-dir", directory)
			if output := run("remove-domain", "api.github.com"); !strings.Contains(output, "inherited or broader grants") {
				t.Fatalf("removal overstates access revocation: %s", output)
			}
			if output := run("show"); !strings.Contains(output, "(none)") {
				t.Fatalf("allowances remain after removal: %s", output)
			}
			// No temporary files or locks should remain after any edit.
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 || entries[0].Name() != agent+"-user.jsonc" {
				t.Fatalf("configuration edit left residue: %v (%v)", entries, err)
			}
		})
	}
}

func TestConfigFileAllowancePinsSymlinkTarget(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			home := filepath.Dir(f.bin)
			first := filepath.Join(home, "instructions/project-a/AGENTS.md")
			second := filepath.Join(home, "instructions/project-b/AGENTS.md")
			alias := filepath.Join(home, "state/AGENTS.md")
			profile := filepath.Join(home, ".config/nono/profiles", agent+"-user.jsonc")
			for _, path := range []string{first, second, alias} {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			writeLauncherFixture(t, first, "First instructions.\n")
			writeLauncherFixture(t, second, "Other instructions.\n")
			if err := os.Symlink(first, alias); err != nil {
				t.Fatal(err)
			}
			run := func(args ...string) string {
				t.Helper()
				stdout, stderr, status := runLauncher(t, f, "", append([]string{"config", agent}, args...), nil)
				if status != 0 || stderr != "" {
					t.Fatalf("%q: status=%d stdout=%q stderr=%q", args, status, stdout, stderr)
				}
				return string(stdout)
			}
			output := run("add-file", alias)
			if !strings.Contains(output, first) || !strings.Contains(output, alias) || !strings.Contains(output, "read-only") {
				t.Fatalf("missing target, source or access mode: %s", output)
			}
			document := configContents(t, profile)
			if !slices.Equal(document["filesystem"]["read_file"], []string{first}) || len(document["filesystem"]["allow_file"]) != 0 {
				t.Fatalf("file allowance was not pinned read-only: %#v", document)
			}
			if output := run("add-file", alias); !strings.Contains(output, "No change") {
				t.Fatalf("repeating the pinned allowance was not a no-op: %s", output)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(second, alias); err != nil {
				t.Fatal(err)
			}
			if output := run("show"); !strings.Contains(output, first) || strings.Contains(output, second) || strings.Contains(output, alias) {
				t.Fatalf("repointing the link changed the displayed allowance: %s", output)
			}
			run("remove-file", first)
			if document := configContents(t, profile); len(document["filesystem"]["read_file"]) != 0 {
				t.Fatalf("could not remove the saved target after repointing: %#v", document)
			}
		})
	}
}

func TestConfigRejectsInterpretedFileTarget(t *testing.T) {
	f := newLauncherFixture(t, "codex")
	home := filepath.Dir(f.bin)
	target := filepath.Join(home, "literal-$HOME/AGENTS.md")
	alias := filepath.Join(f.project, "AGENTS.md")
	profile := filepath.Join(home, ".config/nono/profiles/codex-user.jsonc")
	for _, path := range []string{target, profile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeLauncherFixture(t, target, "Generic instructions.\n")
	writeLauncherFixture(t, profile, "{}\n")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	_, stderr, status := runLauncher(t, f, "", []string{"config", "codex", "add-file", alias}, nil)
	if status != 65 || !strings.Contains(stderr, "profile variable references") {
		t.Fatalf("accepted a file target that nono would reinterpret: status=%d stderr=%q", status, stderr)
	}
	if data, err := os.ReadFile(profile); err != nil || string(data) != "{}\n" {
		t.Fatalf("rejected file target changed the profile: %q (%v)", data, err)
	}
}

func TestConfigPreservesManualJSONC(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	home := filepath.Dir(f.bin)
	config := filepath.Join(home, "xdg config")
	path := filepath.Join(config, "nono/profiles/opencode-user.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = `// Keep this explanation.
{
  "environment": { "allow_vars": ["EDITOR"], },
  "network": {
    "allow_domain": [
      // Keep this endpoint restriction.
      { "domain": "api.github.com", "endpoints": [{ "method": "GET", "path": "/user" }] },
      { "domain": "api.github.com", "endpoints": [{ "method": "GET", "path": "/repos/**" }] },
    ],
  },
  "filesystem": { "read": ["$HOME/docs"], "write": ["$HOME/docs"] },
}
`
	writeLauncherFixture(t, path, original)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		_, stderr, status := runLauncher(t, f, "", append([]string{"config", "opencode"}, args...), []string{"XDG_CONFIG_HOME=" + config})
		if status != 0 {
			t.Fatalf("%q: status=%d, stderr=%s", args, status, stderr)
		}
	}
	run("allow-domain", "another.example")
	data, _ := os.ReadFile(path)
	for _, text := range []string{"Keep this explanation.", "Keep this endpoint restriction.", "EDITOR", "/user", "/repos/**", "GET", "another.example"} {
		if !strings.Contains(string(data), text) {
			t.Fatalf("lost manual configuration %q: %s", text, data)
		}
	}
	run("allow-domain", "--method", "GET", "https://api.github.com/user")
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatalf("repeating an endpoint across manual entries rewrote the profile: %s", after)
	}
	run("remove-domain", "api.github.com")
	run("allow-domain", "api.github.com")
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "endpoints") || !strings.Contains(string(data), `"api.github.com"`) || !strings.Contains(string(data), "EDITOR") {
		t.Fatalf("explicit remove and allow did not replace its endpoint rules: %s", data)
	}
	run("remove-dir", filepath.Join(home, "docs"))
	run("remove-domain", "api.github.com")
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "$HOME/docs") || strings.Contains(string(data), "api.github.com") || !strings.Contains(string(data), "another.example") {
		t.Fatalf("failed to remove matching manual grants: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("changed existing configuration permissions: %v", err)
	}
}

func TestConfigChangesRequireNormalTerminal(t *testing.T) {
	for _, variable := range []string{"NONO_SESSION_ID=session", "NONO_CAP_FILE=/sandbox/capabilities.json"} {
		f := newLauncherFixture(t, "opencode")
		_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "allow-domain", "api.github.com"}, []string{variable})
		if status == 0 || !strings.Contains(stderr, "normal terminal outside nono") {
			t.Fatalf("agent was permitted to edit configuration: status=%d, stderr=%s", status, stderr)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(f.bin), ".config")); !os.IsNotExist(err) {
			t.Fatalf("agent edit attempt created configuration state: %v", err)
		}
	}
}

func TestConfigRefusesWriteAccessToRules(t *testing.T) {
	for _, mode := range []string{"config parent", "profiles", "packages", "profile file", "symlink", "hard link", "profile backing directory", "xdg"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			home := filepath.Dir(f.bin)
			config := filepath.Join(home, ".config")
			var env []string
			if mode == "xdg" {
				config = filepath.Join(home, "xdg config")
				env = []string{"XDG_CONFIG_HOME=" + config}
			}
			profiles := filepath.Join(config, "nono/profiles")
			if err := os.MkdirAll(profiles, 0o700); err != nil {
				t.Fatal(err)
			}
			path, command := config, "add-dir"
			switch mode {
			case "profiles":
				path = profiles
			case "packages":
				path = filepath.Join(config, "nono/packages")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "profile file":
				path, command = filepath.Join(profiles, "opencode-chatgpt.jsonc"), "add-file"
				writeLauncherFixture(t, path, "{}\n")
			case "symlink":
				path = filepath.Join(home, "alias")
				if err := os.Symlink(profiles, path); err != nil {
					t.Fatal(err)
				}
			case "hard link":
				original := filepath.Join(profiles, "opencode-chatgpt.jsonc")
				writeLauncherFixture(t, original, "{}\n")
				path, command = filepath.Join(home, "profile-alias"), "add-file"
				if err := os.Link(original, path); err != nil {
					t.Fatal(err)
				}
			case "profile backing directory":
				path = filepath.Join(home, "dotfiles")
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				original := filepath.Join(path, "profile.jsonc")
				writeLauncherFixture(t, original, "{}\n")
				if err := os.Symlink(original, filepath.Join(profiles, "opencode-chatgpt.jsonc")); err != nil {
					t.Fatal(err)
				}
			}
			_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", command, "--read-write", path}, env)
			if status == 0 || !strings.Contains(stderr, "active rules") {
				t.Fatalf("accepted policy-writing grant: status=%d, stderr=%s", status, stderr)
			}
			if _, err := os.Stat(filepath.Join(profiles, "opencode-user.jsonc")); !os.IsNotExist(err) {
				t.Fatalf("rejected grant created a user profile: %v", err)
			}
		})
	}
}

func TestConfigRefusesCaseAliasOfRules(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	root := filepath.Join(filepath.Dir(f.bin), ".config/nono")
	profiles := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "PROFILES")
	info, err := os.Stat(alias)
	if os.IsNotExist(err) {
		t.Skip("requires a case-insensitive filesystem")
	}
	if err != nil || !info.IsDir() {
		t.Fatalf("inspect case alias: %v", err)
	}
	_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-dir", "--read-write", alias}, nil)
	if status == 0 || !strings.Contains(stderr, "active rules") {
		t.Fatalf("case alias bypassed rule protection: status=%d stderr=%q", status, stderr)
	}
}

func TestConfigErrorsPreserveExistingFile(t *testing.T) {
	for _, test := range []struct {
		name, contents string
		args, env      []string
	}{
		{"invalid JSONC", "{broken", []string{"allow-domain", "api.github.com"}, nil},
		{"non-object", "[]", []string{"allow-domain", "api.github.com"}, nil},
		{"duplicate key", `{"network":{},"network":{}}`, []string{"allow-domain", "api.github.com"}, nil},
		{"invalid section", `{"network":[]}`, []string{"allow-domain", "api.github.com"}, nil},
		{"invalid list", `{"network":{"allow_domain":false}}`, []string{"allow-domain", "api.github.com"}, nil},
		{"invalid host entry", `{"network":{"allow_domain":[null]}}`, []string{"allow-domain", "api.github.com"}, nil},
		{"plain HTTP cannot be narrowed", "{}", []string{"allow-domain", "http://api.github.com/path"}, nil},
		{"URL query", "{}", []string{"allow-domain", "https://api.github.com/path?private=1"}, nil},
		{"URL query without a path", "{}", []string{"allow-domain", "https://api.github.com?private=1"}, nil},
		{"URL user name", "{}", []string{"allow-domain", "https://user@api.github.com/path"}, nil},
		{"domain port", "{}", []string{"allow-domain", "api.github.com:8443"}, nil},
		{"HTTPS default port", "{}", []string{"allow-domain", "https://api.github.com:443/x"}, nil},
		{"HTTPS non-default port", "{}", []string{"allow-domain", "https://api.github.com:8443/x"}, nil},
		{"empty domain port", "{}", []string{"allow-domain", "api.github.com:"}, nil},
		{"bare port", "{}", []string{"allow-domain", "443"}, nil},
		{"single-label name", "{}", []string{"allow-domain", "localhost"}, nil},
		{"IPv4", "{}", []string{"allow-domain", "192.0.2.1"}, nil},
		{"IPv6", "{}", []string{"allow-domain", "[2001:db8::1]"}, nil},
		{"numeric TLD", "{}", []string{"allow-domain", "example.123"}, nil},
		{"bare wildcard", "{}", []string{"allow-domain", "*"}, nil},
		{"embedded wildcard", "{}", []string{"allow-domain", "api.*.example.com"}, nil},
		{"partial wildcard", "{}", []string{"allow-domain", "*example.com"}, nil},
		{"wildcard endpoint", "{}", []string{"allow-domain", "https://*.example.com/api/**"}, nil},
		{"URL fragment", "{}", []string{"allow-domain", "https://api.github.com/x#section"}, nil},
		{"empty URL fragment", "{}", []string{"allow-domain", "https://api.github.com/x#"}, nil},
		{"method without a path", "{}", []string{"allow-domain", "--method", "GET", "api.github.com"}, nil},
		{"invalid method", "{}", []string{"allow-domain", "--method", "G3T", "https://api.github.com/x"}, nil},
		{"empty method", "{}", []string{"allow-domain", "--method", "", "https://api.github.com/x"}, nil},
		{"path flag on a port", "{}", []string{"allow-tcp-port", "--read-write", "8081"}, nil},
		{"fractional port entry", `{"network":{"open_port":[8081.5]}}`, []string{"allow-tcp-port", "8081"}, nil},
		{"port zero", "{}", []string{"allow-tcp-port", "0"}, nil},
		{"port too high", "{}", []string{"allow-tcp-port", "65536"}, nil},
		{"port range", "{}", []string{"allow-tcp-port", "3000-3010"}, nil},
		{"UDP port", "{}", []string{"allow-tcp-port", "3000/udp"}, nil},
		{"port name", "{}", []string{"allow-tcp-port", "http"}, nil},
		{"invalid port entry", `{"network":{"open_port":["8081"]}}`, []string{"allow-tcp-port", "8081"}, nil},
		{"endpoint would replace whole domain", `{"network":{"allow_domain":["api.github.com"]}}`, []string{"allow-domain", "https://api.github.com/x"}, nil},
		{"endpoint would replace empty endpoint list", `{"network":{"allow_domain":[{"domain":"api.github.com","endpoints":[]}]}}`, []string{"allow-domain", "https://api.github.com/x"}, nil},
		{"domain would replace endpoint", `{"network":{"allow_domain":[{"domain":"API.GITHUB.COM.","endpoints":[{"method":"GET","path":"/x"}]}]}}`, []string{"allow-domain", "api.github.com"}, nil},
		{"mixed duplicate scopes", `{"network":{"allow_domain":["api.github.com",{"domain":"api.github.com","endpoints":[{"method":"GET","path":"/x"}]}]}}`, []string{"allow-domain", "api.github.com"}, nil},
		{"missing path", "{}", []string{"add-dir", "missing-directory"}, nil},
		{"empty path", "{}", []string{"add-dir", ""}, nil},
		{"conflicting modes", "{}", []string{"add-dir", ".", "--read-only", "--read-write"}, nil},
		{"directory as file", "{}", []string{"add-file", "."}, nil},
		{"extra argument", "{}", []string{"remove-domain", "one.example", "two.example"}, nil},
		{"validation failure", "{}", []string{"allow-domain", "api.github.com"}, []string{"LAUNCHER_VALIDATE_FAIL=1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles/opencode-user.jsonc")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, path, test.contents)
			_, stderr, status := runLauncher(t, f, "", append([]string{"config", "opencode"}, test.args...), test.env)
			if status == 0 || stderr == "" {
				t.Fatalf("accepted invalid edit: status=%d, stderr=%s", status, stderr)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != test.contents {
				t.Fatalf("failed edit changed the file: %q (%v)", data, err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("failed edit left residue: %v (%v)", entries, err)
			}
		})
	}
}

func TestConfigSymlinkAndLock(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	home := filepath.Dir(f.bin)
	path := filepath.Join(home, ".config/nono/profiles/opencode-user.jsonc")
	target := filepath.Join(home, "dotfile.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, target, "{}\n")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "allow-domain", "api.github.com"}, nil)
	if status != 0 {
		t.Fatalf("symlink edit failed: %s", stderr)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("replaced the user's dotfile symlink: %v", err)
	}
	before, _ := os.ReadFile(target)
	writeLauncherFixture(t, target+".lock", "other editor\n")
	_, stderr, status = runLauncher(t, f, "", []string{"config", "opencode", "allow-domain", "another.example"}, nil)
	if status == 0 || !strings.Contains(stderr, "lock user configuration") {
		t.Fatalf("ignored another writer: status=%d, stderr=%s", status, stderr)
	}
	after, _ := os.ReadFile(target)
	lock, _ := os.ReadFile(target + ".lock")
	if string(before) != string(after) || string(lock) != "other editor\n" {
		t.Fatal("changed another editor's file or lock")
	}
}

func TestConfigValidationCannotClobberAnEdit(t *testing.T) {
	for _, mode := range []string{"concurrent edit", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles/opencode-user.jsonc")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			const original = "{}\n"
			writeLauncherFixture(t, path, original)
			writeNonoFixture(t, f, `
[ "$1 $2" = 'profile validate' ] || exit 99
if [ "$CONFIG_TEST_MODE" = cancelled ]; then
  kill -INT "$PPID"
  exec /bin/sleep 5
fi
printf '// Another editor saved this.\n{}\n' >"$CONFIG_TEST_PATH"
`)
			_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "allow-domain", "api.github.com"}, []string{
				"CONFIG_TEST_MODE=" + mode, "CONFIG_TEST_PATH=" + path,
			})
			if status == 0 || stderr == "" {
				t.Fatalf("unsafe edit succeeded: status=%d, stderr=%s", status, stderr)
			}
			want := original
			if mode == "concurrent edit" {
				want = "// Another editor saved this.\n{}\n"
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != want {
				t.Fatalf("clobbered configuration: %q (%v)", data, err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("failed edit left residue: %v (%v)", entries, err)
			}
		})
	}
}

func TestConfigEndpointsAndPorts(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles/opencode-user.jsonc")
	run := func(args ...string) string {
		t.Helper()
		stdout, stderr, status := runLauncher(t, f, "", append([]string{"config", "opencode"}, args...), nil)
		if status != 0 || stderr != "" {
			t.Fatalf("%q: status=%d, stdout=%q, stderr=%q", args, status, stdout, stderr)
		}
		return string(stdout)
	}
	network := func() map[string]any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err = hujson.Standardize(data)
		if err != nil {
			t.Fatal(err)
		}
		var document struct{ Network map[string]any }
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		return document.Network
	}
	want := func(step string, expected string) {
		t.Helper()
		got, err := json.Marshal(network())
		if err != nil || string(got) != expected {
			t.Fatalf("%s:\n got: %s\nwant: %s", step, got, expected)
		}
	}

	if output := run("allow-domain", "https://GitLab.example/"); !strings.Contains(output, "Allowed gitlab.example: all paths and ports.") {
		t.Fatalf("root URL must describe whole-domain access: %s", output)
	}
	want("root URL allows whole domain", `{"allow_domain":["gitlab.example"]}`)
	run("remove-domain", "gitlab.example")
	output := run("allow-domain", "--method", "get", "https://GitLab.example/api/v4/projects/123/**")
	if !strings.Contains(output, "GET requests to gitlab.example matching /api/v4/projects/123/**") ||
		!strings.Contains(output, "plain HTTP is not path-filtered") || !strings.Contains(output, "request bodies are not checked") {
		t.Fatalf("endpoint description: %s", output)
	}
	want("endpoint added after explicit removal",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"}]}]}`)
	if output := run("allow-domain", "--method", "GET", "https://gitlab.example/api/v4/projects/123/**"); !strings.Contains(output, "No change") {
		t.Fatalf("repeated endpoint: %s", output)
	}
	run("allow-domain", "https://gitlab.example/api/v4/projects/456/**")
	want("second endpoint joins the host",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"},{"method":"*","path":"/api/v4/projects/456/**"}]}]}`)
	run("remove-domain", "https://gitlab.example/api/v4/projects/456/**")
	want("one endpoint removed",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"}]}]}`)
	if output := run("show"); !strings.Contains(output, `HTTPS endpoint: "gitlab.example" method="GET" path="/api/v4/projects/123/**"`) || !strings.Contains(output, "inherited grants are not listed") {
		t.Fatalf("show must distinguish endpoints from whole domains: %s", output)
	}
	// Removal matches the stored method; * is the default for remove-domain.
	if output := run("remove-domain", "https://gitlab.example/api/v4/projects/123/**"); !strings.Contains(output, "No change") {
		t.Fatalf("removal with another method changed the rule: %s", output)
	}
	run("remove-domain", "--method", "GET", "https://gitlab.example/api/v4/projects/123/**")
	want("last endpoint removes the host", `{"allow_domain":[]}`)
	writeLauncherFixture(t, path, `{"network":{"allow_domain":[{"domain":"gitlab.example","endpoints":[]}]}}`)
	run("remove-domain", "gitlab.example")
	run("allow-domain", "https://gitlab.example/api/**")
	want("object whole-host entry explicitly replaced",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"*","path":"/api/**"}]}]}`)
	run("remove-domain", "gitlab.example")
	want("whole host removed", `{"allow_domain":[]}`)
	// Hand-written methods match case-insensitively.
	writeLauncherFixture(t, path, `{"network":{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"get","path":"/api/**"}]}]}}`)
	if output := run("allow-domain", "--method", "GET", "https://gitlab.example/api/**"); !strings.Contains(output, "No change") {
		t.Fatalf("lowercase method duplicated: %s", output)
	}
	run("remove-domain", "--method", "GET", "https://gitlab.example/api/**")
	want("lowercase method removed", `{"allow_domain":[]}`)

	if output := run("allow-tcp-port", "8081"); !strings.Contains(output, "TCP port 8081: connect and listen") || !strings.Contains(output, "including non-loopback addresses") || !strings.Contains(output, "bypass domain filtering") {
		t.Fatalf("port description: %s", output)
	}
	run("allow-tcp-port", "08081")
	run("allow-tcp-port", "5432")
	want("ports", `{"allow_domain":[],"open_port":[8081,5432]}`)
	if output := run("show"); !strings.Contains(output, "TCP port (connect/listen; not loopback-only): 8081") {
		t.Fatalf("show lists ports: %s", output)
	}
	run("remove-tcp-port", "8081")
	want("port removed", `{"allow_domain":[],"open_port":[5432]}`)
	run("allow-tcp-port", "1")
	run("allow-tcp-port", "65535")
	want("TCP port boundaries", `{"allow_domain":[],"open_port":[5432,1,65535]}`)
}

func TestConfigWildcardDomains(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles", agent+"-user.jsonc")
			run := func(args ...string) string {
				t.Helper()
				stdout, stderr, status := runLauncher(t, f, "", append([]string{"config", agent}, args...), nil)
				if status != 0 || stderr != "" {
					t.Fatalf("%q: status=%d stdout=%q stderr=%q", args, status, stdout, stderr)
				}
				return string(stdout)
			}
			output := run("allow-domain", "*.EXAMPLE.COM.")
			for _, text := range []string{"subdomains at any depth, excluding example.com", "Applies to new " + agent + " sessions", "restart the agent"} {
				if !strings.Contains(output, text) {
					t.Fatalf("wildcard scope/lifecycle missing %q: %s", text, output)
				}
			}
			if output := run("allow-domain", "*.example.com"); !strings.Contains(output, "No change") {
				t.Fatalf("wildcard normalization was not idempotent: %s", output)
			}
			run("allow-domain", "example.com")
			run("allow-domain", "api.example.com")
			run("remove-domain", "example.com")
			if got := configContents(t, path)["network"]["allow_domain"]; !slices.Equal(got, []string{"*.example.com", "api.example.com"}) {
				t.Fatalf("exact-domain removal changed other scopes: %v", got)
			}
			run("remove-domain", "*.example.com")
			if got := configContents(t, path)["network"]["allow_domain"]; !slices.Equal(got, []string{"api.example.com"}) {
				t.Fatalf("wildcard removal changed exact domain: %v", got)
			}
			run("remove-domain", "api.example.com")
			if output := run("show"); !strings.Contains(output, "(none)") {
				t.Fatalf("domain removals left a grant: %s", output)
			}
		})
	}
}

func TestConfigRemovesExactStoredDomainKeys(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles", agent+"-user.jsonc")
			keys := []string{"\x1b[31mevil.example", "192.0.2.1", "buildserver", "*", "*.example.com", "api.example.com", "example.com:8443"}
			data, err := json.Marshal(map[string]any{"network": map[string]any{"allow_domain": keys}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, path, string(data))
			for i, key := range keys {
				stdout, stderr, status := runLauncher(t, f, "", []string{"config", agent, "remove-domain", key}, nil)
				if status != 0 {
					t.Fatalf("removing %q: status=%d stderr=%q", key, status, stderr)
				}
				if strings.Contains(string(stdout), "\x1b") {
					t.Fatalf("stored key escaped into terminal control output: %q", stdout)
				}
				if got := configContents(t, path)["network"]["allow_domain"]; !slices.Equal(got, keys[i+1:]) {
					t.Fatalf("removal must match one stored key, not expand %q: %v", key, got)
				}
			}
			// Removal must also support stored endpoint rules outside the add grammar.
			writeLauncherFixture(t, path, `{"network":{"allow_domain":[{"domain":"*.example.com","endpoints":[{"method":"GET","path":"/api/**"}]},{"domain":"192.0.2.1","endpoints":[{"method":"GET","path":"/api/**"}]}]}}`)
			for _, host := range []string{"*.example.com", "192.0.2.1"} {
				_, stderr, status := runLauncher(t, f, "", []string{"config", agent, "remove-domain", "--method", "GET", "https://" + host + "/api/**"}, nil)
				if status != 0 {
					t.Fatalf("removing stored endpoint %q: status=%d stderr=%q", host, status, stderr)
				}
			}
			if got := configContents(t, path)["network"]["allow_domain"]; len(got) != 0 {
				t.Fatalf("removing the last endpoints left a grant: %v", got)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("removal left temporary files: %v (%v)", entries, err)
			}
		})
	}
}

func TestConfigScopeConflictExplainsExplicitReplacement(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	for _, step := range []struct {
		args   []string
		status int
	}{
		{[]string{"allow-domain", "example.com"}, 0},
		{[]string{"allow-domain", "https://example.com/api/**"}, 65},
		{[]string{"remove-domain", "example.com"}, 0},
		{[]string{"allow-domain", "https://example.com/api/**"}, 0},
		{[]string{"allow-domain", "example.com"}, 65},
	} {
		_, stderr, status := runLauncher(t, f, "", append([]string{"config", "opencode"}, step.args...), nil)
		if status != step.status || step.status == 65 && !strings.Contains(stderr, `remove-domain "example.com"`) {
			t.Fatalf("%q: status=%d want=%d stderr=%q", step.args, status, step.status, stderr)
		}
	}
}

func TestConfigAddDirAsksBeforeIncludingConfigRepositories(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	group := filepath.Join(f.project, "sdg")
	if err := os.MkdirAll(filepath.Join(group, "sdg-config", ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles/opencode-user.jsonc")
	_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-dir", group}, nil)
	if status != 77 || !strings.Contains(stderr, "sdg-config") || !strings.Contains(stderr, "Grant a narrower directory") {
		t.Fatalf("status=%d, stderr=%q", status, stderr)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a refused grant was written: %v", err)
	}
	stdout, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-dir", group}, []string{"NONO_WRAPPER_ALLOW_CONFIG=1"})
	if status != 0 || !strings.Contains(string(stdout), "Allowed directory") {
		t.Fatalf("override: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}
