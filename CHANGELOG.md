<!--
SPDX-FileCopyrightText: The nono-wrapper Authors

SPDX-License-Identifier: CC0-1.0
-->

# Changelog

All notable changes to nono-wrapper are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The policy choices originated in
[occwrap](https://codefloe.com/itiquette/occwrap). This repository extends the
official nono agent packs rather than maintaining replacement filesystem
profiles.

## [Unreleased]

### Fixed

- Permit Go module extraction, Cargo dependency caches and exact lock/database
  files, and Gradle cache/daemon writes by default, including relocated roots.
  Keep installed tools, ordinary configuration and dedicated credentials protected.
- Continue independent container preparation lanes after a failure and report
  aggregate failure. Require local rootless Linux amd64 engine/image agreement.
- Replace the unavailable Tumbleweed input with the verified 20261103 digest.
- Permit Gradle wrapper distribution locks within `wrapper/dists`, including
  relocated Gradle homes; retain read-only ordinary configuration and document
  the accepted executable-cache risk.
- Read Linux dpkg's exact configuration file/snippets and allow exact vulnerability, Gradle
  distribution and documentation hosts, without broad filesystem or network grants.
- Keep network I/O failures visible without misleading host-grant suggestions.

### Added

- Report the wrapper executable/version/hash, nono path, resolved offline-profile
  hashes and Podman transport selection in doctor output.
- Use private loopback automatically for Linux Codex offline sessions. Preserve
  online browser callbacks. Test real Go dependency builds/local HTTP servers and
  Cargo dependency builds with credential and host-listener negative controls.
- Allow exact Docker Hub authentication, Google downloads, Debian and Ubuntu
  archive hosts in online profiles; offline access remains blocked.
- Include exact public app-store, Maven, Forgejo, observability, identity and
  security/documentation hosts in both online agents' defaults, without adding
  wildcards, direct TCP grants or offline access.
- Pass native `--isolate` options through the existing `run` command, supporting
  private-loopback test sessions without host-port grants or automatic fallback.
- Include the requested exact Zig community mirrors, unofficial Node builds,
  JSR npm endpoint, documentation and REUSE API hosts in both online agents'
  defaults; keep wildcard expansion and offline networking disabled.
- Allow exact Linux nscd socket connections for both agents' name-service lookups,
  without granting parent directories or other Unix sockets.
- Share Nanolinter's HOME/XDG data directory read-only, including link-check
  defaults and cached configuration. Allow exact Rust/crate/Python download
  endpoints and Sigstore trust-root metadata in online profiles.
- Share Whdkit configuration read-only and prepare its dedicated data/state
  directories read/write for both agents, using XDG paths and HOME fallbacks
  without granting the enclosing configuration, data or state roots.
- Allow exact npm/Cargo indexes, Zig downloads, Semantic Versioning reference
  and Docker Hub, Fedora, openSUSE and Google container registry hosts in both
  online agents' defaults; keep offline networking blocked.
- Allow reviewed documentation, licensing, contributor/asset attribution and
  badge hosts, the Go module proxy and exact OSS-Fuzz build-log host by default
  for both online agents. Offline profiles remain blocked.
- Automatically read existing validated `~/.ssh/*.pub` public keys for
  agent-backed Git signing. Skip redirected, malformed and private-key files;
  keep private keys and the SSH directory denied.
- Support optional policy/application/rule descriptions in wrapper runtime
  configuration, retaining concise string entries and native-profile compatibility.
- Declare runtime mappings and environment defaults in JSONC, with validation
  and consistency checks against native profiles rather than Go grant tables.
- Add exact pip distro metadata, trusted binfmt/public CA and read-only pipx
  runtime access, ordinary app configuration, and scoped prepared Gradle state.
- Add reviewed DevBase compiler-helper, header, PTY and read-only shared-lint
  defaults. Preserve safe environment settings, keep helper updates outside the
  sandbox and retain stricter caller release-age values. Record the application
  inventory, initial failures and fresh isolated native validation.
- Allow the current user's exact Linux rootless Podman socket by default for
  both agents, and select its remote transport automatically when available.
  This intentionally permits host-user access through the container API.
- Add `run` with temporary nono-style profile, path, network and reporting
  options, without changing argument forwarding in the existing shortcuts.
- Add persistent `allow`, `read`, `allow-file` and `read-file` commands, preferred
  `open-port`/`remove-open-port` names, and `-V` for version. Existing names and
  read-only `add-dir`/`add-file` defaults remain compatible.

### Fixed

- Allow exact Go checksum, Mise version, PyPI, Node.js and Semgrep rule hosts
  online while retaining offline blocking and avoiding wildcard grants.
- Prepare scoped user-manual and checksum-verified VM-media directories, allow
  explicit VM download cleanup, and read public Linux dpkg origin metadata.
- Permit multiprocessing Unix-socket bind/connect only in user-owned agent
  temp roots; prepare those roots, default an unset `TMPDIR`, and preserve
  explicit read-only overrides without authorizing all `/tmp` sockets.
- Prepare OpenGrep's exact HOME/XDG log files without truncation, rejecting
  symlink log files and multiple hard links while leaving account/settings
  siblings outside the grant.
- Prepare and grant only OpenGrep's writable cache, including custom
  `XDG_CACHE_HOME` locations, without granting its parent directory.
- Allow the exact `www.brailleinstitute.org` and `check.trivy.dev` online hosts;
  offline profiles remain blocked.
- Make scoped Mise, golangci-lint, Staticcheck and Trivy caches writable by
  default, including XDG and explicit overrides. Accept Mise's code-loading
  cache risk without allowing installation, configuration or trust changes.
- Read Nanolinter's exact installed rumdl, ShellCheck and yamlfmt defaults.
- Allow Linux `/dev/shm` for multiprocessing semaphores; document shared IPC risk.
- Read existing Mise trust hashes without making trust state writable, including
  XDG and `MISE_STATE_DIR` overrides.
- Permit scoped Go download-metadata and build/fuzz cache writes, with custom
  `GOPATH`, `GOMODCACHE` and `GOCACHE` mappings; keep tools/modules read-only.
- Read exact container registry config and Linux subordinate-ID mapping files;
  retain host rootless Podman service delegation without `/proc` write grants.
- Preserve documented missing-path and permission exit statuses when checking
  temporary directory grants.
- Skip redirected optional protection bypasses so config/plugin symlinks cannot
  exempt private keys; suggest `*-file` flags for mistaken directory grants.
- Replace broad Cargo/uv runtime reads with narrow code/config/cache reads that
  do not expose dedicated credential stores by default.
- Honor custom Just runtime roots without granting the parent directory.
- Refuse temporary path grants that widen an explicit read-only user allowance.
- Allow OpenCode 2's versioned model catalog endpoint by default.
- Suppress native failure footers and profile-save prompts that expose the
  internal OpenCode launcher; keep auditing and the wrapper's session report.
- Allow Just's `/run/user/$UID/just` script directory by default on Linux for
  both agents, without granting its parent runtime directory.
- Allow `opencode.ai` and the whole `chatgpt.com` host in the default OpenCode
  profile, including ChatGPT WebSocket connections.
