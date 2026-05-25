# Phase 77 Review Closeout

Updated: 2026-05-25

## Verdict

APPROVE by manual-pass fallback.

`scripts/codex-companion.sh review --base origin/main` was attempted first, but
the Codex token refresh failed because the account session needs re-auth. The
`harness-review` contract permits a read-only `manual-pass` fallback when Codex
review is unavailable, as long as the fallback records evidence, tests, and
review lenses.

## Scope

Reviewed uncommitted working-tree changes for Phase 77:

- English canonical Plans marker writer/read compatibility for Issue #147
- guardrail/runtime English default verification for Issue #146
- Issue #140 and #131 closeout evidence
- Dependabot PR #136/#137 merge evidence
- Issue #149 listing decision evidence
- runner-exit preservation fix in `scripts/codex-loop.sh`
- CHANGELOG user-facing summary

No untracked files were present.

## Review Command Evidence

Attempted:

```bash
bash scripts/codex-companion.sh review --base origin/main
```

Result: failed due Codex auth refresh:

```text
Your access token could not be refreshed because you have since logged out or signed in to another account. Please sign in again.
```

Fallback used: `team_agent_mode=manual-pass`.

## Manual TeamAgent Pass

### Spec Agent

PASS. `spec.md` now states that new/update writer paths emit the English marker
family while legacy Japanese markers remain read-compatible. This matches
`docs/i18n-language-contract.md` and the new tests in
`tests/test-plans-status-markers.sh`.

### Plans Agent

PASS with one boundary. `Plans.md` Phase 77.1.1-77.1.8 are done and have
evidence. 77.1.9 should not close public issues until the fix branch is merged,
because #147 and the #131 runner-exit preservation fix are not on main yet.

### Regression Agent

PASS. Legacy markers remain accepted by parsing, counting, summaries, and
format checks. New writer prompts/templates use `cc:done`. The runner failure
case now preserves `runner_exit` instead of overwriting the state with
`cycle_error`, and `test-codex-loop-cli.sh` passes after the fix.

### Skeptic Agent

PASS with residual risk. The main residual risk is external state, not code:
public issue close/comment should happen after merge, and awesome-codex-plugins
listing should wait for a dedicated icon plus `interface.composerIcon`.

## Static Review

```bash
bash scripts/review-ai-residuals.sh --base-ref HEAD --include-untracked
```

Result: `APPROVE`, `major=0`, `minor=27`.

Rejected minor findings:

- `cc:TODO` / `cc:WIP` findings are compatibility markers, not unfinished TODOs.
- `fake completion` findings are negative instructions telling workers not to
  fake completion, not fake implementation data.

`scripts/review-weak-supervision-report.sh` was not run because there is no
Phase 77 weak-supervision report artifact in this change.

## Validation

Passed:

```bash
bash tests/test-plans-status-markers.sh
bash tests/test-i18n-locale-resolver.sh
bash tests/test-i18n-japanese-ux-regression.sh
go test ./internal/guardrail ./internal/hookhandler ./internal/session
bash tests/test-codex-loop-cli.sh
bash tests/validate-plugin.sh
bash scripts/plans-format-check.sh Plans.md
git diff --check
```

Notable results:

- `bash tests/test-codex-loop-cli.sh`: `passed=42 failed=0`
- `bash tests/validate-plugin.sh`: `passed=95 warnings=0 failures=0`

Remote main after PR #136/#137:

- #136 merge commit: `9fd2071b9c40cdd3a4ba6b4e427af8fd89ee302e`
- #137 merge commit: `5f33130b3e6bd95de10e97bb0dfdb1faa75bfda9`
- `validate-plugin` runs `26377642567` / `26377646959`: success
- `scorecard` runs `26377642555` / `26377646960`: success

## Residual Risk

- Do not close #147 before this branch is merged.
- Do not close #131 before this branch is merged, because the final
  `runner_exit` preservation fix is in this branch.
- #140 can be closed after review/merge with the safe-v1 boundary comment.
- #146 can be closed after review/merge with the English default + Japanese
  opt-in validation evidence.
- #149 should remain open or receive the prepared comment until the icon and
  listing bundle requirements are satisfied.

## Final Decision

Manual review approves the code/docs/test changes. Public issue closeout should
be executed after the change lands on main, not before.
