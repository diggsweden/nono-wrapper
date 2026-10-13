#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Cache pinned archives in .cache/tools and install verified tools into one
# image per selected distribution. just supplies the routine matrix; no
# arguments or `all` builds every image in the manifest. This step uses the
# network. test/containers.sh runs offline by default.
#
#   test/prepare.sh [all | distribution...]
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
# shellcheck source=test/require-linux.sh
. "$root/test/require-linux.sh"

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}
[ "$*" != all ] || set --
# Reject typos before compiling or downloading anything. The manifest owns names.
for target in "$@"; do
  found=false
  while IFS='=' read -r name reference; do
    case $name in '' | \#*) continue ;; esac
    if [ "$name" = "$target" ]; then
      found=true
      break
    fi
  done <"$root/test/images"
  if [ "$found" = false ]; then
    printf "prepare: unknown distribution '%s'; see test/images or use all\n" "$target" >&2
    exit 2
  fi
done
cache=$root/.cache/tools
if [ -L "$root/.cache" ] || [ -L "$cache" ]; then
  echo "prepare: refusing symlinked cache directories" >&2
  exit 1
fi
mkdir -p "$cache"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/agent-prepare.XXXXXX")
scratch=$(CDPATH='' cd -- "$scratch" && pwd -P)
readonly scratch
trap 'rm -rf -- "${scratch:?}"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
tools=$scratch/tools
mkdir "$tools"

# Build the wrapper and standard-library-only test helper as static executables.
# Cross-compile for the same Linux x86_64 target as the pinned agent tools.
command -v go >/dev/null 2>&1 || {
  echo "prepare: Go 1.27.2 or newer is required; run just install-dev-tools" >&2
  exit 1
}
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOENV=off GOFLAGS='' GOTOOLCHAIN=local \
  go -C "$root" build -mod=readonly -trimpath -buildvcs=false -ldflags '-s -w -buildid=' \
  -o "$tools/nono-wrapper" ./cmd/nono-wrapper
(cd "$root" && sha256 go.mod go.sum cmd/nono-wrapper/*.go) >"$tools/nono-wrapper.sources"

probe_sha=$(sha256 "$root/test/probe.go")
probe_sha=${probe_sha%% *}
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOENV=off GOFLAGS='' GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.sourceSHA=$probe_sha" \
  -o "$tools/agent-probe" "$root/test/probe.go"

grep -v -e '^#' -e '^$' "$root/test/tools" | while read -r name tag sum; do
  case $name in
  nono) url=https://github.com/nolabs-ai/nono/releases/download/$tag/nono-$tag-x86_64-unknown-linux-musl.tar.gz ;;
  codex) url=https://github.com/openai/codex/releases/download/$tag/codex-x86_64-unknown-linux-musl.tar.gz ;;
  opencode) url=https://github.com/anomalyco/opencode/releases/download/$tag/opencode-linux-x64-baseline.tar.gz ;;
  *)
    echo "prepare: no download location known for $name" >&2
    exit 1
    ;;
  esac

  archive=$scratch/$name.tar.gz
  cached=$cache/$name.tar.gz
  if [ -L "$cached" ] || { [ -e "$cached" ] && [ ! -f "$cached" ]; }; then
    echo "prepare: cache archive must be a regular file: $cached" >&2
    exit 1
  fi
  # Verify and extract the same private copy; the shared cache is not a build input.
  if [ -f "$cached" ]; then cp "$cached" "$archive"; fi
  if [ ! -f "$archive" ] || ! echo "$sum  $archive" | sha256 -c - >/dev/null 2>&1; then
    echo "== download $name $tag"
    curl -fsSL --retry 3 -o "$archive" "$url"
    echo "$sum  $archive" | sha256 -c - >/dev/null || {
      echo "prepare: checksum mismatch for $name" >&2
      exit 1
    }
    install -m 0644 "$archive" "$cached"
  fi

  unpacked=$scratch/$name
  mkdir "$unpacked"
  tar -xzf "$archive" -C "$unpacked"
  # Select the executable, not whichever archive file happens to come first.
  binary=$(find "$unpacked" -type f \( -name "$name" -o -name "$name-x86_64-unknown-linux-musl" \))
  [ -f "$binary" ] || {
    echo "prepare: expected exactly one $name executable in the archive" >&2
    exit 1
  }
  install -p -m 0755 "$binary" "$tools/$name"
done

grep -v -e '^#' -e '^$' "$root/test/images" | while IFS='=' read -r name reference; do
  if [ $# -gt 0 ]; then
    case " $* " in *" $name "*) ;; *) continue ;; esac
  fi

  sh "$root/test/image-inputs.sh" "$name" >"$tools/image.inputs"
  # Use the recorded reference for FROM as well as the later freshness check.
  IFS= read -r reference <"$tools/image.inputs"
  echo "== $name ($reference)"
  podman build --quiet --build-arg "BASE_IMAGE=$reference" --build-context "tools=$tools" \
    --tag "localhost/nono-agent-profiles:$name" --file "$root/test/Containerfile" "$root/test" >/dev/null
done
