// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

var agentInstallURLs = map[string]string{
	"opencode": "https://opencode.ai/v2/docs/",
	"codex":    "https://developers.openai.com/codex/cli/",
}

func launch(ctx context.Context, agent string, args []string, stderr io.Writer) error {
	nono, err := exec.LookPath("nono")
	if err != nil {
		return cli.Exit("nono is not on PATH; install: https://nono.sh/docs/cli/getting_started/installation", exitNotFound)
	}
	program, err := exec.LookPath(agent)
	if err != nil {
		return cli.Exit(agent+" CLI is not on PATH; install: "+agentInstallURLs[agent], exitNotFound)
	}
	program, err = filepath.Abs(program)
	if err != nil {
		return err
	}
	if err := checkStartDirectory(os.Stdin, stderr, interactiveInput()); err != nil {
		return err
	}
	if path, err := userProfilePath(agent); err != nil {
		return err
	} else if path != "" {
		workdir, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := protectRuleWrite(workdir); err != nil {
			return fmt.Errorf("cannot share this working directory: %w", err)
		}
	}
	agentExecutable, err := resolveExecutable(ctx, agent, program)
	if err != nil {
		return err
	}
	nonoExecutable, err := resolveExecutable(ctx, "nono", nono)
	if err != nil {
		return err
	}
	program, nono = agentExecutable.path, nonoExecutable.path
	protected, err := protectedLaunchPaths(agentExecutable, nonoExecutable)
	if err != nil {
		return err
	}
	runtimePaths, err := prepareAgentState(agent, protected)
	if err != nil {
		return err
	}
	profile := os.Getenv("NONO_PROFILE")
	chosen := profile != ""
	if !chosen {
		profile = agent + "-chatgpt"
	}
	user, err := userProfile(agent)
	if err != nil {
		return err
	}

	// Resolve each profile once for validation and proxy policy. nono performs
	// the actual composition through --extends; network.block is sticky.
	shown, diagnostic, loadErr := showProfile(ctx, nono, profile)
	if err := ctx.Err(); err != nil {
		return err
	}
	if loadErr != nil {
		fmt.Fprint(stderr, diagnostic)
		if chosen && !bundledProfile(profile) {
			return fmt.Errorf("cannot load selected profile %q.\nCheck NONO_PROFILE and inspect that name or path with 'nono profile show'. If it comes from a pack, check that the pack is installed", profile)
		}
		return fmt.Errorf("cannot load profile %q.\nRun 'just install-local' from the nono-wrapper checkout to install our profiles and the pinned nolabs-ai/%s pack.\nSee docs/install.md in the nono-wrapper checkout", profile, agent)
	}
	block, err := profileBlocksNetwork(shown, profile)
	if err != nil {
		return err
	}
	profileArgs := []string{"--profile", profile}
	selectedProfile := profile
	finalShown := shown
	dropSocketMediation := socketMediationUnavailable(ctx, nono, profile, shown)
	if err := ctx.Err(); err != nil {
		return err
	}
	if dropSocketMediation {
		fmt.Fprintf(stderr, "nono-wrapper: %s\n", socketMediationNotice)
	}
	var userShown []byte
	compositionUser := user
	if user == profile {
		compositionUser = ""
	}
	if compositionUser != "" {
		data, userDiagnostic, userErr := showProfile(ctx, nono, user)
		if err := ctx.Err(); err != nil {
			return err
		}
		if userErr != nil {
			fmt.Fprint(stderr, userDiagnostic)
			return fmt.Errorf("cannot load user profile %q; fix its file in your nono profiles directory", user)
		}
		userShown = data
		userBlock, err := profileBlocksNetwork(userShown, user)
		if err != nil {
			return err
		}
		block = block || userBlock
	}
	var gitSSH *gitSSHSession
	if !block && profile == agent+"-chatgpt" {
		userPath := ""
		if compositionUser != "" {
			userPath, err = configPath(agent)
			if err != nil {
				return err
			}
		}
		gitSSH, err = prepareGitSSH(ctx, userPath)
		if err != nil {
			return err
		}
		if gitSSH != nil {
			defer gitSSH.cleanup()
			sshProtected, err := protectedLaunchPaths(gitSSH.ssh)
			if err != nil {
				return err
			}
			protected = append(protected, sshProtected...)
			entries, err := executableEntries(gitSSH.source)
			if err != nil {
				return err
			}
			actual, err := canonicalConfigPath(gitSSH.source)
			if err != nil {
				return err
			}
			for _, path := range append(entries, actual) {
				protected = append(protected, pathRule{path, pathRead, true})
			}
		}
	}
	if compositionUser != "" || dropSocketMediation || len(runtimePaths) > 0 || gitSSH != nil {
		composed, cleanup, err := composeSessionPolicy(ctx, nono, profile, compositionUser, program, shown, userShown, dropSocketMediation, runtimePaths, gitSSH)
		if err != nil {
			return err
		}
		defer cleanup()
		if composed != "" {
			profileArgs = []string{"--profile", composed}
			selectedProfile = composed
		}
	}
	if selectedProfile != profile {
		var diagnostic string
		finalShown, diagnostic, err = showProfile(ctx, nono, selectedProfile)
		if err != nil {
			return fmt.Errorf("inspect final session policy: %w\n%s", err, diagnostic)
		}
	}
	grants, err := effectivePathGrants(ctx, nono, selectedProfile, program, finalShown)
	if err != nil {
		return err
	}
	if err := checkProtectedWrites(grants, protected); err != nil {
		return err
	}
	proxyArgs, credentials, err := upstreamProxyArgs(block)
	if err != nil {
		return err
	}
	if credentials != "" {
		fmt.Fprintf(stderr, "nono-wrapper: %s contains a user name or password; nono cannot log in to a proxy, so it connects without them\n", credentials)
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("name nono session: %w", err)
	}
	name := fmt.Sprintf("%s-%x", agent, nonce)
	nonoArgs := append([]string{"run"}, profileArgs...)
	standalone := agent == "opencode" && openCodeStandalone(args)
	if standalone && runtime.GOOS == "linux" {
		// OpenCode owns its dynamic-port server and its lifetime. Give both
		// processes private loopback, never access to arbitrary host ports.
		nonoArgs = append(nonoArgs, "--isolate", "net,loopback")
	}
	nonoArgs = append(nonoArgs, "--allow-cwd", "--read-file", program)
	nonoArgs = append(nonoArgs, sshAgentArgs()...)
	nonoArgs = append(nonoArgs, proxyArgs...)
	nonoArgs = append(nonoArgs, "--name", name, "--")
	if agent == "opencode" {
		// Probe and exec the same resolved executable inside this session.
		// Never execute the agent on the host just to discover its version.
		nonoArgs = append(nonoArgs, "/bin/sh", "-c", openCodeVersionGuard, "nono-wrapper-opencode", program)
	} else {
		nonoArgs = append(nonoArgs, program)
	}
	if agent == "codex" {
		// https://nono.sh/docs/cli/clients/codex: one OS sandbox, approvals retained.
		nonoArgs = append(nonoArgs, "--no-daemon", "--sandbox=danger-full-access", "--ask-for-approval=on-request")
	} else if standalone {
		// Keep OpenCode 2's server inside the sandbox, not in a shared host service.
		nonoArgs = append(nonoArgs, "--standalone")
	}
	nonoArgs = append(nonoArgs, args...)
	if err := ctx.Err(); err != nil {
		return err
	}
	return runSession(ctx, nono, nonoArgs, agent, name, stderr)
}

// Arguments are positional shell parameters, never interpolated into code.
// OpenCode has printed both bare versions and "opencode vVERSION". Require
// major 2, with numeric minor/patch prefixes, but impose no minor-version floor.
const openCodeVersionGuard = `
refuse_version() {
    printf '%s\n' 'nono-wrapper: OpenCode 2.x.x is required. Install or select OpenCode 2; refusing to start this version.' >&2
    exit 125
}
version=$("$1" --version) || refuse_version
case "$version" in
    'opencode v'*) version=${version#"opencode v"} ;;
    'opencode '*) version=${version#"opencode "} ;;
    v*) version=${version#v} ;;
esac
case "$version" in
    ''|*[!0-9A-Za-z.+-]*) refuse_version ;;
esac
case "$version" in
    2.[0-9]*.[0-9]*) ;;
    *) refuse_version ;;
esac
exec "$@"
`

// An explicit server is the user's delegation choice. The serve command owns
// that server directly and does not accept the client's --standalone flag.
func openCodeStandalone(args []string) bool {
	command := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--server" || strings.HasPrefix(arg, "--server=") {
			return false
		}
		// These root options consume a value, which is not a subcommand or
		// a server selection. Other root flags are boolean or use =value.
		if command == "" && (arg == "--prompt" || arg == "--session" || arg == "-s") {
			i++
			continue
		}
		if command == "" && !strings.HasPrefix(arg, "-") {
			command = arg
		}
	}
	return command != "serve"
}

// nono resolves filesystem grants before pack first-run hooks execute. Make
// the known agent state paths exist first, so a cold launch receives the same
// grants as a later one. Return only non-default locations that the bundled
// session policy must add; external profiles keep native nono behavior.
func prepareAgentState(agent string, protected []pathRule) ([]pathRule, error) {
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("preparing agent state requires an absolute HOME")
	}
	type stateDirectory struct{ path, fallback string }
	var paths []stateDirectory
	if agent == "codex" {
		fallback := filepath.Join(home, ".codex")
		path := os.Getenv("CODEX_HOME")
		if path == "" {
			path = fallback
		}
		paths = append(paths, stateDirectory{path, fallback})
	} else {
		// OpenCode 2's installation under ~/.opencode is read-only upstream.
		path := filepath.Join(home, ".npm")
		paths = append(paths, stateDirectory{path, path})
		for _, location := range []struct{ variable, fallback string }{
			{"XDG_CONFIG_HOME", ".config"}, {"XDG_DATA_HOME", ".local/share"},
			{"XDG_CACHE_HOME", ".cache"}, {"XDG_STATE_HOME", ".local/state"},
		} {
			base := os.Getenv(location.variable)
			if base == "" {
				base = filepath.Join(home, location.fallback)
			}
			fallback := filepath.Join(home, location.fallback)
			paths = append(paths, stateDirectory{filepath.Join(base, "opencode"), filepath.Join(fallback, "opencode")})
			if location.variable == "XDG_DATA_HOME" {
				paths = append(paths, stateDirectory{filepath.Join(base, "opentui"), filepath.Join(fallback, "opentui")})
			}
		}
	}
	var runtimePaths []pathRule
	for _, directory := range paths {
		path := directory.path
		if !filepath.IsAbs(path) {
			return nil, invalidConfig{fmt.Errorf("agent state directory must be absolute: %q", path)}
		}
		actual, err := literalRuntimePath(path)
		if err != nil {
			return nil, err
		}
		if err := protectRuleWrite(path); err != nil {
			return nil, fmt.Errorf("cannot prepare agent state: %w", err)
		}
		if err := checkProtectedWrites([]pathRule{{actual, pathRead | pathWrite, false}}, protected); err != nil {
			return nil, fmt.Errorf("cannot prepare agent state: %w", err)
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, fmt.Errorf("cannot prepare agent state directory %q; check its ownership and the agent's HOME/XDG or CODEX_HOME setting: %w", path, quotePathError(err))
		}
		fallback, err := canonicalConfigPath(directory.fallback)
		if err != nil {
			return nil, err
		}
		if !samePolicyPath(actual, fallback) {
			runtimePaths = append(runtimePaths, pathRule{actual, pathRead | pathWrite, false})
		}
	}
	return runtimePaths, nil
}

type executable struct{ path, manager, entry string }

// Keep the invocation entry as well as its resolved target. Writable access
// to a shim's parent could replace the shim without modifying its target.
func fileEntryPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := canonicalConfigPath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

// Preserve directory symlinks too: replacing one redirects future invocations
// without writing either the final entry or the resolved executable.
func executableEntries(path string) ([]string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var entries []string
	seen := map[string]bool{}
	pending := []string{path}
	for len(pending) > 0 {
		path := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[path] {
			continue
		}
		seen[path] = true
		entry, err := fileEntryPath(path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		for current := path; filepath.Dir(current) != current; current = filepath.Dir(current) {
			entry, err := fileEntryPath(current)
			if err != nil {
				return nil, err
			}
			info, err := os.Lstat(entry)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, quotePathError(err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			entries = append(entries, entry)
			target, err := os.Readlink(entry)
			if err != nil {
				return nil, quotePathError(err)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(entry), target)
			}
			pending = append(pending, target)
		}
	}
	return entries, nil
}

// Resolve the supervisor and agent before inspecting policy. A shim is not
// the executable whose filesystem permissions must stay read-only.
func resolveExecutable(ctx context.Context, name, path string) (executable, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return executable{}, err
	}
	entry := path
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return executable{}, fmt.Errorf("resolve %s executable: %w", name, err)
	}
	resolved := executable{path: path, entry: entry}
	if filepath.Base(path) == "mise" {
		resolved.manager = path
		resolved.path, err = miseTool(ctx, path, name)
	}
	return resolved, err
}

// A mise shim is a link to mise, which picks the tool from the link's name.
// The sandbox needs the tool itself: mise invoked by its own name is not it.
func miseTool(ctx context.Context, mise, agent string) (string, error) {
	var diagnostic strings.Builder
	which := exec.CommandContext(ctx, mise, "which", agent)
	which.Stderr = &diagnostic
	which.WaitDelay = time.Second
	found, err := which.Output()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(found))
	if err != nil || !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s on PATH is a mise shim, and mise cannot resolve it in this directory.\n%sCheck with 'mise which %s'", agent, diagnostic.String(), agent)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s executable: %w", agent, err)
	}
	return path, nil
}

// Only the caller's configuration directory supplies automatic additions.
// Use nono's XDG layout on both Linux and macOS, matching install-local.
func userProfile(agent string) (string, error) {
	path, err := userProfilePath(agent)
	if err != nil || path == "" {
		return "", err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", quotePathError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return "", quotePathError(err)
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("user profile must be a regular file: %q", path)
	}
	return agent + "-user", nil
}

func userProfilePath(agent string) (string, error) {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", nil
		}
		config = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(config) {
		return "", invalidConfig{fmt.Errorf("user profile configuration directory must be absolute: %q", config)}
	}
	return filepath.Join(config, "nono", "profiles", agent+"-user.jsonc"), nil
}

func profileBlocksNetwork(shown []byte, profile string) (bool, error) {
	var resolved struct {
		Network *struct {
			Block *bool `json:"block"`
		} `json:"network"`
	}
	if json.Unmarshal(shown, &resolved) != nil || resolved.Network == nil || resolved.Network.Block == nil {
		return false, fmt.Errorf("nono returned invalid network policy for profile %q", profile)
	}
	return *resolved.Network.Block, nil
}

// Keep JSON and diagnostics separate; logging must not become policy input.
func showProfile(ctx context.Context, nono, profile string) ([]byte, string, error) {
	var diagnostic strings.Builder
	check := exec.CommandContext(ctx, nono, "profile", "show", "--json", profile)
	check.Stderr = &diagnostic
	shown, err := check.Output()
	return shown, diagnostic.String(), err
}

// Only the caller's socket is delegated. Repository Git configuration must not
// expand host file permissions; Git can use an inline public signing key.
func sshAgentArgs() []string {
	socket := os.Getenv("SSH_AUTH_SOCK")
	if os.Getenv("NONO_WRAPPER_SSH_AGENT") == "0" || !filepath.IsAbs(socket) {
		return nil
	}
	if info, err := os.Stat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		return nil
	}
	return []string{"--allow-unix-socket", socket}
}

// upstreamProxyArgs chains nono's proxy through the caller's HTTP proxy. nono
// does not read HTTPS_PROXY itself; NONO_UPSTREAM_PROXY takes precedence.
// nono cannot authenticate to an upstream proxy, so credentials in the URL are
// dropped; the returned name says which variable held them.
func upstreamProxyArgs(blockNetwork bool) ([]string, string, error) {
	if blockNetwork || os.Getenv("NONO_UPSTREAM_PROXY") != "" {
		return nil, "", nil
	}
	var name, value string
	for _, name = range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if value = os.Getenv(name); value != "" {
			break
		}
	}
	if value == "" {
		return nil, "", nil
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	// The value may hold a password, so it is never printed.
	invalid := fmt.Errorf("cannot use %s: expected an HTTP proxy URL or host:port", name)
	proxy, err := url.Parse(value)
	if err != nil || !strings.EqualFold(proxy.Scheme, "http") || proxy.Hostname() == "" ||
		(proxy.Path != "" && proxy.Path != "/") || proxy.RawQuery != "" || proxy.Fragment != "" {
		return nil, "", invalid
	}
	port := proxy.Port()
	if port == "" {
		port = "80"
	}
	if _, err := parsePort(port); err != nil {
		return nil, "", invalid
	}
	credentials := ""
	if proxy.User != nil {
		credentials = name
	}
	return []string{"--upstream-proxy", net.JoinHostPort(proxy.Hostname(), port)}, credentials, nil
}
