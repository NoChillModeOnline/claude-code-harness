# Glossary — Plain-Language Terms / 用語集（やさしい言葉で）

Harness shows a few words in its plans, reports, and HTML screens that look
technical. This page explains each one in one plain sentence so a non-engineer
can read the output and decide "is this OK?" without opening any code.

Harness の計画・レポート・HTML 画面には、少し専門的に見える言葉が出ます。
このページは各語を**1 文のやさしい説明**にしているので、コードを開かなくても
「これで OK か？」を自分で判断できます。

> If you only read one thing: when Harness asks you to **approve** something,
> it is asking "does this match what you wanted?" — not "is this code correct?".
> あなたが見るのは「頼んだ内容と合っているか」であって、「コードが正しいか」ではありません。

---

## Core terms / 中心の言葉

| Term | Plain meaning (EN) | やさしい説明 (JA) |
|------|--------------------|-------------------|
| **spec** (spec.md) | The "what we agreed to build" document — the product promise, in words. | 「何を作るか」を言葉で決めた約束ごと。 |
| **Plans.md** | The to-do list Harness works from — each line is one task. | Harness が見ながら進める ToDo リスト。1 行が 1 タスク。 |
| **contract** | A task written clearly enough that "done / not done" is unambiguous. | 「終わった／終わってない」が誰でも判断できるレベルまで具体化したタスク。 |
| **draft** | A first version for you to check — not final, you can ask for changes. | あなたが確認するための下書き。最終版ではなく、修正を頼んでよい。 |
| **approve** | "Yes, this matches what I asked for (not a judgement on whether the code is perfect), go ahead." | 「頼んだ内容と合っている（コードの完璧さの判定ではない）。進めてOK」の合図。 |

## Status markers you'll see in Plans.md / Plans.md で見る進捗マーク

| Marker | Plain meaning (EN) | やさしい説明 (JA) |
|--------|--------------------|-------------------|
| `cc:TODO` | Not started yet. | まだ手をつけていない。 |
| `cc:WIP` | In progress right now. | いま作業中。 |
| `cc:完了` | Finished and verified. | 終わって、確認も済んだ。 |
| `Spec delta` | "This task changes the product promise" — i.e. it changes what success looks like. Read it before approving; you may need to re-check the overall scope/timeline. | 「このタスクは約束ごと自体を変える（＝完成の定義が変わる）」という注記。承認前に読み、実装範囲の確認が必要な場合がある。 |

## Report words (Plan Brief / Progress / Acceptance HTML) / レポートの言葉

| Term | Plain meaning (EN) | やさしい説明 (JA) |
|------|--------------------|-------------------|
| `$easy` report | A short report style: conclusion first, then reasons, then "what happens next." | 「結論 → 理由 → 次にどうなるか」の順に短くまとめた報告スタイル。 |
| **confidence %** | How sure Harness is. This is **your** rule of thumb, not an automatic switch — Harness does not decide for you based on this number. A common personal guide: 75%+ usually fine to proceed, 40–74% proceed with caution, under 40% pause and ask. Always read the report's own explanation too. | Harness の自信度。これは**あなた自身**の目安であり自動スイッチではありません（この数値で Harness が勝手に決めることはしません）。よくある個人的な目安: 75%以上=だいたい進めてOK、40〜74%=注意、40%未満=止めて相談。レポート本文の説明も必ず読んでください。 |
| **acceptance criteria** | The checklist that decides "done" — what must be true to ship. | 「完成」を判定するチェックリスト。出荷の条件。 |
| **ship / wait / reject** | Final call: release it / hold it / send it back. | 最終判断: 出す／保留／差し戻し。 |
| **risk: info / warn / critical** | How serious a flagged risk is — `critical` means stop and check. | 危険度の段階。`critical` は止めて確認。 |

## Words you only need if something looks off / 何か変なときだけ見る言葉

| Term | Plain meaning (EN) | やさしい説明 (JA) |
|------|--------------------|-------------------|
| **drift** | Reality and the plan no longer match — usually fix by re-syncing. | 計画と実際がズレた状態。だいたい再同期で直る。 |
| `team_validation_mode` | How the plan was double-checked. `unavailable` means it was **not** checked — consider re-planning. | 計画の二重チェック方法。`unavailable` は未チェックなので作り直しを検討。 |
| **harness-mem** | Optional long-term memory of past decisions. Safe to ignore if you didn't set it up. | 過去の決定を覚えておく任意の長期メモリ。設定していなければ無視してOK。 |
| **mirror** | Copies of the skills for other AI tools (such as Codex / opencode). Internal housekeeping — you can ignore it. | 他の AI ツール (Codex / opencode など) 向けのスキル複製。内部の整合管理なので無視してOK。 |
| **guardrail (R01–R15)** | Automatic safety blocks (e.g. no force-push, no committing secrets). They protect you. | 自動の安全ブロック（force-push 禁止、秘密のコミット禁止 等）。あなたを守る仕組み。 |

---

## What to do when you're unsure / 迷ったときの頼み方

You never have to learn commands. Just say it in plain words:

コマンドを覚える必要はありません。普通の言葉で頼んでOK:

- "どうすればいい？" / "what's next?" → Harness suggests the next step.
- "これで合ってる？" / "is this right?" → Harness explains in plain language.
- "やっぱり違う、やり直して" / "no, redo it" → Harness reworks it safely.

See also: [Tool-first onboarding](index.md) · [Install routes](install.md)
