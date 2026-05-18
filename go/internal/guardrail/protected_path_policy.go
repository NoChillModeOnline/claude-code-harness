package guardrail

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Chachamaru127/claude-code-harness/go/pkg/config"
	"github.com/Chachamaru127/claude-code-harness/go/pkg/hookproto"
)

var (
	r03EnvBreakGlassPattern = regexp.MustCompile(`(?:^|/)\.env(?:$|\.)`)
	r03BreakGlassDenyRules  = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|/)\.git(?:/|$)`),
		regexp.MustCompile(`(?:^|/)secrets?(?:/|$)`),
		regexp.MustCompile(`(?:^|/)(?:id_rsa|id_ed25519|id_ecdsa|id_dsa)$`),
		regexp.MustCompile(`\.(?:pem|key|p12|pfx)$`),
		regexp.MustCompile(`(?:^|/)(?:authorized_keys|known_hosts)$`),
		regexp.MustCompile(`(?:^|/)\.husky(?:/|$)`),
		regexp.MustCompile(`(?:^|/)\.claude/hooks(?:/|$)`),
		regexp.MustCompile(`(?:^|/)\.(?:bashrc|bash_profile|bash_login|profile|zshrc|zprofile|zshenv|zlogin|zlogout|kshrc|cshrc|tcshrc)$`),
		regexp.MustCompile(`(?:^|/)\.config/fish/config\.fish$`),
		regexp.MustCompile(`(?:^|/)(?:Microsoft\.)?(?:PowerShell_)?profile\.ps1$`),
	}
)

func resolveProtectedPathAskList(_ hookproto.HookInput, projectRoot string) []hookproto.ProtectedPathAskEntry {
	return readProtectedPathAskListFromHarnessTOML(filepath.Join(projectRoot, "harness.toml"))
}

func readProtectedPathAskListFromHarnessTOML(path string) []hookproto.ProtectedPathAskEntry {
	cfg, err := config.ParseFile(path)
	if err != nil || cfg == nil {
		return nil
	}

	entries := make([]hookproto.ProtectedPathAskEntry, 0, len(cfg.Safety.Guardrail.ProtectedPathAskList))
	for _, entry := range cfg.Safety.Guardrail.ProtectedPathAskList {
		entries = append(entries, hookproto.ProtectedPathAskEntry{
			Path:   entry.Path,
			Reason: entry.Reason,
			Source: path,
		})
	}
	return entries
}

func canonicalProtectedPathAskPath(path, projectRoot string) (string, bool) {
	if strings.TrimSpace(path) == "" {
		return "", false
	}

	normalized := normalizePathForGuardrail(path)
	if projectRoot != "" && filepath.IsAbs(path) {
		rel, err := filepath.Rel(projectRoot, path)
		if err != nil || rel == "." {
			return "", false
		}
		normalized = normalizePathForGuardrail(rel)
	}
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || strings.HasPrefix(normalized, "/") {
		return "", false
	}
	return normalized, true
}

func isR03EnvBreakGlassEligible(path string) bool {
	normalized := normalizePathForGuardrail(path)
	if !r03EnvBreakGlassPattern.MatchString(normalized) {
		return false
	}
	for _, rule := range r03BreakGlassDenyRules {
		if rule.MatchString(normalized) {
			return false
		}
	}
	return true
}

func r03ProtectedPathAskEntry(ctx hookproto.RuleContext, path string) (hookproto.ProtectedPathAskEntry, bool) {
	target, ok := canonicalProtectedPathAskPath(path, ctx.ProjectRoot)
	if !ok {
		return hookproto.ProtectedPathAskEntry{}, false
	}
	for _, entry := range ctx.ProtectedPathAskList {
		if strings.TrimSpace(entry.Reason) == "" {
			continue
		}
		configPath, ok := canonicalProtectedPathAskPath(entry.Path, ctx.ProjectRoot)
		if !ok {
			continue
		}
		if configPath == target {
			return entry, true
		}
	}
	return hookproto.ProtectedPathAskEntry{}, false
}

func r03ProtectedPathAskResult(ctx hookproto.RuleContext, path string) *hookproto.HookResult {
	if !isR03EnvBreakGlassEligible(path) {
		return nil
	}
	entry, ok := r03ProtectedPathAskEntry(ctx, path)
	if !ok {
		return nil
	}
	target, ok := canonicalProtectedPathAskPath(path, ctx.ProjectRoot)
	if !ok {
		return nil
	}
	return &hookproto.HookResult{
		Decision: hookproto.DecisionAsk,
		Reason: fmt.Sprintf(
			"R03 protected path break-glass requires confirmation: matched path %s (source: %s; configured reason: %s)",
			target,
			entry.Source,
			strings.TrimSpace(entry.Reason),
		),
	}
}
