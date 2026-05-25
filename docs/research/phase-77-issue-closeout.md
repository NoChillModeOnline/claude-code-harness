# Phase 77 Issue Closeout Evidence

Updated: 2026-05-25

## Issue #140: R02/R03 Protected Path Opt-Out

Issue: https://github.com/Chachamaru127/claude-code-harness/issues/140

### Current Decision

Partial closeout only.

Harness now covers the narrow operational pain through an R03 `.env` break-glass
ask-list, but it does not implement broad `disabled_rules`, full R02 opt-out, or
silent allow. That is intentional because R02 protects direct Write/Edit/
MultiEdit to secret files, while the requested deployment case is a shell-write
workflow that can be confirmed with a reason.

### Evidence

- `harness.toml:89-95` documents `[[safety.guardrail.protectedPathAskList]]`
  with a required `reason`, and states that R02 plus hard-deny paths remain
  denied.
- `go/internal/guardrail/rules_test.go:259-278` verifies R03 `.env` shell write
  becomes `DecisionAsk` and the reason contains `R03`, `.env`, `harness.toml`,
  and the configured reason without echoing the secret value.
- `go/internal/guardrail/rules_test.go:280-291` verifies the same ask-list does
  not bypass R02 Write/Edit deny.
- `go/internal/guardrail/rules_test.go:293-412` verifies empty reason, non-exact
  paths, project-external paths, hard-deny paths, and mixed hard-deny targets do
  not bypass protection.
- `go/internal/guardrail/rules_test.go:414-424` records that `sed -i .env`
  target extraction is still out of scope for the current R03 extractor.

Validation:

```bash
cd go && go test ./internal/guardrail
```

Result: PASS.

### Comment Draft

```markdown
Implemented a narrower safety-preserving path for the deployment use case:

- `[[safety.guardrail.protectedPathAskList]]` in `harness.toml`
- R03 shell writes to an exact/narrow `.env` path can downgrade from deny to ask
  when a non-empty reason is configured
- the ask reason includes rule id, matched path, config source, and configured
  reason, without echoing secret values

What this intentionally does not do:

- no broad `disabled_rules`
- no R02 Write/Edit/MultiEdit bypass for `.env`
- no silent allow
- no bypass for hard-deny paths such as `.git/`, `secrets/`, `*.pem`, `*.key`,
  SSH trust files, shell rc/profile files, `.claude/hooks`, or `.husky`

Validation:

`cd go && go test ./internal/guardrail`

Boundary: the original `sed -i .env` reproduction remains a separate extractor
scope. Current R03 coverage is redirection / tee style shell writes. I would
close this as the safe v1 break-glass path, and track `sed -i` extraction only
if we want to expand R03 target detection separately.
```

## Issue #131: codex-loop Orphan Active Job

Issue: https://github.com/Chachamaru127/claude-code-harness/issues/131

### Current Decision

Ready to close after review. The current implementation has dedicated stale
runner detection for active jobs and stop-time cancellation/reconciliation.

### Evidence

- `scripts/codex-loop.sh:2876-2908` maps dead runner plus active current job to
  `runner_lost_job_running`, preserves `current_job_id`, and writes an explicit
  error explaining that the runner died while the active job is still recorded.
- `scripts/codex-loop.sh:2964-3006` attempts to cancel the active job during
  `stop`; if cancellation fails, it keeps `runner_lost_job_running` and records
  `stop_cancel_failed` instead of pretending the run is stopped.
- `scripts/codex-loop.sh:3147-3156` preserves a child `run-cycle`
  `runner_exit` failure instead of overwriting it with generic `cycle_error`,
  so the run keeps the explicit failure reason after active-job cancellation.
- `tests/test-codex-loop-cli.sh:838-904` verifies `status --json` reports the
  dedicated state and `stop` cancels/reconciles the active companion job.
- `tests/test-codex-loop-cli.sh:906-958` verifies failed cancellation remains
  unreconciled and visible instead of being falsely closed.
- `tests/test-codex-loop-cli.sh:961-1002` verifies runner failure cancels the
  active job and records the explicit `runner_exit` failure.

Validation:

```bash
bash tests/test-codex-loop-cli.sh
```

Result: PASS on rerun (`passed=42 failed=0`). One earlier local run had a
single failure because the parent run was overwriting the child `runner_exit`
state with `cycle_error`; this was fixed and the full test now passes
(`passed=42 failed=0`).

### Comment Draft

```markdown
This looks addressed by the current codex-loop recovery path.

Implemented / verified behavior:

- dead runner + active `current_job_id` is reported as `runner_lost_job_running`
  instead of generic `state_stale`
- `status --json` preserves the active job id and records a clear error message
- `stop` attempts to cancel the active companion/local job
- if cancellation fails, the run remains unreconciled with `stop_cancel_failed`
  instead of pretending the job stopped safely
- if a child `run-cycle` exits unexpectedly after launching a job, the parent
  preserves the explicit `runner_exit` failure instead of overwriting it with a
  generic cycle error
- companion stdout JSON parsing remains separate from stderr noise

Validation:

`bash tests/test-codex-loop-cli.sh`

Result: `passed=42 failed=0`.

I would close this unless someone can reproduce a new active-job orphan shape on
the current main branch.
```

## Issue #149: awesome-codex-plugins Listing Decision

Issue: https://github.com/Chachamaru127/claude-code-harness/issues/149

External target: https://github.com/hashgraph-online/awesome-codex-plugins

### Current Decision

Do not submit the external listing PR yet.

The target registry is not a README-only list. Its current contribution rules
require a mirrored plugin bundle under `plugins/<owner>/<repo>/`, a valid
`.codex-plugin/plugin.json`, and an icon referenced by
`interface.composerIcon`. This repo has a valid Codex plugin manifest for
`Chachamaru127/claude-code-harness`, but the manifest currently has no
`composerIcon` field and the repo has no dedicated icon asset. The target list
also already contains a different `Claude Code Harness` entry for
`dadwadw233/claude-code-harness`, so a direct same-name PR would be ambiguous.

Recommended path:

1. Add a small dedicated icon asset and `interface.composerIcon` to this repo's
   `.codex-plugin/plugin.json` in a separate compatibility task.
2. Submit one external PR to `hashgraph-online/awesome-codex-plugins` that adds
   `plugins/Chachamaru127/claude-code-harness/` plus one README entry.
3. Keep the listing description inside the current support boundary: Codex CLI
   workflow skills are supported; Codex app proof remains tracked separately.

### Evidence

- Issue #149 says the target accepts direct PRs to `README.md`, but the current
  `CONTRIBUTING.md` additionally requires a plugin bundle under
  `plugins/<owner>/<repo>/`, a valid `.codex-plugin/plugin.json`, and an icon
  referenced by `interface.composerIcon`.
- Target repo current main SHA checked: `68f58ceb19873302b0e771040d47050fc0e5f638`.
- Target README currently has an entry:
  `[Claude Code Harness](https://github.com/dadwadw233/claude-code-harness)`.
- Target mirrored bundle currently exists at
  `plugins/dadwadw233/claude-code-harness/`.
- This repo's `.codex-plugin/plugin.json` points at
  `https://github.com/Chachamaru127/claude-code-harness`, version `4.12.2`, and
  describes Codex CLI compatibility, but has no `interface.composerIcon`.
- `find assets -maxdepth 2 -type f \( -name '*icon*' -o -name '*logo*' \)`
  returned no dedicated icon/logo asset in this repo.

### Proposed Listing Text After Icon Readiness

```markdown
- [Claude Code Harness](https://github.com/Chachamaru127/claude-code-harness) - Evidence-backed plan, work, review, and release workflow skills for Codex CLI users.
```

If the existing same-name entry remains, the PR should either ask maintainers to
replace the older blueprint entry or use a disambiguated display name such as
`Claude Code Harness Workflow`.

### Issue Comment Draft

```markdown
I checked the current awesome-codex-plugins contribution contract before opening
a PR.

Result: not PR-ready yet, but the path is clear.

Why:

- the target repo currently requires more than a README line: it expects a
  mirrored bundle under `plugins/<owner>/<repo>/`
- it also requires an icon referenced by `interface.composerIcon`
- this repo has `.codex-plugin/plugin.json`, but it does not yet include
  `interface.composerIcon`
- the target list already contains a different `Claude Code Harness` entry for
  `dadwadw233/claude-code-harness`, so a same-name PR would be ambiguous

Recommended next step:

1. add a small dedicated icon asset and `interface.composerIcon` here
2. open one external PR adding `plugins/Chachamaru127/claude-code-harness/`
3. use a support-bounded description, for example:

`Claude Code Harness - Evidence-backed plan, work, review, and release workflow skills for Codex CLI users.`

I would not add new support claims to this repo's README just for the listing.
```
