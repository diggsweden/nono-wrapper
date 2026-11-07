<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Development

## Project layout

```text
cmd/nono-wrapper/   Go CLI: launch, config, doctor, changes and man
docs/              user and developer documentation
docs/manpages/     the manual source, embedded and rendered for man(1)
profiles/          editable nono profiles
test/              CLI/installation tests, container probes and version pins
```

| File | Purpose |
| --- | --- |
| `.mise.toml` | explicit host sandbox/build-tool versions; host-tool lockfiles are disabled |
| `justfile` | setup, local installation and test recipes |
| `go.mod`, `go.sum` | pinned Go dependency versions and checksums |
| `test/packs` | official pack pins shared by user installation and test images |
| `profiles/agent-policy.jsonc` | shared environment/socket overrides and certificate compatibility |
| `profiles/developer-network.jsonc` | shared online forge, reference and vulnerability-data hosts |
| `profiles/opencode-base.jsonc` | composes the official OpenCode pack with the shared policy |
| `profiles/codex-base.jsonc` | composes the official Codex pack with the shared policy |
| `profiles/*-chatgpt.jsonc`, `profiles/*-offline.jsonc` | make the network choice |
| `cmd/nono-wrapper/main.go` | urfave/cli v3 command definitions |
| `cmd/nono-wrapper/launch.go` | profile resolution and nono launch arguments |
| `cmd/nono-wrapper/config.go` | persistent user allowance commands and atomic JSONC editing |
| `cmd/nono-wrapper/policy.go` | user precedence over matching bundled profile rules |
| `cmd/nono-wrapper/filesystem_policy.go` | canonical path matching and effective access-mode checks |
| `cmd/nono-wrapper/session.go` | supervised launch, signal forwarding and post-exit audit summary |
| `cmd/nono-wrapper/git_ssh.go` | session-only Git SSH defaults, public host-key snapshots and CONNECT transport |
| `cmd/nono-wrapper/doctor.go` | installed-tool and offline-profile checks |
| `cmd/nono-wrapper/changes.go` | optional, manually invoked changed-project-file listing |
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

`just build` disables module downloads and uses the local Go toolchain with
read-only module metadata. Prepare dependencies with `just install-dev-tools`
first. Building does not install profiles, pull packs or launch an agent.

For native macOS development checks (best effort), use mise for the
repository-pinned tools. Apple's built-in Bash 3.2 works; a separate Bash
installation is not required. From zsh or another shell without mise
activation, use `mise exec -- just verify`. The macOS sandbox/runtime
validation limits in
[What has been validated](policy.md#what-has-been-validated) still apply.

### Host, artifact and execution platforms

On M1, a native build uses a `darwin/arm64` Go toolchain and produces a
`darwin/arm64` wrapper. MacPorts supplies macOS prerequisites; mise selects the
repository-pinned build tools. No Linux container or CPU emulation is needed
for this build.

The artifact target, test execution environment and CI runner are independent.
The release declaration in `.forge-ci/artifacts.yml` includes Linux and Darwin
amd64/arm64 artifacts, while `.github/workflows/pullrequest.yml` currently runs
the native checks on Ubuntu amd64 only. Cross-built Darwin or Linux arm64 files
are not proof that tests ran on those platforms. Native Mac acceptance needs an
actual Darwin arm64 runner; Linux arm64 containers on an M1 Linux VM prove only
Linux behavior. Identify any emulated execution explicitly.

Keep native build/fixture checks, isolated real-agent acceptance and distribution
container tests as distinct execution capabilities. The current container tool
archives are Linux amd64 pins; a Linux arm64 lane needs independently verified
agent archives and image preparation. That is not required to build the wrapper
natively on M1. `just test-native` remains an explicit pack-installing acceptance
operation in a temporary home, separate from the build-only path.

### Reproducible native builds

`just build` explicitly targets the native host with CGO disabled.
`just generate-manpage` renders the same pinned Go-tool manual that
`install-local` installs, but writes only `.cache/man/nono-wrapper.1`.

From a clean committed checkout with the pinned Go/Just tools and modules cached:

```sh
just verify-reproducible
```

This runs those build/generation recipes in two temporary clones with separate
Go build caches. It compares the native binary, manual and all eight shipped
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

Update `.mise.toml` when changing sandbox/build-tool versions.
DevBase owns host OpenCode and Codex versions. Container-test agent
archives in `test/tools` are independently pinned for reproducibility.
Review official pack compatibility in `test/packs` when updating the sandbox
runtime. Host-tool lockfiles are currently disabled.
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
| `just test-apparmor-setup` | run the README's Fish/Bash setup scripts in a disposable Ubuntu container | no |
| `just test-containers` | real profile/launcher integration, representative inherited grants, OpenCode startup and Codex tool execution | no |
| `just test-containers-net` | the same plus positive/negative ChatGPT-host checks | yes |
| `just test` | unit tests followed by the routine container suite | no |
| `just test-net` | unit tests followed by the network-enabled routine container suite | yes |
| `just test-clean` | remove known builds/cached tools and this project's test-image tags | no |

## Local tests

`just test-unit` covers the doctor and changes commands and the probe helper, plus
native CLI and installation/cleanup contracts. CLI tests build the real
wrapper and run it in isolated temporary directories with fake nono/agent
executables. They verify exact arguments (including agent help), profile
selection, executable canonicalization, terminal descriptors, signals, exit
statuses, setup hints and session-specific post-exit reports. Reporting tests
cover clean exits with network denials, repeated events, missing or invalid
audits, lookup timeouts and terminal-control escaping. Doctor tests
exercise success/failure and interrupted cleanup; changes tests cover selected
paths, symlinks and change times. Recipe tests build and install the real
binary into temporary homes, use fake nono to check pinned pack setup,
profile selection and failure handling, and use fake Podman to check bounded
cleanup and file preservation.

Configuration tests cover domain/TCP-port/path editing, exact stored-key
removal (including keys outside the add grammar), access-mode updates,
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

Executable-policy tests include inherited and external-profile writes,
workspace and ambient grants, aliases, managed installation trees and an
unusable manifest. Runtime-path tests reject reserved expressions or globs
hidden behind symlinks. Native negative probes verify refusal before agent
execution and preservation of a harmless executable stand-in.
Control-plane tests include Mise config, trust/state/install locations, explicit
config overrides, backing-file aliases and dangling links. Path discovery is
tested with executable configuration to ensure it never evaluates that input.
Native cache probes cover read/write/delete operations at default, XDG and
explicit tool-cache roots while refusing unrelated cache siblings. Mise cache
is intentionally writable; cache overrides into control-plane state still fail.
Python fixtures exercise real multiprocessing semaphore creation in `/dev/shm`.

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

### Native Git SSH integration

The focused SSH test uses real nono, Git and OpenSSH against a loopback-only
Go SSH server and a temporary agent containing generated test keys. It needs
no model, account, installed agent pack, external server or user credentials:

```sh
NONO_SSH_TEST=1 NONO_TEST_NONO=/absolute/path/to/nono \
  go test -count=1 -v -run '^TestNativeGitSSH$' ./test
```

`NONO_TEST_NONO` selects the native executable, not a Mise shim. The current
test targets Linux. It covers clone/fetch/push, strict host-key rejection,
unlisted destinations, direct TCP bypass refusal, explicit SSH overrides,
shared-default endpoint overrides/denies, and temporary-policy cleanup.
The fixture relocates `XDG_CONFIG_HOME` to a path containing spaces.
Unit proxy tests also check CONNECT to port 443, buffered tunnel bytes and
credential-free diagnostics. macOS/WSL2 Git SSH integration remains untested.

## GitHub checks and releases

`.github/workflows/pullrequest.yml` runs on pull requests and pushes to
`main`. It calls Digg Sweden's Forge CI v0.91.13 and runs the isolated unit
suite, `just lint` and native build with the checkout's selected tools. The general
lint phase is temporarily disabled with `lint-engine: none` while waiting for
the next nanolinter release on Codefloe; its lint/security checks are not
running. ShellCheck, Go formatting and vet still run through `just lint`.
Re-enable that input when the new linter is available. Local
`just verify` remains the full lint and unit check.

The release workflow reruns those checks, then asks Forge CI to prepare a
signed release commit and final tag, generate the changelog, and publish
standalone Linux/macOS amd64/arm64 binaries with checksums, Sigstore signatures
and SBOMs. `.forge-ci/artifacts.yml` selects the Go CLI build; its build-stage
tests are skipped because the preceding checks already ran them with Just.
Profiles are available in the tagged source; the existing `just install-local`
recipe installs profiles, the manual and signed upstream packs from that
checkout. A binary download alone does not install those dependencies.

The workflow uses the organization's existing `RELEASE_TOKEN` and
`RELEASE_SSH_SIGNING_KEY` secrets and `RELEASE_BOT_NAME`/`RELEASE_BOT_EMAIL`
variables. GitHub resolves these through the explicit `secrets` and `vars`
mappings; repository-level duplicates are unnecessary.

`.forge-ci/allowed_signers` trusts the organization release signer already
listed in Digg Sweden's Forge CI allowlist and the maintainer's local
request-tag signer. Keep the organization entry aligned when its key rotates.

Require the build/test check before merging. Restrict creation of
`release-request/*` tags to releasers and prevent changes to final `v*` tags,
following [Forge CI's GitHub setup guide][forge-ci-github].

From the checked, current `main` commit, request a stable release:

```sh
git tag -s release-request/v0.1.0 -m "Release v0.1.0"
git push origin release-request/v0.1.0
```

Forge CI creates `v0.1.0`; do not create or move the final tag manually.
Keep the workflow SHA, CLI version and runtime-image versions aligned when
updating Forge CI. Its current production flow accepts stable versions only.

[forge-ci-github]: https://github.com/diggsweden/forge-ci/blob/v0.91.13/docs/github.md

## Native integration smoke test

Use the tested OpenCode 2 CLI for the fixture installation:

```sh
mise exec opencode@2.2.5 -- just test-native
```

On Linux the v2 probe first runs online/offline `session list --format json`
with no server URL or host-port grants: OpenCode starts its own standalone
server in nono's private loopback. A controlled host-loopback service is
reachable outside the sandbox and unreachable inside it. Namespace support
is required; the test fails rather than skipping that boundary.

The probe then starts a foreground server and clients under the wrapper on
ordinary localhost. Its temporary user profile grants one chosen TCP port,
the ephemeral callback listener and the local server-password variable.
It checks online/offline access, rejects a bad password, cancels the browser
callback and restores the user profile after stopping the server. Model
catalogue fetching is disabled in that fixture. No model account or completed
login is needed. The explicit-server/browser-callback part does not use
namespaces; the preceding Linux standalone checks do. No test changes host
AppArmor settings.

The native suite also tests Linux TCP-port scope against a controlled server
on this machine's non-loopback address: the offline profile refuses it before
the grant, connects with the grant, and refuses it after removal. This checks
actual socket enforcement rather than relying on `nono why`'s localhost label.
It requires curl and a non-loopback IPv4 address; no external server is used.

All native agent state uses non-default XDG roots and `CODEX_HOME` outside the
fixture HOME, workspace and `/tmp`. Real agent startup checks the configured
paths, while write probes verify that read-only user overrides take precedence
and that removing an override restores the runtime default. This prevents
inherited home or temporary-directory grants from masking missing permissions.

The disposable fixture lives under the checkout's ignored `.cache`, not the
real HOME. Before installation, the suite resolves symlinks and rejects cache
locations under `/tmp` (also `/private/tmp`, `/private/var` and `/var` on macOS).
Move the checkout and its cache outside those broadly granted system locations;
the test does not weaken the shipped policy to accommodate them. Native probes
also require unrelated scratch-root state to remain unreadable and unwritable.

To validate the DevBase integration after selecting its AI pack, keep the
generated delegates and limit the test's tools to OpenCode, Codex and nono:

This requires a DevBase revision or overlay that supplies the AI pack and
delegates. The upstream snapshot reviewed on 2026-11-04 does not contain them;
check that your revision does before applying these instructions.

```sh
devbase=/absolute/devbase-core
yq -p=toml -o=toml \
  '.tools |= with_entries(select(
    .key == "aqua:anomalyco/opencode" or
    .key == "aqua:openai/codex" or .key == "nono"))' \
  "$HOME/.config/mise/config.toml" > /tmp/devbase-ai-test.toml
NONO_TEST_MISE_CONFIG=/tmp/devbase-ai-test.toml \
NONO_TEST_MISE_DATA_DIR="$HOME/.local/share/mise" \
NONO_TEST_FISH_FUNCTIONS="$devbase/dot/.config/fish/functions" \
NONO_TEST_BASH_FUNCTIONS="$devbase/devbase_files/bashrc-agent-sandbox" \
just test-native
rm /tmp/devbase-ai-test.toml
```

The suite copies that config into its disposable home, reuses installed tool
artifacts through a symlink, and creates isolated Mise shims, wrappers
and trust state. Both ordinary shell commands and `mise exec` must reach the
real agents through exactly one nono-wrapper sandbox. The supplied Fish
functions are tested together with the generated Mise wrappers. The test
also sources the Bash entry points with an unactivated raw-tool PATH. It
does not activate or modify your global Mise configuration.

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

### AppArmor setup scripts

Prepare the Ubuntu image, then test the actual scripts extracted from README:

```sh
just test-prepare ubuntu-26.04
just test-apparmor-setup
```

The test runs as root **inside a disposable rootless container**, with no
network, no Linux capabilities and no privilege escalation. It installs the
generated file into that container's `/etc/apparmor.d` and uses the real
AppArmor compiler. It covers paths with spaces, future Mise versions,
invalid paths, validation/install/load failures, doctor failures and temporary
file cleanup. `sudo` is a fixture that performs the real file installation
but compiles the load step with `-Q -K`; it never loads policy into the shared
host kernel. `mise` and `doctor` are fixtures; real agent/network behavior is
covered by the separate integration suite. This does not establish that the
host grants `userns` after an actual administrative profile load.

### Agent profiles

Images use the pinned OpenCode 2 CLI and matching pack. Build and run them with:

```sh
just test-prepare
AGENT_TEST_NETWORK=1 just test-containers
```

The release tag and checksum are pinned in `test/tools`; Renovate keeps
OpenCode updates within supported 2.x releases.

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
fingerprint and controller-guard scripts, and `.mise.toml`.
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
limited to 1 MiB. A successful HTTP 200/206 response that reaches that cap
proves reachability without downloading the rest of a large catalogue;
it is not a response-body validation. Local tests exercise these outcomes
using fake curl.

### Results and limits

The native Go cache fixture uses a local file module proxy, compiles a new
dependency, repeats the test warm, and checks that installed tools stay
protected. On Linux it also runs an `httptest` server and refuses a controlled
host-localhost listener. The Cargo fixture builds a local Git dependency cold
and warm while refusing a dedicated credential file. Neither needs registry
credentials or external downloads during execution.

The Gradle fixture normally checks startup with `--version`. To run the actual
offline task probe, use `NONO_TEST_GRADLE_BUILD=1 just test-native`. This still
fails with nono 0.81.0's wildcard-IP/FileLockContentionHandler socket limitation,
even with private loopback and writable caches; it is not a passing build claim.

Container preparation and execution explicitly use the local rootless engine
and Linux amd64 images, rejecting a foreign architecture before execution.
Preparation continues other distributions after a failed pull/build and exits
nonzero with the failed names. Tumbleweed now pins the available 20261103 image;
registry labels identify the openSUSE build and inspection confirms linux/amd64.

The network-enabled routine suite passed with nono 0.81.0, OpenCode 2.2.5,
the OpenCode 0.4.2 pack, Codex 0.163.0 and the Codex 0.2.3 pack on Ubuntu
26.04 and openSUSE Leap 16. openSUSE uses nono's upstream certificate grant;
the wrapper's duplicate grant has been removed. The updated Tumbleweed 20261103
image also passes the network-enabled suite after the preparation/architecture
changes. Debian, Fedora and Arch have not been rerun for this change.

OpenCode 2 starts its own standalone server in private loopback, in
online and offline modes, without host-port grants. Controlled host localhost
access is refused, and live endpoint checks run through the namespace proxy
bridge with the expected allow/deny decisions. The explicit sandboxed server
workflow also passes: wrong passwords are rejected and modern token-sharing
browser callbacks are started and cancelled.

The container runner maps two reserved `.nono.test` names to `127.0.0.2`
and `127.0.0.3`. Two controlled HTTP servers use the same port on those
separate addresses. The approved name returns the expected body only after
`allow-domain`, while the other service remains refused. A third listener on
a different port of the approved IP also works, explicitly demonstrating
that the allowance is not port-scoped. `remove-domain` restores refusal.
This proves deliberate HTTP proxy access to a host-loopback service without
granting direct TCP ports. It does not validate every browser tool, HTTP
client or Ollama model workflow, or guarantee separation from wildcard binds.
Live HTTPS reachability is checked separately, with bounded retries for
transient responses and a strict failure if the final result is a gateway,
DNS, TLS or timeout error.

Native Linux acceptance passed after the administrator loaded the targeted
AppArmor user-namespace rule. This includes OpenCode 2 standalone startup in
private loopback, explicit server/password checks and a cancelled modern
browser callback. Codex 0.164.0 command execution/confinement also passed in
those isolated native fixtures. The native v2 suite requires namespace
support and does not bypass a host restriction. Its ordinary
[explicit-server workflow](opencode.md) remains available for
modern browser login and deliberate host networking.

Native Linux Git SSH tests also passed with nono 0.81.0, including its new
`why --self --unix-socket` diagnostics with the agent grant on and off.
The optional approval and loopback examples pass profile validation.
This host initially refused namespace creation; the targeted AppArmor rule
resolved that startup restriction, and the native integration suite passed.
Rootless Ubuntu/openSUSE
containers do permit the namespaces and passed the updated standalone and
proxy tests with the normal container security settings. Native macOS/WSL2
integration has not been rerun for 0.81.0. The v2 documentation review found
no standalone fixed-port or Unix-socket configuration in pinned 2.2.5;
`service set disabled true` uses the same dynamic-port standalone path.

Interactive TUIs and approval prompts, completed OAuth/token exchange and
authenticated model sessions need separate validation on both platforms.
These tests cover CLI startup, actual tool execution and the selected policy;
they do not establish every agent workflow.

## Cleanup

`just test-clean` is anchored to this checkout, even when invoked from
another working directory. It refuses symlinked `.cache`, `.cache/tools` and
`.cache/bin` directories, deletes only the known generated files, and removes
directories only when empty. Extra files are retained. Image cleanup targets
only `localhost/nono-wrapper:*` tags without force-removal or parent
pruning. The recipe performs no recursive filesystem deletion.
