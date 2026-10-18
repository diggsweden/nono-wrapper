<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Install

## Prerequisites

- Linux with Git, Bash, `/bin/sh`, curl and coreutils, or macOS with Git and
  its built-in Bash, `/bin/sh`, curl and file utilities for native checks.
- [mise](https://mise.jdx.dev/getting-started.html) installed and activated in
  your shell, for example with `eval "$(mise activate bash)"` in `~/.bashrc`.
  DevBase machines already have this. mise installs everything else, including
  Go and just; a system Go is not used and may be too old.
- Your user command directory (`~/.local/bin` by default) on `PATH`.
- An internet connection for setup and a ChatGPT account with access to the
  agent you want to use.

Minimum supported releases: Ubuntu 26.04, and WSL 2.7.7 when running under
WSL2. The wrapper does not check either; older releases are simply untested.

Run these steps as your normal user from this repository's checkout.

## 1. Install the tools

```sh
mise install --locked nono opencode codex go just aqua:koalaman/shellcheck
```

This installs the pinned tools and Go dependencies. `--locked` accepts only
the versions and checksums recorded in `mise.lock`; naming the tools keeps
that check away from tools in your global mise configuration, which have no
entries there. If mise asks for trust, review `.mise.toml` and run
`mise trust` before retrying.

Check that your shell uses these tools: `go version` in this checkout must
print the Go version from `.mise.toml`. If it prints another one, mise is not
activated and the next step fails with `go.mod requires go >= ...`. Activate
mise, or prefix the commands below with `mise exec --`.

Make nono and the agents available in other project directories:

```sh
mise use -g nono@0.80.0 opencode@1.19.3 codex@rust-v0.163.0
```

## 2. Install the wrapper and profiles

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
`XDG_CONFIG_HOME` for profiles and `XDG_DATA_HOME` for the manual. Use
absolute paths and keep the binary directory on `PATH`.

## 3. Check the installation

```sh
nono-wrapper verify
```

Expect `ok` for each check, then [launch an agent and sign in](usage.md).
Rerun `just install-local` after changing the Go code or bundled profiles.

## Pinned versions

| Component | Version |
| --- | --- |
| nono | 0.80.0 |
| OpenCode CLI | 1.19.3 |
| Official OpenCode 1 pack (`nolabs-ai/opencode-v1`) | 0.1.0 |
| Codex CLI | 0.163.0 |
| Official Codex pack | 0.2.3 |

**nono's `nolabs-ai/opencode` pack targets OpenCode 2 since 0.3.0**, and
OpenCode 1 has its own `nolabs-ai/opencode-v1` pack. Both link the same
OpenCode plugin, so `just install-local` removes an installed
`nolabs-ai/opencode` before installing and pinning the OpenCode 1 pack. Keep
the CLI and pack versions paired.

The OpenCode pack's first-run hook creates its required state directories.
Standalone native agent executables are tested on Linux; package-manager
launchers can need additional runtime grants.

## Platform notes

The full container suite targets Linux x86_64. macOS and WSL2 are best
effort; the notes below record what has and has not been tried. BSD is not
supported yet: nono-wrapper builds only for Linux and macOS, and a build for
another target stops with "nono-wrapper supports only Linux and macOS".

### macOS (best effort)

`mise.lock` includes Linux and macOS downloads for x64 and ARM64. Native
checks, an isolated installation and offline-profile smoke checks have passed
on macOS 27 ARM64; macOS x64 is untested. Interactive and authenticated macOS
agent sessions still need validation. See
[What has been validated](policy.md#what-has-been-validated).

Install mise with MacPorts if it is not already installed:

```sh
sudo port install mise
```

For the default macOS zsh, add `eval "$(mise activate zsh)"` to `~/.zshrc`
and run it in the current shell. With a standalone mise installation, use its
full path (normally `~/.local/bin/mise`) in that activation command. You can
also run commands through `mise exec --` without editing your shell setup.

On Apple silicon, `mise exec -- go version` must report `darwin/arm64`.
`Bad CPU type in executable` from a system Go means an incompatible binary
is being selected; use the pinned native toolchain through mise.

### WSL2 (best effort)

WSL2 starts with reduced protection: there is no Unix-socket mediation. The
details, and the advice to keep projects on the Linux filesystem, are in
[WSL2](policy.md#wsl2). The minimum release is WSL 2.7.7 (`wsl --update` in
Windows).
