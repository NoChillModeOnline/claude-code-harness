package hookhandler

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// gitCommitSuccessRe は `git commit` 成功時の典型的な出力 prefix
// `[<branch> <hash>]` (例: `[main abc1234]`) を検出する正規表現。
// commit message に "nothing to commit" 等の error indicator phrase が含まれていても、
// この prefix が先頭にあれば commit は成功している。
var gitCommitSuccessRe = regexp.MustCompile(`^\[[^\]]+\s+[0-9a-fA-F]+\]`)

// bookkeepingFiles はリリース時の bookkeeping commit が変更してよいファイル集合 (#219 fix)。
// この集合のみを変更する commit はレビュー対象外として承認状態を保持する。
var bookkeepingFiles = map[string]struct{}{
	"VERSION":                    {},
	".claude-plugin/plugin.json": {},
	"harness.toml":               {},
	"CHANGELOG.md":               {},
}

// gitRunner は git コマンド実行を抽象化するインターフェース (テストモック用)。
type gitRunner func(args ...string) (string, error)

// defaultGitRunner は実環境で git を実行する。
func defaultGitRunner(projectRoot string) gitRunner {
	return func(args ...string) (string, error) {
		all := append([]string{"-C", projectRoot}, args...)
		cmd := exec.Command("git", all...)
		out, err := cmd.Output()
		return string(out), err
	}
}

// CommitCleanupHandler は PostToolUse フックハンドラ（git commit 後のクリーンアップ）。
// git commit コマンドが成功した後に、レビュー承認状態ファイルを削除する。
//
// shell 版: scripts/posttooluse-commit-cleanup.sh
type CommitCleanupHandler struct {
	// ProjectRoot はプロジェクトルートのパス。空の場合は cwd を使用する。
	ProjectRoot string
	// GitRunner は git コマンド実行関数 (テスト用)。空の場合は defaultGitRunner を使用する。
	GitRunner gitRunner
}

// commitCleanupInput は PostToolUse フックの stdin JSON。
type commitCleanupInput struct {
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolInput  map[string]interface{} `json:"tool_input,omitempty"`
	ToolResult interface{}            `json:"tool_result,omitempty"`
}

// Handle は stdin から PostToolUse ペイロードを読み取り、
// git commit コマンドが成功していた場合にレビュー承認状態ファイルを削除する。
// このハンドラは標準出力にはログメッセージのみ書き出す（JSON 不要）。
func (h *CommitCleanupHandler) Handle(r io.Reader, w io.Writer) error {
	data, _ := io.ReadAll(r)

	if len(data) == 0 {
		return nil
	}

	var inp commitCleanupInput
	if err := json.Unmarshal(data, &inp); err != nil {
		return nil
	}

	// Bash ツール以外はスキップ
	if inp.ToolName != "Bash" {
		return nil
	}

	// コマンドを取得
	command := ""
	if v, ok := inp.ToolInput["command"]; ok {
		if s, ok := v.(string); ok {
			command = s
		}
	}
	if command == "" {
		return nil
	}

	// git commit コマンドかどうかを確認（大文字小文字を区別しない）
	if !isGitCommitCommand(command) {
		return nil
	}

	// ツール結果を文字列に変換
	toolResult := ""
	switch v := inp.ToolResult.(type) {
	case string:
		toolResult = v
	case map[string]interface{}:
		if b, err := json.Marshal(v); err == nil {
			toolResult = string(b)
		}
	}

	// エラーが含まれている場合はスキップ
	if containsErrorIndicator(toolResult) {
		return nil
	}

	// レビュー承認状態ファイルを削除
	projectRoot := h.ProjectRoot
	if projectRoot == "" {
		projectRoot, _ = os.Getwd()
	}

	reviewStateFile := projectRoot + "/.claude/state/review-approved.json"
	reviewResultFile := projectRoot + "/.claude/state/review-result.json"

	stateFileExists := fileExists(reviewStateFile)
	resultFileExists := fileExists(reviewResultFile)

	if stateFileExists || resultFileExists {
		// #219 fix: bookkeeping-only commit (VERSION / plugin.json / harness.toml / CHANGELOG.md)
		// および merge commit は承認状態を保持する。harness-release の multi-commit フロー
		// (work commit + version bump commit) の bump 側がブロックされる問題を防ぐ。
		runner := h.GitRunner
		if runner == nil {
			runner = defaultGitRunner(projectRoot)
		}
		reason, kept := classifyHeadCommitForCleanup(runner)
		_ = appendCleanupAuditLog(projectRoot, reason, kept)

		if kept {
			_, _ = fmt.Fprintf(w, "[Commit Guard] %s commit を検出 — レビュー承認状態を保持しました (#219)。\n", reason)
			return nil
		}

		_ = os.Remove(reviewStateFile)
		_ = os.Remove(reviewResultFile)

		_, _ = fmt.Fprintf(w, "[Commit Guard] レビュー承認状態をクリアしました。次回のコミット前に再度独立レビューを実行してください。\n")
	}

	return nil
}

// classifyHeadCommitForCleanup は HEAD commit の種別を判定し、承認保持すべきかを返す。
// 戻り値: (reason, keepApproval)
//   - ("merge", true)            HEAD は merge commit
//   - ("bookkeeping-only", true) 変更が bookkeepingFiles 集合のみ
//   - ("code-change", false)     コード変更を含む通常 commit (= 従来動作: 承認削除)
//   - ("git-unavailable", false) git 取得失敗 (fail-closed: 承認削除 = 従来動作)
//   - ("empty", false)           空 commit (fail-closed)
func classifyHeadCommitForCleanup(runner gitRunner) (string, bool) {
	// merge commit 判定: HEAD^2 が存在 = merge
	if _, err := runner("rev-parse", "--verify", "HEAD^2"); err == nil {
		return "merge", true
	}

	// HEAD commit の変更ファイル一覧を取得
	out, err := runner("show", "--name-only", "--format=", "HEAD")
	if err != nil {
		return "git-unavailable", false
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "empty", false
	}

	for _, line := range strings.Split(out, "\n") {
		f := strings.TrimSpace(line)
		if f == "" {
			continue
		}
		if _, ok := bookkeepingFiles[f]; !ok {
			return "code-change", false
		}
	}
	return "bookkeeping-only", true
}

// appendCleanupAuditLog は cleanup の判定結果を .claude/state/commit-cleanup-audit.jsonl
// に append-only で記録する (#219 fix の判定根拠監査)。
func appendCleanupAuditLog(projectRoot, reason string, kept bool) error {
	dir := projectRoot + "/.claude/state"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := dir + "/commit-cleanup-audit.jsonl"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	entry := map[string]interface{}{
		"ts":            time.Now().UTC().Format(time.RFC3339),
		"reason":        reason,
		"approval_kept": kept,
		"source":        "posttooluse_commit_cleanup",
	}
	b, _ := json.Marshal(entry)
	_, err = fmt.Fprintf(f, "%s\n", string(b))
	return err
}

// isGitCommitCommand は command 文字列に git commit が含まれているかを判定する。
// bash の grep -Eiq と同等: '(^|[[:space:]])git[[:space:]]+commit([[:space:]]|$)'
func isGitCommitCommand(command string) bool {
	lower := strings.ToLower(command)
	// "git commit" のパターンを順次探索
	searchFrom := 0
	for searchFrom < len(lower) {
		idx := strings.Index(lower[searchFrom:], "git")
		if idx < 0 {
			break
		}
		absIdx := searchFrom + idx

		// "git" の前が行頭またはスペース
		if absIdx > 0 && !isWordBoundaryBefore(lower[absIdx-1]) {
			searchFrom = absIdx + 1
			continue
		}

		// "git" の後にスペースがある
		afterGit := absIdx + 3
		if afterGit >= len(lower) || !isWordBoundaryBefore(lower[afterGit]) {
			searchFrom = absIdx + 1
			continue
		}

		// スペースをスキップして "commit" を探す
		i := afterGit
		for i < len(lower) && isWordBoundaryBefore(lower[i]) {
			i++
		}
		if strings.HasPrefix(lower[i:], "commit") {
			after := i + 6
			if after >= len(lower) || isWordBoundaryBefore(lower[after]) {
				return true
			}
		}
		searchFrom = absIdx + 1
	}
	return false
}

// isWordBoundaryBefore は c がスペース（単語境界）かどうかを返す。
func isWordBoundaryBefore(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// containsErrorIndicator はツール結果にエラーの兆候が含まれているかを判定する。
//
// 修正 (subagent review finding): commit message に "nothing to commit" 等の
// error indicator phrase が含まれる成功 commit (例: `git commit -m "fix nothing
// to commit edge case"` → 出力 `[main abc1234] fix nothing to commit edge case`)
// を error と誤判定して承認状態クリアを skip すると、後続 commit が APPROVE 無し
// で通る bypass が成立する。そこで git の成功 prefix `[<branch> <hash>]` を
// 最初に検出し、成功と判定したら以降の error indicator チェックは skip する。
func containsErrorIndicator(result string) bool {
	trimmed := strings.TrimSpace(result)
	if gitCommitSuccessRe.MatchString(trimmed) {
		return false
	}
	lower := strings.ToLower(result)
	for _, indicator := range []string{"error", "fatal", "failed", "nothing to commit"} {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}
