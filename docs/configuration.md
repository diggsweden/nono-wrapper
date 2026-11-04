<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Configuration

## Everyday access changes

Run these from your normal terminal, outside an agent session. Changes apply
to the next session; use `codex` instead of `opencode` for Codex:

```sh
nono-wrapper config opencode allow-domain api.github.com
nono-wrapper config opencode add-dir /path/to/existing/reference
nono-wrapper config opencode show
nono-wrapper config opencode remove-domain api.github.com
```

Directories are read-only unless you pass `--read-write`. A bare host grants
every path and port on that host. `show` lists your additions, not the entire
effective policy; removing an addition restores any inherited grant.
See [Manage allowances](#manage-allowances-from-the-cli) for URL rules,
files, ports and write access. For concrete examples, see
[HTTPS paths and MCP endpoints](#https-paths-and-mcp-endpoints).
The profile details below explain advanced use.

## Network profiles

| Profile | Allowed network hosts | Additional inherited variables |
| --- | --- | --- |
| `opencode-chatgpt` (default) | ChatGPT/authentication, model catalogue and developer defaults | Git SSH overrides |
| `opencode-offline` | none | none |
| `codex-offline` | none | `CODEX_HOME` |
| `codex-chatgpt` (default) | ChatGPT/authentication and developer defaults | `CODEX_HOME`, Git SSH overrides |

Select a ChatGPT or offline profile. `agent-policy`, `developer-network` and
the `*-base` profiles are composition building blocks, not complete agent
policies.

The online profiles also inherit `profiles/developer-network.jsonc`, the
single source of shared defaults: GitHub, GitLab, Codefloe, Codeberg,
English/Swedish Wikipedia, Stack Overflow, OpenAI API/file delivery and
vulnerability-data services. The file lists exact hosts and explains their
purpose. It grants no raw TCP ports, credentials, container sockets or
scanner installation/cache permissions. Offline profiles do not inherit it.

Agent-specific login rules remain in `profiles/opencode-chatgpt.jsonc` and
`profiles/codex-chatgpt.jsonc`. Their endpoint rules make nono intercept TLS
and refuse other HTTPS methods or paths:

| Host | Allowed HTTPS requests |
| --- | --- |
| `chatgpt.com` (OpenCode) | `POST /backend-api/codex/responses` |
| `chatgpt.com` (Codex) | whole host |
| `auth.openai.com` (OpenCode 2) | `POST /api/accounts/oauth/token`, `GET /.well-known/jwks.json` |
| `auth.openai.com` (Codex) | `POST /oauth/token`, `POST /api/accounts/deviceauth/usercode`, `POST /api/accounts/deviceauth/token`, `POST /oauth/revoke` |

```jsonc
{
  "domain": "auth.openai.com",
  "endpoints": [{ "method": "POST", "path": "/oauth/token" }]
}
```

Codex keeps the whole `chatgpt.com` host because it streams responses over a
WebSocket, and nono refuses WebSocket upgrades on hosts with endpoint
rules. The paths come from the pinned agent executables; review them when
upgrading either CLI. An agent request that nono refuses receives a 403 with
the body `{"error":"Forbidden"}`.

OpenCode 2.2.5's token-sharing flow uses `api.openai.com` for model traffic.
The existing narrow `chatgpt.com` route remains in the profile. Only OpenCode
2 is supported; its policy does not include legacy Codex login endpoints.
See [OpenCode versions and server setup](opencode-2-evaluation.md).

OpenCode may refresh its model catalogue using the bundled GET rule below.
A slow reply can delay startup; the executable also contains a catalogue:

```jsonc
{
  "domain": "models.opencode.ai",
  "endpoints": [{ "method": "GET", "path": "/api.json" }]
}
```

To update the bundled profiles from this checkout, edit the repository files,
rerun `just install-local`, and start a new agent session to use the updated
installed copies in `~/.config/nono/profiles/`
(or `$XDG_CONFIG_HOME/nono/profiles/`).

The host lists constrain connections from the sandbox. Upstream host
integrations, such as delegated URL opening, retain their own upstream
policies. Language-package registries and remote MCP destinations are not
added automatically. Add other forges using `config AGENT allow-domain HOST`.
Whole-host proxy grants can carry HTTPS and SSH through CONNECT; they are not
HTTPS-only or repository-specific. See [Git over SSH](#git-over-ssh).

## User configuration

Extra hosts, directories and files go in an optional per-agent user
configuration. Manage it from your normal terminal, outside an agent session.
It lives alongside the installed profiles:

| Agent | User configuration file |
| --- | --- |
| OpenCode | `~/.config/nono/profiles/opencode-user.jsonc` |
| Codex | `~/.config/nono/profiles/codex-user.jsonc` |

If you set `XDG_CONFIG_HOME`, use `$XDG_CONFIG_HOME/nono/profiles/` instead.
**`just install-local` preserves these user files**, including on reinstall.

### Manage allowances from the CLI

The CLI creates and edits the user file for you. Use `opencode` or `codex` to
choose which agent's allowances to manage. Replace `/path/to/...` with
existing directories and files:

```sh
nono-wrapper config opencode allow-domain api.github.com
nono-wrapper config opencode allow-domain '*.example.com'
nono-wrapper config opencode allow-domain --method GET 'https://gitlab.example.com/api/v4/projects/123/**'
nono-wrapper config opencode add-dir /path/to/reference
nono-wrapper config opencode add-dir --read-write /path/to/shared-project
nono-wrapper config opencode add-file /path/to/settings.toml
nono-wrapper config opencode add-file --read-write /path/to/output.txt
nono-wrapper config opencode show
```

Each command says what it allowed, for example
`Allowed api.github.com: all paths and ports.` Changes apply to new sessions;
restart the agent to use them. Run configuration edits from your normal
terminal outside nono.

OpenCode 2 standalone on Linux still uses these domain/endpoint allowances
through nono's proxy. Its private loopback changes local TCP access, not the
domain filter. `allow-tcp-port` is for deliberate host-network modes instead;
it conflicts with standalone's namespace isolation. See the mode distinction
below before adding a host-port grant.

`add-dir` asks before granting a directory that holds configuration
repositories, the same way a launch does
([Where an agent may start](usage.md#where-an-agent-may-start)): answering no,
or having no terminal, leaves the configuration unchanged.

Directories and files are **read-only by default**; `--read-only` makes that
explicit, and `--read-write` allows changes. Adding the same path with a
different mode updates its user allowance and replaces matching bundled path
grants at launch. Paths must exist when adding them; relative paths are
stored as absolute paths.

### Linked instruction files

A directory grant covers files in that directory, not the targets of symlinks
that point elsewhere. For example, if `~/.codex/AGENTS.md` links to a shared
instructions file outside `.codex`, grant that file explicitly:

```sh
nono-wrapper config codex add-file "$HOME/.codex/AGENTS.md"
```

`add-file` resolves symlinks, including those in parent directories, prints
the source and target when they differ, and saves the target path read-only
unless `--read-write` is requested. `show` lists that saved target. Repointing
the original link does not move the grant to its new target; other grants
may independently allow access there. Targets that would be reinterpreted
as profile-variable expressions are rejected.

After repointing or removing the link, use the saved path from
`config AGENT show` with `remove-file` to remove the original allowance.
Restart the agent after edits.

### Network allowance matrix

The examples below follow `nono-wrapper config opencode`; replace `opencode`
with `codex` for Codex. Domain allowances describe remote destinations;
TCP-port allowances describe a host-independent connect/listen exception.
They are separate grants.

For **OpenCode 2 standalone on Linux**, exact domains, wildcard domains,
HTTPS method/path rules and user overrides work through the same proxy as
before. Host TCP-port grants are incompatible with its private network:
`allow-tcp-port` may be saved for explicit `serve`/`--server` mode, but that
grant must be removed before returning to standalone. Private-loopback
listeners need no host-port grants and are unreachable from the host.
AppArmor's `userns` authorization permits namespace creation; it does not
add domain or internet access. Offline mode still blocks remote networking.

| Command or input | Meaning |
| --- | --- |
| `allow-domain example.com` | Exact hostname, all paths and ports; no implicit subdomains. |
| `allow-domain api.example.com` | That exact subdomain only. |
| `allow-domain '*.example.com'` | Subdomains at any depth, including `deep.api.example.com`, but not bare `example.com`. Quote the wildcard so the shell does not expand it. |
| `allow-domain API.EXAMPLE.COM.` | Normalized to `api.example.com`. |
| `allow-domain 'https://example.com/api/**'` | Matching HTTPS paths, any HTTP method. `*` matches one path segment; `**` matches any number. |
| `allow-domain --method GET 'https://example.com/api/**'` | Matching HTTPS GET requests. |
| `allow-domain https://example.com` or `https://example.com/` | Whole-domain access, all paths and ports; not an HTTPS-only restriction. |
| `allow-domain example.com:8443` | Rejected: domain ports cannot be restricted. Explicit ports in URLs, including `:443`, are also rejected. |
| `allow-domain 443`, `localhost`, or an IP address | Rejected for new grants: use a domain name. TCP-port grants are a separate, host-independent exception. |
| `allow-domain '*'` or `'api.*.example.com'` | Rejected: only a leading `*.` wildcard is supported. |
| `allow-domain 'https://*.example.com/api/**'` | Rejected: endpoint rules require an exact hostname. Use wildcard domains only for whole-domain access. |
| An `http://` URL, or a URL with credentials, query, or fragment | Rejected: these do not express supported endpoint restrictions. |
| `allow-tcp-port 3000` | TCP connect/listen on this port, including non-loopback addresses on Linux; can bypass domain filtering even with an offline profile. |
| `allow-tcp-port 0`, `65536`, `3000-3010`, or `3000/udp` | Rejected: one decimal TCP port from 1 to 65535 is required. |
| `remove-domain example.com` | Remove that exact user host entry, including all its endpoint rules. Does not remove `'*.example.com'`. |
| `remove-domain --method GET 'https://example.com/api/**'` | Remove only that method/path rule; removing the last endpoint removes the user host entry. |
| `remove-domain 192.0.2.1`, `buildserver`, or `'*'` | Remove that exact stored key, even though it cannot be newly allowed. Wildcards are not expanded to remove other entries. |
| `remove-tcp-port 3000` | Remove the user-added TCP port. |

The only network flag is `--method`, for a URL with a non-root path. Its
default is `*` (any method); method names are normalized to uppercase.
Domain names are case-insensitive and a trailing dot is ignored. Numeric
top-level labels and single-label names are rejected for new grants. Removal
matches existing keys, so an allowance never becomes impossible to remove
because the add grammar is stricter. Stored wildcard or numeric-host endpoint
rules can also be removed by their exact HTTPS method/path. No DNS lookup is
needed to save or remove a rule.

Whole-domain and endpoint rules for the same user domain cannot silently
replace each other. Remove that domain's user allowance before changing its
scope:

```sh
nono-wrapper config opencode remove-domain example.com
nono-wrapper config opencode allow-domain --method GET 'https://example.com/api/**'
```

Further URLs add endpoint rules; repeating an existing rule is a no-op.
Distinct wildcard patterns and exact domains remain separate. An exact URL
rule does not cancel a broader wildcard or inherited allowance. `show`
distinguishes whole domains, HTTPS endpoints and TCP ports, but lists
only user allowances, not effective inherited policy.

URL rules restrict HTTPS methods and paths, not ports. Plain HTTP to a listed
host is not path-filtered; nono 0.81.0 has no HTTPS-only setting. They check
neither query strings nor request bodies. **A shared MCP endpoint does not
separate tool or project permissions:** those calls use the same path. Use
server-side permissions or a token scoped to the intended project. Codex streams
over WebSockets, which nono refuses on hosts with URL rules, so keep
`chatgpt.com` as a whole host.

`allow-tcp-port PORT` writes nono's `network.open_port`. On Linux the exception
permits TCP connect/listen by port, not by host: direct connections to
non-loopback addresses can succeed outside the domain-filtering proxy, even
with an offline profile. It is not a way to allow only `example.com:3000` or
only localhost. Binding a development server to `127.0.0.1` limits that server's
listener; it does not restrict the agent's outbound connection destinations.
It grants TCP, not UDP, and accepts no port ranges.

Remove allowances with:

```sh
nono-wrapper config opencode remove-domain api.github.com
nono-wrapper config opencode remove-domain --method GET 'https://gitlab.example.com/api/v4/projects/123/**'
nono-wrapper config opencode remove-tcp-port 8081
nono-wrapper config opencode remove-dir /path/to/reference
nono-wrapper config opencode remove-file /path/to/settings.toml
```

Removing a directory or file clears that exact path's user-added read, write
and read/write entries, restoring any matching bundled grant. It also works
after the path has been deleted.
**Removal does not revoke access inherited from another profile or a broader
directory grant.** `show` lists user allowances only;
`nono profile show opencode-chatgpt` (or `codex-chatgpt`) shows everything the
bundled profile grants.

Edits preserve JSONC comments and unrelated settings, validate with nono, and
replace the file atomically. Repeating an existing allowance is a no-op.

### HTTPS paths and MCP endpoints

Use the published URL of the service you want to reach. For example:

```sh
# Allow GET requests to one API URL and paths below it.
nono-wrapper config codex allow-domain --method GET 'https://api.example.com/projects/project-a'
nono-wrapper config codex allow-domain --method GET 'https://api.example.com/projects/project-a/**'

# Allow a remote MCP endpoint, rather than every HTTPS path on its host.
nono-wrapper config codex allow-domain 'https://mcp.example.com/mcp'
nono-wrapper config codex show
```

The exact API URL and the `/**` pattern are separate rules.
Replace `codex` with `opencode` for OpenCode. Run configuration edits outside
the sandbox and restart the agent. If you already added whole-domain access,
remove that user allowance before adding an endpoint rule. Other wildcard
or inherited grants may still allow more; see the
[network allowance matrix](#network-allowance-matrix).

The MCP example allows any HTTP method on the exact HTTPS path `/mcp`.
Streamable HTTP uses POST and GET and can use DELETE to end a session, so a
GET-only or POST-only rule may break the client. It does not limit the MCP
tools, searches or projects requested in JSON-RPC bodies. Configure those
permissions on the server or in its credentials. See the
[MCP HTTP transport specification](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#streamable-http).
If the service publishes project-specific MCP endpoint URLs, allow the
relevant URL; the server still controls which operations it exposes.

Configure the MCP server itself in Codex or OpenCode; this command only adds
network access. OAuth login may also need discovery/token endpoints or
another host: allow those separately as needed. A local stdio MCP server
does not need an HTTP endpoint grant just to start; its child processes
remain subject to the sandbox's filesystem and network rules.

Arguments after `nono-wrapper codex` or `opencode` go to the agent; native
nono's `--allow-domain` belongs to `nono run`. Use the wrapper's `config`
commands for persistent allowances. The HTTPS rules do not path-filter plain
HTTP traffic.

### User precedence and rule protection

For the bundled profiles, user entries take precedence for matching
resources:

| Resource | Matching rule |
| --- | --- |
| Host | The user rule replaces bundled `allow_domain` rules for the same hostname or wildcard pattern. |
| Directory | The user's access mode replaces bundled read, write and read/write grants for the same canonical path. |
| File | The user's access mode replaces bundled grants for the same canonical file path, including grants written in the general filesystem lists. |

Distinct hosts and paths keep their existing rules.
For example, `allow-domain auth.openai.com` overrides OpenCode's bundled endpoint
restriction for that host. Removing the user entry restores the bundled rule.
Host comparisons ignore case and a trailing dot; different wildcard patterns
remain separate rules.

For paths, profile variables such as `$HOME` and `$XDG_CONFIG_HOME` are
expanded and symlinks are resolved before comparison. Existing objects and
ancestors are also compared by filesystem identity, so case aliases on APFS
and hard-link aliases cannot bypass path matching or rule-file protection.
Filesystem override entries must be literal paths. A manually edited user
profile must specify one access mode per canonical path; conflicting modes
are rejected.

For example, if a bundled profile grants read/write access to an existing
directory, this requests read-only access instead:

```sh
nono-wrapper config opencode add-dir --read-only /path/to/reference
```

**A broader grant can prevent that restriction from being enforced.** A
read-only file inside the writable workspace, or a read-only directory still
covered by a pack/group write grant, cannot be made read-only just by adding
another grant. The wrapper checks the effective filesystem grants, including
workspace and `NONO_ALLOW` additions, and refuses startup with the
conflicting path when the requested mode cannot be honored. Narrow or remove
the conflicting grant, or remove that user override. It never silently starts
with broader access than the requested mode. Signed pack definitions are not
rewritten.

nono itself combines host and filesystem grants, so changing inheritance
order alone is not enough. For these overrides, the wrapper makes private
copies of the bundled profiles, removes matching bundled entries, and lets
nono resolve the copies with the user profile. Other fields and official-pack
references are preserved. The copies live under the read-only profiles area
and are removed when the session exits. A cleanup failure reports the
retained directory on stderr while preserving the session's exit status.
External profiles selected with `NONO_PROFILE` retain native nono
inheritance; an unresolved conflict outside the bundled profiles stops
override composition rather than silently claiming that the user rule won.

**The running agent can recommend changes, but cannot apply them to its
active profiles.** The bundled profiles give it read-only access to the
profiles and package store, and write access to `nono/profile-drafts` for
proposals. Apply changes yourself from a normal terminal. Config-editing
commands reject agent sessions and refuse write grants that expose active
rules, including through symlinks, hard links or parent directories. A live
profile's backing file is protected even when a dotfile symlink stores it
outside the profiles directory. The wrapper also refuses to share a working
directory containing its rule files. Manual user filesystem overrides that
expose active rules are also rejected. Filesystem enforcement blocks direct
edits even if the agent clears its session environment variables.

### Edit the file directly

For example, put this in `opencode-user.jsonc` to allow the GitHub API and
extra directories. Replace the directory examples with existing directories
you need, or omit `filesystem` if you only want the API grant:

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

The wrapper automatically applies the corresponding user profile. No
`extends` field or `NONO_PROFILE` setting is needed in this example. Existing
ChatGPT, authentication and filesystem grants are kept, except for matching
host/path rules explicitly overridden as described above.
The user file also applies when you select a base profile with
`NONO_PROFILE`. With nono 0.81.0, an offline base profile combined with user
host allowances refuses to start; remove those host entries to use the
remaining filesystem allowances offline. User additions cannot turn off the
base profile's network block.

Inspect the user entries and start a session:

```sh
nono-wrapper config opencode show
nono-wrapper opencode
```

Inside the running session, `nono why --self --host api.github.com` checks
the actual sandbox's host access. Use
`nono why --self --path "$HOME/reference" --op read` for a path. A standalone
`nono why --profile ... --extends ...` uses native additive inheritance and
does not reproduce the wrapper's host/path overrides.

Edits take effect on the next session, with no rebuild or reinstall. Invalid
user profiles stop startup and report the error. Remove the user file to stop
applying its additions. `nono-wrapper doctor` checks the bundled offline
profiles, independently of these user additions.

## Settings

| Setting | Meaning | Default |
| --- | --- | --- |
| `NONO_PROFILE` | base profile to select; user additions still apply | `opencode-chatgpt` or `codex-chatgpt` |
| `NONO_WRAPPER_ROOT` | absolute workspace root; an explicit value allows starting in it or below it ([details](usage.md#where-an-agent-may-start)) | `~/development` |
| `NONO_WRAPPER_ALLOW_CONFIG` | `1` includes configuration repositories in the start directory without asking | ask on a terminal, stop otherwise |
| `NONO_WRAPPER_SSH_AGENT` | `0` withholds the SSH agent socket | granted when `SSH_AUTH_SOCK` names a socket |

Normal nono environment settings, including `NONO_THEME`, `NONO_ALLOW` and
`NONO_ALLOW_DOMAIN`, are respected. They can add to the selected policy.

### Outbound proxy

Behind a mandatory outbound proxy, nono must chain through it, and it does
not read `HTTPS_PROXY` itself. The wrapper passes the host and port from
`HTTPS_PROXY` or `HTTP_PROXY` to nono as its upstream proxy, unless the
selected profile blocks the network. `NONO_UPSTREAM_PROXY=host:port` takes
precedence and is handled by nono itself.

Automatic import accepts an `http://host:port` URL or bare `host:port`; HTTP
URLs may omit the port to use 80. IPv6 addresses use brackets. nono cannot log
in to a proxy, so a user name and password in the URL are dropped, with a
notice: a proxy that requires them refuses the agent's requests. HTTPS/SOCKS
proxy transports, paths other than `/`, query strings and invalid ports are
rejected. A configured proxy that cannot
be imported, or a failed profile inspection, stops startup instead of using a
direct route. The proxy URL is never printed in diagnostics. `NO_PROXY` is
not translated: use `NONO_UPSTREAM_BYPASS` for hosts that must be reached
directly.

### Custom extending profiles

To select a separately named configuration, create an extending nono
profile:

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

Then run `env NONO_PROFILE=my-opencode nono-wrapper opencode`. Core,
terminal, editor, locale, XDG, `TMPDIR`, `SSH_AUTH_SOCK` and
toolchain-location (`GOPATH`, `GOROOT`, `JAVA_HOME`, `CARGO_HOME`,
`RUSTUP_HOME`) variables are listed in `profiles/agent-policy.jsonc`; nono
handles its own injected proxy variables and credentials.

### Runtime host approval (optional)

nono 0.81.0 can ask before connecting to a host outside the static allowlist.
To try its built-in terminal backend, merge this into your agent's existing
user profile, preserving other settings:

```json
{
  "network": {
    "approval_backends": {
      "review": { "type": "terminal", "timeout_secs": 60 }
    },
    "approval_defaults": { "backend": "review" }
  }
}
```

Restart a normal online session. Existing allowed hosts do not prompt.
Explicit denies and metadata/link-local protections cannot be approved;
`network.block: true` cannot be combined with these backends. The terminal
backend denies when no terminal is available or the prompt times out.
"Always allow" and "Always deny" last only for this session and host/port;
they do not edit your user profile. Persistent allowances still use
`config AGENT allow-domain` outside the sandbox.

When using an enterprise upstream proxy, forwarded CONNECT requests need
static allowances: only direct/upstream-bypass CONNECTs consult these
backends. See nono's [runtime host approval documentation][host-approval].
This is opt-in; the shipped profiles do not enable prompts.

[host-approval]: https://github.com/nolabs-ai/nono/blob/v0.81.0/docs/cli/features/networking.mdx#runtime-host-approval

### Private loopback (Linux)

nono 0.81.0 can give a sandbox its own network namespace and TCP loopback.
This is useful for tests and development servers that communicate inside
one sandbox. It does not publish their ports to the host.
OpenCode 2's normal wrapper launch uses this feature automatically with
`--standalone`; see [OpenCode setup](opencode-2-evaluation.md) for required
host support, conflicting grants and first-time browser login. The example
below is an optional experiment for Codex.

The wrapper can pass the native namespace option through for a single offline
Codex session, without creating or changing a profile:

```sh
nono-wrapper run --block-net --isolate net,loopback codex -- features list
```

Start with an offline experiment. Save this as
`~/.config/nono/profiles/codex-loopback.jsonc` (under `XDG_CONFIG_HOME` if set):

```json
{
  "extends": "codex-offline",
  "meta": { "name": "codex-loopback" },
  "linux": { "isolate": { "network": true, "loopback": true } }
}
```

From an ordinary terminal in a disposable project, validate the profile and
open one sandboxed shell. Python 3 and curl must be installed:

```sh
nono profile validate codex-loopback
nono run --profile codex-loopback --allow-cwd -- /bin/sh
# Inside that same shell:
python3 -m http.server 8080 --bind 127.0.0.1 &
server=$!
curl --noproxy '*' --retry 5 --retry-connrefused http://127.0.0.1:8080/
kill "$server"
exit
```

Both processes must run in the same sandbox; another `nono run` creates a
different namespace. The host browser cannot reach that server, and the
sandbox cannot reach services on the host's localhost. UDP and netlink
remain blocked. This offline profile cannot run an authenticated model
session, but you can smoke-test wrapper startup with
`env NONO_PROFILE=codex-loopback nono-wrapper codex features list`.

For an online experiment, first sign in with the normal bundled profile.
The example below uses Codex. OpenCode 2 uses private loopback by default
on Linux and has a separate explicit-server workflow for browser login.
Copy the installed `codex-chatgpt.jsonc` to a separately named profile,
change `meta.name`, remove its `network.listen_port` callback grants, and
add the `linux.isolate` block above. Copying retains its current host rules
without inheriting its callback ports. Do not merely extend
`codex-chatgpt`: port lists accumulate, so `listen_port: []` cannot clear
them.
Select the copy with `NONO_PROFILE`; external-profile behavior, including
explicit grants for relocated state and no automatic Git SSH transport,
still applies. Browser callback login needs the normal non-isolated profile.

Isolation rejects effective `open_port`, `connect_port`, `listen_port` and
`no_proxy` grants. Your automatically applied user profile must not add
them; use a disposable HOME/configuration if you want to keep existing
settings intact. Codex does not get a namespace flag automatically; its
isolation comes from an explicit `run --isolate` option or an optional profile.
OpenCode 2 gets the flag at
launch time, except for `serve` and explicit `--server` connections.

Private loopback is not nested-container support. Netlink sockets and writes to
`/proc/*/uid_map` or `setgroups` remain restricted. Ordinary Podman jobs can use
the configured rootless API socket (with its documented host-user authority).
Tests that require private engine storage or their own Bubblewrap/Buildah/user
namespaces must retain those requirements: run their existing guarded launcher
from a suitable host context. Local-only tiers remain local-only; use CI only
where that tier explicitly permits it. Do not silently substitute a shared service,
unset a private-engine refusal, grant writable `/proc` or drop isolation.

The host must allow unprivileged user namespaces. Ubuntu AppArmor policies
and container seccomp policies can prevent this; nono reports the cause and
stops instead of silently dropping isolation. See the upstream
[namespace guide][namespaces] for host setup, limitations and configuration,
and the project's [validation limits](development.md#results-and-limits).
This experiment is Linux-only; macOS
ignores `linux.isolate` and does not provide the private-loopback boundary.

[namespaces]: https://github.com/nolabs-ai/nono/blob/v0.81.0/docs/cli/features/namespace-isolation.mdx

### Local web services through the proxy

Private loopback blocks **direct** connections to the host's localhost.
The nono proxy runs outside that namespace and can reach an explicitly
allowed hostname resolving to host loopback. The
[README walkthrough](../README.md#local-web-services) uses an `/etc/hosts`
entry such as `127.0.0.1 web.nono.test`, an exact
`config opencode allow-domain web.nono.test` grant, and the URL
`http://web.nono.test:3000`. Keep the actual service bound to `127.0.0.1`.

This requires no TCP-port grant and works with the normal v2 standalone
session. The HTTP client must use the session's proxy; it must not bypass it
for the alias. Names ending in `.nono.test` avoid the default `localhost`
proxy-bypass names. The proxy resolves the name through the host's resolver,
so `/etc/hosts` is configured on the machine running nono, not inside a
new network namespace. In a container that means the container's hosts file.

The allowance is **host-wide, on every path and port**. A name such as
`web.nono.test` is not a port-scoped capability: changing `:3000` to another
port can reach another host service if one is listening there. Separate
aliases help describe intent but do not enforce isolation between services
on the same loopback address. HTTPS method/path rules retain their existing
limits; plain HTTP is not path-filtered. Private-address access does not
override nono's always-denied metadata and link-local destinations.

On Linux, services can bind separate actual loopback addresses such as
`127.0.0.2` and `127.0.0.3`, each with its own approved hostname. This can
separate services bound exclusively to those addresses. A hosts-file alias
does not change where a server listens, and wildcard listeners can still be
reached through an approved address. The policy remains hostname-wide, not
port-scoped. nono 0.81.0 explicitly warns that it ignores port suffixes in
`allow_domain`, in both CLI and profile forms; do not rely on an entry such
as `127.0.0.1:3000` as a narrow grant.

If `HTTPS_PROXY`/`HTTP_PROXY` selects an enterprise upstream proxy, keep the
local alias on the direct side of **nono's** proxy. Merge this into the user
profile, preserving existing settings:

```json
{
  "network": { "upstream_bypass": ["web.nono.test"] }
}
```

This bypasses only the enterprise proxy; nono's domain filter still applies.
Do not use `network.no_proxy`: it bypasses nono itself and is incompatible
with private-loopback isolation. Offline profiles do not support this host
allowance. Remove it with `config opencode remove-domain web.nono.test`
before an offline launch. Other broader or inherited rules can still grant
access, as with every domain allowance.

### Just script recipes on Linux

After updating the checkout, run `just install-local` outside the sandbox and
start a new wrapper session. Existing sessions retain their original grants.

### DevBase compiler, terminal and shared-tool defaults

Default grants live in `profiles/agent-policy.jsonc` and the other native nono
profiles. Relocated-root mappings, prepared subdirectories, Podman discovery and
environment fallbacks are declared in `cmd/nono-wrapper/runtime-defaults.jsonc`.
The latter is embedded: rebuild/reinstall after changing it. Go resolves and
validates paths, preserves user precedence and checks protected launch inputs;
it does not carry a second table of developer permissions. Tests verify that
runtime mappings agree with native profile defaults.

Wrapper runtime declarations accept optional `description` fields on the policy,
application, Podman section and individual filesystem rules. A filesystem entry
can remain a plain string or be written as:

```jsonc
{
  "path": "native",
  "description": "Gradle native-library extraction and locks"
}
```

The shared policy adds read-only Linux GCC helper and header directories:
`/usr/libexec/gcc`, `/usr/include` and `/usr/local/include`. Installed toolchains
remain non-writable. `/dev/ptmx` has an exact read/write file grant for PTY
allocation; it is not a grant to terminal-control sockets.
Trusted binfmt metadata at `/var/lib/binfmts`, pip's exact `/etc/debian_version`
file and public `/usr/local/share/ca-certificates` are readable too. No whole
`/etc`, `/var/lib` or private CA directory is granted.

Reviewed shared lint inputs are read-only at
`$XDG_CONFIG_HOME/devtools/formatting`, `$XDG_CONFIG_HOME/devtools/linting` and
`$XDG_DATA_HOME/devbase-check`. The exact ripgrep config and Compose plugin wrapper
are readable too. Exact ordinary npm, pip, Bundler, Gradle and Cargo config files
are shared read-only as-is; users are responsible for secrets placed in those
deliberately shared files. No whole config/data roots, Docker auth store or
Maven master-password file is added.
Whdkit configuration is shared read-only at `~/.config/whdkit` and
`$XDG_CONFIG_HOME/whdkit`, including its `environment` file. Whdkit's dedicated
data and state directories are writable at `~/.local/share/whdkit`,
`~/.local/state/whdkit` and their `$XDG_DATA_HOME` / `$XDG_STATE_HOME` equivalents.
Those writable directories are prepared before launch; their parents remain
ungranted. Secrets placed in the shared configuration, data or state are visible,
and writable application data may influence later unsandboxed Whdkit runs.
Explicit user restrictions still apply. No host-specific path is required.

Broad inherited Cargo/uv data reads are excluded. Rust toolchains, Cargo
bin and uv Python/tools subtrees remain read-only, while
dedicated Cargo and uv credential stores are not included. Installed pipx venvs
and shared libraries are read-only, not writable installation targets.
Cargo's `registry` and `git` dependency caches are writable, together with the
exact `.package-cache`, `.package-cache-mutate` and `.global-cache` files.
The Cargo home itself, installed binaries and credential files are not writable.

Gradle gets exact writable `native`, `notifications`, `.tmp`, `caches`, `daemon`
and `wrapper/dists` directories,
prepared before sandbox launch. `GRADLE_USER_HOME` defaults explicitly to
`$HOME/.gradle` because the JVM's `user.home` need not follow `HOME`. Custom
Gradle, Cargo, rustup and pipx roots receive the same configured subpath access,
not whole-parent grants. User path restrictions still win. Gradle's ordinary
configuration remains read-only; the wrapper parent
is not writable. `wrapper/dists` must be writable because Gradle creates ZIP lock
files beside its distributions. This also permits changing cached executable
distributions, an accepted shared-cache risk like the Maven repository exception.
Use a dedicated `GRADLE_USER_HOME` for untrusted work. Downloads still require
network access and artifact verification; these grants do not establish full builds.

Go can download and extract dependencies under
`$HOME/go/pkg/mod` and write its build/fuzz cache under
`$XDG_CACHE_HOME/go-build` (normally `~/.cache/go-build`). These writable
directories are prepared before launch. Configured mappings also support
`GOMODCACHE`, `GOCACHE` and the first non-empty entry in a `GOPATH` path list
(`first_path: true` in runtime JSONC). Installed `GOPATH/bin` tools remain
read-only. Go, Cargo and Gradle dependency caches contain executable code;
cache changes can influence later host
builds, so use dedicated caches for untrusted work.
Bundled sessions set an empty/unset `GOCACHE` to that prepared XDG location,
including on macOS, so Go's cache selection agrees with the granted directory.

Mise, golangci-lint, Staticcheck and Trivy also receive scoped read/write caches
at `~/.cache/{mise,golangci-lint,staticcheck,trivy}` and their XDG equivalents.
The wrapper prepares the configured directory and supplies its path through
`MISE_CACHE_DIR`, `GOLANGCI_LINT_CACHE`, `STATICCHECK_CACHE` or `TRIVY_CACHE_DIR`
when unset/empty. Explicit absolute overrides receive the same scoped access.
The cache parent is not granted. Trivy database downloads still require network
permission; writable cache access alone does not enable registry traffic.

OpenGrep gets a prepared read/write cache at `~/.cache/opengrep` or
`$XDG_CACHE_HOME/opengrep`, without granting the parent directory.
This cache grant does not expose OpenGrep's `.opengrep` account/settings store.
Its self-extracted executable runtime lives in the cache, so writes can affect
later unsandboxed scanner runs; use a dedicated cache for untrusted work.
The exact default log files `~/.opengrep/semgrep.log` and
`$XDG_CONFIG_HOME/.opengrep/semgrep.log` are prepared without truncation and
receive read/write access. Symlink log files, non-regular files and multiple
hard links are refused. Account/settings siblings are not granted. Local-scan
acceptance uses project-local scanner settings and the default log path; it
does not establish access to a user's live scanner account state.

**Writable Mise cache is an intentional security trade-off.** Cached environments
and artifacts can influence later unsandboxed tool execution. Mise configuration,
trust/state and installations remain protected, including cache aliases into
those locations and grants overlapping actual managed executables. Use isolated
caches or stricter user overrides for untrusted sessions.

Nanolinter's data directory is shared read-only at `~/.local/share/nanolinter`
and `$XDG_DATA_HOME/nanolinter`. This includes installed linter defaults such as
`lychee.toml`, cached remote configuration and setup records. Secrets placed in
that deliberately shared data are visible; no writes or data-parent grant is added.

User manuals are writable under `~/.local/share/man` and `$XDG_DATA_HOME/man`.
The configured tree is prepared automatically; executables and shell completions
are not included. Manuals are user-controlled content, not trusted system docs.

The dedicated `~/.cache/itiquette-test-vm` / `$XDG_CACHE_HOME/itiquette-test-vm`
cache is writable. Its parent is granted so an explicit cleanup recipe can remove
the `downloads` child itself. The general cache directory remains ungranted.
Cleanup can remove media shared with other checkouts; it never runs automatically.
The VM runner must verify pinned checksums before using cached images.

Linux reads `/etc/dpkg/origins` for public vendor metadata, including the usual
`default` symlink to a vendor file, `/etc/dpkg/dpkg.cfg.d` for configuration
snippets and the exact `/etc/dpkg/dpkg.cfg` file, all read even by version
comparisons. Neither all of `/etc/dpkg` nor writes to system metadata are granted.

```sh
nono-wrapper opencode
# Or:
nono-wrapper codex
```

Bundled profiles set `DEVBASE_CHECK_SKIP_UPDATES=1` and `GOTOOLCHAIN=local`.
Install/update helpers and toolchains outside the sandbox. The filter preserves
`RIPGREP_CONFIG_PATH`, `NODE_USE_SYSTEM_CA`, `NODE_EXTRA_CA_CERTS`,
`NPM_CONFIG_MIN_RELEASE_AGE`, `PIPX_HOME`, `PIPX_COOLDOWN`, `GRADLE_USER_HOME` and
`DOCKER_HOST`; preserving a path
does not grant its target. The wrapper supplies npm/pipx release-age defaults of
seven days only when unset, leaving explicit stricter values intact. These
environment settings are not protection against project code changing its own
environment, nor do they grant registry access.

See the [evaluation and follow-up validation](devbase-core-evaluation.md) for
what was tested and what remains unsupported. Nested sandbox execution, package
downloads and full Gradle builds are not established by startup/default grants.

### Name-service cache on Linux

### Rootless Podman on Linux

Both agents' bundled profiles allow connecting to the current user's exact
`/run/user/$UID/podman/podman.sock` endpoint by default. No socket-directory,
rootful `/run/podman/podman.sock`, Docker socket or container-storage grant is
added. If this rootless socket exists and is owned by the current user, the
wrapper supplies `CONTAINER_HOST=unix:///run/user/$UID/podman/podman.sock` so
ordinary Podman commands use the API rather than writing local storage from
inside the sandbox. Existing `CONTAINER_HOST` or `CONTAINER_CONNECTION` settings
take precedence. External profiles are not changed.
The exact ordinary `$XDG_CONFIG_HOME/containers/registries.conf` is readable;
the container config directory and `auth.json` are not granted. Linux public
subordinate-ID mappings `/etc/subuid` and `/etc/subgid` are readable as exact
files. No `/proc/*/setgroups` write grant or filesystem-root grant is added.

**This intentionally grants host-user capabilities.** The rootless API can
create containers mounting your home directory or other paths accessible to
your user, bypassing nono's filesystem restrictions. Containers can also make
network requests outside nono's domain filter, including from an offline
session. Do not treat the agent as confined from your host user when this
service is available. Rootless means “not root”, not “inside nono”.

The wrapper does not start, enable or reconfigure the Podman service. On a
systemd Linux host, enable it from a normal terminal if desired:

```sh
systemctl --user enable --now podman.socket
```

If the service is absent, no automatic transport setting is added. A socket
grant does not install Podman, prepare test images or make nested sandbox tests
automatically runnable. Reinstall profiles and start a new session to apply
changes.
Local-engine commands that explicitly disable remote mode, or require creating
user namespaces from inside nono, can still fail in `newuidmap`. Use the existing
host rootless service for ordinary container jobs rather than widening `/proc`
or cgroup permissions. Tests whose contract requires a private local engine
must run in a suitable host test environment; substituting the service would
test a different contract. `doctor` reports automatic socket selection without
starting or probing the service.
When `NoNewPrivs=1` is enforced, setuid helpers such as `newuidmap` cannot gain
additional privileges. File and Unix-socket grants cannot change that. Run
local-engine suites on an explicit external worker. Ordinary container jobs can
use the host API when their execution contract permits it, but shared engine
storage is not equivalent to an invocation-private engine. The wrapper does not
relax this isolation mechanism to support nested rootless engines.

### Relocated agent state

The pinned upstream filesystem rules use standard home-directory locations.
For the bundled ChatGPT and offline profiles, the wrapper resolves the
configured OpenCode XDG paths and Codex's `CODEX_HOME` once: those same paths
are prepared before launch and added to the protected session profile when
they differ from upstream defaults. Grants are exact directories, not their
XDG parent roots. Their canonical paths must not expose active policy files.
State variables must contain literal absolute paths; values that nono would
reinterpret as profile-variable expressions or glob patterns are rejected.
Validation applies to the canonical target after resolving symlinks and to
the exact path serialized into the session profile.

An exact user path override replaces the corresponding runtime default.
Broader user restrictions, inherited deny rules and the effective-permission
checks still apply: runtime conveniences never silently defeat a read-only
user rule. The signed upstream packs and persistent user profiles are not
rewritten. Upstream grants to default state/cache locations remain inherited;
redirecting state does not revoke access to those locations.

State locations must not expose managed executables or Mise's control plane:

| Protected Mise location | Source |
| --- | --- |
| Global configuration and fragments/tasks | `~/.config/mise`, `$XDG_CONFIG_HOME/mise`, `MISE_CONFIG_DIR` |
| Trusted-config records and state | `~/.local/state/mise`, `$XDG_STATE_HOME/mise`, `MISE_STATE_DIR` |
| Installed tools, plugins and command dispatchers | Data-directory defaults, `MISE_DATA_DIR` and documented install/plugin/shim/download overrides |
| Explicit global/system configuration files | `MISE_GLOBAL_CONFIG_FILE`, `MISE_CONFIG_FILE`, `MISE_SYSTEM_CONFIG_FILE` |
| HOME-level configuration | `.mise.toml`, `mise.toml`, their local/existing environment variants, `.config/mise*.toml`, and the `.mise`/`mise` configuration trees |

Directory and file aliases are protected at their invocation location and
resolved backing path, including intermediate symlinks and dangling links to
future configuration.
Configuration alias discovery inspects filesystem metadata, never evaluates
Mise configuration or templates, and terminates directory cycles. Keep
configuration in dedicated directories; a configuration tree containing HOME
is rejected. Overrides must be absolute paths.

Every agent launch checks the final effective writable grants, regardless of
whether they came from state setup, signed packs, a user/external profile,
the workspace or `NONO_ALLOW`. A read-only file grant cannot cancel a writable
parent-directory grant. Move the state/workspace or narrow the offending
grant when these locations overlap.

External profiles selected through `NONO_PROFILE` retain their own permission
policy: grant relocated paths there explicitly. Forwarding a location variable
alone is not a general filesystem grant. Likewise, an agent's `--cd` or
`--add-dir` argument cannot expand nono's grants.

## Git over SSH

Bundled online profiles automatically route Git SSH through nono's
authenticated CONNECT proxy when `ssh`, an enabled `SSH_AUTH_SOCK`, and valid
entries in `~/.ssh/known_hosts` are available. Verify new servers outside the
sandbox first, using your normal SSH workflow. No PAT or private-key file
grant is needed when your SSH agent already authenticates to the forge.

```sh
# Other forges are added with the same host allowance as HTTPS.
nono-wrapper config codex allow-domain git.example.com
# Inside the agent session, use a normal Git remote.
git clone git@git.example.com:team/project.git
# Use the provider's SSH endpoint on port 443 when it offers one.
git clone ssh://git@ssh.github.com:443/example/project.git
```

The wrapper creates a private, read-only snapshot of parsed public host keys;
it copies neither private keys nor host-key comments or personal SSH config.
The snapshot and generated policy are removed after the session. The SSH
binary and source host-key file must be outside writable grants.

The generated driver uses agent authentication and strict host-key checking.
It is provided as Git's `GIT_SSH` fallback, so an existing `core.sshCommand`
still wins according to Git's normal precedence.
Explicit `GIT_SSH_COMMAND` or `GIT_SSH` values in the caller environment or
the user profile take precedence; `GIT_SSH_VARIANT` is also passed through.
Use an explicit command for host aliases, jump hosts or personal SSH config,
and grant any public configuration files it needs. No global Git/SSH files
are rewritten. External/offline profiles receive no automatic transport.

`ssh-proxy HOST PORT` is the transport helper, not a separate permission:
it only connects to the authenticated loopback proxy of an active nono
session. It does not open port 22, bypass host rules or limit remote
repository permissions. User deny rules remain effective. A host with HTTPS
endpoint restrictions is not silently broadened for SSH; use a separate SSH
host if the service provides one, or deliberately change the host allowance.
Enterprise proxies can independently refuse SSH tunnels, including on 443.

## Signing commits through an SSH agent

When `SSH_AUTH_SOCK` names a socket, the wrapper grants that one socket so
the agent can sign commits with Git's SSH signing. Repository Git settings do
not add host-file permissions. Use Git's inline public-key form so signing
needs the socket without an additional key-file grant.

In the project whose commits you want to sign, set `user.signingkey` to
`key::` followed by the full public-key line for a key already loaded in your
agent. `ssh-add -L` lists those public keys. Replace the placeholders in this
example:

```sh
git config gpg.format ssh
git config user.signingkey 'key::ssh-ed25519 <base64-public-key> <comment>'
```

Then `git commit -S` can use the agent for signing. This is standard
[Git SSH-signing configuration](https://git-scm.com/docs/git-config), and
works without granting access to private-key files. Other sockets still need
explicit grants.

Anything in the sandbox can then ask the SSH agent to sign with every key it
holds. Load only the signing key, require confirmation with `ssh-add -c`, or
turn the grant off:

```sh
env NONO_WRAPPER_SSH_AGENT=0 nono-wrapper codex
```

Signing itself needs no network. Git transport additionally uses the online
proxy setup described above. Running `nono` directly, as `nono-wrapper doctor`
does, adds no wrapper-provided SSH-agent grant or Git transport.
