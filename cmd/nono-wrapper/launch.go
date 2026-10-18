// SPDX-FileCopyrightText: The nono-agent-profiles Authors
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

// officialPacks names each agent's official nono pack, which installs a
// profile of the same name. OpenCode 1 has its own pack since nono's
// nolabs-ai/opencode moved to OpenCode 2.
var officialPacks = map[string]string{"opencode": "opencode-v1", "codex": "codex"}

var agentInstallURLs = map[string]string{
	"opencode": "https://opencode.ai/docs/",
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
	program, err = filepath.EvalSymlinks(program)
	if err != nil {
		return fmt.Errorf("resolve %s executable: %w", agent, err)
	}
	if filepath.Base(program) == "mise" {
		program, err = miseTool(ctx, program, agent)
		if err != nil {
			return err
		}
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
	if loadErr != nil && !chosen && officialProfileOnly(ctx, nono, profile, agent) {
		if runtime.GOOS == "darwin" {
			return fmt.Errorf("cannot load profile %q; refusing the official-profile fallback on macOS because it omits the Apple Events broker denial.\nRun 'just install-local' from the nono-agent-profiles checkout to install the bundled profiles", profile)
		}
		previous := profile
		profile = officialPacks[agent]
		shown, diagnostic, loadErr = showProfile(ctx, nono, profile)
		if loadErr == nil {
			fmt.Fprintf(stderr, "nono-wrapper: profile %q is not installed; using nono's official %q profile without the nono-agent-profiles rules\n", previous, profile)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if loadErr != nil {
		fmt.Fprint(stderr, diagnostic)
		return fmt.Errorf("cannot load profile %q.\nRun 'just install-local' from the nono-agent-profiles checkout to install our profiles and the pinned nolabs-ai/%s pack.\nSee docs/install.md in the nono-agent-profiles checkout", profile, officialPacks[agent])
	}
	block, err := profileBlocksNetwork(shown, profile)
	if err != nil {
		return err
	}
	profileArgs := []string{"--profile", profile}
	dropSocketMediation := socketMediationUnavailable(ctx, nono, profile, shown)
	if err := ctx.Err(); err != nil {
		return err
	}
	if dropSocketMediation {
		fmt.Fprintf(stderr, "nono-wrapper: %s\n", socketMediationNotice)
	}
	if user != "" && user != profile {
		baseShown := shown
		shown, diagnostic, loadErr = showProfile(ctx, nono, user)
		if err := ctx.Err(); err != nil {
			return err
		}
		if loadErr != nil {
			fmt.Fprint(stderr, diagnostic)
			return fmt.Errorf("cannot load user profile %q; fix its file in your nono profiles directory", user)
		}
		userBlock, err := profileBlocksNetwork(shown, user)
		if err != nil {
			return err
		}
		block = block || userBlock
		composed, cleanup, err := composeUserOverrides(ctx, nono, profile, user, program, baseShown, shown, dropSocketMediation)
		if err != nil {
			return err
		}
		defer cleanup()
		if composed != "" {
			profileArgs = []string{"--profile", composed}
		} else {
			profileArgs = append(profileArgs, "--extends", user)
		}
	} else if dropSocketMediation {
		composed, cleanup, err := composeUserOverrides(ctx, nono, profile, "", program, shown, nil, true)
		if err != nil {
			return err
		}
		defer cleanup()
		profileArgs = []string{"--profile", composed}
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
	nonoArgs = append(nonoArgs, "--allow-cwd", "--read-file", program)
	nonoArgs = append(nonoArgs, sshAgentArgs()...)
	nonoArgs = append(nonoArgs, proxyArgs...)
	nonoArgs = append(nonoArgs, "--name", name, "--", program)
	if agent == "codex" {
		// https://nono.sh/docs/cli/clients/codex: one OS sandbox, approvals retained.
		nonoArgs = append(nonoArgs, "--no-daemon", "--sandbox=danger-full-access", "--ask-for-approval=on-request")
	}
	nonoArgs = append(nonoArgs, args...)
	if err := ctx.Err(); err != nil {
		return err
	}
	return runSession(ctx, nono, nonoArgs, agent, name, stderr)
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

// officialProfileOnly reports whether nono lists the official pack's profile
// for the agent but not ours.
func officialProfileOnly(ctx context.Context, nono, ours, agent string) bool {
	var profiles []struct {
		Name string `json:"name"`
		Pack string `json:"pack"`
	}
	listed, err := exec.CommandContext(ctx, nono, "profile", "list", "--json").Output()
	if err != nil || json.Unmarshal(listed, &profiles) != nil {
		return false
	}
	official := false
	for _, profile := range profiles {
		if profile.Name == ours {
			return false
		}
		official = official || profile.Name == officialPacks[agent] && profile.Pack == "nolabs-ai/"+officialPacks[agent]
	}
	return official
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
