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

func TestContainerRunnerNeverPrepares(t *testing.T) {
	for _, test := range []struct {
		name, mode, network string
		status              int
	}{
		{"prepared image offline", "ready", "0", 0},
		{"prepared image network enabled", "ready", "1", 0},
		{"missing image offline", "missing", "0", 1},
		{"missing image network enabled", "missing", "1", 1},
		{"stale image offline", "stale", "0", 1},
		{"stale image network enabled", "stale", "1", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newTaskFixture(t)
			fakeLinuxController(t, f)
			guard, err := os.ReadFile(filepath.Join(testRepository(t), "test/require-linux.sh"))
			if err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, filepath.Join(f.root, "test/require-linux.sh"), string(guard))
			content, err := os.ReadFile(filepath.Join(testRepository(t), "test/containers.sh"))
			if err != nil {
				t.Fatal(err)
			}
			writeLauncherFixture(t, filepath.Join(f.root, "test/containers.sh"), string(content))
			writeLauncherFixture(t, filepath.Join(f.root, "test/prepare.sh"), "#!/bin/sh\nprintf 'called\\n' > \"$RUNNER_PREPARE_LOG\"\nexit 23\n")
			writeLauncherFixture(t, filepath.Join(f.root, "test/image-inputs.sh"), "#!/bin/sh\nprintf 'fixture-base\\nfixture\\ninput-checksums\\n'\n")
			for _, name := range []string{"sh", "dirname", "grep", "cut", "mktemp", "chmod"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(f.tools, name)); err != nil {
					t.Fatal(err)
				}
			}
			writeLauncherFixture(t, filepath.Join(f.tools, "podman"), `#!/bin/sh
printf '%s\n' "$*" >> "$TASK_PODMAN_LOG"
case "$1" in
image) [ "$RUNNER_MODE" != missing ] ;;
run)
  record=
  for arg do
    case "$arg" in *:/run/nono-agent-profiles/expected.inputs:ro) record=${arg%:/run/nono-agent-profiles/expected.inputs:ro} ;; esac
  done
  [ -n "$record" ] || exit 99
  IFS= read -r base < "$record"
  [ "$base" = fixture-base ] || exit 99
  if [ "$RUNNER_MODE" = stale ]; then
    printf 'container: image inputs changed; rebuild with just test-prepare\n' >&2
    exit 1
  fi
  ;;
*) exit 99 ;;
esac
`)
			prepareLog := filepath.Join(f.home, "prepare.log")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(f.tools, "sh"), filepath.Join(f.root, "test/containers.sh"), "fixture")
			cmd.Dir = f.home
			cmd.Env = []string{"PATH=" + f.tools, "HOME=" + f.home, "TMPDIR=" + f.home, "TASK_PODMAN_LOG=" + f.log,
				"RUNNER_MODE=" + test.mode, "RUNNER_PREPARE_LOG=" + prepareLog, "AGENT_TEST_NETWORK=" + test.network}
			cmd.WaitDelay = time.Second
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if ctx.Err() != nil || (err != nil && !errors.As(err, &exit)) {
				t.Fatalf("runner did not execute: %v (%s)", err, output)
			}
			if cmd.ProcessState.ExitCode() != test.status {
				t.Fatalf("status=%d, want=%d (%s)", cmd.ProcessState.ExitCode(), test.status, output)
			}
			if _, err := os.Stat(prepareLog); !os.IsNotExist(err) {
				t.Fatalf("test runner invoked preparation: %v (%s)", err, output)
			}
			entries, err := os.ReadDir(f.home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("runner left temporary image records: %v (%v)", entries, err)
			}
			log, err := os.ReadFile(f.log)
			if err != nil {
				t.Fatal(err)
			}
			if test.mode == "missing" {
				if strings.Contains(string(log), "run ") || !strings.Contains(string(output), "just test-prepare") {
					t.Fatalf("missing image was run or had no preparation hint: log=%s, output=%s", log, output)
				}
			} else if !strings.Contains(string(log), "--pull=never") || !strings.Contains(string(log), "--cidfile") || !strings.Contains(string(log), ":/run/nono-agent-profiles/expected.inputs:ro") ||
				(strings.Contains(string(log), "--network=none") != (test.network == "0")) {
				t.Fatalf("incorrect cidfile/pull/network controls: %s", log)
			}
		})
	}
}

// The suite is PID 1 in its container, where a forwarded signal has no
// effect. The runner must remove the container itself and stop.
func TestContainerRunnerStopsOnInterrupt(t *testing.T) {
	for _, test := range []struct {
		signal syscall.Signal
		status int
	}{{syscall.SIGINT, 130}, {syscall.SIGTERM, 143}} {
		t.Run(test.signal.String(), func(t *testing.T) {
			f := newTaskFixture(t)
			fakeLinuxController(t, f)
			for _, name := range []string{"containers.sh", "require-linux.sh"} {
				data, err := os.ReadFile(filepath.Join(testRepository(t), "test", name))
				if err != nil {
					t.Fatal(err)
				}
				writeLauncherFixture(t, filepath.Join(f.root, "test", name), string(data))
			}
			writeLauncherFixture(t, filepath.Join(f.root, "test/image-inputs.sh"), "#!/bin/sh\nprintf 'fixture\\n'\n")
			for _, name := range []string{"sh", "dirname", "grep", "cut", "mktemp", "chmod", "rm", "sleep", "cat"} {
				path, err := exec.LookPath(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, filepath.Join(f.tools, name)); err != nil && !os.IsExist(err) {
					t.Fatal(err)
				}
			}
			// The stand-in container runs until its "container" is removed.
			writeLauncherFixture(t, filepath.Join(f.tools, "podman"), `#!/bin/sh
printf '%s\n' "$*" >> "$TASK_PODMAN_LOG"
record=
previous=
for arg do
  [ "$previous" != --cidfile ] || record=$arg
  previous=$arg
done
case "$1" in
image) exit 0 ;;
run) printf '%s\n' "$$" > "$record"; exec sleep 60 ;;
rm) kill "$(cat "$record")" ;;
*) exit 99 ;;
esac
`)
			cmd := exec.Command(filepath.Join(f.tools, "sh"), filepath.Join(f.root, "test/containers.sh"), "fixture")
			cmd.Dir = f.home
			cmd.Env = []string{"PATH=" + f.tools, "HOME=" + f.home, "TMPDIR=" + f.home, "TASK_PODMAN_LOG=" + f.log}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			go func() { finished <- cmd.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				log, _ := os.ReadFile(f.log)
				if strings.Contains(string(log), "run ") {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					t.Fatalf("runner did not start a container: %s", log)
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Let the stand-in record its identity before the interrupt.
			time.Sleep(200 * time.Millisecond)
			if err := cmd.Process.Signal(test.signal); err != nil {
				t.Fatal(err)
			}
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("runner still waits for its container after %s", test.signal)
			}
			if cmd.ProcessState.ExitCode() != test.status {
				t.Fatalf("status=%d, want=%d", cmd.ProcessState.ExitCode(), test.status)
			}
			log, err := os.ReadFile(f.log)
			if err != nil || !strings.Contains(string(log), "rm --force --time 0 --ignore --cidfile") {
				t.Fatalf("runner did not remove its container: %v (%s)", err, log)
			}
			if entries, err := os.ReadDir(f.home); err != nil || len(entries) != 0 {
				t.Fatalf("runner left temporary image records: %v (%v)", entries, err)
			}
		})
	}
}

func TestContainerScriptsRefuseMacControllerBeforeEffects(t *testing.T) {
	for _, script := range []string{"prepare.sh", "containers.sh"} {
		t.Run(script, func(t *testing.T) {
			f := newTaskFixture(t)
			fakeLinuxController(t, f)
			for _, name := range []string{script, "require-linux.sh"} {
				data, err := os.ReadFile(filepath.Join(testRepository(t), "test", name))
				if err != nil {
					t.Fatal(err)
				}
				writeLauncherFixture(t, filepath.Join(f.root, "test", name), string(data))
			}
			dirname, err := exec.LookPath("dirname")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dirname, filepath.Join(f.tools, "dirname")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "/bin/sh", filepath.Join(f.root, "test", script), "fixture")
			cmd.Dir = f.home
			cmd.Env = []string{"PATH=" + f.tools, "HOME=" + f.home, "TEST_CONTROLLER_OS=Darwin", "TASK_PODMAN_LOG=" + f.log}
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(output), "Linux x86_64 controller") {
				t.Fatalf("missing platform refusal: %v (%s)", err, output)
			}
			if _, err := os.Stat(f.log); !os.IsNotExist(err) {
				t.Fatalf("platform refusal contacted Podman: %v", err)
			}
			if _, err := os.Stat(filepath.Join(f.root, ".cache")); !os.IsNotExist(err) {
				t.Fatalf("platform refusal created build state: %v", err)
			}
		})
	}
}
