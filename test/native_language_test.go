// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testNativePythonAndGradle(t *testing.T, root, project, wrapper string, env []string) {
	if runtime.GOOS != "linux" {
		t.Skip("language-state validation currently covers Linux")
	}
	bin := filepath.Join(root, "language probes")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+bin+string(os.PathListSeparator)+path)
			break
		}
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	body := "#!/bin/sh\nif [ \"${1:-}\" = --version ]; then printf 'opencode v2.0.0\\n'; exit 0; fi\nset -eu\n"
	count := 0
	var toolReads []string
	if python, err := exec.LookPath("python3"); err == nil {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		if _, err := exec.CommandContext(ctx, python, "-I", "-m", "pip", "--version").Output(); err == nil {
			prefix, err := exec.CommandContext(ctx, python, "-I", "-c", "import sys; print(sys.prefix)").Output()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(strings.TrimSpace(string(prefix)), filepath.Join(root, "home/.pyenv")); err != nil {
				t.Fatal(err)
			}
			writeNativeWheel(t, filepath.Join(project, "nono_fixture-0.0.0-py3-none-any.whl"))
			body += "venv=python-fixture-${0##*/}\n" + quote(python) + " -I -m venv --without-pip \"$venv\"\n" +
				quote(python) + " -I -m pip --isolated --python \"$venv/bin/python\" install --no-index --no-deps --no-build-isolation --no-cache-dir --disable-pip-version-check nono_fixture-0.0.0-py3-none-any.whl\n" +
				"\"$venv/bin/python\" -I -c 'import nono_fixture; print(\"PIP_WHEEL_OK\")'\n"
			body += "\"$venv/bin/python\" -I -c 'import multiprocessing as m; s=m.get_context(\"spawn\").Semaphore(1); assert s.acquire(timeout=1); s.release(); print(\"MULTIPROCESSING_SEMAPHORE_OK\")'\n"
			count++
		}
	}
	if gradle, err := exec.LookPath("gradle"); err == nil {
		java, javaErr := exec.LookPath("java")
		if javaErr == nil {
			java, javaErr = filepath.EvalSymlinks(java)
		}
		gradle, err = filepath.EvalSymlinks(gradle)
		if err == nil && javaErr == nil {
			if err := os.Symlink(filepath.Dir(filepath.Dir(gradle)), filepath.Join(root, "home/.sdkman")); err != nil {
				t.Fatal(err)
			}
			javaHome := filepath.Dir(filepath.Dir(java))
			probeEnv = append(probeEnv, "JAVA_HOME="+javaHome)
			toolReads = append(toolReads, "--read", javaHome)
			gradleProject := filepath.Join(project, "gradle-fixture")
			if err := os.Mkdir(gradleProject, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string]string{
				"settings.gradle": "rootProject.name = 'nono-fixture'\n",
				"build.gradle":    "tasks.register('verifySandbox') { doLast { println 'GRADLE_BUILD_OK' } }\n",
			} {
				if err := os.WriteFile(filepath.Join(gradleProject, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if os.Getenv("NONO_TEST_GRADLE_BUILD") == "1" {
				body += quote(gradle) + " --offline --no-daemon --max-workers=1 -p " + quote(gradleProject) + " verifySandbox\n"
			} else {
				t.Log("Gradle build probe disabled: known wildcard-IP/socket limitation; set NONO_TEST_GRADLE_BUILD=1 to reproduce. Only --version is checked here.")
				body += quote(gradle) + " --offline --no-daemon --version\n"
			}
			count++
		}
	}
	if count == 0 {
		t.Skip("requires a Python with pip or an installed Gradle and system Java")
	}
	body += "printf 'LANGUAGE_FIXTURES_OK\\n'\n"
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(body), 0o700); err != nil {
				t.Fatal(err)
			}
			agentEnv := append([]string{}, probeEnv...)
			if agent == "codex" {
				agentEnv = append(agentEnv, "GRADLE_USER_HOME="+filepath.Join(root, "custom-gradle"))
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			args := append([]string{"run", "--block-net"}, toolReads...)
			cmd := exec.CommandContext(ctx, wrapper, append(args, agent)...)
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, agentEnv, time.Second
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "LANGUAGE_FIXTURES_OK") {
				t.Fatalf("Python/Gradle through %s: %v\n%s", agent, err, output)
			}
			t.Logf("%s", output)
		})
	}
}

func writeNativeWheel(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	wheel := zip.NewWriter(file)
	for name, contents := range map[string]string{
		"nono_fixture.py":                       "VALUE = 1\n",
		"nono_fixture-0.0.0.dist-info/METADATA": "Metadata-Version: 2.1\nName: nono-fixture\nVersion: 0.0.0\n",
		"nono_fixture-0.0.0.dist-info/WHEEL":    "Wheel-Version: 1.0\nGenerator: nono-wrapper-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
		"nono_fixture-0.0.0.dist-info/RECORD":   "nono_fixture.py,,\nnono_fixture-0.0.0.dist-info/METADATA,,\nnono_fixture-0.0.0.dist-info/WHEEL,,\nnono_fixture-0.0.0.dist-info/RECORD,,\n",
	} {
		entry, err := wheel.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := wheel.Close(); err != nil {
		t.Fatal(err)
	}
}
