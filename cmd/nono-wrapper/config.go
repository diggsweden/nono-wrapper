// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"
	"github.com/urfave/cli/v3"
)

func configCommand() *cli.Command {
	command := &cli.Command{
		Name: "config", Usage: "Manage persistent user allowances",
		Description:  "User files survive install-local. Changes apply to new sessions. Removing a user grant does not remove inherited access.\n\nExamples:\n  nono-wrapper config opencode show\n  nono-wrapper config opencode add-host api.github.com\n  nono-wrapper config codex add-dir --read-write /path/to/shared-project",
		OnUsageError: usageError,
		Action:       helpOrUnknown,
	}
	for _, agent := range []string{"opencode", "codex"} {
		child := &cli.Command{Name: agent, Usage: "Manage " + agent + "-user.jsonc", OnUsageError: usageError, Action: helpOrUnknown}
		child.Commands = append(child.Commands, &cli.Command{
			Name: "show", Usage: "List user-added hosts, directories and files", OnUsageError: usageError,
			Action: func(_ context.Context, cmd *cli.Command) error {
				if cmd.NArg() != 0 {
					return cli.Exit("show takes no arguments", exitUsage)
				}
				path, err := configPath(agent)
				if err != nil {
					return err
				}
				return showUserConfig(path, cmd.Root().Writer)
			},
		})
		for _, resource := range []string{"host", "dir", "file"} {
			for _, add := range []bool{true, false} {
				child.Commands = append(child.Commands, configEditCommand(agent, resource, add))
			}
		}
		command.Commands = append(command.Commands, child)
	}
	return command
}

func configEditCommand(agent, resource string, add bool) *cli.Command {
	verb, usage := "remove", "Remove the user allowance for a "
	if add {
		verb, usage = "add", "Allow a "
	}
	argument, noun := "PATH", map[string]string{"host": "host", "dir": "directory", "file": "file"}[resource]
	if resource == "host" {
		argument = "HOST"
	}
	command := &cli.Command{
		Name: verb + "-" + resource, Usage: usage + noun,
		ArgsUsage: argument, OnUsageError: usageError,
	}
	if resource == "host" {
		command.Description = "HOST is a plain name such as api.github.com, without a scheme, path, wildcard or port."
	}
	if add && resource != "host" {
		command.Flags = []cli.Flag{
			&cli.BoolFlag{Name: "read-only", Usage: "Allow reading (the default)"},
			&cli.BoolFlag{Name: "read-write", Usage: "Allow reading and writing"},
		}
		command.Usage += " (read-only unless --read-write)"
		command.Description = "Adds or updates this path's user allowance. The path must exist. Paths are stored as absolute paths."
	}
	command.Action = func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() != 1 || (cmd.Bool("read-only") && cmd.Bool("read-write")) {
			return cli.Exit("usage: nono-wrapper config "+agent+" "+command.Name+" [options] "+argument, exitUsage)
		}
		// This is a usability guard. The sandbox's filesystem boundary is what
		// prevents direct edits, including after these variables are cleared.
		if os.Getenv("NONO_SESSION_ID") != "" || os.Getenv("NONO_CAP_FILE") != "" {
			return cli.Exit("apply configuration changes from your normal terminal outside nono; agents may recommend changes or write profile drafts", exitNoPerm)
		}
		value, err := allowanceValue(resource, cmd.Args().First(), add)
		if err != nil {
			if errors.As(err, new(quotedPathError)) {
				return err
			}
			return cli.Exit(err.Error(), exitDataErr)
		}
		if add && resource != "host" && cmd.Bool("read-write") {
			if err := protectRuleWrite(value); err != nil {
				return err
			}
		}
		path, err := configPath(agent)
		if err != nil {
			return err
		}
		changed, err := editUserConfig(ctx, path, resource, value, add, cmd.Bool("read-write"))
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(cmd.Root().Writer, "No change to %q.\n", path)
			return nil
		}
		fmt.Fprintf(cmd.Root().Writer, "Updated %q. Applies to the next session.\n", path)
		if !add {
			fmt.Fprintln(cmd.Root().Writer, "Removed the user allowance; inherited or broader grants may still allow access.")
		}
		return nil
	}
	return command
}

func configPath(agent string) (string, error) {
	path, err := userProfilePath(agent)
	if err == nil && path == "" {
		err = invalidConfig{errors.New("set HOME or an absolute XDG_CONFIG_HOME to manage user configuration")}
	}
	return path, err
}

func allowanceValue(resource, value string, add bool) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", resource)
	}
	if resource == "host" {
		value = strings.ToLower(strings.TrimSuffix(value, "."))
		valid := len(value) <= 253
		for _, label := range strings.Split(value, ".") {
			if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				valid = false
			}
			for _, char := range label {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
					valid = false
				}
			}
		}
		if !valid {
			return "", fmt.Errorf("expected a hostname such as api.github.com, without a scheme, path, wildcard or port")
		}
		return value, nil
	}
	value = expandHomePath(value)
	if !filepath.IsAbs(value) && strings.HasPrefix(value, "$") {
		return "", fmt.Errorf("use an absolute path or expand the environment variable in your shell")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", quotePathError(err)
	}
	if add {
		info, err := os.Stat(path)
		if err != nil {
			return "", quotePathError(err)
		}
		if resource == "dir" && !info.IsDir() || resource == "file" && !info.Mode().IsRegular() {
			return "", fmt.Errorf("expected an existing %s: %q", resource, path)
		}
	}
	return path, nil
}

func expandHomePath(path string) string {
	home := os.Getenv("HOME")
	if home != "" {
		for _, prefix := range []string{"~", "$HOME", "${HOME}"} {
			if path == prefix {
				return home
			}
			if strings.HasPrefix(path, prefix+"/") {
				return filepath.Join(home, strings.TrimPrefix(path, prefix+"/"))
			}
		}
	}
	return path
}

// Grants must not expose the live policy or package store to the agent.
// The sibling profile-drafts directory remains available for proposals.
func protectRuleWrite(path string) error {
	profile, err := configPath("opencode")
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(profile))
	path, err = canonicalConfigPath(path)
	if err != nil {
		return err
	}
	protectedPaths := []string{
		filepath.Join(root, "profiles"), filepath.Join(root, "packages"),
		filepath.Join(root, "config.toml"), filepath.Join(root, "network-policy.json"),
	}
	// A live profile may be a dotfile symlink or have a hard-link alias outside
	// the profiles directory. Protect its backing file as well as the directory.
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil && !os.IsNotExist(err) {
		return quotePathError(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			protectedPaths = append(protectedPaths, filepath.Join(root, "profiles", entry.Name()))
		}
	}
	for _, protectedPath := range protectedPaths {
		protected, err := canonicalConfigPath(protectedPath)
		if err != nil {
			return err
		}
		if pathWithin(path, protected) || pathWithin(protected, path) {
			return cli.Exit(fmt.Sprintf("write allowance %q would expose nono's active rules; use read-only access or a narrower directory", path), exitNoPerm)
		}
	}
	return nil
}

func pathWithin(path, directory string) bool {
	relative, err := filepath.Rel(directory, path)
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return true
	}
	// EvalSymlinks does not normalize case on case-insensitive APFS. Compare
	// existing ancestors by identity instead of lowercasing paths, which would
	// conflate distinct objects on case-sensitive volumes.
	root, err := os.Stat(directory)
	if err != nil {
		return false
	}
	for {
		if info, err := os.Stat(path); err == nil && os.SameFile(info, root) {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func samePolicyPath(first, second string) bool {
	if first == second {
		return true
	}
	left, err := os.Stat(first)
	if err != nil {
		return false
	}
	right, err := os.Stat(second)
	return err == nil && os.SameFile(left, right)
}

// Resolve existing ancestors too, so symlinked config roots and not-yet-created
// profiles get the same overlap check as existing files.
func canonicalConfigPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) || filepath.Dir(path) == path {
		return "", quotePathError(err)
	}
	parent, err := canonicalConfigPath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func readUserConfig(path string) (hujson.Value, []byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return hujson.Value{Value: &hujson.Object{}}, nil, 0o600, nil
	}
	if err != nil {
		return hujson.Value{}, nil, 0, quotePathError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return hujson.Value{}, nil, 0, quotePathError(err)
		}
	}
	if !info.Mode().IsRegular() {
		return hujson.Value{}, nil, 0, invalidConfig{fmt.Errorf("user profile must be a regular file: %q", path)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return hujson.Value{}, nil, 0, quotePathError(err)
	}
	// The syntax tree retains slices into its input and formatting can mutate
	// them. Keep the original bytes intact for the concurrent-edit check.
	doc, err := hujson.Parse(bytes.Clone(data))
	if err != nil {
		return doc, nil, 0, invalidConfig{fmt.Errorf("parse user profile %q: %w", path, err)}
	}
	if doc.Value.Kind() != '{' {
		return doc, nil, 0, invalidConfig{fmt.Errorf("user profile %q must be a JSON object", path)}
	}
	if err := uniqueConfigKeys(doc); err != nil {
		return doc, nil, 0, invalidConfig{err}
	}
	return doc, data, info.Mode().Perm(), nil
}

// Avoid editing one copy of a key while nono reads another.
func uniqueConfigKeys(value hujson.Value) error {
	switch node := value.Value.(type) {
	case *hujson.Object:
		seen := map[string]bool{}
		for _, member := range node.Members {
			name := member.Name.Value.(hujson.Literal).String()
			if seen[name] {
				return fmt.Errorf("duplicate user configuration key %q", name)
			}
			seen[name] = true
			if err := uniqueConfigKeys(member.Value); err != nil {
				return err
			}
		}
	case *hujson.Array:
		for _, element := range node.Elements {
			if err := uniqueConfigKeys(element); err != nil {
				return err
			}
		}
	}
	return nil
}

func configArray(doc *hujson.Value, section, field string) (*hujson.Array, error) {
	parent := doc.Find("/" + section)
	if parent == nil {
		return nil, nil
	}
	if parent.Value.Kind() != '{' {
		return nil, invalidConfig{fmt.Errorf("user configuration %s must be an object", section)}
	}
	value := doc.Find("/" + section + "/" + field)
	if value == nil {
		return nil, nil
	}
	array, ok := value.Value.(*hujson.Array)
	if !ok {
		return nil, invalidConfig{fmt.Errorf("user configuration %s.%s must be an array", section, field)}
	}
	return array, nil
}

func patchConfig(doc *hujson.Value, op, path string, value any) error {
	data, err := json.Marshal([]map[string]any{{"op": op, "path": path, "value": value}})
	if err != nil {
		return err
	}
	return doc.Patch(data)
}

func changeAllowance(doc *hujson.Value, resource, value string, add, readWrite bool) (bool, error) {
	section, fields := "filesystem", []string{"read", "write", "allow"}
	valueKey := value
	if resource == "host" {
		section, fields = "network", []string{"allow_domain"}
	} else if resource == "file" {
		fields = []string{"read_file", "write_file", "allow_file"}
	}
	if resource != "host" {
		var err error
		valueKey, err = policyPathKey(value)
		if err != nil {
			return false, err
		}
	}
	target := fields[0]
	if readWrite {
		target = fields[len(fields)-1]
	}
	if resource != "host" {
		// A manually authored profile may put a file in read/allow instead of
		// read_file/allow_file. One physical path gets one user-selected mode.
		fields = []string{"read", "write", "allow", "read_file", "write_file", "allow_file"}
	}
	changed, present := false, false
	for _, field := range fields {
		array, err := configArray(doc, section, field)
		if err != nil {
			return false, err
		}
		if array == nil {
			continue
		}
		for i := len(array.Elements) - 1; i >= 0; i-- {
			element := &array.Elements[i]
			if resource == "host" && element.Value.Kind() == '{' {
				element = element.Find("/domain")
			}
			if element == nil || element.Value.Kind() != '"' {
				return false, fmt.Errorf("invalid entry in %s.%s", section, field)
			}
			candidate := element.Value.(hujson.Literal).String()
			if resource != "host" && !strings.HasSuffix(field, "_file") && strings.Contains(candidate, "*") {
				continue
			}
			if resource == "host" {
				candidate = strings.ToLower(strings.TrimSuffix(candidate, "."))
			} else {
				candidate, err = policyPathKey(candidate)
				if err != nil {
					return false, err
				}
			}
			if candidate != valueKey && (resource == "host" || !samePolicyPath(candidate, valueKey)) {
				continue
			}
			if add && field == target && !(resource == "host" && array.Elements[i].Value.Kind() == '{') {
				present = true
				continue
			}
			if err := patchConfig(doc, "remove", "/"+section+"/"+field+"/"+strconv.Itoa(i), nil); err != nil {
				return false, err
			}
			changed = true
		}
	}
	if add && !present {
		if doc.Find("/"+section) == nil {
			if err := patchConfig(doc, "add", "/"+section, map[string]any{}); err != nil {
				return false, err
			}
		}
		path := "/" + section + "/" + target
		if doc.Find(path) == nil {
			if err := patchConfig(doc, "add", path, []string{}); err != nil {
				return false, err
			}
		}
		if err := patchConfig(doc, "add", path+"/-", value); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func editUserConfig(ctx context.Context, path, resource, value string, add, readWrite bool) (bool, error) {
	// Follow a user's dotfile symlink without replacing the symlink itself.
	if _, err := os.Lstat(path); err == nil {
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return false, quotePathError(err)
		}
	} else if !os.IsNotExist(err) {
		return false, quotePathError(err)
	} else if !add {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, quotePathError(err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false, fmt.Errorf("lock user configuration (another edit may be running): %w\nIf no other edit is running, remove %q and retry", quotePathError(err), path+".lock")
	}
	lock.Close()
	defer os.Remove(path + ".lock")
	doc, original, mode, err := readUserConfig(path)
	if err != nil {
		return false, err
	}
	changed, err := changeAllowance(&doc, resource, value, add, readWrite)
	if err != nil || !changed {
		return false, err
	}
	doc.Format()
	file, err := os.CreateTemp(filepath.Dir(path), ".nono-wrapper-*.jsonc")
	if err != nil {
		return false, quotePathError(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(doc.Pack()); err != nil {
		return false, quotePathError(err)
	}
	if err := file.Chmod(mode); err != nil {
		return false, quotePathError(err)
	}
	if err := file.Sync(); err != nil {
		return false, quotePathError(err)
	}
	if err := file.Close(); err != nil {
		return false, quotePathError(err)
	}
	check := exec.CommandContext(ctx, "nono", "profile", "validate", file.Name())
	check.WaitDelay = time.Second
	if output, err := check.CombinedOutput(); err != nil {
		return false, invalidConfig{fmt.Errorf("validate user configuration: %w\n%s", err, output)}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, err := os.ReadFile(path)
	if original == nil && !os.IsNotExist(err) || original != nil && (err != nil || !bytes.Equal(current, original)) {
		return false, fmt.Errorf("user configuration changed during editing; retry")
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return false, quotePathError(err)
	}
	return true, nil
}

func showUserConfig(path string, output io.Writer) error {
	doc, _, _, err := readUserConfig(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "User allowances in %q:\n", path)
	count := 0
	for _, group := range []struct{ section, field, label string }{
		{"network", "allow_domain", "host"},
		{"filesystem", "read", "directory (read-only)"},
		{"filesystem", "write", "directory (write-only)"},
		{"filesystem", "allow", "directory (read/write)"},
		{"filesystem", "read_file", "file (read-only)"},
		{"filesystem", "write_file", "file (write-only)"},
		{"filesystem", "allow_file", "file (read/write)"},
	} {
		array, err := configArray(&doc, group.section, group.field)
		if err != nil {
			return err
		}
		if array == nil {
			continue
		}
		for _, entry := range array.Elements {
			entry = entry.Clone()
			entry.Minimize()
			fmt.Fprintf(output, "  %s: %s\n", group.label, entry.Pack())
			count++
		}
	}
	if count == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	fmt.Fprintln(output, "Inherited grants are not listed here.")
	return nil
}
