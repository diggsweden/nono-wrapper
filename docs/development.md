<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Development

## Project layout

```text
cmd/nono-wrapper/   Go CLI: launch, config, verify, audit and man
docs/              user and developer documentation
docs/manpages/     the manual source, embedded and rendered for man(1)
profiles/          editable nono profiles
test/              CLI/installation tests, container probes and version pins
```

| File | Purpose |
| --- | --- |
| `.mise.toml`, `mise.lock` | pinned host tools and platform-specific download checksums |
| `justfile` | setup, local installation and test recipes |
| `go.mod`, `go.sum` | pinned Go dependency versions and checksums |
| `test/packs` | official pack pins shared by user installation and test images |
| `profiles/agent-policy.jsonc` | shared environment/socket overrides and certificate compatibility |
| `profiles/opencode-base.jsonc` | composes the official OpenCode pack with the shared policy |
| `profiles/codex-base.jsonc` | composes the official Codex pack with the shared policy |
| `profiles/*-chatgpt.jsonc`, `profiles/*-offline.jsonc` | make the network choice |
| `cmd/nono-wrapper/main.go` | urfave/cli v3 command definitions |
| `cmd/nono-wrapper/launch.go` | profile resolution and nono launch arguments |
| `cmd/nono-wrapper/config.go` | persistent user allowance commands and atomic JSONC editing |
| `cmd/nono-wrapper/policy.go` | user precedence over matching bundled profile rules |
| `cmd/nono-wrapper/filesystem_policy.go` | canonical path matching and effective access-mode checks |
| `cmd/nono-wrapper/session.go` | supervised launch, signal forwarding and post-exit audit summary |
| `cmd/nono-wrapper/verify.go` | installed-profile smoke checks |
| `cmd/nono-wrapper/audit.go` | optional, manually invoked project-file reminder |
| `cmd/nono-wrapper/conformance_test.go` | exit-status, stream, no-escape and help-truth contracts |
| `test/http-check.sh` | HTTP response/refusal assertions used by network tests |
| `test/image-inputs.sh` | image-build fingerprint generation and freshness checks |

How the profiles fit together is described in
[Policy and security boundary](policy.md).

## Everyday commands

Install the tools first as described in [Install](install.md). Run
development commands from the `nono-wrapper` checkout:

```sh
just install-dev-tools   # prepare pinned tools and Go dependencies
just                     # list available recipes
just build               # build .cache/bin/nono-wrapper
just fmt                 # format Go source and tests
just verify              # lint and unit tests; no containers or network
```

`just verify` is the everyday check before submitting a change. `just lint`
and `just test-unit` also work separately. Use `mise exec -- just verify`
when running from a shell without mise activation. Local checks run offline
after the dependencies have been prepared. `just build` produces the host
executable; `just install-local` rebuilds it, installs and pins the official
packs, installs our profiles, and checks their inheritance.

For native macOS development checks (best effort), use mise for the
repository-pinned tools. Apple's built-in Bash 3.2 works; a separate Bash
installation is not required. From zsh or another shell without mise
activation, use `mise exec -- just verify`. The macOS sandbox/runtime
validation limits in
[What has been validated](policy.md#what-has-been-validated) still apply.

### Reproducible native builds

`just build` explicitly targets the native host with CGO disabled.
`just generate-manpage` renders the same pinned Go-tool manual that
`install-local` installs, but writes only `.cache/man/nono-wrapper.1`.

From a clean committed checkout with the pinned Go/Just tools and modules cached:

```sh
just verify-reproducible
```

This runs those build/generation recipes in two temporary clones with separate
Go build caches. It compares the native binary, manual and all seven shipped
profile files. Missing, empty, extra profiles or different bytes fail. Go module
downloads are disabled; the command neither installs packs/profiles nor launches
nono, OpenCode or Codex. The caller's `.cache/` is untouched. Failures retain the
temporary trees and print their location; successful trees are removed.

This is local byte-repeatability for the selected host, not proof about upstream
agent packs or an installed sandbox. Compare retained checkouts without building:

```sh
bash test/verify-reproducible.sh --compare /absolute/first/checkout /absolute/second/checkout
```

### Updating pins

Commit `.mise.toml` and `mise.lock` together when updating host tools. Keep
the nono, OpenCode and Codex versions aligned with the Linux container pins
in `test/tools`, regenerate the lockfile with `mise lock`, and review
official pack compatibility in `test/packs`. Host and container archive
checksums can differ because they use different builds of the same release.
Commit `go.mod` and `go.sum` together when updating the Go dependencies.
Upgrade CLI and pack pins together when compatibility changes.

`.gitignore` excludes generated builds and test artifacts in `.cache/` and
machine-local mise overrides (`.mise.local.toml`, `mise.local.toml`,
`mise.local.lock`). Shared mise pins and the Go source in `cmd/` are tracked.

## Recipes

| Command | What it does | Network |
| --- | --- | --- |
| `just install-dev-tools` | install the pinned host tools through mise | yes |
| `just build` | build the host wrapper in `.cache/bin/` | only if Go dependencies are missing |
| `just install-local` | build/install the wrapper, pin official packs, install and check our profiles | yes |
| `just verify` | lint and unit tests | no |
| `just fmt` | format Go source and tests with gofmt | no |
| `just lint` | ShellCheck, Go formatting and go vet | no |
| `just test-unit` | unit and native CLI/installer contract checks | no |
| `just test-native` | isolated native installation, offline profiles, real agent startup and Codex execution | pack installation only |
| `just test-prepare` | build the routine images with the wrapper, pinned tools and official packs | yes |
| `just test-containers` | real profile/launcher integration, representative inherited grants, OpenCode startup and Codex tool execution | no |
| `just test-containers-net` | the same plus positive/negative ChatGPT-host checks | yes |
| `just test` | unit tests followed by the routine container suite | no |
| `just test-net` | unit tests followed by the network-enabled routine container suite | yes |
| `just test-clean` | remove known builds/cached tools and this project's test-image tags | no |

## Local tests

`just test-unit` covers auditing, verification and the probe helper, plus
native CLI and installation/cleanup contracts. CLI tests build the real
wrapper and run it in isolated temporary directories with fake nono/agent
executables. They verify exact arguments (including agent help), profile
selection, executable canonicalization, terminal descriptors, signals, exit
statuses, setup hints and session-specific post-exit reports. Reporting tests
cover clean exits with network denials, repeated events, missing or invalid
audits, lookup timeouts and terminal-control escaping. Verification tests
exercise success/failure and interrupted cleanup; audit tests cover selected
paths, symlinks and change times. Recipe tests build and install the real
binary into temporary homes, use fake nono to check pinned pack setup,
profile selection and failure handling, and use fake Podman to check bounded
cleanup and file preservation.

Configuration tests cover host/directory/file editing, access-mode updates,
JSONC preservation, failed and interrupted writes, symlinks, locking and user
file preservation across reinstalls. Host/path-override tests check that
unrelated policy fields survive and generated files are cleaned up on success
and failure. Preparation tests use local archive fixtures and fake
download/build commands to check cache integrity, refreshes, failure handling
and temporary cleanup. Image-input tests cover changed build recipes and
selected base-image pins. Runner tests ensure missing or stale images never
trigger preparation. Go, just, Bash, `/bin/sh` and standard file utilities
are needed; agents, installed profiles and Podman are not needed for these
unit tests.

These local checks run on the host with temporary homes and explicit
subprocess environments/tool paths. That is fixture isolation; the real-agent
integration tier below supplies OS-level confinement. Go uses its configured
build and module caches. Run native checks on trusted checkouts and use an
unprivileged, secretless environment for automation.

Extra arguments are forwarded to `go test`, so you can select tests and
enable verbose output:

```sh
just test-unit -run TestLauncherInvocation -v
```

`just test` runs those unit tests followed by the routine container suite;
`just test-net` also enables the container networking checks. The explicit
`test-containers` recipes run only the integration suite.

## Native integration smoke test

After installing the pinned tools, run `mise exec -- just test-native` on
Linux, or on macOS (best effort). It installs the wrapper and signed packs
into a disposable home, checks the installed manual and offline profiles,
starts real OpenCode/Codex, and executes the Codex confinement probe without
a login or model call. Pack installation needs network access; the agent
commands use offline profiles. This is a smoke test, not the full Linux
container matrix.

The disposable home is created under your home directory and removed
afterward. On macOS, placing nono's protected state under `/private/var`
conflicts with its system-path grants. Temporary files stay in the OS
temporary directory; the pinned Codex profile does not grant arbitrary
relocated `TMPDIR` paths. The test project is an initialized Git repository
so OpenCode's project discovery stops there instead of loading ancestor
configs.

## Container integration

The full Linux agent integration suite runs in disposable rootless Podman
containers. Install Podman through your system package manager and configure
rootless containers before using these recipes; mise installs the host tools
above. Each image contains the compiled `nono-wrapper`, checksum-pinned nono,
OpenCode and Codex executables (`test/tools`), plus the signed official packs
installed and pinned by nono (`test/packs`).

The six Linux x86_64 release-matrix images are pinned in `test/images`.
Preparation and execution require a Linux x86_64 controller and refuse other
hosts before building, downloading or contacting Podman. From macOS or an
ARM64 host, run this integration tier on a native Linux x86_64 host (or a VM
on one), never under CPU emulation; use `just verify` for native host checks.
Linux VM/container results are not macOS sandbox results.
Ctrl-C stops a run: the runner removes the current container and exits with
status 130.

The `just` recipes default to **Ubuntu 26.04 and openSUSE Leap 16** for
routine checks, including openSUSE's certificate compatibility. Pass `all`
for the full release matrix. The suite focuses on local policy and
representative runtime, Git and agent-state integration rather than
enumerating every upstream grant. It also checks host/file/directory
precedence, read-only and writable user grants, removal, and the agent's
inability to edit active or generated policies while still being able to
write a proposal in `profile-drafts`.

Container commands accept distribution names or `all`, for example:

```sh
just test-prepare ubuntu-26.04
just test-containers ubuntu-26.04
just test-containers-net ubuntu-26.04

# Full release validation:
just test-prepare all
just test-containers-net all
```

Preparation rejects unknown distribution names before builds or downloads.

### Image preparation

Preparation caches the original tool archives in `.cache/tools/`. Every run
copies them into a private temporary directory, verifies the pinned SHA-256
checksums, and extracts fresh executables for the image build. Stale or
corrupt archives are downloaded again and verified; extracted cache files and
checksum markers are never trusted as build inputs. Private build inputs are
removed after preparation. Wrapper/probe-only edits can reuse the image's
pack-install layer.

Each image records its selected base-image reference and checksums for the
Containerfile, dependency and pack installation scripts, preparation,
fingerprint and controller-guard scripts, and `.mise.toml`/`mise.lock`.
The runner hashes those inputs as the caller with `sha256sum` or
`shasum -a 256` and supplies a read-only expected record to the container, so
owner-only configuration files can keep their permissions. Startup checks the
records before testing the agents. A pin change for another distribution does
not invalidate this image.

The probe helper (`test/probe.go`) uses only Go's standard library. Image
preparation requires Go 1.27.2 or newer on the host and cross-compiles a
static Linux x86_64 `nono-wrapper` and `agent-probe`; Go is not needed inside
the containers. The helper owns the Codex app-server exchange, browser-login
callback checks, socket probes and installed-pack validation. The container
entry point rejects a wrapper or helper built from an older source revision,
including changes to the wrapper's Go dependency manifests.

Container startup checks the installed tool/pack versions and manifests.
The container recipes consume prepared images only. Missing or stale images
fail with a `just test-prepare` hint; neither the offline nor network-enabled
runner builds images or downloads dependencies. Run preparation explicitly
after changing image inputs, tool/pack pins, or compiled wrapper/probe
sources. Preparation may need network access when dependencies, archives or
image layers are missing from caches. Profile files are copied from the
checkout when tests start, so profile-only edits do not require rebuilding an
image.

### Inside the containers

Tests use the image's `/home/tester`, where the packs were installed, so the
upstream plugin paths remain valid. The checkout is mounted read-only; no
host home or credentials are mounted. Test scripts refuse direct host runs.
Supervisor update hints are disabled during tests because the versions are
intentionally pinned, including for offline runs.

The Codex probe uses its real app server to execute a command with
`dangerFullAccess`, matching the single-sandbox mode, without a login or
model. It checks project, `.git` and temporary writes alongside refused
home-file reads, home writes and networking, so the denials must come from
nono. Launcher unit tests also verify the on-request approval flag and
absence of the blanket bypass. Browser-login checks start each real agent's
authorization flow and cancel it through its local callback server. They
verify startup and callback access, without signing in or exchanging tokens.
Separate port probes check Codex's fallback port and refusal of unrelated
listeners.

The filesystem probes target locations the container user can otherwise
access. Network-enabled tests include successful requests, so a container
with no network cannot pass merely because every request fails. Endpoint
refusals require nono's explicit 403 response (or a refused proxy CONNECT).
DNS, timeout, TLS and gateway errors fail the request check. Responses are
limited to 1 MiB, and local tests exercise these outcomes using fake curl.

### Results and limits

With the pinned versions in [Install](install.md#pinned-versions), Ubuntu
26.04 and openSUSE Leap 16 passed the full network-enabled suite, including
real Codex tool execution and every ChatGPT hostname check.

Interactive TUIs and approval prompts, completed OAuth/token exchange and
authenticated model sessions need separate validation on both platforms.
These tests cover CLI startup, actual tool execution and the selected policy;
they do not establish every agent workflow.

## Cleanup

`just test-clean` is anchored to this checkout, even when invoked from
another working directory. It refuses symlinked `.cache`, `.cache/tools` and
`.cache/bin` directories, deletes only the known generated files, and removes
directories only when empty. Extra files are retained. Image cleanup targets
only `localhost/nono-agent-profiles:*` tags without force-removal or parent
pruning. The recipe performs no recursive filesystem deletion.
