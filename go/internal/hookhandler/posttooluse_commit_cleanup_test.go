package hookhandler

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitCleanupHandler_EmptyInput(t *testing.T) {
	h := &CommitCleanupHandler{}

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// サイレントクリーンアップ: 出力なし
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func TestCommitCleanupHandler_NotBashTool(t *testing.T) {
	h := &CommitCleanupHandler{}
	input := `{"tool_name":"Read","tool_input":{"command":"git commit -m test"}}`

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for non-Bash tool, got %q", out.String())
	}
}

func TestCommitCleanupHandler_NotGitCommit(t *testing.T) {
	h := &CommitCleanupHandler{}
	input := `{"tool_name":"Bash","tool_input":{"command":"git status"},"tool_result":"On branch main"}`

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for non-commit command, got %q", out.String())
	}
}

func TestCommitCleanupHandler_GitCommitSuccess_ClearsFiles(t *testing.T) {
	dir := t.TempDir()

	// レビューファイルを作成
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	reviewState := filepath.Join(stateDir, "review-approved.json")
	reviewResult := filepath.Join(stateDir, "review-result.json")
	if err := os.WriteFile(reviewState, []byte(`{"approved":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewResult, []byte(`{"verdict":"APPROVE"}`), 0600); err != nil {
		t.Fatal(err)
	}

	h := &CommitCleanupHandler{ProjectRoot: dir}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m 'feat: add feature'"},"tool_result":"[main abc1234] feat: add feature"}`

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ファイルが削除されているか確認
	if _, err := os.Stat(reviewState); err == nil {
		t.Errorf("expected review-approved.json to be deleted")
	}
	if _, err := os.Stat(reviewResult); err == nil {
		t.Errorf("expected review-result.json to be deleted")
	}
}

func TestCommitCleanupHandler_GitCommitError_KeepsFiles(t *testing.T) {
	dir := t.TempDir()

	// レビューファイルを作成
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	reviewState := filepath.Join(stateDir, "review-approved.json")
	if err := os.WriteFile(reviewState, []byte(`{"approved":true}`), 0600); err != nil {
		t.Fatal(err)
	}

	h := &CommitCleanupHandler{ProjectRoot: dir}
	// エラーを含む tool_result
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m test"},"tool_result":"error: nothing to commit, working tree clean"}`

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// エラー時はファイルを保持
	if _, err := os.Stat(reviewState); err != nil {
		t.Errorf("expected review-approved.json to be kept on commit error")
	}
}

func TestCommitCleanupHandler_NoReviewFiles_NoError(t *testing.T) {
	dir := t.TempDir()

	h := &CommitCleanupHandler{ProjectRoot: dir}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m test"},"tool_result":"[main abc1234] test"}`

	var out bytes.Buffer
	// レビューファイルが存在しなくてもエラーにならないこと
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsGitCommitCommand(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"git commit -m test", true},
		{"git commit", true},
		{"git commit --amend", true},
		{"  git commit -m 'message'", true},
		{"git status", false},
		{"git checkout main", false},
		{"echo 'git commit'", false},
		{"notgit commit", false},
		{"git commitish", false},
	}

	for _, tt := range tests {
		got := isGitCommitCommand(tt.command)
		if got != tt.want {
			t.Errorf("isGitCommitCommand(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

func TestContainsErrorIndicator(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"plain success", "[main abc] feat: done", false},
		{"error prefix", "error: nothing to commit", true},
		{"fatal prefix", "fatal: not a git repository", true},
		{"nothing-to-commit clean output", "nothing to commit, working tree clean", true},
		{"failed write", "failed to write", true},
		{"uppercase FAILED", "FAILED tests", true},
		{"empty", "", false},
		// Subagent review regression: success output whose commit message contains
		// "nothing to commit" must NOT be classified as an error. Without the git
		// success prefix bypass, this would silently skip the approval clear and
		// allow the next commit to bypass re-review.
		{"success with 'nothing to commit' in message",
			"[main abc1234] fix nothing to commit edge case\n 1 file changed, 1 insertion(+)", false},
		{"success with 'error' in message",
			"[main def5678] handle error path explicitly", false},
		{"success with 'failed' in message",
			"[feature/x 0011223] retry when remote call failed", false},
		// Multi-line failure still detected (no success prefix at the top).
		{"multi-line failure", "fatal: bad object HEAD\ngit commit failed", true},
	}

	for _, tt := range tests {
		got := containsErrorIndicator(tt.result)
		if got != tt.want {
			t.Errorf("[%s] containsErrorIndicator(%q) = %v, want %v", tt.name, tt.result, got, tt.want)
		}
	}
}

func TestCommitCleanupHandler_StderrMessage(t *testing.T) {
	dir := t.TempDir()

	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "review-approved.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	h := &CommitCleanupHandler{ProjectRoot: dir}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m ok"},"tool_result":"[main 1234567] ok"}`

	var out bytes.Buffer
	err := h.Handle(strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ログメッセージが出力されること
	if !strings.Contains(out.String(), "レビュー承認状態をクリア") {
		t.Errorf("expected cleanup log message, got %q", out.String())
	}
}

// ============================================================
// Phase 94.1.3 (#219 fix) bookkeeping-only commit recognition
// ============================================================

// fakeGitRunner allows deterministic responses for git subcommands.
type fakeGitRunner struct {
	merge          bool
	changedFiles   []string
	gitUnavailable bool
}

func (g *fakeGitRunner) run(args ...string) (string, error) {
	if g.gitUnavailable {
		return "", fmt.Errorf("git unavailable")
	}
	// args例: ["rev-parse", "--verify", "HEAD^2"] or ["show", "--name-only", "--format=", "HEAD"]
	if len(args) >= 2 && args[0] == "rev-parse" && args[len(args)-1] == "HEAD^2" {
		if g.merge {
			return "deadbeef\n", nil
		}
		return "", fmt.Errorf("HEAD^2 not found")
	}
	if len(args) >= 2 && args[0] == "show" {
		return strings.Join(g.changedFiles, "\n") + "\n", nil
	}
	return "", fmt.Errorf("unexpected git args: %v", args)
}

func setupReviewFiles(t *testing.T, dir string) (string, string) {
	t.Helper()
	stateDir := filepath.Join(dir, ".claude", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	reviewState := filepath.Join(stateDir, "review-approved.json")
	reviewResult := filepath.Join(stateDir, "review-result.json")
	if err := os.WriteFile(reviewState, []byte(`{"approved":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reviewResult, []byte(`{"verdict":"APPROVE"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return reviewState, reviewResult
}

func TestCommitCleanup_BookkeepingOnly_KeepsApproval(t *testing.T) {
	dir := t.TempDir()
	reviewState, reviewResult := setupReviewFiles(t, dir)

	runner := &fakeGitRunner{
		changedFiles: []string{"VERSION", "CHANGELOG.md", ".claude-plugin/plugin.json", "harness.toml"},
	}
	h := &CommitCleanupHandler{ProjectRoot: dir, GitRunner: runner.run}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m 'release: v1.2.3'"},"tool_result":"[main abc1234] release: v1.2.3"}`

	var out bytes.Buffer
	if err := h.Handle(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(reviewState); err != nil {
		t.Errorf("expected review-approved.json to be kept (bookkeeping commit), got error: %v", err)
	}
	if _, err := os.Stat(reviewResult); err != nil {
		t.Errorf("expected review-result.json to be kept (bookkeeping commit), got error: %v", err)
	}
	if !strings.Contains(out.String(), "bookkeeping-only") {
		t.Errorf("expected bookkeeping-only message, got %q", out.String())
	}

	// audit log
	auditPath := filepath.Join(dir, ".claude", "state", "commit-cleanup-audit.jsonl")
	auditBody, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("expected audit log to be appended, got error: %v", err)
	}
	if !strings.Contains(string(auditBody), `"approval_kept":true`) {
		t.Errorf("expected approval_kept=true in audit log, got %q", string(auditBody))
	}
	if !strings.Contains(string(auditBody), `"reason":"bookkeeping-only"`) {
		t.Errorf("expected reason=bookkeeping-only in audit log, got %q", string(auditBody))
	}
}

func TestCommitCleanup_CodePlusBookkeepingMix_ClearsApproval(t *testing.T) {
	dir := t.TempDir()
	reviewState, reviewResult := setupReviewFiles(t, dir)

	runner := &fakeGitRunner{
		changedFiles: []string{"VERSION", "src/main.go", "CHANGELOG.md"},
	}
	h := &CommitCleanupHandler{ProjectRoot: dir, GitRunner: runner.run}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m mixed"},"tool_result":"[main abc1234] mixed"}`

	var out bytes.Buffer
	if err := h.Handle(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(reviewState); err == nil {
		t.Errorf("expected review-approved.json to be cleared (mixed commit)")
	}
	if _, err := os.Stat(reviewResult); err == nil {
		t.Errorf("expected review-result.json to be cleared (mixed commit)")
	}

	auditPath := filepath.Join(dir, ".claude", "state", "commit-cleanup-audit.jsonl")
	auditBody, _ := os.ReadFile(auditPath)
	if !strings.Contains(string(auditBody), `"reason":"code-change"`) {
		t.Errorf("expected reason=code-change in audit log, got %q", string(auditBody))
	}
}

func TestCommitCleanup_CodeOnly_ClearsApproval(t *testing.T) {
	dir := t.TempDir()
	reviewState, reviewResult := setupReviewFiles(t, dir)

	runner := &fakeGitRunner{
		changedFiles: []string{"src/main.go", "src/util.go"},
	}
	h := &CommitCleanupHandler{ProjectRoot: dir, GitRunner: runner.run}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m feat"},"tool_result":"[main abc1234] feat"}`

	var out bytes.Buffer
	if err := h.Handle(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(reviewState); err == nil {
		t.Errorf("expected review-approved.json to be cleared (code commit)")
	}
	if _, err := os.Stat(reviewResult); err == nil {
		t.Errorf("expected review-result.json to be cleared (code commit)")
	}
}

func TestCommitCleanup_MergeCommit_KeepsApproval(t *testing.T) {
	dir := t.TempDir()
	reviewState, reviewResult := setupReviewFiles(t, dir)

	runner := &fakeGitRunner{merge: true}
	h := &CommitCleanupHandler{ProjectRoot: dir, GitRunner: runner.run}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m 'Merge branch foo'"},"tool_result":"[main abc1234] Merge"}`

	var out bytes.Buffer
	if err := h.Handle(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(reviewState); err != nil {
		t.Errorf("expected review-approved.json to be kept (merge commit), got error: %v", err)
	}
	if _, err := os.Stat(reviewResult); err != nil {
		t.Errorf("expected review-result.json to be kept (merge commit), got error: %v", err)
	}
	if !strings.Contains(out.String(), "merge") {
		t.Errorf("expected merge message, got %q", out.String())
	}
}

func TestCommitCleanup_GitUnavailable_FallsBackToClear(t *testing.T) {
	// git 取得失敗時は fail-closed (= 従来動作 = 削除) を維持
	dir := t.TempDir()
	reviewState, _ := setupReviewFiles(t, dir)

	runner := &fakeGitRunner{gitUnavailable: true}
	h := &CommitCleanupHandler{ProjectRoot: dir, GitRunner: runner.run}
	input := `{"tool_name":"Bash","tool_input":{"command":"git commit -m x"},"tool_result":"[main abc1234] x"}`

	var out bytes.Buffer
	if err := h.Handle(strings.NewReader(input), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(reviewState); err == nil {
		t.Errorf("expected approval to be cleared on git failure (fail-closed)")
	}

	auditPath := filepath.Join(dir, ".claude", "state", "commit-cleanup-audit.jsonl")
	auditBody, _ := os.ReadFile(auditPath)
	if !strings.Contains(string(auditBody), `"reason":"git-unavailable"`) {
		t.Errorf("expected reason=git-unavailable in audit log, got %q", string(auditBody))
	}
}
