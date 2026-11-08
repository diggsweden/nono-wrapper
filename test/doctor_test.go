// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newDoctorFixture(t *testing.T) (launcherFixture, string) {
	t.Helper()
	f := newLauncherFixture(t, "opencode")
	writeLauncherFixture(t, filepath.Join(f.bin, "codex"), "#!/bin/sh\nexit 99\n")
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
if [ "$1" = --version ]; then
  if [ "${VERIFY_MODE:-}" = version-fails ]; then
    printf 'mise: config is not trusted\nReview the config before trusting it.\n' >&2
    exit 1
  fi
  printf 'nono 0.81.0\n'
  exit 0
fi
if [ "$1 $2 $3" = 'profile show --json' ]; then
  [ "${VERIFY_MODE:-}" != profile-missing ] || { printf 'nono: Profile not found: %s\n' "$4" >&2; exit 1; }
  if [ "${VERIFY_MODE:-}" = network-enabled ]; then
    printf '{"network":{"block":false,"allow_domain":["chatgpt.com"]}}\n'
  else
    printf '{"network":{"block":true}}\n'
  fi
  exit 0
fi
if [ "$5 $6" = '--isolate net,loopback' ]; then
  [ "${VERIFY_MODE:-}" != namespace-denied ] || { printf 'user namespace denied\n' >&2; exit 1; }
  set -- "$1" "$2" "$3" "$4" "$7" "$8" "$9" "${10}" "${11}"
fi
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

func TestDoctorUsesXDGConfigHome(t *testing.T) {
	f, log := newDoctorFixture(t)
	config := filepath.Join(filepath.Dir(f.bin), "actual config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, filepath.Join(config, "settings"), "fixture\n")
	stdout, stderr, status := runLauncher(t, f, "doctor", []string{"codex"}, []string{
		"VERIFY_LOG=" + log, "VERIFY_MODE=config-readable", "XDG_CONFIG_HOME=" + config,
	})
	if status != 1 || stderr != "" || !strings.Contains(string(stdout), "  FAIL  the sandbox hides your configuration directory") {
		t.Fatalf("readable XDG configuration was missed: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
}

func TestDoctorRejectsNetworkEnabledOfflineProfile(t *testing.T) {
	f, log := newDoctorFixture(t)
	stdout, stderr, status := runLauncher(t, f, "doctor", []string{"codex"}, []string{
		"VERIFY_LOG=" + log, "VERIFY_MODE=network-enabled",
	})
	if status != 1 || stderr != "" || !strings.Contains(string(stdout), "network.block must be true") ||
		strings.Contains(string(stdout), "Installed offline-profile checks passed.") {
		t.Fatalf("network-enabled offline profile was accepted: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("probes ran under a network-enabled profile: %v", err)
	}
}

// Without installed profiles every sandbox check fails too, and their
// namespace advice would send a person to AppArmor instead of the install.
func TestDoctorNamesMissingProfilesAndTheirInstall(t *testing.T) {
	f, log := newDoctorFixture(t)
	stdout, _, status := runLauncher(t, f, "doctor", []string{"codex"}, []string{
		"VERIFY_LOG=" + log, "VERIFY_MODE=profile-missing",
	})
	out := string(stdout)
	if status != 1 || !strings.Contains(out, "profile codex-offline cannot be loaded") ||
		!strings.Contains(out, "Profile not found") || !strings.Contains(out, "nono-wrapper install-profiles") ||
		!strings.Contains(out, "/docs/install.md") || strings.Contains(out, "private loopback") {
		t.Fatalf("missing profile was not reported as such: status=%d\n%s", status, out)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("probes ran without a loadable profile: %v", err)
	}
}

func TestDoctorRelativeTempDir(t *testing.T) {
	f, log := newDoctorFixture(t)
	temp := filepath.Join(f.project, "tmp")
	if err := os.Mkdir(temp, 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, status := runLauncher(t, f, "doctor", []string{"codex"}, []string{
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

func TestDoctorOutcomesAndCleanup(t *testing.T) {
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
		{"unusable nono cannot pass", "version-fails", []string{"codex"}, 1, 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, log := newDoctorFixture(t)
			home := filepath.Dir(f.bin)
			codexHome := filepath.Join(home, "custom codex home")
			keep := filepath.Join(home, "keep")
			writeLauncherFixture(t, keep, "keep\n")
			stdout, stderr, status := runLauncher(t, f, "doctor", test.agents, []string{
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
				t.Fatalf("doctor success summary contradicts outcome: %s", stdout)
			}
			if test.mode == "version-fails" && !strings.Contains(string(stdout), "        mise: config is not trusted\n        Review the config before trusting it.\n") {
				t.Fatalf("doctor discarded nono's failure diagnostic: %s", stdout)
			}
			if passed && !strings.Contains(string(stdout), "ChatGPT login, your additional access rules and the full agent launch were not tested.") {
				t.Fatalf("doctor must describe its limited scope: %s", stdout)
			}
			agents := test.agents
			if len(agents) == 0 {
				agents = []string{"opencode", "codex"}
			}
			for _, agent := range []string{"opencode", "codex"} {
				named := strings.Contains(string(stdout), "'nono-wrapper "+agent)
				if passed && named != slices.Contains(agents, agent) {
					t.Fatalf("the next step must name the checked agents only: %s", stdout)
				}
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

func TestDoctorRequiresOnlySelectedAgents(t *testing.T) {
	for _, test := range []struct {
		name, missing string
		agents        []string
		status        int
	}{
		{"missing requested agent", "codex", []string{"codex"}, 1},
		{"default requires both agents", "codex", nil, 1},
		{"OpenCode-only installation", "codex", []string{"opencode"}, 0},
		{"Codex-only installation", "opencode", []string{"codex"}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, log := newDoctorFixture(t)
			if err := os.Remove(filepath.Join(f.bin, test.missing)); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, status := runLauncher(t, f, "doctor", test.agents, []string{"VERIFY_LOG=" + log})
			text := string(stdout)
			if status != test.status || stderr != "" {
				t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if test.status == 1 && (!strings.Contains(text, "  FAIL  "+test.missing+" is not on PATH") ||
				strings.Contains(text, "Installed offline-profile checks passed.")) {
				t.Fatalf("missing selected agent was not reported as a failed check: %s", text)
			}
		})
	}
}

func TestDoctorReportsNamespaceFailureForBundledAgents(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("private loopback is Linux-only")
	}
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			f, log := newDoctorFixture(t)
			env := []string{"VERIFY_LOG=" + log, "VERIFY_MODE=namespace-denied"}
			stdout, stderr, status := runLauncher(t, f, "doctor", []string{agent}, env)
			advice := "namespace requirements"
			if agent == "codex" {
				advice = "Offline Codex sessions on Linux require private loopback; online sessions do not."
			}
			if status != 1 || !strings.Contains(string(stdout), "user namespace denied") ||
				!strings.Contains(string(stdout), advice) ||
				!strings.Contains(string(stdout), "README.md#ubuntu-apparmor-setup") || strings.Contains(probeOutput(stdout), "  ok    ") {
				t.Fatalf("unsupported namespace produced successful checks: %d %s %s", status, stdout, stderr)
			}
		})
	}
}

func TestDoctorResolvesMiseAgentsWithoutExecutingThem(t *testing.T) {
	for _, state := range []string{"active", "inactive"} {
		t.Run(state, func(t *testing.T) {
			f, log := newDoctorFixture(t)
			marker := filepath.Join(filepath.Dir(f.bin), "agent-executed")
			target := filepath.Join(filepath.Dir(f.target), "codex")
			writeLauncherFixture(t, target, "#!/bin/sh\nprintf executed > \"$DOCTOR_AGENT_MARKER\"\nexit 99\n")
			mise := filepath.Join(f.bin, "mise")
			writeLauncherFixture(t, mise, `#!/bin/sh
[ "$1 $2" = 'which codex' ] || exit 99
if [ "$DOCTOR_AGENT_STATE" = inactive ]; then
  printf 'codex is not currently active\n' >&2
  exit 1
fi
printf '%s\n' "$DOCTOR_AGENT_TARGET"
`)
			if err := os.Remove(filepath.Join(f.bin, "codex")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(mise, filepath.Join(f.bin, "codex")); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, status := runLauncher(t, f, "doctor", []string{"codex"}, []string{
				"VERIFY_LOG=" + log, "DOCTOR_AGENT_TARGET=" + target, "DOCTOR_AGENT_MARKER=" + marker,
				"DOCTOR_AGENT_STATE=" + state,
			})
			wantStatus, wantText := 0, "codex found at "+target
			if state == "inactive" {
				wantStatus, wantText = 1, "codex is not currently active"
			}
			if status != wantStatus || stderr != "" || !strings.Contains(string(stdout), wantText) {
				t.Fatalf("agent resolution: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("doctor executed the agent outside nono: %v", err)
			}
		})
	}
}

func TestDoctorPrerequisites(t *testing.T) {
	for _, name := range []string{"nono", "curl", "ls", "touch"} {
		t.Run(name, func(t *testing.T) {
			f, log := newDoctorFixture(t)
			if err := os.Remove(filepath.Join(f.bin, name)); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, status := runLauncher(t, f, "doctor", nil, []string{"VERIFY_LOG=" + log})
			if status != 127 || len(stdout) != 0 || !strings.Contains(stderr, name) {
				t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("probes ran with a missing prerequisite: %v", err)
			}
		})
	}
}

func TestDoctorInterruptedCleanup(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(signal.String(), func(t *testing.T) {
			testVerifyInterruptedCleanup(t, signal)
		})
	}
}

func testVerifyInterruptedCleanup(t *testing.T, signal os.Signal) {
	t.Helper()
	f, log := newDoctorFixture(t)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sleep, filepath.Join(f.bin, "sleep")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, wrapperBinary, "doctor", "codex")
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

func TestDoctorReportsSetupProblems(t *testing.T) {
	f, log := newDoctorFixture(t)
	// Agents run only under nono: verify locates them without executing them.
	ran := filepath.Join(filepath.Dir(f.bin), "agent-ran")
	writeLauncherFixture(t, f.target, "#!/bin/sh\ntouch '"+ran+"'\n")
	stdout, stderr, status := runLauncher(t, f, "doctor", []string{"opencode"}, []string{
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
