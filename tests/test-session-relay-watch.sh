#!/bin/bash
# Regression tests for session-relay-watch.sh (Phase 93.2 cross-session relay).
#
# Verifies the high-water-mark poll, bidirectional addressing (to==self only),
# and self-echo suppression of the relay watcher's --once mode.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WATCH="${REPO_ROOT}/scripts/session-relay-watch.sh"

if ! command -v jq >/dev/null 2>&1; then
  echo "SKIP: jq is required for relay-signals.jsonl parsing"
  exit 0
fi

if [ ! -f "$WATCH" ]; then
  echo "FAIL: ${WATCH} not found (expected during TDD red, before implementation)" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}" 2>/dev/null || true' EXIT

repo="${tmp}/repo"
mkdir -p "${repo}/.claude/sessions"
SIG="${repo}/.claude/sessions/relay-signals.jsonl"

SELF="sessionAAAAAA01"        # >=12 chars; prefix = first 12
S12="${SELF:0:12}"            # sessionAAAAA
OTHER="otherBBBBBBBB"         # 12-char peer prefix

fail=0
expect_emit() {   # output, needle, label
  echo "$1" | grep -q "$2" || { echo "FAIL: $3 (expected emit of '$2')" >&2; fail=1; }
}
expect_silent() { # output, needle, label
  if echo "$1" | grep -q "$2"; then echo "FAIL: $3 (must NOT emit '$2')" >&2; fail=1; fi
}

# test 1: a signal addressed to self is emitted as "<ts> | <from> → <to> | <body>"
printf '{"ts":"2026-06-04T10:00:00Z","from":"%s","to":"%s","body":"hello-self"}\n' "$OTHER" "$S12" > "$SIG"
expect_emit "$(bash "$WATCH" "$SELF" "$repo" --once)" "hello-self" "test1: to==self should be emitted"

# test 2: high-water mark — re-poll with no new lines yields nothing
expect_silent "$(bash "$WATCH" "$SELF" "$repo" --once)" "hello-self" "test2: high-water mark, no replay"

# test 3: a signal addressed to a different session is filtered out
printf '{"ts":"2026-06-04T10:01:00Z","from":"%s","to":"%s","body":"for-other"}\n' "$S12" "$OTHER" >> "$SIG"
expect_silent "$(bash "$WATCH" "$SELF" "$repo" --once)" "for-other" "test3: to==other should be filtered"

# test 4: a signal sent BY self is suppressed (self-echo guard)
printf '{"ts":"2026-06-04T10:02:00Z","from":"%s","to":"%s","body":"echo-me"}\n' "$S12" "$S12" >> "$SIG"
expect_silent "$(bash "$WATCH" "$SELF" "$repo" --once)" "echo-me" "test4: from==self should be suppressed"

# test 5: send helper writes a directed signal that watch picks up (round-trip)
SEND="${REPO_ROOT}/scripts/session-relay-send.sh"
if [ -f "$SEND" ]; then
  PEER="peerCCCCCCCC01"
  bash "$SEND" "$PEER" "$SELF" "round-trip-body" "$repo"
  expect_emit "$(bash "$WATCH" "$SELF" "$repo" --once)" "round-trip-body" "test5: send→watch round-trip"
  # body with quotes/newline must not corrupt the jsonl line
  bash "$SEND" "$PEER" "$SELF" 'quote"and
newline' "$repo"
  expect_emit "$(bash "$WATCH" "$SELF" "$repo" --once)" "quote" "test6: send escapes special chars"

  # test 7: cross-worktree storage sharing via git common-dir (codex P2 regression).
  # send from the main repo, watch from a linked worktree → both resolve to the
  # same relay-signals.jsonl under the git common-dir parent.
  if command -v git >/dev/null 2>&1; then
    mainrepo="${tmp}/mainrepo"; mkdir -p "$mainrepo"
    git -C "$mainrepo" init -q 2>/dev/null
    git -C "$mainrepo" -c user.email=t@t -c user.name=t commit --allow-empty -q -m init 2>/dev/null || true
    wt="${tmp}/wt7"
    if git -C "$mainrepo" worktree add -q "$wt" 2>/dev/null; then
      bash "$SEND" "peerDDDDDDDD" "$S12" "cross-wt-body" "$mainrepo"
      out7="$(bash "$WATCH" "$SELF" "$wt" --once)"
      echo "$out7" | grep -q "cross-wt-body" \
        || { echo "FAIL test7: cross-worktree relay not shared via git common-dir" >&2; fail=1; }
    fi
  fi

  # test 8: send refuses a symlinked signal file (codex 4周目 P2a — an untrusted
  # repo must not be able to redirect relay writes to an arbitrary file).
  symrepo="${tmp}/symrepo"; mkdir -p "${symrepo}/.claude/sessions"
  target="${tmp}/evil-target"; : > "$target"
  ln -sf "$target" "${symrepo}/.claude/sessions/relay-signals.jsonl"
  bash "$SEND" "peerEEEEEEEE" "$S12" "evil-body" "$symrepo" 2>/dev/null || true
  if grep -q "evil-body" "$target" 2>/dev/null; then
    echo "FAIL test8: send followed a symlinked signal file" >&2; fail=1
  fi
else
  echo "FAIL: ${SEND} not found (TDD red expected before send impl)" >&2
  fail=1
fi

if [ "$fail" = "0" ]; then
  echo "PASS: session-relay-watch.sh — addressing + high-water mark + self-echo"
else
  exit 1
fi
