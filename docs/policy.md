<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Policy and security boundary

This repository extends nono's official OpenCode and Codex packs with
ChatGPT login rules, shared developer network defaults and a policy layer:

```text
nolabs-ai/opencode-v1 ── opencode-base ── chatgpt / offline
                              ↑
                        agent-policy
                              ↓
nolabs-ai/codex ──────── codex-base ───── chatgpt / offline

developer-network ───────────────────── chatgpt only (both agents)
```

## What changes from the official packs

| Area | Our extension |
| --- | --- |
| Network | ChatGPT login plus developer hosts from `developer-network.jsonc`; explicit offline profiles |
| Git SSH | online sessions can use the domain-filtered CONNECT proxy, existing SSH agent and a read-only public host-key snapshot |
| Browser login | TCP callback listeners on 1455, plus Codex's fallback 1457 |
| Agent environment | nono's `environment.allow_vars` keeps the core, editor and toolchain-location variables and excludes unrelated credentials |
| Linux pathname Unix sockets | mediation enabled; sockets require explicit grants, and the wrapper grants the SSH agent's. Not on WSL2: see [WSL2](#wsl2) |
| macOS app automation | Apple Events and access to their Mach broker are denied in the bundled profiles |
| Project, agent state, toolchains and Git configuration | inherited from the official pack |
| Temporary files and devices | inherited from nono and the official pack, plus nono's `linux_temp_read` group so temporary files can be read back on Linux; `TMPDIR` is preserved |
| Developer tools | nono's `mise_manager`, `go_runtime` and `java_runtime` groups (read-only), and the Git, delta and SSH allowed-signers configuration a Git setup commonly includes |

### The inherited filesystem access is broader

These extensions deliberately retain upstream grants. With the pinned packs
on Linux:

- **Both agents** get read/write project access and individually granted caches,
  plus read access to Node, Rust, Python and Nix runtime locations
  and user Git configuration.
- **OpenCode** gets its own state, plus read/write `~/.claude`, `~/.codex` and
  `~/.npm`. Its `~/.agents` grant is read-only.
- **Codex** gets read/write `~/.codex` and `~/.agents`. Its inherited npm
  runtime grant is read-only.
- Both get upstream nono package/profile read access and writable
  `profile-drafts`, for sandbox guidance and proposed policy changes.

We do not keep a second copy of those grants. Local settings should express
an intentional policy difference or a compatibility fix still missing
upstream. nono 0.81.0's `system_read_linux_core` group includes openSUSE's
`/var/lib/ca-certificates`; the wrapper needs no extra certificate grant.

`agent-policy` excludes upstream's `user_caches_linux` group: its recursive
`~/.cache` write grant would expose Mise's code-loading cache. Agent-specific
cache grants and the Go build cache remain available.

On top of that, `agent-policy` adds read access through nono's own groups:

- `/tmp` on Linux, so temporary files can be read back. Other programs' files
  there become readable to the agent as well.
- mise's data and configuration (`~/.local/share/mise`, `~/.config/mise`),
  where mise-installed tools live. A mise configuration that holds secrets in
  its `[env]` section exposes them.
- Go and Java locations: `~/go`, the Go build cache, `~/.m2` and `~/.gradle`
  caches and wrappers.

It grants read and write access to `~/.m2/repository` and `~/.m2/wrapper`,
because Maven writes lock files and downloads there even offline, and read
access to `~/.m2/settings.xml`, because Maven stops when that file exists but
cannot be read. DevBase writes it for proxies and registries. **The agent can
read any repository or proxy passwords in it**; keep secrets in
`settings-security.xml`-encrypted form or environment variables. The master
password file `~/.m2/settings-security.xml` is not granted.

**Write access to the Maven repository outlasts the session.** An agent can
replace a plugin or dependency jar there, or a wrapper distribution, and your
next Maven build outside the sandbox runs it with your full rights. The same
holds for individually writable caches and, for OpenCode, `~/.npm`. Treat these
caches like the project itself: review what a session changed, or clear them
after an untrusted session.

The last Gradle check, with nono 0.80, failed with network filtering or
`--block-net`:
it lists the machine's network interfaces at startup, and nono refuses the
socket Java opens for that. It also needs write access to `~/.gradle`.

It also grants `~/.config/git`, `~/.config/delta` and
`~/.config/ssh/allowed_signers` read-only, because Git stops with an error
when a file its configuration includes cannot be read.

### How inheritance combines policy

With nono 0.81.0, filesystem grants, `network.allow_domain` and
`environment.allow_vars` accumulate through inheritance. A shorter or empty
local list does not remove upstream entries. `network.block: true` is sticky:
an extending profile cannot turn networking back on with `false`.

The wrapper gives explicit user host and exact-path rules precedence over
matching rules in its bundled profiles, as described under
[user precedence](configuration.md#user-precedence-and-rule-protection).
Other inheritance remains native nono behavior.

For bundled agent profiles, a protected session layer supplies exact
non-default XDG/Codex runtime directories. It uses the same canonical paths
as state preparation and omits an exact path when the user specifies its own
access mode. Effective-policy checks reject broader grants that would defeat
user restrictions. Inherited deny rules and signed pack references remain
intact; external profiles receive no automatic runtime grants.

Managed executables and launch configuration are invariants for all profiles.
The wrapper
resolves the selected agent and nono, including Mise shims, and checks the
effective filesystem manifest plus launch-time grants before executing the
agent. Writable grants covering those executables, the wrapper or resolved
Mise binary, or overlapping known Mise configuration, trust/state, cache and
data/installation trees, stop the launch. Global configuration matters as much
as binaries: it can select tools, change trust settings and run host templates.
Invocation entries are protected as well as resolved binaries, so a writable
shim directory cannot replace a dispatcher while leaving its target intact.
This includes symlinked parent directories and intermediate symlink targets.
Protected files with multiple hard links are rejected when a directory is
writable: an alias could live anywhere under that grant. Use a single-link
copy of the protected file; the wrapper does not scan workspaces or caches
for aliases.
These checks apply even when no user path override or runtime layer is needed.
Automatic Git SSH also protects its resolved SSH executable and source host
keys from writable grants; it does not grant private-key file access.

The pinned official packs supply no provider host allowlists or environment
allowlists, so those lists currently come from these extensions. Review both
packs and nono's defaults when upgrading, and run the network checks to catch
changes to the effective policy. Linux pathname-socket mediation is an
explicit local setting; upstream socket grants are still inherited.

## What the boundary means

- The agent and its local subprocesses run under nono. Codex's inner OS
  sandbox is disabled, with on-request approvals retained as described in
  [Usage](usage.md#codex-one-sandbox-approvals-retained). nono is the
  enforcement boundary for Codex and its commands.
- Project access includes `.git` and project secrets. On Linux, nono cannot
  carve a hidden or read-only exception out of that read/write grant.
- The inherited agent-state, cache and runtime grants expose their contents,
  including any credentials stored there. State is shared across projects.
- On macOS, the bundled profiles deny Apple Events and access to
  `com.apple.coreservices.appleevents`. Both rules are needed: denying only
  `appleevent-send` did not prevent native calls addressed directly to an
  existing terminal process. Missing bundled profiles stop the wrapper on
  every platform instead of falling back to a different upstream policy.
  Custom `NONO_PROFILE` policies
  must inherit `agent-policy` or supply equivalent restrictions; custom raw
  Seatbelt rules can override inherited rules and need separate review.
- Host allowlists limit destinations, not uploads. Endpoint rules narrow a
  host to listed HTTPS methods and paths and make nono validate its
  certificate. They do not inspect query strings, headers or bodies, and do
  not tie a request to your account: an allowed endpoint accepts data sent
  with any credentials.
- Plain HTTP to a listed host bypasses its endpoint rules; nono 0.81.0 has no
  HTTPS-only setting. String-only rules, such as Codex's `chatgpt.com`,
  tunnel encrypted traffic without inspecting it. A listed hostname resolving
  to a private/local address is not rejected.
- Linux pathname socket access requires explicit `filesystem.unix_socket*`
  grants. Ordinary directory access does not authorize sockets there.
- Running plain `opencode` or `codex` bypasses the wrapper. Explicitly
  permitted remote execution services are outside the local sandbox.

### WSL2

To work around a known [WSL2 seccomp-listener issue](https://github.com/microsoft/WSL/issues/9548),
nono-wrapper disables Unix-socket filtering (`linux.af_unix_mediation`) for
bundled profiles when nono reports WSL2. The agent may then connect to local
services available to your user, such as Docker. The wrapper prints a short
note at launch and in `doctor`; installed profiles are not modified.

- The minimum supported release is WSL 2.7.7 (`wsl --update` in Windows).
  The wrapper does not check it.
- Keep projects on the Linux filesystem. nono reports Landlock as unreliable
  on Windows mounts such as `/mnt/c`.
- WSL2-specific wrapper behavior is covered by simulated tests; this project
  does not run integration tests on WSL2. See nono's
  [WSL2 notes](https://nono.sh/docs/cli/internals/wsl2) for upstream details.

### What has been validated

The full integration suite targets Linux. Earlier macOS 27 ARM64 validation
passed native unit/CLI tests, installation, offline-profile verification,
real agent startup and Codex command execution/confinement. This
does not validate interactive TUIs, completed login or authenticated agent
sessions. The wrapper also has Darwin timestamp and filesystem-identity
checks. Native macOS integration has not been rerun for nono 0.81.0.
The Apple Events broker denial was checked separately on macOS with
native PID-addressed calls; it does not establish broader macOS compatibility.
After updating, reinstall the bundled profiles with `just install-local` and
restart running agent sessions. Existing sessions keep their original policy.

## Background

The policy choices originated in an earlier wrapper project. This repository
extends the official nono agent packs rather than maintaining replacement
filesystem profiles.
