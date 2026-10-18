// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPRequestEvidence(t *testing.T) {
	for _, test := range []struct {
		name, want          string
		http, connect, exit int
		body                string
		pass                bool
	}{
		{"site success", "WORKS", 200, 200, 0, "ok", true},
		{"site requires authentication", "WORKS", 401, 200, 0, "authentication required", true},
		{"site forbidden differs from proxy denial", "WORKS", 403, 200, 0, `{"detail":"not authorized"}`, true},
		{"endpoint denied", "FAILS", 403, 200, 0, `{"error":"Forbidden"}`, true},
		{"denied endpoint is not site success", "WORKS", 403, 200, 0, `{"error":"Forbidden"}`, false},
		{"CONNECT denied with curl 56", "FAILS", 0, 403, 56, "", true},
		{"CONNECT denied with curl 7", "FAILS", 0, 403, 7, "", true},
		{"CONNECT denial is not site success", "WORKS", 0, 403, 7, "", false},
		{"allowed endpoint is not a refusal", "FAILS", 200, 200, 0, "ok", false},
		{"upstream forbidden is not nono refusal", "FAILS", 403, 200, 0, `{"detail":"not authorized"}`, false},
		{"DNS failure is not refusal", "FAILS", 0, 0, 6, "", false},
		{"connection failure alone is not refusal", "FAILS", 0, 0, 7, "", false},
		{"proxy authentication failure is not refusal", "FAILS", 0, 407, 56, "", false},
		{"timeout is not refusal", "FAILS", 0, 0, 28, "", false},
		{"TLS failure is not refusal", "FAILS", 0, 200, 60, "", false},
		{"DNS failure is not site success", "WORKS", 0, 0, 6, "", false},
		{"gateway failure is not site success", "WORKS", 502, 200, 0, "bad gateway", false},
		{"gateway failure is not refusal", "FAILS", 502, 200, 0, "bad gateway", false},
		{"CONNECT failure is not refusal", "FAILS", 0, 502, 56, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"cat", "grep"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
					t.Fatal(err)
				}
			}
			writeLauncherFixture(t, filepath.Join(bin, "curl"), `#!/bin/sh
output= format=
while [ "$#" -gt 0 ]; do
  case "$1" in
  -o) shift; output=$1 ;;
  -w) shift; format=$1 ;;
  esac
  shift
done
[ -n "$output" ] && [ -n "$format" ] || exit 99
printf '%s' "$CURL_BODY" > "$output"
case "$format" in
*http_connect*) printf '%s %s\n' "$CURL_HTTP" "$CURL_CONNECT" ;;
*) printf '%s' "$CURL_HTTP" ;;
esac
exit "$CURL_EXIT"
`)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", `. "$1"; check_request "$2" GET https://fixture.invalid/`, "sh",
				filepath.Join(testRepository(t), "test/http-check.sh"), test.want)
			cmd.Dir = root
			cmd.Env = []string{"PATH=" + bin, "HOME=" + root, "LC_ALL=C", "CURL_BODY=" + test.body,
				fmt.Sprintf("CURL_HTTP=%03d", test.http), fmt.Sprintf("CURL_CONNECT=%03d", test.connect), fmt.Sprintf("CURL_EXIT=%d", test.exit)}
			cmd.WaitDelay = time.Second
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil || (err == nil) != test.pass {
				t.Fatalf("pass=%t, got %v (%s)", test.pass, err, output)
			}
		})
	}
}
