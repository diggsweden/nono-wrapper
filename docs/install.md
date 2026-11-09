<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Install

## Prerequisites

- Linux with Git, Bash, `/bin/sh`, curl and coreutils, or macOS with Git and
  its built-in Bash, `/bin/sh`, curl and file utilities for native checks.
- [mise](https://mise.jdx.dev/getting-started.html) installed and activated in
  your shell, for example with `eval "$(mise activate bash)"` in `~/.bashrc`.
  DevBase machines (Digg's developer-machine setup) already have this; DevBase
  is optional. mise installs the sandbox and build tools, including Go and
  just; a system Go may be too old.
- OpenCode and/or Codex on `PATH`, from DevBase's AI tool configuration or
  any other installer. This checkout does not pin or install host agent
  versions.
- Your user command directory (`~/.local/bin` by default) on `PATH`.
- An internet connection for setup and a ChatGPT account with access to the
  agent you want to use.

Minimum supported releases: Ubuntu 26.04, and WSL 2.7.7 when running under
WSL2. The wrapper does not check either; older releases are simply untested.

Run these steps as your normal user.

## Install from a release binary

A release binary carries its profiles and pack pins, so no checkout is
needed. Release assets are signed with Sigstore.

1. Select nono for every directory: `mise use -g nono@0.81.0`.
2. Download the `nono-wrapper` asset for your platform from the
   [releases page](https://github.com/diggsweden/nono-wrapper/releases) and
   install it on `PATH`, for example
   `install -m 0755 nono-wrapper-ASSET ~/.local/bin/nono-wrapper`.
3. Install the profiles and packs, then check the installation:

   ```sh
   nono-wrapper install-profiles
   nono-wrapper doctor
   ```

`nono-wrapper man` prints the manual. Run `install-profiles` again after
updating the binary; `doctor` notes profiles that differ from it.

## Install from a checkout

Use a checkout to build an unreleased version or to work on nono-wrapper.
Get the checkout first:

```sh
git clone https://github.com/diggsweden/nono-wrapper.git
cd nono-wrapper
```

## 1. Install the tools

mise reads a checkout's `.mise.toml` only after you trust it. Review that
file, then trust it and install:

```sh
mise trust
mise install nono go just
```

This installs the versions selected in `.mise.toml` and prepares Go
dependencies. Host-tool lockfiles are currently disabled. Naming the tools
keeps installation scoped to this checkout's sandbox/build tools.

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
See [OpenCode versions and server setup](opencode.md).
On Linux, the default v2 launch requires permitted user/network namespaces.
`doctor opencode` exercises that startup and reports host restrictions.
Installation does not change AppArmor or system namespace settings. For
macOS and WSL2, see the [platform notes](#platform-notes).

This builds the wrapper, installs it and the manual, and runs its
`install-profiles`, which installs the signed official packs and bundled
profiles and checks the profiles' inheritance. Default
installation paths:

```text
~/.local/bin/nono-wrapper
~/.config/nono/profiles/*.jsonc
~/.local/share/man/man1/nono-wrapper.1
```

The packs also install their agent integrations, including a marked block in
`~/.codex/config.toml`. Reinstalling updates bundled files and preserves your
optional `opencode-user.jsonc` and `codex-user.jsonc`.

Registry DNS lookup timeouts are retried up to three
attempts per pack, with waits of one and two seconds. Signature, trust and
other failures stop immediately. If timeouts persist, check your
DNS/VPN/network connection and any `NONO_REGISTRY` override, then rerun
`just install-local` from a normal terminal outside nono. A pack failure
leaves the installed wrapper binary and manual unchanged.

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
| nono | This checkout's `.mise.toml` | Rerun `mise use -g nono@VERSION` with the version from `.mise.toml`, then `doctor` for the agents you use, and validate real agent integration. |
| Official packs | `profiles/packs`, built into the binary | Run `nono-wrapper install-profiles`, then validate agent integration. |
| Bundled profiles | This repository, built into the binary | Run `nono-wrapper install-profiles`, restart sessions and check the affected access rules. |
| User allowances | You or your platform configuration | Restart the affected agent and verify the intended access. |

The commands above are user checks. Maintainers should also run the
[development checks](development.md#everyday-commands) and the relevant
[integration tests](development.md#recipes) when changing compatibility.

## Uninstall

Run these from your normal terminal, outside an agent session. Remove the
packs first, while nono is still installed; nono also reverses their agent
wiring, such as the marked block in `~/.codex/config.toml`:

```sh
nono remove nolabs-ai/opencode
nono remove nolabs-ai/codex
```

Then remove the wrapper, the manual and the bundled profiles. These are the
default paths; adjust them if you set `INSTALL_DIR` or the XDG variables:

```sh
rm -f ~/.local/bin/nono-wrapper ~/.local/share/man/man1/nono-wrapper.1
cd ~/.config/nono/profiles
rm -f agent-policy.jsonc developer-network.jsonc \
  opencode-base.jsonc opencode-chatgpt.jsonc opencode-offline.jsonc \
  codex-base.jsonc codex-chatgpt.jsonc codex-offline.jsonc
```

`opencode-user.jsonc` and `codex-user.jsonc` in the same directory hold your
own allowances; delete them too if you no longer need them. Finally, drop the
global nono selection with `mise unuse -g nono`. Agent logins and other agent
data stay in the agents' own directories.

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
