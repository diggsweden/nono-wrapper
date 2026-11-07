#!/bin/sh

# SPDX-FileCopyrightText: The nono-wrapper Authors
# SPDX-License-Identifier: CC0-1.0

# These images and tool archives are native Linux amd64 inputs. Ignore ambient
# remote connections: this suite validates the local rootless engine.
podman() {
  command podman --remote=false "$@"
}

engine=$(podman info --format '{{.Host.OS}}/{{.Host.Arch}} {{.Host.Security.Rootless}}') || exit 1
if [ "$engine" != 'linux/amd64 true' ]; then
  printf 'container tests require a local rootless linux/amd64 engine; got %s\n' "$engine" >&2
  exit 1
fi
printf '== Podman engine: %s (local)\n' "$engine"

require_native_image() {
  platform=$(podman image inspect --format '{{.Os}}/{{.Architecture}}' "$1") || return 1
  if [ "$platform" != linux/amd64 ]; then
    printf 'image %s must be linux/amd64; got %s\n' "$1" "$platform" >&2
    return 1
  fi
}
