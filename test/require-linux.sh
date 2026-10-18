#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
# SPDX-License-Identifier: CC0-1.0

# The image/tool pins describe Linux x86_64. Run them on a native x86_64 host,
# not under CPU emulation. Native unit tests are separate.
case "$(uname -s):$(uname -m)" in
Linux:x86_64 | Linux:amd64) ;;
*)
  echo 'container integration requires a Linux x86_64 controller; run it on a native Linux x86_64 host (no CPU emulation). Run just verify for native host checks.' >&2
  exit 2
  ;;
esac
