package semantic

import (
	"testing"

	"github.com/qiangli/gfy/pkg/types"
)

func TestLinkSemanticToAST_BasicMatch(t *testing.T) {
	// Use enough nodes so IDF values are meaningful — in small corpora,
	// shared terms get near-zero IDF.
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			// AST nodes with rich text — the target for matching.
			{ID: "auth_validatetoken", Label: "validateToken", FileType: "code",
				Comment:       "checks authentication credentials and verifies token validity",
				ThrowMessages: []string{"authentication failed", "invalid credentials"}},
			{ID: "session_start", Label: "Start", FileType: "code",
				Comment: "initializes user session"},
			{ID: "db_query", Label: "Query", FileType: "code",
				Comment: "executes database query"},
			{ID: "http_handler", Label: "handleRequest", FileType: "code",
				Comment: "processes incoming HTTP request"},
			{ID: "config_load", Label: "loadConfig", FileType: "code",
				Comment: "loads configuration from YAML"},
			{ID: "cache_store", Label: "storeCache", FileType: "code",
				Comment: "stores computed results in cache layer"},
			{ID: "metrics_emit", Label: "emitMetrics", FileType: "code",
				Comment: "sends telemetry data to monitoring"},
			{ID: "parser_run", Label: "runParser", FileType: "code",
				Comment: "parses source code into abstract syntax tree"},
			// Semantic node about authentication — should match auth_validatetoken.
			{ID: "readme_auth", Label: "Authentication Credential Validation", FileType: "document",
				SourceFile: "README.md"},
		},
		Edges: []types.Edge{},
	}

	result := LinkSemanticToAST(merged)

	// Should have created at least one edge from readme_auth to auth_validatetoken.
	found := false
	for _, e := range result.Edges {
		if e.Source == "readme_auth" && e.Target == "auth_validatetoken" {
			found = true
			if e.Confidence != types.Inferred {
				t.Errorf("expected INFERRED confidence, got %s", e.Confidence)
			}
			if e.ConfidenceScore <= 0 || e.ConfidenceScore > 1 {
				t.Errorf("confidence score %f out of range", e.ConfidenceScore)
			}
		}
	}
	if !found {
		t.Error("expected edge from readme_auth to auth_validatetoken")
		for _, e := range result.Edges {
			t.Logf("  edge: %s -> %s (sim=%.3f, rel=%s)", e.Source, e.Target, e.ConfidenceScore, e.Relation)
		}
	}
}

func TestLinkSemanticToAST_NoSemanticNodes(t *testing.T) {
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "auth_validate", Label: "validate", FileType: "code"},
		},
	}

	result := LinkSemanticToAST(merged)
	if len(result.Edges) != 0 {
		t.Errorf("expected no edges when no semantic nodes, got %d", len(result.Edges))
	}
}

func TestLinkSemanticToAST_NoASTNodes(t *testing.T) {
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "readme_overview", Label: "Project Overview", FileType: "document"},
		},
	}

	result := LinkSemanticToAST(merged)
	if len(result.Edges) != 0 {
		t.Errorf("expected no edges when no AST nodes, got %d", len(result.Edges))
	}
}

func TestLinkSemanticToAST_DeduplicatesEdges(t *testing.T) {
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "auth_validate", Label: "validateToken", FileType: "code",
				Comment: "authentication validation"},
			{ID: "docs_auth", Label: "Authentication Validation", FileType: "document"},
		},
		Edges: []types.Edge{
			// Pre-existing edge.
			{Source: "docs_auth", Target: "auth_validate", Relation: "references",
				Confidence: types.Extracted, ConfidenceScore: 0.9, Weight: 1.0},
		},
	}

	result := LinkSemanticToAST(merged)

	// Count edges from docs_auth to auth_validate.
	count := 0
	for _, e := range result.Edges {
		if e.Source == "docs_auth" && e.Target == "auth_validate" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 edge (deduped), got %d", count)
	}
}

func TestLinkSemanticToAST_Top5Limit(t *testing.T) {
	// Create many similar AST nodes.
	nodes := []types.Node{
		{ID: "sem_auth", Label: "Authentication System Design", FileType: "document"},
	}
	for i := 0; i < 10; i++ {
		nodes = append(nodes, types.Node{
			ID:       "auth_func_" + string(rune('a'+i)),
			Label:    "authFunction",
			FileType: "code",
			Comment:  "handles authentication logic",
		})
	}

	merged := &types.ExtractionResult{Nodes: nodes}
	result := LinkSemanticToAST(merged)

	// Count edges from the semantic node.
	count := 0
	for _, e := range result.Edges {
		if e.Source == "sem_auth" {
			count++
		}
	}
	if count > maxMatchesPerNode {
		t.Errorf("expected at most %d edges per semantic node, got %d", maxMatchesPerNode, count)
	}
}

func TestLinkSemanticToAST_SkipsSelfLinks(t *testing.T) {
	// Edge case: a node ID that appears in both AST and semantic sets
	// shouldn't create a self-link.
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "shared_node", Label: "SharedConcept", FileType: "code",
				Comment: "shared authentication concept"},
			{ID: "shared_node_doc", Label: "SharedConcept Documentation", FileType: "document"},
		},
	}

	result := LinkSemanticToAST(merged)

	for _, e := range result.Edges {
		if e.Source == e.Target {
			t.Errorf("self-link detected: %s -> %s", e.Source, e.Target)
		}
	}
}

func TestLinkSemanticToAST_RelationTypes(t *testing.T) {
	merged := &types.ExtractionResult{
		Nodes: []types.Node{
			// Highly similar: same words.
			{ID: "auth_validate", Label: "validateCredentials", FileType: "code",
				Comment: "validates user credentials for authentication"},
			// Weakly related: some overlap.
			{ID: "logging_setup", Label: "setupLogging", FileType: "code",
				Comment: "configures application logging"},
			// Semantic node.
			{ID: "docs_auth", Label: "Credential Validation Authentication", FileType: "document"},
		},
	}

	result := LinkSemanticToAST(merged)

	for _, e := range result.Edges {
		if e.Source == "docs_auth" && e.Target == "auth_validate" {
			// High similarity match should be "references".
			if e.ConfidenceScore >= referencesThreshold && e.Relation != "references" {
				t.Errorf("high similarity (%.2f) should use 'references', got %q", e.ConfidenceScore, e.Relation)
			}
		}
	}
}

func TestBuildNodeDirectory(t *testing.T) {
	nodes := []types.Node{
		{ID: "auth_validate", Label: "validateToken", FileType: "code",
			SourceFile: "internal/auth/service.go"},
		{ID: "utils_helper", Label: "helper", FileType: "code",
			SourceFile: "internal/utils/helper.go"},
		// Semantic nodes should be skipped.
		{ID: "readme_overview", Label: "Overview", FileType: "document"},
	}

	dir := buildNodeDirectory(nodes)

	if dir == "" {
		t.Fatal("expected non-empty directory")
	}
	if !contains(dir, "auth_validate") {
		t.Error("expected auth_validate in directory")
	}
	if !contains(dir, "validateToken") {
		t.Error("expected label in directory")
	}
	if !contains(dir, "utils_helper") {
		t.Error("expected utils_helper in directory")
	}
	if contains(dir, "readme_overview") {
		t.Error("document nodes should be skipped")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
