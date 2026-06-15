# Upstream Update Snapshot — 2026-06-04

Claude Code 2.1.153 → 2.1.162（10 版）と Codex CLI 0.135 → 0.137（3 版）の分類。
`.claude/rules/cc-update-policy.md` 準拠: **A**（実装あり）/ **C**（CC 自動継承）/ **P**（Plans 化）/ **Reject**。
**B（書いただけ）= 0 件**。CCH は CC 2.1.152 / Codex 0.134 まで（Phase 80）追従済み。

## Claude Code 2.1.153 → 2.1.162

| version | 変更点 | 分類 | 対応 | 出典 |
|---|---|---|---|---|
| 2.1.162 | `claude agents --json` に `waitingFor`（waiting session の停止要因: permission prompt 等） | **A** | `docs/agent-view-policy.md` に「Lead が session 監視で stuck 検知に使える」を反映（Phase 92.2）。CCH は実コードで `claude agents --json` を poll していない（grep は CHANGELOG の歴史記述のみ）ため doc 追記の軽い A | code.claude.com/docs/en/changelog |
| 2.1.162 | SendMessage が深い `$TMPDIR`/`CLAUDE_CODE_TMPDIR` で silently break する不具合の修正 | **C** | CCH は `CLAUDE_CODE_TMPDIR` を設定しない（grep 0 件）。CC 本体修正で自動継承 | 同上 |
| 2.1.160 | shell startup files（`.zshenv` 等）/ build-tool config（`.npmrc`/`.bazelrc` 等）への書込前 prompt | **C** | 照合結果（Phase 92.2）: CCH guardrail は repo 内 `.claude/hooks` を保護、品質基準（eslint/tsconfig/biome）は settings.json deny。home shell startup / build-tool config は CCH scope 外で CC 本体（2.1.160）が gate。責務分離で差分なし=guardrail コード変更不要 | github releases/tag/v2.1.160 |
| 2.1.157 | `.claude/skills` plugin auto-load / `claude plugin init` scaffold / worktree auto-unlock | **C** | skill mirror（`skills/`→`codex/.codex/skills/` 等）との二重ロード競合を check-consistency で確認。worktree unlock は CCH の worktree 隔離に効くが Harness 側実装は不要 | code.claude.com/docs/en/changelog |
| 2.1.157 | `EnterWorktree` mid-session 切替 / isolation 修正（2.1.154/2.1.161 含む） | **C** | breezing の worktree 隔離前提に効く（CC 自動継承）。doc は long-running-harness で言及済み | 同上 |
| 2.1.154 | Opus 4.8 リリース / lean system prompt 既定 / streaming tool 常時有効 | **C** | model-routing は 4.8 対応済み | 同上 |
| 2.1.156 | Opus 4.8 の thinking block API error hotfix | **C** | CC 本体 hotfix | 同上 |
| 2.1.155 / 2.1.159 | user-facing 変更なし（internal/infra のみ） | **C** | — | changelog（2.1.155 は release tag 404、2.1.159 は "no user-facing changes" 明記） |
| 2.1.154 | dynamic workflows（trigger keyword は 2.1.160 で `ultracode` に rename） | **P** | CC ネイティブ dynamic workflows と CCH breezing/Agent Teams は機能領域が重なる。棲み分け or 統合は単独 Phase 相当。将来起票 | 同上 |
| 2.1.158 | Auto mode が Bedrock/Vertex/Foundry の Opus 4.7/4.8 で利用可 | **P** | `--auto-mode` は opt-in rollout（opus-4-7-prompt-audit.md）。プロバイダ拡大の案内は model-routing 3 層更新を伴う。将来 | 同上 |
| その他 | VSCode/Cursor/Windsurf 統合修正 / voice mode / `/usage-credits` / Windsurf→Devin rename | **Reject** | CCH 非対象 | 同上 |

## Codex CLI 0.135 → 0.137

CCH は `scripts/codex-companion.sh` 経由でのみ Codex を呼び、**app-server プロトコル / MCP server / TUI / Python SDK を直接使わない**（`codex-cli-only.md`）。app-server v2 / multi-agent v2 / TUI 変更は構造的に Reject/C。

| version | 変更点 | 分類 | 対応 | 出典 |
|---|---|---|---|---|
| 0.137 | App Server v2 RPC（remote-control pairing/grant の list/revoke） | **Reject** | companion 経由のみで app-server 非使用。**「全面刷新ではなく remote-control 用 RPC 追加」**と明記して誤解防止 | developers.openai.com/codex/changelog |
| 0.137 | `codex plugin list --json`（machine-readable） | **A（低優先）** | companion `setup` が plugin 一覧を読む場合に JSON 化で堅牢化。現状未使用なら Reject 格下げ | 同上 |
| 0.137 | Multi-agent v2（thread ごと runtime 選択）/ hosted web tool | **C / Reject** | companion 1-shot 委譲なので Codex 側 multi-agent 不使用 | 同上 |
| 0.136 | command-safety hardening（`/diff` の Git helper 不実行 / PowerShell parser 回避 / sandbox deny 維持） | **C** | companion 委譲の安全性が自動向上 | 同上 |
| 0.135 | `codex doctor` 診断拡張 / `CODEX_NON_INTERACTIVE=1` 非対話インストール | **C** | CI 非対話導入で有用（`harness-setup` に任意記載可） | 同上（公式 changelog 日付 2026-05-28） |

## カテゴリ集計

| 分類 | 件数 | 内訳 |
|---|---|---|
| **A（実装あり）** | 2 | `waitingFor`（doc 追記 → agent-view-policy.md）/ `codex plugin list --json`（低優先） |
| **C（自動継承）** | 多数 | Opus 4.8 / TMPDIR 修正 / shell-config gate（照合: CC 本体依存）/ worktree / Codex sandbox 強化 等 |
| **P（Plans 化）** | 2 | dynamic workflows（breezing 棲み分け）/ auto mode プロバイダ拡大 |
| **Reject** | 多数 | app-server v2 / TUI / VSCode 統合 / voice mode |
| **B（書いただけ）** | **0** | cc-update-policy.md 準拠 |

## 要明記（誤解防止）

- Codex 「App Server v2」= remote-control 用 RPC 追加であって app-server プロトコル全面刷新ではない。CCH は app-server 非利用で影響なし
- CC 2.1.155 / 2.1.159 は user-facing 変更なし（infra のみ）
- Opus 4.8 の Co-Authored-By 表記は `Claude Opus 4.8 (1M context)` を維持（変更不要）
