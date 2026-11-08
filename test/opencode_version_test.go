// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOpenCodeRequiresMajor2InsideTheSandbox(t *testing.T) {
	for _, mode := range []struct {
		name       string
		args       []string
		standalone bool
	}{
		{"standalone", []string{"run", "argument with spaces", "literal*value", "line\nbreak", "--", "-tail"}, true},
		{"server", []string{"--print-logs", "serve", "--port", "4096"}, false},
		{"client", []string{"--server", "http://127.0.0.1:4096", "session", "list"}, false},
	} {
		for _, version := range []struct {
			name, text, exit string
			accept           bool
		}{
			{"old major", "1.19.3", "0", false},
			{"labelled old major", "opencode v1.19.3", "0", false},
			{"older major", "0.9.0", "0", false},
			{"first v2", "2.0.0", "0", true},
			{"labelled v2", "opencode v2.2.5", "0", true},
			{"unprefixed label", "opencode 2.1.0", "0", true},
			{"v-prefixed v2", "v2.84.0", "0", true},
			{"future v2", "2.999.999", "0", true},
			{"prerelease", "opencode v2.84.0-rc.1+build.5", "0", true},
			{"different future major", "opencode v3.0.0", "0", false},
			{"unrecognized", "not a version", "0", false},
			{"malformed", "2.x.x", "0", false},
			{"multiline", "2.2.5\n1.19.3", "0", false},
			{"probe failure", "opencode v2.2.5", "7", false},
		} {
			t.Run(mode.name+"/"+version.name, func(t *testing.T) {
				f := newLauncherFixture(t, "opencode")
				root := filepath.Dir(f.bin)
				log, arguments := filepath.Join(root, "calls"), filepath.Join(root, "arguments")
				writeLauncherFixture(t, f.target, `#!/bin/sh
[ "${VERSION_CHECK_SANDBOX:-}" = 1 ] || exit 99
if [ "$1" = --version ]; then
  printf 'version\n' >>"$VERSION_CHECK_LOG"
  printf '%s\n' "$VERSION_CHECK_TEXT"
  exit "$VERSION_CHECK_EXIT"
fi
printf 'session\n' >>"$VERSION_CHECK_LOG"
printf '%s\000' "$@" >"$VERSION_CHECK_ARGUMENTS"
`)
				writeNonoFixture(t, f, `
case "$1" in
profile) printf '{"network":{"block":false},"workdir":{"access":"readwrite"}}\n' ;;
run)
  while [ "$1" != -- ]; do shift; done
  shift
  export VERSION_CHECK_SANDBOX=1
  exec "$@" ;;
*) exit 99 ;;
esac
`)
				_, stderr, status := runLauncher(t, f, "opencode", mode.args, []string{
					"VERSION_CHECK_LOG=" + log, "VERSION_CHECK_ARGUMENTS=" + arguments,
					"VERSION_CHECK_TEXT=" + version.text, "VERSION_CHECK_EXIT=" + version.exit,
				})
				calls, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				if !version.accept {
					if status != 125 || string(calls) != "version\n" || !strings.Contains(stderr, "OpenCode 2.x.x is required") {
						t.Fatalf("unsupported version started a session: status=%d calls=%q stderr=%q", status, calls, stderr)
					}
					if _, err := os.Stat(arguments); !os.IsNotExist(err) {
						t.Fatalf("requested command ran after version refusal: %v", err)
					}
					return
				}
				if status != 0 || string(calls) != "version\nsession\n" {
					t.Fatalf("OpenCode 2 was refused: status=%d calls=%q stderr=%q", status, calls, stderr)
				}
				data, err := os.ReadFile(arguments)
				if err != nil {
					t.Fatal(err)
				}
				want := append([]string{}, mode.args...)
				if mode.standalone {
					want = append([]string{"--standalone"}, want...)
				}
				got := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
				if !slices.Equal(got, want) {
					t.Fatalf("guard changed arguments: got=%q want=%q", got, want)
				}
			})
		}
	}
}
