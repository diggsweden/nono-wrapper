#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Run the suite in selected distribution images, offline. A failing
# distribution does not stop the others. just supplies the routine matrix;
# no arguments or `all` selects every image in the manifest.
# Missing or stale images fail with a preparation hint. Builds and downloads
# belong to the explicit test-prepare step, including for network-enabled runs.
#
#   test/containers.sh [all | distribution...]
#
# With AGENT_TEST_NETWORK=1 the containers get a network and host-list checks.
# All executables come from the image itself.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
# shellcheck source=test/require-linux.sh
. "$root/test/require-linux.sh"
failed=""

network=--network=none
if [ "${AGENT_TEST_NETWORK:-}" = 1 ]; then
  network=""
fi

[ "$*" != all ] || set --
names=${*:-$(grep -v -e '^#' -e '^$' "$root/test/images" | cut -d= -f1)}

scratch=$(mktemp -d "${TMPDIR:-/tmp}/agent-image-check.XXXXXX")
scratch=$(CDPATH='' cd -- "$scratch" && pwd -P)
readonly scratch

# Ctrl-C must end the run. The suite is the container's first process, and the
# kernel discards signals that PID 1 has no handler for, so a forwarded signal
# does not stop it. Remove the container from here instead.
stop_container() {
  [ ! -s "$scratch/container" ] ||
    podman rm --force --time 0 --ignore --cidfile "$scratch/container" >/dev/null 2>&1 || :
}
trap 'stop_container; rm -rf -- "${scratch:?}"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# shellcheck disable=SC2329
in_image() {
  name=$1
  shift
  # Hash as the caller: mise.lock may be mode 0600. Only public hashes need
  # to be readable by the container's unprivileged test user.
  sh "$root/test/image-inputs.sh" "$name" >"$scratch/expected.inputs" || return 1
  chmod 0644 "$scratch/expected.inputs" || return 1
  rm -f -- "$scratch/container"
  # Wait in the shell, not in podman: sh runs a trap only between commands.
  # shellcheck disable=SC2086 # Empty when the network is on.
  podman run --rm --cidfile "$scratch/container" --pull=never $network --cap-drop=ALL \
    --security-opt no-new-privileges --security-opt label=disable \
    --volume "$root:/src:ro" \
    --volume "$scratch/expected.inputs:/run/nono-agent-profiles/expected.inputs:ro" \
    --env "AGENT_TEST_NETWORK=${AGENT_TEST_NETWORK:-}" \
    "localhost/nono-agent-profiles:$name" sh /src/test/container-suite.sh "$@" &
  status=0
  wait "$!" || status=$?
  rm -f -- "$scratch/container"
  return "$status"
}

for name in $names; do
  echo
  echo "######## $name"

  podman image exists "localhost/nono-agent-profiles:$name" || {
    echo "no image for $name; run just test-prepare $name first" >&2
    failed="$failed $name"
    continue
  }

  in_image "$name" || failed="$failed $name"
done

echo
if [ -n "$failed" ]; then
  echo "FAILED:$failed"
  exit 1
fi

echo "all selected distributions passed"
