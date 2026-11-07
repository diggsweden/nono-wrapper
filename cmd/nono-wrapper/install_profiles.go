// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/diggsweden/nono-wrapper/profiles"
	"github.com/urfave/cli/v3"
)

func installProfilesCommand() *cli.Command {
	return &cli.Command{
		Name: "install-profiles", Usage: "Install the bundled profiles and the pinned official nono packs",
		Description: "Pulls and pins the official packs this binary was built with, then writes its bundled profiles to " + profilesDir + ". " +
			"nono verifies the packs' signatures and installs their agent integrations. Your opencode-user.jsonc and codex-user.jsonc are kept. " +
			"Run it again after updating nono-wrapper; doctor reports installed profiles that differ from this binary's.",
		OnUsageError: usageError,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 0 {
				return cli.Exit("install-profiles takes no arguments", exitUsage)
			}
			return installProfiles(ctx, cmd.Root().Writer, cmd.Root().ErrWriter)
		},
	}
}

func installProfiles(ctx context.Context, stdout, stderr io.Writer) error {
	if os.Getenv("NONO_SESSION_ID") != "" || os.Getenv("NONO_CAP_FILE") != "" {
		return cli.Exit("install profiles from your normal terminal outside nono", exitNoPerm)
	}
	nono, err := exec.LookPath("nono")
	if err != nil {
		return cli.Exit("nono is not on PATH; install: https://nono.sh/docs/cli/getting_started/installation", exitNotFound)
	}
	config, err := nonoConfigHome()
	if err != nil {
		return err
	}
	packs, err := bundledPacks()
	if err != nil {
		return err
	}
	run := func(output io.Writer, args ...string) error {
		command := exec.CommandContext(ctx, nono, args...)
		command.Stdout, command.Stderr = output, stderr
		if err := command.Run(); err != nil {
			return fmt.Errorf("nono %s: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	// nono canonicalizes XDG_CONFIG_HOME and otherwise falls back to HOME.
	// Create it before pulling packs so a fresh custom location is respected.
	if err := os.MkdirAll(config, 0o700); err != nil {
		return err
	}
	// Retire the old pack before installing v2: their plugin wiring overlaps.
	var installed bytes.Buffer
	if err := run(&installed, "list", "--installed"); err != nil {
		return err
	}
	for line := range strings.Lines(installed.String()) {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "nolabs-ai/opencode-v1" {
			if err := run(stdout, "remove", "nolabs-ai/opencode-v1"); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "Removed the retired OpenCode 1 pack; review custom profiles that extend it.")
		}
	}
	for _, pack := range packs {
		if err := run(stdout, "pull", pack[0]+"@"+pack[1]); err != nil {
			return err
		}
		if err := run(stdout, "pin", pack[0]); err != nil {
			return err
		}
	}
	directory := filepath.Join(config, "nono", "profiles")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	bundled, err := fs.Glob(profiles.Files, "*.jsonc")
	if err != nil {
		return err
	}
	for _, name := range bundled {
		data, err := profiles.Files.ReadFile(name)
		if err != nil {
			return err
		}
		if err := replaceFile(filepath.Join(directory, name), data); err != nil {
			return err
		}
	}
	for _, profile := range []string{"opencode-chatgpt", "codex-chatgpt"} {
		if err := run(io.Discard, "profile", "show", profile); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Profiles: %s\nDefault profiles verified: opencode-chatgpt, codex-chatgpt\nNext: nono-wrapper doctor\n", directory)
	return nil
}

// nonoConfigHome is the configuration root nono uses on Linux and macOS.
func nonoConfigHome() (string, error) {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		config = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(config) {
		return "", cli.Exit(fmt.Sprintf("XDG_CONFIG_HOME must be an absolute path: %q", config), exitConfig)
	}
	return config, nil
}

// bundledPacks reads the "namespace/name version" lines of the packs manifest.
func bundledPacks() ([][2]string, error) {
	data, err := profiles.Files.ReadFile("packs")
	if err != nil {
		return nil, err
	}
	var packs [][2]string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed bundled pack line %q", line)
		}
		packs = append(packs, [2]string{fields[0], fields[1]})
	}
	if len(packs) == 0 {
		return nil, errors.New("no bundled packs")
	}
	return packs, scanner.Err()
}

// replaceFile writes beside the destination and renames over it, so readers
// never see a partial profile and a symlink there is replaced, not followed.
func replaceFile(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// changedProfiles names the bundled profiles whose installed copy is missing
// or differs from this binary's, so doctor can say what an update left behind.
func changedProfiles() []string {
	config, err := nonoConfigHome()
	if err != nil {
		return nil
	}
	bundled, err := fs.Glob(profiles.Files, "*.jsonc")
	if err != nil {
		return nil
	}
	var changed []string
	for _, name := range bundled {
		want, err := profiles.Files.ReadFile(name)
		if err != nil {
			continue
		}
		if got, err := os.ReadFile(filepath.Join(config, "nono", "profiles", name)); err != nil || !bytes.Equal(got, want) {
			changed = append(changed, name)
		}
	}
	return changed
}
