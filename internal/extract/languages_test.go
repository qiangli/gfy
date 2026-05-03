package extract

import (
	"path/filepath"
	"testing"

	"github.com/qiangli/gfy/internal/types"
)

func TestLanguageExtractors(t *testing.T) {
	dir := testdataDir()

	tests := []struct {
		name     string
		file     string
		extract  func(string) *types.ExtractionResult
		wantRels []string
	}{
		{"Java", "sample.java", func(p string) *types.ExtractionResult { return ExtractGeneric(p, JavaConfig()) }, []string{"contains"}},
		{"Rust", "sample.rs", ExtractRust, []string{"contains"}},
		{"C", "sample.c", func(p string) *types.ExtractionResult { return ExtractGeneric(p, CConfig()) }, nil},
		{"C++", "sample.cpp", func(p string) *types.ExtractionResult { return ExtractGeneric(p, CppConfig()) }, nil},
		{"Ruby", "sample.rb", func(p string) *types.ExtractionResult { return ExtractGeneric(p, RubyConfig()) }, nil},
		{"C#", "sample.cs", func(p string) *types.ExtractionResult { return ExtractGeneric(p, CSharpConfig()) }, nil},
		{"Kotlin", "sample.kt", func(p string) *types.ExtractionResult { return ExtractGeneric(p, KotlinConfig()) }, nil},
		{"Scala", "sample.scala", func(p string) *types.ExtractionResult { return ExtractGeneric(p, ScalaConfig()) }, nil},
		{"PHP", "sample.php", func(p string) *types.ExtractionResult { return ExtractGeneric(p, PHPConfig()) }, nil},
		{"Swift", "sample.swift", func(p string) *types.ExtractionResult { return ExtractGeneric(p, SwiftConfig()) }, nil},
		{"Zig", "sample.zig", ExtractZig, []string{"contains"}},
		{"PowerShell", "sample.ps1", ExtractPowerShell, []string{"contains"}},
		{"Elixir", "sample.ex", ExtractElixir, nil},
		{"Julia", "sample.jl", ExtractJulia, []string{"contains"}},
		{"Objective-C", "sample.m", ExtractObjC, []string{"contains"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			result := tt.extract(path)

			if result.Error != "" {
				t.Fatalf("extraction error: %s", result.Error)
			}
			if len(result.Nodes) == 0 {
				t.Fatal("expected nodes, got none")
			}
			if len(result.Edges) == 0 {
				t.Fatal("expected edges, got none")
			}

			t.Logf("%s: %d nodes, %d edges", tt.name, len(result.Nodes), len(result.Edges))

			// Check wanted relations.
			relations := make(map[string]bool)
			for _, e := range result.Edges {
				relations[e.Relation] = true
			}
			for _, want := range tt.wantRels {
				if !relations[want] {
					t.Errorf("missing relation %q", want)
				}
			}

			// No dangling edges (except imports).
			nodeIDs := make(map[string]bool)
			for _, n := range result.Nodes {
				nodeIDs[n.ID] = true
			}
			for _, e := range result.Edges {
				if e.Relation == "imports" || e.Relation == "imports_from" {
					continue
				}
				if !nodeIDs[e.Source] {
					t.Errorf("dangling edge source: %s (relation: %s)", e.Source, e.Relation)
				}
				if !nodeIDs[e.Target] {
					t.Errorf("dangling edge target: %s -> %s (relation: %s)", e.Source, e.Target, e.Relation)
				}
			}
		})
	}
}
