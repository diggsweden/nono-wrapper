<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# OpenCode 2, private loopback and login

OpenCode **2.2.5** with **`nolabs-ai/opencode` 0.4.2** is the tested baseline.
Only OpenCode 2 is supported.
The v2 pack supports 2.1.0 and later; upstream does not support 2.0.x.

## Install

Install the OpenCode 2 CLI through DevBase or your installer, then install
the pack and profiles:

```sh
just install-local
```

The launcher checks the resolved executable with `--version` **inside the
same nono sandbox** before executing the requested command. Only major 2
is accepted, with no minor-version floor in the guard. Other majors,
unrecognized output and failed probes refuse the requested command with
exit 125 and an instruction to install/select OpenCode 2. An unsupported
binary is invoked only for its version probe, not for the requested session.
This also applies to `serve` and explicit `--server` clients. The tested
baseline and upstream pack's compatibility requirements still apply; a
major-version check does not validate every release's behavior.
OpenCode normally receives `--standalone`,
except for `serve` and an explicit `--server` connection. On Linux those
standalone launches also receive nono `--isolate net,loopback`.

Installation removes the retired OpenCode 1 pack if present because its
plugin wiring conflicts with v2, then replaces the bundled profiles with
their v2 versions. User allowance files are preserved and agent data is not
migrated by the wrapper. Review custom profiles and old port grants when
upgrading. There is no installer major-version selector.

## OpenCode 2: normal Linux sessions

```sh
nono-wrapper opencode
NONO_PROFILE=opencode-offline nono-wrapper opencode session list
```

OpenCode starts its own standalone server, generates its own password and
stops the server when the client exits. Both run in one nono sandbox and
network namespace. Dynamic TCP ports are usable only on that sandbox's
loopback, not on the host. No wrapper-owned daemon or server registry is
involved. Online traffic still goes through nono's filtered proxy; offline
sessions can use private loopback but cannot reach the internet.
For an existing host web server or HTTP API, use an explicitly approved
[local hostname through the proxy](../README.md#local-web-services).
That is distinct from a direct connection to host localhost and needs no
host TCP-port grant.

### Host requirements

The host must permit unprivileged user and network namespaces. Run:

```sh
nono-wrapper doctor opencode
```

Doctor starts its offline checks with the same namespace flags. nono probes
namespace creation at startup and fails closed if the kernel, AppArmor or
container policy refuses it. The wrapper does not retry with weaker rules.
Installation does not change host settings or require root.

On Ubuntu with restricted user namespaces, an administrator can authorize
the installed nono binary with a targeted AppArmor profile; see the
[upstream host requirements][namespaces]. Use its real installed executable
path, including the Mise version directory when relevant. Changing that
path after an upgrade may require updating the AppArmor rule. Prefer a
targeted rule over disabling the host's restriction globally.

**WSL2:** expected to work when the Microsoft kernel and distribution allow
namespaces, but upstream marks this feature unverified on WSL2. The startup
probe is required; native WSL2/OpenCode acceptance has not been run here.
This does not add WSL1 support or establish that every WSL2 security feature
is available. See the [upstream WSL2 matrix][wsl2].

**macOS:** nono's private-loopback feature is Linux-only. The automatic v2
dynamic-port workflow is not validated there; use the explicit-server setup
below. No Linux namespace flag is passed on macOS.

### Conflicting allowances

Private network isolation rejects host `open_port`, `connect_port`,
`listen_port` and `no_proxy` rules. These include your automatically merged
`opencode-user.jsonc`. Remove old host-port/callback grants before a normal
standalone session. An empty list cannot clear a grant inherited from another
profile. Use explicit-server mode when you deliberately need host networking.

### Modern ChatGPT browser token-sharing login

Sign in once, from a directory where agents may start:

```sh
nono-wrapper login opencode
```

A host browser cannot reach a callback listener in private loopback, so
`login` uses a temporary explicit server instead. Inside one sandbox session
it generates a server password, starts `opencode serve` on a free loopback
port, runs `auth login openai --method chatgpt-token-sharing` against it and
stops the server. Only that session gets the server port and a listener for
the dynamic callback port; the port grant also permits outbound TCP on that
port number. Nothing is saved to `opencode-user.jsonc`. Afterwards, start
normal sessions with `nono-wrapper opencode`; both modes share the saved
account state. Authenticated token refresh and model use still need
account-backed validation.

The v2 [provider guide][v2-auth] also lists older Codex sign-in methods. The
bundled v2 policy and instructions use modern `chatgpt-token-sharing`; they
do not add legacy device-code endpoints or select an older method to avoid
the callback requirement.

## Explicit server without private namespaces

For a lasting `serve` setup, V2 can use ordinary host loopback, without
changing AppArmor or enabling a network namespace. Sign-in does not need
this; use `login` above. Start an explicit foreground server inside one wrapper
session and connect a client in another wrapper session. Keep both in the
same project and use the same selected profiles and password.

First allow one server port:

```sh
nono-wrapper config opencode open-port 4096
```

That command saved the port in your user file,
`~/.config/nono/profiles/opencode-user.jsonc` (under `$XDG_CONFIG_HOME` if
you set it). Open it in an editor and add the `environment` entry. With no other
user allowances, the file then reads:

```json
{
  "environment": { "allow_vars": ["OPENCODE_PASSWORD"] },
  "network": { "open_port": [4096] }
}
```

Keep any other allowances; a second `network` or `environment` key is
refused. The environment entry lets the client authenticate to your server.

In terminal A, start the server on a loopback address. With the password
variables unset, OpenCode generates a fresh password and prints it:

```sh
env -u OPENCODE_PASSWORD -u OPENCODE_SERVER_PASSWORD \
  nono-wrapper opencode serve --hostname 127.0.0.1 --port 4096
```

In terminal B, set `OPENCODE_PASSWORD` to that password. Reading it keeps
it out of your shell history and the process list; paste it and press Enter:

```sh
read -rs OPENCODE_PASSWORD && export OPENCODE_PASSWORD  # Bash
read --silent --export OPENCODE_PASSWORD                # Fish
```

Then connect explicitly:

```sh
nono-wrapper opencode --server http://127.0.0.1:4096
nono-wrapper opencode --server http://127.0.0.1:4096 session list
```

Keep terminal A running while the client uses it. Stop it with Ctrl-C when
finished. Tools execute in the **server's** sandbox, so review its session
audit as well as the client's. Restart both after policy changes. An explicit
server URL is your delegation choice: the wrapper does not attest that an
arbitrary existing server was started under nono. Use the sandboxed server
command above rather than attaching to an unsandboxed background service.

This is shared host localhost, not private loopback. TCP port grants are not
generally destination-address restrictions on Linux, and listen grants are
not interface-scoped. See the [network allowance matrix](configuration.md#network-allowance-matrix).
Bind the server to `127.0.0.1` and keep its password out of profiles and argv.

### Return to normal sessions

Stop the explicit server with Ctrl-C, then remove the host-port grant:

```sh
nono-wrapper config opencode remove-open-port 4096
```

Remove the `environment` entry from `opencode-user.jsonc`, keeping any other
allowances, and unset `OPENCODE_PASSWORD` in the client terminal. Normal Linux
sessions can now use `nono-wrapper opencode`; OpenCode generates a fresh
private server password itself.

## Why not just configure the shared service?

The v2 [CLI guide][v2-cli] offers `opencode service set disabled true` as
another way to select private servers. In 2.2.5 that selects the same
standalone implementation, which starts `serve --stdio --port 0`. It does
not fix the sandbox's dynamic-port requirement. Persisting a service port or
proxy environment configures the shared service, not standalone servers;
the [network guide][v2-network] makes this distinction explicit. The wrapper
therefore uses the per-invocation `--standalone` flag and nono isolation,
without changing global OpenCode service settings. `NO_PROXY` alone changes
proxy routing; it does not grant a denied socket operation.

The older `/docs/` pages describe different flags and configuration. Use the
`/v2/docs/` pages and the pinned source when checking these workflows.

## Validation scope

Native Linux acceptance passed for standalone and explicit-server modes with
nono 0.81.0 after the targeted AppArmor rule was loaded. Tests
use isolated fixture data and sandbox both server and clients, exercise
online/offline server access, reject an incorrect password, and start then
cancel the browser callback. No account or completed authentication is used;
the public model-catalogue fetch is disabled in those fixtures.

The updated Linux v2 probe additionally runs online/offline `session list`
without a server URL or host-port grants and checks that a controlled host
localhost service remains unreachable from private loopback. These checks
require namespace support; they are not silently skipped on restricted hosts.
The native standalone checks also passed after host namespace authorization.
Local hostname/IP separation is covered by the container fixture described
in [development](development.md#results-and-limits), not a host Ollama model
session.

Completed login, token refresh, a real model request and interactive TUIs
still need account-backed validation. Native macOS/WSL2 behavior remains a
separate validation target.

Sources: [official pack](https://nono.sh/docs/cli/clients/opencode),
[server implementation](https://github.com/anomalyco/opencode/blob/v2.2.5/packages/cli/src/server-process.ts),
[ChatGPT integration](https://github.com/anomalyco/opencode/blob/v2.2.5/packages/core/src/plugin/provider/chatgpt.ts).

[namespaces]: https://nono.sh/docs/cli/features/namespace-isolation
[wsl2]: https://nono.sh/docs/cli/internals/wsl2-feature-matrix
[v2-cli]: https://opencode.ai/v2/docs/cli/
[v2-auth]: https://opencode.ai/v2/docs/cli/providers/
[v2-network]: https://opencode.ai/v2/docs/network/
