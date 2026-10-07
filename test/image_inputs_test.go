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
	"testing"
	"time"
)

var imageInputFiles = []string{
	".mise.toml", "mise.lock", "test/Containerfile", "test/install-deps.sh", "test/install-packs.sh",
	"test/prepare.sh", "test/image-inputs.sh", "test/require-linux.sh",
}

func newImageInputsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, file := range imageInputFiles {
		content, err := os.ReadFile(filepath.Join(testRepository(t), file))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeLauncherFixture(t, path, string(content))
	}
	writeLauncherFixture(t, filepath.Join(root, "test/images"), "fixture=registry.example/base@sha256:111\nother=registry.example/other@sha256:222\n")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"dirname", checksumTool(t), "cat"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runImageInputs(t *testing.T, root string, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{filepath.Join(root, "test/image-inputs.sh")}, args...)...)
	cmd.Dir = root
	cmd.Env = []string{"HOME=" + root, "PATH=" + filepath.Join(root, "bin"), "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if ctx.Err() != nil || (err != nil && !errors.As(err, &exit)) {
		t.Fatalf("image-input check did not execute: %v (%s)", err, output)
	}
	return string(output), cmd.ProcessState.ExitCode()
}

func recordImageInputs(t *testing.T, root string) string {
	t.Helper()
	output, status := runImageInputs(t, root, "fixture")
	if status != 0 || !strings.HasPrefix(output, "registry.example/base@sha256:111\nfixture\n") {
		t.Fatalf("manifest generation failed: status=%d (%s)", status, output)
	}
	path := filepath.Join(root, "image.inputs")
	writeLauncherFixture(t, path, output)
	if output, status := runImageInputs(t, root, "--check", path); status != 0 {
		t.Fatalf("fresh image rejected: %s", output)
	}
	return path
}

func TestImageInputsDetectBuildChanges(t *testing.T) {
	for _, file := range append([]string{"test/images"}, imageInputFiles...) {
		t.Run(file, func(t *testing.T) {
			root := newImageInputsFixture(t)
			manifest := recordImageInputs(t, root)
			path := filepath.Join(root, file)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			changed := string(original) + "\n# changed build input\n"
			if file == "test/images" {
				changed = strings.Replace(string(original), "base@sha256:111", "base@sha256:999", 1)
			}
			writeLauncherFixture(t, path, changed)
			output, status := runImageInputs(t, root, "--check", manifest)
			if status == 0 || !strings.Contains(output, "just test-prepare") {
				t.Fatalf("changed %s accepted or missing rebuild hint: status=%d (%s)", file, status, output)
			}
		})
	}
}

func TestImageInputsIgnoreOtherDistributionPins(t *testing.T) {
	root := newImageInputsFixture(t)
	manifest := recordImageInputs(t, root)
	writeLauncherFixture(t, filepath.Join(root, "test/images"), "fixture=registry.example/base@sha256:111\nother=registry.example/other@sha256:999\n")
	if output, status := runImageInputs(t, root, "--check", manifest); status != 0 {
		t.Fatalf("unrelated distribution invalidated the image: %s", output)
	}
}

func TestImageInputsShasumFallback(t *testing.T) {
	shasum, err := exec.LookPath("shasum")
	if err != nil {
		t.Skip("shasum is not installed")
	}
	root := newImageInputsFixture(t)
	want, status := runImageInputs(t, root, "fixture")
	if status != 0 {
		t.Fatalf("initial fingerprint failed: %s", want)
	}
	if err := os.Remove(filepath.Join(root, "bin/sha256sum")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bin/shasum")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		if err := os.Symlink(shasum, path); err != nil {
			t.Fatal(err)
		}
	}
	got, status := runImageInputs(t, root, "fixture")
	if status != 0 || got != want {
		t.Fatalf("shasum changed the fingerprint: status=%d\n got %q\nwant %q", status, got, want)
	}
}

func TestImageInputsRejectMissingOrInvalidRecord(t *testing.T) {
	for _, data := range []string{"missing", "", "truncated\n", "base\nunknown\n"} {
		t.Run(data, func(t *testing.T) {
			root := newImageInputsFixture(t)
			path := filepath.Join(root, "image.inputs")
			if data != "missing" {
				writeLauncherFixture(t, path, data)
			}
			if output, status := runImageInputs(t, root, "--check", path); status == 0 {
				t.Fatalf("invalid image record was accepted: %q (%s)", data, output)
			}
		})
	}
}

func TestImageInputsCheckCallerRecord(t *testing.T) {
	root := newImageInputsFixture(t)
	manifest := recordImageInputs(t, root)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(root, "expected.inputs")
	writeLauncherFixture(t, expected, string(data))
	// The container may not be able to read source configuration files. The
	// caller's read-only record supplies the expected fingerprint instead.
	if err := os.Remove(filepath.Join(root, "mise.lock")); err != nil {
		t.Fatal(err)
	}
	if output, status := runImageInputs(t, root, "--check", manifest, expected); status != 0 {
		t.Fatalf("caller record required container access to source configuration: %s", output)
	}
	writeLauncherFixture(t, expected, strings.Replace(string(data), "base@sha256:111", "base@sha256:999", 1))
	if output, status := runImageInputs(t, root, "--check", manifest, expected); status == 0 || !strings.Contains(output, "just test-prepare") {
		t.Fatalf("changed caller fingerprint was accepted: %s", output)
	}
	if err := os.Remove(expected); err != nil {
		t.Fatal(err)
	}
	if output, status := runImageInputs(t, root, "--check", manifest, expected); status == 0 {
		t.Fatalf("missing caller record was accepted: %s", output)
	}
}
