// SPDX-FileCopyrightText: The nono-agent-profiles Authors
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
			run("remove-host", "absent.example")
			if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
				t.Fatalf("read/no-op created a configuration directory: %v", err)
			}
			run("add-host", "API.GITHUB.COM.")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("new user configuration should be owner-only: %v", err)
			}
			if output := run("add-host", "api.github.com"); !strings.Contains(output, "No change") {
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
			if output := run("remove-host", "api.github.com"); !strings.Contains(output, "inherited or broader grants") {
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
	run("add-host", "another.example")
	data, _ := os.ReadFile(path)
	for _, text := range []string{"Keep this explanation.", "Keep this endpoint restriction.", "EDITOR", "/user", "/repos/**", "GET", "another.example"} {
		if !strings.Contains(string(data), text) {
			t.Fatalf("lost manual configuration %q: %s", text, data)
		}
	}
	run("add-host", "api.github.com")
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "endpoints") || !strings.Contains(string(data), `"api.github.com"`) || !strings.Contains(string(data), "EDITOR") {
		t.Fatalf("explicit whole-host allowance did not replace its endpoint rules: %s", data)
	}
	run("remove-dir", filepath.Join(home, "docs"))
	run("remove-host", "api.github.com")
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
		_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-host", "api.github.com"}, []string{variable})
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
		{"invalid JSONC", "{broken", []string{"add-host", "api.github.com"}, nil},
		{"non-object", "[]", []string{"add-host", "api.github.com"}, nil},
		{"duplicate key", `{"network":{},"network":{}}`, []string{"add-host", "api.github.com"}, nil},
		{"invalid section", `{"network":[]}`, []string{"add-host", "api.github.com"}, nil},
		{"invalid list", `{"network":{"allow_domain":false}}`, []string{"add-host", "api.github.com"}, nil},
		{"invalid host entry", `{"network":{"allow_domain":[null]}}`, []string{"add-host", "api.github.com"}, nil},
		{"plain HTTP cannot be narrowed", "{}", []string{"add-host", "http://api.github.com/path"}, nil},
		{"URL query", "{}", []string{"add-host", "https://api.github.com/path?private=1"}, nil},
		{"URL query without a path", "{}", []string{"add-host", "https://api.github.com?private=1"}, nil},
		{"URL user name", "{}", []string{"add-host", "https://user@api.github.com/path"}, nil},
		{"invalid host port", "{}", []string{"add-host", "api.github.com:70000"}, nil},
		{"method without a path", "{}", []string{"add-host", "--method", "GET", "api.github.com"}, nil},
		{"invalid method", "{}", []string{"add-host", "--method", "G3T", "https://api.github.com/x"}, nil},
		{"empty method", "{}", []string{"add-host", "--method", "", "https://api.github.com/x"}, nil},
		{"path flag on a port", "{}", []string{"add-port", "--read-write", "8081"}, nil},
		{"fractional port entry", `{"network":{"open_port":[8081.5]}}`, []string{"add-port", "8081"}, nil},
		{"port zero", "{}", []string{"add-port", "0"}, nil},
		{"port name", "{}", []string{"add-port", "http"}, nil},
		{"invalid port entry", `{"network":{"open_port":["8081"]}}`, []string{"add-port", "8081"}, nil},
		{"wildcard is not a host", "{}", []string{"add-host", "*.github.com"}, nil},
		{"missing path", "{}", []string{"add-dir", "missing-directory"}, nil},
		{"empty path", "{}", []string{"add-dir", ""}, nil},
		{"conflicting modes", "{}", []string{"add-dir", ".", "--read-only", "--read-write"}, nil},
		{"directory as file", "{}", []string{"add-file", "."}, nil},
		{"extra argument", "{}", []string{"remove-host", "one.example", "two.example"}, nil},
		{"validation failure", "{}", []string{"add-host", "api.github.com"}, []string{"LAUNCHER_VALIDATE_FAIL=1"}},
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
	_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-host", "api.github.com"}, nil)
	if status != 0 {
		t.Fatalf("symlink edit failed: %s", stderr)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("replaced the user's dotfile symlink: %v", err)
	}
	before, _ := os.ReadFile(target)
	writeLauncherFixture(t, target+".lock", "other editor\n")
	_, stderr, status = runLauncher(t, f, "", []string{"config", "opencode", "add-host", "another.example"}, nil)
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
			_, stderr, status := runLauncher(t, f, "", []string{"config", "opencode", "add-host", "api.github.com"}, []string{
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

	if output := run("add-host", "gitlab.example:8443"); !strings.Contains(output, "Allowed gitlab.example: every path and port.") {
		t.Fatalf("host with port: %s", output)
	}
	want("port dropped", `{"allow_domain":["gitlab.example"]}`)
	output := run("add-host", "--method", "get", "https://GitLab.example/api/v4/projects/123/**")
	if !strings.Contains(output, "GET requests to gitlab.example matching /api/v4/projects/123/**") ||
		!strings.Contains(output, "cannot limit an MCP server") {
		t.Fatalf("endpoint description: %s", output)
	}
	want("endpoint narrows the whole host",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"}]}]}`)
	if output := run("add-host", "--method", "GET", "https://gitlab.example/api/v4/projects/123/**"); !strings.Contains(output, "No change") {
		t.Fatalf("repeated endpoint: %s", output)
	}
	run("add-host", "https://gitlab.example/api/v4/projects/456/**")
	want("second endpoint joins the host",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"},{"method":"*","path":"/api/v4/projects/456/**"}]}]}`)
	run("remove-host", "https://gitlab.example/api/v4/projects/456/**")
	want("one endpoint removed",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"GET","path":"/api/v4/projects/123/**"}]}]}`)
	// Removal matches the stored method; * is the default for remove-host.
	if output := run("remove-host", "https://gitlab.example/api/v4/projects/123/**"); !strings.Contains(output, "No change") {
		t.Fatalf("removal with another method changed the rule: %s", output)
	}
	run("remove-host", "--method", "GET", "https://gitlab.example/api/v4/projects/123/**")
	want("last endpoint removes the host", `{"allow_domain":[]}`)
	writeLauncherFixture(t, path, `{"network":{"allow_domain":[{"domain":"gitlab.example","endpoints":[]}]}}`)
	run("add-host", "https://gitlab.example/api/**")
	want("object whole-host entry narrowed",
		`{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"*","path":"/api/**"}]}]}`)
	run("remove-host", "gitlab.example")
	want("whole host removed", `{"allow_domain":[]}`)
	// Hand-written methods match case-insensitively.
	writeLauncherFixture(t, path, `{"network":{"allow_domain":[{"domain":"gitlab.example","endpoints":[{"method":"get","path":"/api/**"}]}]}}`)
	if output := run("add-host", "--method", "GET", "https://gitlab.example/api/**"); !strings.Contains(output, "No change") {
		t.Fatalf("lowercase method duplicated: %s", output)
	}
	run("remove-host", "--method", "GET", "https://gitlab.example/api/**")
	want("lowercase method removed", `{"allow_domain":[]}`)

	if output := run("add-port", "8081"); !strings.Contains(output, "localhost port 8081") {
		t.Fatalf("port description: %s", output)
	}
	run("add-port", "08081")
	run("add-port", "5432")
	want("ports", `{"allow_domain":[],"open_port":[8081,5432]}`)
	if output := run("show"); !strings.Contains(output, "localhost port: 8081") {
		t.Fatalf("show lists ports: %s", output)
	}
	run("remove-port", "8081")
	want("port removed", `{"allow_domain":[],"open_port":[5432]}`)
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
