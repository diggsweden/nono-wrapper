<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Usage

## Start an agent

Open a terminal in a project below `~/development`. If your projects live
elsewhere, set `NONO_WRAPPER_ROOT` to their absolute parent directory first
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

The agent gets read and write access to everything in its start directory,
so the wrapper starts it only **below your project root**, `~/development` by
default. It refuses the root itself, which would expose every project at
once, and any directory outside it:

```text
nono-wrapper: will not start in "/home/kim": it is outside the project root "/home/kim/development".
```

Set `NONO_WRAPPER_ROOT` to an absolute path if your projects live elsewhere.
It must not be, or contain, your home directory: agents could then start next
to your keys and settings.
A directory that groups several projects, such as `~/development/wallet`, is
fine: that is the way to let an agent work across those repositories.

**Configuration repositories need a yes.** When the start directory holds a
Git repository named `config`, or ending in `-config` or `_config`, within two
levels, including submodules and nested clones inside projects, the wrapper
lists it and asks:

```text
nono-wrapper: the start directory contains configuration repositories:
  /home/kim/development/sdg/sdg-config
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
shell. Its command interface uses [urfave/cli v3](https://cli.urfave.org/v3/).
Its commands are:

| Command | Purpose |
| --- | --- |
| `nono-wrapper opencode [arguments...]` | launch OpenCode under nono |
| `nono-wrapper codex [arguments...]` | launch Codex under nono |
| `nono-wrapper config opencode\|codex show` | list user-added allowances |
| `nono-wrapper config opencode\|codex add-host\|remove-host HOST\|URL` | manage user host and URL allowances |
| `nono-wrapper config opencode\|codex add-port\|remove-port PORT` | manage localhost port allowances |
| `nono-wrapper config opencode\|codex add-dir\|remove-dir PATH` | manage user directory allowances |
| `nono-wrapper config opencode\|codex add-file\|remove-file PATH` | manage user file allowances |
| `nono-wrapper verify [opencode\|codex ...]` | check one or both installed offline profiles |
| `nono-wrapper audit MARKER [DIRECTORY]` | report selected files changed since a marker |
| `nono-wrapper man` | print the manual, as `man nono-wrapper` shows it |
| `nono-wrapper --help` | show wrapper help |
| `nono-wrapper --version` | show embedded build and Go toolchain information |

Agent arguments are forwarded exactly: `nono-wrapper codex --help` displays
Codex's help under nono. Use `nono-wrapper help codex` for the wrapper's
description of that subcommand.

Checkout builds identify themselves as `devel`. A module version and VCS
revision are shown when the Go build includes them; help links to the project
documentation.

The `config` commands are described in [Configuration](configuration.md);
`verify` and `audit` in [Verify and review](verify-and-review.md).

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | success |
| 1 | `verify`: a check failed; `audit`: changed files are listed |
| 64 | usage error: an unknown command or flag, or wrong arguments |
| 65 | a malformed value, such as an invalid host name |
| 66 | a named file or directory does not exist |
| 70 | an unexpected failure |
| 77 | permission denied, or refused by policy |
| 78 | the user configuration is unusable |
| 125 | `opencode`, `codex`: the wrapper could not start the session |
| 127 | nono, the agent or a required tool is not on `PATH` |
| 128 + N | stopped by signal N |
| other | `opencode`, `codex`: the agent's own status |

The wrapper's own failures use the sysexits(3) values. The agent commands
follow `env` and `timeout` instead: the agent's status passes through, so the
wrapper reports its own failures there as 125 or 127. An agent can still
return those values itself. Run without a command, `nono-wrapper` shows a
short help on standard error and exits 64. `nono-wrapper --help` and
`man nono-wrapper` print the same table.

## Automatic session report

Both agent commands print a report on **stderr after every run**, including
normal exits, interrupted sessions and non-interactive commands such as
`--help`. It includes the exit status, audit ID, recorded network events
(allowed and denied), denial reasons when available, and commands for
inspecting the full audit and checking access with `nono why`.

For each refused host, the report ends with the command that allows it, such
as `To allow api.github.com: nono-wrapper config opencode add-host
api.github.com`. Allow only hosts the agent needs: the suggestion is not a
recommendation.

Identical network events are grouped with a count. These are **audit-event
counts**, not request counts: one HTTPS request can produce separate
connection, endpoint-policy and response events. Sessions without network
events say so. nono's own diagnostic footer and profile-save hints still
appear when triggered.

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
separate `nono-wrapper audit MARKER` helper reviews selected changed project
files; see [Verify and review](verify-and-review.md).

## Profiles and login

Both agent subcommands default to **OpenAI / ChatGPT account login**. No
API-key environment variable or profile prefix is needed for that workflow.
Each agent stores its own login, so sign in once in each using the same
ChatGPT account.

The wrapper explicitly selects **our** `opencode-chatgpt` or `codex-chatgpt`
profile. Those profiles inherit the official packs and add our policy. It
also loads your optional [user configuration](configuration.md#user-configuration).
Set `NONO_PROFILE` only when you deliberately want to select another base
profile.

### First-time ChatGPT login

Login commands start the agent like any other session, so run them in a
project below your [project root](#where-an-agent-may-start).

For OpenCode, select **OpenAI → ChatGPT Pro/Plus (browser)** in the
connection dialog, or start that flow directly:

```sh
nono-wrapper opencode auth login --provider openai --method "ChatGPT Pro/Plus (browser)"
```

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
Browser callbacks require TCP port 1455; Codex also supports 1457 as a
fallback. These are listen-only grants, not general outbound port access.
The grants are not interface-scoped, and the pinned OpenCode browser callback
can bind on all interfaces. Other profiles do not add these listeners.

For a terminal without a local browser callback, the agents also offer
device-code login, completed in a browser:

```sh
nono-wrapper opencode auth login --provider openai --method "ChatGPT Pro/Plus (headless)"
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
session. User directory and file allowances still apply. nono 0.80.0 rejects
combining `network.block` with host allowances, so remove user-added hosts
before using an offline profile, for example
`nono-wrapper config opencode remove-host api.github.com`.

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
