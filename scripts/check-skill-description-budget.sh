#!/usr/bin/env bash
# check-skill-description-budget.sh
# Phase 94.2.1 (#200 fix — short-term): skill description budget gate.
#
# 検証する条件:
#   (1) 個別 SKILL.md の `description:` フィールドが ≤150 chars
#   (2) 全 SKILL.md の description char 数の合計が ≤6000 (CC LLM budget)
#
# 対象: skills/*/SKILL.md (mirror は対象外、SSOT のみ)
#
# Usage: ./scripts/check-skill-description-budget.sh [--max-per-skill N] [--max-total N] [--list]
#   --max-per-skill: per-skill upper bound (default: 150)
#   --max-total:     全 SKILL 合計の upper bound (default: 6000)
#   --list:          各 SKILL の文字数を全件出力 (デフォルトは違反のみ)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKILLS_DIR="${ROOT_DIR}/skills"

MAX_PER_SKILL=150
MAX_TOTAL=6000
LIST_MODE="false"

while [ $# -gt 0 ]; do
  case "$1" in
    --max-per-skill) MAX_PER_SKILL="$2"; shift 2 ;;
    --max-total) MAX_TOTAL="$2"; shift 2 ;;
    --list) LIST_MODE="true"; shift ;;
    -h|--help)
      sed -n '2,15p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

[ -d "$SKILLS_DIR" ] || { echo "skills directory not found: $SKILLS_DIR" >&2; exit 1; }

total=0
violations=0
violation_lines=()

# description フィールドの行頭・末尾を抽出して文字数を測る
# - 1 行 description のみ対応 (folded `>-` や複数行は未対応 — Harness 慣行に従う)
extract_description() {
  local skill_md="$1"
  awk '
    BEGIN { in_fm=0 }
    NR==1 && $0=="---" { in_fm=1; next }
    in_fm && $0=="---" { exit }
    in_fm && /^description:[[:space:]]/ {
      sub(/^description:[[:space:]]*/, "")
      # Strip surrounding quotes if present
      sub(/^"/, "")
      sub(/"$/, "")
      print
      exit
    }
  ' "$skill_md"
}

for skill_md in "$SKILLS_DIR"/*/SKILL.md; do
  [ -f "$skill_md" ] || continue
  name="$(basename "$(dirname "$skill_md")")"
  desc="$(extract_description "$skill_md")"
  if [ -z "$desc" ]; then
    # description フィールド不在の skill は対象外 (= 0 chars)
    continue
  fi
  # char count: ASCII byte count is fine for budget purposes; for multibyte
  # consistency we use wc -m
  len=$(printf '%s' "$desc" | wc -m | tr -d ' ')
  total=$((total + len))

  if [ "$len" -gt "$MAX_PER_SKILL" ]; then
    violations=$((violations + 1))
    violation_lines+=("  OVER  ${name}: ${len} chars (limit ${MAX_PER_SKILL})")
  fi

  if [ "$LIST_MODE" = "true" ]; then
    printf '  %-30s %4d chars\n' "$name" "$len"
  fi
done

echo
echo "==========================================="
echo "Skill description budget summary"
echo "==========================================="
printf '  Total: %d chars (limit %d)\n' "$total" "$MAX_TOTAL"
printf '  Per-skill violations: %d\n' "$violations"

if [ "$violations" -gt 0 ]; then
  echo
  echo "Per-skill violations (>${MAX_PER_SKILL} chars):"
  printf '%s\n' "${violation_lines[@]}"
fi

if [ "$total" -gt "$MAX_TOTAL" ] || [ "$violations" -gt 0 ]; then
  echo
  echo "FAIL: budget gate violated"
  exit 1
fi

echo
echo "PASS: budget gate OK"
exit 0
