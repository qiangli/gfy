package graph

import (
	"encoding/json"
	"testing"
)

func TestAddNodeAndEdge(t *testing.T) {
	g := New(false)
	g.AddNode("a", map[string]any{"label": "A"})
	g.AddNode("b", map[string]any{"label": "B"})
	g.AddEdge("a", "b", map[string]any{"relation": "calls"})

	if g.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 1 {
		t.Fatalf("expected 1 edge, got %d", g.EdgeCount())
	}
	if g.Degree("a") != 1 {
		t.Fatalf("expected degree 1 for a, got %d", g.Degree("a"))
	}
	neighbors := g.Neighbors("a")
	if len(neighbors) != 1 || neighbors[0] != "b" {
		t.Fatalf("expected neighbors [b], got %v", neighbors)
	}
	// Undirected: b also sees a.
	neighbors = g.Neighbors("b")
	if len(neighbors) != 1 || neighbors[0] != "a" {
		t.Fatalf("expected neighbors [a], got %v", neighbors)
	}
}

func TestDirectedGraph(t *testing.T) {
	g := New(true)
	g.AddEdge("a", "b", map[string]any{"relation": "imports"})

	if !g.IsDirected() {
		t.Fatal("expected directed graph")
	}
	if len(g.Neighbors("a")) != 1 {
		t.Fatal("a should have 1 neighbor")
	}
	if len(g.Neighbors("b")) != 0 {
		t.Fatal("b should have 0 neighbors in directed graph")
	}
}

func TestRemoveNode(t *testing.T) {
	g := New(false)
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "c", nil)
	g.RemoveNode("b")

	if g.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes after removal, got %d", g.NodeCount())
	}
	if g.EdgeCount() != 0 {
		t.Fatalf("expected 0 edges after removal, got %d", g.EdgeCount())
	}
}

func TestSubgraph(t *testing.T) {
	g := New(false)
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "c", nil)
	g.AddEdge("a", "c", nil)

	sub := g.Subgraph([]string{"a", "b"})
	if sub.NodeCount() != 2 {
		t.Fatalf("expected 2 nodes, got %d", sub.NodeCount())
	}
	if sub.EdgeCount() != 1 {
		t.Fatalf("expected 1 edge, got %d", sub.EdgeCount())
	}
}

func TestShortestPath(t *testing.T) {
	g := New(false)
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "c", nil)
	g.AddEdge("c", "d", nil)

	path := g.ShortestPath("a", "d", 0)
	if len(path) != 4 {
		t.Fatalf("expected path length 4, got %d: %v", len(path), path)
	}

	path = g.ShortestPath("a", "d", 2)
	if path != nil {
		t.Fatalf("expected nil with maxHops=2, got %v", path)
	}

	path = g.ShortestPath("a", "d", 3)
	if len(path) != 4 {
		t.Fatalf("expected path length 4 with maxHops=3, got %d", len(path))
	}
}

func TestBFS(t *testing.T) {
	g := New(false)
	g.AddEdge("a", "b", nil)
	g.AddEdge("b", "c", nil)
	g.AddEdge("c", "d", nil)

	visited, _ := g.BFS([]string{"a"}, 1)
	if len(visited) != 2 {
		t.Fatalf("expected 2 visited at depth 1, got %d: %v", len(visited), visited)
	}

	visited, _ = g.BFS([]string{"a"}, 3)
	if len(visited) != 4 {
		t.Fatalf("expected 4 visited at depth 3, got %d: %v", len(visited), visited)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	g := New(false)
	g.AddNode("a", map[string]any{"label": "ClassA", "file_type": "code"})
	g.AddNode("b", map[string]any{"label": "ClassB", "file_type": "code"})
	g.AddEdge("a", "b", map[string]any{"relation": "imports", "confidence": "EXTRACTED"})

	data, err := g.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	// Verify it parses as valid JSON with expected structure.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if raw["directed"] != false {
		t.Error("expected directed=false")
	}
	nodes := raw["nodes"].([]any)
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes in JSON, got %d", len(nodes))
	}
	links := raw["links"].([]any)
	if len(links) != 1 {
		t.Errorf("expected 1 link in JSON, got %d", len(links))
	}

	// Round-trip.
	g2, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	if g2.NodeCount() != g.NodeCount() {
		t.Errorf("node count mismatch: %d != %d", g2.NodeCount(), g.NodeCount())
	}
	if g2.EdgeCount() != g.EdgeCount() {
		t.Errorf("edge count mismatch: %d != %d", g2.EdgeCount(), g.EdgeCount())
	}
	if g2.NodeAttrs("a")["label"] != "ClassA" {
		t.Errorf("expected label ClassA, got %v", g2.NodeAttrs("a")["label"])
	}
}

func TestToUndirected(t *testing.T) {
	g := New(true)
	g.AddEdge("a", "b", nil)
	g.AddEdge("c", "b", nil)

	u := g.ToUndirected()
	if u.IsDirected() {
		t.Fatal("expected undirected")
	}
	if u.EdgeCount() != 2 {
		t.Fatalf("expected 2 edges, got %d", u.EdgeCount())
	}
	// In undirected, b should see both a and c.
	if len(u.Neighbors("b")) != 2 {
		t.Fatalf("expected 2 neighbors for b, got %d", len(u.Neighbors("b")))
	}
}

func TestNodeOverwrite(t *testing.T) {
	g := New(false)
	g.AddNode("a", map[string]any{"label": "old"})
	g.AddNode("a", map[string]any{"label": "new"})

	if g.NodeCount() != 1 {
		t.Fatalf("expected 1 node, got %d", g.NodeCount())
	}
	if g.NodeAttrs("a")["label"] != "new" {
		t.Errorf("expected label 'new', got %v", g.NodeAttrs("a")["label"])
	}
}
