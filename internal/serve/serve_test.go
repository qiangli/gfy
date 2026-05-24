package serve

import (
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qiangli/gfy/internal/trace"
	"github.com/qiangli/gfy/pkg/analyze"
	"github.com/qiangli/gfy/pkg/cluster"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
)

// buildTestGraph creates a realistic knowledge graph for testing MCP tools.
// Structure:
//
//	main.go (file)
//	  ├── contains → main() [throws]
//	  ├── contains → Server [class-like]
//	  │   ├── method → .Start() [net, async]
//	  │   └── method → .Stop() [logs]
//	  └── imports → fmt
//
//	handler.go (file)
//	  ├── contains → HandleRequest() [net, logs]
//	  └── contains → ValidateInput() [throws]
//
//	calls: main() → Server, .Start() → HandleRequest(), HandleRequest() → ValidateInput()
//	cross-file inferred: .Start() → HandleRequest()
func buildTestGraph() (*graph.Graph, map[int][]string) {
	g := graph.New(false)

	// File nodes
	g.AddNode("main_go", map[string]any{
		"label": "main.go", "file_type": "code", "source_file": "main.go",
	})
	g.AddNode("handler_go", map[string]any{
		"label": "handler.go", "file_type": "code", "source_file": "handler.go",
	})

	// Function/class nodes in main.go
	g.AddNode("main_main", map[string]any{
		"label": "main()", "file_type": "code", "source_file": "main.go",
		"source_location": "L10", "tags": []any{"throws"}, "comment": "Entry point",
	})
	g.AddNode("main_server", map[string]any{
		"label": "Server", "file_type": "code", "source_file": "main.go",
		"source_location": "L20",
	})
	g.AddNode("main_server_start", map[string]any{
		"label": ".Start()", "file_type": "code", "source_file": "main.go",
		"source_location": "L30", "tags": []any{"net", "async"},
	})
	g.AddNode("main_server_stop", map[string]any{
		"label": ".Stop()", "file_type": "code", "source_file": "main.go",
		"source_location": "L50", "tags": []any{"logs"},
		"log_messages": []any{"server shutting down"},
	})

	// Function nodes in handler.go
	g.AddNode("handler_handlerequest", map[string]any{
		"label": "HandleRequest()", "file_type": "code", "source_file": "handler.go",
		"source_location": "L5", "tags": []any{"net", "logs"},
	})
	g.AddNode("handler_validateinput", map[string]any{
		"label": "ValidateInput()", "file_type": "code", "source_file": "handler.go",
		"source_location": "L40", "tags": []any{"throws"},
	})

	// Import node
	g.AddNode("go_pkg_fmt", map[string]any{
		"label": "fmt", "file_type": "code", "source_file": "",
	})

	// Containment edges
	addEdge(g, "main_go", "main_main", "contains")
	addEdge(g, "main_go", "main_server", "contains")
	addEdge(g, "handler_go", "handler_handlerequest", "contains")
	addEdge(g, "handler_go", "handler_validateinput", "contains")

	// Method edges
	addEdge(g, "main_server", "main_server_start", "method")
	addEdge(g, "main_server", "main_server_stop", "method")

	// Import edges
	addEdge(g, "main_go", "go_pkg_fmt", "imports")

	// Call edges
	addCallEdge(g, "main_main", "main_server", "main.go")
	addCallEdge(g, "main_server_start", "handler_handlerequest", "main.go")
	addCallEdge(g, "handler_handlerequest", "handler_validateinput", "handler.go")

	// Cross-file inferred edge
	g.AddEdge("main_server_start", "handler_handlerequest", map[string]any{
		"relation": "calls", "confidence": "INFERRED", "weight": 1.0,
		"_src": "main_server_start", "_tgt": "handler_handlerequest",
		"source_file": "main.go",
	})

	// Ambiguous edge for analysis
	g.AddEdge("handler_validateinput", "go_pkg_fmt", map[string]any{
		"relation": "references", "confidence": "AMBIGUOUS", "weight": 0.5,
		"_src": "handler_validateinput", "_tgt": "go_pkg_fmt",
		"source_file": "handler.go",
	})

	communities := map[int][]string{
		0: {"main_go", "main_main", "main_server", "main_server_start", "main_server_stop", "go_pkg_fmt"},
		1: {"handler_go", "handler_handlerequest", "handler_validateinput"},
	}

	return g, communities
}

func addEdge(g *graph.Graph, src, tgt, relation string) {
	g.AddEdge(src, tgt, map[string]any{
		"relation": relation, "confidence": "EXTRACTED", "weight": 1.0,
		"_src": src, "_tgt": tgt,
	})
}

func addCallEdge(g *graph.Graph, src, tgt, file string) {
	g.AddEdge(src, tgt, map[string]any{
		"relation": "calls", "confidence": "EXTRACTED", "weight": 1.0,
		"_src": src, "_tgt": tgt, "source_file": file,
	})
}

// --- Overview tests ---

func TestGraphStats(t *testing.T) {
	g, communities := buildTestGraph()
	children, parents := buildContainmentMaps(g)
	_ = children
	_ = parents

	// Verify the graph has expected structure.
	if g.NodeCount() != 9 {
		t.Fatalf("expected 9 nodes, got %d", g.NodeCount())
	}

	// Test graph_stats output contains key info.
	result := callGraphStats(g, communities)
	for _, want := range []string{"Nodes:", "Edges:", "Communities:", "EXTRACTED:", "INFERRED:", "AMBIGUOUS:", "Relations:", "contains:", "calls:", "method:", "imports:"} {
		if !strings.Contains(result, want) {
			t.Errorf("graph_stats missing %q in output:\n%s", want, result)
		}
	}
	if !strings.Contains(result, "Behavioral tags:") {
		t.Errorf("graph_stats missing behavioral tags section")
	}
}

// --- Navigation tests ---

func TestGetNode(t *testing.T) {
	g, _ := buildTestGraph()
	children, parents := buildContainmentMaps(g)

	result := formatNode(g, "main_main", children, parents)
	for _, want := range []string{"main()", "code", "main.go", "L10", "throws", "Entry point", "Parent:"} {
		if !strings.Contains(result, want) {
			t.Errorf("get_node missing %q in output:\n%s", want, result)
		}
	}
}

func TestGetChildren(t *testing.T) {
	g, _ := buildTestGraph()
	children, _ := buildContainmentMaps(g)

	// main.go should have 2 contains children: main() and Server
	kids := children["main_go"]
	if len(kids) != 2 {
		t.Fatalf("expected 2 children of main_go, got %d", len(kids))
	}

	// Server should have 2 method children: .Start() and .Stop()
	serverKids := children["main_server"]
	if len(serverKids) != 2 {
		t.Fatalf("expected 2 method children of Server, got %d", len(serverKids))
	}
	for _, c := range serverKids {
		if c.relation != "method" {
			t.Errorf("expected method relation, got %s", c.relation)
		}
	}
}

func TestListRoots(t *testing.T) {
	g, _ := buildTestGraph()
	children, parents := buildContainmentMaps(g)

	var roots []string
	for _, id := range g.Nodes() {
		if _, hasParent := parents[id]; !hasParent {
			if kids := children[id]; len(kids) > 0 {
				roots = append(roots, id)
			}
		}
	}
	if len(roots) != 2 {
		t.Fatalf("expected 2 root nodes (main.go, handler.go), got %d: %v", len(roots), roots)
	}
}

func TestListLeaves(t *testing.T) {
	g, _ := buildTestGraph()
	children, parents := buildContainmentMaps(g)

	var leaves []string
	for _, id := range g.Nodes() {
		if _, isParent := children[id]; isParent {
			continue
		}
		if _, isChild := parents[id]; !isChild {
			continue
		}
		leaves = append(leaves, id)
	}
	// Leaves should be: main(), .Start(), .Stop(), HandleRequest(), ValidateInput()
	if len(leaves) != 5 {
		t.Fatalf("expected 5 leaf nodes, got %d: %v", len(leaves), leaves)
	}
}

func TestListLeavesWithTagFilter(t *testing.T) {
	g, _ := buildTestGraph()
	children, parents := buildContainmentMaps(g)

	var throwLeaves []string
	for _, id := range g.Nodes() {
		if _, isParent := children[id]; isParent {
			continue
		}
		if _, isChild := parents[id]; !isChild {
			continue
		}
		if hasTag(g.NodeAttrs(id), "throws") {
			throwLeaves = append(throwLeaves, id)
		}
	}
	// main() and ValidateInput() have "throws"
	if len(throwLeaves) != 2 {
		t.Fatalf("expected 2 throw leaves, got %d: %v", len(throwLeaves), throwLeaves)
	}
}

func TestListNodes(t *testing.T) {
	g, _ := buildTestGraph()

	// Filter by source file
	var handlerNodes []string
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		if strings.Contains(attrStr(attrs, "source_file"), "handler.go") {
			handlerNodes = append(handlerNodes, id)
		}
	}
	// handler.go file node + HandleRequest() + ValidateInput() = 3
	if len(handlerNodes) != 3 {
		t.Fatalf("expected 3 handler.go nodes, got %d", len(handlerNodes))
	}

	// Filter by tag
	var netNodes []string
	for _, id := range g.Nodes() {
		if hasTag(g.NodeAttrs(id), "net") {
			netNodes = append(netNodes, id)
		}
	}
	// .Start() and HandleRequest() have "net"
	if len(netNodes) != 2 {
		t.Fatalf("expected 2 net-tagged nodes, got %d", len(netNodes))
	}
}

func TestShortestPath(t *testing.T) {
	g, _ := buildTestGraph()

	path := g.ShortestPath("main_main", "handler_validateinput", 0)
	if path == nil {
		t.Fatal("expected path from main() to ValidateInput()")
	}
	if len(path) < 2 {
		t.Fatalf("expected path length >= 2, got %d", len(path))
	}
}

// --- Search tests ---

func TestSearch(t *testing.T) {
	g, _ := buildTestGraph()

	results := search.ScoreNodes(g, "Server")
	if len(results) == 0 {
		t.Fatal("expected search results for 'Server'")
	}
	if results[0].ID != "main_server" {
		t.Errorf("expected top result to be main_server, got %s", results[0].ID)
	}
}

func TestGetSubgraph(t *testing.T) {
	g, _ := buildTestGraph()

	sub := g.Subgraph([]string{"main_server", "main_server_start", "main_server_stop"})
	if sub.NodeCount() != 3 {
		t.Fatalf("expected 3 nodes in subgraph, got %d", sub.NodeCount())
	}
	if sub.EdgeCount() < 2 {
		t.Fatalf("expected at least 2 edges in subgraph, got %d", sub.EdgeCount())
	}
}

// --- Analysis tests ---

func TestGodNodes(t *testing.T) {
	g, _ := buildTestGraph()

	// Exercise via the analyze package directly since we can't call MCP tools directly.
	gods := analyze.GodNodes(g, 3)
	if len(gods) == 0 {
		t.Fatal("expected god nodes")
	}
	// The most connected nodes should be the ones with most edges.
	if gods[0].Degree < 2 {
		t.Errorf("expected top god node degree >= 2, got %d", gods[0].Degree)
	}
}

func TestSurprisingConnections(t *testing.T) {
	g, communities := buildTestGraph()

	conns := analyze.SurprisingConnections(g, communities, 5)
	if len(conns) == 0 {
		t.Fatal("expected surprising connections")
	}
	// The AMBIGUOUS edge should rank high.
	foundAmbiguous := false
	for _, c := range conns {
		if c.Confidence == "AMBIGUOUS" {
			foundAmbiguous = true
			break
		}
	}
	if !foundAmbiguous {
		t.Error("expected AMBIGUOUS edge in surprising connections")
	}
}

func TestSuggestQuestions(t *testing.T) {
	g, communities := buildTestGraph()

	questions := analyze.SuggestQuestions(g, communities, 5)
	if len(questions) == 0 {
		t.Fatal("expected suggested questions")
	}
	// Should find the ambiguous edge.
	foundAmbiguous := false
	for _, q := range questions {
		if q.Type == "ambiguous_edge" {
			foundAmbiguous = true
			break
		}
	}
	if !foundAmbiguous {
		t.Error("expected ambiguous_edge question type")
	}
}

func TestCommunityInfo(t *testing.T) {
	g, communities := buildTestGraph()

	scores := cluster.ScoreAll(g, communities)
	if len(scores) != 2 {
		t.Fatalf("expected 2 community scores, got %d", len(scores))
	}
	for cid, score := range scores {
		if score < 0 || score > 1 {
			t.Errorf("community %d cohesion %.2f out of range [0,1]", cid, score)
		}
	}
}

func TestTraceCalls(t *testing.T) {
	g, _ := buildTestGraph()

	chains := trace.TraceTag(g, "throws", 10, 20)
	if len(chains) == 0 {
		t.Fatal("expected call chains for 'throws' tag")
	}
	// Each chain should end at a throws-tagged node.
	for i, chain := range chains {
		if chain.Tag != "throws" {
			t.Errorf("chain %d: expected tag 'throws', got %q", i, chain.Tag)
		}
	}
}

// --- Containment map tests ---

func TestBuildContainmentMaps(t *testing.T) {
	g, _ := buildTestGraph()
	children, parents := buildContainmentMaps(g)

	// main_go should have children via contains
	if len(children["main_go"]) != 2 {
		t.Errorf("expected 2 children of main_go, got %d", len(children["main_go"]))
	}

	// main_server should have children via method
	if len(children["main_server"]) != 2 {
		t.Errorf("expected 2 children of main_server, got %d", len(children["main_server"]))
	}

	// main_main should have main_go as parent
	if len(parents["main_main"]) != 1 {
		t.Fatalf("expected 1 parent of main_main, got %d", len(parents["main_main"]))
	}
	if parents["main_main"][0].id != "main_go" {
		t.Errorf("expected parent of main_main to be main_go, got %s", parents["main_main"][0].id)
	}

	// main_server_start should have main_server as parent
	if len(parents["main_server_start"]) != 1 {
		t.Fatalf("expected 1 parent of main_server_start, got %d", len(parents["main_server_start"]))
	}
	if parents["main_server_start"][0].id != "main_server" {
		t.Errorf("expected parent of main_server_start to be main_server, got %s", parents["main_server_start"][0].id)
	}

	// go_pkg_fmt should have no children and no containment parent
	if len(children["go_pkg_fmt"]) != 0 {
		t.Errorf("expected 0 children of go_pkg_fmt, got %d", len(children["go_pkg_fmt"]))
	}
	if len(parents["go_pkg_fmt"]) != 0 {
		t.Errorf("expected 0 parents of go_pkg_fmt, got %d", len(parents["go_pkg_fmt"]))
	}
}

// --- Helper function tests ---

func TestAttrTags(t *testing.T) {
	// []any tags (from JSON deserialization)
	attrs := map[string]any{"tags": []any{"throws", "logs"}}
	tags := attrTags(attrs)
	if len(tags) != 2 || tags[0] != "throws" || tags[1] != "logs" {
		t.Errorf("expected [throws, logs], got %v", tags)
	}

	// []string tags
	attrs2 := map[string]any{"tags": []string{"net"}}
	tags2 := attrTags(attrs2)
	if len(tags2) != 1 || tags2[0] != "net" {
		t.Errorf("expected [net], got %v", tags2)
	}

	// No tags
	if got := attrTags(map[string]any{}); got != nil {
		t.Errorf("expected nil tags, got %v", got)
	}
}

func TestHasTag(t *testing.T) {
	attrs := map[string]any{"tags": []any{"throws", "logs"}}
	if !hasTag(attrs, "throws") {
		t.Error("expected hasTag to find 'throws'")
	}
	if hasTag(attrs, "net") {
		t.Error("expected hasTag not to find 'net'")
	}
}

func TestSubgraphToText(t *testing.T) {
	g, _ := buildTestGraph()
	sub := g.Subgraph([]string{"main_server", "main_server_start"})
	text := subgraphToText(sub, sub.Nodes(), sub.Edges())
	if !strings.Contains(text, "2 nodes") {
		t.Error("expected '2 nodes' in subgraph text")
	}
	if !strings.Contains(text, "Server") {
		t.Error("expected 'Server' in subgraph text")
	}
}

// --- Integration: verify all tools register without panic ---

func TestRegisterAllTools(t *testing.T) {
	g, communities := buildTestGraph()

	// This should not panic — exercises the full registration path.
	server := mcp.NewServer(&mcp.Implementation{
		Name: "gfy-test", Version: "test",
	}, nil)
	registerTools(server, g, communities, Options{})
}

// callGraphStats exercises the graph_stats logic directly for testing.
func callGraphStats(g *graph.Graph, communities map[int][]string) string {
	confCounts := map[string]int{}
	relCounts := map[string]int{}
	for _, e := range g.Edges() {
		confCounts[attrStr(e.Attrs, "confidence")]++
		relCounts[attrStr(e.Attrs, "relation")]++
	}
	tagCounts := map[string]int{}
	for _, id := range g.Nodes() {
		for _, tag := range attrTags(g.NodeAttrs(id)) {
			tagCounts[tag]++
		}
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("=", 40) + "\n")
	fmt.Fprintf(&b, "Nodes: %d\nEdges: %d\nCommunities: %d\n", g.NodeCount(), g.EdgeCount(), len(communities))
	fmt.Fprintf(&b, "EXTRACTED: %d\nINFERRED: %d\nAMBIGUOUS: %d\n",
		confCounts["EXTRACTED"], confCounts["INFERRED"], confCounts["AMBIGUOUS"])
	fmt.Fprintf(&b, "\nRelations:\n")
	for k, v := range relCounts {
		fmt.Fprintf(&b, "  %s: %d\n", k, v)
	}
	if len(tagCounts) > 0 {
		fmt.Fprintf(&b, "\nBehavioral tags:\n")
		for k, v := range tagCounts {
			fmt.Fprintf(&b, "  %s: %d\n", k, v)
		}
	}
	return b.String()
}
