// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// configRepoDepth bounds the search below the start directory: a project
// group directory holds repositories, and those hold their own files.
const configRepoDepth = 2

// The agent gets read and write access to its whole start directory. Starting
// in the home directory, or in the directory holding every project, would
// hand it all of them, so agents start only below one project root.
func checkStartDirectory(stdin io.Reader, stderr io.Writer, interactive bool) error {
	root, canonical, err := projectRoot()
	if err != nil {
		return err
	}
	workdir, err := os.Getwd()
	if err != nil {
		return err
	}
	workdir, err = filepath.EvalSymlinks(workdir)
	if err != nil {
		return quotePathError(err)
	}
	switch {
	case samePolicyPath(workdir, canonical):
		return fmt.Errorf("will not start in the project root %q itself: the agent would get read and write access to every project in it.\nStart in one project, or in a directory that groups the projects you want, such as %q", root, filepath.Join(root, "my-project"))
	case !pathWithin(workdir, canonical):
		return fmt.Errorf("will not start in %q: it is outside the project root %q.\nThe agent gets read and write access to the whole start directory. Move to a project inside %q, or set NONO_WRAPPER_ROOT", workdir, root, root)
	}
	return checkConfigRepos(configScope{
		directory: workdir, subject: "the start directory", access: "read and write",
		alternative: "Start in a directory without them",
	}, stdin, stderr, interactive)
}

// projectRoot returns NONO_WRAPPER_ROOT, or ~/development, as configured and
// with symlinks resolved. It must exist.
func projectRoot() (string, string, error) {
	root := os.Getenv("NONO_WRAPPER_ROOT")
	if root == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", "", errors.New("set HOME or NONO_WRAPPER_ROOT: agents start only inside your project root")
		}
		root = filepath.Join(home, "development")
	}
	if !filepath.IsAbs(root) {
		return "", "", fmt.Errorf("NONO_WRAPPER_ROOT must be an absolute path: %q", root)
	}
	canonical, err := filepath.EvalSymlinks(root)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", fmt.Errorf("the project root %q does not exist.\nKeep your projects there, or set NONO_WRAPPER_ROOT to the directory that holds them", root)
	}
	if err != nil {
		return "", "", quotePathError(err)
	}
	// A root holding the home directory would let agents start in ~/.ssh.
	if home, err := filepath.EvalSymlinks(os.Getenv("HOME")); err == nil && pathWithin(home, canonical) {
		return "", "", fmt.Errorf("the project root %q contains your home directory, so agents could start next to your keys and settings.\nSet NONO_WRAPPER_ROOT to the directory that holds your projects", root)
	}
	return root, canonical, nil
}

// configScope describes a directory grant for the configuration question.
type configScope struct {
	directory   string // the granted directory to search
	subject     string // how messages name it
	access      string // the access the agent would get
	alternative string // what to do instead of including the repositories
}

// checkConfigRepos asks before a grant includes configuration repositories,
// unless NONO_WRAPPER_ALLOW_CONFIG=1 includes them without asking.
func checkConfigRepos(scope configScope, stdin io.Reader, stderr io.Writer, interactive bool) error {
	if os.Getenv("NONO_WRAPPER_ALLOW_CONFIG") == "1" {
		return nil
	}
	repos, err := configRepos(scope.directory, configRepoDepth)
	if err != nil || len(repos) == 0 {
		return err
	}
	return confirmConfigRepos(stdin, stderr, interactive, scope, repos)
}

// configRepos lists Git repositories below directory whose names mark them as
// configuration: config, or a name ending in -config or _config. The start
// directory itself is the user's explicit choice and is not listed.
func configRepos(directory string, depth int) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, quotePathError(err)
	}
	var found []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(directory, name)
		// Projects are searched too: a submodule can be a configuration repository.
		if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil && configName(name) {
			found = append(found, path)
		}
		if depth > 1 {
			// The agent cannot read what the user cannot, so skip such entries.
			nested, err := configRepos(path, depth-1)
			if errors.Is(err, fs.ErrPermission) {
				continue
			}
			if err != nil {
				return nil, err
			}
			found = append(found, nested...)
		}
	}
	return found, nil
}

func configName(name string) bool {
	name = strings.ToLower(name)
	return name == "config" || strings.HasSuffix(name, "-config") || strings.HasSuffix(name, "_config")
}

// interactiveInput reports whether standard input is a terminal someone can
// answer from. /dev/null is a character device too, but nobody types there.
func interactiveInput() bool {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}

// nono cannot exclude part of a granted directory, so declining stops the
// launch or the grant. Only an explicit yes on a terminal continues.
func confirmConfigRepos(stdin io.Reader, stderr io.Writer, interactive bool, scope configScope, repos []string) error {
	var list strings.Builder
	for _, repo := range repos {
		fmt.Fprintf(&list, "  %s\n", repo)
	}
	hint := "Linux sandboxing cannot leave one directory out of a granted directory; " +
		"Landlock deny rules are proposed in https://github.com/landlock-lsm/linux/issues/28.\n" +
		scope.alternative + ", or set NONO_WRAPPER_ALLOW_CONFIG=1 to include them without asking"
	if !interactive {
		return fmt.Errorf("%s contains configuration repositories:\n%s%s", scope.subject, list.String(), hint)
	}
	fmt.Fprintf(stderr, "nono-wrapper: %s contains configuration repositories:\n%s"+
		"The agent would get %s access to them as well.\nInclude them? [y/N] ", scope.subject, list.String(), scope.access)
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if answer = strings.ToLower(strings.TrimSpace(answer)); answer == "y" || answer == "yes" {
		return nil
	}
	return fmt.Errorf("stopped: configuration repositories were not included.\n%s", hint)
}
