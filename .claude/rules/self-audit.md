# Self-Audit Rule

## Integrity 検知

CLAUDE.md 末尾に `<!-- harness-integrity: ... -->` マーカーがある。
検知トリガー: `/harness-review` 実行時または診断セッション開始時に、
CLAUDE.md 末尾を **Read ツールで確認**し、以下をチェック:

1. `.claude-plugin/settings.json` の deny エントリが前回監査時から**減少**していないか
2. `.claude-plugin/settings.json` の deny に settings 自己書換保護の 4 パターン
   (`Edit/Write(.claude/settings*)` と `Edit/Write(.claude-plugin/settings*)`) が**揃っているか**
   (CLAUDE.md Permission Boundaries が約束する deny。`tests/validate-plugin.sh` の gate と対応)
3. CLAUDE.md に Feature Table が直接追記されていないか（ポインタのみが正）
4. `.claude/settings.local.json` が存在する場合、その `hooks` ブロックを **Read で確認**し、
   オーナーが意図しない hook（特に `command` 型）が追記されていないか検知する。
   settings.local.json は通常 gitignore 対象でコードレビューを通らないため、
   ここへの hook 注入は永続的なコード実行（persistence）の温床になる。
5. 乖離があれば `/harness-review` で診断を実行

マーカーの更新は人間のオーナーだけが行う。エージェントは読み取りと検知のみ。

## なぜこのルールが必要か

settings.json の deny ルールは「エージェント自身を制約する鎖」。
鎖の本数が減っていたら、意図しない緩和か改ざんの可能性がある。
絶対数ではなく減少方向を検知することで、正当な追加は許容しつつ緩和を捕捉する。

settings.local.json への hook 注入を別建てで監視するのは、deny
（`Write/Edit(.claude/settings*)`）が **ツール経路を塞ぐ preventive 層**であるのに対し、
Bash リダイレクト等の残余経路（guardrail では warn 止まり）に対しては
**detective な backstop** が必要だから。preventive（deny で書かせない）と
detective（後から注入を見つける）の二段で「鎖を外す」改ざんを捕捉する。
