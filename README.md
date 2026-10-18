<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# nono-wrapper

Run OpenCode and Codex inside the [nono](https://nono.sh) sandbox.
`nono-wrapper` extends nono's official agent packs with ChatGPT host
allowlists and prints a report of what each session did.

- Sign in with your ChatGPT account; the agent reaches only the hosts it needs.
- Add hosts, directories and files per agent with `nono-wrapper config`. The
  running agent cannot change its own rules.

Linux is the primary platform. macOS and WSL are best effort. BSD is not
currently supported.

## Quick Start

You need [mise](https://mise.jdx.dev/getting-started.html) activated in your
shell and `~/.local/bin` on `PATH`. From a checkout of this repository:

```sh
mise install --locked nono opencode codex go just aqua:koalaman/shellcheck
mise use -g nono@0.80.0 opencode@1.19.3 codex@rust-v0.163.0
just install-local
nono-wrapper verify
```

Prerequisites, custom installation paths, pinned versions and platform notes
are in [Install](docs/install.md).

Setup sets global mise versions for nono and both agents, installs profiles
and official packs, and may update the pack's marked block in
`~/.codex/config.toml`. Reinstalling preserves your user allowance files.

Then open a terminal in the project the agent should work on, sign in once,
and start it:

```sh
nono-wrapper codex login
nono-wrapper codex
```

Use `nono-wrapper opencode` the same way. The current directory is the
agent's writable workspace, and it must be a project below `~/development`
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
  [Configuration](docs/configuration.md): extra hosts, directories and files,
  settings, proxies and SSH commit signing ·
  [Verify and review](docs/verify-and-review.md)
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
