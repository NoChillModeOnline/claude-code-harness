#!/usr/bin/env bash
# orchestration-rollup.sh — roll a session's delegations into the lifetime accumulator
#
# Phase 90.1.2 (spec.md "Orchestration Visibility Contract"): recording is
# cumulative. This script aggregates the counted delegations of ONE session from
# the ledger and merges them into per-backend lifetime totals.
#
# Usage:
#   bash scripts/orchestration-rollup.sh [session_id]
#
# Invoked from the live Go hook handlers at full-session completion
# (task_completed.go) and again at session end (cleanup.go) as a safety net.
# Because it is idempotent per session_id, running it from both points — or more
# than once — never double-counts.
#
# Contract:
#   - record-only: prints nothing to stdout (it is not a display surface).
#   - idempotent: a session_id already in rolled_up_sessions is a no-op.
#   - fail-open: any error exits 0 without touching the caller's flow.
#   - missing/empty ledger: skip (exit 0).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/lib/orchestration-ledger.sh" ]; then
  # shellcheck source=scripts/lib/orchestration-ledger.sh
  . "${SCRIPT_DIR}/lib/orchestration-ledger.sh" 2>/dev/null || true
fi

main() {
  command -v jq >/dev/null 2>&1 || return 0

  local session_id="${1:-}"
  if [ -z "${session_id}" ]; then
    if command -v __orch_session_id >/dev/null 2>&1; then
      session_id="$(__orch_session_id)"
    fi
  fi
  [ -n "${session_id}" ] || return 0

  local ledger totals now
  if command -v __orch_ledger_path >/dev/null 2>&1; then
    ledger="$(__orch_ledger_path)"
  else
    ledger="${HARNESS_ORCHESTRATION_LEDGER:-}"
  fi
  if command -v __orch_totals_path >/dev/null 2>&1; then
    totals="$(__orch_totals_path)"
  else
    totals="${HARNESS_ORCHESTRATION_TOTALS:-}"
  fi
  [ -n "${ledger}" ] || return 0
  [ -n "${totals}" ] || return 0

  # Missing ledger -> nothing to roll up.
  [ -f "${ledger}" ] || return 0

  now="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo '')"

  # Per-backend counts for this session (counted delegations only).
  local session_counts
  session_counts="$(jq -s --arg sid "${session_id}" \
    '[.[] | select(.session_id == $sid and .counts == true)]
     | group_by(.backend)
     | map({key: .[0].backend, value: length})
     | from_entries' \
    "${ledger}" 2>/dev/null || echo '{}')"
  [ -n "${session_counts}" ] || session_counts='{}'

  # Existing totals or default skeleton.
  local existing
  existing="$(cat "${totals}" 2>/dev/null || true)"
  [ -n "${existing}" ] || existing='{"version":1,"totals":{},"rolled_up_sessions":[],"first_seen":null,"last_seen":null}'

  local dir
  dir="$(dirname "${totals}")"
  mkdir -p "${dir}" 2>/dev/null || return 0

  local tmp merged
  tmp="$(mktemp "${dir}/.orch-totals.XXXXXX" 2>/dev/null || true)"
  [ -n "${tmp}" ] || return 0

  merged="$(printf '%s' "${existing}" | jq \
    --arg sid "${session_id}" \
    --arg now "${now}" \
    --argjson sc "${session_counts}" \
    'if (.rolled_up_sessions | index($sid)) then .
     else
       .version = (.version // 1)
       | .totals = (reduce ($sc | to_entries[]) as $e (.totals // {}; .[$e.key] = (((.[$e.key]) // 0) + $e.value)))
       | .rolled_up_sessions = ((.rolled_up_sessions // []) + [$sid])
       | .first_seen = (.first_seen // $now)
       | .last_seen = $now
     end' 2>/dev/null || true)"

  if [ -z "${merged}" ]; then
    rm -f "${tmp}" 2>/dev/null || true
    return 0
  fi

  printf '%s\n' "${merged}" >"${tmp}" 2>/dev/null || { rm -f "${tmp}" 2>/dev/null || true; return 0; }
  mv "${tmp}" "${totals}" 2>/dev/null || { rm -f "${tmp}" 2>/dev/null || true; return 0; }
  return 0
}

main "$@" || true
exit 0
