#!/usr/bin/env bash
# SPDX-FileCopyrightText: The nono-agent-profiles Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

expected=(
  .cache/bin/nono-wrapper
  .cache/man/nono-wrapper.1
  profiles/agent-policy.jsonc
  profiles/codex-base.jsonc
  profiles/codex-chatgpt.jsonc
  profiles/codex-offline.jsonc
  profiles/opencode-base.jsonc
  profiles/opencode-chatgpt.jsonc
  profiles/opencode-offline.jsonc
)
compare() {
  local first=$1 second=$2 directory file
  local -a profiles
  shopt -s nullglob
  for directory in "$first" "$second"; do
    profiles=("$directory"/profiles/*.jsonc)
    if ((${#profiles[@]} != 7)); then
      printf 'Expected seven shipped profiles in %s\n' "$directory" >&2
      return 1
    fi
    for file in "${expected[@]}"; do
      if [[ ! -f $directory/$file || ! -s $directory/$file || -L $directory/$file ]]; then
        printf 'Missing or empty build file: %s/%s\n' "$directory" "$file" >&2
        return 1
      fi
    done
  done
  for file in "${expected[@]}"; do
    if ! cmp -s -- "$first/$file" "$second/$file"; then
      printf 'Different: %s and %s\n' "$first/$file" "$second/$file" >&2
      return 1
    fi
  done
  printf '9 build files are byte-identical (binary, manpage, seven profiles)\n'
}

if [[ $# == 3 && $1 == --compare ]]; then
  compare "$2" "$3"
  exit
elif (($# != 0)); then
  printf 'Usage: %s [--compare FIRST_CHECKOUT SECOND_CHECKOUT]\n' "$0" >&2
  exit 2
fi
root=$(git rev-parse --show-toplevel)
if [[ -n $(git -C "$root" status --porcelain --untracked-files=normal) ]]; then
  printf 'Use a clean committed checkout; local edits are not included in the rebuild.\n' >&2
  exit 2
fi
commit=$(git -C "$root" rev-parse HEAD)
export GOTOOLCHAIN=local GOWORK=off GOENV=off GOFLAGS=-mod=readonly
export CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 TZ=UTC LC_ALL=C GOPROXY=off GOSUMDB=off
export GOOS GOARCH
GOOS=$(go env GOHOSTOS)
GOARCH=$(go env GOHOSTARCH)
go version
printf 'Source: %s; native target: %s/%s; module downloads: disabled\n' "$commit" "$GOOS" "$GOARCH"
work=$(mktemp -d "${TMPDIR:-/tmp}/nono-wrapper-repro.XXXXXXXX")
cleanup() {
  local status=$?
  if ((status == 0)); then
    rm -rf -- "$work"
  else
    printf 'Build/comparison failed. Outputs retained in %s (inspect with diffoscope).\n' "$work" >&2
  fi
}
trap cleanup EXIT
for pass in first second; do
  git clone --quiet --no-hardlinks --no-checkout "$root" "$work/$pass"
  git -C "$work/$pass" checkout --quiet --detach "$commit"
  (
    cd "$work/$pass"
    export GOCACHE="$work/$pass-cache"
    just build generate-manpage
  )
done
compare "$work/first" "$work/second"
