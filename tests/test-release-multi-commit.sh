#!/usr/bin/env bash
# test-release-multi-commit.sh
# Phase 94.1.3 (#219 fix): harness-release multi-commit (work + bump) safety test
#
# 検証内容:
#   (1) pretooluse-guard.sh の commit guard が bookkeeping-only (VERSION /
#       .claude-plugin/plugin.json / harness.toml / CHANGELOG.md) staged commit を
#       APPROVE 無しで通過させる
#   (2) コード変更 staged commit は従来通り APPROVE が必要
#   (3) bookkeeping commit 通過時に commit-cleanup-audit.jsonl に記録される
#   (4) harness-release SKILL に Phase 94.1.3 multi-commit セーフティの明示記載がある
#   (5) Go cleanup handler の bookkeeping classifier が 5 ケースで正しく動く
#       (TestCommitCleanup_* unit tests と整合)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GUARD_SCRIPT="${ROOT_DIR}/scripts/pretooluse-guard.sh"
RELEASE_SKILL="${ROOT_DIR}/skills/harness-release/SKILL.md"

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

# Sandbox git repo を作る helper
make_sandbox() {
  local sandbox="$1"
  (
    cd "$sandbox"
    git init --quiet --initial-branch=main >/dev/null 2>&1
    git config user.email "test@example.com"
    git config user.name "Test"
    # baseline commit
    echo "1.0.0" > VERSION
    mkdir -p .claude-plugin
    echo '{"name":"x","version":"1.0.0"}' > .claude-plugin/plugin.json
    cat > harness.toml <<'TOML'
[plugin]
version = "1.0.0"
TOML
    cat > CHANGELOG.md <<'MD'
# Changelog

## [Unreleased]

- Initial
MD
    echo "package main" > main.go
    git add -A >/dev/null 2>&1
    git commit --quiet -m "baseline" >/dev/null 2>&1
    # ガード script は ROOT_DIR の locale を見るので CLAUDE_PLUGIN_ROOT 設定
  )
}

# guard を呼ぶ helper (pretooluse-guard.sh は Bash + matcher="Bash" の入力 JSON を受ける)
invoke_guard() {
  local sandbox="$1"
  local command_str="$2"
  local input
  input=$(printf '{"tool_name":"Bash","tool_input":{"command":%s},"cwd":"%s"}' \
    "$(printf '%s' "$command_str" | python3 -c 'import json,sys; sys.stdout.write(json.dumps(sys.stdin.read()))')" \
    "$sandbox")
  (
    cd "$sandbox"
    CLAUDE_PLUGIN_ROOT="$ROOT_DIR" printf '%s' "$input" | bash "$GUARD_SCRIPT" 2>/dev/null
  )
}

# (4) SKILL に Phase 94.1.3 multi-commit セーフティ記載
echo "[1] harness-release SKILL.md には multi-commit セーフティ記載"
assert "release skill mentions multi-commit セーフティ" \
  "grep -q 'multi-commit セーフティ' '${RELEASE_SKILL}'"
assert "release skill mentions bookkeeping" \
  "grep -q 'bookkeeping' '${RELEASE_SKILL}'"

# (1) bookkeeping commit が approve 無しで通過
echo "[2] bookkeeping-only staged commit → 承認不要で通過"
SANDBOX_A="$(mktemp -d)"
trap 'rm -rf "$SANDBOX_A" "$SANDBOX_B" "$SANDBOX_C"' EXIT
make_sandbox "$SANDBOX_A"
(
  cd "$SANDBOX_A"
  # bump 相当: VERSION + CHANGELOG.md のみ変更
  echo "1.0.1" > VERSION
  printf '\n## [1.0.1]\n- bump\n' >> CHANGELOG.md
  git add VERSION CHANGELOG.md
)
GUARD_OUT_A=$(invoke_guard "$SANDBOX_A" "git commit -m 'release: v1.0.1'")
# decision: "approve" であること (review-result.json は無いが bookkeeping 例外で通る)
if printf '%s' "$GUARD_OUT_A" | grep -qE '"(permissionDecision|decision)"[[:space:]]*:[[:space:]]*"deny"'; then
  echo "  FAIL  bookkeeping-only commit was denied (expected approve). out=$GUARD_OUT_A"
  fail=$((fail + 1))
else
  echo "  PASS  bookkeeping-only commit not denied"
  pass=$((pass + 1))
fi

# audit log
AUDIT_A="$SANDBOX_A/.claude/state/commit-cleanup-audit.jsonl"
assert "audit log contains bookkeeping-only reason" \
  "[ -f '$AUDIT_A' ] && grep -q '\"reason\":\"bookkeeping-only\"' '$AUDIT_A'"

# (2) code 変更 commit は承認不要では弾かれる
echo "[3] code-bearing staged commit (no APPROVE) → 通常通り deny"
SANDBOX_B="$(mktemp -d)"
make_sandbox "$SANDBOX_B"
(
  cd "$SANDBOX_B"
  echo "package main // change" > main.go
  git add main.go
)
GUARD_OUT_B=$(invoke_guard "$SANDBOX_B" "git commit -m 'feat: code'")
if printf '%s' "$GUARD_OUT_B" | grep -qE '"(permissionDecision|decision)"[[:space:]]*:[[:space:]]*"deny"'; then
  echo "  PASS  code commit denied (no APPROVE → guard fires)"
  pass=$((pass + 1))
else
  echo "  FAIL  code commit not denied (expected deny). out=$GUARD_OUT_B"
  fail=$((fail + 1))
fi

# (3) code commit に APPROVE があれば通る (既存挙動 regression check)
echo "[4] code-bearing staged commit with APPROVE → 通過"
SANDBOX_C="$(mktemp -d)"
make_sandbox "$SANDBOX_C"
(
  cd "$SANDBOX_C"
  echo "package main // change" > main.go
  git add main.go
  mkdir -p .claude/state
  cat > .claude/state/review-result.json <<'JSON'
{"schema_version":"review-result.v1","verdict":"APPROVE"}
JSON
)
GUARD_OUT_C=$(invoke_guard "$SANDBOX_C" "git commit -m 'feat: code'")
if printf '%s' "$GUARD_OUT_C" | grep -qE '"(permissionDecision|decision)"[[:space:]]*:[[:space:]]*"deny"'; then
  echo "  FAIL  approved code commit was denied. out=$GUARD_OUT_C"
  fail=$((fail + 1))
else
  echo "  PASS  approved code commit passes"
  pass=$((pass + 1))
fi

## (5) chained `git add ... && git commit` is NOT bookkeeping-exempt
## (codex review P2 regression: index-mutating commands must not bypass review)
echo "[5] 'git add VERSION && git commit' chained → bypass を許さず deny"
SANDBOX_D="$(mktemp -d)"
# Subagent review finding: update trap immediately after each mktemp -d so a
# failure between creation and the next trap update does not leak the temp dir
# under `set -euo pipefail`.
trap 'rm -rf "$SANDBOX_A" "$SANDBOX_B" "$SANDBOX_C" "$SANDBOX_D"' EXIT
make_sandbox "$SANDBOX_D"
(
  cd "$SANDBOX_D"
  echo "1.0.1" > VERSION
  # index にはまだ何も staged しない (chained コマンドが直前に staging するシナリオ)
)
GUARD_OUT_D=$(invoke_guard "$SANDBOX_D" "git add VERSION && git commit -m 'bump'")
if printf '%s' "$GUARD_OUT_D" | grep -qE '"(permissionDecision|decision)"[[:space:]]*:[[:space:]]*"deny"'; then
  echo "  PASS  chained git add && commit is denied (no bypass)"
  pass=$((pass + 1))
else
  echo "  FAIL  chained git add+commit incorrectly exempted. out=$GUARD_OUT_D"
  fail=$((fail + 1))
fi

## (6) chained command that swaps bookkeeping index for code is NOT exempt
echo "[6] 'git add src/main.go && git commit' chained → deny (code-bearing)"
SANDBOX_E="$(mktemp -d)"
trap 'rm -rf "$SANDBOX_A" "$SANDBOX_B" "$SANDBOX_C" "$SANDBOX_D" "$SANDBOX_E"' EXIT
make_sandbox "$SANDBOX_E"
(
  cd "$SANDBOX_E"
  echo "1.0.1" > VERSION
  git add VERSION   # 古い bookkeeping staging を残しておく (bypass シナリオ)
  echo "package main // change" > main.go
  # bookkeeping は staged だが、command でさらに code を add してから commit
)
GUARD_OUT_E=$(invoke_guard "$SANDBOX_E" "git add main.go && git commit -m 'mix'")
if printf '%s' "$GUARD_OUT_E" | grep -qE '"(permissionDecision|decision)"[[:space:]]*:[[:space:]]*"deny"'; then
  echo "  PASS  index-mutating command is denied (no bypass)"
  pass=$((pass + 1))
else
  echo "  FAIL  index-mutating command incorrectly exempted. out=$GUARD_OUT_E"
  fail=$((fail + 1))
fi

echo
echo "[summary] pass=${pass} fail=${fail}"
[ "${fail}" -eq 0 ]
