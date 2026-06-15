#!/bin/bash
# session-relay-send.sh
# Append a directed cross-session relay signal to relay-signals.jsonl.
#
# This is the write side of the CCH internal relay (read side =
# session-relay-watch.sh). It writes to CCH's own signal store, so the content
# is subject to harness-mem redaction — but callers must still avoid putting
# secrets in <body>; companion integrations redact before calling this.
#
# Usage: session-relay-send.sh <from_session> <to_session> <body> [project_path]

set -euo pipefail

FROM="${1:?Usage: session-relay-send.sh <from_session> <to_session> <body> [project_path]}"
TO="${2:?Missing to_session}"
BODY="${3:?Missing body}"
PROJECT="${4:-$(pwd)}"

command -v jq >/dev/null 2>&1 || { echo "Error: jq is required for relay send" >&2; exit 1; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/relay-store.sh
. "${SCRIPT_DIR}/lib/relay-store.sh"
# Shared store (git common-dir parent) so a peer worktree's watcher reads the
# same relay-signals.jsonl this send writes to.
SESSIONS_DIR="$(relay_sessions_dir "$PROJECT")"
SIGNALS="${SESSIONS_DIR}/relay-signals.jsonl"
# Refuse symlinks: the store lives under a checked-out project, so an untrusted
# repo could symlink the dir/file to make opt-in relay sends mutate other files.
if [ -L "$SESSIONS_DIR" ]; then echo "Error: relay store dir is a symlink, refusing" >&2; exit 1; fi
mkdir -p "$SESSIONS_DIR" 2>/dev/null || true
# Owner-only (0700/0600), matching the lease store.
chmod 0700 "$SESSIONS_DIR" 2>/dev/null || true
if [ -L "$SIGNALS" ]; then echo "Error: relay signal file is a symlink, refusing" >&2; exit 1; fi
if [ ! -f "$SIGNALS" ]; then
  ( umask 0177; : >> "$SIGNALS" ) 2>/dev/null || true
fi

TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
FROM12="${FROM:0:12}"
TO12="${TO:0:12}"

# jq -cn builds the JSON object so body's quotes/newlines are escaped safely —
# never hand-format JSON with printf for attacker-or-user-controlled text.
jq -cn --arg ts "$TS" --arg from "$FROM12" --arg to "$TO12" --arg body "$BODY" \
  '{ts:$ts, from:$from, to:$to, body:$body}' >> "$SIGNALS"
