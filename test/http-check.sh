#!/bin/sh

# SPDX-FileCopyrightText: The nono-wrapper Authors
#
# SPDX-License-Identifier: CC0-1.0

# Sourced inside nono by the container suite, or with fake curl by local tests.
# Reply files belong to the caller's disposable working directory.
check_request() (
  set -eu
  want=$1 method=$2 url=$3
  case $want in WORKS | FAILS) ;; *) printf 'unknown HTTP expectation\n' >&2; exit 2 ;; esac
  result=0
  curl -sS -m 20 --retry 2 --retry-max-time 30 --max-filesize 1048576 --proto '=https' -X "$method" -o reply \
    -w '%{http_code} %{http_connect}\n' -- "$url" >code || result=$?
  read -r status connect <code || {
    printf 'curl returned no response metadata\n' >&2
    exit 1
  }
  if [ "$result" -ne 0 ] && [ "$connect" = 403 ]; then
    # The proxy's 403 proves refusal; curl versions report it as 7 or 56.
    actual=FAILS
  elif [ "$result" -eq 63 ] && [ "$connect" = 200 ] && { [ "$status" = 200 ] || [ "$status" = 206 ]; }; then
    # A successful response hit our size cap: reachability is proven without
    # downloading an unbounded catalogue. This is not a response-body test.
    actual=WORKS
  elif [ "$result" -ne 0 ]; then
    printf 'HTTP transport failed (curl %s, HTTP %s, CONNECT %s)\n' "$result" "$status" "$connect" >&2
    exit 1
  elif [ "$status" = 403 ] && [ "$(cat reply)" = '{"error":"Forbidden"}' ]; then
    # Intercepted HTTPS reached nono, which refused this method/path.
    actual=FAILS
  else
    case $status in
    2?? | 3?? | 4??) actual=WORKS ;;
    *) printf 'HTTP request failed (HTTP %s)\n' "$status" >&2; exit 1 ;;
    esac
  fi
  if [ "$want" = "$actual" ]; then exit 0; fi
  printf 'expected %s, observed %s (HTTP %s, CONNECT %s) for %s %s\n' "$want" "$actual" "$status" "$connect" "$method" "$url" >&2
  exit 1
)
