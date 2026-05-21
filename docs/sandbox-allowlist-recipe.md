# Sandbox Allowlist Recipe (Firecrawl / Web Scraping 用)

claude-code-harness を install した他プロジェクトで Firecrawl・テックブログ取得・外部 API 呼び出しが `HTTP/2 403 / x-deny-reason: host_not_allowed` で塞がれる時の解決レシピ。

> **TL;DR**: CC sandbox は default で **allowlist 空 = 全 deny**。ユーザー global の `~/.claude/settings.json` に `sandbox.network.allowedDomains` を追加するのが正規ルート。AI 経由で書き換えは self-audit guard で deny されるため、**ユーザー手動編集**。

## 症状

外部プロジェクトで Firecrawl CLI / WebFetch / curl が 403 / connection refused になる。Bash subprocess のログに以下が出る:

```
HTTP/2 403
x-deny-reason: host_not_allowed
```

または

```
curl: (6) Could not resolve host: api.firecrawl.dev
```

## 原因

Claude Code sandbox（macOS Seatbelt / Linux bubblewrap）は **allowlist default**。`~/.claude/settings.json` に `sandbox.network.allowedDomains` が無い = どのホストへも外向き通信できない。

Firecrawl plugin の `SKILL.md` を確認すると `allowed-tools: Bash(firecrawl *)`。つまり Firecrawl CLI は Bash subprocess として走り、sandbox の影響を直接受ける（MCP server ではない）。

## 解決: `~/.claude/settings.json` に sandbox キーを追加

既存の `permissions` / `hooks` / `enabledPlugins` / `mcpServers` 等と **同じ階層** (top-level) に `sandbox` キーを 1 つ追加する。既存キーは touch しない。

追加する完成形 JSON 構造の全体像 (既存キーは省略表記):

```json
{
  "permissions": { /* 既存維持 */ },
  "hooks": { /* 既存維持 */ },
  "enabledPlugins": { /* 既存維持 */ },
  "mcpServers": { /* 既存維持 */ },
  /* ... 他の既存キーも全て維持 ... */

  "sandbox": {
    "enabled": true,
    "autoAllowBashIfSandboxed": true,
    "excludedCommands": [
      "docker",
      "docker-compose",
      "watchman",
      "systemctl",
      "launchctl",
      "brew services"
    ],
    "network": {
      "allowedDomains": [
        "github.com",
        "api.github.com",
        "raw.githubusercontent.com",
        "codeload.github.com",
        "objects.githubusercontent.com",
        "registry.npmjs.org",
        "api.anthropic.com",
        "pypi.org",
        "files.pythonhosted.org",
        "proxy.golang.org",
        "sum.golang.org",
        "crates.io",
        "static.crates.io",
        "rubygems.org",
        "api.firecrawl.dev",
        "firecrawl.dev",
        "techblog.zozo.com",
        "note.com",
        "assets.st-note.com",
        "zenn.dev",
        "qiita.com",
        "dev.to",
        "medium.com",
        "cdn-ak.f.st-hatena.com",
        "engineering.dena.com",
        "developers.cyberagent.co.jp",
        "tech.uzabase.com",
        "engineer.crowdworks.jp",
        "tech.smarthr.jp"
      ],
      "deniedDomains": [
        "169.254.169.254",
        "metadata.google.internal",
        "metadata.azure.com",
        "pastebin.com",
        "transfer.sh",
        "0x0.st",
        "paste.ee",
        "termbin.com",
        "ix.io"
      ]
    }
  }
}
```

**コピペ手順** (推奨: コマンドラインでの安全な merge):

```bash
# 1. backup を取る (rollback 用)
cp ~/.claude/settings.json ~/.claude/settings.json.bak.$(date +%Y%m%d-%H%M%S)

# 2. エディタで ~/.claude/settings.json を開き、既存の top-level keys
#    (permissions, hooks, enabledPlugins, mcpServers 等) と **同じ階層** に
#    上記 "sandbox": {...} ブロックを 1 つ追加する。
#    既存キーとの区切りに , (カンマ) を忘れない。

# 3. JSON 構文と件数を検証
jq -e '.' ~/.claude/settings.json > /dev/null && echo "VALID JSON"
jq '.sandbox.network.allowedDomains | length' ~/.claude/settings.json
# → 29

# 4. CC を完全再起動 (cmd+Q → 再起動) で sandbox 設定が effective に
```

> **template から流用する場合**: `templates/sandbox-settings.json.template` の `sandbox` セクション (4 行目-65 行目) をそのままコピーすると確実 (recipe と数値完全同期)。

## 構成の意図

3 階層で先回り許可する設計:

| 階層 | ドメイン | 用途 |
|------|---------|------|
| **開発コア** (14) | `github.com` / `api.github.com` / `raw.githubusercontent.com` / `codeload.github.com` / `objects.githubusercontent.com` / `registry.npmjs.org` / `api.anthropic.com` / `pypi.org` / `files.pythonhosted.org` / `proxy.golang.org` / `sum.golang.org` / `crates.io` / `static.crates.io` / `rubygems.org` | npm install / pip install / go mod / cargo / git clone |
| **Firecrawl** (2) | `api.firecrawl.dev` / `firecrawl.dev` | Firecrawl API endpoint |
| **スクレイプ対象** (13) | `techblog.zozo.com` / `note.com` / `assets.st-note.com` / `zenn.dev` / `qiita.com` / `dev.to` / `medium.com` / `cdn-ak.f.st-hatena.com` / `engineering.dena.com` / `developers.cyberagent.co.jp` / `tech.uzabase.com` / `engineer.crowdworks.jp` / `tech.smarthr.jp` | 日本/英語のテックブログ・記事スクレイプ |

`deniedDomains` 9 個（クラウド metadata endpoint と pastebin 系）は **SSRF + 情報流出経路の遮断**として維持。`allowedDomains` で許可してもこちらが優先で deny される。

## 各 sandbox オプションの意味

| キー | 値 | 意味 |
|------|-----|------|
| `enabled` | `true` | CC 起動時から sandbox を ON にする。`/sandbox` コマンドでの手動起動が不要 |
| `autoAllowBashIfSandboxed` | `true` | sandbox に閉じ込められた Bash subprocess は permission ダイアログ無しで自動許可。autonomous セッションが止まらない |
| `excludedCommands` | `docker / docker-compose / watchman / systemctl / launchctl / brew services` | sandbox 内で動かせない OS 系コマンドは sandbox 外で実行に逃がす |
| `network.allowedDomains` | 29 個 | 外向き通信を許可するホスト |
| `network.deniedDomains` | 9 個 | 許可リストにあっても拒否する（優先） |

## 検証

ユーザー手動編集後、以下で確認:

```bash
# JSON validity
jq -e '.' ~/.claude/settings.json > /dev/null && echo "VALID JSON"

# sandbox キーが存在するか
jq 'has("sandbox")' ~/.claude/settings.json
# → true

# allowedDomains の件数
jq '.sandbox.network.allowedDomains | length' ~/.claude/settings.json
# → 29

# 既存の enabledPlugins が壊れていないか
jq '.enabledPlugins | length' ~/.claude/settings.json
# → 既存件数 (claude-code-harness を含む)

# 実際に外向き通信が通るか (要 FIRECRAWL_API_KEY)
firecrawl scrape "https://techblog.zozo.com/" -o /tmp/test.md
```

## なぜ AI が自動で編集しないのか

`~/.claude/settings.json` は CC 自身を制約する security boundary。AI が自分の制約を勝手に緩める（self-tampering）のを防ぐため、CC の auto mode classifier と `Edit(.claude/settings*)` / `Write(.claude/settings*)` deny rule が **二重で**ブロックする。Bash 経由の迂回も classifier が「User Deny Rules circumvention」として deny する設計。

このため:
- AI 側: patch JSON を **提示するだけ**
- ユーザー側: 手動で適用 + 検証

これは harness の **責任境界**。AI に security 設定変更の自律権限は持たせない。

## トラブルシューティング

### 編集後も 403 が出る

1. JSON syntax error の可能性。`jq -e '.' ~/.claude/settings.json` で確認
2. CC を **完全再起動**（cmd+Q → 再起動）。sandbox 設定は session start 時に読まれる
3. `FIRECRAWL_API_KEY` 環境変数が未設定の可能性。`.zshrc` を確認

### 別のドメインが必要になった

`allowedDomains` 配列に追加するだけ。CC 2.1.113+ では `*.example.com` の wildcard も使えるが、**漏れの可視性のため明示列挙を推奨**。

### sandbox を一時的に外したい

`"enabled": false` にする。または `--no-sandbox` flag で起動。ただし security 後退するため一時利用に限る。

## 関連

- `templates/sandbox-settings.json.template` — harness の reference 設定。**本 recipe と 29 ドメイン allowlist + 9 ドメイン denylist が完全同期**。新規プロジェクトで一括コピーする場合はこちらをそのまま流用すると確実
- `CLAUDE.md` Permission Boundaries — sandbox 設定が AI による self-tampering 防止層と多層防御を構成
- `.claude/rules/cross-repo-handoff.md` — Layer 1 (server-side) / Layer 2/3 (client-side) の redact 設計
- CC v2.1.108+ sandbox 仕様: 公式 docs の `sandbox` セクション

## 履歴

- 2026-05-21: 初版作成。外部プロジェクトで Firecrawl が 403 になった事例を契機に docs 化
