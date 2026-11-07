<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Usage

## Start an agent

Open a terminal in a project below `~/development`. If your projects live
elsewhere, set `NONO_WRAPPER_ROOT` to the absolute workspace root first
(see [Where an agent may start](#where-an-agent-may-start)). For example:

```sh
cd ~/development/my-project
```

Start OpenCode:

```sh
nono-wrapper opencode
```

Or start Codex:

```sh
nono-wrapper codex
```

The current project directory is the writable workspace granted to the agent.

### Where an agent may start

The agent gets read and write access to everything in its start directory.
By default, it must start **below `~/development`**. The wrapper refuses
`~/development` itself, which would expose every project at once, and any
directory outside it:

```text
nono-wrapper: will not start in "/home/user": it is outside the project root "/home/user/development".
```

An explicitly set `NONO_WRAPPER_ROOT` allows starting **in that directory or
any directory below it**, but not outside it. This supports a workspace that
groups the repositories you want to share, for example:

```text
~/agent-workspace/
  project-a/
  project-b/
```

To work on that whole existing workspace:

```sh
cd ~/agent-workspace
env NONO_WRAPPER_ROOT="$HOME/agent-workspace" nono-wrapper codex
```

To use that root in every session, set it in your shell's startup file:

```sh
export NONO_WRAPPER_ROOT="$HOME/agent-workspace"     # Bash: ~/.bashrc
set -gx NONO_WRAPPER_ROOT "$HOME/agent-workspace"    # Fish: ~/.config/fish/config.fish
```

The whole start directory is writable, including both projects. Starting in
`project-a` instead shares that directory as the workspace. Other profile
grants still apply. The root must be absolute and must not be, or contain,
your home directory; symlinks are resolved before these checks. Existing
checks for protected policy files and executables still apply.

`config AGENT add-dir` adds filesystem access; it does not select the start
directory or change `NONO_WRAPPER_ROOT`.

**Configuration repositories need a yes.** When the start directory holds a
Git repository named `config`, or ending in `-config` or `_config`, within two
levels, including submodules and nested clones inside projects, the wrapper
lists it and asks:

```text
nono-wrapper: the start directory contains configuration repositories:
  /home/user/agent-workspace/team-config
The agent would get read and write access to them as well.
Include them? [y/N]
```

Anything but `y` stops the launch: nono cannot leave one directory out of the
start directory on Linux, so the alternative is to start somewhere without
it. Landlock deny rules that would allow this are proposed in
[landlock-lsm/linux#28](https://github.com/landlock-lsm/linux/issues/28). The
question is asked on every launch. Without a terminal, for example in a
script, the wrapper stops instead of asking. `NONO_WRAPPER_ALLOW_CONFIG=1`
includes them without asking.
The wrapper is installed for your user, so you can run it outside the
`nono-wrapper` checkout. The `just` recipes belong to that checkout.

[nono](https://nono.sh) enforces the sandbox around the agent and its child
processes. The wrapper selects a profile, grants the resolved executable and
runs nono with the terminal connected directly. After nono exits, the wrapper
prints the session's audit summary and preserves its exit status.

If the agent on `PATH` is a mise shim, the wrapper asks `mise which` for the
real executable and grants that file. This happens in shells where mise is not
activated, such as scripts and editors. When mise cannot resolve the agent in
the current directory, the wrapper stops and shows mise's message.

## Commands

`nono-wrapper` is a native executable and works from Fish, Bash or another
shell. Its commands are:

| Command | Purpose |
| --- | --- |
| `nono-wrapper opencode [arguments...]` | launch OpenCode under nono |
| `nono-wrapper codex [arguments...]` | launch Codex under nono |
| `nono-wrapper run [sandbox options...] opencode\|codex [arguments...]` | launch with extra grants for one session; see [Temporary sandbox options](#temporary-sandbox-options) |
| `nono-wrapper config opencode\|codex show` | list user-added allowances |
| `nono-wrapper config opencode\|codex allow-domain\|remove-domain DOMAIN\|HTTPS_URL` | manage user domain and HTTPS endpoint allowances |
| `nono-wrapper config opencode\|codex open-port\|remove-open-port PORT` | manage host-independent TCP connect/listen allowances |
| `nono-wrapper config opencode\|codex add-dir\|remove-dir PATH` | manage user directory allowances, read-only unless `--read-write` |
| `nono-wrapper config opencode\|codex add-file\|remove-file PATH` | manage user file allowances |
| `nono-wrapper doctor [opencode\|codex ...]` | check tools, project root and offline profiles; defaults to both agents |
| `nono-wrapper changes MARKER [DIRECTORY]` | list selected project files changed since a marker |
| `nono-wrapper man` | print the manual, as `man nono-wrapper` shows it |
| `nono-wrapper ssh-proxy HOST PORT` | Git transport helper through the active nono proxy; grants no access itself |
| `nono-wrapper completion bash\|zsh\|fish` | print a completion script for that shell, to source from its startup file |
| `nono-wrapper --help` | show wrapper help |
| `nono-wrapper --version` | show embedded build and Go toolchain information |

Agent arguments are forwarded exactly: `nono-wrapper codex --help` displays
Codex's help under nono. Use `nono-wrapper help codex` for the wrapper's
description of that subcommand.

Release binaries embed the release version and source commit. Checkout builds
identify themselves as `devel`, with module/VCS metadata when available; help
links to the project documentation.

The `config` commands are described in [Configuration](configuration.md);
`doctor` and `changes` in [Check and review](verify-and-review.md).

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | success |
| 1 | `doctor`: a check failed, including a missing selected agent; `changes`: changed files are listed |
| 64 | usage error: an unknown command or flag, or wrong arguments |
| 65 | a malformed value or a conflicting whole-domain/endpoint edit |
| 66 | a named file or directory does not exist |
| 70 | an unexpected failure |
| 77 | permission denied, or refused by policy |
| 78 | the user configuration is unusable |
| 125 | `opencode`, `codex`: the wrapper could not start the session, a refused start directory included |
| 127 | `opencode`/`codex`: nono or the agent is not on `PATH`; `doctor`: nono or a probe utility is not on `PATH` |
| 128 + N | stopped by signal N |
| other | `opencode`, `codex`: the agent's own status |

The wrapper's own failures use the sysexits(3) values. The agent commands
follow `env` and `timeout` instead: the agent's status passes through, so the
wrapper reports its own failures there as 125 or 127. An agent can still
return those values itself. Run without a command, `nono-wrapper` shows a
short help on standard error and exits 64. `nono-wrapper --help` and
`man nono-wrapper` print the same table.

## Temporary sandbox options

Use `run` for nono-style options that apply only to one session:

```sh
nono-wrapper run --allow /path/to/shared-project opencode
nono-wrapper run --read /path/to/reference codex -- --help
nono-wrapper run --profile codex-offline codex -- features list
```

Sandbox options go **before the agent name**. Everything after the name goes to
the agent; one optional `--` separator is consumed. The existing
`nono-wrapper opencode` and `nono-wrapper codex` shortcuts are unchanged.

`--allow` means read/write, `--read` read-only and `--write` write-only. File
variants use `--allow-file`, `--read-file` and `--write-file`. Path flags,
`--allow-domain` and `--open-port` are repeatable. `--profile` overrides
`NONO_PROFILE`; `--block-net` blocks networking. With no explicit profile it
selects the agent's bundled offline profile instead of inheriting online rules.
It cannot be combined with `--allow-domain` or `--open-port`; explicit profiles
and user rules must also be compatible with nono's offline policy. Native deny
rules and wrapper safety checks still apply; an added read-only grant does not
revoke inherited writes. `--open-port` permits TCP connect and listen, including
non-loopback destinations, and conflicts with OpenCode's private-loopback mode.

For persistent access, use `config AGENT add-dir PATH` (read-only) or
`add-dir --read-write PATH`, and `add-file` for single files; see
[Configuration](configuration.md#manage-allowances-from-the-cli). `allow`,
`read`, `allow-file` and `read-file` are short forms using nono's access-mode
names. `allow-tcp-port` and `remove-tcp-port`, the beta names of `open-port`
and `remove-open-port`, still work.

`run --silent` suppresses nono output and the wrapper report, but not the agent's
output. `-V` and the compatible `-v` both show the wrapper version.

### Mapping to nono

| Wrapper spelling | Native meaning | Lifetime / compatibility |
| --- | --- | --- |
| `run --profile/-p` | `nono run --profile/-p` | One launch; overrides `NONO_PROFILE` |
| `run --allow/-a`, `--read/-r`, `--write/-w` | Read/write, read-only, write-only | Additive, temporary directory grants |
| `run --allow-file`, `--read-file`, `--write-file` | Same modes for an exact file | Temporary file grants |
| `run --allow-domain` | Native domain/HTTPS endpoint grant | Temporary; repeat the flag for each rule |
| `run --open-port` | Native bidirectional TCP exception | Not a localhost-only or domain-scoped grant |
| `run --silent/-s` | Native silent output plus no wrapper report | Agent stdout/stderr still pass through |
| `run --no-diagnostics=false` | Restore native diagnostics and save prompts | Wrapper suppresses these by default; audit remains on |
| `config AGENT add-dir`, `add-file` | Save a read-only path, or read/write with `--read-write` | Persistent; `read`, `allow` and their `-file` forms are short forms |
| `config AGENT open-port` | Save `network.open_port` | Persistent; `allow-tcp-port` still works |
| `--version/-V` | Version, as in nono | Old wrapper `-v` remains a version alias |

Each repeated path/domain/port flag takes one literal value: commas in a path or
URL are not separators. Flags after `AGENT` belong to that agent, not nono or
the wrapper. `run` is an explicit supported subset of nono's options, not
arbitrary flag forwarding: it keeps the wrapper's workspace, executable,
profile-composition and audit behavior. In particular, the workspace-root guard
is not nono's `--workdir` variable-expansion option.

## Automatic session report

Both agent commands print a report on **stderr after every run**, including
normal exits, interrupted sessions and non-interactive commands such as
`--help`. It includes the exit status, audit ID, recorded network events
(allowed and denied), denial reasons when available, and commands for
inspecting the full audit and checking access with `nono why`.

The named-profile diagnostic inspects only the profile you select, not the
full session policy. `PROFILE` means a base profile such as
`codex-chatgpt`, an external `NONO_PROFILE` name/path, or a user profile such
as `codex-user`. User composition, runtime grants and launch-time additions
can change the actual session's access. See
[Troubleshooting](verify-and-review.md#troubleshooting) for the distinction
between user additions, base profiles and recorded activity.

For each eligible refused hostname, the report shows a persistent allowance
command, such as `nono-wrapper config opencode allow-domain api.github.com`.
Run it outside the sandbox and restart the agent. Endpoint refusals do not
suggest broadening access to the entire domain. Allow only domains the agent
needs: the suggestion is not a recommendation, and an explicit network block
or deny rule can still prevent access.

Arguments after `nono-wrapper opencode` or `codex` go to the agent. For a
temporary domain grant, put `--allow-domain` before the agent in the `run` form;
use `config AGENT allow-domain` for persistent additions. See the [network allowance
matrix](configuration.md#network-allowance-matrix) for ports, wildcards and
HTTPS endpoint rules.

Identical network events are grouped with a count. These are **audit-event
counts**, not request counts: one HTTPS request can produce separate
connection, endpoint-policy and response events. Sessions without network
events say so. Recorded `deny` events with an `I/O error` reason, such as a peer
connection reset, are transport failures rather than evidence of missing host
permission; the report does not suggest widening access for them. The original
decisions and reasons remain visible.

Native diagnostic footers and profile-save prompts are suppressed
by default: their suggestions expose the internal launcher and generated session
policy rather than a reusable wrapper command. Auditing and this report remain
enabled. To restore native diagnostics explicitly, use
`nono-wrapper run --no-diagnostics=false opencode` (or `codex`).

The wrapper assigns each run a unique name and resolves that exact session
through `nono inspect`; concurrent sessions cannot cause it to report the
most recent unrelated audit. Agent stdout remains available for pipelines and
machine-readable protocols. Interrupt and termination signals are forwarded
to nono so it can finish its session bookkeeping. Signal deaths use the
conventional `128 + signal` exit status.

If a report cannot be retrieved, the wrapper prints an unavailable message
and an audit-list command. Report queries have a five-second timeout;
reporting failures preserve the original nono exit status.

To revisit a report:

```sh
nono audit list --command opencode --recent 5
nono audit show <session-id>
```

Use `--command codex` for Codex sessions. Reports contain supervisor-observed
activity; they are not a complete log of every filesystem access. The
separate `nono-wrapper changes MARKER` helper reviews selected changed project
files; see [Check and review](verify-and-review.md).

## Profiles and login

Both agent subcommands default to **OpenAI / ChatGPT account login**. No
API-key environment variable or profile prefix is needed for that workflow.
Each agent stores its own login, so sign in once in each using the same
ChatGPT account.

The wrapper explicitly selects **our** `opencode-chatgpt` or `codex-chatgpt`
profile. Those profiles inherit the official packs and add our policy. It
also loads your optional [user configuration](configuration.md#user-configuration).
Online profiles include the shared developer network defaults and can route
[Git over SSH](configuration.md#git-over-ssh) through the proxy with an
existing SSH agent and verified host keys.
Set `NONO_PROFILE` only when you deliberately want to select another base
profile.

### First-time ChatGPT login

Login commands start the agent like any other session, so run them in an
[allowed workspace](#where-an-agent-may-start).

For OpenCode 2's **Sign in with ChatGPT** browser/token-sharing flow, start the
sandboxed foreground server and configure its client password as described
in [local-server setup](opencode.md). Use the explicit endpoint:

```sh
nono-wrapper opencode --server http://127.0.0.1:4096 \
  auth login openai --method chatgpt-token-sharing
```

After login, stop that server and remove its temporary TCP/listen grants as
described in the guide. Normal Linux sessions then use just
`nono-wrapper opencode`: OpenCode owns its server in private loopback.
The host browser cannot reach callbacks inside that private network.
The v2 instructions use modern token sharing, not the older Codex login methods.

For Codex, choose **Sign in with ChatGPT** when prompted, or run:

```sh
nono-wrapper codex login
```

Follow the browser instructions. If a browser does not open automatically,
open the printed authorization URL yourself. Complete one login before
starting the other. Afterward, launch with `nono-wrapper opencode` or
`nono-wrapper codex`. Select an OpenAI model available to your account in
OpenCode; the network profile does not select a model or change the agent's
configured provider.

The ChatGPT profiles allow `chatgpt.com` and `auth.openai.com`, limited to
the requests listed under [Network profiles](configuration.md#network-profiles).
Codex browser callbacks use TCP port 1455, with 1457 as a fallback. These are
listen-only grants, not general outbound port access. OpenCode 2.2.5 chooses
a dynamic callback port; its online server needs the explicit
`listen_port: [0]` setup described in the guide. No private namespace or
AppArmor change is needed for these explicit-server browser-login flows.

For Codex on a terminal without a local browser callback, use device-code
login, completed in a browser:

```sh
nono-wrapper codex login --device-auth
```

## Offline mode

Use upstream `NONO_PROFILE` for an explicit override:

```sh
# No network access.
env NONO_PROFILE=opencode-offline nono-wrapper opencode
env NONO_PROFILE=codex-offline nono-wrapper codex
```

The `env` form works in both Fish and Bash. Offline profiles are useful for
local commands and verification; they cannot contact ChatGPT for a model
session. User directory and file allowances still apply. nono 0.81.0 rejects
combining `network.block` with host allowances, so remove user-added hosts
before using an offline profile, for example
`nono-wrapper config opencode remove-domain api.github.com`.

On Linux, both bundled offline sessions use private loopback automatically.
Local test servers can bind dynamic TCP ports, but cannot reach host localhost.
User/network namespaces must be available; `doctor` checks this and fails
without falling back to host networking. Online Codex retains its browser-login
callback ports instead.

## Codex: one sandbox, approvals retained

Codex uses nono as its OS sandbox, keeps on-request approvals, and does not
attach to an existing shared daemon, following
[nono's documented Codex integration](https://nono.sh/docs/cli/clients/codex):

> Prefer a single sandbox layer: let nono enforce filesystem and network
> boundaries, and start Codex with its own sandbox disabled.

The wrapper supplies these options **inside nono**:

| Codex option | Purpose |
| --- | --- |
| `--sandbox=danger-full-access` | disables Codex's inner OS sandbox; nono enforces filesystem and network access |
| `--ask-for-approval=on-request` | retains Codex's approval flow when the model requests approval |
| `--no-daemon` | keeps execution in the sandboxed process tree rather than an existing host daemon |

This is the upstream-recommended mode, documented in
[nono PR #451](https://github.com/nolabs-ai/nono/pull/451). It avoids nesting
Codex's bubblewrap sandbox inside nono's Landlock restrictions on Linux,
where the required
[mount operations are prohibited](https://docs.kernel.org/userspace-api/landlock.html#filesystem-topology-modification).

`on-request` does not prompt for every command, and an approval cannot widen
nono's grants. We do not use `--dangerously-bypass-approvals-and-sandbox`,
which would also bypass the approval flow. These are launch options; the
wrapper does not rewrite the user's Codex configuration.

## What a launch prepares and refuses

Before resolving policy, the wrapper creates the agent's standard state and
cache directories, with owner-only permissions for new directories. This
prevents a cold first launch from losing declared grants because the paths
do not exist yet. It requires an absolute home/state location and refuses
paths that would expose active nono policy files. For bundled profiles, its
protected session profile also grants the exact non-default state paths;
explicit user path restrictions take precedence. External profiles selected
with `NONO_PROFILE` keep their own permission policy.

Before starting the agent, the wrapper inspects the effective filesystem
manifest, including inherited grants and launch-time additions. Writable
access to the agent, nono, the wrapper, its resolved Mise executable, or a
known Mise control/data tree is refused. This includes state symlinks,
workspace grants and `NONO_ALLOW`; read-only access is permitted.
Install managed binaries outside the writable workspace, including local
builds of the wrapper itself.
