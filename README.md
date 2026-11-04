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

OpenCode **2.2.5** is the tested baseline; only OpenCode 2 is supported.
Before running an OpenCode command, the wrapper checks `--version` inside
the same sandbox. Any `2.x.x` is accepted by this major-version guard;
other or unrecognized versions are refused with an instruction to use v2.
On Linux, start it with `nono-wrapper opencode`:
OpenCode manages its own server inside nono's private loopback. The host
must permit user/network namespaces; `doctor opencode` checks startup.
If Ubuntu blocks that check, follow the [AppArmor setup](#ubuntu-apparmor-setup).
See [OpenCode setup](docs/opencode-2-evaluation.md) for host requirements,
first-time browser login, macOS limits and WSL2 validation status.

Install OpenCode and/or Codex through DevBase or your preferred installer.
The agent must be on `PATH`; this checkout installs the wrapper, not the
agents. You also need [mise](https://mise.jdx.dev/getting-started.html)
activated in your shell and `~/.local/bin` on `PATH`.

Follow [Install](docs/install.md) for the prerequisites and installation
steps. From the checkout, the short version is:

```sh
mise install nono go just aqua:koalaman/shellcheck
mise use -g nono@0.81.0
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

Start OpenCode with `nono-wrapper opencode`. OpenCode 2
on Linux requires the [namespace setup](docs/opencode-2-evaluation.md).
The current directory is the
agent's writable workspace. By default, start below `~/development`; an
explicit `NONO_WRAPPER_ROOT` allows starting in that root or below it
([Where an agent may start](docs/usage.md#where-an-agent-may-start)).
[Usage](docs/usage.md) covers OpenCode's sign-in, offline mode and the
session report.

### Ubuntu AppArmor setup

Ubuntu 23.10 and later can restrict unprivileged user namespaces. If
`nono-wrapper doctor opencode` reports
`kernel.apparmor_restrict_unprivileged_userns = 1`, authorize the installed
nono executable using the rule documented in nono's official
[namespace host requirements](https://nono.sh/docs/cli/features/namespace-isolation#host-requirements-and-failure).
This is an administrator step; `just install-local` does not change
AppArmor or system namespace settings.

#### Quick setup scripts for Mise

Save either script below and run it with `fish setup-nono-apparmor.fish` or
`bash setup-nono-apparmor.sh` from your normal host terminal. They require
Mise, `sudo`, `apparmor_parser` and the installed `nono-wrapper`. Each script
validates a temporary profile before replacing
`/etc/apparmor.d/nono-local-userns`, then loads it and runs
`doctor`. Review that file first if you have customized it.

Both scripts derive the real nono path from Mise and authorize all versions
in that installation tree. They do not change the global AppArmor setting.
The manual instructions below explain the rule and exact-path alternative.
The [container setup test](docs/development.md#apparmor-setup-scripts) executes
these exact snippets with real file installation and AppArmor compilation.

##### Fish: setup-nono-apparmor.fish

```fish
#!/usr/bin/env fish

for tool in mise sudo apparmor_parser nono-wrapper mktemp rm
    if not command -q "$tool"
        printf 'Required command is missing: %s\n' "$tool" >&2
        exit 1
    end
end

set nono (mise which nono)
or exit 1

# Accept a normal absolute Mise path, including spaces, without policy syntax.
set expected '^(/[-a-zA-Z0-9_. ]+)+/installs/nono/[-a-zA-Z0-9_.]+/nono$'
if not string match -qr -- "$expected" "$nono"
    printf 'Expected a versioned Mise nono executable: %s\n' "$nono" >&2
    exit 1
end
set pattern (string replace -r '/[^/]+/nono$' '/*/nono' -- "$nono")
set temporary (mktemp)
or exit 1
function clean_profile --on-event fish_exit --inherit-variable temporary
    command rm -f -- "$temporary"
end

printf '%s\n' \
    'abi <abi/4.0>,' \
    'include <tunables/global>' \
    "profile nono-local-userns \"$pattern\" flags=(unconfined) {" \
    '    userns,' \
    '}' >"$temporary"
or exit 1

apparmor_parser -Q -K "$temporary"
or exit 1
sudo install -m 0644 -- "$temporary" /etc/apparmor.d/nono-local-userns
or exit 1

sudo apparmor_parser -r /etc/apparmor.d/nono-local-userns
or exit 1
nono-wrapper doctor opencode
```

##### Bash: setup-nono-apparmor.sh

```bash
#!/usr/bin/env bash
set -euo pipefail

for tool in mise sudo apparmor_parser nono-wrapper mktemp rm; do
    command -v "$tool" >/dev/null || {
        printf 'Required command is missing: %s\n' "$tool" >&2
        exit 1
    }
done

nono=$(mise which nono)

# Accept a normal absolute Mise path, including spaces, without policy syntax.
expected='^(/[-a-zA-Z0-9_. ]+)+/installs/nono/[-a-zA-Z0-9_.]+/nono$'
if [[ ! $nono =~ $expected ]]; then
    printf 'Expected a versioned Mise nono executable: %s\n' "$nono" >&2
    exit 1
fi
directory=${nono%/*}
pattern="${directory%/*}/*/nono"
temporary=$(mktemp)
trap 'rm -f -- "$temporary"' EXIT

printf '%s\n' \
    'abi <abi/4.0>,' \
    'include <tunables/global>' \
    "profile nono-local-userns \"$pattern\" flags=(unconfined) {" \
    '    userns,' \
    '}' >"$temporary"

apparmor_parser -Q -K "$temporary"
sudo install -m 0644 -- "$temporary" /etc/apparmor.d/nono-local-userns

sudo apparmor_parser -r /etc/apparmor.d/nono-local-userns
nono-wrapper doctor opencode
```

#### Manual setup

Run these steps in your normal host terminal, outside the agent sandbox.
Find the actual nono executable, rather than its Mise shim:

```sh
mise which nono
sudoedit /etc/apparmor.d/nono-local-userns
```

Add the following profile, replacing `/absolute/path/to/nono` with the
absolute executable path printed above. With Mise, that normally includes
the installed version directory:

```text
abi <abi/4.0>,
include <tunables/global>

profile nono-local-userns /absolute/path/to/nono flags=(unconfined) {
    userns,
}
```

**Mise upgrades:** an exact executable path authorizes only that installed
version. To keep the rule working across upgrades such as 0.81.0 to 0.84.0,
replace only the version directory with AppArmor's `*` wildcard. For a
standard Mise installation, the profile header would be:

```text
profile nono-local-userns
    /home/YOUR_USER/.local/share/mise/installs/nono/*/nono flags=(unconfined) {
```

Replace `/home/YOUR_USER` with your actual absolute home path, or adapt the
prefix to the location reported by `mise which nono`. Keep the `userns,`
body and closing brace above. The `*` matches one directory component, so
the rule covers installed and future versions under that nono installation
tree. An exact path is narrower but needs editing after each path-changing
upgrade. Use one of these headers in the profile, not both.

Save and close the editor, then load the profile and rerun the check:

```sh
sudo apparmor_parser -r /etc/apparmor.d/nono-local-userns
nono-wrapper doctor opencode
```

No reboot should be needed. Once the check passes, start OpenCode from your
project directory:

```sh
nono-wrapper opencode
```

This resolves the namespace startup restriction. First-time modern ChatGPT
browser login still needs the separate
[token-sharing login setup](docs/opencode-2-evaluation.md#modern-chatgpt-browser-token-sharing-login).

With an exact path, update and reload the profile whenever an upgrade changes
the executable path. With the version wildcard, ordinary Mise version
updates need no profile change; moving the installation tree still does.
Rerun `nono-wrapper doctor opencode` after upgrading to check compatibility.
The rule permits namespace creation for matching executables; nono still
enforces filesystem restrictions and domain/HTTPS endpoint filtering.
On WSL2, apply this only if its startup check reports the same AppArmor
restriction; the requirement depends on the active kernel and distribution
policy. See [OpenCode setup](docs/opencode-2-evaluation.md#host-requirements)
for WSL2 validation status and other startup restrictions.

**Why OpenCode 1 worked without this:** its former direct workflow did not
request a private network namespace. OpenCode 2's standalone client starts
a separate server on a dynamic TCP port. The wrapper uses nono's
`--isolate net,loopback` so both processes can communicate without granting
access to arbitrary host ports. That additional isolation requires user
namespaces; the existing domain filter still applies.

### Local web services

An approved local HTTP(S) service can be reached **through nono's proxy**
while private loopback stays enabled. Direct host-localhost connections are
isolated; a proxy-approved hostname is a deliberate exception.

For example, to use a web server running on your host's `127.0.0.1:3000`:

1. Keep the service listening on `127.0.0.1`; no public bind address is needed.
2. Run `sudoedit /etc/hosts` and add this local name once:

   ```text
   127.0.0.1 web.nono.test
   ```

3. Allow that exact hostname from your normal terminal:

   ```sh
   nono-wrapper config opencode allow-domain web.nono.test
   ```

4. Restart OpenCode with `nono-wrapper opencode` and use
   **`http://web.nono.test:3000`**, rather than `http://localhost:3000`.
   For a shell request **inside the agent session**, use:

   ```sh
   curl --noproxy '' http://web.nono.test:3000/
   ```

The name is resolved by the proxy on the host. Clients must honor nono's
`HTTP_PROXY`/`HTTPS_PROXY`; `--noproxy ''` explicitly keeps curl on that proxy.
Do not add `allow-tcp-port` or `network.no_proxy` for this workflow: those
are direct-network settings and conflict with private loopback.

**Scope:** `allow-domain` grants that hostname on **all paths and ports**,
not only port 3000. Use an exact name, not a wildcard, and grant it only when
you intend to expose local services through the proxy. This is HTTP(S)
access, not automatic support for raw database connections or host browsers
reaching listeners started inside the sandbox. With an enterprise upstream
proxy, the local name also needs an
[upstream bypass](docs/configuration.md#local-web-services-through-the-proxy).

For Ollama, use the same steps with `ollama.nono.test` and allow that exact
name. Its OpenAI-compatible base URL is
`http://ollama.nono.test:11434/v1`; configure your client's provider to use
that URL. No Ollama account/model request is verified by the HTTP smoke test.

**Several local services:** on Linux, you can instead bind each service to
a different actual loopback IP, for example:

| Service | Actual listener | Entry in `/etc/hosts` |
| --- | --- | --- |
| Web app | `127.0.0.2:3000` | `127.0.0.2 web.nono.test` |
| API | `127.0.0.3:8000` | `127.0.0.3 api.nono.test` |

The service must really bind that IP. For a disposable web-server example:

```sh
python3 -m http.server 3000 --bind 127.0.0.2
```

Allowing only `web.nono.test` does not grant `api.nono.test`. However, all
ports on `127.0.0.2` reachable under the approved name remain in scope,
including listeners bound to `0.0.0.0`. Check actual listeners with
`ss -ltn`; aliases pointing to the same IP do not separate services.
Native WSL2/macOS address-binding behavior is not covered by the Linux test.

To remove the user allowance, run outside the sandbox and restart OpenCode:

```sh
nono-wrapper config opencode remove-domain web.nono.test
```

Removing the allowance does not edit `/etc/hosts`. Offline profiles still
refuse host allowances; remove these rules before selecting offline mode.

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
