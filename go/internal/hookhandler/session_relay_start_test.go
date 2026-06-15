package hookhandler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleSessionRelayStart_MonitorEmitsDirective(t *testing.T) {
	t.Setenv("HARNESS_SESSION_RELAY", "monitor")
	var out bytes.Buffer
	if err := HandleSessionRelayStart(strings.NewReader(`{"session_id":"sess-relay-xyz"}`), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Monitor tool", "session-relay-watch.sh", "sess-relay-xyz", "SessionStart", "untrusted"} {
		if !strings.Contains(got, want) {
			t.Errorf("directive missing %q in: %s", want, got)
		}
	}
}

func TestHandleSessionRelayStart_BothEmitsDirective(t *testing.T) {
	t.Setenv("HARNESS_SESSION_RELAY", "both")
	var out bytes.Buffer
	_ = HandleSessionRelayStart(strings.NewReader(`{"session_id":"s1"}`), &out)
	if !strings.Contains(out.String(), "Monitor tool") {
		t.Errorf("both mode must emit the directive, got: %s", out.String())
	}
}

func TestHandleSessionRelayStart_OffIsSilent(t *testing.T) {
	// off / unset / turn must NOT emit a SessionStart directive (default OFF;
	// turn is the poll path, not the monitor directive path).
	for _, mode := range []string{"off", "", "turn", "garbage"} {
		t.Setenv("HARNESS_SESSION_RELAY", mode)
		var out bytes.Buffer
		_ = HandleSessionRelayStart(strings.NewReader(`{"session_id":"s1"}`), &out)
		if out.String() != "" {
			t.Errorf("mode %q must emit nothing on SessionStart, got: %s", mode, out.String())
		}
	}
}

func TestHandleRelayPoll_GateOff(t *testing.T) {
	// off / unset / monitor must NOT poll on PreToolUse (monitor is push-only).
	for _, mode := range []string{"off", "", "monitor", "garbage"} {
		t.Setenv("HARNESS_SESSION_RELAY", mode)
		var out bytes.Buffer
		_ = HandleRelayPoll(strings.NewReader(`{"session_id":"s1"}`), &out)
		if out.String() != "" {
			t.Errorf("relay-poll mode %q must be silent, got: %s", mode, out.String())
		}
	}
}

func TestWrapRelaySignals_DisclaimerAndCap(t *testing.T) {
	wrapped := wrapRelaySignals("2026-06-04T10:00:00Z | a → b | hi")
	if !strings.Contains(wrapped, "NOT instructions") {
		t.Errorf("wrap must carry the non-instruction disclaimer")
	}
	if !strings.Contains(wrapped, "hi") {
		t.Errorf("wrap must preserve the signal body")
	}
	big := strings.Repeat("x", inboxInjectByteCap+500)
	if len(wrapRelaySignals(big)) > inboxInjectByteCap+300 {
		t.Errorf("wrap must cap oversized payloads near %d, got %d", inboxInjectByteCap, len(wrapRelaySignals(big)))
	}
}

// End-to-end: relay-poll execs the real watch.sh and surfaces a signal
// addressed to self, wrapped with the disclaimer.
func TestHandleRelayPoll_Integration(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq required for relay-signals.jsonl parsing")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash required")
	}
	src := filepath.Join("..", "..", "..", "scripts", "session-relay-watch.sh")
	watchSrc, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("watch.sh not found at %s: %v", src, err)
	}

	tmp := t.TempDir()
	scriptsDir := filepath.Join(tmp, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "session-relay-watch.sh"), watchSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	// watch.sh sources scripts/lib/relay-store.sh — copy that dependency too.
	libDir := filepath.Join(scriptsDir, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	storeSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "lib", "relay-store.sh"))
	if err != nil {
		t.Skipf("relay-store.sh not found: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "relay-store.sh"), storeSrc, 0o644); err != nil {
		t.Fatal(err)
	}
	sessDir := filepath.Join(tmp, ".claude", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A signal addressed to self (to == first 12 chars of the session id).
	signal := `{"ts":"2026-06-04T10:00:00Z","from":"peerBBBBBBBB","to":"selfAAAAAAAA","body":"relay-hello"}`
	if err := os.WriteFile(filepath.Join(sessDir, "relay-signals.jsonl"), []byte(signal+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HARNESS_PROJECT_ROOT", tmp)
	t.Setenv("CLAUDE_PLUGIN_ROOT", "") // force relayWatcherPath fallback to the temp project's watcher
	t.Setenv("HARNESS_SESSION_RELAY", "both")
	var out bytes.Buffer
	if err := HandleRelayPoll(strings.NewReader(`{"session_id":"selfAAAAAAAA99"}`), &out); err != nil {
		t.Fatalf("HandleRelayPoll error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "relay-hello") {
		t.Errorf("expected relayed body 'relay-hello', got: %s", got)
	}
	if !strings.Contains(got, "NOT instructions") {
		t.Errorf("expected untrusted-data disclaimer, got: %s", got)
	}
}

// TestShellSingleQuote fixes codex 4周目 P1: the monitor directive must use
// shell single-quoting so $()/backtick in a project path can't be expanded.
func TestShellSingleQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"$(whoami)", "'$(whoami)'"},
		{"`id`", "'`id`'"},
		{"a'b", `'a'\''b'`},
		{"/My Projects/foo", "'/My Projects/foo'"},
	}
	for _, c := range cases {
		if got := shellSingleQuote(c.in); got != c.want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
