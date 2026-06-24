#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_HOME="$(mktemp -d)"
TMP_OUT="$(mktemp)"
trap 'rm -rf "$TMP_HOME" "$TMP_OUT"' EXIT

FAKE_COMPANION_DIR="${TMP_HOME}/.codex/plugins/cache/openai-codex/codex/99.0.0/scripts"
mkdir -p "$FAKE_COMPANION_DIR"
cat >"${FAKE_COMPANION_DIR}/codex-companion.mjs" <<'NODE'
process.stdout.write(JSON.stringify(process.argv.slice(2)) + "\n");
NODE

HOME="$TMP_HOME" \
HARNESS_DISABLE_MODEL_ROUTING=1 \
CODEX_EFFORT=medium \
bash "$ROOT_DIR/scripts/codex-companion.sh" task "default effort smoke" >"$TMP_OUT"

node - "$TMP_OUT" <<'NODE'
const fs = require("fs");
const args = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
function fail(message) {
  console.error(message);
  process.exit(1);
}
if (args[0] !== "task") fail("missing task subcommand");
if (!args.includes("--effort")) fail("wrapper did not add --effort");
const effortIndex = args.indexOf("--effort");
if (!["none", "minimal", "low", "medium", "high", "xhigh"].includes(args[effortIndex + 1])) {
  fail("default effort did not resolve to a valid effort value");
}
if (!args.includes("default effort smoke")) fail("task prompt was not preserved");
NODE

echo "test-codex-companion-default-effort: ok"
