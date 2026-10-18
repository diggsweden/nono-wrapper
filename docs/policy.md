<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Policy and security boundary

This repository extends nono's official OpenCode and Codex packs with
ChatGPT host allowlists and a shared policy layer:

```text
nolabs-ai/opencode-v1 ── opencode-base ── chatgpt / offline
                              ↑
                        agent-policy
                              ↓
nolabs-ai/codex ──────── codex-base ───── chatgpt / offline
```

## What changes from the official packs

| Area | Our extension |
| --- | --- |
| Network | ChatGPT host lists by default; explicit offline profiles |
| Browser login | TCP callback listeners on 1455, plus Codex's fallback 1457 |
| Agent environment | nono's `environment.allow_vars` keeps the core, editor and toolchain-location variables and excludes unrelated credentials |
| Linux pathname Unix sockets | mediation enabled; sockets require explicit grants, and the wrapper grants the SSH agent's. Not on WSL2: see [WSL2](#wsl2) |
| macOS app automation | Apple Events and access to their Mach broker are denied in the bundled profiles |
| Certificates | read access to `/var/lib/ca-certificates` for openSUSE compatibility |
| Project, agent state, toolchains and Git configuration | inherited from the official pack |
| Temporary files and devices | inherited from nono and the official pack, plus nono's `linux_temp_read` group so temporary files can be read back on Linux; `TMPDIR` is preserved |
| Developer tools | nono's `mise_manager`, `go_runtime` and `java_runtime` groups (read-only), and the Git, delta and SSH allowed-signers configuration a Git setup commonly includes |

### The inherited filesystem access is broader

These extensions deliberately retain upstream grants. With the pinned packs
on Linux:

- **Both agents** get read/write project access and the whole `~/.cache`
  directory, plus read access to Node, Rust, Python and Nix runtime locations
  and user Git configuration.
- **OpenCode** gets its own state, plus read/write `~/.claude`, `~/.codex` and
  `~/.npm`. Its `~/.agents` grant is read-only.
- **Codex** gets read/write `~/.codex` and `~/.agents`. Its inherited npm
  runtime grant is read-only.
- Both get upstream nono package/profile read access and writable
  `profile-drafts`, for sandbox guidance and proposed policy changes.

We do not keep a second copy of those grants. Local settings should express
an intentional policy difference or a compatibility fix still missing
upstream. The certificate grant can go once upstream covers openSUSE's
certificate path.

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
holds for the writable `~/.cache` and, for OpenCode, `~/.npm`. Treat these
caches like the project itself: review what a session changed, or clear them
after an untrusted session.

Gradle does not run under nono 0.80 with network filtering or `--block-net`:
it lists the machine's network interfaces at startup, and nono refuses the
socket Java opens for that. It also needs write access to `~/.gradle`.

It also grants `~/.config/git`, `~/.config/delta` and
`~/.config/ssh/allowed_signers` read-only, because Git stops with an error
when a file its configuration includes cannot be read.

### How inheritance combines policy

With nono 0.80.0, filesystem grants, `network.allow_domain` and
`environment.allow_vars` accumulate through inheritance. A shorter or empty
local list does not remove upstream entries. `network.block: true` is sticky:
an extending profile cannot turn networking back on with `false`.

The wrapper gives explicit user host and exact-path rules precedence over
matching rules in its bundled profiles, as described under
[user precedence](configuration.md#user-precedence-and-rule-protection).
Other inheritance remains native nono behavior.

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
  existing terminal process. The wrapper refuses the official-profile fallback
  on macOS when its bundled profile is missing. Custom `NONO_PROFILE` policies
  must inherit `agent-policy` or supply equivalent restrictions; custom raw
  Seatbelt rules can override inherited rules and need separate review.
- Host allowlists limit destinations, not uploads. Endpoint rules narrow a
  host to listed HTTPS methods and paths and make nono validate its
  certificate. They do not inspect query strings, headers or bodies, and do
  not tie a request to your account: an allowed endpoint accepts data sent
  with any credentials.
- Plain HTTP to a listed host bypasses its endpoint rules; nono 0.80.0 has no
  HTTPS-only setting. String-only rules, such as Codex's `chatgpt.com`,
  tunnel encrypted traffic without inspecting it. A listed hostname resolving
  to a private/local address is not rejected.
- Linux pathname socket access requires explicit `filesystem.unix_socket*`
  grants. Ordinary directory access does not authorize sockets there.
- Running plain `opencode` or `codex` bypasses the wrapper. Explicitly
  permitted remote execution services are outside the local sandbox.

### WSL2

WSL2 is best effort and starts with reduced protection.

- WSL2 has no Unix-socket mediation. nono needs a seccomp listener for it,
  which WSL2 refuses ([microsoft/WSL#9548](https://github.com/microsoft/WSL/issues/9548)),
  and nono then declines to start. When nono reports WSL2, the wrapper
  starts the bundled profiles without `linux.af_unix_mediation` and says so
  on every launch and in `nono-wrapper verify`: the agent can then connect to
  any Unix socket your user can, such as Docker's. The installed profile
  files are not changed, so running `nono` directly with them still fails on
  WSL2.
- The minimum supported release is WSL 2.7.7 (`wsl --update` in Windows).
  The wrapper does not check it.
- Keep projects on the Linux filesystem. nono reports Landlock as unreliable
  on Windows mounts such as `/mnt/c`.
- WSL2 support is not validated: no test here runs on WSL2. The behaviour
  above follows nono 0.79.0's own checks, which the 0.80.0 release notes do
  not change, and
  [WSL2 notes](https://nono.sh/docs/cli/internals/wsl2).

### What has been validated

The full integration suite targets Linux. On macOS 27 ARM64, which is best
effort, native unit/CLI tests, installation, offline-profile verification,
real agent startup and Codex command execution/confinement have passed. This
does not validate interactive TUIs, completed login or authenticated agent
sessions. The wrapper also has Darwin timestamp and filesystem-identity
checks. The Apple Events broker denial was checked separately on macOS with
native PID-addressed calls; it does not establish broader macOS compatibility.
After updating, reinstall the bundled profiles with `just install-local` and
restart running agent sessions. Existing sessions keep their original policy.

## Background

The policy choices originated in
[occwrap](https://codefloe.com/itiquette/occwrap). This repository now
extends the official nono agent packs rather than maintaining replacement
filesystem profiles.
