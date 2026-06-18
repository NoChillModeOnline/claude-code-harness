#!/usr/bin/env bash
# test-reviewer-stop-persist.sh
# Phase 94.1.2 (#218 part-2): subagentstop-reviewer-persist.sh deterministic backstop test
#
# Validates 4 scenarios:
#   (1) reviewer-like assistant turn with APPROVE JSON → review-result.json updated
#   (2) reviewer-like assistant turn with REQUEST_CHANGES JSON → review-result.json updated
#   (3) non-reviewer subagent (no review-result.v1 JSON) → no-op, no file change
#   (4) malformed JSON (almost-valid) → no-op, fail-open

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HANDLER="${ROOT_DIR}/scripts/hook-handlers/subagentstop-reviewer-persist.sh"

[ -x "$HANDLER" ] || { echo "FAIL: handler not executable at $HANDLER"; exit 1; }

pass=0
fail=0

assert() {
  local name="$1"; local cond="$2"
  if eval "$cond"; then
    echo "  PASS  $name"; pass=$((pass + 1))
  else
    echo "  FAIL  $name"; fail=$((fail + 1))
  fi
}

# Helper: build a fake transcript JSONL whose last assistant turn contains the
# given text, then invoke the handler in a sandboxed CWD.
run_handler_with_text() {
  local text="$1"
  local sandbox="$2"

  mkdir -p "$sandbox/.claude/state"
  local transcript="$sandbox/transcript.jsonl"

  # noise user turn
  printf '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"レビューして"}]}}\n' > "$transcript"
  # last assistant turn — escape text for JSON
  local escaped
  escaped="$(printf '%s' "$text" | python3 -c 'import json,sys; sys.stdout.write(json.dumps(sys.stdin.read()))')"
  printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]}}\n' "$escaped" >> "$transcript"

  # build hook input
  local input
  input="$(printf '{"session_id":"test-%s","transcript_path":"%s","cwd":"%s"}\n' "$$" "$transcript" "$sandbox")"

  # invoke handler — must succeed even on no-op.
  # Note: env must reach `bash "$HANDLER"` (right side of the pipe), not just
  # `printf` on the left, so it is set inline on the bash invocation.
  (cd "$sandbox" && printf '%s' "$input" | CLAUDE_PLUGIN_ROOT="${ROOT_DIR}" bash "$HANDLER")
}

APPROVE_BODY='## Review Result

### APPROVE - all gates pass

```json
{
  "schema_version": "review-result.v1",
  "verdict": "APPROVE",
  "decision_needed": {"required": false, "ask_tool": "AskUserQuestion"},
  "accepted_findings": [],
  "rejected_findings": [],
  "acceptance_bar": {"critical_major_zero": true, "spec_alignment": "pass", "plans_alignment": "pass", "regression_safety": "pass", "verification_evidence": "pass"},
  "critical_issues": [],
  "major_issues": [],
  "observations": [],
  "recommendations": []
}
```'

RC_BODY='## Review Result

### REQUEST_CHANGES - failing tests

```json
{
  "schema_version": "review-result.v1",
  "verdict": "REQUEST_CHANGES",
  "decision_needed": {"required": false, "ask_tool": "AskUserQuestion"},
  "accepted_findings": [],
  "rejected_findings": [],
  "acceptance_bar": {"critical_major_zero": false, "spec_alignment": "pass", "plans_alignment": "pass", "regression_safety": "fail", "verification_evidence": "pass"},
  "critical_issues": ["tests fail"],
  "major_issues": [],
  "observations": [],
  "recommendations": []
}
```'

NON_REVIEWER_BODY='## Worker complete

実装したファイル:
- foo.go
- bar.go

self_review 5 件 verified.'

MALFORMED_BODY='## Almost JSON

```json
{
  "schema_version": "review-result.v1",
  "verdict": "APPROVE"
  /* missing comma, malformed */
  "critical_issues": []
}
```'

# (1) APPROVE
echo "[1] APPROVE reviewer turn → review-result.json persisted with verdict APPROVE"
SANDBOX1="$(mktemp -d)"
trap 'rm -rf "$SANDBOX1" "$SANDBOX2" "$SANDBOX3" "$SANDBOX4"' EXIT
run_handler_with_text "$APPROVE_BODY" "$SANDBOX1"
assert "review-result.json exists" "[ -f '$SANDBOX1/.claude/state/review-result.json' ]"
if [ -f "$SANDBOX1/.claude/state/review-result.json" ] && command -v jq >/dev/null 2>&1; then
  V=$(jq -r '.verdict' "$SANDBOX1/.claude/state/review-result.json")
  assert "verdict is APPROVE" "[ '$V' = 'APPROVE' ]"
  AUDIT="$SANDBOX1/.claude/state/subagentstop-persist-audit.jsonl"
  assert "audit log appended" "[ -s '$AUDIT' ]"
fi

# (2) REQUEST_CHANGES
echo "[2] REQUEST_CHANGES reviewer turn → review-result.json persisted with verdict REQUEST_CHANGES"
SANDBOX2="$(mktemp -d)"
run_handler_with_text "$RC_BODY" "$SANDBOX2"
assert "review-result.json exists" "[ -f '$SANDBOX2/.claude/state/review-result.json' ]"
if [ -f "$SANDBOX2/.claude/state/review-result.json" ] && command -v jq >/dev/null 2>&1; then
  V=$(jq -r '.verdict' "$SANDBOX2/.claude/state/review-result.json")
  assert "verdict is REQUEST_CHANGES" "[ '$V' = 'REQUEST_CHANGES' ]"
fi

# (3) Non-reviewer subagent — must no-op
echo "[3] Non-reviewer subagent (no review-result.v1) → no-op (fail-open)"
SANDBOX3="$(mktemp -d)"
run_handler_with_text "$NON_REVIEWER_BODY" "$SANDBOX3"
assert "review-result.json NOT created" "[ ! -f '$SANDBOX3/.claude/state/review-result.json' ]"

# (4) Malformed JSON — must no-op
echo "[4] Malformed JSON → no-op (fail-open)"
SANDBOX4="$(mktemp -d)"
run_handler_with_text "$MALFORMED_BODY" "$SANDBOX4"
assert "review-result.json NOT created" "[ ! -f '$SANDBOX4/.claude/state/review-result.json' ]"

echo
echo "[summary] pass=${pass} fail=${fail}"
[ "${fail}" -eq 0 ]
