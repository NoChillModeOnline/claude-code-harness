# Phase 93.1 Spike — Cross-Session Relay 内部実装 実装可能性検証

調査日: 2026-06-04
結論: **実装 GO**。CCH 内部実装で agmsg と同等の cross-session relay が成立する。配達は both モード (monitor + turn fallback) に確定。

## 目的

Phase 93「cross-session relay 内部実装」の 2 つの未確認点を実コードで検証し、外部ツール agmsg を install/連携せず CCH 内部実装で同等の relay が作れるかを確定する。

## 確認点 (i): harness signal storage が双方向 addressing を載せられるか

**結論: YES（拡張要）**

- 現状: `go/internal/hookhandler/inbox_check.go:20` の broadcast.md ヘッダ形式は `## <ISO8601> [sender-prefix]`。送信元（12 文字 prefix）のみ encode し、宛先フィルタは無い（全 broadcast を全 session が読み、送信元 self のみ除外: `:235-238`）。
- body: backtick 内の path のみ trusted structured field（`broadcastPathRe`）。それ以外は attacker-controllable text で verbatim 注入禁止。注入上限 `inboxInjectByteCap = 4096`。
- 実装方針（案 B 推奨）: `.claude/sessions/relay-signals.jsonl` を新設し `{from_session, to_session, body, ts}` で記録。inbox_check の filter を `(from==self OR to==self)` に拡張。既存 broadcast.md と分離して後方互換を保つ。
- 参考: agmsg は SQLite `messages(from_agent, to_agent, body)` で同じ addressing（`watch.sh:172-177`）。CCH は jsonl で等価実装できる。

## 確認点 (ii): SessionStart hook から Monitor 常駐起動が可能か

**結論: 実装可能（stdout directive 方式）。確実性が LLM 依存ゆえ both モードで turn fallback を添える。**

- 決定的根拠: agmsg `scripts/session-start.sh:171-188` は SessionStart hook の **stdout に自然言語の指示**を出すだけ:
  ```
  AGMSG monitor mode: invoke the Monitor tool now with the following parameters,
  before any other action in this session.
    command: <watch.sh> <session_id> <project> <type>
    description: agmsg inbox stream
    persistent: true
  ```
  CC（LLM）がこれを読んで Monitor tool を起動する。
- CCH 適用: SessionStart **command-type hook**（現状 `hooks/hooks.json:115-144` は全て command type）の stdout に同形式の directive を出せば、CC が Monitor で session-relay-watch.sh を常駐起動する。CCH の SessionStart=command 限定 rule（`.claude/rules/hooks-2.1.139-plus.md`）に抵触しない。
- 弱点: directive は additionalContext の指示で、CC が従う保証は LLM 依存（強制ではない）。agmsg は "before any other action" と強く指示して従わせている。
- 対策: **both モード**（agmsg delivery mode `both` と同じ）。
  - monitor（stdout directive、best-effort）: 即時 5 秒 push
  - turn（PreToolUse poll、確実）: tool 実行ごとに relay-signals.jsonl を pull
  - monitor が滑っても turn で relay が死なない。degrade-safe。

## 確認点 (iii): agmsg watch.sh のエッジケース移植リスト

session-relay-watch.sh に移植必須（`/Users/tachibanashuuta/LocalWork/Code/agmsg/scripts/watch.sh`）:

1. **high-water mark**（`watch.sh:163-168`）: 起動時に MAX(id) を取得し、以降はそれ以上の id だけ fetch。履歴 replay を防止。CCH は jsonl の行 offset or ID sequence で等価実装。
2. **signal trap 即時 shutdown**（`watch.sh:189-194`）: `sleep $INTERVAL &` + `wait $!` で SIGTERM/INT を即座に発火（foreground sleep だと trap が defer され shutdown が INTERVAL 秒遅延）。
3. **pidfile race ガード**（`watch.sh:58-66`, `session-start.sh:103-167`）: 再起動時に前 holder を kill する前に `kill -0` 生存確認 + `ps -o args=` cmdline 照合（pid recycling 対策）。EXIT trap は自分の pid のときだけ pidfile 削除。

## 設計確定

| 軸 | 確定 |
|---|---|
| storage | `.claude/sessions/relay-signals.jsonl`（双方向 from→to addressing、jsonl、後方互換）。harness-mem redaction（Layer 1-3）に乗る |
| 配達 | both モード = monitor（SessionStart stdout directive）+ turn（PreToolUse poll fallback）|
| untrusted-data 隔離 | 受信 body は構造化信頼フィールドのみ注入 + disclaimer wrap（`inbox_check.go` の `broadcastPathRe` パターン踏襲）。上限 4096 bytes |
| opt-in | `HARNESS_SESSION_RELAY=monitor`（or `both`）で配布 default OFF |
| エッジケース | high-water mark / signal trap / pidfile race を watch.sh から移植 |
| harness-loop | Monitor tool 取り合いのため二者択一（同時併用しない契約を doc 明記）|

## Phase 92 副産物（同時調査で確定）

- `waitingFor`（2.1.162）: **A（doc 追記）**。CCH は `claude agents --json` を実コードで poll していない（CHANGELOG の歴史記述のみ、`docs/agent-view-policy.md` は Phase 69.2.2 で実在）。同 doc に「Lead が session 監視で stuck 検知に使える」を反映。
- `CLAUDE_CODE_TMPDIR`（2.1.162 SendMessage 修正）: **C（CC 自動継承）**。CCH は TMPDIR を設定しない（grep 0 件）。
- shell-config gate（2.1.160）: **A**。settings の ask + R01-R13 に shell startup / build-tool config を追加して CC のガードと二重化。
- B（書いただけ）= 0 件。
