// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

type launchOptions struct {
	profile           string
	nonoArgs          []string
	paths             []pathRule
	blockNet          bool
	silent            bool
	nativeDiagnostics bool
}

func runCommand() *cli.Command {
	stop := 1
	command := &cli.Command{
		Name: "run", Usage: "Run an agent with temporary nono-style sandbox options",
		ArgsUsage: "AGENT [--] [agent arguments...]", StopOnNthArg: &stop,
		// Native nono takes one value per occurrence. Commas in paths and URLs
		// are literal data, not a second grant or a second destination.
		DisableSliceFlagSeparator: true,
		Description:               "Put sandbox options before AGENT (opencode or codex); arguments after AGENT belong to the agent, including --help. An optional -- after AGENT separates agent arguments. Grants are additive and apply only to this session; inherited deny rules and wrapper safety checks still apply. Use config for persistent changes. The current directory remains the workspace.\n\nExamples:\n  nono-wrapper run --profile codex-offline codex -- features list\n  nono-wrapper run --allow /path/to/shared-project opencode\n  nono-wrapper run --read /path/to/reference codex -- --help",
		OnUsageError:              usageError,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "profile", Aliases: []string{"p"}, Usage: "Profile name or path; overrides NONO_PROFILE"},
			&cli.BoolFlag{Name: "block-net", Usage: "Block network access for this session"},
			&cli.StringSliceFlag{Name: "isolate", Usage: "Linux namespaces: net,loopback,ipc,uts (comma-separated, repeatable); nono validates host support and conflicts"},
			&cli.BoolFlag{Name: "silent", Aliases: []string{"s"}, Usage: "Suppress nono output and the wrapper session report, not agent output"},
			&cli.BoolFlag{Name: "no-diagnostics", Value: true, Usage: "Suppress native footer and profile-save prompts; =false restores them"},
			&cli.StringSliceFlag{Name: "allow-domain", Usage: "Allow a domain or HTTPS URL rule for this session (repeatable)"},
			&cli.StringSliceFlag{Name: "open-port", Usage: "Allow TCP connect and listen on a port, including non-loopback addresses (repeatable)"},
		},
	}
	for _, field := range filesystemFields {
		name := map[string]string{"read_file": "read-file", "write_file": "write-file", "allow_file": "allow-file"}[field.name]
		if name == "" {
			name = field.name
		}
		flag := &cli.StringSliceFlag{Name: name, Usage: fmt.Sprintf("Grant %s access to a path for this session (repeatable)", field.access)}
		switch name {
		case "allow":
			flag.Aliases = []string{"a"}
		case "read":
			flag.Aliases = []string{"r"}
		case "write":
			flag.Aliases = []string{"w"}
		}
		command.Flags = append(command.Flags, flag)
	}
	command.Action = func(ctx context.Context, cmd *cli.Command) error {
		args := cmd.Args().Slice()
		if len(args) == 0 || (args[0] != "opencode" && args[0] != "codex") {
			return cli.Exit("usage: nono-wrapper run [options] opencode|codex [--] [agent arguments...]", exitUsage)
		}
		options, err := runOptions(cmd)
		if err != nil {
			return err
		}
		agent, args := args[0], args[1:]
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		err = launchWithOptions(ctx, agent, args, cmd.Root().ErrWriter, options)
		return agentLaunchError(err)
	}
	return command
}

func runOptions(cmd *cli.Command) (launchOptions, error) {
	options := launchOptions{profile: cmd.String("profile"), blockNet: cmd.Bool("block-net"), silent: cmd.Bool("silent"), nativeDiagnostics: !cmd.Bool("no-diagnostics")}
	if cmd.IsSet("profile") && options.profile == "" {
		return options, cli.Exit("--profile requires a non-empty name or path", exitUsage)
	}
	if options.blockNet && (len(cmd.StringSlice("allow-domain")) != 0 || len(cmd.StringSlice("open-port")) != 0) {
		return options, cli.Exit("--block-net cannot be combined with --allow-domain or --open-port", exitUsage)
	}
	for _, name := range []string{"block-net", "silent"} {
		if cmd.Bool(name) {
			options.nonoArgs = append(options.nonoArgs, "--"+name)
		}
	}
	for _, name := range []string{"allow-domain", "open-port", "isolate"} {
		for _, value := range cmd.StringSlice(name) {
			if value == "" {
				return options, cli.Exit("--"+name+" requires a non-empty value", exitDataErr)
			}
			if name == "open-port" {
				if _, err := parseAllowance("port", value, "*", true); err != nil {
					return options, cli.Exit(err.Error(), exitDataErr)
				}
			}
			options.nonoArgs = append(options.nonoArgs, "--"+name, value)
		}
	}
	for _, field := range filesystemFields {
		name := field.name
		if field.file {
			name = map[string]string{"read_file": "read-file", "write_file": "write-file", "allow_file": "allow-file"}[name]
		}
		for _, raw := range cmd.StringSlice(name) {
			if raw == "" {
				return options, cli.Exit("--"+name+" requires a non-empty path", exitDataErr)
			}
			path, err := filepath.Abs(raw)
			if err != nil {
				return options, err
			}
			path, err = canonicalConfigPath(path)
			if err != nil {
				return options, err
			}
			if field.access&pathWrite != 0 {
				if err := protectRuleWrite(path); err != nil {
					return options, cli.Exit(err, exitNoPerm)
				}
			}
			if !field.file {
				info, err := os.Stat(path)
				if err != nil {
					return options, quotePathError(err)
				}
				if !info.IsDir() {
					return options, cli.Exit(fmt.Sprintf("--%s requires a directory; use --%s-file for a single file", name, name), exitDataErr)
				}
				if err := checkConfigRepos(configScope{directory: path, subject: fmt.Sprintf("%q", path), access: field.access.String(), alternative: "Grant a narrower directory"}, cmd.Root().Reader, cmd.Root().ErrWriter, interactiveInput()); err != nil {
					return options, cli.Exit(err, exitNoPerm)
				}
			}
			options.paths = append(options.paths, pathRule{path, field.access, field.file})
			options.nonoArgs = append(options.nonoArgs, "--"+name, path)
		}
	}
	return options, nil
}
