<!--
SPDX-FileCopyrightText: The nono-agent-profiles Authors

SPDX-License-Identifier: CC0-1.0
-->

# Verify and review

## Verify the installed profiles

After installing or upgrading:

```sh
nono-wrapper verify          # both installed offline profiles
nono-wrapper verify codex
```

It first reports the setup: the versions of nono and the agents it finds,
whether the [project root](usage.md#where-an-agent-may-start) exists, and
whether the proxy setting can be used. A missing project root or an unusable
proxy setting fails verification, because every launch would stop on it.

Then it runs probe commands under the profiles without an agent login or model.
Success means the installed offline-profile checks passed; ChatGPT login and
your additional access rules were not tested.
It checks successful project/temp writes alongside refused home/config
listing and networking. The configuration check uses `XDG_CONFIG_HOME`,
falling back to `~/.config`. It uses a private temporary directory and stops
checking a profile if a positive control fails. The full container suite also
tests the wrapper, representative inherited grants and ChatGPT allowlists;
see [Development](development.md#container-integration).

## Review a session

Use nono's [session audit](https://nono.sh/docs/cli/features/audit) for
recorded activity and its
[rollback features](https://nono.sh/docs/cli/features/atomic-rollbacks) for
snapshot-based change review and restoration. Enable snapshots through
`nono run --rollback ...`; `.git` is excluded by default, and
`--rollback-include .git` explicitly includes it.

The wrapper's own post-exit report is described under
[Automatic session report](usage.md#automatic-session-report).

## Selected-file change review

For lightweight reminders, `nono-wrapper audit MARKER [DIRECTORY]` reports
changed project hooks/configuration, editor tasks, CI workflows, package
scripts and agent settings for review before later host execution. This
includes mise's shared/local configuration and lockfiles.

Create a marker before your session and run the helper afterwards. This block
works from Fish or Bash:

```sh
sh -c '
marker=$(mktemp)
nono-wrapper codex
nono-wrapper audit "$marker" .
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
