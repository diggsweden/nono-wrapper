// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPodmanEnvironmentRequiresOwnedSocket(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "podman.sock")
	if got := podmanEnvironmentForSocket(path, "CONTAINER_HOST"); len(got) != 0 {
		t.Fatalf("missing socket selected: %v", got)
	}
	file := filepath.Join(root, "regular-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := podmanEnvironmentForSocket(file, "CONTAINER_HOST"); len(got) != 0 {
		t.Fatalf("regular file selected: %v", got)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if got := podmanEnvironmentForSocket(path, "CONTAINER_HOST"); !slices.Equal(got, []string{"CONTAINER_HOST=unix://" + path}) {
		t.Fatalf("owned socket not selected: %v", got)
	}
	alias := filepath.Join(root, "alias.sock")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if got := podmanEnvironmentForSocket(alias, "CONTAINER_HOST"); len(got) != 0 {
		t.Fatalf("symlink selected as default socket: %v", got)
	}
}

func TestPodmanEnvironmentPreservesExplicitTransport(t *testing.T) {
	defaults, err := loadRuntimeDefaults()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"CONTAINER_HOST", "CONTAINER_CONNECTION"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "explicit")
			if got := podmanSessionEnvironment("opencode-chatgpt", defaults); len(got) != 0 {
				t.Fatalf("explicit transport overridden: %v", got)
			}
		})
	}
	if got := podmanSessionEnvironment("custom", defaults); len(got) != 0 {
		t.Fatalf("external profile changed: %v", got)
	}
}
