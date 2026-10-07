#!/usr/bin/env just --justfile

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# just's default shell is `sh -cu` -- no pipefail. Run every recipe line under
# bash with the full strict triad instead.
set shell := ["bash", "-euo", "pipefail", "-c"]
set positional-arguments

scripts := "test/suite.sh test/http-check.sh test/image-inputs.sh test/prepare.sh test/containers.sh test/container-suite.sh test/install-deps.sh test/install-packs.sh test/require-container.sh test/require-linux.sh"
routine := "ubuntu-26.04 opensuse-leap-16"
image_tag_format := "{{.Repository}}:{{.Tag}}"

[private]
default:
    @"{{ just_executable() }}" --justfile "{{ justfile() }}" --list --unsorted

# Announce the selected work before it starts.
_job name details:
    #!/usr/bin/env bash
    printf '== Job: %s | host=%s/%s\n   %s\n' "$1" "$(uname -s)" "$(uname -m)" "$2" >&2

[arg('recipe', help='Recipe name; omit to list recipes. Prints help without running the selected job.')]
[doc('Show recipe argument help')]
[group('help')]
help recipe="":
    #!/usr/bin/env bash
    if [[ -n "$1" ]]; then
        {{ quote(just_executable()) }} --justfile {{ quote(justfile()) }} --usage="$1"
    else
        {{ quote(just_executable()) }} --justfile {{ quote(justfile()) }} --list --unsorted
    fi

# Select this project's tools even when other tools are configured globally.
[doc('Install pinned tools')]
[group('setup')]
install-dev-tools: (_job "install-dev-tools" "tools=locked nono/OpenCode/Codex/Go/Just/ShellCheck; scope=user installation; network=allowed")
    mise --cd {{ quote(justfile_directory()) }} install --locked nono opencode codex go just aqua:koalaman/shellcheck

# Compile the host executable into the ignored build directory.
[doc('Build nono-wrapper')]
[group('build')]
build: (_job "build" "target=Go toolchain default; output=.cache/bin/nono-wrapper")
    mkdir -p {{ quote(justfile_directory() / ".cache/bin") }}
    GOTOOLCHAIN=local go -C {{ quote(justfile_directory()) }} build -mod=readonly -trimpath -buildvcs=false -o .cache/bin/nono-wrapper ./cmd/nono-wrapper

# Build and install the wrapper, signed official packs and our profiles.
# INSTALL_DIR / XDG_BIN_HOME follow the sibling projects' bin convention.
[doc('Install user; network')]
[group('install')]
install-local: build
    #!/usr/bin/env bash
    set -euo pipefail
    : "${HOME:?HOME must be set}"
    root={{ quote(justfile_directory()) }}
    bin_dir="${INSTALL_DIR:-${XDG_BIN_HOME:-$HOME/.local/bin}}"
    profile_dir="${XDG_CONFIG_HOME:-$HOME/.config}/nono/profiles"
    man_dir="${XDG_DATA_HOME:-$HOME/.local/share}/man/man1"
    for destination in "$bin_dir" "$profile_dir" "$man_dir"; do
        case "$destination" in
            /*) ;;
            *) printf 'Install destination must be absolute: %s\n' "$destination" >&2; exit 2 ;;
        esac
    done
    command -v nono >/dev/null 2>&1 || {
        printf 'nono is required; run mise install from this checkout.\n' >&2
        exit 127
    }
    while read -r name version; do
        case "$name" in '' | \#*) continue ;; esac
        nono pull "$name@$version"
        nono pin "$name"
    done < "$root/test/packs"
    mkdir -p "$bin_dir" "$profile_dir" "$man_dir"
    install -m 0755 "$root/.cache/bin/nono-wrapper" "$bin_dir/nono-wrapper"
    # The manual 'nono-wrapper man' prints, rendered for man(1).
    GOTOOLCHAIN=local go -C "$root" tool go-md2man -in docs/manpages/nono-wrapper.1.md -out "$man_dir/nono-wrapper.1"
    # User additions (opencode-user.jsonc and codex-user.jsonc) are not bundled.
    # Copy only the shipped profiles, preserving these optional user files.
    install -m 0644 "$root"/profiles/*.jsonc "$profile_dir/"
    for profile in opencode-chatgpt codex-chatgpt; do
        nono profile show "$profile" >/dev/null
    done
    printf 'Commands: %s\nProfiles: %s\nManual: %s\n' "$bin_dir" "$profile_dir" "$man_dir/nono-wrapper.1"
    printf 'Default profiles verified: opencode-chatgpt, codex-chatgpt\n'
    case ":$PATH:" in
        *":$bin_dir:"*) ;;
        *) printf 'Add %s to your PATH to run nono-wrapper.\n' "$bin_dir" ;;
    esac

# Everyday local checks; no containers or network required.
[doc('Run local checks')]
[group('quality')]
verify: (_job "verify" "checks=shell/Go lint + launcher/probe unit contracts; execution=native; network=none") lint test-unit

[doc('Format Go code')]
[group('quality')]
fmt:
    gofmt -w cmd/nono-wrapper test

# Check container scripts, Go formatting and Go diagnostics.
[doc('Check scripts and Go')]
[group('quality')]
lint:
    shellcheck {{ scripts }}
    test -z "$(gofmt -l cmd/nono-wrapper test)"
    GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go vet -mod=readonly ./...

# Isolated launcher contracts plus probe parsers, protocol and socket checks.
# Includes temporary install/cleanup fixtures; no agents, containers or network.
# Disable result caching: subprocess builds are outside Go's test dependency graph.
[arg('args', help='Arguments passed to go test, for example -v or -run TestName')]
[doc('Run unit tests')]
[group('test')]
test-unit *args: (_job "test-unit" "suite=launcher contracts + probe parsers/protocol/socket checks; execution=native isolated fixtures; network=none")
    GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly -count=1 "$@" ./...

# The test home contains upstream plugin wiring, so all agent tests run in
# disposable containers. Host installations and settings are not used.
[doc('Run offline tests')]
[group('test')]
test: (_job "test" ("suites=unit + agent/profile container checks; target=Linux/x86_64; distributions=" + routine + "; runtime-network=" + (if env("AGENT_TEST_NETWORK", "") == "1" { "enabled" } else { "none" }))) test-unit
    sh test/containers.sh {{ routine }}

# Uses the network: caches pinned archives, revalidates them in private build
# inputs, and installs the tools plus signed official packs into each image.
#
#   just test-prepare
#   just test-prepare fedora-44 arch
#   just test-prepare all
[arg('distributions', help='Distribution names, for example fedora-44 arch, or all; omit for the routine selection')]
[doc('Build images; network')]
[group('test')]
test-prepare *distributions:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ $# -eq 0 ]]; then set -- {{ routine }}; fi
    "{{ just_executable() }}" --justfile "{{ justfile() }}" _job "test-prepare" "engine=Podman; target=Linux/x86_64; distributions=$*; outputs=.cache/tools + test images; network=allowed"
    sh test/prepare.sh "$@"

# Offline, in rootless Podman. A failing distribution does not stop the
# others. Linux x86_64 only.
#
#   just test-containers
#   just test-containers fedora-44 arch
#   just test-containers all
[arg('distributions', help='Prepared distribution names, for example fedora-44 arch, or all; omit for the routine selection')]
[doc('Run container tests')]
[group('test')]
test-containers *distributions=routine:
    @"{{ just_executable() }}" --justfile "{{ justfile() }}" _job "test-containers" "suite=agent/profile checks; execution=rootless Podman; target=Linux/x86_64; distributions=$*"
    @if [[ "${AGENT_TEST_NETWORK:-}" == 1 ]]; then printf 'Runtime network: enabled\n' >&2; else printf 'Runtime network: none\n' >&2; fi
    sh test/containers.sh "$@"

# The same, with a network in the containers and the suite's checks against
# real websites switched on. Depends on the internet; not for routine use.
[arg('distributions', help='Prepared distribution names, for example fedora-44 arch, or all; enables real website checks')]
[doc('Test containers; network')]
[group('test')]
test-containers-net *distributions=routine:
    @"{{ just_executable() }}" --justfile "{{ justfile() }}" _job "test-containers-net" "suite=agent/profile + website checks; execution=rootless Podman; target=Linux/x86_64; distributions=$*; network=enabled"
    AGENT_TEST_NETWORK=1 sh test/containers.sh "$@"

# Like `just test`, plus the checks against real websites.
[doc('Run tests; network')]
[group('test')]
test-net: (_job "test-net" ("suites=unit + agent/profile/website checks; execution=native then rootless Podman; target=Linux/x86_64; distributions=" + routine + "; network=enabled")) test-unit
    AGENT_TEST_NETWORK=1 sh test/containers.sh {{ routine }}

# Delete known generated files only, anchored to this justfile's checkout.
# Refuse symlinked cache directories; preserve extra files and image parents.
[doc('Remove build/test files')]
[group('test')]
test-clean:
    #!/usr/bin/env bash
    set -euo pipefail
    root={{ quote(justfile_directory()) }}
    cache="$root/.cache"
    if [[ ! -f "$root/test/tools" || ! -f "$root/profiles/agent-policy.jsonc" ]]; then
        printf 'Refusing cleanup outside a nono-agent-profiles checkout.\n' >&2
        exit 1
    fi
    if [[ -L "$cache" || -L "$cache/tools" || -L "$cache/bin" ]]; then
        printf 'Refusing cleanup of symlinked cache directories.\n' >&2
        exit 1
    fi
    for name in nono.tar.gz codex.tar.gz opencode.tar.gz nono codex opencode agent-probe nono-wrapper nono-wrapper.sources nono.sha256 codex.sha256 opencode.sha256; do
        rm -f -- "$cache/tools/$name"
    done
    rm -f -- "$cache/bin/nono-wrapper"
    for directory in "$cache/tools" "$cache/bin" "$cache"; do
        if [[ -d "$directory" ]] && ! rmdir "$directory"; then
            printf 'Kept directory: %s\n' "$directory"
        fi
    done
    if command -v podman >/dev/null 2>&1; then
        podman images --format {{ quote(image_tag_format) }} --filter reference='localhost/nono-agent-profiles:*' |
        while IFS= read -r image; do
            case "$image" in
                localhost/nono-agent-profiles:*) podman rmi --ignore --no-prune "$image" ;;
            esac
        done
    fi
