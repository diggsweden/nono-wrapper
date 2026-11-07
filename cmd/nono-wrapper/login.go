// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/urfave/cli/v3"
)

func loginCommand() *cli.Command {
	stop := 1
	return &cli.Command{
		Name: "login", Usage: "Sign in to an agent with your ChatGPT account",
		ArgsUsage: "opencode|codex [codex login arguments...]", StopOnNthArg: &stop,
		Description: "Run it once per agent, in a directory where agents may start.\n\n" +
			"OpenCode: starts a temporary OpenCode server on a free loopback port inside one sandbox session, signs in through your browser, then stops the server. " +
			"Only that session gets the port and a browser-callback listener; the port grant also permits outbound TCP on that port number. Nothing is saved to your user configuration.\n\n" +
			"Codex: runs 'codex login' with any arguments that follow.\n\n" +
			"Examples:\n  nono-wrapper login opencode\n  nono-wrapper login codex\n  nono-wrapper login codex --device-auth",
		OnUsageError: usageError,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) == 0 || (args[0] != "opencode" && args[0] != "codex") {
				return cli.Exit("usage: nono-wrapper login opencode|codex [codex login arguments...]", exitUsage)
			}
			agent, args := args[0], args[1:]
			if agent == "codex" {
				return agentLaunchError(launch(ctx, agent, append([]string{"login"}, args...), cmd.Root().ErrWriter))
			}
			if len(args) != 0 {
				return cli.Exit("login opencode takes no arguments", exitUsage)
			}
			port, err := freeLoopbackPort()
			if err != nil {
				return err
			}
			options := launchOptions{loginPort: port, nonoArgs: []string{"--open-port", port, "--listen-port", "0"}}
			return agentLaunchError(launchWithOptions(ctx, agent, nil, cmd.Root().ErrWriter, options))
		},
	}
}

// accountCommand reports Codex's own sign-in commands, which need no login.
func accountCommand(args []string) bool {
	return len(args) > 0 && (args[0] == "login" || args[0] == "logout")
}

// savedLogin looks for the file each agent keeps its ChatGPT sign-in in, in
// the state directory the session shares. It only checks that the file
// exists: the agent never runs on the host.
func savedLogin(agent string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", true
	}
	var path string
	if agent == "codex" {
		state := os.Getenv("CODEX_HOME")
		if state == "" {
			state = filepath.Join(home, ".codex")
		}
		path = filepath.Join(state, "auth.json")
	} else {
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		path = filepath.Join(data, "opencode", "auth.json")
	}
	_, err = os.Stat(path)
	return path, !errors.Is(err, fs.ErrNotExist)
}

// freeLoopbackPort asks the kernel for an unused port. Another program could
// take it before the server binds; the server then fails and login says so.
func freeLoopbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}
