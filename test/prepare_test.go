// SPDX-FileCopyrightText: The nono-agent-profiles Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func toolArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	if err := archive.WriteHeader(&tar.Header{Name: "nono", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func newPrepareFixture(t *testing.T, archive []byte) taskFixture {
	t.Helper()
	f := newTaskFixture(t)
	fakeLinuxController(t, f)
	for _, file := range []string{"prepare.sh", "probe.go", "image-inputs.sh", "require-linux.sh", "Containerfile", "install-deps.sh", "install-packs.sh"} {
		content, err := os.ReadFile(filepath.Join(testRepository(t), "test", file))
		if err != nil {
			t.Fatal(err)
		}
		writeLauncherFixture(t, filepath.Join(f.root, "test", file), string(content))
	}
	for _, file := range []string{".mise.toml", "mise.lock"} {
		content, err := os.ReadFile(filepath.Join(testRepository(t), file))
		if err != nil {
			t.Fatal(err)
		}
		writeLauncherFixture(t, filepath.Join(f.root, file), string(content))
	}
	for _, name := range []string{"sh", "dirname", "mktemp", checksumTool(t), "grep", "cat", "cp", "tar", "gzip", "find"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(f.tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	// Only cache preparation is under test; compilation and image builds use fakes.
	if err := os.Remove(filepath.Join(f.tools, "go")); err != nil {
		t.Fatal(err)
	}
	writeLauncherFixture(t, filepath.Join(f.tools, "go"), `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; printf 'compiled fixture\n' > "$1"; exit 0; fi
  shift
done
exit 99
`)
	writeLauncherFixture(t, filepath.Join(f.tools, "curl"), `#!/bin/sh
printf 'download\n' >> "$PREPARE_CURL_LOG"
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = -o ]; then shift; output=$1; fi
  shift
done
[ -n "$output" ] || exit 99
cp "$PREPARE_ARCHIVE" "$output"
`)
	writeLauncherFixture(t, filepath.Join(f.tools, "podman"), `#!/bin/sh
[ "$1" = build ] || exit 99
tools= base=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --build-context ]; then shift; tools=${1#tools=}; fi
  if [ "$1" = --build-arg ]; then shift; base=${1#BASE_IMAGE=}; fi
  shift
done
[ -n "$tools" ] || exit 99
IFS= read -r recorded_base < "$tools/image.inputs"
[ "$base" = "$recorded_base" ] || exit 99
printf '%s\n' "$tools" > "$TASK_PODMAN_LOG"
cp "$tools/nono" "$PREPARE_CAPTURE"
cp "$tools/image.inputs" "$PREPARE_INPUTS"
`)
	writeLauncherFixture(t, filepath.Join(f.root, "test/tools"), fmt.Sprintf("nono v0.79.0 %x\n", sha256.Sum256(archive)))
	writeLauncherFixture(t, filepath.Join(f.root, "test/images"), "fixture=invalid.example/image:fixture\n")
	writeLauncherFixture(t, filepath.Join(f.home, "download.tar.gz"), string(archive))
	if err := os.MkdirAll(filepath.Join(f.root, ".cache/tools"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.home, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

func prepareEnv(f taskFixture) []string {
	return []string{
		"PATH=" + f.tools, "HOME=" + f.home, "TMPDIR=" + filepath.Join(f.home, "tmp"),
		"TASK_PODMAN_LOG=" + f.log, "PREPARE_CURL_LOG=" + filepath.Join(f.home, "curl.log"),
		"PREPARE_ARCHIVE=" + filepath.Join(f.home, "download.tar.gz"), "PREPARE_CAPTURE=" + filepath.Join(f.home, "built-tool"),
		"PREPARE_INPUTS=" + filepath.Join(f.home, "image.inputs"),
	}
}

func TestPrepareRecordsImageInputs(t *testing.T) {
	f := newPrepareFixture(t, toolArchive(t, "trusted executable\n"))
	if output, status := runPrepare(t, f); status != 0 {
		t.Fatalf("preparation failed: %s", output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", filepath.Join(f.root, "test/image-inputs.sh"), "fixture")
	cmd.Dir = f.home
	cmd.Env = prepareEnv(f)
	want, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("could not read expected build inputs: %v (%s)", err, want)
	}
	got, err := os.ReadFile(filepath.Join(f.home, "image.inputs"))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("wrong image inputs handed to build: got %q, want %q (%v)", got, want, err)
	}
}

func runPrepare(t *testing.T, f taskFixture) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(f.tools, "sh"), filepath.Join(f.root, "test/prepare.sh"), "fixture")
	cmd.Dir = f.home
	cmd.Env = prepareEnv(f)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || (err != nil && !errors.As(err, &exit)) {
		t.Fatalf("preparation failed to execute: %v (%s)", err, output)
	}
	entries, err := os.ReadDir(filepath.Join(f.home, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("preparation left temporary files: %v (%v)", entries, err)
	}
	return string(output), cmd.ProcessState.ExitCode()
}

func TestPrepareRecipeDefaultTargets(t *testing.T) {
	f := newPrepareFixture(t, toolArchive(t, "trusted executable\n"))
	writeLauncherFixture(t, filepath.Join(f.root, "test/images"),
		"ubuntu-26.04=invalid.example/ubuntu:fixture\nopensuse-leap-16=invalid.example/opensuse:fixture\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.just, "--justfile", filepath.Join(f.root, "justfile"), "test-prepare")
	cmd.Dir = f.home
	cmd.Env = prepareEnv(f)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "== ubuntu-26.04 (") || !strings.Contains(string(output), "== opensuse-leap-16 (") {
		t.Fatalf("default recipe did not prepare both routine targets: %v (%s)", err, output)
	}
}

func TestPrepareRevalidatesCachedTools(t *testing.T) {
	const trusted = "trusted archive executable\n"
	archive := toolArchive(t, trusted)
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("valid cached archive=%t", valid), func(t *testing.T) {
			f := newPrepareFixture(t, archive)
			cache := filepath.Join(f.root, ".cache/tools")
			cached := archive
			if !valid {
				cached = toolArchive(t, "altered archive contents\n")
			}
			writeLauncherFixture(t, filepath.Join(cache, "nono.tar.gz"), string(cached))
			// An executable and matching marker must not override the verified archive.
			writeLauncherFixture(t, filepath.Join(cache, "nono"), "altered cached executable\n")
			writeLauncherFixture(t, filepath.Join(cache, "nono.sha256"), fmt.Sprintf("%x\n", sha256.Sum256(archive)))
			output, status := runPrepare(t, f)
			if status != 0 {
				t.Fatalf("preparation failed: %s", output)
			}
			got, err := os.ReadFile(filepath.Join(f.home, "built-tool"))
			if err != nil || string(got) != trusted {
				t.Fatalf("unverified executable reached the image build: %q (%v)", got, err)
			}
			_, err = os.Stat(filepath.Join(f.home, "curl.log"))
			if (valid && !os.IsNotExist(err)) || (!valid && err != nil) {
				t.Fatalf("incorrect cache/download selection: %v", err)
			}
			context, err := os.ReadFile(f.log)
			if err != nil {
				t.Fatal(err)
			}
			path := strings.TrimSpace(string(context))
			if !strings.HasPrefix(path, filepath.Join(f.home, "tmp")+string(os.PathSeparator)) {
				t.Fatalf("build consumed a shared cache instead of private inputs: %s", path)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("private build context remains: %v", err)
			}
		})
	}
}

func TestPrepareRejectsInvalidDownload(t *testing.T) {
	f := newPrepareFixture(t, toolArchive(t, "trusted executable\n"))
	writeLauncherFixture(t, filepath.Join(f.home, "download.tar.gz"), string(toolArchive(t, "wrong executable\n")))
	output, status := runPrepare(t, f)
	if status == 0 || !strings.Contains(output, "checksum mismatch") {
		t.Fatalf("invalid download was accepted: status=%d (%s)", status, output)
	}
	if _, err := os.Stat(f.log); !os.IsNotExist(err) {
		t.Fatalf("image build ran after checksum failure: %v", err)
	}
}

func TestPrepareRejectsUnknownTarget(t *testing.T) {
	f := newPrepareFixture(t, toolArchive(t, "trusted executable\n"))
	writeLauncherFixture(t, filepath.Join(f.root, "test/images"), "known=invalid.example/image:fixture\n")
	// runPrepare requests "fixture", which is absent from this manifest.
	output, status := runPrepare(t, f)
	if status == 0 || !strings.Contains(output, "unknown distribution") {
		t.Fatalf("unknown target accepted: status=%d (%s)", status, output)
	}
	for _, log := range []string{f.log, filepath.Join(f.home, "curl.log")} {
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Fatalf("preparation ran before validating the target: %s (%v)", log, err)
		}
	}
}

func TestPrepareRefusesUnsafeCache(t *testing.T) {
	for _, kind := range []string{"cache directory symlink", "archive symlink", "archive directory"} {
		t.Run(kind, func(t *testing.T) {
			f := newPrepareFixture(t, toolArchive(t, "trusted executable\n"))
			outside := filepath.Join(f.home, "outside")
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(outside, "keep")
			writeLauncherFixture(t, canary, "keep\n")
			cache := filepath.Join(f.root, ".cache/tools")
			switch kind {
			case "cache directory symlink":
				if err := os.Remove(cache); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, cache); err != nil {
					t.Fatal(err)
				}
			case "archive symlink":
				if err := os.Symlink(canary, filepath.Join(cache, "nono.tar.gz")); err != nil {
					t.Fatal(err)
				}
			case "archive directory":
				if err := os.Mkdir(filepath.Join(cache, "nono.tar.gz"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			output, status := runPrepare(t, f)
			if status == 0 || !strings.Contains(output, "prepare:") {
				t.Fatalf("unsafe cache accepted: status=%d (%s)", status, output)
			}
			if data, err := os.ReadFile(canary); err != nil || string(data) != "keep\n" {
				t.Fatalf("cache refusal modified another file: %v", err)
			}
			for _, log := range []string{f.log, filepath.Join(f.home, "curl.log")} {
				if _, err := os.Stat(log); !os.IsNotExist(err) {
					t.Fatalf("external command ran after cache refusal: %s (%v)", log, err)
				}
			}
		})
	}
}
