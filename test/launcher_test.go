// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var launcherSpecs = []struct {
	agent, profile, official, installURL string
	agentArgs                            []string
}{
	{"opencode", "opencode-chatgpt", "opencode", "https://opencode.ai/v2/docs/", []string{"--standalone"}},
	{"codex", "codex-chatgpt", "codex", "https://developers.openai.com/codex/cli/", []string{
		"--no-daemon", "--sandbox=danger-full-access", "--ask-for-approval=on-request",
	}},
}

func expectedRunProfile(profile string, privateLoopback bool) []string {
	args := []string{"run", "--profile", profile, "--no-diagnostics"}
	if privateLoopback && runtime.GOOS == "linux" {
		args = append(args, "--isolate", "net,loopback")
	}
	return args
}

var wrapperBinary string
var buildEnv = []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0", "GOENV=off", "GOFLAGS="}

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// Build the real CLI once. Tests exercise it as a process, including signals.
	root, err := os.MkdirTemp("", "nono-wrapper-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(root)
	output, err := exec.Command("go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOROOT").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go env:", err)
		return 1
	}
	var settings map[string]string
	if err := json.Unmarshal(output, &settings); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for key, value := range settings {
		buildEnv = append(buildEnv, key+"="+value)
	}
	_, source, _, _ := runtime.Caller(0)
	wrapperBinary = filepath.Join(root, "nono-wrapper")
	cmd := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", wrapperBinary, "./cmd/nono-wrapper")
	cmd.Dir = filepath.Dir(filepath.Dir(source))
	cmd.Env = append(os.Environ(), buildEnv...)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI: %v\n%s", err, output)
		return 1
	}
	return m.Run()
}

type launcherFixture struct {
	bin, project, target string
}

// rootSetting makes the fixture's temporary directory the project root, so
// its project is a valid start directory.
func (f launcherFixture) rootSetting() string {
	return "NONO_WRAPPER_ROOT=" + filepath.Dir(f.project)
}

func testRepository(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), ".."))
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func newLauncherFixture(t *testing.T, agent string) launcherFixture {
	t.Helper()
	root := canonicalTempDir(t)
	f := launcherFixture{
		bin:     filepath.Join(root, "bin with spaces"),
		project: filepath.Join(root, "projects", "project with spaces"),
		target:  filepath.Join(root, "agent installation", agent),
	}
	// HOME is the fixture root; the project root below it must not contain HOME.
	codexHome, openCodeData := filepath.Join(root, ".codex"), filepath.Join(root, ".local/share/opencode")
	for _, path := range []string{f.bin, f.project, filepath.Dir(f.target), codexHome, openCodeData} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A signed-in user: both agents' default login files exist.
	for _, directory := range []string{codexHome, openCodeData} {
		writeLauncherFixture(t, filepath.Join(directory, "auth.json"), "{}\n")
	}
	if err := os.Symlink(wrapperBinary, filepath.Join(f.bin, "nono-wrapper")); err != nil {
		t.Fatal(err)
	}
	// A direct agent invocation must fail: only the fake nono may be executed.
	writeLauncherFixture(t, f.target, "#!/bin/sh\necho 'agent executed outside nono' >&2\nexit 99\n")
	if err := os.Symlink(f.target, filepath.Join(f.bin, agent)); err != nil {
		t.Fatal(err)
	}
	var err error
	f.target, err = filepath.EvalSymlinks(f.target)
	if err != nil {
		t.Fatal(err)
	}
	writeNonoFixture(t, f, `
case "$1" in
profile)
  [ -z "${LAUNCHER_PROFILE_LOG:-}" ] || printf '%s\n' "$*" >> "$LAUNCHER_PROFILE_LOG"
  if [ "$2" = validate ]; then
    [ "$#" -eq 3 ] && [ -f "$3" ] || exit 99
    if [ -n "${LAUNCHER_VALIDATE_FAIL:-}" ]; then
      printf 'fixture profile validation failed\n' >&2
      exit 1
    fi
    exit 0
  fi
  [ "$2" = show ] || exit 99
  name=$3
  if [ "$3" = --json ]; then [ "$#" -eq 4 ] || exit 99; name=$4; else [ "$#" -eq 3 ] || exit 99; fi
  if [ -n "${LAUNCHER_TEST_PROFILE_ERROR:-}" ]; then
    printf '%s\n' "$LAUNCHER_TEST_PROFILE_ERROR" >&2
    exit 1
  fi
  if [ "$name" = "${LAUNCHER_TEST_MISSING_PROFILE:-}" ]; then
    printf 'nono: Profile not found: %s\n' "$name" >&2
    exit 1
  fi
  if [ "$3" = --json ]; then
    case "$name" in
      *-user)
        if [ -n "${LAUNCHER_TEST_USER_JSON:-}" ]; then
          printf '%s\n' "$LAUNCHER_TEST_USER_JSON"
        else
          printf '{"network":{"block":%s}}\n' "${LAUNCHER_TEST_USER_BLOCK:-false}"
        fi ;;
      *) printf '{"network":{"block":%s},"workdir":{"access":"readwrite"}}\n' "${LAUNCHER_TEST_BASE_BLOCK:-false}" ;;
    esac
  fi
  ;;
run)
  printf '%s\000' "$@"
  exit "${LAUNCHER_TEST_EXIT:-0}"
  ;;
*) exit 99 ;;
esac
`)
	// Ensure wildcard-forwarding tests would catch accidental shell expansion.
	writeLauncherFixture(t, filepath.Join(f.project, "literal-expanded-argument"), "fixture\n")
	return f
}

func writeLauncherFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

// Reporting uses nono's supported inspection commands after the run. Each
// fixture has an isolated nono; no test queries the user's real audit store.
func writeNonoFixture(t *testing.T, f launcherFixture, body string) {
	t.Helper()
	writeLauncherFixture(t, filepath.Join(f.bin, "nono"), `#!/bin/sh
case "$1" in
--version)
  printf 'nono 0.81.0\n'
  exit 0 ;;
profile)
  if [ "$2 $3 $4" = 'show --format manifest' ]; then
    [ -z "${LAUNCHER_PROFILE_LOG:-}" ] || printf '%s\n' "$*" >> "$LAUNCHER_PROFILE_LOG"
    if [ -n "${LAUNCHER_TEST_MANIFEST_JSON:-}" ]; then
      printf '%s\n' "$LAUNCHER_TEST_MANIFEST_JSON"
    else
      printf '{"version":"0.1.0","filesystem":{"grants":[]}}\n'
    fi
    exit 0
  fi
  ;;
inspect)
  [ -z "${LAUNCHER_REPORT_LOG:-}" ] || printf '%s\n' "$*" >> "$LAUNCHER_REPORT_LOG"
  case "${LAUNCHER_REPORT_MODE:-}" in
    missing) exit 1 ;;
    timeout) exec /bin/sleep 60 ;;
    invalid) printf 'invalid JSON'; exit 0 ;;
    wrong-name) printf '{"name":"another-run","rollback_session":"0123456789abcdef"}'; exit 0 ;;
    no-audit) printf '{"name":"%s","rollback_session":null}' "$2"; exit 0 ;;
  esac
  printf '{"name":"%s","rollback_session":"0123456789abcdef"}\n' "$2"
  exit 0 ;;
audit)
  [ "$2" = show ] && [ "$3" = 0123456789abcdef ] && [ "$4" = --json ] || exit 99
  [ -z "${LAUNCHER_REPORT_LOG:-}" ] || printf '%s\n' "$*" >> "$LAUNCHER_REPORT_LOG"
  [ "${LAUNCHER_REPORT_MODE:-}" != audit-fails ] || exit 1
  if [ -n "${LAUNCHER_AUDIT_JSON:-}" ]; then
    printf '%s\n' "$LAUNCHER_AUDIT_JSON"
  else
    printf '{"session_id":"0123456789abcdef","audit_event_count":2,"network_events":[]}\n'
  fi
  exit 0 ;;
esac
`+body)
}

func emptySessionReport(agent string, status int) string {
	return fmt.Sprintf("\nnono-wrapper: %s session ended (exit %d)\n", agent, status) +
		"  Audit: 0123456789abcdef (2 recorded events)\n" +
		"  Network: 0 recorded events\n" +
		"  Full audit: nono audit show 0123456789abcdef\n" +
		"  Check a named profile: nono why --profile PROFILE --host HOST (or --path PATH --op read)\n" +
		"    PROFILE is a base or user profile name/path, not the full session policy.\n"
}

func capturedLauncherArgs(t *testing.T, stdout []byte, agent string) []string {
	t.Helper()
	if !bytes.HasSuffix(stdout, []byte{0}) {
		t.Fatalf("fake nono did not capture arguments: %q", stdout)
	}
	args := strings.Split(string(stdout[:len(stdout)-1]), "\x00")
	index := slices.Index(args, "--name")
	if index < 0 || index+1 >= slices.Index(args, "--") {
		t.Fatalf("missing session name before agent arguments: %q", args)
	}
	name := args[index+1]
	nonce, err := hex.DecodeString(strings.TrimPrefix(name, agent+"-"))
	if !strings.HasPrefix(name, agent+"-") || err != nil || len(nonce) != 16 {
		t.Fatalf("invalid unique session name: %q", name)
	}
	args = slices.Delete(args, index, index+2)
	if agent == "opencode" {
		separator := slices.Index(args, "--")
		if len(args) <= separator+5 || args[separator+1] != "/bin/sh" || args[separator+2] != "-c" ||
			args[separator+3] == "" || args[separator+4] != "nono-wrapper-opencode" {
			t.Fatalf("missing in-sandbox OpenCode version guard: %q", args)
		}
		// Other launcher tests compare the effective agent arguments. The
		// version guard's execution/refusal behavior has dedicated tests.
		args = slices.Delete(args, separator+1, separator+5)
	}
	return args
}

func runLauncher(t *testing.T, f launcherFixture, launcher string, args, env []string) ([]byte, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if launcher != "" {
		args = append([]string{launcher}, args...)
	}
	cmd := exec.CommandContext(ctx, filepath.Join(f.bin, "nono-wrapper"), args...)
	cmd.Dir = f.project
	cmd.Env = append([]string{
		"PATH=" + f.bin,
		"HOME=" + filepath.Dir(f.bin),
		"TMPDIR=" + filepath.Dir(f.bin),
		"LC_ALL=C",
		f.rootSetting(),
	}, env...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("launcher timed out: %s", stderr.String())
	}
	var exitError *exec.ExitError
	if err != nil && !errors.As(err, &exitError) {
		t.Fatal(err)
	}
	return stdout.Bytes(), stderr.String(), cmd.ProcessState.ExitCode()
}

func TestLauncherInvocation(t *testing.T) {
	for _, spec := range launcherSpecs {
		t.Run(spec.agent, func(t *testing.T) {
			for _, test := range []struct {
				name, profile string
				profileSet    bool
				args          []string
				status        int
			}{
				{name: "default profile"},
				{name: "empty profile uses default", profileSet: true},
				{name: "profile override", profileSet: true, profile: "custom-offline"},
				{name: "profile path with spaces", profileSet: true, profile: "/custom profiles/agent.jsonc"},
				{name: "exact arguments", args: []string{"", "argument with spaces", "literal*argument", "line\nbreak", "--", "-value"}},
				{name: "agent help flag", args: []string{"--help"}},
				{name: "short agent help", args: []string{"-h"}},
				{name: "agent help command", args: []string{"help", "login"}},
				{name: "agent version", args: []string{"--version"}},
				{name: "leading flags", args: []string{"--model", "model name", "--unknown-flag"}},
				{name: "leading separator", args: []string{"--", "--help", ""}},
				{name: "nonzero exit status", status: 7},
				{name: "the wrapper's own statuses pass through", status: 3},
				{name: "launch failure status passes through", status: 125},
				{name: "interrupted exit status", status: 130},
			} {
				t.Run(test.name, func(t *testing.T) {
					f := newLauncherFixture(t, spec.agent)
					env := []string{"LAUNCHER_TEST_EXIT=" + strconv.Itoa(test.status)}
					profile := spec.profile
					if test.profileSet {
						env = append(env, "NONO_PROFILE="+test.profile)
						if test.profile != "" {
							profile = test.profile
						}
					}
					stdout, stderr, status := runLauncher(t, f, spec.agent, test.args, env)
					if status != test.status || stderr != emptySessionReport(spec.agent, test.status) {
						t.Fatalf("status=%d, want=%d, stderr=%q", status, test.status, stderr)
					}
					got := capturedLauncherArgs(t, stdout, spec.agent)
					want := append(expectedRunProfile(profile, spec.agent == "opencode"), "--allow-cwd", "--read-file", f.target, "--", f.target)
					want = append(want, spec.agentArgs...)
					want = append(want, test.args...)
					if !slices.Equal(got, want) {
						t.Fatalf("nono arguments:\n got: %q\nwant: %q", got, want)
					}
				})
			}
		})
	}
}

func TestLauncherUpstreamProxy(t *testing.T) {
	for _, test := range []struct {
		name   string
		env    []string
		want   []string
		stderr string
		status int
	}{
		{name: "no proxy"},
		{name: "proxy URL", env: []string{"HTTPS_PROXY=http://proxy.example:3128/"}, want: []string{"--upstream-proxy", "proxy.example:3128"}},
		{name: "bare host and port", env: []string{"https_proxy=proxy.example:8080"}, want: []string{"--upstream-proxy", "proxy.example:8080"}},
		{name: "default port", env: []string{"HTTP_PROXY=http://proxy.example"}, want: []string{"--upstream-proxy", "proxy.example:80"}},
		{name: "IPv6 proxy", env: []string{"HTTPS_PROXY=http://[::1]:3128/"}, want: []string{"--upstream-proxy", "[::1]:3128"}},
		{name: "HTTPS_PROXY before HTTP_PROXY", env: []string{"HTTP_PROXY=http://plain.example:1", "HTTPS_PROXY=http://secure.example:2"},
			want: []string{"--upstream-proxy", "secure.example:2"}},
		{name: "explicit nono setting wins", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "NONO_UPSTREAM_PROXY=other.example:1"}},
		{name: "profile blocks the network", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_TEST_BLOCK=true"}},
		{name: "offline profile ignores unused proxy", env: []string{"HTTPS_PROXY=http://user:secret@proxy.example:3128", "LAUNCHER_TEST_BLOCK=true"}},
		{name: "credentials are dropped", env: []string{"HTTPS_PROXY=http://user:secret@proxy.example:3128"},
			want:   []string{"--upstream-proxy", "proxy.example:3128"},
			stderr: "nono-wrapper: HTTPS_PROXY contains a user name or password; nono cannot log in to a proxy, so it connects without them\n"},
		{name: "HTTPS proxy transport unsupported", env: []string{"HTTPS_PROXY=https://proxy.example:443"}, status: 125},
		{name: "SOCKS proxy transport unsupported", env: []string{"HTTPS_PROXY=socks5://proxy.example:1080"}, status: 125},
		{name: "invalid proxy URL", env: []string{"HTTPS_PROXY=http://user:secret@[broken"}, status: 125},
		{name: "proxy path unsupported", env: []string{"HTTPS_PROXY=http://proxy.example/path"}, status: 125},
		{name: "proxy query unsupported", env: []string{"HTTPS_PROXY=http://proxy.example/?secret=value"}, status: 125},
		{name: "invalid port", env: []string{"HTTPS_PROXY=http://proxy.example:70000"}, status: 125},
		{name: "profile query fails", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_TEST_JSON=fail"}, status: 125},
		{name: "profile JSON malformed", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_TEST_JSON=malformed"}, status: 125},
		{name: "profile JSON null", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_TEST_JSON=null"}, status: 125},
		{name: "profile network missing", env: []string{"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_TEST_JSON=empty"}, status: 125},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			writeNonoFixture(t, f, `
case "$1" in
profile)
  if [ "$3" = --json ]; then
    case "${LAUNCHER_TEST_JSON:-}" in
    fail) exit 1 ;;
    malformed) printf 'not JSON\n' ;;
    null) printf 'null\n' ;;
    empty) printf '{}\n' ;;
    *) printf '{"network":{"block":%s}}\n' "${LAUNCHER_TEST_BLOCK:-false}" ;;
    esac
  fi
  ;;
run) printf '%s\000' "$@" ;;
esac
`)
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, test.env)
			if status != test.status {
				t.Fatalf("status=%d, stderr=%q", status, stderr)
			}
			if test.status != 0 {
				if len(stdout) != 0 || stderr == "" || strings.Contains(stderr, "secret") {
					t.Fatalf("failure launched the agent or disclosed credentials: stdout=%q, stderr=%q", stdout, stderr)
				}
				return
			}
			if stderr != test.stderr+emptySessionReport("opencode", test.status) {
				t.Fatalf("unexpected diagnostic: %q", stderr)
			}
			got := capturedLauncherArgs(t, stdout, "opencode")
			want := append(expectedRunProfile("opencode-chatgpt", true), "--allow-cwd", "--read-file", f.target)
			want = append(append(want, test.want...), "--", f.target, "--standalone")
			if !slices.Equal(got, want) {
				t.Fatalf("nono arguments:\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestAgentCompletionDoesNotLaunchASession(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			stdout, stderr, status := runLauncher(t, f, agent, []string{"--generate-shell-completion"},
				[]string{"NONO_WRAPPER_ROOT=" + filepath.Join(f.project, "missing")})
			if status != 0 || stderr != "" || bytes.ContainsRune(stdout, 0) {
				t.Fatalf("completion tried to launch the agent: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestLauncherNamesLoginWhenNoSignInIsSaved(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f := newLauncherFixture(t, agent)
			login := filepath.Join(filepath.Dir(f.bin), ".codex/auth.json")
			if agent == "opencode" {
				login = filepath.Join(filepath.Dir(f.bin), ".local/share/opencode/auth.json")
			}
			if err := os.Remove(login); err != nil {
				t.Fatal(err)
			}
			hint := "run 'nono-wrapper login " + agent + "' first"
			if _, stderr, status := runLauncher(t, f, agent, nil, nil); status != 0 || !strings.Contains(stderr, hint) {
				t.Fatalf("online session without a sign-in gave no hint: status=%d stderr=%q", status, stderr)
			}
			if _, stderr, _ := runLauncher(t, f, agent, nil, []string{"NONO_PROFILE=" + agent + "-offline"}); strings.Contains(stderr, hint) {
				t.Fatalf("offline session asked for a sign-in: %q", stderr)
			}
			if agent == "codex" {
				if _, stderr, _ := runLauncher(t, f, "login", []string{"codex"}, nil); strings.Contains(stderr, hint) {
					t.Fatalf("login asked for a sign-in: %q", stderr)
				}
			}
		})
	}
}

func TestLauncherStartDirectory(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, f launcherFixture) []string
		want  string // diagnostic of a refused start; empty when the agent starts
	}{
		{name: "inside the project root", setup: func(*testing.T, launcherFixture) []string { return nil }},
		{name: "default root is development in the home directory", want: "is outside the project root",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.Mkdir(filepath.Join(filepath.Dir(f.bin), "development"), 0o700); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT="}
			}},
		{name: "explicit workspace root containing multiple projects",
			setup: func(t *testing.T, f launcherFixture) []string {
				for _, name := range []string{"project-a", "project-b"} {
					if err := os.MkdirAll(filepath.Join(f.project, name, ".git"), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				return []string{"NONO_WRAPPER_ROOT=" + f.project}
			}},
		{name: "explicit project root with trailing slash",
			setup: func(_ *testing.T, f launcherFixture) []string {
				return []string{"NONO_WRAPPER_ROOT=" + f.project + "/"}
			}},
		{name: "default project root itself remains refused", want: "will not start in the project root",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.Symlink(f.project, filepath.Join(filepath.Dir(f.bin), "development")); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT="}
			}},
		{name: "inside default project root",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.Symlink(filepath.Dir(f.project), filepath.Join(filepath.Dir(f.bin), "development")); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT="}
			}},
		{name: "outside the project root", want: "is outside the project root",
			setup: func(t *testing.T, f launcherFixture) []string {
				other := filepath.Join(filepath.Dir(f.project), "other")
				if err := os.Mkdir(other, 0o700); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT=" + other}
			}},
		{name: "missing project root", want: "does not exist",
			setup: func(_ *testing.T, f launcherFixture) []string {
				return []string{"NONO_WRAPPER_ROOT=" + filepath.Join(filepath.Dir(f.project), "missing")}
			}},
		{name: "project root containing the home directory", want: "contains your home directory",
			setup: func(_ *testing.T, f launcherFixture) []string {
				return []string{"NONO_WRAPPER_ROOT=" + filepath.Dir(f.bin)}
			}},
		{name: "project root above the home directory", want: "contains your home directory",
			setup: func(_ *testing.T, f launcherFixture) []string {
				return []string{"NONO_WRAPPER_ROOT=" + filepath.Dir(filepath.Dir(f.bin))}
			}},
		{name: "relative project root", want: "must be an absolute path",
			setup: func(*testing.T, launcherFixture) []string { return []string{"NONO_WRAPPER_ROOT=development"} }},
		{name: "symlinked project root",
			setup: func(t *testing.T, f launcherFixture) []string {
				link := filepath.Join(filepath.Dir(f.bin), "root link")
				if err := os.Symlink(filepath.Dir(f.project), link); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT=" + link}
			}},
		{name: "symlinked explicit root itself",
			setup: func(t *testing.T, f launcherFixture) []string {
				link := filepath.Join(filepath.Dir(f.bin), "workspace link")
				if err := os.Symlink(f.project, link); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT=" + link}
			}},
		{name: "symlinked home remains refused", want: "contains your home directory",
			setup: func(t *testing.T, f launcherFixture) []string {
				link := filepath.Join(filepath.Dir(f.bin), "home link")
				if err := os.Symlink(filepath.Dir(f.bin), link); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT=" + link}
			}},
		{name: "configuration repository in explicit root still needs approval", want: "team-config",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.MkdirAll(filepath.Join(f.project, "team-config", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ROOT=" + f.project}
			}},
		{name: "configuration repository without a terminal", want: "sdg-config",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.MkdirAll(filepath.Join(f.project, "sdg-config", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				return nil
			}},
		{name: "configuration repository allowed by setting",
			setup: func(t *testing.T, f launcherFixture) []string {
				if err := os.MkdirAll(filepath.Join(f.project, "sdg-config", ".git"), 0o700); err != nil {
					t.Fatal(err)
				}
				return []string{"NONO_WRAPPER_ALLOW_CONFIG=1"}
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, test.setup(t, f))
			if test.want == "" {
				if status != 0 || stderr != emptySessionReport("opencode", 0) {
					t.Fatalf("start refused: status=%d, stderr=%q", status, stderr)
				}
				return
			}
			if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, test.want) {
				t.Fatalf("status=%d, stdout=%q, stderr=%q, want refusal mentioning %q", status, stdout, stderr, test.want)
			}
		})
	}
}

func TestLauncherInspectsStructuredPolicyAndEffectiveManifest(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	log := filepath.Join(filepath.Dir(f.bin), "profile.log")
	_, stderr, status := runLauncher(t, f, "opencode", nil, []string{
		"HTTPS_PROXY=http://proxy.example:3128", "LAUNCHER_PROFILE_LOG=" + log,
	})
	if status != 0 || stderr != emptySessionReport("opencode", 0) {
		t.Fatalf("launch failed: status=%d (%s)", status, stderr)
	}
	data, err := os.ReadFile(log)
	if err != nil || string(data) != "profile show --json opencode-chatgpt\nprofile show --format manifest opencode-chatgpt\n" {
		t.Fatalf("missing or repeated structured policy/manifest inspection: %q (%v)", data, err)
	}
}

func TestLauncherCancellationBeforeExec(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	writeNonoFixture(t, f, `
case "$1" in
profile)
  if [ "$3" = --json ]; then kill -INT "$PPID"; exec /bin/sleep 5; fi
  ;;
run) printf 'agent started after cancellation\n' ;;
esac
`)
	stdout, stderr, status := runLauncher(t, f, "opencode", nil, []string{"HTTPS_PROXY=http://proxy.example:3128"})
	if status == 0 || len(stdout) != 0 || !strings.Contains(stderr, "context canceled") {
		t.Fatalf("cancellation did not prevent launch: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}

func TestLauncherSSHAgent(t *testing.T) {
	// Socket paths are length-limited, so keep this one short.
	dir, err := os.MkdirTemp("", "ssh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	public, private := filepath.Join(dir, "id.pub"), filepath.Join(dir, "id")
	for _, path := range []string{public, private} {
		writeLauncherFixture(t, path, "key\n")
	}
	alias := filepath.Join(dir, "misleading.pub")
	if err := os.Symlink(private, alias); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		env  []string
		want []string
	}{
		{name: "Git public filename adds no file permission", env: []string{"SSH_AUTH_SOCK=" + socket, "LAUNCHER_TEST_SIGNING_KEY=" + public},
			want: []string{"--allow-unix-socket", socket}},
		{name: "private signing key is not granted", env: []string{"SSH_AUTH_SOCK=" + socket, "LAUNCHER_TEST_SIGNING_KEY=" + private},
			want: []string{"--allow-unix-socket", socket}},
		{name: "no signing key", env: []string{"SSH_AUTH_SOCK=" + socket},
			want: []string{"--allow-unix-socket", socket}},
		{name: "public suffix cannot grant a private target", env: []string{"SSH_AUTH_SOCK=" + socket, "LAUNCHER_TEST_SIGNING_KEY=" + alias},
			want: []string{"--allow-unix-socket", socket}},
		{name: "turned off", env: []string{"SSH_AUTH_SOCK=" + socket, "LAUNCHER_TEST_SIGNING_KEY=" + public, "NONO_WRAPPER_SSH_AGENT=0"}},
		{name: "not a socket", env: []string{"SSH_AUTH_SOCK=" + public, "LAUNCHER_TEST_SIGNING_KEY=" + public}},
		{name: "relative socket", env: []string{"SSH_AUTH_SOCK=agent"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			log := filepath.Join(filepath.Dir(f.bin), "git.log")
			writeLauncherFixture(t, filepath.Join(f.bin, "git"), `#!/bin/sh
printf 'invoked\n' > "$LAUNCHER_GIT_LOG"
[ -n "${LAUNCHER_TEST_SIGNING_KEY:-}" ] || exit 1
printf '%s\n' "$LAUNCHER_TEST_SIGNING_KEY"
`)
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, append(test.env, "LAUNCHER_GIT_LOG="+log))
			if status != 0 || stderr != emptySessionReport("opencode", 0) {
				t.Fatalf("status=%d, stderr=%q", status, stderr)
			}
			got := capturedLauncherArgs(t, stdout, "opencode")
			want := append(expectedRunProfile("opencode-chatgpt", true), "--allow-cwd", "--read-file", f.target)
			want = append(append(want, test.want...), "--", f.target, "--standalone")
			if !slices.Equal(got, want) {
				t.Fatalf("nono arguments:\n got: %q\nwant: %q", got, want)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("repository Git configuration was consulted for host grants: %v", err)
			}
		})
	}
}

func TestLauncherMissingPrerequisites(t *testing.T) {
	for _, spec := range launcherSpecs {
		for _, test := range []struct{ missing, message, hint string }{
			{"nono", "nono is not on PATH", "https://nono.sh/docs/cli/getting_started/installation"},
			{spec.agent, spec.agent + " CLI is not on PATH", spec.installURL},
		} {
			t.Run(spec.agent+"/"+test.missing, func(t *testing.T) {
				f := newLauncherFixture(t, spec.agent)
				if err := os.Remove(filepath.Join(f.bin, test.missing)); err != nil {
					t.Fatal(err)
				}
				stdout, stderr, status := runLauncher(t, f, spec.agent, nil, nil)
				if status != 127 || len(stdout) != 0 || !strings.Contains(stderr, test.message) || !strings.Contains(stderr, test.hint) {
					t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
				}
			})
		}
	}
}

// A mise shim links the agent's name to mise itself. Granting and running that
// link's target would start mise, not the agent.
func TestLauncherResolvesMiseShim(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	mise := filepath.Join(filepath.Dir(f.target), "mise")
	writeLauncherFixture(t, mise, `#!/bin/sh
[ "$1 $2" = 'which opencode' ] || exit 99
[ -z "${MISE_FIXTURE_INACTIVE:-}" ] || { echo 'mise ERROR opencode is not currently active' >&2; exit 1; }
printf '%s\n' "$MISE_FIXTURE_TOOL"
`)
	shim := filepath.Join(f.bin, "opencode")
	if err := os.Remove(shim); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mise, shim); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runLauncher(t, f, "opencode", nil, []string{"MISE_FIXTURE_TOOL=" + f.target})
	if status != 0 {
		t.Fatalf("status=%d, stderr=%q", status, stderr)
	}
	want := append(expectedRunProfile("opencode-chatgpt", true), "--allow-cwd", "--read-file", f.target, "--", f.target, "--standalone")
	if got := capturedLauncherArgs(t, stdout, "opencode"); !slices.Equal(got, want) {
		t.Fatalf("nono arguments:\n got: %q\nwant: %q", got, want)
	}
	stdout, stderr, status = runLauncher(t, f, "opencode", nil, []string{"MISE_FIXTURE_INACTIVE=1"})
	if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, "mise shim") || !strings.Contains(stderr, "not currently active") {
		t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}

func TestLauncherProfileSetupHints(t *testing.T) {
	for _, spec := range launcherSpecs {
		for _, test := range []struct{ name, diagnostic string }{
			{"missing local profile", "nono: Profile not found: " + spec.profile},
			{"missing official pack", "nono: Profile inheritance error: base profile 'nolabs-ai/" + spec.official + "' not found"},
			{"invalid profile", "nono: invalid JSON in profile"},
		} {
			t.Run(spec.agent+"/"+test.name, func(t *testing.T) {
				f := newLauncherFixture(t, spec.agent)
				stdout, stderr, status := runLauncher(t, f, spec.agent, nil, []string{"LAUNCHER_TEST_PROFILE_ERROR=" + test.diagnostic})
				if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, test.diagnostic) || !strings.Contains(stderr, "just install-local") ||
					!strings.Contains(stderr, "docs/install.md") || !strings.Contains(stderr, "the pinned nolabs-ai/"+spec.official+" pack") {
					t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
				}
			})
		}
	}
}

func TestOpenCodeServerSelection(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		standalone bool
	}{
		{"default private server", nil, true},
		{"foreground server", []string{"serve", "--hostname", "127.0.0.1", "--port", "4096"}, false},
		{"global flag before foreground server", []string{"--print-logs", "serve", "--port", "4096"}, false},
		{"root option value is not a command", []string{"--prompt", "serve"}, true},
		{"root option value is not a server flag", []string{"--prompt", "--server=example"}, true},
		{"session option before foreground server", []string{"--session", "example", "serve"}, false},
		{"short session option value", []string{"-s", "serve"}, true},
		{"explicit server", []string{"--server", "http://127.0.0.1:4096", "session", "list"}, false},
		{"server equals form", []string{"session", "list", "--server=http://127.0.0.1:4096"}, false},
		{"message after separator", []string{"run", "--", "--server=http://127.0.0.1:4096"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			stdout, stderr, status := runLauncher(t, f, "opencode", test.args, nil)
			if status != 0 {
				t.Fatalf("launch failed: status=%d stderr=%q", status, stderr)
			}
			want := append(expectedRunProfile("opencode-chatgpt", test.standalone), "--allow-cwd", "--read-file", f.target, "--", f.target)
			if test.standalone {
				want = append(want, "--standalone")
			}
			want = append(want, test.args...)
			if got := capturedLauncherArgs(t, stdout, "opencode"); !slices.Equal(got, want) {
				t.Fatalf("server selection changed forwarded arguments: got=%q want=%q", got, want)
			}
		})
	}
}

func TestLauncherRequiresItsSelectedProfiles(t *testing.T) {
	for _, spec := range launcherSpecs {
		t.Run(spec.agent+"/default profile missing", func(t *testing.T) {
			f := newLauncherFixture(t, spec.agent)
			stdout, stderr, status := runLauncher(t, f, spec.agent, nil, []string{"LAUNCHER_TEST_MISSING_PROFILE=" + spec.profile})
			if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, "just install-local") {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
		t.Run(spec.agent+"/chosen profile missing", func(t *testing.T) {
			f := newLauncherFixture(t, spec.agent)
			stdout, stderr, status := runLauncher(t, f, spec.agent, nil, []string{"NONO_PROFILE=custom", "LAUNCHER_TEST_MISSING_PROFILE=custom"})
			if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, `cannot load selected profile "custom"`) ||
				!strings.Contains(stderr, "Check NONO_PROFILE") || strings.Contains(stderr, "Run 'just install-local'") {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
		t.Run(spec.agent+"/default profile broken", func(t *testing.T) {
			f := newLauncherFixture(t, spec.agent)
			writeLauncherFixture(t, filepath.Join(f.bin, "nono"), `#!/bin/sh
[ "$1" != run ] || { printf ran; exit 0; }
echo "nono: Profile not found: $3" >&2
exit 1
`)
			stdout, stderr, status := runLauncher(t, f, spec.agent, nil, []string{"LAUNCHER_AGENT=" + spec.agent, "LAUNCHER_OFFICIAL=" + spec.official})
			if status != 125 || len(stdout) != 0 || !strings.Contains(stderr, "cannot load profile") {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestCLIHelpAndUsage(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    []string
		status  int
		message string
	}{
		{"no arguments", nil, 64, "Run 'nono-wrapper --help'"},
		{"configuration without a command", []string{"config"}, 64, "COMMANDS:"},
		{"root help", []string{"--help"}, 0, "COMMANDS:"},
		{"wrapper version", []string{"--version"}, 0, runtime.Version()},
		{"launcher help", []string{"help", "codex"}, 0, "NONO_PROFILE"},
		{"configuration help", []string{"config", "--help"}, 0, "Manage persistent user allowances"},
		{"agent configuration help", []string{"config", "opencode", "--help"}, 0, "allow-domain"},
		{"directory configuration help", []string{"config", "codex", "add-dir", "--help"}, 0, "read-only"},
		{"doctor help", []string{"doctor", "--help"}, 0, "offline profiles"},
		{"changes help", []string{"changes", "--help"}, 0, "MARKER"},
		{"unknown command", []string{"unknown-agent"}, 64, "unknown command"},
		{"mistyped command", []string{"doctr"}, 64, `did you mean "doctor"?`},
		{"unknown flag", []string{"--unknown"}, 64, "flag provided but not defined"},
		{"mistyped flag", []string{"--hlep"}, 64, `did you mean "--help"?`},
		{"unknown flag names its help", []string{"doctor", "--unknown"}, 64, "see nono-wrapper doctor --help"},
		{"unknown configuration command", []string{"config", "opencode", "unknown"}, 64, "see nono-wrapper config opencode --help"},
		{"unknown help topic", []string{"help", "unknown"}, 64, "unknown"},
		{"exit statuses in help", []string{"--help"}, 0, "Exit status:"},
		{"missing marker file", []string{"changes", "missing-marker"}, 66, "marker"},
		{"unknown doctor agent", []string{"doctor", "unknown"}, 64, "unknown agent"},
		{"missing marker", []string{"changes"}, 64, "MARKER"},
		{"extra changes arguments", []string{"changes", "marker", ".", "extra"}, 64, "MARKER"},
		{"manual outside a checkout", []string{"man"}, 0, "## SYNOPSIS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			stdout, stderr, status := runLauncher(t, f, "", test.args, nil)
			if status != test.status || !strings.Contains(string(stdout)+stderr, test.message) {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
		})
	}
}

func TestLauncherPreservesProcessIOAndSignalStatus(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	writeNonoFixture(t, f, `
[ "$1" = profile ] && { printf '{"network":{"block":false}}\n'; exit 0; }
printf '%s\n' "$PWD" "$LAUNCHER_TEST_ENV"
IFS= read -r input
printf '%s\n' "$input"
kill -TERM "$$"
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wrapperBinary, "opencode")
	cmd.Dir = f.project
	cmd.Env = []string{"PATH=" + f.bin, "HOME=" + filepath.Dir(f.bin), f.rootSetting(), "LAUNCHER_TEST_ENV=preserved"}
	cmd.Stdin = strings.NewReader("input with spaces\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || ctx.Err() != nil {
		t.Fatalf("expected termination by signal: %v (%s)", err, output)
	}
	if exit.ExitCode() != 128+int(syscall.SIGTERM) || stderr.String() != emptySessionReport("opencode", 143) {
		t.Fatalf("signal status or reporting was lost: %v (%s)", exit, &stderr)
	}
	want := fmt.Sprintf("%s\npreserved\ninput with spaces\n", f.project)
	if string(output) != want {
		t.Fatalf("cwd, environment or stdin changed:\n got %q\nwant %q", output, want)
	}
}
