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

