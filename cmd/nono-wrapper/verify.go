// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"
)

func verify(ctx context.Context, agents []string, output io.Writer) error {
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
			return cli.Exit(tool+" is required for verification", exitNotFound)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	work, err := os.MkdirTemp("", "nono-wrapper-verify-")
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
		{"the home directory cannot be listed", `ls "$HOME"`, false},
		{"your configuration directory cannot be listed", `ls "${XDG_CONFIG_HOME:-$HOME/.config}"`, false},
		{"the network is unreachable", "curl -s -m 5 -o /dev/null https://example.com", false},
	}
	failed := false
	for _, agent := range agents {
		fmt.Fprintf(output, "%s under nono (profile %s-offline):\n", agent, agent)
		for _, check := range checks {
			cmd := exec.CommandContext(ctx, nono, "run", "--silent", "--profile", agent+"-offline", "--allow-cwd", "--", "/bin/sh", "-c", check.script)
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
					break
				}
			}
		}
	}
	if failed {
		return cli.Exit("", exitFinding)
	}
	return nil
}
