// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newVerifyFixture(t *testing.T) (launcherFixture, string) {
	t.Helper()
	f := newLauncherFixture(t, "opencode")
	writeLauncherFixture(t, filepath.Join(f.bin, "curl"), "#!/bin/sh\nexit 99\n")
	for _, name := range []string{"ls", "touch"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(f.bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(filepath.Dir(f.bin), "verify.log")
	writeLauncherFixture(t, filepath.Join(f.bin, "nono"), `#!/bin/sh
[ "$#" -eq 9 ] && [ "$1" = run ] && [ "$2" = --silent ] && [ "$3" = --profile ] &&
  [ "$5" = --allow-cwd ] && [ "$6" = -- ] && [ "$7" = /bin/sh ] && [ "$8" = -c ] || exit 99
printf '%s\000' "$4" "$PWD" "$TMPDIR" "$CODEX_HOME" >> "$VERIFY_LOG"
case "${VERIFY_MODE:-}" in
deny) exit 1 ;;
allow) exit 0 ;;
wait) exec sleep 60 ;;
esac
case "$9" in
*touch*) exec /bin/sh -c "$9" ;;
'ls "$HOME"') exit 1 ;;
ls*)
  if [ "${VERIFY_MODE:-}" = config-readable ]; then exec /bin/sh -c "$9"; fi
  [ "${VERIFY_MODE:-}" = unavailable ] && exit 127; exit 1
  ;;
*) [ "${VERIFY_MODE:-}" = unavailable ] && exit 127; exit 1 ;;
esac
`)
	return f, log
}

func TestVerifyUsesXDGConfigHome(t *testing.T) {
	f, log := newVerifyFixture(t)
	config := filepath.Join(filepath.Dir(f.bin), "actual config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, filepath.Join(config, "settings"), "fixture\n")
	stdout, stderr, status := runLauncher(t, f, "verify", []string{"codex"}, []string{
		"VERIFY_LOG=" + log, "VERIFY_MODE=config-readable", "XDG_CONFIG_HOME=" + config,
	})
	if status != 1 || stderr != "" || !strings.Contains(string(stdout), "  FAIL  your configuration directory cannot be listed") {
		t.Fatalf("readable XDG configuration was missed: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}

func TestVerifyRelativeTempDir(t *testing.T) {
	f, log := newVerifyFixture(t)
	temp := filepath.Join(f.project, "tmp")
	if err := os.Mkdir(temp, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runLauncher(t, f, "verify", []string{"codex"}, []string{
		"VERIFY_LOG=" + log, "TMPDIR=tmp",
	})
	if status != 0 || stderr != "" || strings.Count(probeOutput(stdout), "  ok    ") != 5 {
		t.Fatalf("relative TMPDIR failed: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
	entries, err := os.ReadDir(temp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("verification left temporary data: %v (%v)", entries, err)
	}
}

func TestVerifyOutcomesAndCleanup(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		agents     []string
		status     int
		checks     int
	}{
		{"both profiles", "", nil, 0, 5},
		{"one profile", "", []string{"codex"}, 0, 5},
		{"failed positive control stops that profile", "deny", nil, 1, 1},
		{"all allowed cannot pass", "allow", nil, 1, 5},
		{"missing sandbox utilities cannot pass", "unavailable", nil, 1, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, log := newVerifyFixture(t)
			home := filepath.Dir(f.bin)
			codexHome := filepath.Join(home, "custom codex home")
			keep := filepath.Join(home, "keep")
			writeLauncherFixture(t, keep, "keep\n")
			stdout, stderr, status := runLauncher(t, f, "verify", test.agents, []string{
				"VERIFY_LOG=" + log, "VERIFY_MODE=" + test.mode, "CODEX_HOME=" + codexHome,
				"NONO_PROFILE=ignored-for-verification",
			})
			if status != test.status || stderr != "" || (test.status == 1 && !strings.Contains(string(stdout), "  FAIL  ")) {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
			if test.mode == "deny" && strings.Contains(probeOutput(stdout), "  ok    ") {
				t.Fatalf("failed sandbox produced successful refusal checks: %s", stdout)
			}
			passed := strings.Contains(string(stdout), "Installed offline-profile checks passed.")
			if passed != (test.status == 0) {
				t.Fatalf("verification success summary contradicts outcome: %s", stdout)
			}
			if passed && !strings.Contains(string(stdout), "ChatGPT login and your additional access rules were not tested.") {
				t.Fatalf("verification must describe its limited scope: %s", stdout)
			}
			agents := test.agents
			if len(agents) == 0 {
				agents = []string{"opencode", "codex"}
			}
			contents, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Split(strings.TrimSuffix(string(contents), "\x00"), "\x00")
			if len(fields) != len(agents)*test.checks*4 {
				t.Fatalf("expected %d probes per agent, got log %q", test.checks, fields)
			}
			for index := 0; index < len(fields); index += 4 {
				profile, project, temp, gotHome := fields[index], fields[index+1], fields[index+2], fields[index+3]
				if profile != agents[index/(test.checks*4)]+"-offline" || gotHome != codexHome || filepath.Dir(project) != filepath.Dir(temp) || project == temp {
					t.Fatalf("incorrect probe environment: %q", fields[index:index+4])
				}
				for _, path := range []string{project, temp} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatalf("verification left temporary files: %s (%v)", path, err)
					}
				}
			}
			if contents, err := os.ReadFile(keep); err != nil || string(contents) != "keep\n" {
				t.Fatalf("cleanup changed an unrelated file: %v", err)
			}
		})
	}
}

func TestVerifyPrerequisites(t *testing.T) {
	for _, name := range []string{"nono", "curl", "ls", "touch"} {
		t.Run(name, func(t *testing.T) {
			f, log := newVerifyFixture(t)
			if err := os.Remove(filepath.Join(f.bin, name)); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, status := runLauncher(t, f, "verify", nil, []string{"VERIFY_LOG=" + log})
			if status != 127 || len(stdout) != 0 || !strings.Contains(stderr, name) {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("probes ran with a missing prerequisite: %v", err)
			}
		})
	}
}

func TestVerifyInterruptedCleanup(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(signal.String(), func(t *testing.T) {
			testVerifyInterruptedCleanup(t, signal)
		})
	}
}

func testVerifyInterruptedCleanup(t *testing.T, signal os.Signal) {
	t.Helper()
	f, log := newVerifyFixture(t)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sleep, filepath.Join(f.bin, "sleep")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wrapperBinary, "verify", "codex")
	cmd.Env = []string{"PATH=" + f.bin, "HOME=" + filepath.Dir(f.bin), "TMPDIR=" + filepath.Dir(f.bin), "VERIFY_LOG=" + log, "VERIFY_MODE=wait"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var contents []byte
	for ctx.Err() == nil {
		contents, _ = os.ReadFile(log)
		if strings.Count(string(contents), "\x00") == 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := cmd.Process.Signal(signal); err != nil {
		cancel()
		cmd.Wait()
		t.Fatal(err)
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if ctx.Err() != nil || !errors.As(err, &exit) {
		t.Fatalf("verification did not stop after interruption: %v", err)
	}
	fields := strings.Split(string(contents), "\x00")
	for _, path := range fields[1:3] {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("interrupted verification left temporary files: %s (%v)", path, err)
		}
	}
}

// probeOutput drops verify's setup section, leaving the sandbox probes.
func probeOutput(stdout []byte) string {
	text := string(stdout)
	if index := strings.Index(text, " under nono (profile "); index >= 0 {
		return text[index:]
	}
	return ""
}

func TestVerifyReportsSetupProblems(t *testing.T) {
	f, log := newVerifyFixture(t)
	// Agents run only under nono: verify locates them without executing them.
	ran := filepath.Join(filepath.Dir(f.bin), "agent-ran")
	writeLauncherFixture(t, f.target, "#!/bin/sh\ntouch '"+ran+"'\n")
	stdout, stderr, status := runLauncher(t, f, "verify", []string{"opencode"}, []string{
		"VERIFY_LOG=" + log, "NONO_WRAPPER_ROOT=" + filepath.Join(f.project, "missing"),
		"HTTPS_PROXY=http://user:secret@proxy.example:3128",
	})
	text := string(stdout)
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("verify ran an agent outside the sandbox")
	}
	if status != 1 || stderr != "" || !strings.Contains(text, "  FAIL  the project root") ||
		!strings.Contains(text, "  note  HTTPS_PROXY contains a user name or password") || strings.Contains(text, "secret") {
		t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}
