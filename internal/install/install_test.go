package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		in      string
		want    []Target
		wantErr bool
	}{
		{"", []Target{TargetClaudeCode}, false},
		{"claude-code", []Target{TargetClaudeCode}, false},
		{"claude", []Target{TargetClaudeCode}, false},
		{"cursor", []Target{TargetCursor}, false},
		{"all", []Target{TargetClaudeCode, TargetCursor}, false},
		{"bogus", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseTarget(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseTarget(%q): want error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTarget(%q): %v", tt.in, err)
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("ParseTarget(%q): got %v want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseTarget(%q)[%d]: got %s want %s", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestRun_ClaudeCode_ProjectScope(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	written, err := Run(Options{
		Target:     TargetClaudeCode,
		Scope:      ScopeProject,
		ProjectDir: dir,
		BinaryPath: "/usr/local/bin/gfy",
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 path, got %v", written)
	}
	expected := filepath.Join(dir, ".claude", "skills", "gfy", "SKILL.md")
	if written[0] != expected {
		t.Errorf("wrong path: got %s want %s", written[0], expected)
	}
	content, err := os.ReadFile(written[0])
	if err != nil {
		t.Fatalf("read written skill: %v", err)
	}
	body := string(content)
	if !strings.Contains(body, "name: gfy") {
		t.Errorf("skill missing name frontmatter:\n%s", body)
	}
	if !strings.Contains(body, "/usr/local/bin/gfy build .") {
		t.Errorf("skill missing binary path interpolation:\n%s", body)
	}
	if !strings.Contains(out.String(), "MCP server snippet") {
		t.Errorf("expected MCP snippet in stdout, got: %s", out.String())
	}
}

func TestRun_Cursor_ProjectScope(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	written, err := Run(Options{
		Target:     TargetCursor,
		Scope:      ScopeProject,
		ProjectDir: dir,
		BinaryPath: "gfy",
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("expected 1 path, got %v", written)
	}
	expected := filepath.Join(dir, ".cursor", "rules", "gfy.mdc")
	if written[0] != expected {
		t.Errorf("wrong path: got %s want %s", written[0], expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("rules file not written: %v", err)
	}
}

func TestRun_DryRun_DoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	_, err := Run(Options{
		Target:     TargetClaudeCode,
		Scope:      ScopeProject,
		ProjectDir: dir,
		BinaryPath: "gfy",
		DryRun:     true,
		Out:        &out,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	path := filepath.Join(dir, ".claude", "skills", "gfy", "SKILL.md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("dry-run wrote file at %s", path)
	}
	if !strings.Contains(out.String(), "[dry-run]") {
		t.Errorf("expected [dry-run] marker, got: %s", out.String())
	}
}

func TestRun_Uninstall_RemovesSkill(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	// First install.
	if _, err := Run(Options{
		Target: TargetClaudeCode, Scope: ScopeProject, ProjectDir: dir,
		BinaryPath: "gfy", Out: &out,
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	skillPath := filepath.Join(dir, ".claude", "skills", "gfy", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("install didn't create skill: %v", err)
	}

	// Now uninstall.
	out.Reset()
	if _, err := Run(Options{
		Target: TargetClaudeCode, Scope: ScopeProject, ProjectDir: dir,
		BinaryPath: "gfy", Uninstall: true, Out: &out,
	}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	skillDir := filepath.Join(dir, ".claude", "skills", "gfy")
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Errorf("uninstall didn't remove skill dir %s", skillDir)
	}
	if strings.Contains(out.String(), "MCP server snippet") {
		t.Errorf("uninstall should not print MCP snippet, got: %s", out.String())
	}
}
