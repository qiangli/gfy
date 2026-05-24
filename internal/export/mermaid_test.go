package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/gfy/pkg/graph"
)

// buildCallGraph creates a small graph with `calls` edges and a few mixed
// edges that the Mermaid exporter should ignore.
func buildCallGraph(t *testing.T) (*graph.Graph, map[int][]string, map[int]string) {
	t.Helper()
	g := graph.New(false)

	g.AddNode("main", map[string]any{"label": "main()", "file_type": "code", "source_file": "main.go", "tags": []string{"throws"}})
	g.AddNode("server_start", map[string]any{"label": ".Start()", "file_type": "code", "source_file": "server.go", "tags": []string{"net"}})
	g.AddNode("handle_request", map[string]any{"label": "HandleRequest()", "file_type": "code", "source_file": "handler.go", "tags": []string{"net", "logs"}})
	g.AddNode("read_file", map[string]any{"label": "readFile()", "file_type": "code", "source_file": "io.go", "tags": []string{"fs"}})
	g.AddNode("unused", map[string]any{"label": "unused", "file_type": "code"})

	addRel(g, "main", "server_start", "calls", "main.go")
	addRel(g, "server_start", "handle_request", "calls", "server.go")
	addRel(g, "handle_request", "read_file", "calls", "handler.go")

	// Non-call edges (different endpoints so they don't overwrite the calls
	// edges in the undirected adjacency map) — must be filtered out.
	addRel(g, "main", "unused", "imports", "main.go")
	addRel(g, "main", "handle_request", "contains", "main.go")

	communities := map[int][]string{
		0: {"main", "server_start"},
		1: {"handle_request", "read_file"},
	}
	labels := map[int]string{
		0: "Bootstrap",
		1: "Request Path",
	}
	return g, communities, labels
}

func addRel(g *graph.Graph, src, tgt, rel, file string) {
	g.AddEdge(src, tgt, map[string]any{
		"relation": rel, "confidence": "EXTRACTED", "weight": 1.0,
		"_src": src, "_tgt": tgt, "source_file": file,
	})
}

func TestToMermaid_Basic(t *testing.T) {
	g, _, _ := buildCallGraph(t)
	outDir := t.TempDir()
	path := filepath.Join(outDir, "callflow.md")

	if err := ToMermaid(g, nil, nil, path, MermaidOptions{}); err != nil {
		t.Fatalf("ToMermaid: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	for _, want := range []string{
		"```mermaid",
		"flowchart TD",
		"main[\"main()\"]",
		"server_start[\".Start()\"]",
		"handle_request[\"HandleRequest()\"]",
		"read_file[\"readFile()\"]",
		"main --> server_start",
		"server_start --> handle_request",
		"handle_request --> read_file",
		"```",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, out)
		}
	}
	// Unused node has no `calls` edges → should be absent.
	if strings.Contains(out, "unused") {
		t.Errorf("call-flow should not include unused node:\n%s", out)
	}
	// `contains` and `imports` edges should not appear.
	if strings.Contains(out, "-[contains]") || strings.Contains(out, "-[imports]") {
		t.Errorf("call-flow should not include non-calls edges:\n%s", out)
	}
}

func TestToMermaid_DirectionLR(t *testing.T) {
	g, _, _ := buildCallGraph(t)
	path := filepath.Join(t.TempDir(), "callflow.md")
	if err := ToMermaid(g, nil, nil, path, MermaidOptions{Direction: "LR"}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "flowchart LR") {
		t.Errorf("expected flowchart LR")
	}
}

func TestToMermaid_GroupByCommunity(t *testing.T) {
	g, communities, labels := buildCallGraph(t)
	path := filepath.Join(t.TempDir(), "callflow.md")
	if err := ToMermaid(g, communities, labels, path, MermaidOptions{GroupByCommunity: true}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)

	for _, want := range []string{
		"subgraph c0 [\"Bootstrap\"]",
		"subgraph c1 [\"Request Path\"]",
		"end",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\nGot:\n%s", want, s)
		}
	}
}

func TestToMermaid_HighlightTags(t *testing.T) {
	g, communities, labels := buildCallGraph(t)
	path := filepath.Join(t.TempDir(), "callflow.md")
	if err := ToMermaid(g, communities, labels, path, MermaidOptions{HighlightTags: true}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)

	// Nodes with tags should be suffixed with :::<tag>.
	for _, want := range []string{
		":::throws", // main has throws
		":::net",    // server_start, handle_request
		":::fs",     // read_file
		"classDef throws",
		"classDef net",
		"classDef fs",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\nGot:\n%s", want, s)
		}
	}
}

func TestToMermaid_HighlightTags_PriorityOrder(t *testing.T) {
	// A node tagged with both "throws" and "logs" should pick "throws" (higher priority).
	g := graph.New(false)
	g.AddNode("a", map[string]any{"label": "A", "tags": []string{"logs", "throws"}})
	g.AddNode("b", map[string]any{"label": "B"})
	addRel(g, "a", "b", "calls", "x.go")

	path := filepath.Join(t.TempDir(), "callflow.md")
	if err := ToMermaid(g, nil, nil, path, MermaidOptions{HighlightTags: true}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	if !strings.Contains(s, ":::throws") {
		t.Errorf("expected priority pick 'throws', got:\n%s", s)
	}
	if strings.Contains(s, ":::logs") {
		t.Errorf("should pick throws over logs, got:\n%s", s)
	}
}

func TestToMermaid_MaxNodes_PrunesByDegree(t *testing.T) {
	g := graph.New(false)
	// Hub with 5 callees (hub degree 5; callees degree 1).
	g.AddNode("hub", map[string]any{"label": "hub"})
	for i := 0; i < 5; i++ {
		id := "callee_" + string(rune('a'+i))
		g.AddNode(id, map[string]any{"label": id})
		addRel(g, "hub", id, "calls", "x.go")
	}
	// Sub-hub with 2 callees (sub-hub degree 2; callees degree 1).
	g.AddNode("subhub", map[string]any{"label": "subhub"})
	for i := 0; i < 2; i++ {
		id := "subcallee_" + string(rune('a'+i))
		g.AddNode(id, map[string]any{"label": id})
		addRel(g, "subhub", id, "calls", "x.go")
	}

	path := filepath.Join(t.TempDir(), "callflow.md")
	// Keep only the 2 highest-degree nodes — must be hub (5) and subhub (2).
	if err := ToMermaid(g, nil, nil, path, MermaidOptions{MaxNodes: 2}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)

	if !strings.Contains(s, "Graph trimmed") {
		t.Errorf("expected trim notice, got:\n%s", s)
	}
	if !strings.Contains(s, "hub[\"hub\"]") {
		t.Errorf("expected hub (degree 5) to survive trim:\n%s", s)
	}
	if !strings.Contains(s, "subhub[\"subhub\"]") {
		t.Errorf("expected subhub (degree 2) to survive trim:\n%s", s)
	}
	// All degree-1 callees should be dropped.
	for _, leaf := range []string{"callee_a", "callee_b", "subcallee_a"} {
		if strings.Contains(s, leaf+"[") {
			t.Errorf("degree-1 leaf %q leaked through pruning:\n%s", leaf, s)
		}
	}
}

func TestToMermaid_EmptyCallGraph(t *testing.T) {
	g := graph.New(false)
	g.AddNode("solo", map[string]any{"label": "solo"})

	path := filepath.Join(t.TempDir(), "callflow.md")
	if err := ToMermaid(g, nil, nil, path, MermaidOptions{}); err != nil {
		t.Fatalf("ToMermaid: %v", err)
	}
	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "flowchart TD") {
		t.Errorf("expected flowchart header even for empty graph, got:\n%s", out)
	}
	// No `-->` lines.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "-->") {
			t.Errorf("unexpected edge in empty call graph: %q", line)
		}
	}
}

func TestMermaidID_Sanitization(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"main_main", "main_main"},
		{"foo.bar", "foo_bar"},
		{"a/b/c", "a_b_c"},
		{"0starts_digit", "n_0starts_digit"},
		{"", "n_empty"},
		{"hyphen-name", "hyphen_name"},
	}
	for _, tt := range tests {
		if got := mermaidID(tt.in); got != tt.want {
			t.Errorf("mermaidID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMermaidEscapeLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{`quote"inside`, `quote\"inside`},
		{`back\slash`, `back\\slash`},
		{"line1\nline2", "line1 line2"},
		{"cr\rfeed", "cr feed"},
	}
	for _, tt := range tests {
		if got := mermaidEscapeLabel(tt.in); got != tt.want {
			t.Errorf("mermaidEscapeLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToMermaid_DeterministicOutput(t *testing.T) {
	// Map iteration is non-deterministic; the exporter must produce identical
	// output across runs so generated callflow.md files don't churn in git.
	g, communities, labels := buildCallGraph(t)
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.md")
	p2 := filepath.Join(dir, "b.md")

	opts := MermaidOptions{GroupByCommunity: true, HighlightTags: true}
	if err := ToMermaid(g, communities, labels, p1, opts); err != nil {
		t.Fatal(err)
	}
	if err := ToMermaid(g, communities, labels, p2, opts); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(p1)
	b, _ := os.ReadFile(p2)
	if string(a) != string(b) {
		t.Errorf("output is non-deterministic between runs:\n--- run 1 ---\n%s\n--- run 2 ---\n%s", a, b)
	}
}
