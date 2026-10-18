#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Print or check the project-owned inputs baked into a test image.
# Format: base reference, distribution name, then relative-path SHA-256 records.
# Usage: image-inputs.sh DISTRIBUTION | image-inputs.sh --check MANIFEST [EXPECTED]
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}

image_inputs() {
  target=$1
  reference=
  while IFS='=' read -r name value; do
    case $name in '' | \#*) continue ;; esac
    if [ "$name" = "$target" ]; then
      reference=$value
      break
    fi
  done <"$root/test/images"
  if [ -z "$reference" ]; then
    printf "image-inputs: unknown distribution '%s'; see test/images\n" "$target" >&2
    return 2
  fi
  printf '%s\n%s\n' "$reference" "$target"
  (
    cd "$root"
    sha256 .mise.toml mise.lock test/Containerfile test/install-deps.sh test/install-packs.sh test/prepare.sh test/image-inputs.sh test/require-linux.sh
  )
}

if { [ "$#" -eq 2 ] || [ "$#" -eq 3 ]; } && [ "$1" = --check ]; then
  manifest=$2
  if [ ! -f "$manifest" ]; then
    echo "container: image build record missing; rebuild with just test-prepare" >&2
    exit 1
  fi
  if ! { IFS= read -r _base && IFS= read -r target; } <"$manifest"; then
    echo "container: invalid image build record; rebuild with just test-prepare" >&2
    exit 1
  fi
  if [ "$#" -eq 3 ]; then
    if [ ! -f "$3" ]; then
      echo "container: expected image inputs missing; use just test-containers" >&2
      exit 1
    fi
    expected=$(cat -- "$3")
  else
    expected=$(image_inputs "$target")
  fi
  actual=$(cat -- "$manifest")
  if [ "$expected" != "$actual" ]; then
    printf 'container: image build inputs changed; rebuild with just test-prepare %s\n' "$target" >&2
    exit 1
  fi
elif [ "$#" -eq 1 ] && [ "$1" != --check ]; then
  image_inputs "$1"
else
  echo "usage: image-inputs.sh DISTRIBUTION | image-inputs.sh --check MANIFEST [EXPECTED]" >&2
  exit 2
fi
