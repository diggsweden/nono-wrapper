<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Configuration

## Everyday access changes

Run these from your normal terminal, outside an agent session. Changes apply
to the next session; use `codex` instead of `opencode` for Codex:

```sh
nono-wrapper config opencode add-host api.github.com
nono-wrapper config opencode add-dir /path/to/existing/reference
nono-wrapper config opencode show
nono-wrapper config opencode remove-host api.github.com
```

Directories are read-only unless you pass `--read-write`. A bare host grants
every path and port on that host. `show` lists your additions, not the entire
effective policy; removing an addition restores any inherited grant.
See [Manage allowances](#manage-allowances-from-the-cli) for URL rules,
files, ports and write access. The profile details below explain advanced use.

## Network profiles

| Profile | Allowed network hosts | Additional inherited variables |
| --- | --- | --- |
| `opencode-chatgpt` (default) | `chatgpt.com`, `auth.openai.com` | none |
| `opencode-offline` | none | none |
| `codex-offline` | none | `CODEX_HOME` |
| `codex-chatgpt` (default) | `chatgpt.com`, `auth.openai.com` | `CODEX_HOME` |

Select a ChatGPT or offline profile, not `agent-policy` or either `*-base`
composition profile: those do not restrict the network on their own.

The allowlists are in `profiles/opencode-chatgpt.jsonc` and
`profiles/codex-chatgpt.jsonc`. Most entries carry endpoint rules, so nono
intercepts TLS for that host and refuses any other HTTPS method or path:

| Host | Allowed HTTPS requests |
| --- | --- |
| `chatgpt.com` (OpenCode) | `POST /backend-api/codex/responses` |
| `chatgpt.com` (Codex) | whole host |
| `auth.openai.com` | `POST /oauth/token`, `POST /api/accounts/deviceauth/usercode`, `POST /api/accounts/deviceauth/token`; Codex also `POST /oauth/revoke` |

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

The pinned OpenCode fetches its model catalogue from `models.opencode.ai`,
which is not listed, so it uses the catalogue bundled with the executable. To
refresh it, extend the profile's `network.allow_domain` with this entry; a
slow reply from that host can delay OpenCode's startup:

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
policies. Package registries, Git hosts and remote MCP destinations are not
added to the network list automatically. Git over SSH does not work through
the HTTP proxy; use HTTPS with an appropriate host grant.

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
nono-wrapper config opencode add-host api.github.com
nono-wrapper config opencode add-host --method GET 'https://gitlab.example/api/v4/projects/123/**'
nono-wrapper config opencode add-port 8081
nono-wrapper config opencode add-dir /path/to/reference
nono-wrapper config opencode add-dir --read-write /path/to/shared-project
nono-wrapper config opencode add-file /path/to/settings.toml
nono-wrapper config opencode add-file --read-write /path/to/output.txt
nono-wrapper config opencode show
```

Each command says what it allowed, for example
`Allowed gitlab.example: every path and port.`

`add-dir` asks before granting a directory that holds configuration
repositories, the same way a launch does
([Where an agent may start](usage.md#where-an-agent-may-start)): answering no,
or having no terminal, leaves the configuration unchanged.

Directories and files are **read-only by default**; `--read-only` makes that
explicit, and `--read-write` allows changes. Adding the same path with a
different mode updates its user allowance and replaces matching bundled path
grants at launch. Paths must exist when adding them; relative paths are
stored as absolute paths.

`add-host` takes a host name such as `api.github.com`, which allows every path
on every port: nono does not restrict a listed host by port, so
`gitlab.example:8443` is stored as `gitlab.example`. An `https://` URL with a
path allows only matching HTTPS requests: `*` matches one path segment and
`**` any number. `--method GET` limits the rule to one HTTP method, such as
read-only API calls; the default `*` allows any. A URL rule narrows the host:
it replaces whole-host access for that host, and further URLs add to it.

URL rules check the method and path, not the request body. **They cannot
limit an MCP server to one project:** an MCP server receives every call on the
same path. Use a token that can read only that project instead. Codex streams
over WebSockets, which nono refuses on hosts with URL rules, so keep
`chatgpt.com` as a whole host.

`add-port PORT` lets the agent and the programs it runs, such as a test
server, listen on and connect to that localhost port. It writes nono's
`network.open_port`, and works with both the ChatGPT and offline profiles.

Remove allowances with:

```sh
nono-wrapper config opencode remove-host api.github.com
nono-wrapper config opencode remove-host 'https://gitlab.example/api/v4/projects/123/**'
nono-wrapper config opencode remove-port 8081
nono-wrapper config opencode remove-dir /path/to/reference
nono-wrapper config opencode remove-file /path/to/settings.toml
```

Removing a directory or file clears that exact path's user-added read, write
and read/write entries, restoring any matching bundled grant. It also works
after the path has been deleted.
**Removal does not revoke access inherited from another profile or a broader
directory grant.** `show` lists user allowances only; `nono profile show opencode-chatgpt` (or
`codex-chatgpt`) shows everything the bundled profile grants.

Edits preserve JSONC comments and unrelated settings, validate with nono, and
replace the file atomically. Repeating an existing allowance is a no-op.
`add-host` means whole-host access: it replaces endpoint-restricted entries
for that host in the user file. Removing a host removes its user entries,
including endpoint rules; removing a URL removes that one rule, with the same
`--method` it was added with.

### User precedence and rule protection

For the bundled profiles, user entries take precedence for matching
resources:

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
`NONO_PROFILE`. With nono 0.80.0, an offline base profile combined with user
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
applying its additions. `nono-wrapper verify` checks the bundled offline
profiles, independently of these user additions.

## Settings

| Setting | Meaning | Default |
| --- | --- | --- |
| `NONO_PROFILE` | base profile to select; user additions still apply | `opencode-chatgpt` or `codex-chatgpt` |
| `NONO_WRAPPER_ROOT` | absolute project root; agents start only below it ([details](usage.md#where-an-agent-may-start)) | `~/development` |
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

### Relocated agent state

The pinned upstream filesystem rules use standard home-directory locations.
If you relocate agent state with XDG variables or `CODEX_HOME`, add the
corresponding path grant in your own profile. Forwarding a location variable
does not itself grant filesystem access. Likewise, an agent's `--cd` or
`--add-dir` argument cannot expand nono's grants.

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

This does not enable pushing over SSH, which needs a direct connection the
sandbox refuses. Running `nono` directly, as `nono-wrapper verify` does, adds
no such grant.
