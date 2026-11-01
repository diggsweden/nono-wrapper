// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tailscale/hujson"
	"github.com/urfave/cli/v3"
)

func configCommand() *cli.Command {
	command := &cli.Command{
		Name: "config", Usage: "Manage persistent user allowances",
		Description:  "Run edits from your normal terminal outside nono. User files survive install-local. Changes apply to new sessions. Removing a user grant does not remove inherited access.\n\nExamples:\n  nono-wrapper config opencode show\n  nono-wrapper config opencode allow-domain api.github.com\n  nono-wrapper config codex allow-tcp-port 3000\n  nono-wrapper config codex add-dir --read-write /path/to/shared-project",
		OnUsageError: usageError,
		Action:       helpOrUnknown,
	}
	for _, agent := range []string{"opencode", "codex"} {
		child := &cli.Command{Name: agent, Usage: "Manage " + agent + "-user.jsonc", OnUsageError: usageError, Action: helpOrUnknown}
		child.Commands = append(child.Commands, &cli.Command{
			Name: "show", Usage: "List user-added domains, HTTPS endpoints, TCP ports and paths", OnUsageError: usageError,
			Action: func(_ context.Context, cmd *cli.Command) error {
				if cmd.NArg() != 0 {
					return cli.Exit("show takes no arguments", exitUsage)
				}
				path, err := configPath(agent)
				if err != nil {
					return err
				}
				return showUserConfig(path, agent, cmd.Root().Writer)
			},
		})
		for _, resource := range []string{"host", "port", "dir", "file"} {
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
	argument, noun := "PATH", map[string]string{"host": "host", "port": "TCP port", "dir": "directory", "file": "file"}[resource]
	commandResource := resource
	switch resource {
	case "host":
		argument, noun, commandResource = "DOMAIN|HTTPS_URL", "domain or HTTPS endpoint", "domain"
	case "port":
		argument, noun, commandResource = "PORT", "TCP port", "tcp-port"
	}
	if add && (resource == "host" || resource == "port") {
		verb = "allow"
	}
	command := &cli.Command{
		Name: verb + "-" + commandResource, Usage: usage + noun,
		ArgsUsage: argument, OnUsageError: usageError,
	}
	switch resource {
	case "host":
		command.Description = `DOMAIN allows one exact hostname on every path and port.
A quoted '*.example.com' allows subdomains at any depth, not example.com itself.
Numeric addresses, single-label names and explicit ports are rejected;
domain ports cannot be restricted.

An HTTPS URL with an exact hostname and a non-root path adds an endpoint rule:
* matches one path segment, ** any number. --method limits its HTTP method.
Ports remain unrestricted; plain HTTP is not path-filtered, and request bodies
are not checked. A URL without a path, or with /, means whole-domain access.
Wildcard domains are supported only for whole-domain access.

Remove the domain's user allowance before switching between whole-domain and
endpoint rules. Removing a URL matches its method and path; removing a domain
removes all its user endpoint rules. Removal also accepts exact stored keys,
including IP addresses, single-label names and wildcard patterns that cannot
be newly allowed. It never expands a pattern to remove other entries.
Other wildcard or inherited grants may still allow access. Run outside nono;
changes apply to new sessions.

Examples:
  nono-wrapper config opencode allow-domain api.github.com
  nono-wrapper config opencode allow-domain '*.example.com'
  nono-wrapper config opencode allow-domain --method GET 'https://gitlab.example/api/v4/projects/123/**'`
		command.Flags = []cli.Flag{&cli.StringFlag{Name: "method", Value: "*", Usage: "HTTP method of a URL rule, such as GET; * allows any"}}
	case "port":
		command.Description = "Lets the agent and its programs connect to and listen on one TCP port, from 1 to 65535. This is a host-independent grant: it can permit direct connections to non-loopback addresses on that port, outside domain filtering, even with an offline profile. It is not restricted to localhost and does not force a server's bind address. No ranges or UDP. Run outside nono; changes apply to new sessions."
	}
	if add && (resource == "dir" || resource == "file") {
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
		method := "*"
		if resource == "host" {
			method = cmd.String("method")
		}
		// This is a usability guard. The sandbox's filesystem boundary is what
		// prevents direct edits, including after these variables are cleared.
		if os.Getenv("NONO_SESSION_ID") != "" || os.Getenv("NONO_CAP_FILE") != "" {
			return cli.Exit("apply configuration changes from your normal terminal outside nono; agents may recommend changes or write profile drafts", exitNoPerm)
		}
		rule, err := parseAllowance(resource, cmd.Args().First(), method, add)
		if err != nil {
			if errors.As(err, new(quotedPathError)) {
				return err
			}
			return cli.Exit(err.Error(), exitDataErr)
		}
		rule.readWrite = cmd.Bool("read-write")
		if add && rule.resource == "dir" {
			access := "read-only"
			if rule.readWrite {
				access = "read and write"
			}
			if err := checkConfigRepos(configScope{
				directory: rule.value, subject: strconv.Quote(rule.value), access: access,
				alternative: "Grant a narrower directory",
			}, os.Stdin, cmd.Root().ErrWriter, interactiveInput()); err != nil {
				return cli.Exit(err.Error(), exitNoPerm)
			}
		}
		if add && rule.isPath() && rule.readWrite {
			if err := protectRuleWrite(rule.value); err != nil {
				return err
			}
		}
		path, err := configPath(agent)
		if err != nil {
			return err
		}
		changed, err := editUserConfig(ctx, path, rule, add)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(cmd.Root().Writer, "No change to %q.\n", path)
			return nil
		}
		if add {
			fmt.Fprintf(cmd.Root().Writer, "Allowed %s.\n", rule.describe())
		} else {
			description := rule.describe()
			if rule.resource == "host" && rule.endpoint == nil {
				description = fmt.Sprintf("domain key %q", rule.value)
			}
			fmt.Fprintf(cmd.Root().Writer, "Removed the user allowance for %s; inherited or broader grants may still allow access.\n", description)
		}
		fmt.Fprintf(cmd.Root().Writer, "Updated %q. Applies to new %s sessions; restart the agent to use it.\n", path, agent)
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

// allowance is one user rule: a host, optionally narrowed to one endpoint; a
// TCP port; or a directory or file with its access mode.
type allowance struct {
	resource  string // host, port, dir or file
	value     string // host name, port number or absolute path
	endpoint  *endpointRule
	readWrite bool
}

func (rule allowance) isPath() bool {
	return rule.resource == "dir" || rule.resource == "file"
}

// describe says in a few words what the rule opens.
func (rule allowance) describe() string {
	switch rule.resource {
	case "host":
		if rule.endpoint == nil {
			scope := rule.value + ": all paths and ports"
			if strings.HasPrefix(rule.value, "*.") {
				scope += "; subdomains at any depth, excluding " + strings.TrimPrefix(rule.value, "*.")
			}
			return scope
		}
		method := rule.endpoint.Method + " requests"
		if method == "* requests" {
			method = "requests of any method"
		}
		return fmt.Sprintf("HTTPS %s to %s matching %s. Ports are unrestricted; plain HTTP is not path-filtered. Other wildcard or inherited grants may still allow access; request bodies are not checked", method, rule.value, rule.endpoint.Path)
	case "port":
		return "TCP port " + rule.value + ": connect and listen, including non-loopback addresses; direct TCP access can bypass domain filtering"
	}
	mode := "read-only"
	if rule.readWrite {
		mode = "read and write"
	}
	return fmt.Sprintf("%s %q (%s)", map[string]string{"dir": "directory", "file": "file"}[rule.resource], rule.value, mode)
}

func parseAllowance(resource, value, method string, add bool) (allowance, error) {
	rule := allowance{resource: resource}
	if value == "" {
		return rule, fmt.Errorf("%s must not be empty", resource)
	}
	switch resource {
	case "host":
		return parseHostAllowance(value, method, add)
	case "port":
		port, err := parsePort(value)
		if err != nil {
			return rule, err
		}
		rule.value = strconv.FormatUint(port, 10)
		return rule, nil
	}
	value = expandHomePath(value)
	if !filepath.IsAbs(value) && strings.HasPrefix(value, "$") {
		return rule, fmt.Errorf("use an absolute path or expand the environment variable in your shell")
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return rule, quotePathError(err)
	}
	if add {
		info, err := os.Stat(path)
		if err != nil {
			return rule, quotePathError(err)
		}
		if resource == "dir" && !info.IsDir() || resource == "file" && !info.Mode().IsRegular() {
			return rule, fmt.Errorf("expected an existing %s: %q", resource, path)
		}
	}
	rule.value = path
	return rule, nil
}

// parseHostAllowance accepts an exact domain, a leading *. wildcard or an
// HTTPS URL. Reject ports instead of silently granting broader access.
func parseHostAllowance(value, method string, add bool) (allowance, error) {
	rule := allowance{resource: "host"}
	const usage = "expected a domain such as api.github.com, '*.example.com' or an HTTPS URL such as https://gitlab.example/api/v4/projects/123/**"
	path := ""
	// Removal matches stored keys, not the vocabulary for creating new grants.
	// Never resolve DNS, expand wildcard matches or drop a stored port.
	if !add && !strings.Contains(value, "://") {
		if method != "*" {
			return rule, errors.New("--method needs an HTTPS URL with a non-root path")
		}
		rule.value = hostKey(value)
		return rule, nil
	}
	if strings.Contains(value, "://") {
		target, err := url.Parse(value)
		switch {
		case err != nil:
			return rule, errors.New(usage)
		case !strings.EqualFold(target.Scheme, "https"):
			return rule, fmt.Errorf("only https:// URLs can be narrowed to paths; plain HTTP would bypass the rule.\n%s", usage)
		case target.User != nil || target.RawQuery != "" || target.ForceQuery || strings.Contains(value, "#"):
			return rule, fmt.Errorf("a URL rule matches a path only, without a query, fragment or user name.\n%s", usage)
		}
		value, path = target.Host, target.EscapedPath()
	}
	if add && net.ParseIP(strings.Trim(value, "[]")) != nil {
		return rule, fmt.Errorf("IP addresses are not domains.\n%s", usage)
	}
	if add && strings.Contains(value, ":") {
		return rule, errors.New("domain allowances apply to every port; an explicit domain port cannot be enforced. Omit the port to allow the domain on all ports. allow-tcp-port is a separate host-independent grant, not a domain-port restriction")
	}
	value = hostKey(value)
	if add && !validDomain(value) {
		return rule, fmt.Errorf("%s; numeric and single-label names are not accepted", usage)
	}
	rule.value = value
	if path == "/" || path == "" {
		if method != "*" {
			return rule, errors.New("--method needs a URL with a path, such as https://" + value + "/api/**")
		}
		return rule, nil
	}
	if add && strings.HasPrefix(value, "*.") {
		return rule, errors.New("HTTPS endpoint rules require an exact hostname; wildcard domains are supported only for whole-domain access")
	}
	method = strings.ToUpper(method)
	valid := method != ""
	for _, char := range method {
		valid = valid && (char >= 'A' && char <= 'Z' || method == "*")
	}
	if !valid {
		return rule, fmt.Errorf("expected an HTTP method such as GET, or *: %q", method)
	}
	rule.endpoint = &endpointRule{Method: method, Path: path}
	return rule, nil
}

// parsePort accepts a decimal TCP port from 1 to 65535.
func parsePort(value string) (uint64, error) {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return 0, fmt.Errorf("expected a TCP port from 1 to 65535: %q", value)
	}
	return port, nil
}

func validHostname(value string) bool {
	valid := value != "" && len(value) <= 253
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
	return valid
}

func validDomain(value string) bool {
	value = strings.TrimPrefix(value, "*.")
	tld := value[strings.LastIndex(value, ".")+1:]
	return validHostname(value) && strings.Contains(value, ".") && strings.Trim(tld, "0123456789") != ""
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
	return canonicalConfigPathDepth(path, 0)
}

func canonicalConfigPathDepth(path string, links int) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	// A missing target does not make a symlink's future write location safe.
	// Resolve dangling leaf links too, before walking missing ancestors.
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		if links >= 40 {
			return "", fmt.Errorf("too many symbolic links while resolving %q", path)
		}
		target, readErr := os.Readlink(path)
		if readErr != nil {
			return "", quotePathError(readErr)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return canonicalConfigPathDepth(target, links+1)
	}
	if !os.IsNotExist(err) || filepath.Dir(path) == path {
		return "", quotePathError(err)
	}
	parent, err := canonicalConfigPathDepth(filepath.Dir(path), links)
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

// appendConfig adds value to the array at section.field, creating both.
func appendConfig(doc *hujson.Value, section, field string, value any) error {
	if doc.Find("/"+section) == nil {
		if err := patchConfig(doc, "add", "/"+section, map[string]any{}); err != nil {
			return err
		}
	}
	path := "/" + section + "/" + field
	if doc.Find(path) == nil {
		if err := patchConfig(doc, "add", path, []string{}); err != nil {
			return err
		}
	}
	return patchConfig(doc, "add", path+"/-", value)
}

func changeAllowance(doc *hujson.Value, rule allowance, add bool) (bool, error) {
	if rule.resource == "host" && add {
		if err := checkDomainScope(doc, rule); err != nil {
			return false, err
		}
	}
	switch {
	case rule.resource == "port":
		return changePort(doc, rule.value, add)
	case rule.endpoint != nil:
		return changeEndpoint(doc, rule, add)
	}
	resource, value, readWrite := rule.resource, rule.value, rule.readWrite
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
		if err := appendConfig(doc, section, target, value); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

// changePort edits network.open_port: host-independent TCP listen/connect access.
func changePort(doc *hujson.Value, port string, add bool) (bool, error) {
	array, err := configArray(doc, "network", "open_port")
	if err != nil {
		return false, err
	}
	if array != nil {
		for i, element := range array.Elements {
			literal, ok := element.Value.(hujson.Literal)
			number, err := parsePort(literal.String())
			if !ok || literal.Kind() != '0' || err != nil {
				return false, invalidConfig{errors.New("user configuration network.open_port must hold port numbers from 1 to 65535")}
			}
			if strconv.FormatUint(number, 10) != port {
				continue
			}
			if add {
				return false, nil
			}
			return true, patchConfig(doc, "remove", "/network/open_port/"+strconv.Itoa(i), nil)
		}
	}
	if !add {
		return false, nil
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return false, err
	}
	return true, appendConfig(doc, "network", "open_port", number)
}

// Reject implicit scope replacements before changing any entries, including
// duplicate hand-written entries. Distinct wildcard patterns remain separate.
func checkDomainScope(doc *hujson.Value, rule allowance) error {
	array, err := configArray(doc, "network", "allow_domain")
	if err != nil || array == nil {
		return err
	}
	for _, element := range array.Elements {
		element = element.Clone()
		element.Standardize()
		entry, err := decodeHostRule(element.Pack())
		if err != nil {
			return invalidConfig{err}
		}
		if entry.Domain == rule.value && (len(entry.Endpoints) > 0) != (rule.endpoint != nil) {
			return cli.Exit(fmt.Sprintf("domain %q already has a different user allowance scope; use remove-domain %q before switching between whole-domain and HTTPS endpoint rules", rule.value, rule.value), exitDataErr)
		}
	}
	return nil
}

// changeEndpoint adds or removes one endpoint rule for a host. The last
// endpoint's removal removes the host, since an empty list allows it all.
func changeEndpoint(doc *hujson.Value, rule allowance, add bool) (bool, error) {
	array, err := configArray(doc, "network", "allow_domain")
	if err != nil {
		return false, err
	}
	changed, present := false, false
	appendAt := ""
	var elements []hujson.ArrayElement
	if array != nil {
		elements = array.Elements
	}
	for i := len(elements) - 1; i >= 0; i-- {
		element := &elements[i]
		domain := element
		if element.Value.Kind() == '{' {
			domain = element.Find("/domain")
		}
		if domain == nil || domain.Value.Kind() != '"' {
			return false, fmt.Errorf("invalid entry in network.allow_domain")
		}
		if hostKey(domain.Value.(hujson.Literal).String()) != rule.value {
			continue
		}
		base := "/network/allow_domain/" + strconv.Itoa(i)
		standard := element.Clone()
		standard.Standardize()
		entry, err := decodeHostRule(standard.Pack())
		if err != nil {
			return false, invalidConfig{fmt.Errorf("user configuration network.allow_domain: %w", err)}
		}
		// A name, or an object without endpoints, allows the whole host.
		if len(entry.Endpoints) == 0 {
			continue
		}
		index := slices.IndexFunc(entry.Endpoints, func(endpoint endpointRule) bool {
			return strings.EqualFold(endpoint.Method, rule.endpoint.Method) && endpoint.Path == rule.endpoint.Path
		})
		switch {
		case add && index >= 0:
			present = true
		case add:
			appendAt = base + "/endpoints/-"
		case !add && index >= 0:
			if len(entry.Endpoints) == 1 {
				err = patchConfig(doc, "remove", base, nil)
			} else {
				err = patchConfig(doc, "remove", base+"/endpoints/"+strconv.Itoa(index), nil)
			}
			if err != nil {
				return false, err
			}
			changed = true
		}
	}
	if add && !present {
		var err error
		if appendAt != "" {
			err = patchConfig(doc, "add", appendAt, *rule.endpoint)
		} else {
			err = appendConfig(doc, "network", "allow_domain", hostRule{Domain: rule.value, Endpoints: []endpointRule{*rule.endpoint}})
		}
		if err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func editUserConfig(ctx context.Context, path string, rule allowance, add bool) (bool, error) {
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
	changed, err := changeAllowance(&doc, rule, add)
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

func showUserConfig(path, agent string, output io.Writer) error {
	doc, _, _, err := readUserConfig(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "User allowances in %q:\n", path)
	count := 0
	for _, group := range []struct{ section, field, label string }{
		{"network", "allow_domain", "domain"},
		{"network", "open_port", "TCP port (connect/listen; not loopback-only)"},
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
			if group.field == "allow_domain" {
				entry.Standardize()
				rule, err := decodeHostRule(entry.Pack())
				if err != nil {
					return invalidConfig{err}
				}
				if len(rule.Endpoints) == 0 {
					fmt.Fprintf(output, "  domain: %q (all paths and ports)\n", rule.Domain)
				} else {
					for _, endpoint := range rule.Endpoints {
						fmt.Fprintf(output, "  HTTPS endpoint: %q method=%q path=%q (all ports; plain HTTP is not path-filtered)\n", rule.Domain, endpoint.Method, endpoint.Path)
					}
				}
				count++
				continue
			}
			entry.Minimize()
			fmt.Fprintf(output, "  %s: %s\n", group.label, entry.Pack())
			count++
		}
	}
	if count == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	fmt.Fprintf(output, "Bundled and inherited grants are not listed here; see them with: nono profile show %s-chatgpt\n", agent)
	return nil
}
