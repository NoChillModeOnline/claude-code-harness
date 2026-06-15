#!/bin/bash
# Tests for relay-notify.sh (Phase 93.4 cross-agent handoff notification).
# Verifies the opt-in gate, peer addressing, and structural redaction.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LIB="${REPO_ROOT}/scripts/lib/relay-notify.sh"

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq is required (session-relay-send.sh dependency)"
  exit 0
fi
if [ ! -f "$LIB" ]; then
  echo "FAIL: ${LIB} not found (expected during TDD red)" >&2
  exit 1
fi
# shellcheck source=/dev/null
. "$LIB"

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}" 2>/dev/null || true' EXIT
repo="${tmp}/repo"
mkdir -p "${repo}/.claude/sessions"
SIG="${repo}/.claude/sessions/relay-signals.jsonl"
export HARNESS_PROJECT_ROOT="$repo"

fail=0

# test 1: gate off → no signal written
HARNESS_SESSION_RELAY=off HARNESS_RELAY_TO=peerXXXXXXXX HARNESS_RELAY_FROM=selfYYYYYYYY \
  relay_notify codex task 1
if [ -f "$SIG" ]; then echo "FAIL test1: off mode must not write a signal" >&2; fail=1; fi

# test 2: missing HARNESS_RELAY_TO → no signal (addressing required)
HARNESS_SESSION_RELAY=both HARNESS_RELAY_FROM=selfYYYYYYYY relay_notify codex task 1
if [ -f "$SIG" ]; then echo "FAIL test2: missing TO must not write" >&2; fail=1; fi

# test 3: both mode + addressing → redacted handoff signal written
HARNESS_SESSION_RELAY=both HARNESS_RELAY_TO=peerXXXXXXXX HARNESS_RELAY_FROM=selfYYYYYYYY \
  relay_notify codex task 1
if [ -f "$SIG" ]; then
  grep -q "handoff: codex task" "$SIG" || { echo "FAIL test3: handoff body missing" >&2; fail=1; }
  grep -q '"to":"peerXXXXXXXX"' "$SIG" || { echo "FAIL test3: peer addressing missing" >&2; fail=1; }
  grep -q '"from":"selfYYYYYYYY"' "$SIG" || { echo "FAIL test3: from missing" >&2; fail=1; }
else
  echo "FAIL test3: both mode did not write a signal" >&2; fail=1
fi

# test 4: structural redaction — relay_notify has no prompt arg, so no secret
# can reach the store even if the surrounding env holds one.
export SECRET_TOKEN="sk-do-not-leak-123"
HARNESS_SESSION_RELAY=both HARNESS_RELAY_TO=peerXXXXXXXX HARNESS_RELAY_FROM=selfYYYYYYYY \
  relay_notify codex review 0
if grep -qi "sk-do-not-leak\|SECRET_TOKEN" "$SIG" 2>/dev/null; then
  echo "FAIL test4: signal leaked secret-like content" >&2; fail=1
fi

# test 5: CLAUDE_CODE_SESSION_ID is the `from` fallback when HARNESS_RELAY_FROM is
# unset (codex P1a: the Bash subprocess session id env is CLAUDE_CODE_SESSION_ID).
rm -f "$SIG"
HARNESS_SESSION_RELAY=both HARNESS_RELAY_TO=peerXXXXXXXX CLAUDE_CODE_SESSION_ID=ccsessAAAAAA01 \
  relay_notify codex task 1
if [ -f "$SIG" ]; then
  grep -q '"from":"ccsessAAAAAA"' "$SIG" \
    || { echo "FAIL test5: CLAUDE_CODE_SESSION_ID not used as from" >&2; fail=1; }
else
  echo "FAIL test5: CLAUDE_CODE_SESSION_ID fallback did not write a signal" >&2; fail=1
fi

# test 6: monitor mode also enables send (codex 3周目 P2a — monitor is an enabling mode).
rm -f "$SIG"
HARNESS_SESSION_RELAY=monitor HARNESS_RELAY_TO=peerXXXXXXXX HARNESS_RELAY_FROM=selfYYYYYYYY \
  relay_notify codex task 1
[ -f "$SIG" ] || { echo "FAIL test6: monitor mode must enable companion send" >&2; fail=1; }

# test 7: the signal file is owner-only (codex 3周目 P2b — 0600).
if [ -f "$SIG" ]; then
  perm="$(stat -f '%Lp' "$SIG" 2>/dev/null || stat -c '%a' "$SIG" 2>/dev/null || echo '')"
  case "$perm" in
    600|'') ;;  # 600 expected; empty = stat unsupported, skip the assertion
    *) echo "FAIL test7: relay-signals.jsonl must be 0600, got $perm" >&2; fail=1 ;;
  esac
fi

if [ "$fail" = "0" ]; then
  echo "PASS: relay-notify — gate + addressing + structural redaction"
else
  exit 1
fi
