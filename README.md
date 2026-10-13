<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# nono-agent-profiles

**A small Go CLI and policy extensions for nono's official OpenCode and Codex packs.**

`nono-wrapper` launches either agent with ChatGPT host allowlists and provides
an automatic session report, profile verification and optional file-review helpers. Its command interface
uses [urfave/cli v3](https://cli.urfave.org/v3/).

```text
nolabs-ai/opencode ── opencode-base ── chatgpt / offline
                           ↑
                     agent-policy
                           ↓
nolabs-ai/codex ───── codex-base ───── chatgpt / offline
```

[nono](https://nono.sh) enforces the sandbox around the agent and its child
processes. The wrapper selects a profile, grants the resolved executable and
runs nono with the terminal connected directly. After nono exits, the wrapper
prints the session's audit summary and preserves its exit status. Codex uses nono as its OS sandbox, keeps
on-request approvals, and does not attach to an existing shared daemon,
following [nono's Codex guidance](https://nono.sh/docs/cli/clients/codex).

**New here?** Follow [Install](#install), then [Use](#use) to sign in and start
an agent. To work on this repository, see [Develop and test](#develop-and-test).

## What changes from the official packs

| Area | Our extension |
| --- | --- |
| Network | ChatGPT host lists by default; explicit offline profiles |
| Browser login | TCP callback listeners on 1455, plus Codex's fallback 1457 |
| Agent environment | nono's `environment.allow_vars` keeps the core, editor and toolchain-location variables and excludes unrelated credentials |
| Linux pathname Unix sockets | mediation enabled; sockets require explicit grants, and the wrapper grants the SSH agent's |
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
an intentional policy difference or a compatibility fix still missing upstream.
The certificate grant can go once upstream covers openSUSE's certificate path.

On top of that, `agent-policy` adds read access through nono's own groups:

- `/tmp` on Linux, so temporary files can be read back. Other programs' files
  there become readable to the agent as well.
- mise's data and configuration (`~/.local/share/mise`, `~/.config/mise`),
  where mise-installed tools live. A mise configuration that holds secrets in
  its `[env]` section exposes them.
- Go and Java locations: `~/go`, the Go build cache, `~/.m2` and `~/.gradle`
  caches and wrappers.

It also grants `~/.config/git`, `~/.config/delta` and
`~/.config/ssh/allowed_signers` read-only, because Git stops with an error
when a file its configuration includes cannot be read.

### How inheritance combines policy

With nono 0.79.0, filesystem grants, `network.allow_domain` and
`environment.allow_vars` accumulate through inheritance. A shorter or empty
local list does not remove upstream entries. `network.block: true` is sticky:
an extending profile cannot turn networking back on with `false`.

The wrapper gives explicit user host and exact-path rules precedence over matching rules in
its bundled profiles, as described under [user configuration](#user-configuration-extra-hosts-directories-and-files).
Other inheritance remains native nono behavior.

The pinned official packs supply no provider host allowlists or environment
allowlists, so those lists currently come from these extensions. Review both
packs and nono's defaults when upgrading, and run the network checks to catch
changes to the effective policy. Linux pathname-socket mediation is an explicit
local setting; upstream socket grants are still inherited.

## Install

Prerequisites:

- Linux with Git, Bash, `/bin/sh`, curl and coreutils.
- [mise](https://mise.jdx.dev/) installed and activated in your shell.
- Your user command directory (`~/.local/bin` by default) on `PATH`.
- An internet connection for setup and a ChatGPT account with access to the
  agent you want to use.

**Tested on Linux x86_64 only. macOS has not been tested yet. WSL2 starts with
reduced protection; see [What the boundary means](#what-the-boundary-means).**

Run these steps as your normal user from this repository's checkout.

### 1. Install the tools

```sh
mise install --locked nono opencode codex go just aqua:koalaman/shellcheck
```

This installs the pinned tools and Go dependencies. `--locked` accepts only
the versions and checksums recorded in `mise.lock`; naming the tools keeps
that check away from tools in your global mise configuration, which have no
entries there. If mise asks for trust, review `.mise.toml` and run
`mise trust` before retrying.

Make nono and the agents available in other project directories:

```sh
mise use -g nono@0.79.0 opencode@1.18.35 codex@rust-v0.161.0
```

### 2. Install the wrapper and profiles

```sh
just install-local
```

This builds the wrapper, installs the signed official packs, bundled
profiles and the manual, and checks the profiles' inheritance. Default
installation paths:

```text
~/.local/bin/nono-wrapper
~/.config/nono/profiles/*.jsonc
~/.local/share/man/man1/nono-wrapper.1
```

The packs also install their agent integrations, including a marked block in
`~/.codex/config.toml`. Reinstalling updates bundled files and preserves your
optional `opencode-user.jsonc` and `codex-user.jsonc`.

For custom paths, set `INSTALL_DIR` or `XDG_BIN_HOME` for the binary,
`XDG_CONFIG_HOME` for profiles and `XDG_DATA_HOME` for the manual. Use absolute paths and keep the binary directory
on `PATH`.

### 3. Check the installation

```sh
nono-wrapper verify
```

Expect `ok` for each check, then [launch an agent and sign in](#use).
Rerun `just install-local` after changing the Go code or bundled profiles.

### Pinned versions and platform support

| Component | Version |
| --- | --- |
| nono | 0.79.0 |
| OpenCode CLI | 1.18.35 |
| Official OpenCode pack | 0.2.0 |
| Codex CLI | 0.161.0 |
| Official Codex pack | 0.2.3 |

**OpenCode pack 0.3.0 adds `--standalone` for OpenCode v2.** The pinned
OpenCode v1 CLI does not accept that flag. Keep the CLI and pack versions
paired; `just install-local` installs and pins the compatible pack versions.

`mise.lock` includes Linux and macOS downloads for x64 and ARM64; macOS support
is currently untested. The OpenCode pack's first-run hook creates its required
state directories. Standalone native agent executables are tested on Linux;
package-manager launchers can need additional runtime grants.

## Use

Open a terminal in the project you want the agent to work on. For example,
replace `/path/to/your/project` below with your project's directory:

```sh
cd /path/to/your/project
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
The wrapper is installed for your user, so you can run it outside the
`nono-agent-profiles` checkout. The `just` recipes belong to that checkout.

`nono-wrapper` is a native executable and works from Fish, Bash or another
shell. Its commands are:

| Command | Purpose |
| --- | --- |
| `nono-wrapper opencode [arguments...]` | launch OpenCode under nono |
| `nono-wrapper codex [arguments...]` | launch Codex under nono |
| `nono-wrapper config opencode\|codex show` | list user-added allowances |
| `nono-wrapper config opencode\|codex add-host\|remove-host HOST` | manage user host allowances |
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

If the agent on `PATH` is a mise shim, the wrapper asks `mise which` for the
real executable and grants that file. This happens in shells where mise is not
activated, such as scripts and editors. When mise cannot resolve the agent in
the current directory, the wrapper stops and shows mise's message.

### Exit status

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

The wrapper's own failures use the sysexits(3) values. The agent commands follow
`env` and `timeout` instead: the agent's status passes through, so the wrapper
reports its own failures there as 125 or 127. An agent can still return those
values itself. Run without a command, `nono-wrapper` shows a short help on
standard error and exits 64. `nono-wrapper --help` and `man nono-wrapper` print
the same table.

### Automatic session report

Both agent commands print a report on **stderr after every run**, including
normal exits, interrupted sessions and non-interactive commands such as `--help`.
It includes the exit status, audit ID, recorded network events (allowed and
denied), denial reasons when available, and commands for inspecting the full
audit and checking access with `nono why`.

Identical network events are grouped with a count. These are **audit-event
counts**, not request counts: one HTTPS request can produce separate connection,
endpoint-policy and response events. Sessions without network events say so.
nono's own diagnostic footer and profile-save hints still appear when triggered.

The wrapper assigns each run a unique name and resolves that exact session
through `nono inspect`; concurrent sessions cannot cause it to report the most
recent unrelated audit. Agent stdout remains available for pipelines and
machine-readable protocols. Interrupt and termination signals are forwarded to
nono so it can finish its session bookkeeping. Signal deaths use the conventional
`128 + signal` exit status.

If a report cannot be retrieved, the wrapper prints an unavailable message and
an audit-list command. Report queries have a five-second timeout; reporting
failures preserve the original nono exit status.

To revisit a report:

```sh
nono audit list --command opencode --recent 5
nono audit show <session-id>
```

Use `--command codex` for Codex sessions. Reports contain supervisor-observed
activity; they are not a complete log of every filesystem access. The separate
`nono-wrapper audit MARKER` helper below reviews selected changed project files.

### Agent configuration

Checkout builds identify themselves as `devel`. A module version and VCS
revision are shown when the Go build includes them; help links to the project
documentation.

Both agent subcommands default to **OpenAI / ChatGPT account login**. No API-key
environment variable or profile prefix is needed for that workflow. Each agent
stores its own login, so sign in once in each using the same ChatGPT account.

The wrapper explicitly selects **our** `opencode-chatgpt` or `codex-chatgpt`
profile. Those profiles inherit the official packs and add our policy. It also
loads your optional [user configuration](#user-configuration-extra-hosts-directories-and-files).
Set `NONO_PROFILE` only when you deliberately want to select another base profile.

### First-time ChatGPT login

For OpenCode, select **OpenAI → ChatGPT Pro/Plus (browser)** in the connection
dialog, or start that flow directly:

```sh
nono-wrapper opencode auth login --provider openai --method "ChatGPT Pro/Plus (browser)"
```

For Codex, choose **Sign in with ChatGPT** when prompted, or run:

```sh
nono-wrapper codex login
```

Follow the browser instructions. If a browser does not open automatically,
open the printed authorization URL yourself. Complete one login before starting
the other. Afterward, launch with `nono-wrapper opencode` or `nono-wrapper codex`.
Select an OpenAI model available to your account in OpenCode; the network
profile does not select a model or change the agent's configured provider.

The ChatGPT profiles allow `chatgpt.com` and `auth.openai.com`, limited to the
requests listed under [Network profiles](#network-profiles). Browser callbacks require TCP port 1455; Codex also supports 1457
as a fallback. These are listen-only grants, not general outbound port access.
The grants are not interface-scoped, and the pinned OpenCode browser callback
can bind on all interfaces. Other profiles do not add these listeners.

For a terminal without a local browser callback, the agents also offer
device-code login, completed in a browser:

```sh
nono-wrapper opencode auth login --provider openai --method "ChatGPT Pro/Plus (headless)"
nono-wrapper codex login --device-auth
```

### Offline mode

Use upstream `NONO_PROFILE` for an explicit override:

```sh
# No network access.
env NONO_PROFILE=opencode-offline nono-wrapper opencode
env NONO_PROFILE=codex-offline nono-wrapper codex
```

The `env` form works in both Fish and Bash. Offline profiles are useful for
local commands and verification; they cannot contact ChatGPT for a model session.
User directory and file allowances still apply. nono 0.79.0 rejects combining
`network.block` with host allowances, so remove user-added hosts before using an
offline profile, for example `nono-wrapper config opencode remove-host api.github.com`.

### Codex: one sandbox, approvals retained

We follow [nono's documented Codex integration](https://nono.sh/docs/cli/clients/codex):

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
Codex's bubblewrap sandbox inside nono's Landlock restrictions on Linux, where
the required [mount operations are prohibited](https://docs.kernel.org/userspace-api/landlock.html#filesystem-topology-modification).

`on-request` does not prompt for every command, and an approval cannot widen
nono's grants. We do not use `--dangerously-bypass-approvals-and-sandbox`, which
would also bypass the approval flow. These are launch options; the wrapper
does not rewrite the user's Codex configuration.

### Network profiles

| Profile | Allowed network hosts | Additional inherited variables |
| --- | --- | --- |
| `opencode-chatgpt` (default) | `chatgpt.com`, `auth.openai.com` | none |
| `opencode-offline` | none | none |
| `codex-offline` | none | `CODEX_HOME` |
| `codex-chatgpt` (default) | `chatgpt.com`, `auth.openai.com` | `CODEX_HOME` |

The allowlists are in `profiles/opencode-chatgpt.jsonc` and
`profiles/codex-chatgpt.jsonc`. Most entries carry endpoint rules, so nono
intercepts TLS for that host and refuses any other HTTPS method or path:

| Host | Allowed HTTPS requests |
| --- | --- |
| `chatgpt.com` (OpenCode) | `POST /backend-api/codex/responses` |
| `chatgpt.com` (Codex) | whole host |
| `auth.openai.com` | `POST /oauth/token`, `POST /api/accounts/deviceauth/usercode`, `POST /api/accounts/deviceauth/token`; Codex also `POST /oauth/revoke` |

```jsonc
{ "domain": "auth.openai.com", "endpoints": [{ "method": "POST", "path": "/oauth/token" }] }
```

Codex keeps the whole `chatgpt.com` host because it streams responses over a
WebSocket, and nono 0.79.0 refuses WebSocket upgrades on hosts with endpoint
rules. The paths come from the pinned agent executables; review them when
upgrading either CLI. An agent request that nono refuses receives a 403 with
the body `{"error":"Forbidden"}`.

The pinned OpenCode fetches its model catalogue from `models.opencode.ai`,
which is not listed, so it uses the catalogue bundled with the executable. To
refresh it, extend the profile with
`{ "domain": "models.opencode.ai", "endpoints": [{ "method": "GET", "path": "/api.json" }] }`;
a slow reply from that host can delay OpenCode's startup.

To update the bundled profiles from this checkout, edit the repository files,
rerun `just install-local`, and start a new agent session to use the updated
installed copies in `~/.config/nono/profiles/`
(or `$XDG_CONFIG_HOME/nono/profiles/`).

The host lists constrain connections from the sandbox. Upstream host
integrations, such as delegated URL opening, retain their own upstream
policies. Package registries, Git hosts and remote MCP destinations are not
added to the network list automatically. Git over SSH does not work through
the HTTP proxy; use HTTPS with an appropriate host grant.

### User configuration: extra hosts, directories and files

Manage your optional user configuration from your normal terminal, outside an
agent session. It lives alongside the installed profiles:

| Agent | User configuration file |
| --- | --- |
| OpenCode | `~/.config/nono/profiles/opencode-user.jsonc` |
| Codex | `~/.config/nono/profiles/codex-user.jsonc` |

If you set `XDG_CONFIG_HOME`, use `$XDG_CONFIG_HOME/nono/profiles/` instead.
**`just install-local` preserves these user files**, including on reinstall.

#### Manage allowances from the CLI

The CLI creates and edits the user file for you. Use `opencode` or `codex` to
choose which agent's allowances to manage. Replace `/path/to/...` with existing
directories and files:

```sh
nono-wrapper config opencode add-host api.github.com
nono-wrapper config opencode add-dir /path/to/reference
nono-wrapper config opencode add-dir --read-write /path/to/shared-project
nono-wrapper config opencode add-file /path/to/settings.toml
nono-wrapper config opencode add-file --read-write /path/to/output.txt
nono-wrapper config opencode show
```

Directories and files are **read-only by default**; `--read-only` makes that
explicit, and `--read-write` allows changes. Adding the same path with a
different mode updates its user allowance and replaces matching bundled path
grants at launch. Paths must exist when adding them;
relative paths are stored as absolute paths. Hosts are plain names such as
`api.github.com`, without `https://`, a path or `:443`.

Remove allowances with:

```sh
nono-wrapper config opencode remove-host api.github.com
nono-wrapper config opencode remove-dir /path/to/reference
nono-wrapper config opencode remove-file /path/to/settings.toml
```

Removing a directory or file clears that exact path's user-added read, write
and read/write entries, restoring any matching bundled grant. It also works
after the path has been deleted.
**Removal does not revoke access inherited from another profile or a broader
directory grant.** `show` lists user allowances only.

Edits preserve JSONC comments and unrelated settings, validate with nono, and
replace the file atomically. Repeating an existing allowance is a no-op.
`add-host` means whole-host access: it replaces endpoint-restricted entries for
that host in the user file. Removing a host removes its user entries, including
endpoint rules.

#### User precedence and rule protection

For the bundled profiles, user entries take precedence for matching resources:

| Resource | Matching rule |
| --- | --- |
| Host | The user rule replaces bundled `allow_domain` rules for the same hostname or wildcard pattern. |
| Directory | The user's access mode replaces bundled read, write and read/write grants for the same canonical path. |
| File | The user's access mode replaces bundled grants for the same canonical file path, including grants written in the general filesystem lists. |

Distinct hosts and paths keep their existing rules.
For example, `add-host chatgpt.com` overrides OpenCode's bundled endpoint
restriction for that host. Removing the user entry restores the bundled rule.
Host comparisons ignore case and a trailing dot; different wildcard patterns
remain separate rules.

For paths, profile variables such as `$HOME` and `$XDG_CONFIG_HOME` are expanded
and symlinks are resolved before comparison. Existing objects and ancestors are
also compared by filesystem identity, so case aliases on APFS and hard-link
aliases cannot bypass path matching or rule-file protection. Filesystem override entries must
be literal paths. A manually edited user profile must specify one access mode
per canonical path; conflicting modes are rejected.

For example, if a bundled profile grants read/write access to an existing
directory, this requests read-only access instead:

```sh
nono-wrapper config opencode add-dir --read-only /path/to/reference
```

**A broader grant can prevent that restriction from being enforced.** A
read-only file inside the writable workspace, or a read-only directory still
covered by a pack/group write grant, cannot be made read-only just by adding
another grant. The wrapper checks the effective filesystem grants, including
workspace and `NONO_ALLOW` additions, and refuses startup with the conflicting
path when the requested mode cannot be honored. Narrow or remove the conflicting
grant, or remove that user override. It never silently starts with broader
access than the requested mode. Signed pack definitions are not rewritten.

nono itself combines host and filesystem grants, so changing inheritance order
alone is not enough. For these overrides, the wrapper makes private copies of
the bundled profiles, removes matching bundled entries, and lets nono
resolve the copies with the user profile. Other fields and official-pack
references are preserved. The copies live under the read-only profiles area
and are removed when the session exits. A cleanup failure reports the retained
directory on stderr while preserving the session's exit status. External profiles selected with
`NONO_PROFILE` retain native nono inheritance; an unresolved conflict outside
the bundled profiles stops override composition rather than silently claiming
that the user rule won.

**The running agent can recommend changes, but cannot apply them to its active
profiles.** The bundled profiles give it read-only access to the profiles and
package store, and write access to `nono/profile-drafts` for proposals. Apply
changes yourself from a normal terminal. Config-editing commands reject agent
sessions and refuse write grants that expose active rules, including through
symlinks, hard links or parent directories. A live profile's backing file is
protected even when a dotfile symlink stores it outside the profiles directory.
The wrapper also refuses to share a working
directory containing its rule files. Manual user filesystem overrides that
expose active rules are also rejected. Filesystem enforcement blocks direct
edits even if the agent clears its session environment variables.

#### Edit the file directly

For example, put this in `opencode-user.jsonc` to allow the GitHub API and extra
directories. Replace the directory examples with existing directories you need,
or omit `filesystem` if you only want the API grant:

```jsonc
{
  "network": {
    "allow_domain": ["api.github.com"]
  },
  "filesystem": {
    "read": ["$HOME/reference"],       // Read-only directory access.
    "allow": ["$HOME/shared-project"] // Read and write directory access.
  }
}
```

The same format works in `codex-user.jsonc`. Add more hosts or directories to
the arrays as needed. Directory grants cover their contents recursively;
`read_file` and `allow_file` grant access to individual files instead.

The wrapper automatically applies the corresponding user profile. No `extends`
field or `NONO_PROFILE` setting is needed in this example. Existing ChatGPT,
authentication and filesystem grants are kept, except for matching host/path rules
explicitly overridden as described above.
The user file also applies when you select a base profile with `NONO_PROFILE`.
With nono 0.79.0, an offline base profile combined with user host allowances
refuses to start; remove those host entries to use the remaining filesystem
allowances offline. User additions cannot turn off the base profile's network block.

Inspect the user entries and start a session:

```sh
nono-wrapper config opencode show
nono-wrapper opencode
```

Inside the running session, `nono why --self --host api.github.com` checks the
actual sandbox's host access. Use `nono why --self --path "$HOME/reference"
--op read` for a path. A standalone `nono why --profile ... --extends ...` uses
native additive inheritance and does not reproduce the wrapper's host/path overrides.

Edits take effect on the next session, with no rebuild or reinstall. Invalid
user profiles stop startup and report the error. Remove the user file to stop
applying its additions. `nono-wrapper verify` checks the bundled offline
profiles, independently of these user additions.

### Settings and custom extensions

| Setting | Meaning | Default |
| --- | --- | --- |
| `NONO_PROFILE` | base profile to select; user additions still apply | `opencode-chatgpt` or `codex-chatgpt` |
| `NONO_WRAPPER_SSH_AGENT` | `0` withholds the SSH agent socket | granted when `SSH_AUTH_SOCK` names a socket |

Normal nono environment settings, including `NONO_THEME`, `NONO_ALLOW` and
`NONO_ALLOW_DOMAIN`, are respected. They can add to the selected policy.

Behind a mandatory outbound proxy, nono must chain through it, and it does not
read `HTTPS_PROXY` itself. The wrapper passes the host and port from
`HTTPS_PROXY` or `HTTP_PROXY` to nono as its upstream proxy, unless the
selected profile blocks the network. `NONO_UPSTREAM_PROXY=host:port` takes
precedence and is handled by nono itself.

Automatic import accepts an unauthenticated `http://host:port` URL or bare
`host:port`; HTTP URLs may omit the port to use 80. IPv6 addresses use brackets.
HTTPS/SOCKS proxy transports, credentials, paths other than `/`, query strings
and invalid ports are rejected. A configured proxy that cannot be imported,
or a failed profile inspection, stops startup instead of using a direct route.
The proxy URL is never printed in diagnostics. `NO_PROXY` is not translated:
use `NONO_UPSTREAM_BYPASS` for hosts that must be reached directly.

To select a separately named configuration, create an extending nono profile:

```json
{
  "extends": "opencode-chatgpt",
  "meta": { "name": "my-opencode" },
  "network": { "allow_domain": ["github.com"] },
  "environment": { "allow_vars": ["MY_SERVICE_TOKEN"] }
}
```

To compose additional profiles, use an array such as
`"extends": ["opencode-chatgpt", "my-tool-access"]` in that named profile.

Then run `env NONO_PROFILE=my-opencode nono-wrapper opencode`. Core, terminal,
editor, locale, XDG, `TMPDIR`, `SSH_AUTH_SOCK` and toolchain-location
(`GOPATH`, `GOROOT`, `JAVA_HOME`, `CARGO_HOME`, `RUSTUP_HOME`) variables are
listed in `profiles/agent-policy.jsonc`;
nono handles its own injected proxy variables and credentials.

### Signing commits through an SSH agent

When `SSH_AUTH_SOCK` names a socket, the wrapper grants that one socket so the
agent can sign commits with Git's SSH signing. Repository Git settings do not
add host-file permissions. Use Git's inline public-key form so signing needs
the socket without an additional key-file grant.

In the project whose commits you want to sign, set `user.signingkey` to `key::`
followed by the full public-key line for a key already loaded in your agent.
`ssh-add -L` lists those public keys. Replace the placeholders in this example:

```sh
git config gpg.format ssh
git config user.signingkey 'key::ssh-ed25519 <base64-public-key> <comment>'
```

Then `git commit -S` can use the agent for signing. This is standard
[Git SSH-signing configuration](https://git-scm.com/docs/git-config), and works
without granting access to private-key files. Other sockets still need explicit
grants.

Anything in the sandbox can then ask the SSH agent to sign with every key it
holds. Load only the signing key, require confirmation with `ssh-add -c`, or
turn the grant off:

```sh
env NONO_WRAPPER_SSH_AGENT=0 nono-wrapper codex
```

This does not enable pushing over SSH, which needs a direct connection the
sandbox refuses. Running `nono` directly, as `nono-wrapper verify` does, adds
no such grant.

The pinned upstream filesystem rules use standard home-directory locations.
If you relocate agent state with XDG variables or `CODEX_HOME`, add the
corresponding path grant in your own profile. Forwarding a location variable
does not itself grant filesystem access. Likewise, an agent's `--cd` or
`--add-dir` argument cannot expand nono's grants.

## Project layout

```text
cmd/nono-wrapper/   Go CLI: launch, config, verify, audit and man
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

Select a ChatGPT/offline profile, not `agent-policy` or either `*-base`
composition profile: those do not restrict the network on their own.

## What the boundary means

- The agent and its local subprocesses run under nono. Codex's inner OS sandbox
  is disabled, with on-request approvals retained as described above. nono is
  the enforcement boundary for Codex and its commands.
- Project access includes `.git` and project secrets. On Linux, nono cannot
  carve a hidden or read-only exception out of that read/write grant.
- The inherited agent-state, cache and runtime grants expose their contents,
  including any credentials stored there. State is shared across projects.
- Host allowlists limit destinations, not uploads. Endpoint rules narrow a
  host to listed HTTPS methods and paths and make nono validate its
  certificate. They do not inspect query strings, headers or bodies, and do
  not tie a request to your account: an allowed endpoint accepts data sent
  with any credentials.
- Plain HTTP to a listed host bypasses its endpoint rules; nono 0.79.0 has no
  HTTPS-only setting. String-only rules, such as Codex's `chatgpt.com`, tunnel
  encrypted traffic without inspecting it. A listed hostname resolving to a
  private/local address is not rejected.
- Linux pathname socket access requires explicit `filesystem.unix_socket*`
  grants. Ordinary directory access does not authorize sockets there.
- Running plain `opencode` or `codex` bypasses the wrapper. Explicitly
  permitted remote execution services are outside the local sandbox.

These profiles target Linux. macOS has not been validated.
The Go wrapper has Darwin timestamp support and native unit tests, including
filesystem-identity checks; these do not establish Seatbelt or agent runtime
compatibility. Validate those on an actual Mac before relying on the sandbox.

## Verify and review

After installing or upgrading:

```sh
nono-wrapper verify          # both installed offline profiles
nono-wrapper verify codex
```

This runs probe commands under the profiles without an agent login or model.
It checks successful project/temp writes alongside refused home/config
listing and networking. The configuration check uses `XDG_CONFIG_HOME`, falling
back to `~/.config`. It uses a private temporary directory and stops checking
a profile if a positive control fails. The full container suite also tests
the wrapper, representative inherited grants and ChatGPT
allowlists.

Use nono's [session audit](https://nono.sh/docs/cli/features/audit) for recorded
activity and its [rollback features](https://nono.sh/docs/cli/features/atomic-rollbacks)
for snapshot-based change review and restoration. Enable snapshots through
`nono run --rollback ...`; `.git` is excluded by default, and
`--rollback-include .git` explicitly includes it.

For lightweight reminders, `nono-wrapper audit MARKER [DIRECTORY]` reports
changed project hooks/configuration, editor tasks, CI workflows, package scripts
and agent settings for review before later host execution. This includes mise's
shared/local configuration and lockfiles.

Create a marker before your session and run the helper afterwards. This block
works from Fish or Bash:

```sh
sh -c '
marker=$(mktemp)
nono-wrapper codex
nono-wrapper audit "$marker" .
rm -f "$marker"
'
```

This is a timestamp-based aid, not enforcement or a complete change log. It
does not report all global state changes, restore files or detect deletions.
It compares file change times (ctime) with the marker's modification time,
and exits 1 when selected changes are listed, 0 when none are found, or 2 when
the check could not run.
Reported filenames are quoted, with control characters and newlines escaped.
For worktrees and `.git` or `.git/hooks` symlinks, it reports pointer changes
without following the pointer to shared Git metadata, and scans project files.
The automatic session report describes nono's recorded activity. This selected-file
review and marker cleanup remain separate manual steps.

## Develop and test

For native macOS development checks, use Bash 5+ from MacPorts
(`sudo port install bash`) and mise for the repository-pinned tools.
Put `/opt/local/bin` before `/bin` on `PATH`, start `/opt/local/bin/bash`, and
confirm `bash --version` reports 5 or newer. Installing it does not replace
Apple's `/bin/bash`. The macOS sandbox/runtime validation limits above still
apply.

Run development commands from the `nono-agent-profiles` checkout:

```sh
just install-dev-tools   # prepare pinned tools and Go dependencies
just                     # list available recipes
just build               # build .cache/bin/nono-wrapper
just fmt                 # format Go source and tests
just verify              # lint and unit tests; no containers or network
```

`just verify` is the everyday check before submitting a change. `just lint`
and `just test-unit` also work separately. Use `mise exec -- just verify` when
running from a shell without mise activation. Local checks run offline after
the dependencies have been prepared. `just build` produces the host executable;
`just install-local` rebuilds it, installs and pins the official packs,
installs our profiles, and checks their inheritance.

Commit `.mise.toml` and `mise.lock` together when updating host tools. Keep
the nono, OpenCode and Codex versions aligned with the Linux container pins in
`test/tools`, regenerate the lockfile with `mise lock`, and review official
pack compatibility in `test/packs`. Host and container archive checksums can
differ because they use different builds of the same release. Commit `go.mod`
and `go.sum` together when updating the Go dependencies.

`.gitignore` excludes generated builds and test artifacts in `.cache/` and
machine-local mise overrides (`.mise.local.toml`, `mise.local.toml`, `mise.local.lock`).
Shared mise pins and the Go source in `cmd/` are tracked.

### Local tests

`just test-unit` covers auditing, verification and the probe helper, plus
native CLI and installation/cleanup contracts. CLI tests build the
real wrapper and run it in isolated temporary directories with fake nono/agent
executables. They verify exact arguments (including agent help), profile
selection, executable canonicalization, terminal descriptors, signals, exit
statuses, setup hints and session-specific post-exit reports. Reporting tests
cover clean exits with network denials, repeated events, missing or invalid
audits, lookup timeouts and terminal-control escaping. Verification tests exercise success/failure and
interrupted cleanup; audit tests cover selected paths, symlinks and change
times. Recipe tests build and install the real binary into temporary homes,
use fake nono to check pinned pack setup, profile selection and failure handling,
and use fake Podman to check bounded cleanup and file preservation.
Configuration tests cover host/directory/file editing, access-mode updates,
JSONC preservation, failed and interrupted writes, symlinks, locking and user
file preservation across reinstalls. Host/path-override tests check that unrelated
policy fields survive and generated files are cleaned up on success and failure.
Preparation tests use local archive fixtures and fake download/build commands
to check cache integrity, refreshes, failure handling and temporary cleanup.
Image-input tests cover changed build recipes and selected base-image pins.
Runner tests ensure missing or stale images never trigger preparation.
Go, just, Bash, `/bin/sh` and standard file utilities are needed; agents,
installed profiles and Podman are not needed for these unit tests.

These local checks run on the host with temporary homes and explicit subprocess
environments/tool paths. That is fixture isolation; the real-agent integration
tier below supplies OS-level confinement. Go uses its configured build and
module caches. Run native checks on trusted checkouts and use an unprivileged,
secretless environment for automation.

Extra arguments are forwarded to `go test`, so you can select tests and enable
verbose output:

```sh
just test-unit -run TestLauncherInvocation -v
```

`just test` runs those unit tests followed by the routine container suite;
`just test-net` also enables the container networking checks. The explicit
`test-containers` recipes run only the integration suite.

### Container integration

Tests that launch real agents run in disposable rootless Podman containers.
Install Podman through your system package manager and configure rootless
containers before using these recipes; mise installs the host tools above.
Each image contains the compiled `nono-wrapper`, checksum-pinned nono,
OpenCode and Codex executables (`test/tools`), plus the signed official packs
installed and pinned by nono (`test/packs`).
The six Linux x86_64 release-matrix images are pinned in `test/images`.
Preparation and execution require a Linux x86_64 controller and refuse other
hosts before building, downloading or contacting Podman. On macOS, run this
integration tier inside a Linux x86_64 VM; use `just verify` for native host
checks. Linux VM/container results are not macOS sandbox results.
Ctrl-C stops a run: the runner removes the current container and exits with
status 130.
The `just` recipes default to **Ubuntu 26.04 and openSUSE Leap 16** for routine
checks, including openSUSE's certificate compatibility. Pass `all` for the full
release matrix. The suite focuses on local policy and representative runtime,
Git and agent-state integration rather than enumerating every upstream grant.
It also checks host/file/directory precedence, read-only and writable user grants, removal,
and the agent's inability to edit active or generated policies while still being
able to write a proposal in `profile-drafts`.

Preparation caches the original tool archives in `.cache/tools/`. Every run
copies them into a private temporary directory, verifies the pinned SHA-256
checksums, and extracts fresh executables for the image build. Stale or corrupt
archives are downloaded again and verified; extracted cache files and checksum
markers are never trusted as build inputs. Private build inputs are removed
after preparation. Wrapper/probe-only edits can reuse the image's pack-install
layer.

Each image records its selected base-image reference and checksums for the
Containerfile, dependency and pack installation scripts, preparation,
fingerprint and controller-guard scripts, and `.mise.toml`/`mise.lock`.
The runner hashes those inputs as the caller with `sha256sum` or `shasum -a 256`
and supplies a read-only expected record to the container, so
owner-only configuration files can keep their permissions. Startup checks the
records before testing the agents. A pin change for another distribution does
not invalidate this image.

The probe helper (`test/probe.go`) uses only Go's standard library. Image
preparation requires Go 1.27.2 or newer on the host and cross-compiles a static
Linux x86_64 `nono-wrapper` and `agent-probe`; Go is not needed inside the
containers. The helper owns the Codex app-server exchange, browser-login callback checks, socket
probes and installed-pack validation. The container entry point rejects a
wrapper or helper built from an older source revision, including changes to
the wrapper's Go dependency manifests.

Tests use the image's `/home/tester`, where the packs were installed, so the
upstream plugin paths remain valid. The checkout is mounted read-only; no
host home or credentials are mounted. Test scripts refuse direct host runs.
Supervisor update hints are disabled during tests because the versions are
intentionally pinned, including for offline runs.

| Command | What it does | Network |
| --- | --- | --- |
| `just install-dev-tools` | install the pinned host tools through mise | yes |
| `just build` | build the host wrapper in `.cache/bin/` | only if Go dependencies are missing |
| `just install-local` | build/install the wrapper, pin official packs, install and check our profiles | yes |
| `just verify` | lint and unit tests | no |
| `just fmt` | format Go source and tests with gofmt | no |
| `just lint` | ShellCheck, Go formatting and go vet | no |
| `just test-unit` | unit and native CLI/installer contract checks | no |
| `just test-prepare` | build the routine images with the wrapper, pinned tools and official packs | yes |
| `just test-containers` | real profile/launcher integration, representative inherited grants, OpenCode startup and Codex tool execution | no |
| `just test-containers-net` | the same plus positive/negative ChatGPT-host checks | yes |
| `just test` | unit tests followed by the routine container suite | no |
| `just test-net` | unit tests followed by the network-enabled routine container suite | yes |
| `just test-clean` | remove known builds/cached tools and this project's test-image tags | no |

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

Container startup checks the installed tool/pack versions and manifests.
The container recipes consume prepared images only. Missing or stale images
fail with a `just test-prepare` hint; neither the offline nor network-enabled
runner builds images or downloads dependencies. Run preparation explicitly
after changing image inputs, tool/pack pins, or compiled wrapper/probe sources.
Preparation may need network access when dependencies, archives or image layers
are missing from caches. Profile files are copied from the checkout when tests
start, so profile-only edits do not require rebuilding an image.
Upgrade CLI and pack pins together when compatibility changes.

`just test-clean` is anchored to this checkout, even when invoked from another
working directory. It refuses symlinked `.cache`, `.cache/tools` and `.cache/bin`
directories, deletes only the known generated files, and removes directories only when
empty. Extra files are retained. Image cleanup targets only
`localhost/nono-agent-profiles:*` tags without force-removal or parent pruning.
The recipe performs no recursive filesystem deletion.

The Codex probe uses its real app server to execute a command with
`dangerFullAccess`, matching the single-sandbox mode, without a login or model.
It checks project, `.git` and temporary writes alongside refused home-file reads,
home writes and networking, so the denials must come from nono. Launcher unit tests
also verify the on-request approval flag and absence of the blanket bypass.
Browser-login checks start each real agent's authorization flow and cancel it
through its local callback server. They verify startup and callback access,
without signing in or exchanging tokens. Separate port probes check Codex's
fallback port and refusal of unrelated listeners.

The filesystem probes target locations the container user can otherwise
access. Network-enabled tests include successful requests, so a container
with no network cannot pass merely because every request fails.
Endpoint refusals require nono's explicit 403 response (or a refused proxy
CONNECT). DNS, timeout, TLS and gateway errors fail the request check. Responses
are limited to 1 MiB, and local tests exercise these outcomes using fake curl.

With the pinned versions above, Ubuntu 26.04 and openSUSE Leap 16 passed the
full network-enabled suite, including real Codex tool execution and every
ChatGPT hostname check.

Interactive TUIs and approval prompts, completed OAuth/token exchange,
authenticated model sessions and macOS need separate validation. These tests
cover CLI startup, actual tool execution and the selected policy; they do not
establish every agent workflow.

## Background

The policy choices originated in
[occwrap](https://codefloe.com/itiquette/occwrap). This repository now extends
the official nono agent packs rather than maintaining replacement filesystem
profiles.
