#!/bin/sh

# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Image-build step, run as tester with HOME=/home/tester. nono verifies the
# registry signatures and installs the upstream profiles and agent wiring.
set -eu

while read -r name version; do
  case $name in '' | \#*) continue ;; esac
  nono pull "$name@$version"
  nono pin "$name"
done </usr/local/share/nono-wrapper/packs
