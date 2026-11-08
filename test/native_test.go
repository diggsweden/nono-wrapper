// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package main

import (
	"context"
	"encoding/json"
	"fmt"
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

	// System temporary locations have broad inherited reads that conflict with
	// protected state or mask missing grants. Use the ignored checkout cache,
	// outside the sandbox workspace, without granting the cache or scratch root.
	cache := filepath.Join(testRepository(t), ".cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	cache, err := filepath.EvalSymlinks(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkNativeSmokeCache(cache, runtime.GOOS); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(cache, "native-smoke-")
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
	// The official Codex profiles grant OS temporary locations, not arbitrary
	// directories chosen as TMPDIR. Keep temporary files there, whatever
	// TMPDIR this test process inherited.
	temp := osTempDir(t)
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
	t.Run("OpenCode model catalog", func(t *testing.T) {
		for _, spec := range []struct{ path, status string }{
			{"/api.json", "allowed"},
			{"/experimental/v2.0/api.json?type=chat,decision,image", "allowed"},
			{"/unrelated", "denied"},
		} {
			cmd := exec.CommandContext(t.Context(), "nono", "why", "--profile", "opencode-chatgpt", "--host", "https://models.opencode.ai"+spec.path, "--json")
			cmd.Dir, cmd.Env, cmd.WaitDelay = project, env, time.Second
			data, err := cmd.Output()
			if err != nil {
				t.Fatalf("catalog policy query: %v\n%s", err, data)
			}
			output := string(data)
			var result struct{ Status string }
			if err := json.Unmarshal([]byte(output), &result); err != nil || result.Status != spec.status {
				t.Fatalf("catalog %s: want %s, got %s (%v)", spec.path, spec.status, output, err)
			}
		}
	})
	// Resolve reference-host policy without contacting any destination.
	t.Run("developer reference hosts", func(t *testing.T) {
		for _, agent := range []string{"opencode", "codex"} {
			for _, spec := range []struct{ suffix, status string }{{"-chatgpt", "allowed"}, {"-offline", "denied"}} {
				for _, host := range requiredDeveloperReferenceHosts {
					cmd := exec.CommandContext(t.Context(), "nono", "why", "--profile", agent+spec.suffix, "--host", host, "--json")
					cmd.Dir, cmd.Env, cmd.WaitDelay = project, env, time.Second
					data, err := cmd.Output()
					if err != nil {
						t.Fatalf("reference-host policy query: %v\n%s", err, data)
					}
					var result struct{ Status string }
					if err := json.Unmarshal(data, &result); err != nil || result.Status != spec.status {
						t.Fatalf("%s%s host %s: want %s, got %s (%v)", agent, spec.suffix, host, spec.status, data, err)
					}
				}
			}
		}
	})
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
	t.Run("Just script recipes", func(t *testing.T) {
		testNativeJustRecipes(t, root, project, wrapper, env)
	})
	t.Run("rootless Podman socket", func(t *testing.T) {
		testNativePodman(t, root, project, wrapper, env)
	})
	t.Run("DevBase developer defaults", func(t *testing.T) {
		testNativeDevBaseDefaults(t, root, project, wrapper, env)
	})
	t.Run("Mise trust records stay read-only", func(t *testing.T) {
		testNativeMiseTrust(t, root, project, wrapper, env)
	})
	t.Run("Go caches stay scoped", func(t *testing.T) {
		testNativeGoCaches(t, root, project, wrapper, env)
	})
	t.Run("Cargo dependency build", func(t *testing.T) {
		testNativeCargo(t, root, project, wrapper, env)
	})
	t.Run("Tool caches stay scoped", func(t *testing.T) {
		testNativeToolCaches(t, root, project, wrapper, env)
	})
	t.Run("Public keys permit agent-backed Git signing", func(t *testing.T) {
		testNativePublicKeySigning(t, root, project, wrapper, env)
	})
	t.Run("Python and Gradle fixtures", func(t *testing.T) {
		testNativePythonAndGradle(t, root, project, wrapper, env)
	})
	t.Run("private multiprocessing sockets", func(t *testing.T) {
		testNativeTemporarySockets(t, root, project, wrapper, env)
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
	t.Chdir(project)
	if err := probeOpenCode(wrapper, filepath.Join(configHome, "nono/profiles/opencode-user.jsonc"), env); err != nil {
		t.Fatalf("OpenCode 2 local server/client and callback: %v", err)
	}
	t.Log("passed: sandboxed OpenCode 2 server/client, password checks and cancelled browser callback")
	run(project, []string{"NONO_PROFILE=codex-offline"}, wrapper, "codex", "features", "list")

	// Reuse the container suite's real command-execution probe with this fresh
	// home and project. No login or model is needed by the Codex app server.
	if err := os.WriteFile(filepath.Join(home, "private"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := probeCodex(wrapper, append(env, "NONO_PROFILE=codex-offline")); err != nil {
		t.Fatal(err)
	}
	t.Log("passed: real Codex command execution and confinement")
}

// osTempDir makes a session temporary directory in the system location the
// agent profiles grant: /tmp on Linux, where a caller's TMPDIR may point to
// any directory, and the per-user temporary directory on macOS.
func osTempDir(t *testing.T) string {
	t.Helper()
	base := "/tmp"
	if runtime.GOOS == "darwin" {
		base = os.TempDir()
	}
	temp, err := os.MkdirTemp(base, "nono-native-tmp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(temp) })
	temp, err = filepath.EvalSymlinks(temp)
	if err != nil {
		t.Fatal(err)
	}
	return temp
}

// cache must be canonical so a checkout or .cache symlink cannot hide a
// broadly granted temporary parent. Do this before installing test tools.
func checkNativeSmokeCache(cache, platform string) error {
	parents := []string{"/tmp"}
	if platform == "darwin" {
		parents = append(parents, "/private/tmp", "/private/var", "/var")
	}
	for _, parent := range parents {
		if cache == parent || strings.HasPrefix(cache, parent+string(filepath.Separator)) {
			return fmt.Errorf("native smoke cache %q is under broadly granted temporary path %q; move the checkout and its .cache outside system temporary locations before running test-native", cache, parent)
		}
	}
	return nil
}

func TestNativeSmokeCacheLocation(t *testing.T) {
	for _, test := range []struct {
		path, platform string
		reject         bool
	}{
		{"/home/developer/project/.cache", "linux", false},
		{"/tmp-adjacent/project/.cache", "linux", false},
		{"/tmp", "linux", true},
		{"/tmp/project/.cache", "linux", true},
		{"/Users/developer/project/.cache", "darwin", false},
		{"/private/tmp/project/.cache", "darwin", true},
		{"/private/var/folders/project/.cache", "darwin", true},
		{"/var/project/.cache", "darwin", true},
	} {
		t.Run(test.platform+test.path, func(t *testing.T) {
			err := checkNativeSmokeCache(test.path, test.platform)
			if (err != nil) != test.reject {
				t.Fatalf("cache location: %v, want rejection=%t", err, test.reject)
			}
		})
	}
	t.Run("symlink to temporary storage", func(t *testing.T) {
		root := t.TempDir()
		link := filepath.Join(root, "cache-link")
		if err := os.Symlink("/tmp", link); err != nil {
			t.Fatal(err)
		}
		cache, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkNativeSmokeCache(cache, runtime.GOOS); err == nil {
			t.Fatal("accepted cache symlink into temporary storage")
		}
	})
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
		{"opencode", filepath.Join(root, "xdg cache/mise"), `"$MISE_CACHE_DIR/runtime-probe"`},
		{"codex", filepath.Join(root, "xdg cache/mise"), `"$MISE_CACHE_DIR/runtime-probe"`},
		{"opencode", filepath.Join(root, "xdg data/man"), `"$XDG_DATA_HOME/man/runtime-probe"`},
		{"codex", filepath.Join(root, "xdg data/man"), `"$XDG_DATA_HOME/man/runtime-probe"`},
	} {
		body := "#!/bin/sh\nif printf probe > " + spec.target + "; then printf 'STATE_WRITABLE\\n'; else printf 'STATE_READ_ONLY\\n'; fi\n"
		if spec.agent == "opencode" {
			body = "#!/bin/sh\nif [ \"$1\" = --version ]; then printf 'opencode v2.0.0\\n'; exit 0; fi\n" + strings.TrimPrefix(body, "#!/bin/sh\n")
		}
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
	// OpenCode's upstream cache grant covers this child. Reject an impossible
	// read-only override rather than claiming that stripping its bind is enough.
	temp := filepath.Join(root, "xdg cache/opencode/tmp")
	invoke(nil, "config", "opencode", "add-dir", temp)
	cmd := exec.CommandContext(t.Context(), wrapper, "opencode")
	cmd.Dir, cmd.Env, cmd.WaitDelay = project, append(append([]string{}, probeEnv...), "NONO_PROFILE=opencode-offline"), time.Second
	output, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "cannot enforce read-only user access") {
		t.Fatalf("overlapping cache permission did not fail closed: %v\n%s", err, output)
	}
	invoke(nil, "config", "opencode", "remove-dir", temp)
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
