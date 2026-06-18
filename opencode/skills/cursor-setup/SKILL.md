---
name: cursor-setup
description: "Configure and verify Cursor backend for CCH. Triggers: cursor:setup, set Cursor as local default, check Cursor readiness. Distribution default stays opt-in; only local env changes on request."
---

# cursor:setup - Cursor Backend Setup

Cursor backend の setup / verification 用 skill。配布 plugin の fallback は `claude` のままにし、現在の repo / user 環境だけ `cursor` default にする時に使う。

## Quick Reference

```bash
cursor:setup --check
cursor:setup --user-default
cursor:setup --project-default
cursor:setup --unset
```

## Rules

- Do not change distribution defaults or plugin manifests to make Cursor the shipped fallback.
- Use `scripts/resolve-impl-backend.sh` / `scripts/set-impl-backend.sh`; do not infer from `HARNESS_IMPL_BACKEND` alone.
- Treat `~/.cursor/permissions.json`, `.cursorignore`, and Claude sandbox allowlists as manual/user-owned setup surfaces unless the user explicitly asks to edit a local file.

## Flow

First resolve the Harness helper root. Keep the current working directory as the target project so project-scoped `env.local` writes still land in the user's repo:

```bash
HARNESS_PLUGIN_ROOT="${HARNESS_PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT:-}}"
if [ -z "$HARNESS_PLUGIN_ROOT" ] && [ -n "${CLAUDE_SKILL_DIR:-}" ]; then
  probe="$(cd "${CLAUDE_SKILL_DIR}" && pwd)"
  while [ "$probe" != "/" ] && [ ! -d "$probe/scripts" ]; do
    probe="$(cd "$probe/.." && pwd)"
  done
  [ -d "$probe/scripts" ] && HARNESS_PLUGIN_ROOT="$probe"
fi
if [ -z "$HARNESS_PLUGIN_ROOT" ]; then
  echo "ERROR: HARNESS_PLUGIN_ROOT is not set and could not be derived from CLAUDE_PLUGIN_ROOT or CLAUDE_SKILL_DIR" >&2
  exit 2
fi
```

1. For `--check`, run:

   ```bash
   bash "${HARNESS_PLUGIN_ROOT}/scripts/setup-cursor.sh" --check
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --show
   bash "${HARNESS_PLUGIN_ROOT}/scripts/resolve-impl-backend.sh" --role worker
   CURSOR_AGENT_BIN="${CURSOR_AGENT_BIN:-}"
   if [ -z "$CURSOR_AGENT_BIN" ]; then
     if command -v cursor-agent >/dev/null 2>&1; then
       CURSOR_AGENT_BIN="$(command -v cursor-agent)"
     elif [ -x "$HOME/.local/bin/cursor-agent" ]; then
       CURSOR_AGENT_BIN="$HOME/.local/bin/cursor-agent"
     fi
   fi
   if [ -z "$CURSOR_AGENT_BIN" ]; then
     echo "ERROR: cursor-agent not found in PATH or $HOME/.local/bin" >&2
     exit 3
   fi
   "$CURSOR_AGENT_BIN" --version
   ```

2. For `--user-default`, run:

   ```bash
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --user cursor
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --show
   ```

3. For `--project-default`, run:

   ```bash
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" cursor
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --show
   ```

4. For `--unset`, ask whether the target is user or project scope if not clear from the request, then run exactly one matching unset command:

   Project scope:

   ```bash
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --unset
   ```

   User scope:

   ```bash
   bash "${HARNESS_PLUGIN_ROOT}/scripts/set-impl-backend.sh" --unset --user
   ```

## Output

Report three facts only:

- resolved backend (`claude` / `codex` / `cursor`)
- Cursor package readiness (`setup-cursor.sh --check`)
- next manual step if Cursor is not ready
