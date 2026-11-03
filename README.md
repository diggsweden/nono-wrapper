<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# nono-wrapper

Run OpenCode and Codex inside the [nono](https://nono.sh) sandbox.
`nono-wrapper` extends nono's official agent packs with ChatGPT login,
developer network defaults and a report of each session.

- Sign in with your ChatGPT account; online profiles include common forges,
  reference sites and vulnerability-data services.
- Use Git over SSH through nono's proxy with your existing SSH agent and
  verified host keys; no extra PAT is needed for that workflow.
- Add hosts, directories and files per agent with `nono-wrapper config`. The
  running agent cannot change its own rules.

Linux is the primary platform. See the [platform notes](docs/install.md#platform-notes)
for macOS and WSL2. BSD is out of scope: the wrapper builds only for Linux
and macOS.

## Quick Start

Install OpenCode and/or Codex through DevBase or your preferred installer.
The agent must be on `PATH`; this checkout installs the wrapper, not the
agents. You also need [mise](https://mise.jdx.dev/getting-started.html)
activated in your shell and `~/.local/bin` on `PATH`.

Follow [Install](docs/install.md) for the prerequisites and installation
steps. From the checkout, the short version is:

```sh
mise install --locked nono go just aqua:koalaman/shellcheck
mise use -g nono@0.80.0
just install-local
nono-wrapper doctor codex
```

Use `doctor opencode` if that is your agent, or `doctor` to check both.
It checks prerequisites and offline confinement, not login or a full agent
launch. [Install](docs/install.md) covers custom paths and platform notes.

Setup selects nono globally, installs profiles and official packs, and may
update the pack's marked block in
`~/.codex/config.toml`. Reinstalling preserves your user allowance files.

Then open a terminal in the project the agent should work on, sign in once,
and start it:

```sh
nono-wrapper codex login
nono-wrapper codex
```

Use `nono-wrapper opencode` the same way. The current directory is the
agent's writable workspace. By default, start below `~/development`; an
explicit `NONO_WRAPPER_ROOT` allows starting in that root or below it
([Where an agent may start](docs/usage.md#where-an-agent-may-start)).
[Usage](docs/usage.md) covers OpenCode's sign-in, offline mode and the
session report.

## Known limitations

- **Gradle does not run inside the sandbox.** At startup it lists the
  machine's network interfaces, and nono refuses the socket Java opens for
  that. Run Gradle builds outside the agent; Maven builds work.

## Using nono-wrapper

- **How-to** • [Install](docs/install.md) ·
  [Usage](docs/usage.md): starting agents, ChatGPT login and offline mode ·
  [Configuration](docs/configuration.md): extra hosts,
  [HTTPS paths and MCP endpoints](docs/configuration.md#https-paths-and-mcp-endpoints),
  directories and files,
  settings, proxies and SSH commit signing ·
  [Check and review](docs/verify-and-review.md) ·
  [Troubleshooting](docs/verify-and-review.md#troubleshooting)
- **Reference** • [Commands](docs/usage.md#commands) ·
  [Exit status](docs/usage.md#exit-status) ·
  [Network profiles](docs/configuration.md#network-profiles) ·
  [Manual page](docs/manpages/nono-wrapper.1.md) (`man nono-wrapper` or
  `nono-wrapper man`)
- **Concepts** • [Policy and security boundary](docs/policy.md): what changes
  from the official packs, inherited access, and what the sandbox does and
  does not protect

Built-in help is available through `nono-wrapper --help`.

## Working on nono-wrapper

- **[Development](docs/development.md)** • Project layout, checks, local,
  native and container tests
- **[Issues](https://codefloe.com/itiquette/nono-wrapper/issues)** • Report
  bugs or request features

## License

All files in this repository are licensed CC0-1.0. The licence text is in
[`LICENSES/`](LICENSES).
