// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func testNativePublicKeySigning(t *testing.T, root, project, wrapper string, env []string) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("SSH signing requires ssh-keygen")
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "isolated signing fixture")
	if err != nil {
		t.Fatal(err)
	}
	secret := pem.EncodeToMemory(block)
	sshDir, bin := filepath.Join(root, "home/.ssh"), filepath.Join(root, "signing probes")
	for _, path := range []string{sshDir, bin} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string][]byte{
		"signing-fixture.pub":     ssh.MarshalAuthorizedKey(key),
		"private-signing-fixture": secret,
		"renamed-private.pub":     secret,
		"config":                  []byte("Host fixture.invalid\n"),
	} {
		if err := os.WriteFile(filepath.Join(sshDir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("private-signing-fixture", filepath.Join(sshDir, "linked-private.pub")); err != nil {
		t.Fatal(err)
	}
	// The signing key stays in this test process, outside the sandbox. Expose
	// only an isolated agent socket, never the user's real agent or private key.
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "signing-agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _ = agent.ServeAgent(keyring, conn) }()
		}
	}()
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
			break
		}
	}
	probeEnv = append(probeEnv, "SSH_AUTH_SOCK="+socket, "NONO_WRAPPER_SSH_AGENT=1")
	body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
cat "$HOME/.ssh/signing-fixture.pub" >/dev/null
if (printf changed > "$HOME/.ssh/signing-fixture.pub") 2>/dev/null; then exit 91; fi
for name in private-signing-fixture renamed-private.pub linked-private.pub config; do
  if cat "$HOME/.ssh/$name" >/dev/null 2>&1; then exit 92; fi
done
work=public-signing-${0##*/}
mkdir "$work"
cd "$work"
git init -q
git -c gpg.format=ssh -c user.signingkey="$HOME/.ssh/signing-fixture.pub" -c commit.gpgsign=true -c user.name=Fixture -c user.email=fixture@example.invalid commit -q --allow-empty -m 'isolated signing fixture'
git cat-file commit HEAD | grep -q 'BEGIN SSH SIGNATURE'
printf 'PUBLIC_KEY_SIGNING_OK\n'
`
	for _, name := range []string{"opencode", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()
		// Validate the default policy without explicitly requesting private-key
		// capabilities: the automatic exceptions must expose only public data.
		cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", name)
		cmd.Dir, cmd.Env, cmd.WaitDelay = project, probeEnv, time.Second
		output, err := cmd.CombinedOutput()
		if err != nil || !strings.Contains(string(output), "PUBLIC_KEY_SIGNING_OK") {
			t.Fatalf("public-key signing through %s: %v\n%s", name, err, output)
		}
	}
}
