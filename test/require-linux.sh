#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
# SPDX-License-Identifier: CC0-1.0

# The image/tool pins describe Linux x86_64. Native unit tests are separate.
case "$(uname -s):$(uname -m)" in
Linux:x86_64 | Linux:amd64) ;;
*)
  echo 'container integration requires a Linux x86_64 controller; use a Linux VM. Run just verify for native host checks.' >&2
  exit 2
  ;;
esac
