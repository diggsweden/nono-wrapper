#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Container-only entry point. Require the pinned real tools before running
# any probes, so an old image or missing executable cannot pass by skipping.
# With `check`, only report whether the image matches the checkout.
#
#   test/container-suite.sh [check]
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
# shellcheck source=test/require-container.sh
. "$root/test/require-container.sh"
sh "$root/test/image-inputs.sh" --check /usr/local/share/nono-agent-profiles/image.inputs /run/nono-agent-profiles/expected.inputs
expected_manifest=$(sha256sum "$root/test/tools")
installed_manifest=$(sha256sum /usr/local/share/nono-agent-profiles/tools)
[ "${expected_manifest%% *}" = "${installed_manifest%% *}" ] || {
  echo "container: tool pins changed; rebuild with just test-prepare" >&2
  exit 1
}
expected_packs=$(sha256sum "$root/test/packs")
installed_packs=$(sha256sum /usr/local/share/nono-agent-profiles/packs)
[ "${expected_packs%% *}" = "${installed_packs%% *}" ] || {
  echo "container: official pack pins changed; rebuild with just test-prepare" >&2
  exit 1
}

grep -v -e '^#' -e '^$' "$root/test/tools" | while read -r name tag _sum; do
  case $name in
  nono) expected="nono ${tag#v}" ;;
  codex) expected="codex-cli ${tag#rust-v}" ;;
  opencode) expected=${tag#v} ;;
  *) echo "container: unknown tool $name" >&2; exit 1 ;;
  esac
  actual=$("/usr/local/bin/$name" --version)
  [ "$actual" = "$expected" ] || {
    echo "container: expected $expected, found $actual" >&2
    exit 1
  }
  echo "  installed: $name ($actual)"
done

# Verify the installed store, not just the copied build manifest.
command -v nono-wrapper >/dev/null 2>&1 || {
  echo "container: wrapper missing; rebuild with just test-prepare" >&2
  exit 1
}
expected_wrapper=$(cd "$root" && sha256sum go.mod go.sum cmd/nono-wrapper/*.go)
installed_wrapper=$(cat /usr/local/share/nono-agent-profiles/nono-wrapper.sources)
[ "$expected_wrapper" = "$installed_wrapper" ] || {
  echo "container: wrapper source or dependencies changed; rebuild with just test-prepare" >&2
  exit 1
}
command -v agent-probe >/dev/null 2>&1 || {
  echo "container: Go probe missing; rebuild with just test-prepare" >&2
  exit 1
}
expected_probe=$(sha256sum "$root/test/probe.go")
installed_probe=$(agent-probe source-sha256)
[ "${expected_probe%% *}" = "$installed_probe" ] || {
  echo "container: probe source changed; rebuild with just test-prepare" >&2
  exit 1
}
agent-probe packs "$root/test/packs" "$HOME/.config/nono/packages/lockfile.json"

[ "${1:-}" != check ] || exit 0
sh "$root/test/suite.sh"
