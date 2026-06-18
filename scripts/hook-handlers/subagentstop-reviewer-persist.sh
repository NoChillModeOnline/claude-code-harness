#!/usr/bin/env bash
# subagentstop-reviewer-persist.sh — Phase 94.1.2 (#218 part-2, deterministic backstop)
#
# SubagentStop hook handler that extracts a review-result.v1 JSON block from the
# most recent subagent's final assistant turn (= reviewer subagent output) in the
# session transcript, and persists it to .claude/state/review-result.json via
# write-review-result.sh. This is a second-layer defense against the SKILL-step
# write being skipped by the LLM.
#
# Fail-open contract:
#   - any extraction failure (non-reviewer subagent, no JSON, malformed JSON,
#     missing transcript) results in a silent no-op (exit 0).
#   - reviewer subagent is intentionally not detected by name. We detect the
#     review-result.v1 schema_version in the final assistant text instead.
#     This means worker / advisor / scaffolder etc. cannot accidentally trigger
#     a write (their final output doesn't contain that schema_version).

set -euo pipefail

# Read hook input from stdin (Claude Code SubagentStop payload).
INPUT="$(cat 2>/dev/null || true)"
[ -z "$INPUT" ] && exit 0

# Extract transcript_path and cwd.
TRANSCRIPT_PATH="$(printf '%s' "$INPUT" | jq -r '.transcript_path // empty' 2>/dev/null || true)"
[ -z "$TRANSCRIPT_PATH" ] && exit 0
[ ! -f "$TRANSCRIPT_PATH" ] && exit 0

CWD="$(printf '%s' "$INPUT" | jq -r '.cwd // empty' 2>/dev/null || true)"
[ -z "$CWD" ] && CWD="$PWD"

SESSION_ID="$(printf '%s' "$INPUT" | jq -r '.session_id // empty' 2>/dev/null || true)"

# Pull the most recent assistant message's text content.
# Transcript schema: each line is {"type":"assistant"|"user"|...,"message":{"role":"assistant","content":[{"type":"text","text":"..."},{"type":"thinking",...}]}}
LAST_ASSISTANT_TEXT="$(
  awk '
    /"type"[[:space:]]*:[[:space:]]*"assistant"/ { last = $0 }
    END { if (last != "") print last }
  ' "$TRANSCRIPT_PATH" 2>/dev/null \
  | jq -r '[.message.content[]? | select(.type == "text") | .text] | join("\n")' 2>/dev/null || true
)"
[ -z "$LAST_ASSISTANT_TEXT" ] && exit 0

# Extract a review-result.v1 JSON block. Strategy:
#   1) Find a fenced ```json ... ``` block containing the schema_version literal.
#   2) Fallback: scan raw text for balanced { ... } blocks containing review-result.v1.
JSON_BLOCK=""

# Strategy 1: fenced ```json block.
if printf '%s\n' "$LAST_ASSISTANT_TEXT" | grep -q '```json'; then
  CANDIDATE="$(printf '%s\n' "$LAST_ASSISTANT_TEXT" | awk '
    /^```json[[:space:]]*$/ { in_block=1; next }
    /^```[[:space:]]*$/      { if (in_block) { in_block=0; print "---END-BLOCK---" } ; next }
    in_block                  { print }
  ')"
  # Pick first block that contains review-result.v1.
  JSON_BLOCK="$(printf '%s\n' "$CANDIDATE" \
    | awk 'BEGIN { RS="---END-BLOCK---\n"; ORS="\n---END-BLOCK---\n" } /review-result\.v1/ { print; exit }' \
    | sed 's/---END-BLOCK---//g')"
fi

# Strategy 2: balanced JSON block in raw text (python is widely available, jq cannot do this).
if ! printf '%s' "$JSON_BLOCK" | grep -q 'review-result.v1' 2>/dev/null; then
  JSON_BLOCK="$(printf '%s' "$LAST_ASSISTANT_TEXT" | python3 -c '
import json
import re
import sys

text = sys.stdin.read()
# Find candidate JSON object spans by balanced-brace scan.
candidates = []
i = 0
n = len(text)
while i < n:
    if text[i] == "{":
        depth = 0
        in_str = False
        esc = False
        start = i
        while i < n:
            c = text[i]
            if in_str:
                if esc:
                    esc = False
                elif c == "\\":
                    esc = True
                elif c == "\"":
                    in_str = False
            else:
                if c == "\"":
                    in_str = True
                elif c == "{":
                    depth += 1
                elif c == "}":
                    depth -= 1
                    if depth == 0:
                        candidates.append(text[start:i+1])
                        break
            i += 1
    i += 1

for c in candidates:
    if "review-result.v1" not in c:
        continue
    try:
        obj = json.loads(c)
    except json.JSONDecodeError:
        continue
    if obj.get("schema_version") == "review-result.v1":
        sys.stdout.write(c)
        break
' 2>/dev/null || true)"
fi

[ -z "$JSON_BLOCK" ] && exit 0

# Final validation: it must be parseable AND have schema_version == review-result.v1.
if ! printf '%s' "$JSON_BLOCK" | jq -e '.schema_version == "review-result.v1"' >/dev/null 2>&1; then
  exit 0
fi

# Resolve plugin root for write-review-result.sh.
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-}"
if [ -z "$PLUGIN_ROOT" ] || [ ! -f "$PLUGIN_ROOT/scripts/write-review-result.sh" ]; then
  for c in \
    "${CLAUDE_PROJECT_DIR:-}" \
    "$CWD" \
    "$PWD" \
    "$HOME/.claude/plugins/marketplaces/claude-code-harness-marketplace" \
    "$HOME/.claude/plugins/cache/claude-code-harness-marketplace/claude-code-harness/"*; do
    if [ -n "$c" ] && [ -f "$c/scripts/write-review-result.sh" ]; then
      PLUGIN_ROOT="$c"
      break
    fi
  done
fi
[ -z "$PLUGIN_ROOT" ] && exit 0
[ ! -f "$PLUGIN_ROOT/scripts/write-review-result.sh" ] && exit 0

# Stage the JSON block into a temp file for write-review-result.sh.
TMP_JSON="$(mktemp "/tmp/subagentstop-review-XXXXXX.json" 2>/dev/null || mktemp)"
trap 'rm -f "$TMP_JSON"' EXIT
printf '%s' "$JSON_BLOCK" > "$TMP_JSON"

# Operate from CWD so .claude/state/ resolves correctly.
if ! cd "$CWD" 2>/dev/null; then
  cd "$PWD" 2>/dev/null || exit 0
fi

# Capture the current commit hash; non-fatal if missing.
COMMIT_HASH="$(git rev-parse --short HEAD 2>/dev/null || true)"

# Persist via write-review-result.sh. stderr is suppressed; failure is fail-open.
bash "$PLUGIN_ROOT/scripts/write-review-result.sh" "$TMP_JSON" "$COMMIT_HASH" >/dev/null 2>&1 || true

# Append audit entry so operators can see backstop activations.
# Subagent review finding: build the JSONL line via jq -n --arg so that any
# special characters (double quotes, newlines, backslashes) in $VERDICT or
# $SESSION_ID cannot corrupt the audit log. This matches the Go-side
# appendCleanupAuditLog (encoding/json) safety contract.
mkdir -p .claude/state 2>/dev/null || true
AUDIT_LOG=".claude/state/subagentstop-persist-audit.jsonl"
VERDICT="$(jq -r '.verdict // empty' "$TMP_JSON" 2>/dev/null || true)"
TS="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"
jq -cn \
  --arg ts "$TS" \
  --arg session_id "$SESSION_ID" \
  --arg verdict "$VERDICT" \
  --arg commit_hash "$COMMIT_HASH" \
  '{ts:$ts, session_id:$session_id, verdict:$verdict, commit_hash:$commit_hash, source:"subagentstop-reviewer-persist"}' \
  >> "$AUDIT_LOG" 2>/dev/null || true

exit 0
