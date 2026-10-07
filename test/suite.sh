#!/bin/sh

# SPDX-FileCopyrightText: The nono-agent-profiles Authors
#
# SPDX-License-Identifier: CC0-1.0

# Exercise the official-pack extensions and both launchers in the disposable
# container home. AGENT_TEST_NETWORK=1 also checks ChatGPT allowlists against
# real websites. No login or model is needed.
# Variables in probe strings are expanded by the sandboxed shell.
# shellcheck disable=SC2016
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P)
# shellcheck source=test/require-container.sh
. "$root/test/require-container.sh"
home=$HOME
work=$(mktemp -d "$home/agent-suite.XXXXXX")
work=$(CDPATH='' cd -- "$work" && pwd -P)
readonly work
temp=
trap 'rm -rf -- "${work:?}"; [ -z "$temp" ] || rm -rf -- "${temp:?}"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

temp=$(mktemp -d "${TMPDIR:-/tmp}/agent-suite.XXXXXX")
temp=$(CDPATH='' cd -- "$temp" && pwd -P)
readonly temp
export XDG_CONFIG_HOME="$home/.config" XDG_DATA_HOME="$home/.local/share"
export XDG_STATE_HOME="$home/.local/state" XDG_CACHE_HOME="$home/.cache"
export CODEX_HOME="$home/.codex" TMPDIR="$temp/tmp"
export PATH="$work/bin:$PATH"
unset XDG_RUNTIME_DIR SSH_AUTH_SOCK

for tool in nono opencode codex nono-wrapper curl agent-probe; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "suite: $tool is required" >&2
    exit 1
  }
done
real_codex=$(command -v codex)
real_opencode=$(command -v opencode)

project=$work/project
mkdir -p "$TMPDIR" "$work/bin" "$home/extra" "$home/user-writable" "$home/fs-parent/child" "$project/.git" \
  "$home/.ssh" "$home/.cargo" \
  "$XDG_CONFIG_HOME/nono/profiles" "$XDG_CONFIG_HOME/nono/profile-drafts" "$XDG_CONFIG_HOME/opencode" "$XDG_DATA_HOME/opencode" \
  "$XDG_STATE_HOME/opencode" "$XDG_CACHE_HOME/opencode" "$CODEX_HOME"
echo tool >"$home/.cargo/marker"
echo secret >"$home/.ssh/key"
echo private >"$home/private"
echo extra >"$home/extra/marker"
echo user-file >"$home/user-file"
echo opencode >"$XDG_DATA_HOME/opencode/marker"
echo codex >"$CODEX_HOME/marker"
printf '[user]\n\tname = suite\n' >"$home/.gitconfig"
cp "$root"/profiles/*.jsonc "$XDG_CONFIG_HOME/nono/profiles/"
cat >"$XDG_CONFIG_HOME/nono/profiles/agent-test-env.jsonc" <<'PROFILE'
{
  "meta": { "name": "agent-test-env" },
  "environment": { "allow_vars": ["SUITE_FORWARDED"] }
}
PROFILE
for agent in opencode codex; do
  cat >"$XDG_CONFIG_HOME/nono/profiles/$agent-test.jsonc" <<PROFILE
{
  "extends": ["$agent-offline", "agent-test-env"],
  "meta": { "name": "$agent-test" },
  "filesystem": { "read": ["\$HOME/extra"] }
}
PROFILE
done

failed=0
expect() {
  want=$1 what=$2
  shift 2
  if output=$("$@" 2>&1); then got=WORKS; else got=FAILS; fi
  if [ "$got" = "$want" ]; then
    echo "  ok    $what"
  else
    echo "  FAIL  $what"
    printf '%s\n' "$output"
    failed=1
  fi
}

# shellcheck disable=SC2329
in_nono() {
  nono run --silent --profile "$1" --allow-cwd -- /bin/sh -c "$2"
}
# shellcheck disable=SC2329
said() { printf '%s\n' "$out" | grep -F -q -- "$1"; }
# shellcheck disable=SC2329
twice() { "$@" || "$@"; }

cd "$project"

echo "Profiles: syntax and inheritance"
for file in "$XDG_CONFIG_HOME"/nono/profiles/*.jsonc; do
  expect WORKS "$(basename "$file") validates" nono profile validate "$file"
done
nono-wrapper verify || failed=1

# Exercise the installed agents with their real upstream plugin wiring.
expect WORKS "nono-wrapper starts real OpenCode" nono-wrapper opencode session list
expect WORKS "nono-wrapper starts real Codex" nono-wrapper codex features list
expect WORKS "real Codex commands remain confined by nono with its native sandbox off" agent-probe codex
expect WORKS "OpenCode's default profile starts its ChatGPT browser callback" agent-probe login opencode
expect WORKS "Codex's default profile starts its ChatGPT browser callback" agent-probe login codex
expect WORKS "Codex's fallback browser callback port is permitted" \
  nono run --silent --profile codex-chatgpt --allow-cwd -- agent-probe tcp-bind 1457
expect WORKS "the unlisted listener port is available outside nono" agent-probe tcp-bind 1456
for profile in opencode-chatgpt codex-chatgpt; do
  expect FAILS "$profile: unrelated listener ports are refused" \
    nono run --silent --profile "$profile" --allow-cwd -- agent-probe tcp-bind 1456
done
for profile in opencode-offline codex-offline; do
  expect FAILS "$profile: browser callback ports require a ChatGPT profile" \
    nono run --silent --profile "$profile" --allow-cwd -- agent-probe tcp-bind 1455
done
expect WORKS "the official OpenCode plugin is installed" test -f "$XDG_CONFIG_HOME/opencode/plugins/nono-sandbox.ts"
expect WORKS "the official Codex plugin is installed" test -f "$CODEX_HOME/plugins/cache/nolabs-ai/nono/local/.codex-plugin/plugin.json"

: >"$work/audit-started"

# The stand-ins are deliberately unrestricted shell programs: these checks
# exercise nono's boundary independently of either agent's own safeguards.
cat >"$work/bin/opencode" <<'STANDIN'
#!/bin/sh
echo STANDIN-RAN
echo "secret=${SUITE_SECRET:-absent} forwarded=${SUITE_FORWARDED:-absent}"
echo "tmp=${TMPDIR:-/tmp}"
touch "${TMPDIR:-/tmp}/written"
# Before the refused request below: on Ubuntu, nono 0.79.0 briefly refuses a
# granted socket straight after one.
if [ -n "${SSH_AUTH_SOCK:-}" ]; then
  # 7 is curl's "could not connect"; the listener sends no reply.
  code=0
  curl -s -m 5 -o /dev/null --unix-socket "$SSH_AUTH_SOCK" http://agent/ || code=$?
  [ "$code" -eq 7 ] || echo AGENT-SOCKET-REACHED
fi
if cat "$HOME/private" >/dev/null 2>&1; then echo READ-ESCAPED; fi
if touch "$HOME/escaped" 2>/dev/null; then echo WRITE-ESCAPED; fi
if curl -s -m 5 -o /dev/null https://example.com; then echo NET-ESCAPED; fi
if cat "$HOME/.cargo/marker" >/dev/null 2>&1; then echo TOOLCHAIN-READ; fi
if cat "$HOME/.gitconfig" >/dev/null 2>&1; then echo GIT-READ; fi
if cat "$HOME/extra/marker" >/dev/null 2>&1; then echo EXTRA-READ; fi
if (printf 'updated\n' >"$HOME/extra/written") 2>/dev/null; then echo EXTRA-WRITE; fi
if (printf 'updated\n' >"$HOME/user-writable/written") 2>/dev/null; then echo USER-WRITE; fi
if cat "$HOME/user-file" >/dev/null 2>&1; then echo USER-FILE-READ; fi
if printf 'updated\n' >"$HOME/user-file" 2>/dev/null; then echo USER-FILE-WRITE; fi
# Active rules must stay read-only even if an agent bypasses the config CLI.
for rule in "$XDG_CONFIG_HOME"/nono/profiles/*.jsonc; do
  if (printf '{}\n' >"$rule") 2>/dev/null; then echo ACTIVE-RULE-WRITE; fi
done
if nono-wrapper config opencode add-host agent-self-grant.example >/dev/null 2>&1; then echo AGENT-CONFIG-EDIT; fi
if (unset NONO_SESSION_ID NONO_CAP_FILE; nono-wrapper config opencode add-host agent-self-grant.example) >/dev/null 2>&1; then
  echo AGENT-CONFIG-EDIT-WITHOUT-ENV
fi
if printf '{"meta":{"name":"review-proposal","version":"1.0.0"},"network":{"allow_domain":["suggested.example"]}}\n' >"$XDG_CONFIG_HOME/nono/profile-drafts/review-proposal.json"; then
  echo PROFILE-DRAFT-WRITE
fi
if nono profile promote --yes review-proposal >/dev/null 2>&1; then echo AGENT-PROMOTE; fi
if [ -e inspect-user-policy ]; then
  for policy in "$XDG_CONFIG_HOME"/nono/profiles/.nono-wrapper-policy-*/session.json; do
    [ -f "$policy" ] || continue
    resolved=$(nono profile show --json "$policy") || exit 1
    if printf '%s\n' "$resolved" | grep -F -q '"chatgpt.com"' &&
      ! printf '%s\n' "$resolved" | grep -F -q '"domain": "chatgpt.com"'; then
      echo USER-HOST-OVERRIDE
    fi
    if printf '%s\n' "$resolved" | grep -F -q '"domain": "auth.openai.com"'; then echo AUTH-RULE-KEPT; fi
    if (printf '{}\n' >"$policy") 2>/dev/null; then echo GENERATED-RULE-WRITE; fi
  done
fi
if [ -e ask-allowed-site ]; then
  # Paced and repeated: nono 0.79.0 can refuse back-to-back proxy connections.
  for _ in 1 2 3; do
    sleep 1
    curl -s -m 5 -o /dev/null -X POST https://auth.openai.com/oauth/token || true
  done
fi
mkdir -p .git/hooks web
echo planted >.git/hooks/pre-commit
echo '{}' >web/package.json
echo harmless >notes.txt
exit 0
STANDIN
cp "$work/bin/opencode" "$work/bin/codex"
chmod +x "$work/bin/opencode" "$work/bin/codex"

# Host services the launcher may hand to the sandbox: an SSH agent socket and
# an outbound proxy. Each records that something connected to it.
agent-probe listen unix "$work/agent.sock" "$work/agent-connected" &
listeners=$!
agent-probe listen tcp4 127.0.0.1:3128 "$work/proxy-connected" &
listeners="$listeners $!"
# shellcheck disable=SC2064,SC2086
trap "kill $listeners 2>/dev/null; rm -rf -- \"\${work:?}\"; [ -z \"\$temp\" ] || rm -rf -- \"\${temp:?}\"" EXIT
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ ! -S "$work/agent.sock" ] || break
  sleep 1
done

for agent in opencode codex; do
  echo "$agent: representative upstream grants, local policy and launcher"
  offline=$agent-offline
  expect FAILS "the SSH credential fixture cannot be read" in_nono "$offline" 'cat "$HOME/.ssh/key"'
  expect WORKS "the official runtime grants are inherited" in_nono "$offline" 'cat "$HOME/.cargo/marker"'
  expect FAILS "the inherited Rust runtime grant is read-only" in_nono "$offline" 'touch "$HOME/.cargo/written"'
  expect WORKS "the official Git configuration grant is inherited" in_nono "$offline" 'cat "$HOME/.gitconfig"'
  expect WORKS "nono filters the environment even without the launcher" env SUITE_SECRET=leak \
    nono run --silent --profile "$agent-offline" --allow-cwd -- /bin/sh -c 'test -z "${SUITE_SECRET:-}"'
  if [ "$agent" = opencode ]; then
    own=$XDG_DATA_HOME/opencode
  else
    own=$CODEX_HOME
  fi
  expect WORKS "the agent's state is readable and writable" in_nono "$offline" "cat \"$own/marker\" && touch \"$own/written\""
  if [ "$(uname)" = Linux ]; then
    expect WORKS "pathname sockets need explicit grants even in writable directories" agent-probe socket "$agent-offline"
  fi

  status=0
  out=$(SUITE_SECRET=leak SUITE_FORWARDED=yes \
    nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "the stand-in runs successfully under nono" test "$status" -eq 0
  expect WORKS "a clean exit prints the agent session report" said "$agent session ended (exit 0)"
  expect WORKS "the report resolves this run's recorded audit" said 'Full audit: nono audit show '
  expect FAILS "the recorded report is available" said 'Session report unavailable:'
  expect WORKS "the report includes the blocked website" said '"deny" "example.com:443"'
  expect WORKS "an unlisted variable stays out" said 'secret=absent'
  expect WORKS "an extra variable is absent from the default profile" said 'forwarded=absent'
  expect FAILS "the agent process cannot read outside its grants" said READ-ESCAPED
  expect FAILS "the agent process cannot write outside its grants" said WRITE-ESCAPED
  expect FAILS "an extra directory is not granted by default" said EXTRA-READ
  expect FAILS "a user directory is not writable by default" said USER-WRITE
  expect FAILS "a user file is not readable by default" said USER-FILE-READ
  expect FAILS "the agent cannot directly edit active profiles" said ACTIVE-RULE-WRITE
  expect FAILS "the agent cannot apply config changes" said AGENT-CONFIG-EDIT
  expect WORKS "the agent can write a proposal in profile-drafts" said PROFILE-DRAFT-WRITE
  expect WORKS "the proposed draft is valid" nono profile validate --draft review-proposal
  expect FAILS "the agent cannot promote its own draft" said AGENT-PROMOTE
  expect FAILS "no live profile is created by the agent's promotion attempt" test -e "$XDG_CONFIG_HOME/nono/profiles/review-proposal.json"
  expect FAILS "the default ChatGPT profile refuses unrelated destinations" said NET-ESCAPED
  expect FAILS "the launcher leaves project-file auditing to manual review" said 'Changed files that this computer may run by itself.'
  reported_tmp=$(printf '%s\n' "$out" | sed -n 's/^tmp=//p')
  expect WORKS "the launcher preserves TMPDIR" test "$reported_tmp" = "$TMPDIR"
  expect WORKS "the launcher leaves the caller's temporary directory intact" test -d "$TMPDIR"
  expect WORKS "the launcher retains upstream runtime access" said TOOLCHAIN-READ
  expect WORKS "the launcher retains upstream Git configuration access" said GIT-READ

  out=$(NONO_ALLOW="$home/extra" nono-wrapper "$agent" 2>&1) || true
  expect WORKS "normal NONO_ALLOW configuration is respected" said EXTRA-READ
  out=$(NONO_PROFILE="$agent-test" SUITE_SECRET=leak SUITE_FORWARDED=yes nono-wrapper "$agent" 2>&1) || true
  expect WORKS "normal NONO_PROFILE configuration is respected" said EXTRA-READ
  expect WORKS "a named profile composes additional environment policy" said 'forwarded=yes'
  expect WORKS "a composed profile still filters unrelated variables" said 'secret=absent'

  expect WORKS "the config CLI adds a user host" nono-wrapper config "$agent" add-host api.github.com
  expect WORKS "the config CLI adds a read-only directory" nono-wrapper config "$agent" add-dir "$home/extra"
  expect WORKS "the config CLI adds a writable directory" nono-wrapper config "$agent" add-dir --read-write "$home/user-writable"
  expect WORKS "the config CLI adds a read-only file" nono-wrapper config "$agent" add-file "$home/user-file"
  out=$(nono why --json --profile "$agent-chatgpt" --extends "$agent-user" --host api.github.com 2>&1) || true
  expect WORKS "user host additions compose with the base policy" said '"status": "allowed"'
  out=$(nono why --json --profile "$agent-chatgpt" --extends "$agent-user" --host chatgpt.com 2>&1) || true
  expect WORKS "user host additions retain the base ChatGPT grant" said '"status": "allowed"'
  status=0
  out=$(SUITE_SECRET=leak nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "the launcher automatically loads the user profile" test "$status" -eq 0
  expect WORKS "the user profile adds read-only directories" said EXTRA-READ
  expect FAILS "the read-only user directory remains unwritable" said EXTRA-WRITE
  expect WORKS "the user profile adds writable directories" said USER-WRITE
  expect WORKS "the user profile adds individual files" said USER-FILE-READ
  expect FAILS "the individual file remains read-only" said USER-FILE-WRITE
  expect FAILS "the user profile retains unrelated filesystem denials" said READ-ESCAPED
  expect WORKS "the user profile retains the environment filter" said 'secret=absent'
  expect FAILS "user allowances keep active profiles read-only" said ACTIVE-RULE-WRITE
  expect FAILS "clearing session variables cannot bypass rule protection" said AGENT-CONFIG-EDIT-WITHOUT-ENV
  expect WORKS "the config CLI can override a bundled host" nono-wrapper config "$agent" add-host chatgpt.com
  : >inspect-user-policy
  status=0
  out=$(SUITE_SECRET=leak nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "the user host override starts successfully" test "$status" -eq 0
  expect WORKS "the user host rule replaces the bundled endpoint rule" said USER-HOST-OVERRIDE
  expect WORKS "other bundled endpoint rules are preserved" said AUTH-RULE-KEPT
  expect WORKS "host overrides preserve environment filtering" said 'secret=absent'
  expect FAILS "the generated session policy is read-only to the agent" said GENERATED-RULE-WRITE
  expect FAILS "host overrides do not allow edits to original profiles" said ACTIVE-RULE-WRITE
  for policy in "$XDG_CONFIG_HOME"/nono/profiles/.nono-wrapper-policy-*; do
    expect FAILS "generated policy directories are removed after exit" test -e "$policy"
  done
  rm inspect-user-policy
  expect WORKS "removing a host override restores the bundled rule" nono-wrapper config "$agent" remove-host chatgpt.com
  out=$(nono why --json --profile "$agent-offline" --extends "$agent-user" --host api.github.com 2>&1) || true
  expect WORKS "user host additions cannot unblock an offline profile" said '"status": "denied"'
  status=0
  out=$(NONO_PROFILE="$agent-offline" nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "nono rejects an offline profile combined with host additions" test "$status" -ne 0
  expect WORKS "the offline conflict identifies contradictory network settings" said '--block-net and --allow-domain are contradictory'
  expect FAILS "conflicting offline settings never launch the agent" said STANDIN-RAN
  expect WORKS "the config CLI removes a user host" nono-wrapper config "$agent" remove-host api.github.com
  expect WORKS "the config CLI updates a file's access" nono-wrapper config "$agent" add-file --read-write "$home/user-file"
  status=0
  out=$(NONO_PROFILE="$agent-offline" HTTPS_PROXY=https://unsupported.example nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "offline launches with user additions ignore the unused proxy" test "$status" -eq 0
  if [ "$status" -ne 0 ]; then printf '%s\n' "$out"; fi
  expect WORKS "user directory additions also apply to an explicit profile" said EXTRA-READ
  expect WORKS "the user profile adds writable files" said USER-FILE-WRITE
  expect WORKS "the config CLI removes a user directory" nono-wrapper config "$agent" remove-dir "$home/extra"
  expect WORKS "the config CLI removes a user file" nono-wrapper config "$agent" remove-file "$home/user-file"
  out=$(nono why --json --profile "$agent-chatgpt" --extends "$agent-user" --host api.github.com 2>&1) || true
  expect WORKS "the removed user host is denied again" said '"status": "denied"'
  printf '{invalid}\n' >"$XDG_CONFIG_HOME/nono/profiles/$agent-user.jsonc"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "an invalid user profile stops startup" test "$status" -ne 0
  expect FAILS "an invalid user profile never launches the agent" said STANDIN-RAN
  rm "$XDG_CONFIG_HOME/nono/profiles/$agent-user.jsonc"

  echo "$agent: exact-path user precedence"
  # Change only disposable installed copies. The bundled source stays mounted
  # read-only, and the original installed profile is restored below.
  cp "$XDG_CONFIG_HOME/nono/profiles/$agent-chatgpt.jsonc" "$work/$agent-profile-original.jsonc"
  cat >"$XDG_CONFIG_HOME/nono/profiles/$agent-chatgpt.jsonc" <<PROFILE
{
  "extends": "$agent-base",
  "network": { "block": true },
  "filesystem": {
    "allow": ["\$HOME/extra", "\$HOME/fs-parent"],
    "allow_file": ["\$HOME/user-file"]
  }
}
PROFILE
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "the bundled filesystem fixture starts" test "$status" -eq 0
  expect WORKS "the bundled directory is initially writable" said EXTRA-WRITE
  expect WORKS "the bundled file is initially writable" said USER-FILE-WRITE
  expect WORKS "configure a read-only directory override" nono-wrapper config "$agent" add-dir "$home/extra"
  expect WORKS "configure a read-only file override" nono-wrapper config "$agent" add-file "$home/user-file"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "read-only filesystem overrides start" test "$status" -eq 0
  if [ "$status" -ne 0 ]; then printf '%s\n' "$out"; fi
  expect WORKS "the overridden directory remains readable" said EXTRA-READ
  expect FAILS "the user directory mode replaces bundled write access" said EXTRA-WRITE
  expect WORKS "the overridden file remains readable" said USER-FILE-READ
  expect FAILS "the user file mode replaces bundled write access" said USER-FILE-WRITE
  expect FAILS "filesystem overrides keep active rules read-only" said ACTIVE-RULE-WRITE
  status=0
  out=$(NONO_ALLOW="$home/extra" nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "a CLI-environment grant cannot silently widen the user mode" test "$status" -ne 0
  expect WORKS "the environment conflict explains the overlapping grant" said 'overlapping read/write grant'
  expect FAILS "a conflicting runtime grant does not start the agent" said STANDIN-RAN
  expect WORKS "update the directory override to read/write" nono-wrapper config "$agent" add-dir --read-write "$home/extra"
  expect WORKS "update the file override to read/write" nono-wrapper config "$agent" add-file --read-write "$home/user-file"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "read/write filesystem overrides start" test "$status" -eq 0
  expect WORKS "the updated directory is writable" said EXTRA-WRITE
  expect WORKS "the updated file is writable" said USER-FILE-WRITE
  expect WORKS "remove the directory override" nono-wrapper config "$agent" remove-dir "$home/extra"
  expect WORKS "remove the file override" nono-wrapper config "$agent" remove-file "$home/user-file"
  out=$(nono-wrapper "$agent" 2>&1) || true
  expect WORKS "removal restores the bundled directory grant" said EXTRA-WRITE
  expect WORKS "removal restores the bundled file grant" said USER-FILE-WRITE

  expect WORKS "configure a read-only child of a writable parent" nono-wrapper config "$agent" add-dir "$home/fs-parent/child"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "an unenforceable read-only child stops startup" test "$status" -ne 0
  expect WORKS "the remaining parent grant is explained" said 'overlapping read/write grant'
  expect FAILS "the agent never starts with a silently widened child" said STANDIN-RAN
  expect WORKS "remove the conflicting child rule" nono-wrapper config "$agent" remove-dir "$home/fs-parent/child"
  expect WORKS "configure a path also granted by the official pack" nono-wrapper config "$agent" add-dir "$own"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "an immutable pack grant cannot silently widen the user mode" test "$status" -ne 0
  expect WORKS "the pack conflict identifies the remaining access" said 'overlapping read/write grant'
  expect FAILS "the agent never starts after a pack conflict" said STANDIN-RAN
  printf '{"filesystem":{"allow":["$NONO_CONFIG/profiles"]}}\n' >"$XDG_CONFIG_HOME/nono/profiles/$agent-user.jsonc"
  status=0
  out=$(nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "manual user rules cannot grant writes to active policy" test "$status" -ne 0
  expect WORKS "the protected-rule conflict is explained" said 'active rules'
  expect FAILS "a policy-writing grant never starts an agent" said STANDIN-RAN
  rm "$XDG_CONFIG_HOME/nono/profiles/$agent-user.jsonc"
  cp "$work/$agent-profile-original.jsonc" "$XDG_CONFIG_HOME/nono/profiles/$agent-chatgpt.jsonc"
  for policy in "$XDG_CONFIG_HOME"/nono/profiles/.nono-wrapper-policy-*; do
    expect FAILS "filesystem override checks leave no policy snapshots" test -e "$policy"
  done

  out=$(SSH_AUTH_SOCK="$work/agent.sock" nono-wrapper "$agent" 2>&1) || true
  expect WORKS "the caller's SSH agent socket is granted" said AGENT-SOCKET-REACHED
  out=$(SSH_AUTH_SOCK="$work/agent.sock" NONO_WRAPPER_SSH_AGENT=0 nono-wrapper "$agent" 2>&1) || true
  expect WORKS "the agent still starts with the SSH agent grant turned off" said STANDIN-RAN
  expect FAILS "the SSH agent socket is refused with the grant turned off" said AGENT-SOCKET-REACHED

  out=$(HTTPS_PROXY=http://127.0.0.1:3128 nono-wrapper "$agent" 2>&1) || true
  expect WORKS "the agent starts behind the caller's proxy" said STANDIN-RAN
  out=$(HTTPS_PROXY=http://127.0.0.1:3128 NONO_PROFILE="$agent-test" nono-wrapper "$agent" 2>&1) || true
  expect WORKS "an offline profile still starts behind the caller's proxy" said STANDIN-RAN
  # nono resolves the destination itself before using its upstream proxy.
  if [ "${AGENT_TEST_NETWORK:-}" = 1 ]; then
    : >ask-allowed-site
    rm -f "$work/proxy-connected"
    out=$(nono-wrapper "$agent" 2>&1) || true
    expect FAILS "without a proxy setting nono does not use the caller's proxy" test -e "$work/proxy-connected"
    out=$(HTTPS_PROXY=http://127.0.0.1:3128 nono-wrapper "$agent" 2>&1) || true
    expect WORKS "allowed requests leave through the caller's proxy" test -e "$work/proxy-connected"
    rm -f ask-allowed-site
  fi

  mkdir -p "$work/packs-only/nono"
  cp -R "$XDG_CONFIG_HOME/nono/packages" "$work/packs-only/nono/"
  status=0
  out=$(XDG_CONFIG_HOME="$work/packs-only" nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "without our profiles the launcher announces the official profile" said "using nono's official \"$agent\" profile"
  expect WORKS "the official profile starts the agent" said STANDIN-RAN
  expect FAILS "the official profile still confines the agent" said READ-ESCAPED

  mkdir -p "$work/none/.config" "$work/no-packs/nono/profiles"
  expect FAILS "missing profiles make verification fail" env XDG_CONFIG_HOME="$work/none/.config" nono-wrapper verify "$agent"
  status=0
  out=$(XDG_CONFIG_HOME="$work/none/.config" nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "missing profiles make the launcher fail" test "$status" -ne 0
  expect WORKS "missing profiles point to setup instructions" said 'nono-agent-profiles README'
  expect FAILS "a failed sandbox never launches the agent" said STANDIN-RAN
  cp "$root"/profiles/*.jsonc "$work/no-packs/nono/profiles/"
  status=0
  out=$(XDG_CONFIG_HOME="$work/no-packs" nono-wrapper "$agent" 2>&1) || status=$?
  expect WORKS "missing official packs make the launcher fail" test "$status" -ne 0
  expect WORKS "missing official packs identify the required pack" said "nolabs-ai/$agent"
  expect WORKS "missing official packs point to setup instructions" said 'nono-agent-profiles README'
  expect FAILS "missing official packs never launch the agent" said STANDIN-RAN
done

# Restore the real image executables before the remaining profile checks.
rm "$work/bin/opencode" "$work/bin/codex"
expect WORKS "the real OpenCode executable is restored" test "$(command -v opencode)" = "$real_opencode"
expect WORKS "the real Codex executable is restored" test "$(command -v codex)" = "$real_codex"

status=0
out=$(nono-wrapper audit "$work/audit-started" "$project" 2>&1) || status=$?
expect WORKS "manual auditing reports selected changes" test "$status" -eq 1
expect WORKS "manual auditing reports a planted Git hook" said .git/hooks/pre-commit
expect WORKS "manual auditing reports a nested package.json" said web/package.json
expect FAILS "manual auditing leaves ordinary files alone" said notes.txt

for profile in opencode-offline opencode-chatgpt codex-offline codex-chatgpt; do
  expect WORKS "$profile: temporary files can be read back" \
    in_nono "$profile" 'file=$(mktemp) && echo kept >"$file" && grep -q kept "$file"'
  expect WORKS "$profile: unrelated credentials stay out of the environment" \
    env OPENAI_API_KEY=suite-key CODEX_API_KEY=suite-key GITHUB_TOKEN=suite-key \
    AWS_SECRET_ACCESS_KEY=suite-key SUITE_SECRET=suite-key \
    nono run --silent --profile "$profile" --allow-cwd -- /bin/sh -c '
      for key in OPENAI_API_KEY CODEX_API_KEY GITHUB_TOKEN AWS_SECRET_ACCESS_KEY SUITE_SECRET; do
        if printenv "$key" >/dev/null; then exit 1; fi
      done
    '
done

if [ "${AGENT_TEST_NETWORK:-}" = 1 ]; then
  echo "ChatGPT host and endpoint lists, against real websites"
  # The helper requires a response for allowed requests and an explicit nono
  # refusal for denied requests. Transport errors fail either expectation.
  # shellcheck disable=SC2329
  check_site() {
    selected=$1
    shift
    nono run --silent --profile "$selected" --allow-cwd --read-file "$root/test/http-check.sh" -- \
      /bin/sh -c '. "$1"; shift; check_request "$@"' sh "$root/test/http-check.sh" "$@"
  }
  while read -r profile want method url; do
    case $want in
    WORKS) expect WORKS "$profile: $method $url reaches the site" twice check_site "$profile" "$want" "$method" "$url" ;;
    FAILS) expect WORKS "$profile: $method $url is refused" check_site "$profile" "$want" "$method" "$url" ;;
    esac
  done <<'REQUESTS'
opencode-chatgpt WORKS POST https://chatgpt.com/backend-api/codex/responses
opencode-chatgpt WORKS POST https://auth.openai.com/oauth/token
opencode-chatgpt WORKS POST https://auth.openai.com/api/accounts/deviceauth/usercode
opencode-chatgpt WORKS POST https://auth.openai.com/api/accounts/deviceauth/token
opencode-chatgpt FAILS GET https://chatgpt.com/
opencode-chatgpt FAILS GET https://chatgpt.com/backend-api/codex/responses
opencode-chatgpt FAILS POST https://chatgpt.com/backend-api/files
opencode-chatgpt FAILS GET https://auth.openai.com/oauth/authorize
opencode-chatgpt FAILS GET https://models.opencode.ai/api.json
codex-chatgpt WORKS GET https://chatgpt.com/
codex-chatgpt WORKS POST https://chatgpt.com/backend-api/codex/responses
codex-chatgpt WORKS POST https://auth.openai.com/oauth/token
codex-chatgpt WORKS POST https://auth.openai.com/oauth/revoke
codex-chatgpt WORKS POST https://auth.openai.com/api/accounts/deviceauth/usercode
codex-chatgpt WORKS POST https://auth.openai.com/api/accounts/deviceauth/token
codex-chatgpt FAILS GET https://auth.openai.com/oauth/authorize
codex-chatgpt FAILS POST https://auth.openai.com/api/accounts/other
REQUESTS
  for profile in opencode-chatgpt codex-chatgpt; do
    expect WORKS "$profile: an unlisted host is refused" check_site "$profile" FAILS GET https://example.org/
    expect FAILS "$profile: bypassing the proxy is refused" in_nono "$profile" "curl --noproxy '*' -sS -m 10 -o /dev/null https://chatgpt.com/"
    expect WORKS "$profile: a bare IP is refused" check_site "$profile" FAILS GET https://1.1.1.1/
  done
fi

exit "$failed"
