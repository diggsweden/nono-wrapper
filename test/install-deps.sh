#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Install what the suite needs, then add the unprivileged test user. Runs as
# root during the image build.
set -eu

# shellcheck source=/dev/null
. /etc/os-release

case "$ID" in
ubuntu | debian)
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates git libstdc++6
  apt-get clean
  ;;
fedora)
  dnf install -y --setopt=install_weak_deps=False curl ca-certificates findutils shadow-utils git libstdc++
  dnf clean all
  ;;
opensuse-leap | opensuse-tumbleweed)
  zypper --non-interactive install --no-recommends curl ca-certificates findutils shadow git libstdc++6
  zypper clean --all
  ;;
arch)
  pacman -Syu --noconfirm --needed curl ca-certificates git gcc-libs
  pacman -Scc --noconfirm
  ;;
*)
  echo "install-deps: unsupported distribution: $ID" >&2
  exit 1
  ;;
esac

useradd --create-home --uid 4242 tester
