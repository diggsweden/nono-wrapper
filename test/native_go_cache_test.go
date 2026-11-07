// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"archive/zip"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testNativeGoCaches(t *testing.T, root, project, wrapper string, env []string) {
	goProgram := filepath.Join(runtime.GOROOT(), "bin/go")
	host, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	for _, mode := range []string{"default", "gopath", "overrides"} {
		t.Run(mode, func(t *testing.T) {
			work, bin := filepath.Join(project, "go cache "+mode), filepath.Join(root, "go probes "+mode)
			gopath := filepath.Join(root, "home/go")
			gopathEnv := ""
			if mode != "default" {
				gopath = filepath.Join(root, "custom gopath")
				gopathEnv = gopath + string(os.PathListSeparator) + filepath.Join(root, "secondary gopath")
			}
			modules := filepath.Join(gopath, "pkg/mod")
			moduleEnv, buildCacheEnv := "", ""
			if mode == "overrides" {
				modules = filepath.Join(root, "custom module cache")
				moduleEnv = modules
				buildCacheEnv = filepath.Join(root, "custom Go build cache")
			}
			proxy := filepath.Join(work, "proxy/fixture.test/dependency/@v")
			for _, dir := range []string{work, bin, modules, proxy, filepath.Join(gopath, "bin")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for path, data := range map[string]string{
				filepath.Join(work, "go.mod"): "module fixture.test/cache\n\ngo 1.24\n\nrequire fixture.test/dependency v1.0.0\n",
				filepath.Join(work, "cache_test.go"): fmt.Sprintf(`package cache
import (
  "fixture.test/dependency"
  "net"
  "net/http"
  "net/http/httptest"
  "runtime"
  "testing"
  "time"
)
func TestBuildAndLoopback(t *testing.T) {
  if dependency.Value != 42 { t.Fatal("wrong dependency") }
  if runtime.GOOS != "linux" { return }
  conn, err := net.DialTimeout("tcp", %q, time.Second)
  if err == nil { conn.Close(); t.Fatal("private loopback reached the host listener") }
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
  defer server.Close()
  client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
  defer client.CloseIdleConnections()
  response, err := client.Get(server.URL)
  if err != nil { t.Fatal(err) }
  response.Body.Close()
  if response.StatusCode != http.StatusNoContent { t.Fatal(response.Status) }
}
`, host.Addr().String()),
				filepath.Join(proxy, "v1.0.0.mod"):          "module fixture.test/dependency\n\ngo 1.24\n",
				filepath.Join(proxy, "v1.0.0.info"):         `{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`,
				filepath.Join(gopath, "bin/protected-tool"): "installed tool\n",
			} {
				if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			archive, err := os.Create(filepath.Join(proxy, "v1.0.0.zip"))
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(archive)
			for name, data := range map[string]string{"go.mod": "module fixture.test/dependency\n\ngo 1.24\n", "dependency.go": "package dependency\nconst Value = 42\n"} {
				entry, err := writer.Create("fixture.test/dependency@v1.0.0/" + name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			probeEnv := append([]string{}, env...)
			for i := len(env) - 1; i >= 0; i-- {
				if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
					probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
					break
				}
			}
			probeEnv = append(probeEnv, "GOPATH="+gopathEnv, "GOMODCACHE="+moduleEnv, "GOCACHE="+buildCacheEnv)
			body := `#!/bin/sh
if [ "${1:-}" = --version ]; then printf 'opencode v2.0.0\n'; exit 0; fi
set -eu
while [ "${1:-}" != --fixture ]; do shift; done
shift
go_program=$1
modules=$2
tool=$3
[ "$("$go_program" env GOMODCACHE)" = "$modules" ]
if (printf changed > "$tool") 2>/dev/null; then exit 91; fi
export GOPROXY="file://$PWD/proxy" GOSUMDB=off GOFLAGS=-modcacherw
"$go_program" test -mod=mod -count=1 ./...
"$go_program" test -mod=readonly -count=1 ./...
printf 'GO_CACHES_SCOPED_OK\n'
`
			for _, agent := range []string{"opencode", "codex"} {
				if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				// The isolated HOME intentionally has no host toolchain install.
				// Share compiler code read-only, never the host cache or trust state.
				cmd := exec.CommandContext(ctx, wrapper, "run", "--block-net", "--read", runtime.GOROOT(), agent, "--fixture", goProgram, modules, filepath.Join(gopath, "bin/protected-tool"))
				cmd.Dir, cmd.Env, cmd.WaitDelay = work, probeEnv, time.Second
				output, err := cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(output), "GO_CACHES_SCOPED_OK") {
					t.Fatalf("Go caches through %s: %v\n%s", agent, err, output)
				}
			}
		})
	}
}
