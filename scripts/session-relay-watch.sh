#!/bin/bash
# session-relay-watch.sh
# Cross-session relay watcher: poll relay-signals.jsonl and emit signals
# addressed to this session.
#
# Mirrors the agmsg watch.sh design (high-water mark, signal trap, pidfile
# race guard) but reads CCH's own signal store — no external SQLite, no
# settings.local.json hook. The delivery layer is wired through CCH's own
# hooks.json (see session-start directive), so it passes the R01-R13 guardrail.
#
# Usage: session-relay-watch.sh <session_id> <project_path> [--once]
#
# Emits one line per new signal: "<ts> | <from> → <to> | <body>"
# --once polls a single time (used by tests); the default is a persistent loop
# launched by the Monitor tool from the SessionStart hook when
# HARNESS_SESSION_RELAY=monitor|both.

set -euo pipefail

SESSION_ID="${1:?Usage: session-relay-watch.sh <session_id> <project_path> [--once]}"
PROJECT="${2:?Missing project_path}"
ONCE=0
[ "${3:-}" = "--once" ] && ONCE=1

# jq parses relay-signals.jsonl. Relay is opt-in (HARNESS_SESSION_RELAY), so a
# missing jq degrades to a silent no-op rather than an error.
command -v jq >/dev/null 2>&1 || exit 0

INTERVAL="${HARNESS_SESSION_RELAY_INTERVAL:-5}"
case "$INTERVAL" in ''|*[!0-9]*) INTERVAL=5 ;; esac
# Clamp 0 (and anything <1) to 1s — sleep 0 in the persistent loop is a CPU spin.
[ "$INTERVAL" -ge 1 ] 2>/dev/null || INTERVAL=1

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/relay-store.sh
. "${SCRIPT_DIR}/lib/relay-store.sh"
# Shared store (git common-dir parent) so peer worktrees of the same repo read
# and write one relay-signals.jsonl — the only way cross-worktree relay works.
SESSIONS_DIR="$(relay_sessions_dir "$PROJECT")"
SIGNALS="${SESSIONS_DIR}/relay-signals.jsonl"
SELF="${SESSION_ID:0:12}"
MARK="${SESSIONS_DIR}/.relay-watch-mark.${SELF}"
PIDFILE="${SESSIONS_DIR}/.relay-watch.${SELF}.pid"

# Refuse symlinks (same guard as session-relay-send.sh): an untrusted repo could
# symlink the store dir or state files to redirect chmod/writes outside the project.
if [ -L "$SESSIONS_DIR" ]; then echo "Error: relay store dir is a symlink, refusing" >&2; exit 1; fi
mkdir -p "$SESSIONS_DIR" 2>/dev/null || true
# Owner-only store dir (0700), matching the lease store and session-relay-send.sh.
chmod 0700 "$SESSIONS_DIR" 2>/dev/null || true
for __f in "$MARK" "$PIDFILE"; do
  [ -L "$__f" ] && { echo "Error: relay state file is a symlink, refusing" >&2; exit 1; }
done

read_mark() {
  local v
  v="$(cat "$MARK" 2>/dev/null || echo 0)"
  case "$v" in ''|*[!0-9]*) v=0 ;; esac
  printf '%s' "$v"
}

count_signals() {
  local n
  [ -f "$SIGNALS" ] || { printf '0'; return; }
  n="$(wc -l < "$SIGNALS" 2>/dev/null | tr -d ' ')"
  case "$n" in ''|*[!0-9]*) n=0 ;; esac
  printf '%s' "$n"
}

# Emit signals newer than the high-water mark that are addressed to self.
poll_once() {
  [ -f "$SIGNALS" ] || return 0
  local last total
  last="$(read_mark)"
  total="$(count_signals)"
  [ "$total" -le "$last" ] && return 0
  tail -n +"$((last + 1))" "$SIGNALS" 2>/dev/null | while IFS= read -r line; do
    [ -z "$line" ] && continue
    local to from body ts
    to="$(printf '%s' "$line" | jq -r '.to // ""' 2>/dev/null || true)"
    [ "$to" = "$SELF" ] || continue       # bidirectional addressing: to==self only
    from="$(printf '%s' "$line" | jq -r '.from // ""' 2>/dev/null || true)"
    [ "$from" = "$SELF" ] && continue      # self-echo guard
    body="$(printf '%s' "$line" | jq -r '.body // ""' 2>/dev/null || true)"
    # Sanitize/cap the untrusted body before streaming to Monitor: strip control
    # chars, collapse newlines to spaces, cap length (one-line + byte-cap contract).
    body="$(printf '%s' "$body" | tr -d '\000-\010\013\014\016-\037' | tr '\n\r' '  ')"
    body="${body:0:2048}"
    ts="$(printf '%s' "$line" | jq -r '.ts // ""' 2>/dev/null || true)"
    printf '%s | %s → %s | %s\n' "$ts" "$from" "$to" "$body"
  done
  printf '%s\n' "$total" > "$MARK"
}

if [ "$ONCE" = "1" ]; then
  poll_once
  exit 0
fi

# ---- Persistent mode (launched by the Monitor tool) ----

# pidfile race guard (agmsg watch.sh #66): stop a stale predecessor, but only
# when its cmdline still matches us (defends against pid recycling).
if [ -f "$PIDFILE" ]; then
  prev_pid="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "$prev_pid" ] && [ "$prev_pid" != "$$" ] && kill -0 "$prev_pid" 2>/dev/null; then
    prev_cmd="$(ps -o args= -p "$prev_pid" 2>/dev/null || true)"
    case "$prev_cmd" in
      *session-relay-watch.sh*) kill "$prev_pid" 2>/dev/null || true ;;
    esac
  fi
fi
printf '%s\n' "$$" > "$PIDFILE"
# EXIT trap removes the pidfile only if it still records our pid (a successor
# may have overwritten it before killing us).
trap '[ "$(cat "$PIDFILE" 2>/dev/null)" = "$$" ] && rm -f "$PIDFILE"' EXIT
trap 'exit 0' INT TERM HUP

# High-water mark init: start from the current tail so existing history is not
# replayed — only signals arriving after launch are streamed.
printf '%s\n' "$(count_signals)" > "$MARK"

while true; do
  poll_once
  # Background sleep + wait so SIGTERM/SIGINT fire immediately instead of being
  # deferred until the foreground sleep returns.
  sleep "$INTERVAL" &
  wait $! 2>/dev/null
done
