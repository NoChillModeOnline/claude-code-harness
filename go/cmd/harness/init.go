package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Chachamaru127/claude-code-harness/go/internal/scaffold"
)

// runInit implements the "harness init" subcommand.
//
// It writes harness.toml to the project root (first argument, or cwd if none).
// If harness.toml already exists the command exits with an error to prevent
// accidental overwrite.  It also ensures the .claude-plugin/ directory exists
// so that a subsequent "harness sync" has a destination to write into.
//
// After writing the template it prints guidance to run "harness sync".
func runInit(args []string) {
	// Determine project root (same logic as runSync).
	projectRoot, err := resolveProjectRoot(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness init: %v\n", err)
		os.Exit(1)
	}

	tomlPath := filepath.Join(projectRoot, "harness.toml")

	// Refuse to overwrite an existing harness.toml.
	if _, err := os.Stat(tomlPath); err == nil {
		fmt.Fprintf(os.Stderr, "harness init: %s already exists — remove it first or edit it directly\n", tomlPath)
		os.Exit(1)
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "harness init: stat %s: %v\n", tomlPath, err)
		os.Exit(1)
	}

	// Ensure .claude-plugin/ directory exists so "harness sync" can write into it.
	pluginDir := filepath.Join(projectRoot, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "harness init: mkdir %s: %v\n", pluginDir, err)
		os.Exit(1)
	}

	// Write harness.toml template.
	if err := os.WriteFile(tomlPath, []byte(scaffold.HarnessTomlTemplate), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "harness init: write %s: %v\n", tomlPath, err)
		os.Exit(1)
	}

	fmt.Printf("  wrote %s\n", rel(projectRoot, tomlPath))
	fmt.Printf("  created %s\n", rel(projectRoot, pluginDir))
	fmt.Println()
	fmt.Println("harness init: done")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit harness.toml — set [project].name and adjust [safety] rules")
	fmt.Println("  2. Run `harness sync` to generate CC plugin files")
}
