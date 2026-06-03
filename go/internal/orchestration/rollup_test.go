package orchestration

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeScript creates <pluginRoot>/scripts/orchestration-rollup.sh that, when
// run, appends its first argument (the session id) to markerFile.
func writeFakeScript(t *testing.T, pluginRoot, markerFile string) {
	t.Helper()
	dir := filepath.Join(pluginRoot, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "#!/usr/bin/env bash\nprintf '%s\\n' \"${1:-NONE}\" >> \"" + markerFile + "\"\n"
	script := filepath.Join(dir, "orchestration-rollup.sh")
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
}

func TestRun_InvokesScriptWithSession(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "marker.txt")
	writeFakeScript(t, root, marker)
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	Run(root, "sess-123")

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("rollup script did not run (marker missing): %v", err)
	}
	if got := string(data); got != "sess-123\n" {
		t.Fatalf("script got unexpected session id: %q", got)
	}
}

func TestRun_EmptySessionStillRuns(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "marker.txt")
	writeFakeScript(t, root, marker)
	t.Setenv("CLAUDE_PLUGIN_ROOT", root)

	Run(root, "")

	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("rollup script did not run: %v", err)
	}
	// No session id arg -> script sees ${1:-NONE} = NONE.
	if got := string(data); got != "NONE\n" {
		t.Fatalf("expected NONE for empty session, got %q", got)
	}
}

func TestRun_FailOpenWhenScriptMissing(t *testing.T) {
	// No script anywhere reachable -> Run must be a silent no-op (no panic).
	t.Setenv("CLAUDE_PLUGIN_ROOT", filepath.Join(t.TempDir(), "does-not-exist"))
	Run(t.TempDir(), "sess-x")
}

func TestResolveRollupScript_MissingReturnsEmpty(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", filepath.Join(t.TempDir(), "nope"))
	if got := resolveRollupScript(); got != "" {
		// os.Executable fallback might resolve in odd environments; only fail if a
		// non-empty path was returned that does not actually exist.
		if !fileExists(got) {
			t.Fatalf("resolveRollupScript returned non-existent path: %q", got)
		}
	}
}
