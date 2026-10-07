// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/urfave/cli/v3"
)

// The four conformance tests of the CLI convention profile. They run the
// wrapper's own commands in-process through run(), the single exit funnel.
// Agent commands are covered by the launcher tests in test/.

func runCLI(t *testing.T, args ...string) (stdout, stderr string, status int) {
	t.Helper()
	var out, errOut bytes.Buffer
	status = run(append([]string{"nono-wrapper"}, args...), &out, &errOut)
	return out.String(), errOut.String(), status
}

// isolate gives each case its own configuration and a normal terminal session.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("NONO_SESSION_ID", "")
	t.Setenv("NONO_CAP_FILE", "")
	return home
}

func TestExitCodeContract(t *testing.T) {
	for _, test := range []struct {
		name   string
		setup  func(t *testing.T, home string)
		args   []string
		status int
	}{
		{name: "help", args: []string{"--help"}, status: 0},
		{name: "help command", args: []string{"help", "verify"}, status: 0},
		{name: "version", args: []string{"--version"}, status: 0},
		{name: "manual", args: []string{"man"}, status: 0},
		{name: "show an empty configuration", args: []string{"config", "opencode", "show"}, status: 0},
		{name: "bare invocation requires a command", args: nil, status: exitUsage},
		{name: "bare group requires a command", args: []string{"config"}, status: exitUsage},
		{name: "unknown command", args: []string{"frobnicate"}, status: exitUsage},
		{name: "unknown flag", args: []string{"--frobnicate"}, status: exitUsage},
		{name: "unknown help topic", args: []string{"help", "frobnicate"}, status: exitUsage},
		{name: "wrong argument count", args: []string{"audit"}, status: exitUsage},
		{name: "malformed host", args: []string{"config", "opencode", "add-host", "https://example.org"}, status: exitDataErr},
		{name: "missing marker", args: []string{"audit", "missing-marker"}, status: exitNoInput},
		{name: "missing directory", args: []string{"config", "opencode", "add-dir", "/nonexistent/nono-wrapper"}, status: exitNoInput},
		{
			name: "refused inside a nono session",
			setup: func(t *testing.T, _ string) {
				t.Setenv("NONO_SESSION_ID", "session")
			},
			args:   []string{"config", "opencode", "add-host", "example.org"},
			status: exitNoPerm,
		},
		{
			name: "unusable user configuration",
			setup: func(t *testing.T, home string) {
				path := filepath.Join(home, ".config", "nono", "profiles", "opencode-user.jsonc")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			args:   []string{"config", "opencode", "show"},
			status: exitConfig,
		},
		{
			name: "relative configuration directory",
			setup: func(t *testing.T, _ string) {
				t.Setenv("XDG_CONFIG_HOME", "relative")
			},
			args:   []string{"config", "opencode", "show"},
			status: exitConfig,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := isolate(t)
			if test.setup != nil {
				test.setup(t, home)
			}
			stdout, stderr, status := runCLI(t, test.args...)
			if status != test.status {
				t.Fatalf("status=%d, want %d\nstdout=%q\nstderr=%q", status, test.status, stdout, stderr)
			}
		})
	}
}

func TestExitCodeClassification(t *testing.T) {
	interruptedBy := func(signal syscall.Signal) context.Context {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(interrupted{signal})
		return ctx
	}
	for _, test := range []struct {
		name   string
		ctx    context.Context
		err    error
		status int
	}{
		{"agent status passes through", t.Context(), cli.Exit("", 42), 42},
		{"agent status 3 passes through", t.Context(), cli.Exit("", 3), 3},
		{"framework help-topic status is a usage error", t.Context(), cli.Exit("No help topic for 'x'", 3), exitUsage},
		{"SIGINT", interruptedBy(syscall.SIGINT), context.Canceled, 130},
		{"SIGTERM", interruptedBy(syscall.SIGTERM), context.Canceled, 143},
		{"SIGHUP", interruptedBy(syscall.SIGHUP), context.Canceled, 129},
		{"missing file", t.Context(), &os.PathError{Op: "stat", Path: "x", Err: os.ErrNotExist}, exitNoInput},
		{"permission denied", t.Context(), &os.PathError{Op: "open", Path: "x", Err: os.ErrPermission}, exitNoPerm},
		{"unusable configuration", t.Context(), invalidConfig{errors.New("bad")}, exitConfig},
		{"unexpected failure", t.Context(), errors.New("unexpected"), exitSoftware},
	} {
		t.Run(test.name, func(t *testing.T) {
			if status := exitCode(test.ctx, test.err); status != test.status {
				t.Fatalf("exitCode=%d, want %d", status, test.status)
			}
		})
	}
}

// Exit 69 is for conditions a retry could resolve. Every failure the wrapper
// reports itself is permanent, so none of its statuses may be 69, and none may
// collide with another meaning.
func TestPermanentStatusesAreNeverRetryable(t *testing.T) {
	const exitUnavailable = 69
	statuses := []int{exitFinding, exitUsage, exitDataErr, exitNoInput, exitSoftware, exitNoPerm, exitConfig, exitLaunch, exitNotFound}
	for i, status := range statuses {
		if status == exitUnavailable || slices.Contains(statuses[i+1:], status) {
			t.Fatalf("status %d is retryable or not unique: %v", status, statuses)
		}
	}
}

func TestStreamSeparation(t *testing.T) {
	for _, test := range []struct {
		name       string
		args       []string
		wantStdout bool
	}{
		{"help goes to stdout", []string{"--help"}, true},
		{"a missing manual is reported on stderr", []string{"man"}, false},
		{"command output goes to stdout", []string{"config", "opencode", "show"}, true},
		{"bare invocation help goes to stderr", nil, false},
		{"usage error goes to stderr", []string{"--frobnicate"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolate(t)
			stdout, stderr, _ := runCLI(t, test.args...)
			if test.wantStdout && (stdout == "" || stderr != "") || !test.wantStdout && (stdout != "" || stderr == "") {
				t.Fatalf("stdout=%q\nstderr=%q", stdout, stderr)
			}
		})
	}
}

// The wrapper writes no colour at all, so it has no colour decision to test:
// output to a pipe must hold no escape sequence on either stream.
func TestNoANSIWhenPiped(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"help", "config"}, {"man"}, {"config", "opencode", "show"}, {"--frobnicate"}} {
		isolate(t)
		stdout, stderr, _ := runCLI(t, args...)
		if strings.ContainsRune(stdout+stderr, 0x1b) {
			t.Fatalf("%v wrote an escape sequence:\nstdout=%q\nstderr=%q", args, stdout, stderr)
		}
	}
}

// Every 'nono-wrapper ...' invocation the help and the manual show must name
// real commands and flags.
func TestHelpTruth(t *testing.T) {
	manual, err := os.ReadFile(filepath.Join("..", "..", manualPath))
	if err != nil {
		t.Fatal(err)
	}
	root := command()
	texts := []string{conciseHelp, string(manual)}
	var collect func(*cli.Command)
	collect = func(cmd *cli.Command) {
		texts = append(texts, cmd.Description, cmd.Usage, cmd.ArgsUsage)
		for _, sub := range cmd.Commands {
			collect(sub)
		}
	}
	collect(root)

	// An invocation starts a line (examples), follows "; " (a chained
	// example) or is quoted; elsewhere "nono-wrapper" is prose.
	invocation := regexp.MustCompile(`(?m)(?:^\s*|; |')nono-wrapper((?: (?:-{1,2}[a-z][a-z-]*|[a-z][a-z-]*))+)`)
	checked := 0
	for _, text := range texts {
		for _, match := range invocation.FindAllStringSubmatch(text, -1) {
			checked++
			cmd := root
			for _, word := range strings.Fields(match[1]) {
				if strings.HasPrefix(word, "-") {
					if !hasFlag(cmd, strings.TrimLeft(word, "-")) {
						t.Errorf("%q: %s has no flag %s", match[0], cmd.FullName(), word)
					}
					continue
				}
				if cmd.SkipFlagParsing || len(cmd.Commands) == 0 {
					break // the rest are arguments
				}
				sub := cmd.Command(word)
				if sub == nil {
					t.Errorf("%q: %s has no command %q", match[0], cmd.FullName(), word)
					break
				}
				cmd = sub
			}
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d invocations; the pattern no longer matches the help", checked)
	}
}

func hasFlag(cmd *cli.Command, name string) bool {
	if name == "help" || name == "h" || cmd.Root() == cmd && (name == "version" || name == "v") {
		return true
	}
	for _, flag := range cmd.Flags {
		if slices.Contains(flag.Names(), name) {
			return true
		}
	}
	return false
}
