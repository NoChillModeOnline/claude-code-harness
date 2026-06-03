#!/usr/bin/env bash
# test-orchestration-totals.sh
# Phase 90.1.2: lifetime accumulator + idempotent rollup.
#
# Verifies:
#   - orchestration-totals.v1 schema exists
#   - scripts/orchestration-rollup.sh aggregates a session's counted delegations
#     from the ledger into per-backend cumulative totals
#   - idempotent per session_id: rolling up the same session twice (e.g. completion
#     + session-end, or a re-run) never double-counts
#   - counts=false lines (status/setup polling) are excluded
#   - missing ledger -> skip (no crash); unwritable totals -> fail-open
#   - record-only: rollup prints nothing to stdout
#   - the live Go hook handlers invoke the rollup (task_completed.go + cleanup.go)

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
ROLLUP="${REPO_ROOT}/scripts/orchestration-rollup.sh"
SCHEMA="${REPO_ROOT}/skills/harness-progress/schemas/orchestration-totals.v1.schema.json"
TASK_GO="${REPO_ROOT}/go/internal/hookhandler/task_completed.go"
CLEANUP_GO="${REPO_ROOT}/go/internal/session/cleanup.go"

PASS=0
FAIL=0
ok() { PASS=$((PASS + 1)); printf 'PASS: %s\n' "$1"; }
ng() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1"; }

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq required for totals test"
  exit 0
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/orch-totals-test.XXXXXX")"
cleanup() { rm -rf "${TMP}"; }
trap cleanup EXIT

[ -f "${SCHEMA}" ] && ok "totals schema exists" || ng "totals schema missing"
[ -f "${ROLLUP}" ] && ok "rollup script exists" || ng "rollup script missing"

if [ ! -f "${ROLLUP}" ]; then
  printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
  exit 1
fi

LEDGER="${TMP}/ledger.jsonl"
TOTALS="${TMP}/totals.json"

# Session A: 2 codex (counts=true), 1 cursor (counts=true), 1 codex status (counts=false)
cat >"${LEDGER}" <<'EOF'
{"ts":"2026-06-03T00:00:00Z","backend":"codex","subcommand":"task","write":true,"exit_code":null,"duration_ms":null,"session_id":"sess-A","counts":true}
{"ts":"2026-06-03T00:00:01Z","backend":"codex","subcommand":"review","write":false,"exit_code":null,"duration_ms":null,"session_id":"sess-A","counts":true}
{"ts":"2026-06-03T00:00:02Z","backend":"cursor","subcommand":"task","write":true,"exit_code":0,"duration_ms":120,"session_id":"sess-A","counts":true}
{"ts":"2026-06-03T00:00:03Z","backend":"codex","subcommand":"status","write":false,"exit_code":null,"duration_ms":null,"session_id":"sess-A","counts":false}
EOF

run_rollup() {
  HARNESS_ORCHESTRATION_LEDGER="${LEDGER}" HARNESS_ORCHESTRATION_TOTALS="${TOTALS}" \
    bash "${ROLLUP}" "$1"
}

# 1. first rollup of sess-A
out="$(run_rollup sess-A 2>/dev/null)"
rc=$?
[ "${rc}" -eq 0 ] && ok "rollup sess-A exit 0" || ng "rollup sess-A rc=${rc}"
[ -z "${out}" ] && ok "rollup is record-only (no stdout)" || ng "rollup printed stdout: [${out}]"

if [ -f "${TOTALS}" ]; then
  ok "totals file created"
  [ "$(jq -r '.totals.codex' "${TOTALS}")" = "2" ] && ok "codex total=2 (status excluded)" || ng "codex total ($(jq -r '.totals.codex' "${TOTALS}"))"
  [ "$(jq -r '.totals.cursor' "${TOTALS}")" = "1" ] && ok "cursor total=1" || ng "cursor total ($(jq -r '.totals.cursor' "${TOTALS}"))"
  [ "$(jq -r '.rolled_up_sessions | length' "${TOTALS}")" = "1" ] && ok "1 session rolled up" || ng "rolled_up count"
else
  ng "totals file not created"
fi

# 2. idempotent: roll up sess-A again -> unchanged
run_rollup sess-A >/dev/null 2>&1
[ "$(jq -r '.totals.codex' "${TOTALS}")" = "2" ] && ok "idempotent: codex still 2" || ng "idempotent codex ($(jq -r '.totals.codex' "${TOTALS}"))"
[ "$(jq -r '.totals.cursor' "${TOTALS}")" = "1" ] && ok "idempotent: cursor still 1" || ng "idempotent cursor"
[ "$(jq -r '.rolled_up_sessions | length' "${TOTALS}")" = "1" ] && ok "idempotent: still 1 session" || ng "idempotent session count"

# 3. second session adds on top
cat >>"${LEDGER}" <<'EOF'
{"ts":"2026-06-03T01:00:00Z","backend":"codex","subcommand":"task","write":true,"exit_code":null,"duration_ms":null,"session_id":"sess-B","counts":true}
EOF
run_rollup sess-B >/dev/null 2>&1
[ "$(jq -r '.totals.codex' "${TOTALS}")" = "3" ] && ok "sess-B adds: codex=3" || ng "sess-B codex ($(jq -r '.totals.codex' "${TOTALS}"))"
[ "$(jq -r '.rolled_up_sessions | length' "${TOTALS}")" = "2" ] && ok "2 sessions rolled up" || ng "2 sessions"

# 4. missing ledger -> skip, no crash
NOLEDGER="${TMP}/none.jsonl"
NOTOTALS="${TMP}/none-totals.json"
HARNESS_ORCHESTRATION_LEDGER="${NOLEDGER}" HARNESS_ORCHESTRATION_TOTALS="${NOTOTALS}" \
  bash "${ROLLUP}" sess-X >/dev/null 2>&1
mrc=$?
[ "${mrc}" -eq 0 ] && ok "missing ledger: exit 0 (skip)" || ng "missing ledger rc=${mrc}"

# 5. fail-open: unwritable totals path
BLOCK="${TMP}/blockfile"
: >"${BLOCK}"
HARNESS_ORCHESTRATION_LEDGER="${LEDGER}" HARNESS_ORCHESTRATION_TOTALS="${BLOCK}/sub/totals.json" \
  bash "${ROLLUP}" sess-A >/dev/null 2>&1
frc=$?
[ "${frc}" -eq 0 ] && ok "fail-open: unwritable totals exit 0" || ng "fail-open rc=${frc}"

# 6. live Go handlers invoke the rollup (via the orchestration package)
if [ -f "${TASK_GO}" ] && grep -q 'orchestration\.Run' "${TASK_GO}"; then
  ok "task_completed.go invokes orchestration.Run (all-done)"
else
  ng "task_completed.go does not invoke orchestration.Run"
fi
if [ -f "${CLEANUP_GO}" ] && grep -q 'orchestration\.Run' "${CLEANUP_GO}"; then
  ok "cleanup.go invokes orchestration.Run (session-end safety net)"
else
  ng "cleanup.go does not invoke orchestration.Run"
fi
# and the orchestration package actually drives the rollup script
ROLLUP_GO="${REPO_ROOT}/go/internal/orchestration/rollup.go"
if [ -f "${ROLLUP_GO}" ] && grep -q 'orchestration-rollup.sh' "${ROLLUP_GO}"; then
  ok "orchestration package execs orchestration-rollup.sh"
else
  ng "orchestration package does not reference the rollup script"
fi

printf '\n%d passed, %d failed\n' "${PASS}" "${FAIL}"
[ "${FAIL}" -eq 0 ]
