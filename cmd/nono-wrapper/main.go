// SPDX-FileCopyrightText: The nono-wrapper Authors
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
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/diggsweden/nono-wrapper/docs/manpages"
	"github.com/urfave/cli/v3"
)

// Exit statuses are a documented contract (see docs/usage.md, "Exit status", and the
// manual). The wrapper's own failures use sysexits(3) values. The agent
// commands follow env(1) and timeout(1) instead: the agent's own status passes
// through, so the wrapper's failures there use statuses agents rarely do.
const (
	exitFinding  = 1   // doctor: a check failed; changes: changed files listed
	exitUsage    = 64  // EX_USAGE: unknown command or flag, or wrong arguments
	exitDataErr  = 65  // EX_DATAERR: a malformed value, such as a host name
	exitNoInput  = 66  // EX_NOINPUT: a named file does not exist
	exitSoftware = 70  // EX_SOFTWARE: an unexpected failure
	exitNoPerm   = 77  // EX_NOPERM: permission denied or refused by policy
	exitConfig   = 78  // EX_CONFIG: the user configuration is unusable
	exitLaunch   = 125 // opencode, codex: the wrapper could not start the session, a refused start directory included
	exitNotFound = 127 // launch/doctor prerequisite not on PATH; missing doctor agents use 1
)

const (
	docsURL     = "https://github.com/diggsweden/nono-wrapper"
	issuesURL   = docsURL + "/issues"
	helpLinks   = "Docs:   " + docsURL + "\nIssues: " + issuesURL + "\nManual: nono-wrapper man"
	profilesDir = "$XDG_CONFIG_HOME/nono/profiles, normally ~/.config/nono/profiles"
	// Output names documents by URL: a binary-only installation has no checkout.
	installDoc  = docsURL + "/blob/main/docs/install.md"
	openCodeDoc = docsURL + "/blob/main/docs/opencode.md"
	appArmorDoc = docsURL + "/blob/main/README.md#ubuntu-apparmor-setup"
)

// conciseHelp is what a bare invocation prints, on stderr, before exiting 64.
const conciseHelp = `Run OpenCode and Codex with nono's sandbox profiles.

Examples:
  nono-wrapper doctor           check the installation
  nono-wrapper login opencode   sign in once
  nono-wrapper opencode         start OpenCode in this directory
  nono-wrapper codex            start Codex in this directory

OpenCode 2 uses private loopback on Linux; see 'nono-wrapper help opencode'.

Run 'nono-wrapper --help' for all commands and options.

` + helpLinks + "\n"

const rootDescription = `Examples:
  nono-wrapper login codex                           sign in once
  nono-wrapper opencode                              start OpenCode in this directory
  nono-wrapper codex                                 start Codex in this directory
  nono-wrapper run --read /path/to/reference codex    temporary read-only grant
  nono-wrapper doctor                                check the installation
  nono-wrapper config opencode allow-domain api.github.com

Help forwarding:
  nono-wrapper help codex       wrapper help
  nono-wrapper codex --help     Codex help, run under the sandbox
  nono-wrapper run --help       sandbox options; put these before the agent

Environment:
  NONO_PROFILE                 profile to use instead of AGENT-chatgpt
  NONO_WRAPPER_ROOT            absolute workspace root; allows start in it or below it
  NONO_WRAPPER_ALLOW_CONFIG=1  include configuration repositories without asking
  NONO_WRAPPER_SSH_AGENT=0     do not share the SSH agent socket
  GIT_SSH_COMMAND, GIT_SSH      explicit Git SSH transport overrides
  GIT_SSH_VARIANT              explicit Git SSH client variant
  CONTAINER_HOST, CONTAINER_CONNECTION  explicit container transport overrides
  HTTPS_PROXY, HTTP_PROXY      upstream proxy for the sandbox's network proxy

Without NONO_WRAPPER_ROOT, agents start only below ~/development.

Exit status:
  0      success
  1      doctor: a check failed, including a missing selected agent; changes: changed files listed
  64     usage error: unknown command or flag, or wrong arguments
  65     a malformed value, such as an invalid host name
  66     a named file or directory does not exist
  70     an unexpected failure
  77     permission denied, or refused by policy
  78     the user configuration is unusable
  125    opencode, codex: the wrapper could not start the session, a refused start directory included
  127    opencode/codex: nono or agent not on PATH; doctor: nono or a probe utility not on PATH
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
		HideVersion: true,
		Flags: []cli.Flag{&cli.BoolFlag{Name: "version", Aliases: []string{"V", "v"}, Usage: "Show version", Action: func(_ context.Context, cmd *cli.Command, value bool) error {
			if value {
				cli.ShowVersion(cmd.Root())
				return cli.Exit("", 0)
			}
			return nil
		}}},
		// Shell completion uses the framework's action-free completion protocol.
		EnableShellCompletion: true,
		Description:           rootDescription,
		// Return errors to main so helper cleanup runs before exiting.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		OnUsageError:   usageError,
		Action:         helpOrUnknown,
		Commands: []*cli.Command{
			runCommand(),
			loginCommand(),
			installProfilesCommand(),
			agentCommand("opencode", "OpenCode"),
			agentCommand("codex", "Codex"),
			configCommand(),
			manCommand(),
			sshProxyCommand(),
			{
				Name: "doctor", Usage: "Check the installed tools and offline profiles without an agent login",
				Description: "Reports the nono version and resolved agent paths, and checks the project root and proxy setting. Resolves Mise shims without executing agents; an inactive shim fails the check. With no arguments, checks both agents; name one for a single-agent installation. A missing selected agent fails the check.\n\nRuns short probes under the installed offline profiles: the project and temporary directory must be writable; the home directory, configuration directory and network must be refused. Login, additional access rules and the full agent launch are not tested.\n\nExits 0 when every check passes and 1 when a check fails; see 'nono-wrapper --help' for the other statuses.\n\nExamples:\n  nono-wrapper doctor\n  nono-wrapper doctor codex",
				ArgsUsage:   "[opencode|codex ...]", OnUsageError: usageError,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return doctor(ctx, cmd.Args().Slice(), cmd.Root().Writer)
				},
			},
			{
				Name: "changes", Usage: "List selected project files changed since a marker",
				Description: "MARKER is a file you create before a session, for example with mktemp. Lists hooks, CI workflows, editor tasks, package scripts and agent or tool settings in DIRECTORY (default: the current directory) that changed after MARKER was last modified.\n\nFor nono's own record of the session, see 'nono audit'.\n\nExits 0 when nothing is listed and 1 when changed files are listed; see 'nono-wrapper --help' for the other statuses.\n\nExample:\n  marker=$(mktemp); nono-wrapper codex; nono-wrapper changes \"$marker\" .; rm -f \"$marker\"",
				ArgsUsage:   "MARKER [DIRECTORY]", OnUsageError: usageError,
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.NArg() < 1 || cmd.NArg() > 2 {
						return cli.Exit("usage: nono-wrapper changes MARKER [DIRECTORY]", exitUsage)
					}
					directory := "."
					if cmd.NArg() == 2 {
						directory = cmd.Args().Get(1)
					}
					return changes(ctx, cmd.Args().First(), directory, cmd.Root().Writer)
				},
			},
		},
	}
}

// Forge CI injects these for release binaries; checkout builds use Go metadata.
var version, commit string

func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	if version != "" {
		label := version
		if commit != "" {
			label += " " + commit[:min(len(commit), 12)]
		}
		if ok {
			label += " (" + info.GoVersion + ")"
		}
		return label
	}
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

func manCommand() *cli.Command {
	return &cli.Command{
		Name: "man", Usage: "Print the manual",
		Description:  "Prints the embedded manual that 'man nono-wrapper' shows, including outside a checkout and on systems without man(1).",
		OnUsageError: usageError,
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return cli.Exit("man takes no arguments", exitUsage)
			}
			_, err := io.WriteString(cmd.Root().Writer, manpages.Manual)
			return err
		},
	}
}

func agentCommand(agent, name string) *cli.Command {
	examples := fmt.Sprintf("Examples:\n  %-34s sign in once\n  %-34s start %s in this directory", "nono-wrapper login "+agent, "nono-wrapper "+agent, name)
	if agent == "opencode" {
		examples = "Sign in once with 'nono-wrapper login opencode'.\n\nOpenCode 2 (Linux):\n  nono-wrapper opencode\n  NONO_PROFILE=opencode-offline nono-wrapper opencode session list\n\nThe installed CLI's --version is checked inside the sandbox before the requested command. Only major 2 is accepted; other or unrecognized versions are refused with an instruction to use OpenCode 2.\n\nThe wrapper adds --standalone and nono --isolate net,loopback. OpenCode owns the private server and stops it with the client. User/network namespaces must be permitted; doctor checks startup. No automatic fallback opens host ports. WSL2 support needs native validation.\n\nFor host requirements, explicit servers and macOS limits, see " + openCodeDoc + "."
	}
	return &cli.Command{
		Name: agent, Usage: "Run " + name + " under nono", ArgsUsage: "[agent arguments...]",
		Description: "All arguments, including --help, go to " + name + "; this text is 'nono-wrapper help " + agent + "'. The profile is NONO_PROFILE or " + agent + "-chatgpt, plus the optional " + agent + "-user.jsonc in your nono profiles directory (" + profilesDir + "). A session report follows on stderr after exit. To list project files the session changed, create a marker file before you start; see 'nono-wrapper help changes'.\n\n" + examples,
		// In particular, neither --help nor a positional 'help' belongs to us.
		SkipFlagParsing: true,
		HideHelp:        true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			err := launch(ctx, agent, cmd.Args().Slice(), cmd.Root().ErrWriter)
			return agentLaunchError(err)
		},
	}
}

func agentLaunchError(err error) error {
	var exit cli.ExitCoder
	if err != nil && !errors.As(err, &exit) {
		return cli.Exit(err, exitLaunch)
	}
	return err
}
