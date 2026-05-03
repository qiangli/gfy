package semantic

import (
	"testing"

	"github.com/qiangli/gfy/internal/types"
)

func TestParseParamSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"8.0B", 8e9},
		{"14B", 14e9},
		{"3.8B", 3800000000},
		{"500M", 500e6},
		{"", 0},
	}
	for _, tt := range tests {
		got := parseParamSize(tt.input)
		if got != tt.want {
			t.Errorf("parseParamSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSelectModel(t *testing.T) {
	models := []ModelInfo{
		{Name: "llama3.2:3b", paramBytes: 3e9},
		{Name: "qwen3:8b", paramBytes: 8e9},
		{Name: "qwen3:14b", paramBytes: 14e9},
		{Name: "codellama:7b", paramBytes: 7e9},
	}
	got := SelectModel(models)
	if got != "qwen3:14b" {
		t.Errorf("SelectModel = %q, want qwen3:14b", got)
	}
}

func TestSelectModelNoMatch(t *testing.T) {
	models := []ModelInfo{
		{Name: "tinyllama:1b", paramBytes: 1e9},
	}
	got := SelectModel(models)
	if got != "" {
		t.Errorf("SelectModel = %q, want empty", got)
	}
}

func TestSelectModelPrefersLargestUnder14B(t *testing.T) {
	models := []ModelInfo{
		{Name: "gemma3:4b", paramBytes: 4e9},
		{Name: "gemma3:12b", paramBytes: 12e9},
		{Name: "gemma3:27b", paramBytes: 27e9},
	}
	got := SelectModel(models)
	if got != "gemma3:12b" {
		t.Errorf("SelectModel = %q, want gemma3:12b", got)
	}
}

func TestSelectModelFallsBackToLarger(t *testing.T) {
	// If all models are > 14B, pick the first one found for the family.
	models := []ModelInfo{
		{Name: "qwen3:32b", paramBytes: 32e9},
	}
	got := SelectModel(models)
	if got != "qwen3:32b" {
		t.Errorf("SelectModel = %q, want qwen3:32b", got)
	}
}

func TestParseLLMResponse(t *testing.T) {
	raw := `{"nodes":[{"id":"main_server","label":"Server","file_type":"code","source_file":"main.go"}],"edges":[{"source":"main_server","target":"main_db","relation":"conceptually_related_to","confidence":"INFERRED","confidence_score":0.7,"source_file":"main.go","weight":1.0}]}`

	result := parseLLMResponse(raw)
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != "main_server" {
		t.Errorf("node ID = %q, want main_server", result.Nodes[0].ID)
	}
	if len(result.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result.Edges))
	}
	if result.Edges[0].Confidence != types.Inferred {
		t.Errorf("edge confidence = %q, want INFERRED", result.Edges[0].Confidence)
	}
	if result.Edges[0].ConfidenceScore != 0.7 {
		t.Errorf("edge confidence_score = %f, want 0.7", result.Edges[0].ConfidenceScore)
	}
}

func TestParseLLMResponseWithMarkdownFences(t *testing.T) {
	raw := "```json\n{\"nodes\":[],\"edges\":[]}\n```"
	result := parseLLMResponse(raw)
	if result.Nodes != nil || result.Edges != nil {
		t.Errorf("expected empty result, got %d nodes %d edges", len(result.Nodes), len(result.Edges))
	}
}

func TestParseLLMResponseWithThinkingTags(t *testing.T) {
	raw := "<think>\nLet me analyze this code...\n</think>\n{\"nodes\":[{\"id\":\"a\",\"label\":\"A\",\"file_type\":\"code\",\"source_file\":\"a.go\"}],\"edges\":[]}"
	result := parseLLMResponse(raw)
	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	if result.Nodes[0].ID != "a" {
		t.Errorf("node ID = %q, want a", result.Nodes[0].ID)
	}
}

func TestParseLLMResponseInvalid(t *testing.T) {
	result := parseLLMResponse("not json at all")
	if len(result.Nodes) != 0 || len(result.Edges) != 0 {
		t.Errorf("expected empty result for invalid JSON")
	}
}

func TestParseLLMResponseBadConfidence(t *testing.T) {
	raw := `{"nodes":[],"edges":[{"source":"a","target":"b","relation":"calls","confidence":"WRONG","confidence_score":-1,"source_file":"x.go","weight":0}]}`
	result := parseLLMResponse(raw)
	if len(result.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result.Edges))
	}
	if result.Edges[0].Confidence != types.Inferred {
		t.Errorf("bad confidence should default to INFERRED, got %q", result.Edges[0].Confidence)
	}
	if result.Edges[0].ConfidenceScore != 0.8 {
		t.Errorf("bad score should default to 0.8, got %f", result.Edges[0].ConfidenceScore)
	}
	if result.Edges[0].Weight != 1.0 {
		t.Errorf("bad weight should default to 1.0, got %f", result.Edges[0].Weight)
	}
}

func TestParseLLMResponseCompressed(t *testing.T) {
	input := `{"n":[{"id":"readme_auth","l":"Auth Design","t":"document","f":"README.md"}],"e":[{"s":"readme_auth","g":"auth_validate","r":"references","c":"EXTRACTED","cs":0.9,"f":"README.md","w":1.0}]}`
	result := parseLLMResponse(input)

	if len(result.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result.Nodes))
	}
	n := result.Nodes[0]
	if n.ID != "readme_auth" || n.Label != "Auth Design" || n.FileType != "document" || n.SourceFile != "README.md" {
		t.Errorf("compressed node parsed incorrectly: %+v", n)
	}

	if len(result.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result.Edges))
	}
	e := result.Edges[0]
	if e.Source != "readme_auth" || e.Target != "auth_validate" || e.Relation != "references" {
		t.Errorf("compressed edge parsed incorrectly: %+v", e)
	}
	if e.Confidence != types.Extracted || e.ConfidenceScore != 0.9 {
		t.Errorf("compressed edge confidence wrong: %s, %f", e.Confidence, e.ConfidenceScore)
	}
}

func TestParseLLMResponseMixed(t *testing.T) {
	// If LLM returns verbose format, it should still work.
	input := `{"nodes":[{"id":"a","label":"A","file_type":"concept","source_file":"doc.md"}],"edges":[]}`
	result := parseLLMResponse(input)
	if len(result.Nodes) != 1 || result.Nodes[0].Label != "A" {
		t.Errorf("verbose fallback failed: %+v", result)
	}
}

func TestMerge(t *testing.T) {
	ast := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "a", Label: "A (ast)", FileType: "code", SourceFile: "a.go"},
			{ID: "b", Label: "B", FileType: "code", SourceFile: "b.go"},
		},
		Edges: []types.Edge{
			{Source: "a", Target: "b", Relation: "calls", Confidence: types.Extracted},
		},
	}
	sem := &types.ExtractionResult{
		Nodes: []types.Node{
			{ID: "a", Label: "A (semantic)", FileType: "code", SourceFile: "a.go"},
			{ID: "c", Label: "C", FileType: "concept", SourceFile: ""},
		},
		Edges: []types.Edge{
			{Source: "a", Target: "c", Relation: "conceptually_related_to", Confidence: types.Inferred},
		},
		InputTokens:  100,
		OutputTokens: 50,
	}

	merged := Merge(ast, sem)

	// 3 unique nodes: a (overwritten by semantic), b, c
	if len(merged.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(merged.Nodes))
	}
	// Node "a" should have the semantic label.
	for _, n := range merged.Nodes {
		if n.ID == "a" && n.Label != "A (semantic)" {
			t.Errorf("node a label = %q, want 'A (semantic)'", n.Label)
		}
	}
	// 2 edges total (no dedup).
	if len(merged.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(merged.Edges))
	}
	if merged.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", merged.InputTokens)
	}
}

func TestStripMarkdownFences(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
	}
	for _, tt := range tests {
		got := stripMarkdownFences(tt.input)
		if got != tt.want {
			t.Errorf("stripMarkdownFences(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripThinkingTags(t *testing.T) {
	input := "<think>\nsome reasoning\n</think>\n{\"result\": true}"
	want := "{\"result\": true}"
	got := stripThinkingTags(input)
	if got != want {
		t.Errorf("stripThinkingTags = %q, want %q", got, want)
	}
}
