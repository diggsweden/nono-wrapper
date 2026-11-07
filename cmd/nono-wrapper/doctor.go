// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

func doctor(ctx context.Context, agents []string, output io.Writer) error {
	if len(agents) == 0 {
		agents = []string{"opencode", "codex"}
	}
	for _, agent := range agents {
		if _, ok := agentInstallURLs[agent]; !ok {
			return cli.Exit(fmt.Sprintf("unknown agent %q (opencode, codex)", agent), exitUsage)
		}
	}
	nono, err := exec.LookPath("nono")
	if err != nil {
		return cli.Exit("nono is not on PATH; install: https://nono.sh/docs/cli/getting_started/installation", exitNotFound)
	}
	nono, err = filepath.Abs(nono)
	if err != nil {
		return err
	}
	// The sandbox probes use these utilities; missing tools must not look like denials.
	for _, tool := range []string{"curl", "ls", "touch"} {
		if _, err := exec.LookPath(tool); err != nil {
			return cli.Exit(tool+" is required for the checks", exitNotFound)
		}
	}
	failed := !checkSetup(ctx, nono, agents, output)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	work, err := os.MkdirTemp("", "nono-wrapper-doctor-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	work, err = filepath.Abs(work)
	if err != nil {
		return err
	}
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		return err
	}
	project, temp := filepath.Join(work, "project"), filepath.Join(work, "tmp")
	for _, directory := range []string{project, temp} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			return err
		}
	}
	checks := []struct {
		name, script string
		works        bool
	}{
		{"the sandbox starts and the project is writable", "command -v ls >/dev/null && command -v curl >/dev/null && touch ./written", true},
		{"the temporary directory is writable", `touch "$TMPDIR/written"`, true},
		{"the sandbox hides the home directory", `ls "$HOME"`, false},
		{"the sandbox hides your configuration directory", `ls "${XDG_CONFIG_HOME:-$HOME/.config}"`, false},
		{"the sandbox blocks the network", "curl -s -m 5 -o /dev/null https://example.com", false},
	}
	for _, agent := range agents {
		fmt.Fprintf(output, "%s under nono (profile %s-offline):\n", agent, agent)
		profile := agent + "-offline"
		shown, diagnostic, err := showProfile(ctx, nono, profile)
		if err != nil {
			// The sandbox checks would fail for this reason too, and their
			// namespace advice would point the wrong way.
			failed = true
			fmt.Fprintf(output, "  FAIL  profile %s cannot be loaded\n%s", profile, diagnostic)
			fmt.Fprintf(output, "  Install the bundled profiles with 'just install-local' from a nono-wrapper checkout; see %s.\n", installDoc)
			continue
		}
		fmt.Fprintf(output, "  profile SHA-256: %x (installed, resolved)\n", sha256.Sum256(shown))
		blocked, policyErr := profileBlocksNetwork(shown, profile)
		if policyErr != nil {
			failed = true
			fmt.Fprintf(output, "  FAIL  %s\n", policyErr)
			continue
		}
		if !blocked {
			failed = true
			fmt.Fprintf(output, "  FAIL  profile %q is not offline: network.block must be true\n", profile)
			continue
		}
		if socketMediationUnavailable(ctx, nono, profile, shown) {
			composed, cleanup, err := composeSessionPolicy(ctx, nono, profile, "", "", shown, nil, true, nil, nil)
			if err != nil {
				return err
			}
			defer cleanup()
			profile = composed
			fmt.Fprintf(output, "  note  %s\n", socketMediationNotice)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		privateLoopback := runtime.GOOS == "linux"
		for _, check := range checks {
			args := []string{"run", "--silent", "--profile", profile}
			if privateLoopback {
				// Use nono's actual namespace startup, also on WSL2. A kernel
				// version or sysctl alone cannot establish host support.
				args = append(args, "--isolate", "net,loopback")
			}
			args = append(args, "--allow-cwd", "--", "/bin/sh", "-c", check.script)
			cmd := exec.CommandContext(ctx, nono, args...)
			cmd.Dir = project
			cmd.Env = append(os.Environ(), "CODEX_HOME="+codexHome, "TMPDIR="+temp)
			cmd.WaitDelay = time.Second
			result, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var exit *exec.ExitError
			denied := errors.As(err, &exit) && exit.ExitCode() > 0 && exit.ExitCode() != 126 && exit.ExitCode() != 127
			if (check.works && err == nil) || (!check.works && denied) {
				fmt.Fprintf(output, "  ok    %s\n", check.name)
			} else {
				failed = true
				fmt.Fprintf(output, "  FAIL  %s\n%s", check.name, result)
				if err != nil && len(result) == 0 {
					fmt.Fprintln(output, err)
				}
				// Refusals are meaningful only after the positive controls succeed.
				if check.works {
					if privateLoopback {
						fmt.Fprintf(output, "  Bundled Linux sessions require private loopback. For Ubuntu AppArmor setup, see %s.\n", appArmorDoc)
						fmt.Fprintf(output, "  For other namespace requirements and explicit-server setup, see %s.\n", openCodeDoc)
					}
					break
				}
			}
		}
	}
	if failed {
		fmt.Fprintln(output, "Checks failed. Resolve the FAIL entries and rerun doctor; see 'nono-wrapper man' under TROUBLESHOOTING.")
		return cli.Exit("", exitFinding)
	}
	fmt.Fprintln(output, "Installed offline-profile checks passed.")
	fmt.Fprintln(output, "ChatGPT login, your additional access rules and the full agent launch were not tested.")
	if root, _, err := projectRoot(); err == nil {
		fmt.Fprintln(output, "Next: sign in once, then start the agent in a project below "+root+" (NONO_WRAPPER_ROOT changes it).")
		for _, agent := range agents {
			if agent == "opencode" {
				// ChatGPT sign-in needs a temporary explicit server; one command cannot show it.
				fmt.Fprintf(output, "  OpenCode: sign in with the explicit-server steps in %s,\n            then start 'nono-wrapper opencode'.\n", openCodeLoginDoc)
				continue
			}
			fmt.Fprintf(output, "  Codex: 'nono-wrapper codex %s', then 'nono-wrapper codex'.\n", agentLogin(agent))
		}
	}
	return nil
}

// checkSetup reports what testers met before the sandbox itself: the tools
// found, and the project root and proxy setting that every launch needs.
// Agents run only under nono, so their paths are resolved but not executed here.
func checkSetup(ctx context.Context, nono string, agents []string, output io.Writer) bool {
	fmt.Fprintln(output, "Setup:")
	fmt.Fprintf(output, "  wrapper: %s\n  nono executable: %s\n", buildVersion(), nono)
	ok := true
	if path, err := os.Executable(); err == nil {
		if file, err := os.Open(path); err == nil {
			hash := sha256.New()
			if _, err := io.Copy(hash, file); err == nil {
				fmt.Fprintf(output, "  wrapper executable: %s\n  wrapper SHA-256: %x\n", path, hash.Sum(nil))
			}
			file.Close()
		}
	}
	if defaults, err := loadRuntimeDefaults(); err == nil && runtime.GOOS == "linux" {
		if env := podmanSessionEnvironment("opencode-offline", defaults); len(env) > 0 {
			fmt.Fprintf(output, "  Podman transport: %s (existing user-owned socket; service not probed)\n", env[0])
		}
	}
	if version, err := exec.CommandContext(ctx, nono, "--version").Output(); err == nil {
		fmt.Fprintf(output, "  ok    %s\n", strings.TrimSpace(string(version)))
	} else {
		ok = false
		fmt.Fprintf(output, "  FAIL  nono --version failed: %v; check nono on PATH and its Mise configuration, if used\n", err)
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			fmt.Fprintf(output, "        %s\n", strings.ReplaceAll(strings.TrimSpace(string(exit.Stderr)), "\n", "\n        "))
		}
	}
	for _, agent := range agents {
		path, err := exec.LookPath(agent)
		if err != nil {
			ok = false
			fmt.Fprintf(output, "  FAIL  %s is not on PATH; install it: %s\n", agent, agentInstallURLs[agent])
			continue
		}
		resolved, err := resolveExecutable(ctx, agent, path)
		if err != nil {
			ok = false
			fmt.Fprintf(output, "  FAIL  %s\n", strings.ReplaceAll(err.Error(), "\n", "\n        "))
			continue
		}
		fmt.Fprintf(output, "  ok    %s found at %s\n", agent, resolved.path)
	}
	root, _, err := projectRoot()
	if err == nil {
		fmt.Fprintf(output, "  ok    project root %s\n", root)
	} else {
		ok = false
		fmt.Fprintf(output, "  FAIL  %s\n", strings.ReplaceAll(err.Error(), "\n", "\n        "))
	}
	if _, credentials, err := upstreamProxyArgs(false); err != nil {
		ok = false
		fmt.Fprintf(output, "  FAIL  %s\n", err)
	} else if credentials != "" {
		fmt.Fprintf(output, "  note  %s contains a user name or password; nono connects to the proxy without them\n", credentials)
	}
	return ok
}
