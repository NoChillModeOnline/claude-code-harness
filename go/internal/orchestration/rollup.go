// Package orchestration wires the Phase 90 orchestration-visibility ledger
// rollup into the Go hook handlers.
//
// The rollup logic itself lives in scripts/orchestration-rollup.sh (so it stays
// shell-unit-testable and shared with non-Go callers). The Go handlers only
// invoke it — at full-session completion (TaskCompleted) and again at session
// end (SessionEnd) as a safety net. Because the script is idempotent per
// session_id, invoking it from both points never double-counts.
package orchestration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// rollupTimeout caps the rollup so a hung jq can never block a hook.
const rollupTimeout = 5 * time.Second

// Run invokes scripts/orchestration-rollup.sh for the given session, folding the
// session's counted delegations into the lifetime accumulator.
//
// Record-only and fail-open: the script writes nothing to stdout, and any error
// here is intentionally ignored so orchestration telemetry never breaks a hook.
// sessionID may be empty — the script then resolves it from CLAUDE_SESSION_ID or
// session.json under projectRoot.
func Run(projectRoot, sessionID string) {
	script := resolveRollupScript()
	if script == "" {
		return
	}

	args := []string{script}
	if sessionID != "" {
		args = append(args, sessionID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rollupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", args...)
	if projectRoot != "" {
		cmd.Dir = projectRoot
	}
	// Errors are deliberately ignored (fail-open).
	_ = cmd.Run()
}

// resolveRollupScript locates scripts/orchestration-rollup.sh relative to the
// running binary (<root>/bin/harness -> <root>/scripts/...), falling back to
// CLAUDE_PLUGIN_ROOT. Returns "" if not found.
func resolveRollupScript() string {
	if exe, err := os.Executable(); err == nil {
		root := filepath.Dir(filepath.Dir(exe)) // <root>/bin/harness -> <root>
		candidate := filepath.Join(root, "scripts", "orchestration-rollup.sh")
		if fileExists(candidate) {
			return candidate
		}
	}
	if pr := os.Getenv("CLAUDE_PLUGIN_ROOT"); pr != "" {
		candidate := filepath.Join(pr, "scripts", "orchestration-rollup.sh")
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
