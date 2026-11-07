<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Check and review

## Check the installation

After installing or upgrading:

```sh
nono-wrapper doctor codex    # Codex-only installation
nono-wrapper doctor         # both agents and their offline profiles
```

It first reports the setup: the nono version and the paths of the selected agents,
whether the [project root](usage.md#where-an-agent-may-start) exists, and
whether the proxy setting can be used. A missing project root or an unusable
proxy setting fails verification, because every launch would stop on it.
No arguments selects both agents; a missing selected agent or an unusable
nono executable also fails. Agents are resolved through symlinks and Mise
shims without executing them; an inactive Mise agent fails the check.
This is still not a full agent launch check.

Then it runs probe commands under the profiles without an agent login or model.
Each resolved offline profile must set `network.block` to `true`; a host
allowlist that merely refuses the sample destination is not an offline check.
Success means the installed offline-profile checks passed; ChatGPT login,
your additional access rules and the full agent launch were not tested.
It checks successful project/temp writes alongside refused home/config
listing and networking. The configuration check uses `XDG_CONFIG_HOME`,
falling back to `~/.config`. It uses a private temporary directory and stops
checking a profile if a positive control fails. The full container suite also
tests the wrapper, representative inherited grants and ChatGPT allowlists;
see [Development](development.md#container-integration).

## Troubleshooting

Start with the first failure, before changing permissions. Wrapper startup
errors appear before the agent starts; the post-exit report describes nono's
recorded session activity. An agent or service error alone does not establish
a sandbox denial.

| Symptom | First check and next step |
| --- | --- |
| The agent does not start | Read the wrapper error. Check the named executable on `PATH`, the selected profile, or the [start directory](usage.md#where-an-agent-may-start), as indicated. For a Mise shim, run `mise which codex` or `mise which opencode` from that project. |
| `doctor` passes but launch fails | Doctor uses offline shell probes, not the full agent launch. Check the launch error for agent resolution, state-directory or [protected-path](usage.md#what-a-launch-prepares-and-refuses) failures. |
| A writable grant overlaps a protected launch location | Read both paths in the error. Check the workspace, `NONO_ALLOW`, user path grants and relocated state. Move the executable/state or narrow the grant; adding write access cannot fix this refusal. |
| Login fails or a service is unreachable | Check the session report for a matching network refusal. If there is none, check the agent's login/provider and the service response. A 403 alone does not identify who refused the request. See [login](usage.md#first-time-chatgpt-login) and [network allowances](configuration.md#network-allowance-matrix). |
| A file cannot be read or written | Establish the exact path and operation. Check whether it is in the workspace or covered by a user or inherited grant. Start with read-only access when reading is all that is needed; see [user configuration](configuration.md#user-configuration). |
| A linked `AGENTS.md` cannot be read | The target may be outside the granted directory. Use `config AGENT add-file PATH` to save a read-only grant for the resolved file, then restart. See [linked instruction files](configuration.md#linked-instruction-files). |
| An SSH agent or other Unix socket is refused | Inside the session, use `nono why --self --unix-socket /absolute/path/to/socket`. It checks socket connection access; `--path` checks filesystem access instead. A named base profile does not include the wrapper's SSH-agent grant. |
| An OpenCode 2 client cannot connect or authenticate | Start `serve` under the wrapper, allow its chosen TCP port, and pass the same server password through `OPENCODE_PASSWORD` in the client's allowed environment. Restart the server after policy changes; see [local-server setup](opencode.md). |
| Normal OpenCode 2 startup fails with a namespace error | Run `doctor opencode`; the Linux standalone workflow requires permitted user/network namespaces. Resolve the reported AppArmor/kernel/container restriction or use deliberate explicit-server mode. There is no weaker automatic fallback. See [host requirements](opencode.md#host-requirements). |
| OpenCode 2 private networking rejects port grants | Remove old `open_port`, `connect_port`, `listen_port` and `no_proxy` rules from the effective base/user policy, including temporary browser-login grants. Host networking and private loopback are separate modes. |
| An allowance change has no effect | Confirm the agent named in `config`, inspect its user additions with `config AGENT show`, and restart the session. Broader grants or deny rules may still determine access. |
| The session report is unavailable | Use the printed `nono audit list` command to locate the recorded session. A reporting failure does not change the agent's exit status. |

### Which policy does a diagnostic inspect?

- `nono-wrapper config codex show` lists **user additions**, not all access.
- `nono profile show codex-chatgpt` shows that **base profile** with its
  inheritance. For another selected base, use its `NONO_PROFILE` name/path.
- `nono why --profile codex-chatgpt --host example.com` checks that named
  profile only. Use `--path /absolute/path --op read` for a file check, or
  `codex-user` to inspect user policy separately.
- `nono audit show SESSION_ID` shows **recorded activity** from a session,
  not every permission or filesystem access.

nono 0.81.0 also provides socket-specific diagnostics inside a session:

```sh
nono why --self --unix-socket "$SSH_AUTH_SOCK"
nono why --self --unix-socket /absolute/path/to/service.sock --socket-op bind
```

`unmediated` means nono is not filtering that socket operation in this
configuration, as with the WSL2 compatibility mode. It is not a confirmation
that the host will allow the operation. `unknown` can mean the session was
started by an older nono; restart before querying again.

Use `opencode` instead of `codex` for OpenCode. A named-profile check does
not reconstruct the full session policy: user composition, generated runtime
grants and launch-time additions can change access. The wrapper's report
labels these diagnostics accordingly.

For wrapper help, use `nono-wrapper help codex`; `nono-wrapper codex --help`
launches Codex's help under the sandbox. Native nono flags belong to
`nono run`, not the wrapper's agent commands. Make persistent changes with
`nono-wrapper config` from a normal terminal and restart the agent.

## Review a session

Use nono's [session audit](https://nono.sh/docs/cli/features/audit) for
recorded activity. nono's
[rollback snapshots](https://nono.sh/docs/cli/features/atomic-rollbacks) are
not available through the wrapper: `nono-wrapper run` does not accept
`--rollback`, and a plain `nono run` session lacks the wrapper's profiles,
start-directory guard and report. Review project changes with Git and
[`nono-wrapper changes`](#selected-file-change-review) instead.

The wrapper's own post-exit report is described under
[Automatic session report](usage.md#automatic-session-report).

## Selected-file change review

For lightweight reminders, `nono-wrapper changes MARKER [DIRECTORY]` reports
changed project hooks/configuration, editor tasks, CI workflows, package
scripts and agent settings for review before later host execution. This
includes mise's shared/local configuration and lockfiles.

Create a marker before your session and run the helper afterwards. This block
works from Fish or Bash:

```sh
sh -c '
marker=$(mktemp)
nono-wrapper codex
nono-wrapper changes "$marker" .
rm -f "$marker"
'
```

This is a timestamp-based aid, not enforcement or a complete change log. It
does not report all global state changes, restore files or detect deletions.
It compares file change times (ctime) with the marker's modification time,
and exits 1 when selected changes are listed and 0 when none are found. When
the check cannot run it exits with a failure status from the
[exit-status table](usage.md#exit-status): 66 for a missing or non-regular
marker, 77 when a file cannot be read, 70 otherwise.
Reported filenames are quoted, with control characters and newlines escaped.
For worktrees and `.git` or `.git/hooks` symlinks, it reports pointer changes
without following the pointer to shared Git metadata, and scans project
files. The automatic session report describes nono's recorded activity. This
selected-file review and marker cleanup remain separate manual steps.
