// SPDX-FileCopyrightText: The nono-wrapper Authors
// SPDX-License-Identifier: CC0-1.0

package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Real-agent smoke coverage is explicit: installation downloads signed packs.
// The ordinary unit suite continues to use fake agents and work offline.
func TestNativeIntegration(t *testing.T) {
	if os.Getenv("NONO_NATIVE_TEST") != "1" {
		t.Skip("use just test-native for real-agent integration")
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Fatal("native integration requires macOS or Linux")
	}
	for _, tool := range []string{"nono", "opencode", "codex", "just", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required; install agents through DevBase and sandbox/build tools with just install-dev-tools: %v", tool, err)
		}
	}

	// macOS grants system access under /private/var, which cannot also contain
	// protected nono state. Keep the isolated home on the user's filesystem.
	hostHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(hostHome, ".nono-wrapper-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove native test home %s: %v", root, err)
		}
	})
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "projects", "project with spaces")
	// State lives outside HOME, the workspace and /tmp. Otherwise inherited
	// default or temporary-directory grants would hide missing runtime grants.
	configHome, dataHome := filepath.Join(root, "xdg config"), filepath.Join(root, "xdg data")
	stateHome, cacheHome := filepath.Join(root, "xdg state"), filepath.Join(root, "xdg cache")
	codexHome := filepath.Join(root, "custom codex")
	// The official macOS Codex profile grants OS temporary locations, not
	// arbitrary directories chosen as TMPDIR. Keep temporary files there.
	temp := canonicalTempDir(t)
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(home, ".local", "bin")
	env := append([]string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		"SHELL=/bin/sh",
		"TMPDIR=" + temp,
		"XDG_CONFIG_HOME=" + configHome,
		"XDG_DATA_HOME=" + dataHome,
		"XDG_STATE_HOME=" + stateHome,
		"XDG_CACHE_HOME=" + cacheHome,
		"CODEX_HOME=" + codexHome,
		"NONO_WRAPPER_SSH_AGENT=0",
		"NONO_WRAPPER_ROOT=" + filepath.Dir(project),
		"NONO_NO_UPDATE_CHECK=1",
		"NONO_NO_PACK_UPDATE_HINTS=1",
		"NO_COLOR=1",
	}, buildEnv...)
	// Optional cross-repository validation uses a generated DevBase config.
	// Share only installed artifacts; shims, wrappers, trust and state stay isolated.
	mise := ""
	if config := os.Getenv("NONO_TEST_MISE_CONFIG"); config != "" {
		mise, err = exec.LookPath("mise")
		if err != nil {
			t.Fatal(err)
		}
		mise, err = filepath.EvalSymlinks(mise)
		if err != nil {
			t.Fatal(err)
		}
		data := os.Getenv("NONO_TEST_MISE_DATA_DIR")
		if !filepath.IsAbs(config) || !filepath.IsAbs(data) {
			t.Fatal("NONO_TEST_MISE_CONFIG and NONO_TEST_MISE_DATA_DIR must be absolute")
		}
		configDir, dataDir := filepath.Join(configHome, "mise"), filepath.Join(dataHome, "mise")
		for _, path := range []string{configDir, dataDir} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		contents, err := os.ReadFile(config)
		if err != nil {
			t.Fatal(err)
		}
		localConfig := filepath.Join(configDir, "config.toml")
		if err := os.WriteFile(localConfig, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(data, "installs"), filepath.Join(dataDir, "installs")); err != nil {
			t.Fatal(err)
		}
		env = append(env, "MISE_GLOBAL_CONFIG_FILE="+localConfig, "MISE_CONFIG_DIR="+configDir,
			"MISE_DATA_DIR="+dataDir, "MISE_CACHE_DIR="+filepath.Join(cacheHome, "mise"),
			"MISE_STATE_DIR="+filepath.Join(stateHome, "mise"), "MISE_CEILING_PATHS="+root,
			"MISE_TRUSTED_CONFIG_PATHS="+configDir, "MISE_AUTO_INSTALL=false")
	}
	run := func(directory string, extra []string, command string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir = directory
		cmd.Env = append(append([]string{}, env...), extra...)
		cmd.WaitDelay = time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", command, args, err, output)
		}
		t.Logf("passed: %s %s", filepath.Base(command), strings.Join(args, " "))
		return string(output)
	}

	// A real Git root bounds OpenCode's project configuration discovery.
	run(project, nil, "git", "init", "-q")
	run(testRepository(t), nil, "just", "install-local")
	wrapper := filepath.Join(bin, "nono-wrapper")
	if output := run(project, nil, wrapper, "man"); !strings.Contains(output, "## SYNOPSIS") {
		t.Fatal("installed manual is missing")
	}
	if mise != "" {
		run(home, nil, mise, "reshim")
		var active map[string]string
		if output := run(project, nil, mise, "env", "--json"); json.Unmarshal([]byte(output), &active) != nil || active["PATH"] == "" {
			t.Fatalf("cannot activate the generated DevBase config: %s", output)
		}
		env = append(env, "PATH="+active["PATH"])
		for _, agent := range []string{"opencode", "codex"} {
			commands := [][]string{{"/bin/sh", "-c", "exec " + agent + " --version"}, {mise, "exec", "--", agent, "--version"}}
			var extra []string
			if script := os.Getenv("NONO_TEST_BASH_FUNCTIONS"); script != "" {
				if !filepath.IsAbs(script) {
					t.Fatal("NONO_TEST_BASH_FUNCTIONS must be absolute")
				}
				extra = append(extra, "NONO_TEST_BASH_FUNCTIONS="+script)
				commands = append(commands, []string{"/bin/bash", "--noprofile", "--norc", "-c", `source "$NONO_TEST_BASH_FUNCTIONS"; ` + agent + " --version"})
			}
			if functions := os.Getenv("NONO_TEST_FISH_FUNCTIONS"); functions != "" {
				if !filepath.IsAbs(functions) {
					t.Fatal("NONO_TEST_FISH_FUNCTIONS must be absolute")
				}
				fish, err := exec.LookPath("fish")
				if err != nil {
					t.Fatal(err)
				}
				extra = append(extra, "NONO_TEST_FISH_FUNCTIONS="+functions)
				commands = append(commands, []string{fish, "--no-config", "-c", `set -p fish_function_path "$NONO_TEST_FISH_FUNCTIONS"; ` + agent + " --version"})
			}
			for _, command := range commands {
				commandEnv := append([]string{}, extra...)
				if command[0] == "/bin/bash" {
					// Exercise standalone Bash with raw tool bins, not activation.
					commandEnv = append(commandEnv, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				}
				output := run(project, commandEnv, command[0], command[1:]...)
				if strings.Count(output, "nono-wrapper: "+agent+" session ended") != 1 || strings.Count(output, "Applying sandbox") != 1 {
					t.Fatalf("DevBase must launch exactly one %s sandbox: %s", agent, output)
				}
			}
		}
	}
	run(project, nil, wrapper, "doctor")
	// Validate saved rules with real nono, without contacting any destination.
	// Keep a baseline domain so removing a test rule never means open networking.
	for _, agent := range []string{"opencode", "codex"} {
		run(project, nil, wrapper, "config", agent, "allow-domain", "retained.example")
		checkHost := func(host, status string) {
			t.Helper()
			output := run(project, nil, "nono", "why", "--profile", agent+"-user", "--host", host, "--json")
			var result struct{ Status string }
			if err := json.Unmarshal([]byte(output), &result); err != nil || result.Status != status {
				t.Fatalf("%s host %q: want %s, got %s (%v)", agent, host, status, output, err)
			}
		}
		run(project, nil, wrapper, "config", agent, "allow-domain", "example.com")
		checkHost("example.com", "allowed")
		checkHost("api.example.com", "denied")
		run(project, nil, wrapper, "config", agent, "remove-domain", "example.com")
		run(project, nil, wrapper, "config", agent, "allow-domain", "*.example.com")
		checkHost("example.com", "denied")
		checkHost("api.example.com", "allowed")
		checkHost("deep.api.example.com", "allowed")
		checkHost("notexample.com", "denied")
		run(project, nil, wrapper, "config", agent, "remove-domain", "*.example.com")
		checkHost("api.example.com", "denied")
		run(project, nil, wrapper, "config", agent, "allow-domain", "--method", "GET", "https://api.example.com/api/**")
		checkHost("https://api.example.com/api/items", "allowed")
		checkHost("https://api.example.com/other", "denied")
		run(project, nil, wrapper, "config", agent, "remove-domain", "--method", "GET", "https://api.example.com/api/**")
		checkHost("api.example.com", "denied")
		run(project, nil, wrapper, "config", agent, "allow-tcp-port", "3000")
		run(project, nil, wrapper, "config", agent, "remove-tcp-port", "3000")
		run(project, nil, wrapper, "config", agent, "remove-domain", "retained.example")
	}
	t.Run("TCP grants are not loopback-only", func(t *testing.T) {
		testNativeTCPGrant(t, project, wrapper, env)
	})
	if !t.Run("runtime state respects explicit read-only overrides", func(t *testing.T) {
		testNativeRuntimeOverrides(t, root, project, wrapper, filepath.Join(dataHome, "opencode"), codexHome, env)
	}) {
		t.FailNow()
	}
	if !t.Run("managed executables stay read-only", func(t *testing.T) {
		testNativeExecutableProtection(t, root, project, wrapper, env)
	}) {
		t.FailNow()
	}
	run(project, []string{"NONO_PROFILE=opencode-offline"}, wrapper, "opencode", "session", "list")
	run(project, []string{"NONO_PROFILE=codex-offline"}, wrapper, "codex", "features", "list")

	// Reuse the container suite's real command-execution probe with this fresh
	// home and project. No login or model is needed by the Codex app server.
	if err := os.WriteFile(filepath.Join(home, "private"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	if err := probeCodex(wrapper, append(env, "NONO_PROFILE=codex-offline")); err != nil {
		t.Fatal(err)
	}
	t.Log("passed: real Codex command execution and confinement")
}

func testNativeRuntimeOverrides(t *testing.T, root, project, wrapper, opencodeData, codexHome string, env []string) {
	t.Helper()
	probeBin := filepath.Join(root, "runtime probes")
	if err := os.Mkdir(probeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	probeEnv := append([]string{}, env...)
	for i := len(env) - 1; i >= 0; i-- {
		if path, ok := strings.CutPrefix(env[i], "PATH="); ok {
			probeEnv = append(probeEnv, "PATH="+probeBin+string(os.PathListSeparator)+path)
			break
		}
	}
	invoke := func(extra []string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, wrapper, args...)
		cmd.Dir, cmd.Env, cmd.WaitDelay = project, append(append([]string{}, probeEnv...), extra...), time.Second
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("runtime probe %v: %v\n%s", args, err, output)
		}
		return string(output)
	}
	for _, spec := range []struct{ agent, path, target string }{
		{"opencode", opencodeData, `"$XDG_DATA_HOME/opencode/runtime-probe"`},
		{"codex", codexHome, `"$CODEX_HOME/runtime-probe"`},
	} {
		body := "#!/bin/sh\nif printf probe > " + spec.target + "; then printf 'STATE_WRITABLE\\n'; else printf 'STATE_READ_ONLY\\n'; fi\n"
		if err := os.WriteFile(filepath.Join(probeBin, spec.agent), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, readOnly := range []bool{false, true, false} {
			verb, marker := "remove-dir", "STATE_WRITABLE"
			if readOnly {
				verb, marker = "add-dir", "STATE_READ_ONLY"
			}
			invoke(nil, "config", spec.agent, verb, spec.path)
			output := invoke([]string{"NONO_PROFILE=" + spec.agent + "-offline"}, spec.agent)
			if !strings.Contains(output, marker) {
				t.Fatalf("runtime defaults ignored user access for %s (read-only=%v): %s", spec.agent, readOnly, output)
			}
		}
	}
}

func testNativeTCPGrant(t *testing.T, project, wrapper string, env []string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("checks Linux's port-only TCP exception")
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	var host string
	for _, address := range addresses {
		if ip, ok := address.(*net.IPNet); ok && ip.IP.To4() != nil && ip.IP.IsGlobalUnicast() && !ip.IP.IsLoopback() {
			host = ip.IP.String()
			break
		}
	}
	if host == "" {
		t.Skip("requires a local non-loopback IPv4 address; no external host is contacted")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("NON_LOOPBACK_CONNECTED"))
	})}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatal("native TCP checks require curl")
	}
	curl, err = filepath.EvalSymlinks(curl)
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(command string, args ...string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Dir, cmd.Env, cmd.WaitDelay = project, env, time.Second
		return cmd.CombinedOutput()
	}
	for _, allowed := range []bool{false, true, false} {
		verb := "remove-tcp-port"
		if allowed {
			verb = "allow-tcp-port"
		}
		if output, err := invoke(wrapper, "config", "codex", verb, port); err != nil {
			t.Fatalf("%s: %v\n%s", verb, err, output)
		}
		output, err := invoke("nono", "run", "--profile", "codex-offline", "--extends", "codex-user", "--allow-cwd", "--read-file", curl,
			"--", curl, "--disable", "--noproxy", "*", "--silent", "--show-error", "--fail", "--connect-timeout", "2", "--max-time", "4", "http://"+listener.Addr().String()+"/")
		if allowed {
			if err != nil || !strings.Contains(string(output), "NON_LOOPBACK_CONNECTED") {
				t.Fatalf("TCP port grant must expose its actual non-loopback scope: %v\n%s", err, output)
			}
		} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 7 || strings.Contains(string(output), "NON_LOOPBACK_CONNECTED") {
			t.Fatalf("without the grant, the offline profile must refuse the connection: %v\n%s", err, output)
		}
	}
}
