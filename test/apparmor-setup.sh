#!/usr/bin/env bash
# SPDX-FileCopyrightText: The nono-wrapper Authors
# SPDX-License-Identifier: CC0-1.0
# Runs the README's Fish and Bash AppArmor setup snippets against fake mise,
# sudo and doctor steps with the real profile compiler, checking call order and
# the installed profile per scenario. Root inside a disposable container only;
# called by `just test-apparmor-setup`.
set -euo pipefail

# Root only inside a disposable rootless container. Never load a profile into
# the shared host kernel: the sudo fixture compiles the load step with -Q.
if [[ ! -f /run/.containerenv || $(id -u) != 0 ]]; then
  printf 'Use just test-apparmor-setup in a disposable Ubuntu container.\n' >&2
  exit 1
fi
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
sh "$root/test/image-inputs.sh" --check /usr/local/share/nono-wrapper/image.inputs
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir "$work/bin"
export SETUP_LOG="$work/calls" SETUP_PARSER
SETUP_PARSER=$(command -v apparmor_parser)

cat >"$work/bin/mise" <<'SH'
#!/bin/sh
printf 'mise\n' >>"$SETUP_LOG"
[ "$*" = 'which nono' ] || exit 99
[ "$SETUP_SCENARIO" != mise-fails ] || exit 7
printf '%s\n' "$SETUP_NONO"
SH
cat >"$work/bin/apparmor_parser" <<'SH'
#!/bin/sh
printf 'validate\n' >>"$SETUP_LOG"
[ "$SETUP_SCENARIO" != validation-fails ] || exit 65
exec "$SETUP_PARSER" "$@"
SH
cat >"$work/bin/sudo" <<'SH'
#!/bin/sh
case "$1" in
install)
  printf 'install\n' >>"$SETUP_LOG"
  [ "$SETUP_SCENARIO" != install-fails ] || exit 1
  shift
  exec /usr/bin/install "$@" ;;
apparmor_parser)
  printf 'load\n' >>"$SETUP_LOG"
  [ "$#" = 3 ] && [ "$2" = -r ] || exit 99
  [ "$3" = /etc/apparmor.d/nono-local-userns ] || exit 99
  [ "$SETUP_SCENARIO" != load-fails ] || exit 1
  exec "$SETUP_PARSER" -Q -K "$3" ;;
*) exit 99 ;;
esac
SH
cat >"$work/bin/nono-wrapper" <<'SH'
#!/bin/sh
printf 'doctor\n' >>"$SETUP_LOG"
[ "$*" = 'doctor opencode' ] || exit 99
[ "$SETUP_SCENARIO" != doctor-fails ] || exit 9
SH
chmod +x "$work/bin/mise" "$work/bin/apparmor_parser" \
  "$work/bin/sudo" "$work/bin/nono-wrapper"

for shell in fish bash; do
  # Execute the actual README snippets, not maintained copies of the scripts.
  awk -v language="$shell" '
    $0 == "```" language { active=1; next }
    active && $0 == "```" { exit }
    active { print }
  ' "$root/README.md" >"$work/setup.$shell"
  [[ -s "$work/setup.$shell" ]]
  "$shell" -n "$work/setup.$shell"
  for scenario in success spaces mise-fails invalid-path validation-fails \
    install-fails load-fails doctor-fails; do
    export SETUP_SCENARIO="$scenario" SETUP_NONO
    SETUP_NONO=/home/example/.local/share/mise/installs/nono/0.84.0/nono
    if [[ $scenario == spaces ]]; then
      SETUP_NONO='/home/example user/.local/share/mise/installs/nono/0.84.0/nono'
    elif [[ $scenario == invalid-path ]]; then
      SETUP_NONO='/home/example/"policy"/installs/nono/0.84.0/nono'
    fi
    printf 'previous profile\n' >/etc/apparmor.d/nono-local-userns
    : >"$SETUP_LOG"
    mkdir "$work/tmp"
    status=0
    PATH="$work/bin:$PATH" TMPDIR="$work/tmp" \
      "$shell" "$work/setup.$shell" >"$work/output" 2>&1 || status=$?
    calls=$(cat "$SETUP_LOG")
    case $scenario in
    success | spaces | doctor-fails)
      expected=$'mise\nvalidate\ninstall\nload\ndoctor'
      if [[ $scenario == doctor-fails ]]; then [[ $status == 9 ]]; else [[ $status == 0 ]]; fi
      ;;
    mise-fails | invalid-path)
      expected=mise
      [[ $status != 0 ]]
      ;;
    validation-fails)
      expected=$'mise\nvalidate'
      [[ $status != 0 ]]
      ;;
    install-fails)
      expected=$'mise\nvalidate\ninstall'
      [[ $status != 0 ]]
      ;;
    load-fails)
      expected=$'mise\nvalidate\ninstall\nload'
      [[ $status != 0 ]]
      ;;
    esac
    if [[ $calls != "$expected" ]]; then
      printf '%s/%s: unexpected call order: %s\n' "$shell" "$scenario" "$calls" >&2
      cat "$work/output" >&2
      exit 1
    fi
    case $scenario in
    mise-fails | invalid-path | validation-fails | install-fails)
      [[ $(cat /etc/apparmor.d/nono-local-userns) == 'previous profile' ]]
      ;;
    *)
      directory=${SETUP_NONO%/*}
      pattern="${directory%/*}/*/nono"
      printf '%s\n' 'abi <abi/4.0>,' 'include <tunables/global>' \
        "profile nono-local-userns \"$pattern\" flags=(unconfined) {" \
        '    userns,' '}' >"$work/expected"
      cmp "$work/expected" /etc/apparmor.d/nono-local-userns
      [[ $(stat -c %a /etc/apparmor.d/nono-local-userns) == 644 ]]
      ;;
    esac
    # Both success and failure must remove the temporary profile.
    rmdir "$work/tmp"
    printf '  ok    %s setup: %s\n' "$shell" "$scenario"
  done
done
printf 'AppArmor setup scripts passed (real file install/compiler; kernel load and doctor are fixtures).\n'
