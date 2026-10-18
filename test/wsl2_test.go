// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fake nono decides whether the host is WSL2, as the real one does. Its
// run command fails unless a rewritten profile lost only socket mediation.
func useWSL2Fixture(t *testing.T, f launcherFixture) string {
	t.Helper()
	profiles := filepath.Join(filepath.Dir(f.bin), ".config/nono/profiles")
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"opencode-chatgpt": `{"extends":"agent-policy","network":{"block":false}}`,
		"opencode-offline": `{"extends":"agent-policy","network":{"block":true}}`,
		"agent-policy":     `{"environment":{"allow_vars":["KEEP"]},"linux":{"af_unix_mediation":"pathname"}}`,
	} {
		writeLauncherFixture(t, filepath.Join(profiles, name+".jsonc"), data)
	}
	grep, err := exec.LookPath("grep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(grep, filepath.Join(f.bin, "grep")); err != nil {
		t.Fatal(err)
	}
	writeNonoFixture(t, f, `
case "$1" in
setup)
  [ "$#" -eq 2 ] && [ "$2" = --check-only ] || exit 99
  [ -z "${WSL2_FIXTURE:-}" ] || printf '[2/4] Testing sandbox support...\n  * WSL2 environment detected\n'
  exit 0 ;;
profile)
  [ "$#" -eq 4 ] && [ "$2 $3" = "show --json" ] || exit 99
  case "$4" in
    /*) printf '{"network":{"block":false}}\n' ;;
    *) printf '{"network":{"block":false},"linux":{"af_unix_mediation":"pathname"}}\n' ;;
  esac
  exit 0 ;;
run)
  profile=$3
  [ "$2" != --silent ] || profile=$4
  case "$profile" in
    /*)
      ! grep -q pathname "${profile%/*}"/*.json || exit 98
      grep -q KEEP "${profile%/*}"/*.json || exit 97 ;;
    *) [ -z "${WSL2_FIXTURE:-}" ] || exit 96 ;;
  esac
  if [ "$2" = --silent ]; then
    case "$9" in
      *touch*) exec /bin/sh -c "$9" ;;
      *) exit 1 ;;
    esac
  fi
  printf '%s\n' "$profile"
  exit 0 ;;
*) exit 99 ;;
esac
`)
	return profiles
}

func TestLauncherDropsSocketMediationOnWSL2(t *testing.T) {
	for _, test := range []struct {
		name string
		env  []string
		wsl2 bool
	}{
		{"nono reports WSL2", []string{"WSL_DISTRO_NAME=fixture", "WSL2_FIXTURE=1"}, true},
		{"hint without nono's confirmation", []string{"WSL_DISTRO_NAME=fixture"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLauncherFixture(t, "opencode")
			profiles := useWSL2Fixture(t, f)
			stdout, stderr, status := runLauncher(t, f, "opencode", nil, test.env)
			if status != 0 {
				t.Fatalf("launch failed: status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
			}
			profile := strings.TrimSpace(string(stdout))
			noted := strings.HasPrefix(stderr, "nono-wrapper: WSL2 cannot run the seccomp listener")
			if test.wsl2 && (!noted || !strings.HasPrefix(profile, profiles+string(filepath.Separator))) {
				t.Fatalf("socket mediation was kept on WSL2: profile=%q, stderr=%q", profile, stderr)
			}
			if !test.wsl2 && (profile != "opencode-chatgpt" || stderr != emptySessionReport("opencode", 0)) {
				t.Fatalf("socket mediation was dropped without nono reporting WSL2: profile=%q, stderr=%q", profile, stderr)
			}
			if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 3 {
				t.Fatalf("generated policy residue remains: %v (%v)", entries, err)
			}
		})
	}
}

func TestVerifyNotesMissingSocketMediationOnWSL2(t *testing.T) {
	f, _ := newVerifyFixture(t)
	profiles := useWSL2Fixture(t, f)
	stdout, stderr, status := runLauncher(t, f, "verify", []string{"opencode"}, []string{"WSL_DISTRO_NAME=fixture", "WSL2_FIXTURE=1"})
	if status != 0 || stderr != "" || strings.Count(probeOutput(stdout), "  ok    ") != 5 ||
		strings.Count(string(stdout), "  note  WSL2 cannot run the seccomp listener") != 1 {
		t.Fatalf("status=%d, stdout=%q, stderr=%q", status, stdout, stderr)
	}
	if entries, err := os.ReadDir(profiles); err != nil || len(entries) != 3 {
		t.Fatalf("generated policy residue remains: %v (%v)", entries, err)
	}
}
