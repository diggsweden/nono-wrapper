// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/urfave/cli/v3"
)

// Exit statuses are a documented contract (see README, "Exit status", and the
// manual). The wrapper's own failures use sysexits(3) values. The agent
// commands follow env(1) and timeout(1) instead: the agent's own status passes
// through, so the wrapper's failures there use statuses agents rarely do.
const (
	exitFinding  = 1   // verify: a check failed; audit: changed files listed
	exitUsage    = 64  // EX_USAGE: unknown command or flag, or wrong arguments
	exitDataErr  = 65  // EX_DATAERR: a malformed value, such as a host name
	exitNoInput  = 66  // EX_NOINPUT: a named file does not exist
	exitSoftware = 70  // EX_SOFTWARE: an unexpected failure
	exitNoPerm   = 77  // EX_NOPERM: permission denied or refused by policy
	exitConfig   = 78  // EX_CONFIG: the user configuration is unusable
	exitLaunch   = 125 // opencode, codex: the wrapper could not start the session
	exitNotFound = 127 // nono, the agent or a required tool is not on PATH
)

const (
	docsURL   = "https://codefloe.com/itiquette/nono-wrapper"
	issuesURL = "https://codefloe.com/itiquette/nono-wrapper/issues"
	helpLinks = "Docs:   " + docsURL + "\nIssues: " + issuesURL + "\nManual: man nono-wrapper"
)

// conciseHelp is what a bare invocation prints, on stderr, before exiting 64.
const conciseHelp = `Run OpenCode and Codex with nono's sandbox profiles.

Examples:
  nono-wrapper opencode    start OpenCode in this directory
  nono-wrapper codex       start Codex in this directory
  nono-wrapper verify      check the installed profiles

Run 'nono-wrapper --help' for all commands and options.

` + helpLinks + "\n"

const rootDescription = `Examples:
  nono-wrapper opencode                              start OpenCode in this directory
  nono-wrapper codex                                 start Codex in this directory
  nono-wrapper verify                                check the installed profiles
  nono-wrapper config opencode add-host api.github.com

Environment:
  NONO_PROFILE              profile to use instead of AGENT-chatgpt
  NONO_WRAPPER_SSH_AGENT=0  do not share the SSH agent socket
  HTTPS_PROXY, HTTP_PROXY   upstream proxy for the sandbox's network proxy

Exit status:
  0      success
  1      verify: a check failed; audit: changed files listed
  64     usage error: unknown command or flag, or wrong arguments
  65     a malformed value, such as an invalid host name
  66     a named file or directory does not exist
  70     an unexpected failure
  77     permission denied, or refused by policy
  78     the user configuration is unusable
  125    opencode, codex: the wrapper could not start the session
  127    nono, the agent or a required tool is not on PATH
  128+N  stopped by signal N
  Otherwise opencode and codex exit with the agent's status.

` + helpLinks

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// interrupted records which signal cancelled the run, for its 128+N status.
type interrupted struct{ signal syscall.Signal }

func (i interrupted) Error() string { return "interrupted by " + i.signal.String() }

// run is the single exit funnel: deferred cleanup has run when it returns.
func run(args []string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
			cancel(interrupted{sig.(syscall.Signal)})
		case <-ctx.Done():
		}
	}()

	root := command()
	root.Writer, root.ErrWriter = stdout, stderr
	err := root.Run(ctx, args)
	if err == nil {
		return 0
	}
	if err.Error() != "" {
		fmt.Fprintln(stderr, "nono-wrapper:", err)
	}
	return exitCode(ctx, err)
}

// exitCode classifies by type, never by message text.
func exitCode(ctx context.Context, err error) int {
	var exit cli.ExitCoder
	var signalled interrupted
	switch {
	case errors.As(err, &exit):
		code := exit.ExitCode()
		// urfave/cli reports an unknown "help TOPIC" with its own status 3.
		// An agent's status 3 arrives without a message and passes through.
		if code == 3 && err.Error() != "" {
			return exitUsage
		}
		return code
	case errors.As(context.Cause(ctx), &signalled):
		return 128 + int(signalled.signal)
	case errors.As(err, new(invalidConfig)):
		return exitConfig
	case errors.Is(err, fs.ErrNotExist):
		return exitNoInput
	case errors.Is(err, fs.ErrPermission):
		return exitNoPerm
	}
	return exitSoftware
}

// invalidConfig marks a user configuration nono-wrapper cannot use.
type invalidConfig struct{ error }

func (err invalidConfig) Unwrap() error { return err.error }

func command() *cli.Command {
	return &cli.Command{
		Name:        "nono-wrapper",
		Usage:       "Run OpenCode and Codex with nono's sandbox profiles",
		Version:     buildVersion(),
		Description: rootDescription,
		// Return errors to main so helper cleanup runs before exiting.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		OnUsageError:   usageError,
		Action:         helpOrUnknown,
		Commands: []*cli.Command{
			agentCommand("opencode", "Run OpenCode under nono"),
			agentCommand("codex", "Run Codex under nono"),
			configCommand(),
			manCommand(),
			{
				Name: "verify", Usage: "Check installed offline profiles without an agent login",
				Description: "Runs short probes under each agent's offline profile: the project and the temporary directory must be writable; the home directory, your configuration directory and the network must be refused.\n\nExits 0 when every check passes and 1 when a check fails; see 'nono-wrapper --help' for the other statuses.\n\nExamples:\n  nono-wrapper verify\n  nono-wrapper verify codex",
				ArgsUsage:   "[opencode|codex ...]", OnUsageError: usageError,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return verify(ctx, cmd.Args().Slice(), cmd.Root().Writer)
				},
			},
			{
				Name: "audit", Usage: "Report selected project files changed since a marker",
				Description: "MARKER is a file you create before a session, for example with mktemp. Lists hooks, CI workflows, editor tasks, package scripts and agent or tool settings in DIRECTORY (default: the current directory) that changed after MARKER was last modified.\n\nThis is not nono's session audit; see 'nono audit'.\n\nExits 0 when nothing is listed and 1 when changed files are listed; see 'nono-wrapper --help' for the other statuses.\n\nExample:\n  marker=$(mktemp); nono-wrapper codex; nono-wrapper audit \"$marker\" .; rm -f \"$marker\"",
				ArgsUsage:   "MARKER [DIRECTORY]", OnUsageError: usageError,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 1 || cmd.NArg() > 2 {
						return cli.Exit("usage: nono-wrapper audit MARKER [DIRECTORY]", exitUsage)
					}
					directory := "."
					if cmd.NArg() == 2 {
						directory = cmd.Args().Get(1)
					}
					return audit(ctx, cmd.Args().First(), directory, cmd.Root().Writer)
				},
			},
		},
	}
}

// Use metadata already embedded by Go; development builds are labelled honestly.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" {
		version += " " + revision[:min(len(revision), 12)]
		if modified {
			version += "-dirty"
		}
	}
	return version + " (" + info.GoVersion + ")"
}

func usageError(_ context.Context, cmd *cli.Command, err error, _ bool) error {
	message := err.Error()
	if flag, ok := strings.CutPrefix(message, "flag provided but not defined: "); ok {
		var names []string
		for _, defined := range append(cmd.VisibleFlags(), cli.HelpFlag) {
			for _, name := range defined.Names() {
				prefix := "--"
				if len(name) == 1 {
					prefix = "-"
				}
				names = append(names, prefix+name)
			}
		}
		message += didYouMean(strings.TrimLeft(flag, "-"), names, "-")
	}
	return cli.Exit(fmt.Sprintf("%s; see %s --help", message, cmd.FullName()), exitUsage)
}

// A command that only groups subcommands needs one: run bare, it shows its
// help on stderr and exits 64. Anything else typed there is a mistake; say
// so instead of the framework's "No help topic".
func helpOrUnknown(_ context.Context, cmd *cli.Command) error {
	if cmd.Args().Present() {
		var names []string
		for _, sub := range cmd.VisibleCommands() {
			names = append(names, sub.Name)
		}
		provided := cmd.Args().First()
		return cli.Exit(fmt.Sprintf("unknown command %q%s; see %s --help", provided, didYouMean(provided, names, ""), cmd.FullName()), exitUsage)
	}
	root := cmd.Root()
	if root == cmd {
		fmt.Fprint(root.ErrWriter, conciseHelp)
		return cli.Exit("", exitUsage)
	}
	writer := root.Writer
	root.Writer = root.ErrWriter
	defer func() { root.Writer = writer }()
	if err := cli.ShowSubcommandHelp(cmd); err != nil {
		return err
	}
	return cli.Exit("", exitUsage)
}

// didYouMean suggests the closest name, and only a close one: it never runs
// the suggestion, because guessing what a mistyped command meant is unsafe.
func didYouMean(provided string, names []string, trim string) string {
	best, bestDistance := "", 3
	for _, name := range names {
		if distance := editDistance(provided, strings.TrimLeft(name, trim)); distance < bestDistance {
			best, bestDistance = name, distance
		}
	}
	if best == "" || bestDistance >= max(len(provided), 2) {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

// manualPath is the manual source in a checkout. install-local renders it for
// man(1); 'nono-wrapper man' prints it where man(1) is missing.
const manualPath = "docs/manpages/nono-wrapper.1.md"

func manCommand() *cli.Command {
	return &cli.Command{
		Name: "man", Usage: "Print the manual",
		Description:  "Prints the manual that 'man nono-wrapper' shows, for systems without man(1). The source ships in a checkout; elsewhere use 'man nono-wrapper' or the documentation.",
		OnUsageError: usageError,
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return cli.Exit("man takes no arguments", exitUsage)
			}
			for _, path := range manualCandidates() {
				if manual, err := os.ReadFile(path); err == nil {
					_, err = cmd.Root().Writer.Write(manual)
					return err
				}
			}
			// A missing manual never fails the run.
			fmt.Fprintf(cmd.Root().ErrWriter, "nono-wrapper: the manual source is not in this installation; run 'man nono-wrapper' or see %s\n", docsURL)
			return nil
		},
	}
}

// manualCandidates finds a checkout only relative to the executable: the
// working directory is the caller's, not a trusted checkout.
func manualCandidates() []string {
	executable, err := os.Executable()
	if err != nil {
		return nil
	}
	directory := filepath.Dir(executable)
	return []string{
		filepath.Join(directory, manualPath),             // binary at the checkout root
		filepath.Join(directory, "..", "..", manualPath), // .cache/bin/nono-wrapper
	}
}

func agentCommand(agent, usage string) *cli.Command {
	return &cli.Command{
		Name: agent, Usage: usage, ArgsUsage: "[agent arguments...]",
		Description: "Uses NONO_PROFILE or " + agent + "-chatgpt, plus optional " + agent + "-user.jsonc in your nono profiles directory. Prints a session audit summary on stderr after exit. All arguments, including --help, go to the agent.",
		// In particular, neither --help nor a positional 'help' belongs to us.
		SkipFlagParsing: true,
		HideHelp:        true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			err := launch(ctx, agent, cmd.Args().Slice(), cmd.Root().ErrWriter)
			var exit cli.ExitCoder
			if err != nil && !errors.As(err, &exit) {
				return cli.Exit(err, exitLaunch)
			}
			return err
		},
	}
}
