# Skill Consolidation Decision: cognitive-load trio

Status: 判断保留 (実装は別 Phase で判断確定後)
Created: 2026-06-18 (Phase 94.2.2 / #200 medium-term)

## 背景

Issue #200 で Skill listing budget overflow (28 skills sent to LLM = 9089 chars > 6000 budget) が報告された。短期対応 (Phase 94.2.1) で上位 10 件の description を trim し total を 8099 chars に圧縮したが、6000 chars 厳格達成には残り 28 件の trim か、**重複機能を持つ skill の統合**が必要。

本 doc では cognitive-load trio (`harness-accept` + `harness-plan-brief` + `harness-progress`) を 1 つの skill に統合する案を検証する。

## 対象 skills

| Skill | 役割 | description trim 後 (chars) | 入口語 |
|---|---|---|---|
| `harness-plan-brief` | 着手前: Plan Brief HTML (理解・選択肢・リスク・受け入れ基準) | 196 | 計画概要, planning preview, 計画レビュー |
| `harness-progress` | 進行中: Progress Tracker HTML (cc:WIP / cc:TODO / cc:完了 + drift alerts) | 187 | progress tracker, 進捗確認, dashboard |
| `harness-accept` | 完了直後: Acceptance Demo HTML (ship/wait/reject + 検収) | 195 | 受け入れ判断, ship/wait/reject, 検収レビュー |

3 件 合計 description: **578 chars**。3 surface すべて非エンジニア向け HTML 生成という単一責務系統 (Phase 65 cognitive-load surface 設計)。

## 統合案: `harness-cognitive` 単一 skill + サブコマンド

### 提案する surface

```
/harness-cognitive plan       — Plan Brief (着手前)
/harness-cognitive progress   — Progress Tracker (進行中)
/harness-cognitive accept     — Acceptance Demo (完了直後)
```

`argument-hint: "plan|progress|accept [--out <path>] [--no-open]"` の単一 skill にする。

## 比較表

| 観点 | 現状 (3 skill 独立) | 統合案 (1 skill + サブコマンド) |
|---|---|---|
| description budget | 3 件で 578 chars | 1 件で ~250 chars → **約 328 chars 削減** |
| trigger 精度 (auto-loading) | 各 skill の trigger phrase が分離 (高精度) | 統合後は trigger が分散して low-precision recall になる懸念 |
| SKILL.md 維持コスト | 3 ファイル独立 (各責務が明確) | 1 ファイルにサブコマンド分岐 (内部分岐が複雑化) |
| ユーザー学習コスト | `/harness-plan-brief` を一発で打てる (人の言語と一致) | `/harness-cognitive plan` の subcommand 規約を覚える必要 |
| references/ 共有 | 一部重複 (HTML render helper など) | 統合で重複削減できる (副次効果) |
| 既存 ユーザー impact | breaking change なし | `/harness-plan-brief` 等を打つユーザーは新エイリアスが必要 |
| README / docs 修正範囲 | 既存どおり | docs/cognitive-load-surfaces.md などの参照 path 修正 |
| budget over 解消への寄与 | 0 chars 削減 | 約 328 chars 削減 (残 budget 達成にはこれ単独では不足) |

## 判断

**保留**。理由:

1. **budget 達成への単独寄与が小さい**: 統合で 328 chars 削減できても、残り 28 件の trim と合わせなければ 6000 chars 達成不可。trim と統合を**両方やる**より、trim だけで達成できる目処が立つなら統合は不要。
2. **trigger 精度低下のリスク**: 「進捗確認」「ship/wait/reject」「計画レビュー」は非エンジニアの自然語で、現状の独立 skill だと auto-loading で確実にヒットする。統合 skill だと description に 3 surface 分の trigger phrase を詰める必要があり、LLM が誤分岐するリスク。
3. **breaking change の正当化が弱い**: `/harness-plan-brief` 等の既存呼び出しを抜本変更する justification が「budget 圧縮 328 chars」だけでは弱い。Phase 65 で 3 surface を意図的に分離した設計判断 (`docs/cognitive-load-surfaces.md`) を覆すには根拠不足。

## 推奨される次の Phase action

| Action | Phase 候補 | 優先度 |
|---|---|---|
| 残り 28 件の description を trim (中位 verbose 上位 10 件 → ≤200 chars) | Phase 95.1 (#200 long-term) | 高 |
| 全 38 件を ≤150 chars に揃え (Issue #200 元 DoD 厳格達成) | Phase 95.2 (任意) | 中 |
| 統合 case を再検討 (本 doc) — budget over が trim だけで解消できなかった場合のみ | Phase 96 以降 | 低 |
| Description-ja の trim (本 Phase 範囲外、別軸の i18n SSOT 整理) | Phase 96+ | 低 |

## 関連

- Issue [#200](https://github.com/Chachamaru127/claude-code-harness/issues/200)
- `docs/cognitive-load-surfaces.md` (Phase 65 — 3 surface 分離の設計判断 SSOT)
- `scripts/check-skill-description-budget.sh` (Phase 94.2.1)
- Plans.md 94.2.1 / 94.2.2

## 見直し条件

- Trigger A: Phase 95.1 で残り 28 件 trim しても total が 6000 chars 達成できない場合 → 統合を再検討
- Trigger B: CC 側で skill description budget が引き上げられた場合 (例: ≥10000 chars) → 統合不要、trim も最小限
- Trigger C: 3 surface のユースケースが実運用で「同じ局面で連続的に呼ばれる」事実が観測された場合 → 統合の付加価値が出る
