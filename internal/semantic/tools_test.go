package semantic

import (
	"strings"
	"testing"

	"github.com/qiangli/gfy/internal/types"
)

func makeTestResult() *types.ExtractionResult {
	return &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "auth_validate", Label: "validateToken", FileType: "code",
				SourceFile: "internal/auth/service.go", SourceLocation: "L42",
				Comment: "checks authentication credentials",
				Tags: []string{"throws"}, ThrowMessages: []string{"invalid token"}},
			{ID: "auth_login", Label: "login", FileType: "code",
				SourceFile: "internal/auth/service.go", SourceLocation: "L10",
				LogMessages: []string{"user logged in"}},
			{ID: "db_query", Label: "Query", FileType: "code",
				SourceFile: "internal/db/store.go", SourceLocation: "L5",
				Comment: "executes database query"},
			{ID: "http_handler", Label: "handleRequest", FileType: "code",
				SourceFile: "internal/http/server.go",
				Comment: "processes incoming HTTP requests"},
		},
		Edges: []types.Edge{
			{Source: "auth_login", Target: "auth_validate", Relation: "calls",
				Confidence: types.Extracted, ConfidenceScore: 1.0,
				SourceFile: "internal/auth/service.go"},
			{Source: "auth_validate", Target: "db_query", Relation: "calls",
				Confidence: types.Extracted, ConfidenceScore: 1.0,
				SourceFile: "internal/auth/service.go"},
			{Source: "http_handler", Target: "auth_login", Relation: "calls",
				Confidence: types.Extracted, ConfidenceScore: 1.0,
				SourceFile: "internal/http/server.go"},
		},
	}
}

func TestSearchNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	searchTool := findTool(tools, "search_nodes")
	if searchTool == nil {
		t.Fatal("search_nodes tool not found")
	}

	result := searchTool.Execute(map[string]any{"query": "validate"})
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate in results, got: %s", result)
	}

	// Search by partial match.
	result = searchTool.Execute(map[string]any{"query": "auth"})
	if !strings.Contains(result, "auth_validate") || !strings.Contains(result, "auth_login") {
		t.Errorf("expected both auth nodes, got: %s", result)
	}

	// No match.
	result = searchTool.Execute(map[string]any{"query": "xyznonexistent"})
	if strings.Contains(result, "auth_validate") {
		t.Error("expected no matches for nonsense query")
	}
}

func TestGetNodeDetail(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_node_detail")
	if tool == nil {
		t.Fatal("get_node_detail tool not found")
	}

	result := tool.Execute(map[string]any{"node_id": "auth_validate"})

	checks := []string{
		"auth_validate",
		"validateToken",
		"checks authentication credentials",
		"throws",
		"invalid token",
		"L42",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected %q in node detail, got: %s", check, result)
		}
	}

	// Not found.
	result = tool.Execute(map[string]any{"node_id": "nonexistent"})
	if !strings.Contains(result, "not found") {
		t.Error("expected 'not found' for missing node")
	}
}

func TestGetNeighbors(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_neighbors")
	if tool == nil {
		t.Fatal("get_neighbors tool not found")
	}

	result := tool.Execute(map[string]any{"node_id": "auth_validate"})
	// auth_validate has edges to auth_login (incoming) and db_query (outgoing).
	if !strings.Contains(result, "auth_login") || !strings.Contains(result, "db_query") {
		t.Errorf("expected both neighbors, got: %s", result)
	}
	if !strings.Contains(result, "calls") {
		t.Error("expected 'calls' relation in output")
	}

	// With relation filter.
	result = tool.Execute(map[string]any{"node_id": "auth_validate", "relation": "contains"})
	if !strings.Contains(result, "No matching edges") {
		t.Error("expected no matches for 'contains' relation filter")
	}
}

func TestListFiles(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "list_files")
	if tool == nil {
		t.Fatal("list_files tool not found")
	}

	result := tool.Execute(map[string]any{})
	if !strings.Contains(result, "internal/auth/service.go") {
		t.Error("expected auth service file")
	}
	if !strings.Contains(result, "internal/db/store.go") {
		t.Error("expected db store file")
	}
	if !strings.Contains(result, "3 files") {
		t.Errorf("expected 3 files, got: %s", result)
	}
}

func TestGetFileNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_file_nodes")
	if tool == nil {
		t.Fatal("get_file_nodes tool not found")
	}

	result := tool.Execute(map[string]any{"source_file": "internal/auth/service.go"})
	if !strings.Contains(result, "auth_validate") || !strings.Contains(result, "auth_login") {
		t.Errorf("expected both auth nodes, got: %s", result)
	}
	if !strings.Contains(result, "2 nodes") {
		t.Errorf("expected 2 nodes, got: %s", result)
	}

	// Not found.
	result = tool.Execute(map[string]any{"source_file": "nonexistent.go"})
	if !strings.Contains(result, "No nodes found") {
		t.Error("expected 'No nodes found' for missing file")
	}
}

func TestSimilarNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "similar_nodes")
	if tool == nil {
		t.Fatal("similar_nodes tool not found")
	}

	result := tool.Execute(map[string]any{"query": "authentication credential validation"})
	// Should find auth_validate as most similar.
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate in similar results, got: %s", result)
	}

	// Database query should match db-related terms.
	result = tool.Execute(map[string]any{"query": "database query execution"})
	if !strings.Contains(result, "db_query") {
		t.Errorf("expected db_query in similar results, got: %s", result)
	}
}

func TestSearchByTag(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "search_by_tag")
	if tool == nil {
		t.Fatal("search_by_tag tool not found")
	}

	result := tool.Execute(map[string]any{"tag": "throws"})
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate for 'throws' tag, got: %s", result)
	}

	result = tool.Execute(map[string]any{"tag": "nonexistent"})
	if !strings.Contains(result, "No nodes with tag") {
		t.Error("expected no results for nonexistent tag")
	}
}

func TestSearchComments(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "search_comments")
	if tool == nil {
		t.Fatal("search_comments tool not found")
	}

	// Search in comments.
	result := tool.Execute(map[string]any{"keyword": "authentication"})
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate for 'authentication', got: %s", result)
	}

	// Search in throw messages.
	result = tool.Execute(map[string]any{"keyword": "invalid token"})
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate for 'invalid token', got: %s", result)
	}

	// Search in log messages.
	result = tool.Execute(map[string]any{"keyword": "logged in"})
	if !strings.Contains(result, "auth_login") {
		t.Errorf("expected auth_login for 'logged in', got: %s", result)
	}
}

func TestGetCallers(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_callers")
	if tool == nil {
		t.Fatal("get_callers tool not found")
	}

	// auth_validate is called by auth_login.
	result := tool.Execute(map[string]any{"node_id": "auth_validate"})
	if !strings.Contains(result, "auth_login") {
		t.Errorf("expected auth_login as caller, got: %s", result)
	}
	// Should NOT show db_query (that's a callee, not caller).
	if strings.Contains(result, "db_query") {
		t.Error("db_query should not appear in callers (it's a callee)")
	}
}

func TestGetCallees(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_callees")
	if tool == nil {
		t.Fatal("get_callees tool not found")
	}

	// auth_validate calls db_query.
	result := tool.Execute(map[string]any{"node_id": "auth_validate"})
	if !strings.Contains(result, "db_query") {
		t.Errorf("expected db_query as callee, got: %s", result)
	}
	if strings.Contains(result, "auth_login") {
		t.Error("auth_login should not appear in callees (it's a caller)")
	}
}

func TestSearchFiles(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "search_files")
	if tool == nil {
		t.Fatal("search_files tool not found")
	}

	result := tool.Execute(map[string]any{"pattern": "auth"})
	if !strings.Contains(result, "internal/auth/service.go") {
		t.Errorf("expected auth service file, got: %s", result)
	}
	if strings.Contains(result, "internal/db") {
		t.Error("db file should not match 'auth' pattern")
	}

	result = tool.Execute(map[string]any{"pattern": "nonexistent"})
	if !strings.Contains(result, "No files matching") {
		t.Error("expected no results for nonexistent pattern")
	}
}

func TestGetRootNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_root_nodes")
	if tool == nil {
		t.Fatal("get_root_nodes tool not found")
	}

	result := tool.Execute(map[string]any{})
	// http_handler has no incoming edges → root node.
	if !strings.Contains(result, "http_handler") {
		t.Errorf("expected http_handler as root node, got: %s", result)
	}
	// auth_validate has incoming edges → NOT a root node.
	if strings.Contains(result, "auth_validate") {
		t.Error("auth_validate has incoming edges, should not be a root")
	}
}

func TestGetLeafNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_leaf_nodes")
	if tool == nil {
		t.Fatal("get_leaf_nodes tool not found")
	}

	result := tool.Execute(map[string]any{})
	// db_query has no outgoing edges → leaf node.
	if !strings.Contains(result, "db_query") {
		t.Errorf("expected db_query as leaf node, got: %s", result)
	}
	// http_handler has outgoing edges → NOT a leaf.
	if strings.Contains(result, "http_handler") {
		t.Error("http_handler has outgoing edges, should not be a leaf")
	}
}

func TestGetPath(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_path")
	if tool == nil {
		t.Fatal("get_path tool not found")
	}

	// http_handler → auth_login → auth_validate → db_query
	result := tool.Execute(map[string]any{"from": "http_handler", "to": "db_query"})
	if !strings.Contains(result, "Path") {
		t.Errorf("expected path found, got: %s", result)
	}
	if !strings.Contains(result, "http_handler") || !strings.Contains(result, "db_query") {
		t.Errorf("expected both endpoints in path, got: %s", result)
	}

	// No path between disconnected nodes — but in our test data all are connected.
	// Test not found.
	result = tool.Execute(map[string]any{"from": "nonexistent", "to": "db_query"})
	if !strings.Contains(result, "not found") {
		t.Error("expected 'not found' for missing node")
	}
}

func TestGetSubgraph(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_subgraph")
	if tool == nil {
		t.Fatal("get_subgraph tool not found")
	}

	// Depth 1 from auth_validate should find auth_login and db_query.
	result := tool.Execute(map[string]any{"node_id": "auth_validate", "depth": 1})
	if !strings.Contains(result, "auth_login") || !strings.Contains(result, "db_query") {
		t.Errorf("expected neighbors in subgraph, got: %s", result)
	}

	// Not found.
	result = tool.Execute(map[string]any{"node_id": "nonexistent"})
	if !strings.Contains(result, "not found") {
		t.Error("expected 'not found' for missing node")
	}
}

func TestWalkChain(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "walk_chain")
	if tool == nil {
		t.Fatal("walk_chain tool not found")
	}

	// Walk outgoing "calls" from http_handler.
	result := tool.Execute(map[string]any{"node_id": "http_handler", "relation": "calls"})
	if !strings.Contains(result, "auth_login") {
		t.Errorf("expected auth_login in call chain, got: %s", result)
	}

	// Walk incoming "calls" to db_query.
	result = tool.Execute(map[string]any{"node_id": "db_query", "relation": "calls", "direction": "incoming"})
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate as caller in chain, got: %s", result)
	}

	// No matching edges.
	result = tool.Execute(map[string]any{"node_id": "http_handler", "relation": "contains"})
	if !strings.Contains(result, "No outgoing") {
		t.Errorf("expected no edges for 'contains', got: %s", result)
	}
}

func TestGetHubNodes(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	tool := findTool(tools, "get_hub_nodes")
	if tool == nil {
		t.Fatal("get_hub_nodes tool not found")
	}

	result := tool.Execute(map[string]any{"top_n": 3})
	// auth_validate has the most edges (2 as source/target in calls).
	if !strings.Contains(result, "auth_validate") {
		t.Errorf("expected auth_validate as hub, got: %s", result)
	}
	if !strings.Contains(result, "degree") {
		t.Error("expected degree info in output")
	}
}

func TestASTToolsToToolDefs(t *testing.T) {
	tools := NewASTTools(makeTestResult())
	defs := astToolsToToolDefs(tools)

	if len(defs) != len(tools) {
		t.Fatalf("expected %d tool defs, got %d", len(tools), len(defs))
	}

	for _, def := range defs {
		if def.Type != "function" {
			t.Errorf("expected type 'function', got %q", def.Type)
		}
		if def.Function.Name == "" {
			t.Error("expected non-empty function name")
		}
		if def.Function.Parameters.Type != "object" {
			t.Errorf("expected parameters type 'object', got %q", def.Function.Parameters.Type)
		}
	}
}

func findTool(tools []ASTTool, name string) *ASTTool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
