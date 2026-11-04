<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Install

## Prerequisites

- Linux with Git, Bash, `/bin/sh`, curl and coreutils, or macOS with Git and
  its built-in Bash, `/bin/sh`, curl and file utilities for native checks.
- [mise](https://mise.jdx.dev/getting-started.html) installed and activated in
  your shell, for example with `eval "$(mise activate bash)"` in `~/.bashrc`.
  DevBase machines already have this. mise installs the sandbox and build
  tools, including Go and just; a system Go may be too old.
- OpenCode and/or Codex installed through DevBase's AI tool configuration,
  or an equivalent installation on `PATH`. This checkout does not pin or
  install host agent versions.
- Your user command directory (`~/.local/bin` by default) on `PATH`.
- An internet connection for setup and a ChatGPT account with access to the
  agent you want to use.

Minimum supported releases: Ubuntu 26.04, and WSL 2.7.7 when running under
WSL2. The wrapper does not check either; older releases are simply untested.

Run these steps as your normal user from this repository's checkout.

## 1. Install the tools

```sh
mise install nono go just aqua:koalaman/shellcheck
```

This installs the versions selected in `.mise.toml` and prepares Go
dependencies. Host-tool lockfiles are currently disabled. Naming the tools
keeps installation scoped to this checkout's sandbox/build tools. If mise
asks for trust, review `.mise.toml` and run `mise trust` before retrying.

Check that your shell uses these tools: `go version` in this checkout must
print the Go version from `.mise.toml`. If it prints another one, mise is not
activated and the next step fails with `go.mod requires go >= ...`. Activate
mise, or prefix the commands below with `mise exec --`.

Make nono available in other project directories:

```sh
mise use -g nono@0.81.0
```

## 2. Install the wrapper and profiles

```sh
just install-local
```

This installs OpenCode 2 profiles. Install the OpenCode 2 CLI through
DevBase/Mise first; the installer does not install or guess agent versions.
See [OpenCode versions and server setup](opencode-2-evaluation.md).
On Linux, the default v2 launch requires permitted user/network namespaces.
`doctor opencode` exercises that startup and reports host restrictions.
Installation does not change AppArmor or system namespace settings. WSL2's
namespace workflow still needs native validation; macOS needs explicit-server
setup because nono's private loopback is Linux-only.

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
nono-wrapper doctor codex
```

Use `doctor opencode` for OpenCode, or `doctor` to check both agents. A missing
selected agent fails the check. Success covers prerequisites and baseline
offline confinement; login, additional access rules and the full agent
launch are not tested. Then [launch an agent and sign in](usage.md).
For a failure, start with [Troubleshooting](verify-and-review.md#troubleshooting).
Rerun `just install-local` after changing the Go code or bundled profiles.

## Update ownership

| Component | Selected by | Check after an update |
| --- | --- | --- |
| Host OpenCode/Codex | DevBase or your installer | Run `doctor AGENT`, then launch that agent and check login and project operations. |
| nono | This checkout's `.mise.toml` | Run `doctor` for the agents you use and validate real agent integration. |
| Official packs | This checkout's `test/packs` | Reinstall with `just install-local`, then validate agent integration. |
| Bundled profiles | This repository | Reinstall, restart sessions and check the affected access rules. |
| User allowances | You or your platform configuration | Restart the affected agent and verify the intended access. |

The commands above are user checks. Maintainers should also run the
[development checks](development.md#everyday-commands) and the relevant
[integration tests](development.md#recipes) when changing compatibility.

## Sandbox and test versions

DevBase owns host OpenCode and Codex stable-version selection and updates.
The CLI versions below are the independently pinned container-test baseline
in `test/tools`, not host installation requirements. Retest sandbox
integration when updating agents; this checkout supports only OpenCode 2.
Independently updated host agents are not automatically covered by
that container-test baseline.

| Component | Version |
| --- | --- |
| nono | 0.81.0 |
| OpenCode 2 CLI (test baseline) | 2.2.5 |
| Official OpenCode 2 pack (`nolabs-ai/opencode`) | 0.4.2 |
| Codex CLI (container tests) | 0.163.0 |
| Official Codex pack | 0.2.3 |

The official `nolabs-ai/opencode` 0.4.2 pack requires OpenCode 2.1.0 or later;
2.0.x is not supported upstream. Keep the CLI and pack versions paired.
OpenCode 1 is no longer supported. Installation removes its retired pack if
present because its plugin wiring conflicts with v2, and installs the v2
profiles. User allowance files are preserved; review custom profiles and
old callback/port grants before starting a private-loopback session.

The OpenCode pack's first-run hook creates its required state directories.
Standalone native agent executables are tested on Linux; package-manager
launchers can need additional runtime grants.

## Platform notes

The full container suite targets Linux x86_64. macOS and WSL2 are best
effort; the notes below record what has and has not been tried. BSD is out of
scope: nono-wrapper builds only for Linux and macOS, and a build for
another target stops with "nono-wrapper supports only Linux and macOS".

### macOS (best effort)

Mise selects the host tools for the current platform. Earlier native checks, an
isolated installation and offline-profile smoke checks have passed
on macOS 27 ARM64; these have not been rerun for nono 0.81.0. macOS x64 is
untested. Interactive and authenticated macOS agent sessions still need
validation. See
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

The wrapper disables Unix-socket filtering for compatibility with a known
[WSL2 issue](https://github.com/microsoft/WSL/issues/9548). The agent may access
local services available to your user. See [WSL2](policy.md#wsl2) for details
and test coverage. Keep projects on the Linux filesystem rather than `/mnt/c`.
The minimum release is WSL 2.7.7 (`wsl --update` in Windows).
