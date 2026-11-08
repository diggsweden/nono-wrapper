#!/bin/sh

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Sourced before any test writes. The installed packs have path-bound plugin
# wiring, so tests use the disposable container home where they were pulled.
if [ ! -f /run/.containerenv ] || [ "${HOME:-}" != /home/tester ]; then
  echo "tests require the disposable Podman image; use just test-containers" >&2
  exit 1
fi

# Tests use deliberately pinned tools and packs, including with no network.
# Avoid unrelated supervisor update queries delaying an offline test run.
export NONO_NO_UPDATE_CHECK=1 NONO_NO_PACK_UPDATE_HINTS=1
