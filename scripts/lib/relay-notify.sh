#!/bin/bash
# relay-notify.sh — opt-in cross-agent handoff notification via the relay store.
#
# Sourced by companion scripts (codex/cursor). relay_notify writes a REDACTED
# handoff signal to HARNESS_RELAY_TO when HARNESS_SESSION_RELAY is both|turn.
# This is the cross-agent (CC↔Codex/Cursor) handoff path: companion delegations
# become relay signals a peer CC session can observe via session-relay-watch.sh.
# Default OFF.
#
# Redaction is structural: relay_notify takes only <backend> <subcommand>
# <write> — it has NO prompt parameter, so the companion's prompt body (which
# may contain secrets) can never be forwarded to the relay store.
#
# Usage (sourced): relay_notify <backend> <subcommand> <write>

relay_notify() {
  local backend="${1:-}" subcommand="${2:-}" write="${3:-0}"
  case "${HARNESS_SESSION_RELAY:-off}" in
    monitor|both|turn) ;;   # monitor is an enabling mode too (starts the peer watcher)
    *) return 0 ;;          # opt-in, default OFF
  esac
  local to="${HARNESS_RELAY_TO:-}"
  # CC exposes the Bash subprocess session id as CLAUDE_CODE_SESSION_ID; keep
  # CLAUDE_SESSION_ID as a secondary fallback for older runtimes.
  local from="${HARNESS_RELAY_FROM:-${CLAUDE_CODE_SESSION_ID:-${CLAUDE_SESSION_ID:-}}}"
  [ -n "$to" ] && [ -n "$from" ] || return 0

  local lib_dir scripts_dir send
  lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  scripts_dir="$(cd "${lib_dir}/.." && pwd)"
  send="${scripts_dir}/session-relay-send.sh"
  [ -x "$send" ] || return 0

  # body carries only backend/subcommand/write — never prompt text or secrets.
  bash "$send" "$from" "$to" "handoff: ${backend} ${subcommand} (write=${write})" \
    "${HARNESS_PROJECT_ROOT:-$PWD}" 2>/dev/null || true
}
