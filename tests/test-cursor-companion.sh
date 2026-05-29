#!/usr/bin/env bash
# test-cursor-companion.sh
# scripts/cursor-companion.sh の挙動を MOCK cursor-agent で検証する。
#
# 隔離: 実 cursor-agent は呼ばない。temp dir に偽の cursor-agent を置き、
#       PATH の先頭に挿すことでラッパーの command -v 解決がモックを拾う。
#       実ネットワーク smoke は HARNESS_CURSOR_AGENT_SMOKE=1 のときだけ走る
#       （default では skip）。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WRAPPER="${PROJECT_ROOT}/scripts/cursor-companion.sh"

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

[ -f "$WRAPPER" ] || fail "missing script: $WRAPPER"

command -v jq >/dev/null 2>&1 || fail "jq is required for these tests"

# 隔離した temp 作業領域（TMPDIR を尊重する）
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/cursor-companion-test.XXXXXX")"
MOCK_BIN_DIR="${TMP_DIR}/bin"
MOCK_AGENT="${MOCK_BIN_DIR}/cursor-agent"
ARGS_FILE="${TMP_DIR}/captured-args.txt"
# write モード用の独立 workspace（repo root でも $HOME でもない実在ディレクトリ）
WORKSPACE_DIR="${TMP_DIR}/ws"
mkdir -p "${MOCK_BIN_DIR}" "${WORKSPACE_DIR}"

cleanup() {
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

# モック cursor-agent を生成するヘルパ。
#   $1 = stdout に出す内容（空なら何も出さない）
#   $2 = exit code
#   $3 = stderr に出す内容（省略可）
# 起動時に自分の引数を ARGS_FILE に記録する（--mode ask / --force の検証用）。
make_mock() {
  local stdout_body="$1"
  local exit_code="$2"
  local stderr_body="${3:-}"
  {
    printf '#!/usr/bin/env bash\n'
    # captured args を 1 行で記録する
    printf 'printf %s "$*" > %s\n' "'%s\\n'" "${ARGS_FILE}"
    if [ -n "${stderr_body}" ]; then
      printf 'echo %s >&2\n' "${stderr_body}"
    fi
    if [ -n "${stdout_body}" ]; then
      printf 'cat <<'\''JSON_EOF'\''\n%s\nJSON_EOF\n' "${stdout_body}"
    fi
    printf 'exit %s\n' "${exit_code}"
  } >"${MOCK_AGENT}"
  chmod +x "${MOCK_AGENT}"
}

# ラッパーをモック PATH 付きで実行するヘルパ。
# command -v が先頭の MOCK_BIN_DIR から cursor-agent を拾うことを保証する。
run_wrapper() {
  PATH="${MOCK_BIN_DIR}:${PATH}" bash "${WRAPPER}" "$@"
}

# ---------------------------------------------------------------------------
# (a) success: is_error=false / result="DONE" / exit 0
#     → ラッパーは DONE を出力し exit 0
# ---------------------------------------------------------------------------
make_mock '{"is_error":false,"result":"DONE"}' 0
set +e
out="$(run_wrapper task "do the thing" 2>/dev/null)"
rc=$?
set -e
[ "$rc" -eq 0 ] || fail "(a) success should exit 0, got $rc"
[ "$out" = "DONE" ] || fail "(a) success should print 'DONE', got '$out'"

# ---------------------------------------------------------------------------
# (b) error-no-json: stdout 空 / stderr=boom / exit 1
#     → ラッパーは非ゼロ終了し、偽の空 success（DONE/空文字）を出力しない
# ---------------------------------------------------------------------------
make_mock '' 1 'boom'
set +e
out="$(run_wrapper task "do the thing" 2>/dev/null)"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "(b) error-no-json should exit non-zero, got 0"
[ "$out" != "DONE" ] || fail "(b) error must not print a bogus 'DONE'"
[ -z "$out" ] && true # stdout は空であるべき（空 success として扱われない）
[ -z "$out" ] || fail "(b) error must not print any stdout result, got '$out'"

# ---------------------------------------------------------------------------
# (c) is_error true: is_error=true / result="nope" / exit 0
#     → ラッパーは failure 扱いで exit 1
# ---------------------------------------------------------------------------
make_mock '{"is_error":true,"result":"nope"}' 0
set +e
out="$(run_wrapper task "do the thing" 2>/dev/null)"
rc=$?
set -e
[ "$rc" -eq 1 ] || fail "(c) is_error=true should exit 1, got $rc"
[ "$out" != "nope" ] || fail "(c) is_error=true must not print result as success"

# ---------------------------------------------------------------------------
# (d) read-only default は --mode ask を構築する / write では付けない
# ---------------------------------------------------------------------------
make_mock '{"is_error":false,"result":"DONE"}' 0
run_wrapper task "do the thing" >/dev/null 2>&1
grep -q -- "--mode ask" "${ARGS_FILE}" \
  || fail "(d) read-only default should build '--mode ask', args were: $(cat "${ARGS_FILE}")"

make_mock '{"is_error":false,"result":"DONE"}' 0
run_wrapper task --write --workspace "${WORKSPACE_DIR}" "do the thing" >/dev/null 2>&1
if grep -q -- "--mode ask" "${ARGS_FILE}"; then
  fail "(d) write mode must NOT build '--mode ask', args were: $(cat "${ARGS_FILE}")"
fi

# ---------------------------------------------------------------------------
# (e) --force は read-only / write のどちらでも構築されない
# ---------------------------------------------------------------------------
make_mock '{"is_error":false,"result":"DONE"}' 0
run_wrapper task "do the thing" >/dev/null 2>&1
if grep -qE -- "--force|--yolo" "${ARGS_FILE}"; then
  fail "(e) read-only must never build --force/--yolo, args were: $(cat "${ARGS_FILE}")"
fi
make_mock '{"is_error":false,"result":"DONE"}' 0
run_wrapper task --write --workspace "${WORKSPACE_DIR}" "do the thing" >/dev/null 2>&1
if grep -qE -- "--force|--yolo" "${ARGS_FILE}"; then
  fail "(e) write mode must never build --force/--yolo, args were: $(cat "${ARGS_FILE}")"
fi

# ---------------------------------------------------------------------------
# (f) --write without --workspace → exit 2 (guard)
# ---------------------------------------------------------------------------
make_mock '{"is_error":false,"result":"DONE"}' 0
set +e
run_wrapper task --write "do the thing" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -eq 2 ] || fail "(f) --write without --workspace should exit 2, got $rc"

# ---------------------------------------------------------------------------
# (g) --write --workspace=<repo root> → exit 2 (guard)
# ---------------------------------------------------------------------------
make_mock '{"is_error":false,"result":"DONE"}' 0
set +e
run_wrapper task --write --workspace "${PROJECT_ROOT}" "do the thing" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -eq 2 ] || fail "(g) --write --workspace=repo-root should exit 2, got $rc"

# ---------------------------------------------------------------------------
# (h) cursor-agent 不在 → exit 3 (not-found), ハングしない
#     PATH からモックを外し、$HOME も temp に差し替えてフォールバックも外す。
# ---------------------------------------------------------------------------
set +e
PATH="/usr/bin:/bin" HOME="${TMP_DIR}/empty-home" \
  bash "${WRAPPER}" task "do the thing" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -eq 3 ] || fail "(h) missing cursor-agent should exit 3, got $rc"

# ---------------------------------------------------------------------------
# 任意: 実ネットワーク smoke（default skip）
# ---------------------------------------------------------------------------
if [ "${HARNESS_CURSOR_AGENT_SMOKE:-0}" = "1" ]; then
  echo "running real cursor-agent smoke..."
  bash "${WRAPPER}" task "Reply with the single word READY" || fail "(smoke) real cursor-agent failed"
fi

echo "ok"
