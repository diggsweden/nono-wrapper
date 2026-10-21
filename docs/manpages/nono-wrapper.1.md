# NONO-WRAPPER 1 "User Commands"

## NAME

nono-wrapper - run OpenCode and Codex with nono's sandbox profiles

## SYNOPSIS

**nono-wrapper** **opencode**|**codex** [*agent-argument*...]  
**nono-wrapper** **config** **opencode**|**codex** *subcommand* [*argument*]  
**nono-wrapper** **verify** [**opencode**|**codex**...]  
**nono-wrapper** **audit** *MARKER* [*DIRECTORY*]  
**nono-wrapper** **man**  
**nono-wrapper** **--help**|**--version**

## DESCRIPTION

**nono-wrapper** starts an AI coding agent inside nono's sandbox. The current
directory is the writable workspace; the rest of the home directory, other
projects and unlisted network hosts stay out of reach.

The profile is **NONO_PROFILE**, or *AGENT*-chatgpt, plus the optional
*AGENT*-user.jsonc in the nono profiles directory. After the agent exits, the
wrapper prints a summary of the session's audit record on standard error.

On macOS, the bundled profiles deny Apple Events and access to their Mach
broker. The wrapper refuses to fall back to the official agent profile when
the bundled profile is missing, because that would omit these restrictions.
Custom **NONO_PROFILE** policies must inherit **agent-policy** or provide
equivalent restrictions. Reinstall profiles and restart sessions to apply
policy updates.

On WSL2 nono cannot mediate Unix sockets, so the wrapper starts the bundled
profiles without **linux.af_unix_mediation** and says so on standard error:
the agent can then connect to any Unix socket the user can. The minimum
supported releases are WSL 2.7.7 and Ubuntu 26.04; neither is checked.

## COMMANDS

**opencode** [*agent-argument*...], **codex** [*agent-argument*...]
:   Start the agent under nono. Every argument, including **--help**, goes to
    the agent. If the agent on **PATH** is a mise shim, the wrapper asks
    **mise which** for the real executable.

**config** **opencode**|**codex** **show**
:   List the hosts, directories and files the user has added.

**config** **opencode**|**codex** **add-host**|**remove-host** *HOST*
:   Allow a host, or remove the user allowance for it. *HOST* is a plain name
    such as api.github.com, without a scheme, path, wildcard or port.

**config** **opencode**|**codex** **add-dir** [**--read-write**] *PATH*, **remove-dir** *PATH*
:   Allow a directory, read-only unless **--read-write**, or remove the user
    allowance for it. The path must exist and is stored as an absolute path.

**config** **opencode**|**codex** **add-file** [**--read-write**] *PATH*, **remove-file** *PATH*
:   The same for a single file.

Configuration changes apply to new sessions and must be made from a normal
terminal, outside a nono session.

**verify** [**opencode**|**codex**...]
:   Run short probes under each agent's offline profile. The project and the
    temporary directory must be writable; the home directory, the
    configuration directory and the network must be refused.

**audit** *MARKER* [*DIRECTORY*]
:   List hooks, CI workflows, editor tasks, package scripts and agent or tool
    settings in *DIRECTORY* (default: the current directory) that changed after
    *MARKER* was last modified. Create *MARKER* before a session, for example
    with mktemp.

**man**
:   Print this manual, for systems without man(1).

## OPTIONS

**-h**, **--help**
:   Show help for the wrapper or for one of its commands.

**-v**, **--version**
:   Show the build and Go toolchain version.

## ENVIRONMENT

**NONO_PROFILE**
:   Profile to use instead of *AGENT*-chatgpt.

**NONO_WRAPPER_SSH_AGENT=0**
:   Do not share the SSH agent socket with the sandbox.

**HTTPS_PROXY**, **HTTP_PROXY**
:   Upstream proxy for the sandbox's network proxy.

## EXIT STATUS

**0**
:   Success.

**1**
:   **verify**: a check failed; **audit**: changed files are listed.

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
:   **opencode**, **codex**: the wrapper could not start the session.

**127**
:   nono, the agent or a required tool is not on **PATH**.

**128**+*N*
:   Stopped by signal *N*.

Otherwise **opencode** and **codex** exit with the agent's own status, as
env(1) and timeout(1) do.

## FILES

*$XDG_CONFIG_HOME/nono/profiles/*
:   The installed profiles and the user files opencode-user.jsonc and
    codex-user.jsonc.

## EXAMPLES

Start OpenCode in the current project:

    nono-wrapper opencode

Allow one more host for Codex:

    nono-wrapper config codex add-host api.github.com

Check what changed during a session:

    marker=$(mktemp); nono-wrapper codex; nono-wrapper audit "$marker" .; rm -f "$marker"

## SEE ALSO

nono(1), opencode(1), codex(1)

Documentation: <https://codefloe.com/itiquette/nono-wrapper>  
Issues: <https://codefloe.com/itiquette/nono-wrapper/issues>
