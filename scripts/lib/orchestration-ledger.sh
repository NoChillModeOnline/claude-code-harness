#!/usr/bin/env bash
# orchestration-ledger.sh — shared helper for the orchestration visibility ledger
#
# Companion scripts (codex-companion.sh / cursor-companion.sh) source this file
# and call orch_emit_ledger once per delegation. The ledger answers the question
# "which backend actually did the work this session?" (Phase 90 / spec.md
# "Orchestration Visibility Contract").
#
# Hard contract:
#   - The ledger line carries ONLY fixed scalar fields. It never receives or
#     records prompt text, file contents, or secrets — orch_emit_ledger has no
#     prompt parameter by design.
#   - Fields (8): ts, backend, subcommand, write, exit_code, duration_ms,
#     session_id, counts (matches orchestration-ledger.v1 schema).
#   - exit_code / duration_ms are nullable: codex-companion exec()s into the
#     codex process and cannot post-process, so it records them as null; cursor-
#     companion runs cursor-agent as a child and records real values.
#   - counts is true only for real delegations (task / review / adversarial-review);
#     status / setup / result / cancel are recorded with counts=false so polling
#     does not inflate the score.
#   - Fail-open: callers MUST invoke as `orch_emit_ledger ... || true`. Invoking
#     in a `|| true` context suppresses set -e inside the function body, so a
#     ledger write failure never changes the delegation's own exit code.
#
# This file only defines functions; sourcing it has no side effects.

# Resolve the ledger path. HARNESS_ORCHESTRATION_LEDGER overrides for tests.
__orch_ledger_path() {
  if [ -n "${HARNESS_ORCHESTRATION_LEDGER:-}" ]; then
    printf '%s' "${HARNESS_ORCHESTRATION_LEDGER}"
    return 0
  fi
  local root
  root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
  printf '%s/.claude/state/orchestration-ledger.jsonl' "${root}"
}

# Resolve the lifetime totals path. HARNESS_ORCHESTRATION_TOTALS overrides for tests.
__orch_totals_path() {
  if [ -n "${HARNESS_ORCHESTRATION_TOTALS:-}" ]; then
    printf '%s' "${HARNESS_ORCHESTRATION_TOTALS}"
    return 0
  fi
  local root
  root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
  printf '%s/.claude/state/orchestration-totals.json' "${root}"
}

# Milliseconds since epoch. macOS `date` has no %N, so use second precision * 1000.
__orch_now_ms() {
  local s
  s="$(date +%s 2>/dev/null || echo 0)"
  printf '%s000' "${s}"
}

# Best-effort session id: env first, then session.json, then "unknown".
__orch_session_id() {
  if [ -n "${CLAUDE_SESSION_ID:-}" ]; then
    printf '%s' "${CLAUDE_SESSION_ID}"
    return 0
  fi
  local sj sid
  sj="$(git rev-parse --show-toplevel 2>/dev/null || pwd)/.claude/state/session.json"
  if [ -f "${sj}" ] && command -v jq >/dev/null 2>&1; then
    sid="$(jq -r '.session_id // empty' "${sj}" 2>/dev/null || true)"
    if [ -n "${sid}" ]; then
      printf '%s' "${sid}"
      return 0
    fi
  fi
  printf 'unknown'
}

# orch_counts_for <subcommand> -> "true" for real delegations, else "false".
orch_counts_for() {
  case "${1:-}" in
    task | review | adversarial-review) printf 'true' ;;
    *) printf 'false' ;;
  esac
}

# orch_emit_ledger <backend> <subcommand> <write 0|1> <exit_code|""> <duration_ms|"">
# Appends exactly one JSONL line. Never records prompt text. Always returns 0.
orch_emit_ledger() {
  {
    local backend="${1:-}" subcommand="${2:-}" write_flag="${3:-0}"
    local exit_code="${4:-}" duration_ms="${5:-}"
    [ -n "${backend}" ] || return 0
    [ -n "${subcommand}" ] || return 0

    local ts session counts write_json ec_json dur_json
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo '')"
    session="$(__orch_session_id)"
    counts="$(orch_counts_for "${subcommand}")"
    if [ "${write_flag}" = "1" ]; then write_json="true"; else write_json="false"; fi
    if [ -n "${exit_code}" ]; then ec_json="${exit_code}"; else ec_json="null"; fi
    if [ -n "${duration_ms}" ]; then dur_json="${duration_ms}"; else dur_json="null"; fi

    local path dir line=""
    path="$(__orch_ledger_path)"
    dir="$(dirname "${path}")"
    mkdir -p "${dir}" 2>/dev/null || return 0

    if command -v jq >/dev/null 2>&1; then
      line="$(jq -cn \
        --arg ts "${ts}" \
        --arg backend "${backend}" \
        --arg subcommand "${subcommand}" \
        --argjson write "${write_json}" \
        --argjson exit_code "${ec_json}" \
        --argjson duration_ms "${dur_json}" \
        --arg session_id "${session}" \
        --argjson counts "${counts}" \
        '{ts:$ts,backend:$backend,subcommand:$subcommand,write:$write,exit_code:$exit_code,duration_ms:$duration_ms,session_id:$session_id,counts:$counts}' \
        2>/dev/null)"
    fi
    if [ -z "${line}" ]; then
      # jq unavailable or failed -> manual JSON. Only controlled scalars here.
      line="{\"ts\":\"${ts}\",\"backend\":\"${backend}\",\"subcommand\":\"${subcommand}\",\"write\":${write_json},\"exit_code\":${ec_json},\"duration_ms\":${dur_json},\"session_id\":\"${session}\",\"counts\":${counts}}"
    fi
    printf '%s\n' "${line}" >>"${path}" 2>/dev/null || return 0
  } 2>/dev/null
  return 0
}
