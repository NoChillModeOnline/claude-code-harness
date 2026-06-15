package harnessmem

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultPlatforms        = "codex,claude"
	defaultNpmPackage       = "@chachamaru127/harness-mem"
	ContractVersion         = "claude-harness-companion.v1"
	defaultCommandTimeout   = 90 * time.Second
	defaultSetupHookTimeout = 120 * time.Second
)

var ErrNotInstalled = errors.New("harness-mem is not installed")

// DoctorReport is the subset of `harness-mem doctor --json` that
// claude-code-harness needs for orchestration. The harness never reads the
// harness-mem SQLite schema directly.
type DoctorReport struct {
	Status            string          `json:"status"`
	AllGreen          bool            `json:"all_green"`
	FailedCount       int             `json:"failed_count"`
	Checks            json.RawMessage `json:"checks"`
	FixCommand        string          `json:"fix_command"`
	BackendMode       string          `json:"backend_mode"`
	ContractVersion   string          `json:"contract_version"`
	HarnessMemVersion string          `json:"harness_mem_version"`
}

type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type Invocation struct {
	Name      string
	ArgPrefix []string
	Installed bool
}

// goosForInvocation / lookPathForInvocation are runtime.GOOS / exec.LookPath
// split out for test injection. Production code never reassigns them.
var (
	goosForInvocation     = runtime.GOOS
	lookPathForInvocation = exec.LookPath
)

// ResolveInvocation finds an installed harness-mem CLI. If allowNpx is true,
// it falls back to npx so setup/update can bootstrap a missing companion.
//
// Resolved script paths go through wrapScriptInvocation so that .js entry
// points (and node/bun shebang scripts on Windows, where exec does not honor
// #!) are launched via a JS runtime instead of being exec'd directly (#207).
func ResolveInvocation(allowNpx bool) (Invocation, bool) {
	if cli := os.Getenv("HARNESS_MEM_CLI"); cli != "" {
		return wrapScriptInvocation(Invocation{Name: cli, Installed: true}), true
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		candidate := filepath.Join(home, ".harness-mem", "runtime", "harness-mem", "scripts", "harness-mem")
		// The extensionless file is a bash script that Windows cannot exec;
		// the Windows-capable node entry is the sibling harness-mem.js
		// (both always ship together in the npm package). Prefer the .js
		// entry on Windows so the wrap below launches something node can run.
		if goosForInvocation == "windows" {
			jsCandidate := candidate + ".js"
			if info, err := os.Stat(jsCandidate); err == nil && !info.IsDir() {
				return wrapScriptInvocation(Invocation{Name: jsCandidate, Installed: true}), true
			}
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return wrapScriptInvocation(Invocation{Name: candidate, Installed: true}), true
		}
	}

	if os.Getenv("HARNESS_MEM_DISABLE_PATH_LOOKUP") != "1" {
		if path, err := lookPathForInvocation("harness-mem"); err == nil {
			return wrapScriptInvocation(Invocation{Name: path, Installed: true}), true
		}
	}

	if !allowNpx {
		return Invocation{}, false
	}

	npxBin := os.Getenv("HARNESS_MEM_NPX_BIN")
	if npxBin == "" {
		npxBin = "npx"
	}
	pkg := os.Getenv("HARNESS_MEM_NPM_PACKAGE")
	if pkg == "" {
		pkg = defaultNpmPackage
	}
	return Invocation{
		Name:      npxBin,
		ArgPrefix: []string{"-y", "--package", pkg, "harness-mem"},
		Installed: false,
	}, true
}

// wrapScriptInvocation converts scripts the OS cannot exec directly into a
// JS-runtime launch (#207):
//
//   - .js / .mjs / .cjs on every OS: Windows does not honor shebangs ("%1 is
//     not a valid Win32 application") and on Unix this removes the dependency
//     on the shebang line / executable bit. A bun shebang keeps bun preferred.
//   - Extensionless real files on Windows: wrapped only when their shebang
//     names node or bun. Anything else (e.g. the bash CLI that ships next to
//     harness-mem.js) is left alone for the normal exec error to surface.
//
// When no JS runtime is installed the original Invocation is returned and the
// pre-existing exec error surfaces unchanged.
func wrapScriptInvocation(inv Invocation) Invocation {
	needs, runtimeOrder := scriptRuntimePreference(inv.Name)
	if !needs {
		return inv
	}
	runtimeBin := findJSRuntime(runtimeOrder)
	if runtimeBin == "" {
		return inv
	}
	return Invocation{
		Name:      runtimeBin,
		ArgPrefix: append([]string{inv.Name}, inv.ArgPrefix...),
		Installed: inv.Installed,
	}
}

// scriptRuntimePreference reports whether name must be launched via a JS
// runtime, and in which order the runtimes should be tried. A shebang naming
// bun promotes bun over node; the default order prefers node.
func scriptRuntimePreference(name string) (bool, []string) {
	nodeFirst := []string{"node", "bun"}
	bunFirst := []string{"bun", "node"}

	switch strings.ToLower(filepath.Ext(name)) {
	case ".js", ".mjs", ".cjs":
		if shebangRuntime(name) == "bun" {
			return true, bunFirst
		}
		return true, nodeFirst
	}
	// Windows does not honor shebangs, so an extensionless real file can only
	// be exec'd directly when it is a PE binary. Wrap it when its shebang
	// names a JS runtime; otherwise leave it alone (e.g. a bash script).
	// PATH-style bare command names ("npx" etc.) fail the open and stay as-is.
	if goosForInvocation == "windows" && filepath.Ext(name) == "" {
		switch shebangRuntime(name) {
		case "node":
			return true, nodeFirst
		case "bun":
			return true, bunFirst
		}
	}
	return false, nil
}

// shebangRuntime returns "node" or "bun" when the first line of name is a
// shebang naming one of them (including `#!/usr/bin/env -S node ...` forms),
// or "" otherwise.
func shebangRuntime(name string) string {
	f, err := os.Open(name)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 128)
	n, _ := f.Read(buf)
	line := string(buf[:n])
	if !strings.HasPrefix(line, "#!") {
		return ""
	}
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	for _, field := range strings.Fields(line[2:]) {
		switch strings.ToLower(filepath.Base(field)) {
		case "node":
			return "node"
		case "bun":
			return "bun"
		}
	}
	return ""
}

// findJSRuntime returns the executable path of the first available JS runtime
// in order, or "" when none is installed.
func findJSRuntime(order []string) string {
	for _, bin := range order {
		if path, err := lookPathForInvocation(bin); err == nil {
			return path
		}
	}
	return ""
}

func Run(ctx context.Context, command string, args []string, allowNpx bool) (CommandResult, error) {
	inv, ok := ResolveInvocation(allowNpx)
	if !ok {
		return CommandResult{ExitCode: 127}, ErrNotInstalled
	}

	fullArgs := append([]string{}, inv.ArgPrefix...)
	fullArgs = append(fullArgs, command)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, inv.Name, fullArgs...)
	cmd.Env = withNonInteractiveEnv(os.Environ())

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := CommandResult{
		ExitCode: exitCode(err),
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
	}
	return result, err
}

func Stream(ctx context.Context, command string, args []string, allowNpx bool, stdout, stderr io.Writer) (int, error) {
	inv, ok := ResolveInvocation(allowNpx)
	if !ok {
		fmt.Fprintln(stderr, "harness-mem is not installed")
		return 127, ErrNotInstalled
	}

	fullArgs := append([]string{}, inv.ArgPrefix...)
	fullArgs = append(fullArgs, command)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, inv.Name, fullArgs...)
	cmd.Env = withNonInteractiveEnv(os.Environ())
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	return exitCode(err), err
}

func Doctor(ctx context.Context, allowNpx bool) (DoctorReport, CommandResult, error) {
	result, err := Run(ctx, "doctor", []string{"--json", "--platform", DefaultPlatforms, "--skip-version-check"}, allowNpx)
	if errors.Is(err, ErrNotInstalled) {
		return DoctorReport{}, result, err
	}
	if strings.TrimSpace(result.Stdout) == "" {
		if err != nil {
			return DoctorReport{}, result, err
		}
		return DoctorReport{}, result, fmt.Errorf("doctor returned empty stdout")
	}

	var report DoctorReport
	if jsonErr := json.Unmarshal([]byte(result.Stdout), &report); jsonErr != nil {
		return DoctorReport{}, result, fmt.Errorf("doctor returned invalid JSON: %w", jsonErr)
	}
	return report, result, err
}

type AutoSetupResult struct {
	Attempted bool
	Skipped   bool
	Ready     bool
	Reason    string
	ExitCode  int
}

func AutoSetupFromSetupHook(markerPath string) AutoSetupResult {
	if os.Getenv("CLAUDE_CODE_HARNESS_MEM_AUTO_SETUP") == "0" {
		return AutoSetupResult{Skipped: true, Reason: "disabled"}
	}
	if markerPath != "" {
		if _, err := os.Stat(markerPath); err == nil {
			return AutoSetupResult{Skipped: true, Reason: "already-attempted"}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultSetupHookTimeout)
	defer cancel()

	report, _, err := Doctor(ctx, false)
	if err == nil && report.AllGreen {
		writeMarker(markerPath, "ready")
		return AutoSetupResult{Skipped: true, Ready: true, Reason: "already-ready"}
	}

	setupArgs := []string{"--platform", DefaultPlatforms, "--skip-quality", "--auto-update", "enable"}
	result, setupErr := Run(ctx, "setup", setupArgs, true)
	writeMarker(markerPath, "attempted")

	if setupErr != nil {
		reason := strings.TrimSpace(result.Stderr)
		if reason == "" {
			reason = setupErr.Error()
		}
		return AutoSetupResult{Attempted: true, Reason: reason, ExitCode: result.ExitCode}
	}

	return AutoSetupResult{Attempted: true, Ready: true, Reason: "setup-complete", ExitCode: 0}
}

func DefaultTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), defaultCommandTimeout)
}

func withNonInteractiveEnv(env []string) []string {
	filtered := make([]string, 0, len(env)+1)
	seen := false
	for _, item := range env {
		if strings.HasPrefix(item, "HARNESS_MEM_NON_INTERACTIVE=") {
			filtered = append(filtered, "HARNESS_MEM_NON_INTERACTIVE=1")
			seen = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !seen {
		filtered = append(filtered, "HARNESS_MEM_NON_INTERACTIVE=1")
	}
	return filtered
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func writeMarker(path, status string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	payload := map[string]string{
		"status":       status,
		"attempted_at": time.Now().UTC().Format(time.RFC3339),
		"contract":     ContractVersion,
	}
	data, _ := json.Marshal(payload)
	_ = os.WriteFile(path, append(data, '\n'), 0o644)
}
