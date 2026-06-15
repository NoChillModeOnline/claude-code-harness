// Package scaffold は harness のプロジェクト初期化で生成するファイルの
// テンプレートを提供する。`harness init` (CLI) と Setup hook (auto-bootstrap)
// の両方から同じテンプレートを参照することで、生成内容のドリフトを防ぐ。
package scaffold

// HarnessTomlTemplate is the default harness.toml content written by
// "harness init" and the Setup hook auto-bootstrap.
// It documents every supported section with commented examples so users know
// what they can configure without consulting external docs.
const HarnessTomlTemplate = `# Harness v4 Configuration
# Edit this file, then run ` + "`harness sync`" + ` to generate CC plugin files.

[project]
name = ""
version = "0.1.0"
description = ""

[agent]
# default = "security-reviewer"

[env]
# CLAUDE_CODE_SUBPROCESS_ENV_SCRUB = "1"

[safety.permissions]
deny = [
  "Bash(sudo:*)",
]
ask = [
  "Bash(rm -r:*)",
  "Bash(git push --force:*)",
]

# Optional R03 break-glass. TOML only; YAML does not support this list.
# [[safety.guardrail.protectedPathAskList]]
# path = ".env"
# reason = "customer deploy env update"

[safety.sandbox]
failIfUnavailable = false

[safety.sandbox.filesystem]
# denyRead = [".env", "secrets/**"]
# allowRead = [".env.example"]

`
