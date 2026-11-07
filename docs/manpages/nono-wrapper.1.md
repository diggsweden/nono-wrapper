# NONO-WRAPPER 1 "User Commands"

## NAME

nono-wrapper - run OpenCode and Codex with nono's sandbox profiles

## SYNOPSIS

**nono-wrapper** **opencode**|**codex** [*agent-argument*...]

**nono-wrapper** **run** [*sandbox-option*...] **opencode**|**codex** [**--**] [*agent-argument*...]

**nono-wrapper** **config** **opencode**|**codex** *subcommand* [*argument*]

**nono-wrapper** **doctor** [**opencode**|**codex**...]

**nono-wrapper** **changes** *MARKER* [*DIRECTORY*]

**nono-wrapper** **man**

**nono-wrapper** **--help**|**--version**

## DESCRIPTION

**nono-wrapper** starts an AI coding agent inside nono's sandbox. The current
directory is the writable workspace. Other access comes from the selected
profile, inherited packs, user allowances and launch-time grants. Bundled
profiles also grant agent state and selected caches outside the workspace;
the workspace is not the agent's only writable location.

By default, the agent starts only below ~/development, not in that root
itself. An explicitly set **NONO_WRAPPER_ROOT** allows starting in the chosen
root or below it, never outside it. The root must be absolute and must not
be, or contain, the home directory; symlinks are resolved before checking.
When the start directory holds Git repositories named config, or
ending in -config or _config, the wrapper lists them and asks before
including them. Without a terminal to answer from, it stops instead.

The profile is **NONO_PROFILE**, or *AGENT*-chatgpt, plus the optional
*AGENT*-user.jsonc in the nono profiles directory. After the agent exits, the
wrapper prints a summary of the session's audit record on standard error.

Only OpenCode 2 is supported. Before the requested command, the launcher
checks **--version** inside the same sandbox. Any 2.x.x passes the major
check; other or unrecognized versions, and failed probes, refuse startup
with exit 125 and an instruction to use OpenCode 2.
It normally receives **--standalone**;
on Linux the wrapper adds nono **--isolate net,loopback** so OpenCode manages
its own dynamic-port server in the same sandbox. The host must permit
user/network namespaces; **doctor opencode** checks startup. Unsupported
hosts fail closed, without opening host ports. Native WSL2 is unverified.
Explicit **serve** and **--server** retain ordinary host networking instead.
Modern ChatGPT token-sharing browser login needs the explicit-server setup.
See <https://github.com/diggsweden/nono-wrapper/blob/main/docs/opencode.md>
for host requirements and macOS limits.
Host browser callbacks cannot enter private loopback.

Online profiles inherit the common forge, reference and vulnerability-data
hosts from developer-network.jsonc. Offline profiles do not. With an SSH
agent and verified entries in ~/.ssh/known_hosts, online sessions can route
Git SSH through nono's proxy. Public host keys are copied to a private
read-only session file; private keys and personal SSH config are not copied.
Strict host-key checking remains enabled. Explicit GIT_SSH_COMMAND or
GIT_SSH settings take precedence; no global Git/SSH files are rewritten.

On macOS, the bundled profiles deny Apple Events and access to their Mach
broker. The wrapper refuses to fall back to the official agent profile when
the bundled profile is missing, because that would omit these restrictions.
Custom **NONO_PROFILE** policies must inherit **agent-policy** or provide
equivalent restrictions. Reinstall profiles and restart sessions to apply
policy updates.

On WSL2, the wrapper disables Unix-socket filtering for compatibility with
<https://github.com/microsoft/WSL/issues/9548>. The agent may access local
services available to your user. The wrapper prints a note on standard error.
The minimum supported releases are WSL 2.7.7 and Ubuntu 26.04; neither is
checked.

## COMMANDS

On Linux, bundled profiles also allow the current user's exact rootless Podman
socket at /run/user/$UID/podman/podman.sock. When it is available and owned by
the user, the wrapper sets **CONTAINER_HOST** to that Unix endpoint unless a
container transport was explicitly selected. The wrapper does not start the
service or grant its parent directory, rootful sockets or container storage.
**The rootless API can mount host paths and run programs outside nono as your
user, including outside its network filter.** This is intentional host-user
access, not a confined container interface.

**run** [*sandbox-option*...] **opencode**|**codex** [**--**] [*agent-argument*...]
:   Run with temporary, additive nono-style permissions. Put all sandbox options
    before the agent name. Everything after it belongs to the agent, including
    **--help**; one optional **--** separator is consumed by the wrapper.
    Supports **--profile** (**-p**), **--allow** (**-a**, read/write),
    **--read** (**-r**), **--write** (**-w**, write-only), **--allow-file**,
    **--read-file**, **--write-file**, **--allow-domain**, **--open-port**,
    **--block-net**, **--isolate**, **--silent** (**-s**) and **--no-diagnostics**.
    Path, domain and port options are repeatable. **--profile** overrides
    **NONO_PROFILE**. Inherited deny rules, workspace checks and protections for
    active policies, managed executables and Mise remain in force.
    **--open-port** permits connect and listen, including non-loopback addresses;
    it can bypass domain filtering and conflicts with Linux private loopback.
    **--isolate net,loopback** gives Linux tests private dynamic TCP ports, not
    access to host localhost. It requires user/network namespace support and a
    blocked or proxied network policy without direct TCP grants. Nono validates
    namespace names and conflicts; failures never fall back to host networking.

    On Linux, bundled Codex offline sessions also select private loopback
    automatically. Online Codex retains host networking for browser callbacks.
    **--silent** suppresses nono output and the wrapper report, not agent output.
    Native failure footers and profile-save prompts are suppressed by default;
    **--no-diagnostics=false** restores them. Auditing stays enabled.
    These options do not edit persistent user configuration.

**opencode** [*agent-argument*...], **codex** [*agent-argument*...]
:   Start the agent under nono. Every argument, including **--help**, goes to
    the agent. If the agent on **PATH** is a mise shim, the wrapper asks
    **mise which** for the real executable. To list project files the session
    changed, create a marker file before you start; see **changes** below.

**config** **opencode**|**codex** **show**
:   List user-added domains, HTTPS method/path rules, TCP ports and
    paths. Bundled and inherited grants are not listed.

**config** **opencode**|**codex** **allow-domain**|**remove-domain**
[**--method** *METHOD*] *DOMAIN*|*HTTPS_URL*
:   Allow an exact domain on all paths and ports, or remove its user rules.
    A quoted '\*.example.com' includes subdomains at any depth but not bare
    example.com. Only a leading \*. wildcard is supported. Single-label
    names, numeric addresses and explicit ports (including :443) are rejected
    for new grants. Removal accepts exact stored keys, including addresses,
    single-label names and wildcard patterns, without expanding other entries.
    An https:// URL with a non-root path adds an HTTPS endpoint rule (\* is
    one path segment, \*\* any number); **--method** defaults to \* (any).
    URLs without a path, or with /, mean whole-domain access.
    Endpoint rules require an exact hostname; wildcard domains are supported
    only for whole-domain access.
    URL credentials, queries and fragments are rejected. Ports are unrestricted;
    plain HTTP is not path-filtered, and request bodies are not checked.
    Remove the exact user domain before switching between whole-domain and
    endpoint rules. Removing a URL matches its method and path; removing the
    last endpoint removes the user domain. Other wildcard and inherited
    allowances may still permit access.
    MCP calls may share an endpoint such as /mcp; URL rules do not restrict
    tools or projects named in request bodies. Use server-side permissions.

**config** **opencode**|**codex** **open-port**|**remove-open-port** *PORT*
:   Let the agent and its programs connect to and listen on one TCP port.
    This is host-independent: on Linux it can permit direct connections to
    non-loopback addresses, bypassing domain filtering even with an offline
    profile. It is not limited to localhost or one remote domain. Binding a
    server to 127.0.0.1 does not restrict the agent's outbound destinations.
    Accepts one decimal port from 1 to 65535, not ranges or UDP.
    The beta names **allow-tcp-port** and **remove-tcp-port** still work.

**config** **opencode**|**codex** **allow**|**read** *PATH*
:   Persist read/write (**allow**) or read-only (**read**) directory access.
    **allow-file** and **read-file** do the same for individual files.
    These are short forms of **add-dir** and **add-file**, using nono's
    access-mode names.

**config** **opencode**|**codex** **add-dir** [**--read-write**] *PATH*,
**remove-dir** *PATH*
:   Allow a directory, read-only unless **--read-write**, or remove the user
    allowance for it. The path must exist and is stored as an absolute path.

**config** **opencode**|**codex** **add-file** [**--read-write**] *PATH*,
**remove-file** *PATH*
:   The same for a single file. **add-file** resolves symlinks, shows the source
    and target when they differ, and saves the target path. Repointing the
    original link does not move this grant. **show** lists the saved target;
    use that path with **remove-file** after a link is repointed or removed.

Configuration changes apply to new sessions and must be made from a normal
terminal, outside a nono session. Restart the agent to use them.
Native nono's **--allow-domain** flag belongs to **nono run**;
use **config** *AGENT* **allow-domain** for persistent wrapper allowances.

**doctor** [**opencode**|**codex**...]
:   Report the nono version, agent paths, project root and proxy setting, then run
    short probes under each agent's offline profile. The project and the
    temporary directory must be writable; the home directory, the
    configuration directory and the network must be refused.
    With no arguments, check both agents; name one for a single-agent
    installation. A missing selected agent or an inactive Mise shim fails
    the check. Agent paths are resolved without executing the agents.
    Success covers the installed offline profiles, not ChatGPT login,
    user-added access rules or the full agent launch.

**changes** *MARKER* [*DIRECTORY*]
:   List hooks, CI workflows, editor tasks, package scripts and agent or tool
    settings in *DIRECTORY* (default: the current directory) that changed after
    *MARKER* was last modified. Create *MARKER* before a session, for example
    with mktemp. For nono's own record of the session, see nono audit;
    it does not detect deletions or provide a complete change log.

**man**
:   Print this manual, for systems without man(1).

**ssh-proxy** *HOST* *PORT*
:   Git transport helper for an active nono session. Connects through nono's
    authenticated loopback proxy. It adds no host or TCP-port permission.
    Normal SSH remotes, including port 443 when supported by the forge, use
    the same domain policy. Verify new host keys outside the sandbox first.

**completion** **bash**|**zsh**|**fish**
:   Print a completion script for that shell, to source from its startup
    file; see **nono-wrapper help completion**.

## OPTIONS

**-h**, **--help**
:   Show help for the wrapper or for one of its commands.

**-V**, **-v**, **--version**
:   Show the build and Go toolchain version.

## ENVIRONMENT

**NONO_PROFILE**
:   Profile to use instead of *AGENT*-chatgpt.

**NONO_WRAPPER_ROOT**
:   Absolute workspace root. An explicit value allows starting in that
    directory or below it, sharing the whole start directory read/write.
    Without it, agents start only below ~/development.

**NONO_WRAPPER_ALLOW_CONFIG=1**
:   Include configuration repositories in the start directory without asking.

**NONO_WRAPPER_SSH_AGENT=0**
:   Do not share the SSH agent socket with the sandbox.

**GIT_SSH_COMMAND**, **GIT_SSH**, **GIT_SSH_VARIANT**
:   Explicit Git SSH settings are passed through by online profiles. An
    explicit command replaces the automatic agent-based transport.

**CONTAINER_HOST**, **CONTAINER_CONNECTION**
:   Explicit container transports, preserved instead of the automatic Linux
    rootless Podman endpoint. Selecting another socket does not grant it.

**HTTPS_PROXY**, **HTTP_PROXY**
:   Upstream proxy for the sandbox's network proxy. A user name and password
    in the URL are not used: nono cannot log in to a proxy.

## EXIT STATUS

**0**
:   Success.

**1**
:   **doctor**: a check failed, including a missing selected agent;
    **changes**: changed files are listed.

**64**
:   Usage error: an unknown command or flag, or wrong arguments.

**65**
:   A malformed value, such as an invalid host name.

**66**
:   A named file or directory does not exist.

**70**
:   An unexpected failure.

**77**
:   Permission denied, or refused by policy.

**78**
:   The user configuration is unusable.

**125**
:   **opencode**, **codex**: the wrapper could not start the session, a refused
    start directory included.

**127**
:   **opencode**/**codex**: nono or the agent is not on **PATH**.
    **doctor**: nono or a probe utility is not on **PATH**. A missing selected
    agent is a failed check (status 1).

**128**+*N*
:   Stopped by signal *N*.

Otherwise **opencode** and **codex** exit with the agent's own status, as
env(1) and timeout(1) do.

## TROUBLESHOOTING

For a single-agent installation, run **doctor codex** or **doctor opencode**.
Doctor checks tool availability and offline shell probes, not login, agent
resolution or the full session policy. Read the first FAIL entry or launch
error before changing access rules.

If a Mise shim cannot resolve an agent, run **mise which codex** or
**mise which opencode** from the project directory. If a writable grant
overlaps a protected launch location, check the two paths in the error and
move the executable/state or narrow the grant. Adding write access cannot
fix that refusal.

For login or network problems, check the session report for a matching
refusal. A service's error alone does not establish a sandbox denial.
Apply needed allowances with **config** from a normal terminal, outside the
sandbox, then restart the affected agent.

**config** *AGENT* **show** lists user additions only. **nono why --profile**
*PROFILE* checks that named profile, not the full session policy. PROFILE is
a base name such as codex-chatgpt, an external NONO_PROFILE name/path, or
a user profile such as codex-user. Runtime and launch-time grants can differ.
**nono audit show** *SESSION_ID* shows recorded activity, not every filesystem
access. If the report is unavailable, use its printed audit-list command.

For wrapper help, use **nono-wrapper help codex**; **nono-wrapper codex --help**
runs Codex's help under the sandbox. Native nono flags belong to **nono run**.

## FILES

*$XDG_CONFIG_HOME/nono/profiles/*
:   The installed profiles and the user files opencode-user.jsonc and
    codex-user.jsonc.

## EXAMPLES

Start OpenCode in the current project:

    nono-wrapper opencode

Allow one more host for Codex:

    nono-wrapper config codex allow-domain api.github.com

Allow a remote MCP HTTPS endpoint (use the server's actual path):

    nono-wrapper config codex allow-domain 'https://mcp.example.com/mcp'

Check what changed during a session:

    marker=$(mktemp); nono-wrapper codex;
    nono-wrapper changes "$marker" .; rm -f "$marker"

## SEE ALSO

nono(1), opencode(1), codex(1)

Documentation: <https://github.com/diggsweden/nono-wrapper>  
Issues: <https://github.com/diggsweden/nono-wrapper/issues>
