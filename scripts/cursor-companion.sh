#!/usr/bin/env bash
# cursor-companion.sh — Delegate a whole task to cursor-agent (the Cursor execution backend)
#
# Harness のスキル・エージェントが Cursor をバックエンドとして使うときの
# 唯一の入口。scripts/codex-companion.sh の役割を Cursor 側にミラーする。
#
# Usage:
#   bash scripts/cursor-companion.sh task "Explain the failing test"            # read-only (default)
#   bash scripts/cursor-companion.sh task --write --workspace <dir> "Fix bug"   # write mode
#   bash scripts/cursor-companion.sh task --model <m> "..."                     # model override
#
# Subcommands: task
#
# 安全契約（Phase 82/83 spike + Cursor 公式ドキュメントで確認済み）:
#   - `--force` / `--yolo`（Cursor の "Run Everything" = "Never use"）は決して渡さない。
#     auto-run はユーザーの ~/.cursor/permissions.json の allowlist に委ねる。
#   - read-only は `--mode ask`（hard read-only stop）で表現する。
#   - cursor-agent の `--sandbox enabled` は file write を封じ込めない。
#     本当の境界は worktree + Lead レビューであり、このラッパーは書き込みを
#      jail しているフリをしない。代わりに --write 時の workspace ガードで
#     誤って main tree を指すことを防ぐ（runtime escape は防げない）。
#   - エラー時 cursor-agent は stdout に JSON を出さず（exit 非ゼロ + stderr）。
#     ゆえに `jq -r .result` だけに頼らず、必ず exit code を先に確認する。
#   - model は scripts/model-routing.sh --host cursor --role worker --field model
#     （→ composer-2.5-fast）で解決する。
#
# Exit codes:
#   0  ok            — 成功し、.result を stdout に出力した
#   1  result-error  — 実行は exit 0 だが is_error=true / result が null・空
#   2  bad-guard     — --write の workspace ガード違反（未指定 / repo root / $HOME / 非ディレクトリ）
#   3  not-found     — cursor-agent バイナリが見つからない（not-configured）

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODEL_ROUTER="${SCRIPT_DIR}/model-routing.sh"
REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || (cd "${SCRIPT_DIR}/.." && pwd))"

# ---- cursor-agent バイナリ解決 -------------------------------------------
# command -v を優先（テストの PATH モックがここで拾われる）。
# 見つからなければ $HOME/.local/bin/cursor-agent にフォールバックする。
resolve_cursor_agent() {
  local bin
  if bin="$(command -v cursor-agent 2>/dev/null)" && [ -n "${bin}" ]; then
    printf '%s\n' "${bin}"
    return 0
  fi
  local fallback="${HOME}/.local/bin/cursor-agent"
  if [ -x "${fallback}" ]; then
    printf '%s\n' "${fallback}"
    return 0
  fi
  return 1
}

# ---- model 解決 -----------------------------------------------------------
resolve_cursor_model() {
  if [ ! -x "${MODEL_ROUTER}" ]; then
    return 0
  fi
  bash "${MODEL_ROUTER}" --host cursor --role worker --field model 2>/dev/null || true
}

usage() {
  cat <<'EOF'
Usage:
  cursor-companion.sh task [--write] [--workspace <dir>] [--model <m>] "<prompt>"
EOF
}

SUBCOMMAND="${1:-}"
if [ "${SUBCOMMAND}" != "task" ]; then
  echo "ERROR: unsupported subcommand: '${SUBCOMMAND}' (only 'task' is supported)" >&2
  usage >&2
  exit 2
fi
shift || true

# ---- 引数パース -----------------------------------------------------------
WRITE=0
WORKSPACE=""
MODEL=""
PROMPT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --write)
      WRITE=1
      shift
      ;;
    --workspace)
      WORKSPACE="${2:-}"
      shift 2
      ;;
    --workspace=*)
      WORKSPACE="${1#*=}"
      shift
      ;;
    --model)
      MODEL="${2:-}"
      shift 2
      ;;
    --model=*)
      MODEL="${1#*=}"
      shift
      ;;
    --)
      shift
      [ "$#" -gt 0 ] && PROMPT="$1"
      break
      ;;
    -*)
      echo "ERROR: unknown flag: '$1'" >&2
      usage >&2
      exit 2
      ;;
    *)
      # 非フラグ引数 = プロンプト（最後の 1 つを採用）
      PROMPT="$1"
      shift
      ;;
  esac
done

# ---- model 確定 -----------------------------------------------------------
if [ -z "${MODEL}" ]; then
  MODEL="$(resolve_cursor_model)"
fi
if [ -z "${MODEL}" ]; then
  echo "ERROR: could not resolve a Cursor model (model-routing.sh unavailable)" >&2
  exit 2
fi

# ---- WRITE 時の PRE-LAUNCH WORKSPACE GUARD --------------------------------
# codex-primary-environment-guard と同趣旨: --write を誤って main tree や
# $HOME に向けることを防ぐ。runtime escape までは防げない点に注意
# （本当の境界は worktree + Lead レビュー）。
if [ "${WRITE}" -eq 1 ]; then
  if [ -z "${WORKSPACE}" ]; then
    echo "ERROR: --write requires --workspace <dir> (refusing to write without an explicit isolated workspace)" >&2
    exit 2
  fi
  if [ ! -d "${WORKSPACE}" ]; then
    echo "ERROR: --workspace '${WORKSPACE}' is not a directory" >&2
    exit 2
  fi
  # シンボリックリンク等を解決してから比較する
  ws_abs="$(cd "${WORKSPACE}" 2>/dev/null && pwd -P || true)"
  if [ -z "${ws_abs}" ]; then
    echo "ERROR: --workspace '${WORKSPACE}' could not be resolved to an absolute path" >&2
    exit 2
  fi
  repo_abs="$(cd "${REPO_ROOT}" 2>/dev/null && pwd -P || printf '%s' "${REPO_ROOT}")"
  home_abs="$(cd "${HOME}" 2>/dev/null && pwd -P || printf '%s' "${HOME}")"
  if [ "${ws_abs}" = "${repo_abs}" ]; then
    echo "ERROR: --write --workspace must not point at the repo root ('${repo_abs}'); use an isolated worktree" >&2
    exit 2
  fi
  if [ "${ws_abs}" = "${home_abs}" ]; then
    echo "ERROR: --write --workspace must not point at \$HOME ('${home_abs}')" >&2
    exit 2
  fi
fi

if [ -z "${PROMPT}" ]; then
  echo "ERROR: a prompt is required" >&2
  usage >&2
  exit 2
fi

# ---- cursor-agent 解決（ここで初めて行い、ガード違反は早く返す） ----------
CURSOR_AGENT="$(resolve_cursor_agent || true)"
if [ -z "${CURSOR_AGENT}" ]; then
  echo "ERROR: cursor-agent not found (not-configured)" >&2
  echo "       Install Cursor CLI or place the binary at \$HOME/.local/bin/cursor-agent" >&2
  exit 3
fi

# ---- コマンド構築 ---------------------------------------------------------
# 共通: -p（print/headless）+ JSON 出力 + model。
# read-only（default）: --mode ask（hard read-only stop）。
# write: --mode ask を付けない（auto-run は permissions.json に委ねる）。
# どちらの場合も --force / --yolo は決して付けない。
cmd=("${CURSOR_AGENT}" -p --output-format json --model "${MODEL}")
if [ "${WRITE}" -eq 0 ]; then
  cmd+=(--mode ask)
fi
if [ -n "${WORKSPACE}" ]; then
  cmd+=(--workspace "${WORKSPACE}")
fi
cmd+=("${PROMPT}")

# ---- 実行（stdout を temp に捕捉し、exit code を先に確認）-----------------
# stdout と stderr を別ファイルに分けて捕捉する。
# stdout には成功時の JSON、stderr には診断メッセージが流れる。
OUT_FILE="$(mktemp "${TMPDIR:-/tmp}/cursor-companion.XXXXXX")"
ERR_FILE="$(mktemp "${TMPDIR:-/tmp}/cursor-companion-err.XXXXXX")"
cleanup() {
  rm -f "${OUT_FILE}" "${ERR_FILE}"
}
trap cleanup EXIT

set +e
"${cmd[@]}" >"${OUT_FILE}" 2>"${ERR_FILE}"
rc=$?
set -e

# (1) exit code を最優先で確認。cursor-agent はエラー時 stdout に JSON を出さない。
if [ "${rc}" -ne 0 ]; then
  echo "ERROR: cursor-agent failed (exit ${rc})" >&2
  if [ -s "${ERR_FILE}" ]; then
    cat "${ERR_FILE}" >&2
  fi
  exit "${rc}"
fi

# (2) 成功 exit でも結果が不正なら failure 扱い（空 success を出力しない）。
if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq is required to parse cursor-agent output" >&2
  exit 1
fi

is_error="$(jq -r 'if .is_error == true then "true" else "false" end' "${OUT_FILE}" 2>/dev/null || echo "parse-error")"
if [ "${is_error}" = "parse-error" ]; then
  echo "ERROR: cursor-agent produced unparseable output (no valid JSON result)" >&2
  if [ -s "${ERR_FILE}" ]; then
    cat "${ERR_FILE}" >&2
  fi
  exit 1
fi
if [ "${is_error}" = "true" ]; then
  echo "ERROR: cursor-agent reported is_error=true" >&2
  jq -r '.result // empty' "${OUT_FILE}" >&2 2>/dev/null || true
  exit 1
fi

result="$(jq -r '.result // empty' "${OUT_FILE}" 2>/dev/null || true)"
if [ -z "${result}" ]; then
  echo "ERROR: cursor-agent returned a null/empty result" >&2
  exit 1
fi

# (3) 本当の成功: result テキストを stdout に出力する。
printf '%s\n' "${result}"
