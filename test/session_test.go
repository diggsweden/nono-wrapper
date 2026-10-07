// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLauncherReportsNetworkEvents(t *testing.T) {
	const audit = `{"session_id":"0123456789abcdef","audit_event_count":6,"network_events":[
{"decision":"allow","target":"chatgpt.com","port":443,"method":"CONNECT"},
{"decision":"deny","target":"svt.se","port":443,"reason":"host not in allowlist"},
{"decision":"deny","target":"svt.se","port":443,"reason":"host not in allowlist"},
{"decision":"deny","target":"auth.openai.com","port":443,"method":"GET","path":"/private","reason":"endpoint denied"}
]}`
	for _, spec := range launcherSpecs {
		for _, exit := range []int{0, 7, 130} {
			t.Run(fmt.Sprintf("%s/exit%d", spec.agent, exit), func(t *testing.T) {
				f := newLauncherFixture(t, spec.agent)
				log := filepath.Join(filepath.Dir(f.bin), "report.log")
				stdout, stderr, status := runLauncher(t, f, spec.agent, nil, []string{
					"LAUNCHER_AUDIT_JSON=" + audit, "LAUNCHER_REPORT_LOG=" + log,
					"LAUNCHER_TEST_EXIT=" + strconv.Itoa(exit),
				})
				if status != exit {
					t.Fatalf("report changed exit status: %d (%s)", status, stderr)
				}
				for _, want := range []string{
					fmt.Sprintf("%s session ended (exit %d)", spec.agent, exit),
					"Network: 4 recorded events", `"allow" "chatgpt.com:443"`,
					`"deny" "svt.se:443" reason="host not in allowlist" (2 events)`,
					`method="GET" path="/private" reason="endpoint denied"`,
					"nono audit show 0123456789abcdef", "nono why --profile PROFILE --host HOST",
				} {
					if !strings.Contains(stderr, want) {
						t.Fatalf("report missing %q:\n%s", want, stderr)
					}
				}
				if strings.Count(stderr, `"svt.se:443"`) != 1 {
					t.Fatalf("repeated events were not grouped: %s", stderr)
				}
				args := strings.Split(string(stdout), "\x00")
				name := args[slices.Index(args, "--name")+1]
				contents, err := os.ReadFile(log)
				want := "inspect " + name + " --json\naudit show 0123456789abcdef --json\n"
				if err != nil || string(contents) != want {
					t.Fatalf("did not query this exact run: %q (%v)", contents, err)
				}
			})
		}
	}
}

func TestLauncherReportFailuresPreserveExit(t *testing.T) {
	for _, test := range []struct{ name, mode, audit string }{
		{name: "runtime session missing", mode: "missing"},
		{name: "invalid inspection JSON", mode: "invalid"},
		{name: "wrong runtime session", mode: "wrong-name"},
		{name: "no audit recorded", mode: "no-audit"},
		{name: "audit query failed", mode: "audit-fails"},
		{name: "audit malformed", audit: "{"},
		{name: "audit null", audit: "null"},
		{name: "wrong audit session", audit: `{"session_id":"ffffffffffffffff","network_events":[{"target":"unrelated-session.example"}]}`},
	} {
		for _, agent := range []string{"opencode", "codex"} {
			for _, exit := range []int{0, 7} {
				t.Run(fmt.Sprintf("%s/%s/exit%d", test.name, agent, exit), func(t *testing.T) {
					f := newLauncherFixture(t, agent)
					_, stderr, status := runLauncher(t, f, agent, nil, []string{
						"LAUNCHER_TEST_EXIT=" + strconv.Itoa(exit), "LAUNCHER_REPORT_MODE=" + test.mode,
						"LAUNCHER_AUDIT_JSON=" + test.audit,
					})
					if status != exit || !strings.Contains(stderr, "Session report unavailable:") ||
						!strings.Contains(stderr, "nono audit list --command "+agent) ||
						strings.Contains(stderr, "unrelated-session.example") {
						t.Fatalf("incorrect reporting failure: status=%d (%s)", status, stderr)
					}
				})
			}
		}
	}
}

func TestLauncherReportTimeout(t *testing.T) {
	f := newLauncherFixture(t, "opencode")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wrapperBinary, "opencode")
	cmd.Env = []string{"PATH=" + f.bin, "LAUNCHER_REPORT_MODE=timeout", "LAUNCHER_TEST_EXIT=7"}
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != 7 ||
		!bytes.Contains(output, []byte("Session report unavailable:")) {
		t.Fatalf("report timeout lost status or hung: %v (%s)", err, output)
	}
}

func TestLauncherForwardsSignalsAndReports(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
			t.Run(agent+"/"+sig.String(), func(t *testing.T) {
				f := newLauncherFixture(t, agent)
				ready := filepath.Join(filepath.Dir(f.bin), "ready")
				writeNonoFixture(t, f, `
[ "$1" = profile ] && { printf '{"network":{"block":false}}\n'; exit 0; }
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
printf ready > "$LAUNCHER_READY"
while :; do /bin/sleep 0.01; done
`)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, wrapperBinary, agent)
				cmd.Env = []string{"PATH=" + f.bin, "LAUNCHER_READY=" + ready}
				cmd.WaitDelay = time.Second
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				for ctx.Err() == nil {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				if err := cmd.Process.Signal(sig); err != nil {
					cancel()
					cmd.Wait()
					t.Fatal(err)
				}
				err := cmd.Wait()
				var exit *exec.ExitError
				code := 128 + int(sig)
				if ctx.Err() != nil || !errors.As(err, &exit) || exit.ExitCode() != code || stderr.String() != emptySessionReport(agent, code) {
					t.Fatalf("signal was not forwarded or report was canceled: %v (%s)", err, &stderr)
				}
			})
		}
	}
}

func TestLauncherKeepsTerminalDescriptors(t *testing.T) {
	// util-linux script supplies a real PTY without adding a Go dependency.
	if runtime.GOOS != "linux" {
		t.Skip("requires util-linux script")
	}
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("script is not installed")
	}
	f := newLauncherFixture(t, "opencode")
	writeNonoFixture(t, f, `
[ "$1" = profile ] && { printf '{"network":{"block":false}}\n'; exit 0; }
[ -t 0 ] && [ -t 1 ] && [ -t 2 ] || exit 99
printf 'all descriptors are terminals\n'
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, script, "-q", "-e", "-c", "'"+strings.ReplaceAll(wrapperBinary, "'", "'\\''")+"' opencode", os.DevNull)
	cmd.Env = []string{"PATH=" + f.bin, "SHELL=/bin/sh", "TERM=xterm"}
	output, err := cmd.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("all descriptors are terminals")) ||
		!bytes.Contains(output, []byte("opencode session ended (exit 0)")) {
		t.Fatalf("terminal or post-exit output lost: %v (%s)", err, output)
	}
}
