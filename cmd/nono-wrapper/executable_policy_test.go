// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutableWritesRejectFilesAncestorsAndManagedTrees(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "bin/agent")
	installs := filepath.Join(root, "mise/installs")
	for _, path := range []string{filepath.Dir(program), installs} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(program, []byte("stand-in"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "agent-alias")
	protected := []pathRule{{program, pathRead, true}, {installs, pathRead, false}}
	for _, test := range []struct {
		name string
		rule pathRule
		deny bool
	}{
		{"file write", pathRule{program, pathWrite, true}, true},
		{"hard-link write", pathRule{alias, pathWrite, true}, true},
		{"parent directory", pathRule{filepath.Dir(program), pathRead | pathWrite, false}, true},
		{"ancestor directory", pathRule{root, pathWrite, false}, true},
		{"read-only executable", pathRule{program, pathRead, true}, false},
		{"unrelated child of bin", pathRule{filepath.Join(root, "bin/state"), pathWrite, false}, false},
		{"managed resource file", pathRule{filepath.Join(installs, "agent/1/resources/data"), pathWrite, true}, true},
		{"managed child directory", pathRule{filepath.Join(installs, "agent/1/cache"), pathWrite, false}, true},
		{"read-only managed tree", pathRule{installs, pathRead, false}, false},
		{"directory glob", pathRule{filepath.Join(root, "b?n"), pathWrite, false}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "hard-link write" {
				if err := os.Link(program, alias); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(alias) })
			}
			err := checkProtectedWrites([]pathRule{test.rule}, protected)
			if (err != nil) != test.deny {
				t.Fatalf("unexpected executable boundary decision: %v", err)
			}
		})
	}
}

func TestExecutableInvocationProtectsSymlinkedParents(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "home/.config"))
	tools, workspace, intermediate := filepath.Join(root, "tools"), filepath.Join(root, "workspace"), filepath.Join(root, "intermediate")
	for _, directory := range []string{tools, workspace, intermediate} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(tools, "agent")
	if err := os.WriteFile(program, []byte("stand-in"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../tools", filepath.Join(intermediate, "dispatch")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../intermediate/dispatch", filepath.Join(workspace, "dispatch")); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecutable(t.Context(), "agent", filepath.Join(workspace, "dispatch/agent"))
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protectedLaunchPaths(resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{workspace, intermediate, tools} {
		if err := checkProtectedWrites([]pathRule{{directory, pathWrite, false}}, protected); err == nil {
			t.Fatalf("writable invocation directory was accepted: %s", directory)
		}
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Join(workspace, "state"), pathWrite, false}}, protected); err != nil {
		t.Fatalf("unrelated sibling state was rejected: %v", err)
	}
}

func TestProtectedHardLinksRejectDirectoryWritesWithoutScanning(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "home/.config"))
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(root, "agent")
	if err := os.WriteFile(program, []byte("stand-in"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(workspace, "nested/alias")
	if err := os.Link(program, alias); err != nil {
		t.Fatal(err)
	}
	protected := []pathRule{{program, pathRead, true}}
	grant := pathRule{workspace, pathRead | pathWrite, false}
	if err := checkProtectedWrites([]pathRule{grant}, protected); err == nil || !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("directory-contained hard-link alias was accepted: %v", err)
	}
	for _, safe := range []pathRule{{workspace, pathRead, false}, {filepath.Join(workspace, "unrelated-file"), pathWrite, true}} {
		if err := checkProtectedWrites([]pathRule{safe}, protected); err != nil {
			t.Fatalf("safe grant was rejected: %v", err)
		}
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := checkProtectedWrites([]pathRule{grant}, protected); err != nil {
		t.Fatalf("single-link executable was rejected: %v", err)
	}
}

func TestManagedInstallationAliasesResolveToOneProtectedTree(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("MISE_DATA_DIR", filepath.Join(root, "custom mise"))
	actual := filepath.Join(root, "shared installs")
	if err := os.MkdirAll(actual, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "custom mise"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, filepath.Join(root, "custom mise/installs")); err != nil {
		t.Fatal(err)
	}
	protected, err := protectedLaunchPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Join(actual, "another-version"), pathWrite, false}}, protected); err == nil {
		t.Fatal("installation alias allowed modifying an unselected cached version")
	}
	if err := checkProtectedWrites([]pathRule{{filepath.Join(root, "home/.local/share/mise/installs/future"), pathWrite, false}}, protected); err == nil {
		t.Fatal("an absent installation namespace was left writable")
	}
	for _, path := range []string{"shims", "command-wrappers/bin", "plugins", "cache"} {
		if err := checkProtectedWrites([]pathRule{{filepath.Join(root, "custom mise", path), pathWrite, false}}, protected); err == nil {
			t.Fatalf("managed dispatcher/data path remained writable: %s", path)
		}
	}
}

func TestExecutableInvocationEntryStaysProtectedAfterResolvingSymlink(t *testing.T) {
	root := t.TempDir()
	bin, target := filepath.Join(root, "entry bin"), filepath.Join(root, "real bin/agent")
	for _, path := range []string{bin, filepath.Dir(target)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, []byte("stand-in"), 0o700); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(bin, "agent")
	if err := os.Symlink(target, entry); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveExecutable(t.Context(), "agent", entry)
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protectedLaunchPaths(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkProtectedWrites([]pathRule{{bin, pathWrite, false}}, protected); err == nil {
		t.Fatal("writable invocation directory could replace the executable symlink")
	}
}
