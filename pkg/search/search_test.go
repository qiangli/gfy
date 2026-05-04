package search

import (
	"testing"

	"github.com/qiangli/gfy/pkg/graph"
)

func makeTestGraph() *graph.Graph {
	g := graph.New(false)
	g.AddNode("server", map[string]any{"label": "Server", "file_type": "code", "source_file": "server.go"})
	g.AddNode("new_server", map[string]any{"label": "NewServer()", "file_type": "code", "source_file": "server.go"})
	g.AddNode("start", map[string]any{"label": ".Start()", "file_type": "code", "source_file": "server.go"})
	g.AddNode("client", map[string]any{"label": "Client", "file_type": "code", "source_file": "client.go"})
	g.AddNode("handler", map[string]any{"label": "Handler", "file_type": "code", "source_file": "handler.go"})
	g.AddNode("auth", map[string]any{"label": "Authenticate()", "file_type": "code", "source_file": "auth.go"})
	g.AddNode("café", map[string]any{"label": "Café", "file_type": "code", "source_file": "cafe.go"})

	// Server is highly connected (hub).
	g.AddEdge("server", "new_server", nil)
	g.AddEdge("server", "start", nil)
	g.AddEdge("server", "handler", nil)
	g.AddEdge("server", "client", nil)
	g.AddEdge("server", "auth", nil)
	g.AddEdge("client", "handler", nil)
	return g
}

func TestScoreNodes_ExactMatch(t *testing.T) {
	g := makeTestGraph()
	results := ScoreNodes(g, "Server")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].ID != "server" {
		t.Errorf("expected server as top result, got %s", results[0].ID)
	}
}

func TestScoreNodes_PrefixMatch(t *testing.T) {
	g := makeTestGraph()
	results := ScoreNodes(g, "hand")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	found := false
	for _, r := range results {
		if r.ID == "handler" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected handler in results")
	}
}

func TestScoreNodes_FuzzyMatch(t *testing.T) {
	g := makeTestGraph()
	// "servr" is 1 edit from "server"
	results := ScoreNodes(g, "servr")
	if len(results) == 0 {
		t.Fatal("expected fuzzy results for 'servr'")
	}
	found := false
	for _, r := range results {
		if r.ID == "server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected server via fuzzy match")
	}
}

func TestScoreNodes_DegreeRanking(t *testing.T) {
	g := makeTestGraph()
	// Both "server" and "client" contain "l" in source file... but let's
	// test that when scores are similar, higher-degree nodes rank first.
	results := ScoreNodes(g, "server")
	if len(results) == 0 {
		t.Fatal("no results")
	}
	// "server" node has degree 5, should be top.
	if results[0].ID != "server" {
		t.Errorf("expected server (degree 5) as top, got %s", results[0].ID)
	}
}

func TestScoreNodes_SourceFile(t *testing.T) {
	g := makeTestGraph()
	results := ScoreNodes(g, "auth")
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// "auth" matches both label "Authenticate()" and source_file "auth.go"
	found := false
	for _, r := range results {
		if r.ID == "auth" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected auth in results")
	}
}

func TestFindNode_Exact(t *testing.T) {
	g := makeTestGraph()
	id := FindNode(g, "Server")
	if id != "server" {
		t.Errorf("expected server, got %q", id)
	}
}

func TestFindNode_Diacritics(t *testing.T) {
	g := makeTestGraph()
	id := FindNode(g, "Cafe") // no accent
	if id != "café" {
		t.Errorf("expected café, got %q", id)
	}
}

func TestFindNode_Fuzzy(t *testing.T) {
	g := makeTestGraph()
	id := FindNode(g, "Clent") // 1 edit from Client
	if id != "client" {
		t.Errorf("expected client via fuzzy, got %q", id)
	}
}

func TestNormalizeLabel(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Café", "cafe"},
		{"Server", "server"},
		{"über", "uber"},
		{"naïve", "naive"},
	}
	for _, tt := range tests {
		got := NormalizeLabel(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"server", "server", 0},
		{"server", "servr", 1},
		{"server", "servre", 2},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
