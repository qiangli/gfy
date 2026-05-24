// Package install writes skill/plugin registration files so that gfy is
// auto-discoverable from Claude Code, Cursor, and other LLM IDEs.
//
// Two artefacts can be produced per target:
//
//   - A "skill" file (Claude Code SKILL.md, Cursor .mdc) that tells the host
//     LLM when and how to invoke gfy. The host loads these on startup, so the
//     model proactively suggests `gfy build`, `gfy query`, etc.
//   - An MCP server config snippet. gfy already speaks MCP via `gfy serve`;
//     adding it as a configured server lets the host call gfy tools directly
//     (semantic_search, trace_calls, ...).
//
// We never modify a user's MCP config in place — `~/.claude.json` and friends
// are user-owned and risky to merge programmatically. The snippet is printed
// for the user to paste, with the exact target path called out.
package install

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Target identifies a host IDE. Add to allTargets when extending.
type Target string

const (
	TargetClaudeCode Target = "claude-code"
	TargetCursor     Target = "cursor"
)

// AllTargets returns every supported install target.
func AllTargets() []Target {
	return []Target{TargetClaudeCode, TargetCursor}
}

// ParseTarget converts a user-supplied string ("claude-code", "cursor", "all")
// into a list of targets. Empty string defaults to claude-code.
func ParseTarget(s string) ([]Target, error) {
	switch s {
	case "", "claude-code", "claude":
		return []Target{TargetClaudeCode}, nil
	case "cursor":
		return []Target{TargetCursor}, nil
	case "all":
		return AllTargets(), nil
	}
	return nil, fmt.Errorf("unknown target %q (want: claude-code, cursor, all)", s)
}

// Scope determines where the skill is installed.
type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

// Options drives the install process.
type Options struct {
	Target     Target
	Scope      Scope
	ProjectDir string // used when Scope=project
	BinaryPath string // absolute path to the gfy binary; embedded into snippets
	DryRun     bool   // print what would be written instead of writing
	Uninstall  bool   // remove previously written files
	Out        io.Writer
}

// Run performs the install (or dry-run / uninstall) for a single target.
// Returns the absolute paths of files written (or that would be written).
func Run(opts Options) ([]string, error) {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.BinaryPath == "" {
		opts.BinaryPath = "gfy"
	}

	switch opts.Target {
	case TargetClaudeCode:
		return runClaudeCode(opts)
	case TargetCursor:
		return runCursor(opts)
	}
	return nil, fmt.Errorf("unsupported target: %s", opts.Target)
}

func runClaudeCode(opts Options) ([]string, error) {
	skillDir, err := claudeSkillDir(opts.Scope, opts.ProjectDir)
	if err != nil {
		return nil, err
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if opts.Uninstall {
		return uninstallPath(opts, skillDir)
	}

	content := claudeSkillTemplate(opts.BinaryPath)
	if err := writeFile(opts, skillPath, content); err != nil {
		return nil, err
	}

	printMCPSnippet(opts, "Claude Code (~/.claude.json or claude_desktop_config.json)", claudeMCPSnippet(opts.BinaryPath))
	return []string{skillPath}, nil
}

func runCursor(opts Options) ([]string, error) {
	rulesPath, err := cursorRulesPath(opts.Scope, opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	if opts.Uninstall {
		return uninstallPath(opts, rulesPath)
	}

	content := cursorRulesTemplate(opts.BinaryPath)
	if err := writeFile(opts, rulesPath, content); err != nil {
		return nil, err
	}

	printMCPSnippet(opts, "Cursor (~/.cursor/mcp.json)", cursorMCPSnippet(opts.BinaryPath))
	return []string{rulesPath}, nil
}

func claudeSkillDir(scope Scope, projectDir string) (string, error) {
	switch scope {
	case ScopeProject:
		base := projectDir
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
		return filepath.Join(base, ".claude", "skills", "gfy"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "skills", "gfy"), nil
	}
}

func cursorRulesPath(scope Scope, projectDir string) (string, error) {
	switch scope {
	case ScopeProject:
		base := projectDir
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
		return filepath.Join(base, ".cursor", "rules", "gfy.mdc"), nil
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cursor", "rules", "gfy.mdc"), nil
	}
}

// writeFile creates parents and writes content. In DryRun mode it prints the
// intended path and content size to the configured writer.
func writeFile(opts Options, path, content string) error {
	if opts.DryRun {
		fmt.Fprintf(opts.Out, "[dry-run] would write %s (%d bytes)\n", path, len(content))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(opts.Out, "wrote %s\n", path)
	return nil
}

func uninstallPath(opts Options, path string) ([]string, error) {
	if opts.DryRun {
		fmt.Fprintf(opts.Out, "[dry-run] would remove %s\n", path)
		return []string{path}, nil
	}
	if err := os.RemoveAll(path); err != nil {
		return nil, fmt.Errorf("remove %s: %w", path, err)
	}
	fmt.Fprintf(opts.Out, "removed %s\n", path)
	return []string{path}, nil
}

func printMCPSnippet(opts Options, label, snippet string) {
	if opts.Uninstall {
		return
	}
	fmt.Fprintf(opts.Out, "\nMCP server snippet for %s:\n", label)
	fmt.Fprintln(opts.Out, snippet)
}
