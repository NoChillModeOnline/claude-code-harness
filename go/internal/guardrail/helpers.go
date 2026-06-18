package guardrail

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Chachamaru127/claude-code-harness/go/pkg/hookproto"
)

// ---------------------------------------------------------------------------
// Protected path taxonomy
// ---------------------------------------------------------------------------

type protectedPathLevel int

const (
	protectedPathNone protectedPathLevel = iota
	protectedPathWarn
	protectedPathAsk
	protectedPathDeny
)

type protectedPathMatch struct {
	Level  protectedPathLevel
	Reason string
	Path   string
}

type protectedPathRule struct {
	level   protectedPathLevel
	reason  string
	pattern *regexp.Regexp
}

// Claude Code 2.1.121/2.1.126 protected path taxonomy:
//   - deny: .git/, secrets, shell rc/profile files, destructive hook entrypoints.
//   - ask: .claude/skills/, .claude/agents/, .claude/commands/, .vscode/.
//   - warn: .claude/rules/, .claude/memory/, setup metadata.
//
// This intentionally does not deny every .claude/ path. Runtime state and other
// project-local Claude data remain governed by the normal write rules.
var protectedPathRules = []protectedPathRule{
	// deny: repository internals, secrets, hook entrypoints, and shell startup files
	{protectedPathDeny, "Git internal metadata", regexp.MustCompile(`(?:^|/)\.git(?:/|$)`)},
	{protectedPathDeny, "secret or credential file", regexp.MustCompile(`(?:^|/)\.env(?:$|\.)`)},
	{protectedPathDeny, "secret or credential file", regexp.MustCompile(`(?:^|/)\.envrc$`)},
	{protectedPathDeny, "secret or credential file", regexp.MustCompile(`(?:^|/)secrets?(?:/|$)`)},
	{protectedPathDeny, "secret or credential file", regexp.MustCompile(`(?:^|/)(?:id_rsa|id_ed25519|id_ecdsa|id_dsa)$`)},
	{protectedPathDeny, "secret or credential file", regexp.MustCompile(`\.(?:pem|key|p12|pfx)$`)},
	{protectedPathDeny, "SSH trust file", regexp.MustCompile(`(?:^|/)(?:authorized_keys|known_hosts)$`)},
	{protectedPathDeny, "destructive hook entrypoint", regexp.MustCompile(`(?:^|/)\.husky(?:/|$)`)},
	{protectedPathDeny, "destructive hook entrypoint", regexp.MustCompile(`(?:^|/)\.claude/hooks(?:/|$)`)},
	{protectedPathDeny, "shell rc/profile file", regexp.MustCompile(`(?:^|/)\.(?:bashrc|bash_profile|bash_login|profile|zshrc|zprofile|zshenv|zlogin|zlogout|kshrc|cshrc|tcshrc)$`)},
	{protectedPathDeny, "shell rc/profile file", regexp.MustCompile(`(?:^|/)\.config/fish/config\.fish$`)},
	{protectedPathDeny, "shell rc/profile file", regexp.MustCompile(`(?:^|/)(?:Microsoft\.)?(?:PowerShell_)?profile\.ps1$`)},

	// ask: agent capability surfaces and editor automation settings
	{protectedPathAsk, "Claude capability path", regexp.MustCompile(`(?:^|/)\.claude/(?:skills|agents|commands)(?:/|$)`)},
	{protectedPathAsk, "editor automation settings", regexp.MustCompile(`(?:^|/)\.vscode(?:/|$)`)},

	// warn: policy/memory/setup metadata that is important but not hard-denied
	{protectedPathWarn, "Claude rule or memory path", regexp.MustCompile(`(?:^|/)\.claude/(?:rules|memory)(?:/|$)`)},
	{protectedPathWarn, "setup metadata", regexp.MustCompile(`(?:^|/)\.claude/(?:settings(?:\.local)?\.json|config(?:/|$)|Plans\.md$)`)},
	{protectedPathWarn, "setup metadata", regexp.MustCompile(`(?:^|/)\.claude-plugin/(?:plugin|settings(?:\.local)?)\.json$`)},
	{protectedPathWarn, "setup metadata", regexp.MustCompile(`(?:^|/)(?:CLAUDE|AGENTS)\.md$`)},
	{protectedPathWarn, "setup metadata", regexp.MustCompile(`(?:^|/)\.mcp\.json$`)},
	{protectedPathWarn, "setup metadata", regexp.MustCompile(`(?:^|/)harness\.toml$`)},
}

func normalizePathForGuardrail(filePath string) string {
	cleaned := filepath.Clean(filePath)
	if cleaned == "." {
		return filePath
	}
	return filepath.ToSlash(cleaned)
}

func classifyProtectedPathPattern(filePath string) protectedPathMatch {
	normalized := normalizePathForGuardrail(filePath)
	best := protectedPathMatch{Level: protectedPathNone, Path: normalized}
	for _, rule := range protectedPathRules {
		if rule.pattern.MatchString(normalized) && rule.level > best.Level {
			best = protectedPathMatch{
				Level:  rule.level,
				Reason: rule.reason,
				Path:   normalized,
			}
		}
	}
	return best
}

func strongerProtectedPathMatch(a, b protectedPathMatch) protectedPathMatch {
	if b.Level > a.Level {
		return b
	}
	return a
}

func classifyProtectedPath(filePath string) protectedPathMatch {
	match := classifyProtectedPathPattern(filePath)

	// Resolve symlinks and check the real path (CC 2.1.89: symlink target resolution)
	realPath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		// Fail-safe: symlink loop, broken link, or other error → deny.
		// Exception: if the path simply doesn't exist, it's classified from
		// the path text only, so new non-sensitive files are not over-blocked.
		if _, statErr := os.Lstat(filePath); os.IsNotExist(statErr) {
			return match
		}
		return protectedPathMatch{
			Level:  protectedPathDeny,
			Reason: "unresolvable protected path",
			Path:   normalizePathForGuardrail(filePath),
		}
	}

	return strongerProtectedPathMatch(match, classifyProtectedPathPattern(realPath))
}

// isProtectedPath checks whether filePath matches any protected taxonomy level.
// If EvalSymlinks returns an error (symlink loop, broken link, etc.),
// the function returns true via the fail-safe deny classification.
func isProtectedPath(filePath string) bool {
	return classifyProtectedPath(filePath).Level != protectedPathNone
}

// ---------------------------------------------------------------------------
// Bash write target extraction
// ---------------------------------------------------------------------------

var (
	bashRedirectionTargetPattern = regexp.MustCompile(`(?:^|[\s;&|])(?:\d*&>>?|\d*>>?|&>>?|>\|)\s*['"]?([^'"` + "`" + `\s;&|]+)['"]?`)
	bashTeeCommandPattern        = regexp.MustCompile(`(?:^|[|;&]\s*)tee\b([^;&|]*)`)
)

func stripShellTokenQuotes(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "'\"")
	return token
}

func extractBashWriteTargets(command string) []string {
	var targets []string
	for _, m := range bashRedirectionTargetPattern.FindAllStringSubmatch(command, -1) {
		if len(m) >= 2 {
			targets = append(targets, stripShellTokenQuotes(m[1]))
		}
	}

	for _, m := range bashTeeCommandPattern.FindAllStringSubmatch(command, -1) {
		if len(m) < 2 {
			continue
		}
		for _, token := range strings.Fields(m[1]) {
			token = stripShellTokenQuotes(token)
			if token == "" || token == "--" {
				continue
			}
			if strings.HasPrefix(token, "-") {
				continue
			}
			if strings.ContainsAny(token, "<>|`$") {
				continue
			}
			targets = append(targets, token)
		}
	}

	return targets
}

func classifyBashProtectedWrite(command string) protectedPathMatch {
	best := protectedPathMatch{Level: protectedPathNone}
	for _, target := range extractBashWriteTargets(command) {
		best = strongerProtectedPathMatch(best, classifyProtectedPathPattern(target))
	}
	return best
}

func bashProtectedWriteHookResult(ctx hookproto.RuleContext, command string) *hookproto.HookResult {
	var askResult *hookproto.HookResult
	var warnResult *hookproto.HookResult

	for _, target := range extractBashWriteTargets(command) {
		match := classifyProtectedPathPattern(target)
		switch match.Level {
		case protectedPathDeny:
			if result := r03ProtectedPathAskResult(ctx, match.Path); result != nil {
				if askResult == nil {
					askResult = result
				}
				continue
			}
			return protectedPathHookResult(match, match.Path, "shell write to a protected path")
		case protectedPathAsk:
			if askResult == nil {
				askResult = protectedPathHookResult(match, match.Path, "shell write to a protected path")
			}
		case protectedPathWarn:
			if warnResult == nil {
				warnResult = protectedPathHookResult(match, match.Path, "shell write to a protected path")
			}
		}
	}

	if askResult != nil {
		return askResult
	}
	if warnResult != nil {
		return warnResult
	}
	return nil
}

// ---------------------------------------------------------------------------
// Project root check
// ---------------------------------------------------------------------------

func isUnderProjectRoot(filePath, projectRoot string) bool {
	// 相対パスは projectRoot を基準に解決
	resolved := filePath
	if !filepath.IsAbs(filePath) {
		resolved = filepath.Join(projectRoot, filePath)
	}
	cleaned := filepath.Clean(resolved)
	root := filepath.Clean(projectRoot)
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(cleaned, root) || cleaned == root
}

// ---------------------------------------------------------------------------
// Whitespace normalization (CC 2.1.98: wildcard pattern defense-in-depth)
// ---------------------------------------------------------------------------

// wsNormPattern matches one or more whitespace characters (spaces, tabs, etc.)
var wsNormPattern = regexp.MustCompile(`\s+`)

// normalizeCommand collapses consecutive whitespace characters (spaces, tabs,
// and other whitespace) into a single space and trims leading/trailing whitespace.
// This is used as a defense-in-depth measure before wildcard pattern matching,
// so that "git  push  --force" and "git\tpush\t--force" are treated identically
// to "git push --force".
func normalizeCommand(cmd string) string {
	return strings.TrimSpace(wsNormPattern.ReplaceAllString(cmd, " "))
}

// ---------------------------------------------------------------------------
// Dangerous deletion detection
// ---------------------------------------------------------------------------

var (
	rmRecursivePattern            = regexp.MustCompile(`\brm\s+--recursive\b`)
	findDeletePattern             = regexp.MustCompile(`\bfind\s+.*(?:\s-delete(?:\s|$)|\s-exec\s+rm\s+.*(?:\\;|;|\+|$))`)
	macOSDangerousRmTargetPattern = regexp.MustCompile(
		`\brm\s+.*(?:/private/(?:etc|var|tmp|home)(?:/|\s|$)|/System(?:/|\s|$)|/Library/(?:LaunchDaemons|LaunchAgents|Preferences|Keychains)(?:/|\s|$)|~/Library(?:/|\s|$)|/Users/[^/\s]+/Library(?:/|\s|$))`,
	)
)

// rmRfManual detects rm with both -r and -f flags (in any order/combination).
// Go regexp doesn't support lookahead (?=...) so we check manually.
var rmWithFlags = regexp.MustCompile(`\brm\s+(.+)`)

func hasDangerousRmRf(command string) bool {
	// Normalize whitespace before matching (CC 2.1.98: defense-in-depth)
	command = normalizeCommand(command)
	if hasDangerousFindDelete(command) || hasDangerousMacOSRemovalPath(command) {
		return true
	}
	if rmRecursivePattern.MatchString(command) {
		return true
	}
	// Check for -rf, -fr, -r -f, etc. in rm arguments
	m := rmWithFlags.FindStringSubmatch(command)
	if m == nil {
		return false
	}
	args := m[1]
	// Scan tokens for flag groups containing both r and f
	hasR := false
	hasF := false
	for _, token := range strings.Fields(args) {
		if !strings.HasPrefix(token, "-") || strings.HasPrefix(token, "--") {
			continue // skip non-short-flags and long flags
		}
		flags := token[1:] // strip leading -
		for _, c := range flags {
			if c == 'r' {
				hasR = true
			}
			if c == 'f' {
				hasF = true
			}
		}
	}
	return hasR && hasF
}

func hasDangerousFindDelete(command string) bool {
	return findDeletePattern.MatchString(command)
}

func hasDangerousMacOSRemovalPath(command string) bool {
	return macOSDangerousRmTargetPattern.MatchString(command)
}

// ---------------------------------------------------------------------------
// git push --force detection
// ---------------------------------------------------------------------------

var (
	forcePushPattern = regexp.MustCompile(`\bgit\s+push\b.*--force(?:-with-lease)?\b`)
	forcePushShort   = regexp.MustCompile(`\bgit\s+push\b.*-f\b`)
)

func hasForcePush(command string) bool {
	// Normalize whitespace before matching (CC 2.1.98: defense-in-depth)
	command = normalizeCommand(command)
	return forcePushPattern.MatchString(command) || forcePushShort.MatchString(command)
}

// ---------------------------------------------------------------------------
// sudo detection
// ---------------------------------------------------------------------------

// sudoPattern matches "sudo" preceded by start-of-string, whitespace,
// or shell metacharacters that introduce a subshell context: (, |, &, `, ;.
// This prevents bypass via "echo $(sudo ...)" or "echo `sudo ...`".
// CC 2.1.110: extended to cover subshell and backtick contexts.
var sudoPattern = regexp.MustCompile(`(?:^|[\s(|&` + "`" + `;])sudo\s`)

func hasSudo(command string) bool {
	command = normalizeCommand(command)
	return sudoPattern.MatchString(command)
}

// ---------------------------------------------------------------------------
// --no-verify / --no-gpg-sign detection
// ---------------------------------------------------------------------------

// shellTokenBoundary matches the characters that terminate a flag token on a
// shell command line. Besides whitespace, bash treats the metacharacters
// `;`, `&`, `|`, `(`, `)`, `<` and `>` as token separators, so a flag such as
// `--no-verify` is still effective when written as `--no-verify&&echo` or
// `--no-verify;cmd`. Anchoring on this class (instead of `\s` alone) prevents
// the detection from being bypassed by appending a metacharacter without a
// surrounding space.
const shellTokenBoundary = `[\s;&|()<>]`

var (
	noVerifyPattern  = regexp.MustCompile(`(?:^|` + shellTokenBoundary + `)--no-verify(?:` + shellTokenBoundary + `|$)`)
	noGpgSignPattern = regexp.MustCompile(`(?:^|` + shellTokenBoundary + `)--no-gpg-sign(?:` + shellTokenBoundary + `|$)`)
)

func hasDangerousGitBypassFlag(command string) bool {
	command = normalizeCommand(command)
	return noVerifyPattern.MatchString(command) || noGpgSignPattern.MatchString(command)
}

// ---------------------------------------------------------------------------
// Protected branch reset --hard detection
// ---------------------------------------------------------------------------

var protectedBranchRefPattern = regexp.MustCompile(
	`^(?:origin/|upstream/)?(?:refs/heads/)?(?:main|master)(?:[~^]\d+)?$`,
)

func normalizeGitToken(token string) string {
	return strings.Trim(token, "'\"")
}

func hasProtectedBranchResetHard(command string) bool {
	command = normalizeCommand(command)
	tokens := strings.Fields(command)
	resetIndex := -1
	hasHard := false
	for i, t := range tokens {
		normalized := normalizeGitToken(t)
		if normalized == "reset" {
			resetIndex = i
		}
		if normalized == "--hard" {
			hasHard = true
		}
	}
	if resetIndex == -1 || !hasHard {
		return false
	}
	for _, t := range tokens[resetIndex+1:] {
		normalized := normalizeGitToken(t)
		if strings.HasPrefix(normalized, "-") {
			continue
		}
		if protectedBranchRefPattern.MatchString(normalized) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Direct push to protected branch detection
// ---------------------------------------------------------------------------

var gitPushPattern = regexp.MustCompile(`\bgit\s+push\b`)

func hasDirectPushToProtectedBranch(command string) bool {
	command = normalizeCommand(command)
	if !gitPushPattern.MatchString(command) {
		return false
	}
	tokens := strings.Fields(command)
	pushIndex := -1
	for i, t := range tokens {
		if t == "push" {
			pushIndex = i
			break
		}
	}
	if pushIndex == -1 {
		return false
	}

	// Collect non-flag args after "push"
	var args []string
	for _, t := range tokens[pushIndex+1:] {
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
		}
	}
	if len(args) == 0 {
		return false
	}

	for _, arg := range args {
		normalized := normalizeGitToken(arg)
		if protectedBranchRefPattern.MatchString(normalized) {
			return true
		}
		// Check refspec (src:dst)
		parts := strings.SplitN(arg, ":", 2)
		if len(parts) == 2 {
			if protectedBranchRefPattern.MatchString(normalizeGitToken(parts[1])) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Protected review path detection (warn-only)
// ---------------------------------------------------------------------------

var protectedReviewPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|/)package\.json$`),
	regexp.MustCompile(`(?:^|/)Dockerfile$`),
	regexp.MustCompile(`(?:^|/)docker-compose\.yml$`),
	regexp.MustCompile(`(?:^|/)\.github/workflows/[^/]+$`),
	regexp.MustCompile(`(?:^|/)schema\.prisma$`),
	regexp.MustCompile(`(?:^|/)wrangler\.toml$`),
	regexp.MustCompile(`(?:^|/)index\.html$`),
}

func isProtectedReviewPath(filePath string) bool {
	for _, p := range protectedReviewPathPatterns {
		if p.MatchString(filePath) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Secret file staging detection (R15)
// ---------------------------------------------------------------------------

// gitGlobalValueOpts are git global options that consume the following token as
// their value when written as a separate token (e.g. `-C /repo`, not
// `--git-dir=/repo`). They may appear between `git` and the subcommand.
var gitGlobalValueOpts = map[string]bool{
	"-C":             true,
	"-c":             true,
	"--git-dir":      true,
	"--work-tree":    true,
	"--namespace":    true,
	"--exec-path":    true,
	"--super-prefix": true,
}

// r15SecretStagingPatterns is a staging-focused superset of the R09 read-warn
// patterns. It deliberately targets credential-bearing files (dotenv variants,
// private keys, cloud/SSH credential stores) and NOT tracked config such as
// settings.json or .github/workflows, which are legitimately committed.
var r15SecretStagingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|/)\.env(?:\.[^/]+)?$`), // .env, .env.local, .env.production
	regexp.MustCompile(`(?:^|/)id_rsa(?:\.[^/]+)?$`),
	regexp.MustCompile(`(?:^|/)id_ed25519(?:\.[^/]+)?$`),
	regexp.MustCompile(`\.pem$`),
	regexp.MustCompile(`\.key$`),
	regexp.MustCompile(`\.p12$`),
	regexp.MustCompile(`\.pfx$`),
	regexp.MustCompile(`(?:^|/)\.npmrc$`),
	regexp.MustCompile(`(?:^|/)\.pypirc$`),
	regexp.MustCompile(`(?:^|/)credentials$`),
	regexp.MustCompile(`(?:^|/)secrets?/`),
	regexp.MustCompile(`(?:^|/)\.aws/`),
	regexp.MustCompile(`(?:^|/)\.ssh/`),
}

// shellToken is one lexed token of a shell command. op marks a control operator
// (`&&`, `||`, `;`, `|`, `&`, `$(`, `)`, backtick) that ends a sub-command;
// quoted marks a token whose content came (wholly or partly) from inside single
// or double quotes, so a quoted "--" or pathspec is not mistaken for a bare one.
type shellToken struct {
	value  string
	quoted bool
	op     bool
}

// shellLex tokenizes a command while respecting single/double quotes. Control
// operators become op tokens; whitespace separates tokens; characters inside
// quotes (including separators like ';' or '&&') are kept literal. This makes
// `git commit -m "x; y" -- .env` stay a single sub-command whose message is one
// quoted token, instead of being split at the in-message ';'.
func shellLex(command string) []shellToken {
	var tokens []shellToken
	var cur strings.Builder
	curQuoted := false
	curHas := false
	var quote byte // 0, '\'' or '"'

	emit := func() {
		if curHas {
			tokens = append(tokens, shellToken{value: cur.String(), quoted: curQuoted})
		}
		cur.Reset()
		curQuoted = false
		curHas = false
	}
	emitOp := func() {
		emit()
		tokens = append(tokens, shellToken{op: true})
	}

	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
				curHas = true
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			curQuoted = true
			curHas = true // an empty "" is still a (quoted) token
		case ' ', '\t', '\n', '\r':
			emit()
		case ';', ')', '`':
			emitOp()
		case '|', '&':
			if i+1 < len(command) && command[i+1] == c {
				i++ // collapse "||" / "&&" into one operator
			}
			emitOp()
		case '$':
			if i+1 < len(command) && command[i+1] == '(' {
				i++
				emitOp()
			} else {
				cur.WriteByte(c)
				curHas = true
			}
		default:
			cur.WriteByte(c)
			curHas = true
		}
	}
	emit()
	return tokens
}

// indexOfGitSubcommand returns the index of the git subcommand token (e.g.
// "add") for the first `git` invocation in the segment, skipping any global
// options (and their values) that appear between `git` and the subcommand. This
// makes `git -C /repo add .env` resolve to the "add" token. Returns -1 when the
// segment has no git invocation with a subcommand.
func indexOfGitSubcommand(tokens []shellToken) int {
	for i := 0; i < len(tokens); i++ {
		if tokens[i].quoted || tokens[i].value != "git" {
			continue
		}
		for j := i + 1; j < len(tokens); j++ {
			t := tokens[j]
			if t.quoted || !strings.HasPrefix(t.value, "-") {
				return j // first non-option token is the subcommand
			}
			// Skip a separate value token for value-taking global options
			// (e.g. "-C /repo"). The "--opt=value" form is a single token.
			if !strings.Contains(t.value, "=") && gitGlobalValueOpts[t.value] {
				j++
			}
		}
		return -1 // git present but no subcommand in this segment
	}
	return -1
}

// gitAddPathspecs returns the pathspec arguments of a `git add`/`git stage`
// invocation. Quoted tokens are always pathspecs; bare flags and a bare `--`
// separator are skipped. `git add` takes no message option.
func gitAddPathspecs(args []shellToken) []string {
	var out []string
	for _, t := range args {
		if !t.quoted && (t.value == "--" || strings.HasPrefix(t.value, "-")) {
			continue
		}
		if t.value == "" {
			continue
		}
		out = append(out, t.value)
	}
	return out
}

// gitCommitPathspecs returns the pathspec arguments of a `git commit`
// invocation: only tokens after a bare (unquoted) `--` separator. This ignores
// a `-m "fix .env"` message entirely — the message is a quoted token, and a
// `--` appearing inside that quoted message is not treated as the separator.
func gitCommitPathspecs(args []shellToken) []string {
	var out []string
	sawSep := false
	for _, t := range args {
		if !sawSep {
			if !t.quoted && t.value == "--" {
				sawSep = true
			}
			continue
		}
		if t.value != "" {
			out = append(out, t.value) // after --, every token is a pathspec
		}
	}
	return out
}

// extractGitStagedPaths returns every path that a command would add to the git
// index via `git add`/`git stage`/`git commit <pathspec>`. The command is lexed
// quote-aware and split into sub-commands at control operators, so chained and
// subshell-wrapped invocations are all covered.
//
// Known accepted scope gaps (consistent with the bulk `git add .` case): paths
// supplied to git through stdin (`echo .env | xargs git add`) are invisible to
// command-string analysis, and `git commit -a` re-stages already-tracked files
// without naming them. Both rely on .gitignore plus the R02/R03 write guards.
func extractGitStagedPaths(command string) []string {
	var paths []string
	var segment []shellToken

	flush := func() {
		if len(segment) == 0 {
			return
		}
		if idx := indexOfGitSubcommand(segment); idx >= 0 {
			switch segment[idx].value {
			case "add", "stage":
				paths = append(paths, gitAddPathspecs(segment[idx+1:])...)
			case "commit":
				paths = append(paths, gitCommitPathspecs(segment[idx+1:])...)
			}
		}
		segment = nil
	}

	for _, tok := range shellLex(command) {
		if tok.op {
			flush()
			continue
		}
		segment = append(segment, tok)
	}
	flush()
	return paths
}

// secretFileStaging reports the first staged path that looks like a secret
// file, if any. It is the detection backing guard rule R15.
func secretFileStaging(command string) (string, bool) {
	for _, path := range extractGitStagedPaths(command) {
		for _, p := range r15SecretStagingPatterns {
			if p.MatchString(path) {
				return path, true
			}
		}
	}
	return "", false
}
