package harnessmem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setInvocationTestHooks swaps goosForInvocation / lookPathForInvocation for
// the duration of the test and restores them afterwards.
func setInvocationTestHooks(t *testing.T, goos string, lookPath func(string) (string, error)) {
	t.Helper()
	origGOOS := goosForInvocation
	origLookPath := lookPathForInvocation
	if goos != "" {
		goosForInvocation = goos
	}
	if lookPath != nil {
		lookPathForInvocation = lookPath
	}
	t.Cleanup(func() {
		goosForInvocation = origGOOS
		lookPathForInvocation = origLookPath
	})
}

// fakeLookPath resolves only node / bun to fixed paths.
func fakeLookPath(t *testing.T) func(string) (string, error) {
	t.Helper()
	return func(bin string) (string, error) {
		switch bin {
		case "node":
			return "/fake/bin/node", nil
		case "bun":
			return "/fake/bin/bun", nil
		}
		return "", errors.New("not found: " + bin)
	}
}

// unsetEnvForTest unsets key for the duration of the test. t.Setenv registers
// restoration of the ambient value; the raw os.Unsetenv then removes it.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	os.Unsetenv(key)
}

const (
	nodeShebangScript = "#!/usr/bin/env node\nconsole.log(1)\n"
	bunShebangScript  = "#!/usr/bin/env bun\nconsole.log(1)\n"
	bashShebangScript = "#!/bin/bash\necho hi\n"
)

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// runtimeRootCandidate returns the resolver's home-candidate path rooted at a
// temp HOME (also exported as USERPROFILE for the Windows-GOOS code path).
func runtimeRootCandidate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetEnvForTest(t, "HARNESS_MEM_CLI")
	return filepath.Join(home, ".harness-mem", "runtime", "harness-mem", "scripts", "harness-mem")
}

func TestResolveInvocation_WrapsJSExtensionWithNode(t *testing.T) {
	setInvocationTestHooks(t, "", fakeLookPath(t))

	script := filepath.Join(t.TempDir(), "harness-mem.js")
	writeScript(t, script, nodeShebangScript)
	t.Setenv("HARNESS_MEM_CLI", script)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != "/fake/bin/node" {
		t.Errorf("Name = %q, want node runtime", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != script {
		t.Errorf("ArgPrefix = %v, want [%s]", inv.ArgPrefix, script)
	}
	if !inv.Installed {
		t.Error("Installed should remain true")
	}
}

func TestResolveInvocation_BunShebangJSPrefersBun(t *testing.T) {
	// Both runtimes are installed; a bun shebang must keep bun selected so a
	// Bun-specific entry point does not silently switch to node.
	setInvocationTestHooks(t, "", fakeLookPath(t))

	script := filepath.Join(t.TempDir(), "harness-mem.js")
	writeScript(t, script, bunShebangScript)
	t.Setenv("HARNESS_MEM_CLI", script)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != "/fake/bin/bun" {
		t.Errorf("Name = %q, want bun runtime for bun shebang", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != script {
		t.Errorf("ArgPrefix = %v, want [%s]", inv.ArgPrefix, script)
	}
}

func TestResolveInvocation_FallsBackToBunWhenNodeMissing(t *testing.T) {
	setInvocationTestHooks(t, "", func(bin string) (string, error) {
		if bin == "bun" {
			return "/fake/bin/bun", nil
		}
		return "", errors.New("not found: " + bin)
	})

	script := filepath.Join(t.TempDir(), "harness-mem.js")
	writeScript(t, script, nodeShebangScript)
	t.Setenv("HARNESS_MEM_CLI", script)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != "/fake/bin/bun" {
		t.Errorf("Name = %q, want bun runtime", inv.Name)
	}
}

func TestResolveInvocation_NoJSRuntimeKeepsOriginal(t *testing.T) {
	setInvocationTestHooks(t, "", func(bin string) (string, error) {
		return "", errors.New("not found: " + bin)
	})

	script := filepath.Join(t.TempDir(), "harness-mem.js")
	writeScript(t, script, nodeShebangScript)
	t.Setenv("HARNESS_MEM_CLI", script)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	// Without a JS runtime the original invocation is kept and the
	// pre-existing exec error surfaces unchanged.
	if inv.Name != script {
		t.Errorf("Name = %q, want original script %q", inv.Name, script)
	}
	if len(inv.ArgPrefix) != 0 {
		t.Errorf("ArgPrefix = %v, want empty", inv.ArgPrefix)
	}
}

func TestResolveInvocation_UnixExtensionlessNotWrapped(t *testing.T) {
	setInvocationTestHooks(t, "linux", fakeLookPath(t))

	candidate := runtimeRootCandidate(t)
	writeScript(t, candidate, nodeShebangScript)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	// Unix honors shebangs, so extensionless scripts keep the historical
	// direct-exec behavior.
	if inv.Name != candidate {
		t.Errorf("Name = %q, want unwrapped candidate %q", inv.Name, candidate)
	}
	if len(inv.ArgPrefix) != 0 {
		t.Errorf("ArgPrefix = %v, want empty", inv.ArgPrefix)
	}
}

func TestResolveInvocation_WindowsPrefersJSSiblingOverBashWrapper(t *testing.T) {
	// Standard npm layout: the extensionless file is a bash CLI and the
	// sibling harness-mem.js is the node entry. Windows must pick the .js
	// (wrapping the bash file with node would fail with a SyntaxError).
	setInvocationTestHooks(t, "windows", fakeLookPath(t))

	candidate := runtimeRootCandidate(t)
	writeScript(t, candidate, bashShebangScript)
	jsCandidate := candidate + ".js"
	writeScript(t, jsCandidate, nodeShebangScript)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != "/fake/bin/node" {
		t.Errorf("Name = %q, want node runtime", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != jsCandidate {
		t.Errorf("ArgPrefix = %v, want [%s] (js sibling preferred)", inv.ArgPrefix, jsCandidate)
	}
}

func TestResolveInvocation_WindowsBashOnlyCandidateNotWrapped(t *testing.T) {
	// Without a .js sibling, a bash CLI must not be fed to node; keeping the
	// original invocation preserves the pre-existing (clearer) exec error.
	setInvocationTestHooks(t, "windows", fakeLookPath(t))

	candidate := runtimeRootCandidate(t)
	writeScript(t, candidate, bashShebangScript)
	t.Setenv("HARNESS_MEM_DISABLE_PATH_LOOKUP", "1")

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != candidate {
		t.Errorf("Name = %q, want unwrapped bash candidate %q", inv.Name, candidate)
	}
	if len(inv.ArgPrefix) != 0 {
		t.Errorf("ArgPrefix = %v, want empty", inv.ArgPrefix)
	}
}

func TestResolveInvocation_WindowsNodeShebangExtensionlessWrapped(t *testing.T) {
	// An extensionless file whose shebang names node cannot be exec'd on
	// Windows; it is wrapped with the JS runtime.
	setInvocationTestHooks(t, "windows", fakeLookPath(t))

	candidate := runtimeRootCandidate(t)
	writeScript(t, candidate, nodeShebangScript)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != "/fake/bin/node" {
		t.Errorf("Name = %q, want node runtime (shebang not honored on Windows)", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != candidate {
		t.Errorf("ArgPrefix = %v, want [%s]", inv.ArgPrefix, candidate)
	}
}

func TestResolveInvocation_WindowsJSOnlyCandidateWrapped(t *testing.T) {
	setInvocationTestHooks(t, "windows", fakeLookPath(t))

	candidate := runtimeRootCandidate(t)
	jsCandidate := candidate + ".js"
	writeScript(t, jsCandidate, nodeShebangScript)

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve via .js candidate")
	}
	if inv.Name != "/fake/bin/node" {
		t.Errorf("Name = %q, want node runtime", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != jsCandidate {
		t.Errorf("ArgPrefix = %v, want [%s]", inv.ArgPrefix, jsCandidate)
	}
}

func TestResolveInvocation_WindowsCmdShimNotWrapped(t *testing.T) {
	calls := 0
	setInvocationTestHooks(t, "windows", func(bin string) (string, error) {
		calls++
		if bin == "harness-mem" {
			return `C:\Users\test\AppData\Roaming\npm\harness-mem.cmd`, nil
		}
		return "", errors.New("not found: " + bin)
	})

	home := t.TempDir() // no runtime-root candidate exists
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetEnvForTest(t, "HARNESS_MEM_CLI")
	unsetEnvForTest(t, "HARNESS_MEM_DISABLE_PATH_LOOKUP")

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve via PATH shim")
	}
	// .cmd shims launch fine through CreateProcess; never wrap them.
	if !strings.HasSuffix(inv.Name, "harness-mem.cmd") {
		t.Errorf("Name = %q, want .cmd shim unwrapped", inv.Name)
	}
	if len(inv.ArgPrefix) != 0 {
		t.Errorf("ArgPrefix = %v, want empty", inv.ArgPrefix)
	}
	if calls == 0 {
		t.Error("lookPath should have been consulted")
	}
}

func TestResolveInvocation_WindowsPathLookupJSWrapped(t *testing.T) {
	// Windows LookPath can resolve harness-mem.js via PATHEXT (.JS is in the
	// default list). That resolution must also be wrapped — this is the exact
	// error path reported in issue #207.
	jsOnPath := `C:\Users\test\.harness-mem\runtime\harness-mem\scripts\harness-mem.js`
	setInvocationTestHooks(t, "windows", func(bin string) (string, error) {
		switch bin {
		case "harness-mem":
			return jsOnPath, nil
		case "node":
			return `C:\Program Files\nodejs\node.exe`, nil
		}
		return "", errors.New("not found: " + bin)
	})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetEnvForTest(t, "HARNESS_MEM_CLI")
	unsetEnvForTest(t, "HARNESS_MEM_DISABLE_PATH_LOOKUP")

	inv, ok := ResolveInvocation(false)
	if !ok {
		t.Fatal("expected invocation to resolve")
	}
	if inv.Name != `C:\Program Files\nodejs\node.exe` {
		t.Errorf("Name = %q, want node runtime", inv.Name)
	}
	if len(inv.ArgPrefix) != 1 || inv.ArgPrefix[0] != jsOnPath {
		t.Errorf("ArgPrefix = %v, want [%s]", inv.ArgPrefix, jsOnPath)
	}
}

func TestResolveInvocation_NpxFallbackNotWrapped(t *testing.T) {
	setInvocationTestHooks(t, "windows", fakeLookPath(t))

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetEnvForTest(t, "HARNESS_MEM_CLI")
	t.Setenv("HARNESS_MEM_DISABLE_PATH_LOOKUP", "1")

	inv, ok := ResolveInvocation(true)
	if !ok {
		t.Fatal("expected npx fallback")
	}
	if inv.Name != "npx" {
		t.Errorf("Name = %q, want npx", inv.Name)
	}
	if inv.Installed {
		t.Error("npx fallback should report Installed=false")
	}
}

func TestScriptRuntimePreference(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		path      string
		wantNeeds bool
		wantFirst string // first entry of the runtime order when wantNeeds
	}{
		{"js on linux", "linux", "x.js", true, "node"},
		{"mjs on darwin", "darwin", "x.mjs", true, "node"},
		{"cjs on windows", "windows", `C:\x.cjs`, true, "node"},
		{"exe on windows", "windows", `C:\harness-mem.exe`, false, ""},
		{"cmd on windows", "windows", `C:\harness-mem.cmd`, false, ""},
		{"missing extensionless on windows", "windows", `C:\does\not\exist\harness-mem`, false, ""},
		{"extensionless on linux", "linux", "/usr/local/bin/harness-mem", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setInvocationTestHooks(t, tt.goos, nil)
			needs, order := scriptRuntimePreference(tt.path)
			if needs != tt.wantNeeds {
				t.Fatalf("scriptRuntimePreference(%q) needs = %v, want %v", tt.path, needs, tt.wantNeeds)
			}
			if tt.wantNeeds && (len(order) == 0 || order[0] != tt.wantFirst) {
				t.Errorf("runtime order = %v, want first %q", order, tt.wantFirst)
			}
		})
	}
}

func TestScriptRuntimePreference_WindowsExtensionlessByShebang(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantNeeds bool
		wantFirst string
	}{
		{"node shebang", nodeShebangScript, true, "node"},
		{"bun shebang", bunShebangScript, true, "bun"},
		{"env -S node", "#!/usr/bin/env -S node --no-warnings\n", true, "node"},
		{"bash shebang", bashShebangScript, false, ""},
		{"no shebang", "plain text\n", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setInvocationTestHooks(t, "windows", nil)
			script := filepath.Join(t.TempDir(), "harness-mem")
			writeScript(t, script, tt.content)

			needs, order := scriptRuntimePreference(script)
			if needs != tt.wantNeeds {
				t.Fatalf("needs = %v, want %v", needs, tt.wantNeeds)
			}
			if tt.wantNeeds && (len(order) == 0 || order[0] != tt.wantFirst) {
				t.Errorf("runtime order = %v, want first %q", order, tt.wantFirst)
			}
		})
	}
}

func TestShebangRuntime(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		writeScript(t, path, content)
		return path
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"env node", write("a", "#!/usr/bin/env node\n"), "node"},
		{"direct node", write("b", "#!/usr/local/bin/node\n"), "node"},
		{"env bun", write("c", "#!/usr/bin/env bun\n"), "bun"},
		{"env -S node flags", write("d", "#!/usr/bin/env -S node --no-warnings\n"), "node"},
		{"bash", write("e", "#!/bin/bash\n"), ""},
		{"no shebang", write("f", "console.log(1)\n"), ""},
		{"missing file", filepath.Join(dir, "missing"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shebangRuntime(tt.path); got != tt.want {
				t.Errorf("shebangRuntime(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
