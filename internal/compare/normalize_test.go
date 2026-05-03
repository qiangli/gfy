package compare

import (
	"testing"

	"github.com/qiangli/gfy/internal/graph"
)

// buildCodeGraph creates a small code-like graph with file, functions, and calls.
func buildCodeGraph(fileLabel string, funcs []string, calls [][2]int) *graph.Graph {
	g := graph.New(false)

	// File node.
	fileID := "file_" + fileLabel
	g.AddNode(fileID, map[string]any{"label": fileLabel, "file_type": "code"})

	// Function nodes.
	funcIDs := make([]string, len(funcs))
	for i, name := range funcs {
		fid := fileID + "_" + name
		funcIDs[i] = fid
		g.AddNode(fid, map[string]any{"label": name + "()", "file_type": "code"})
		g.AddEdge(fileID, fid, map[string]any{
			"relation": "contains", "_src": fileID, "_tgt": fid,
		})
	}

	// Call edges.
	for _, call := range calls {
		src, tgt := funcIDs[call[0]], funcIDs[call[1]]
		g.AddEdge(src, tgt, map[string]any{
			"relation": "calls", "_src": src, "_tgt": tgt,
		})
	}

	return g
}

func TestInferNodeType(t *testing.T) {
	tests := []struct {
		label string
		in    []dirEdge
		out   []dirEdge
		want  string
	}{
		{
			label: "main.go",
			out:   []dirEdge{{"f1", "contains"}, {"imp1", "imports"}},
			want:  "file",
		},
		{
			label: "funcA()",
			in:    []dirEdge{{"file1", "contains"}},
			out:   []dirEdge{{"funcB", "calls"}},
			want:  "function",
		},
		{
			label: ".methodA()",
			in:    []dirEdge{{"ClassA", "method"}},
			want:  "method",
		},
		{
			label: "MyClass",
			out:   []dirEdge{{"m1", "method"}, {"m2", "method"}},
			want:  "class",
		},
		{
			label: "fmt",
			in:    []dirEdge{{"file1", "imports"}},
			want:  "import",
		},
	}

	for _, tt := range tests {
		got := inferNodeType(tt.label, tt.in, tt.out)
		if got != tt.want {
			t.Errorf("inferNodeType(%q) = %q, want %q", tt.label, got, tt.want)
		}
	}
}

func TestCanonicalTokens(t *testing.T) {
	tests := []struct {
		label string
		want  []string
	}{
		{"MY_CONST", []string{"const", "my"}},
		{"your_constant", []string{"constant", "your"}},
		{"calculateTotal", []string{"calculate", "total"}},
		{".processRequest()", []string{"process", "request"}},
		{"handleHTTPRequest", []string{"handle", "http", "request"}},
	}

	for _, tt := range tests {
		got := canonicalTokens(tt.label)
		if len(got) != len(tt.want) {
			t.Errorf("canonicalTokens(%q) = %v, want %v", tt.label, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("canonicalTokens(%q)[%d] = %q, want %q", tt.label, i, got[i], tt.want[i])
			}
		}
	}
}

func TestNormalizedLabelSimilarity(t *testing.T) {
	// "MY_CONST" vs "your_constant" have no token overlap after canonicalization:
	// ["const", "my"] vs ["constant", "your"]. This is expected — label similarity
	// is intentionally a weak signal (20% weight). The structural fingerprint
	// handles the matching instead.
	sim := normalizedLabelSimilarity("MY_CONST", "your_constant")
	if sim != 0.0 {
		t.Errorf("expected 0.0 for MY_CONST vs your_constant (no shared tokens), got %f", sim)
	}

	// Labels that share tokens after splitting should have similarity.
	sim = normalizedLabelSimilarity("processRequest", "handleRequest")
	if sim <= 0 {
		t.Errorf("expected some similarity for processRequest vs handleRequest, got %f", sim)
	}

	// Identical labels should have similarity 1.
	sim = normalizedLabelSimilarity("calculateTotal", "calculateTotal")
	if sim != 1.0 {
		t.Errorf("expected 1.0 for identical labels, got %f", sim)
	}

	// Completely different labels should have similarity 0.
	sim = normalizedLabelSimilarity("foo", "bar")
	if sim != 0.0 {
		t.Errorf("expected 0.0 for completely different labels, got %f", sim)
	}
}

func TestBuildSignatures(t *testing.T) {
	g := buildCodeGraph("main.go", []string{"funcA", "funcB", "funcC"}, [][2]int{{0, 1}, {0, 2}})

	sigs := BuildSignatures(g)
	if len(sigs) != 4 { // 1 file + 3 functions
		t.Fatalf("expected 4 signatures, got %d", len(sigs))
	}

	// The file node should be typed as "file".
	fileSig := sigs["file_main.go"]
	if fileSig == nil {
		t.Fatal("missing signature for file node")
	}
	if fileSig.NodeType != "file" {
		t.Errorf("expected file type 'file', got %q", fileSig.NodeType)
	}

	// funcA calls funcB and funcC, so it should have arity 2.
	funcASig := sigs["file_main.go_funcA"]
	if funcASig == nil {
		t.Fatal("missing signature for funcA")
	}
	if funcASig.NodeType != "function" {
		t.Errorf("expected function type 'function', got %q", funcASig.NodeType)
	}
	if funcASig.Arity != 2 {
		t.Errorf("expected arity 2, got %d", funcASig.Arity)
	}
}

func TestAlignGraphs_IdenticalStructure_DifferentNames(t *testing.T) {
	// Two graphs with identical structure but completely different names.
	// Graph A: main.go → funcA() calls funcB()
	// Graph B: app.go  → handler() calls helper()
	a := buildCodeGraph("main.go", []string{"funcA", "funcB"}, [][2]int{{0, 1}})
	b := buildCodeGraph("app.go", []string{"handler", "helper"}, [][2]int{{0, 1}})

	alignment := AlignGraphs(a, b, NormalizeOptions{MinMatchScore: 0.3})

	if alignment.MatchedCount == 0 {
		t.Fatal("expected at least some matches for structurally identical graphs")
	}

	// The file nodes should match each other.
	if matched, ok := alignment.Matched["file_main.go"]; ok {
		if matched != "file_app.go" {
			t.Errorf("expected file_main.go → file_app.go, got → %s", matched)
		}
	}

	t.Logf("Matched %d pairs, avg score: %.2f", alignment.MatchedCount, alignment.AvgScore)
	for idA, idB := range alignment.Matched {
		t.Logf("  %s → %s (score: %.2f)", idA, idB, alignment.Scores[idA])
	}
}

func TestAlignGraphs_SameConstantDifferentName(t *testing.T) {
	// Simulate: MY_CONST = 3.14 vs your_constant = 3.14
	// Both are entities contained by a file with same structural role.
	a := graph.New(false)
	a.AddNode("file_a", map[string]any{"label": "config.go", "file_type": "code"})
	a.AddNode("const_a", map[string]any{"label": "MY_CONST", "file_type": "code", "tags": []any{}})
	a.AddEdge("file_a", "const_a", map[string]any{"relation": "contains", "_src": "file_a", "_tgt": "const_a"})

	b := graph.New(false)
	b.AddNode("file_b", map[string]any{"label": "settings.go", "file_type": "code"})
	b.AddNode("const_b", map[string]any{"label": "your_constant", "file_type": "code", "tags": []any{}})
	b.AddEdge("file_b", "const_b", map[string]any{"relation": "contains", "_src": "file_b", "_tgt": "const_b"})

	alignment := AlignGraphs(a, b, NormalizeOptions{MinMatchScore: 0.3})

	// The two constants should be matched.
	if alignment.MatchedCount < 1 {
		t.Errorf("expected at least 1 match, got %d", alignment.MatchedCount)
	}
	t.Logf("Matched %d pairs, avg score: %.2f", alignment.MatchedCount, alignment.AvgScore)
	for idA, idB := range alignment.Matched {
		t.Logf("  %s → %s (score: %.2f)", idA, idB, alignment.Scores[idA])
	}
}

func TestCompareWithNormalize(t *testing.T) {
	// Two structurally identical graphs with different names.
	a := buildCodeGraph("main.go", []string{"processData", "validateInput", "saveResult"}, [][2]int{{0, 1}, {0, 2}})
	b := buildCodeGraph("app.go", []string{"handleData", "checkInput", "storeResult"}, [][2]int{{0, 1}, {0, 2}})

	// Without normalize: should see high difference (all nodes different by ID).
	resultRaw := Compare(a, b, "v1", "v2", Options{SkipCommunities: true, SkipImpact: true})
	t.Logf("Without normalize: Jaccard=%.2f, added=%d, removed=%d",
		resultRaw.Similarity.NodeJaccard, resultRaw.Summary.NodesAdded, resultRaw.Summary.NodesRemoved)

	// With normalize: should see much higher similarity.
	resultNorm := Compare(a, b, "v1", "v2", Options{
		SkipCommunities:  true,
		SkipImpact:       true,
		Normalize:        true,
		NormalizeOptions: NormalizeOptions{MinMatchScore: 0.3},
	})
	t.Logf("With normalize: Jaccard=%.2f, added=%d, removed=%d, matched=%d",
		resultNorm.Similarity.NodeJaccard, resultNorm.Summary.NodesAdded, resultNorm.Summary.NodesRemoved,
		resultNorm.Alignment.MatchedCount)

	// Normalized comparison should show fewer differences.
	if resultNorm.Summary.NodesAdded >= resultRaw.Summary.NodesAdded {
		t.Errorf("expected fewer added nodes with normalization: raw=%d, norm=%d",
			resultRaw.Summary.NodesAdded, resultNorm.Summary.NodesAdded)
	}
	if resultNorm.Similarity.NodeJaccard <= resultRaw.Similarity.NodeJaccard {
		t.Errorf("expected higher Jaccard with normalization: raw=%.2f, norm=%.2f",
			resultRaw.Similarity.NodeJaccard, resultNorm.Similarity.NodeJaccard)
	}
}

func TestWLRefine(t *testing.T) {
	g := buildCodeGraph("main.go", []string{"a", "b", "c"}, [][2]int{{0, 1}, {1, 2}})
	sigs := BuildSignatures(g)

	// Record hashes before WL.
	hashBefore := make(map[string]string)
	for id, sig := range sigs {
		hashBefore[id] = sig.Hash
	}

	WLRefine(g, sigs, 3)

	// Hashes should change after WL refinement.
	changed := 0
	for id, sig := range sigs {
		if sig.Hash != hashBefore[id] {
			changed++
		}
	}
	if changed == 0 {
		t.Error("expected WL refinement to change at least some hashes")
	}

	// Nodes with different neighborhoods should get different hashes.
	// "a" calls "b"; "b" calls "c"; "c" calls nothing — all different neighborhoods.
	if sigs["file_main.go_a"].Hash == sigs["file_main.go_c"].Hash {
		t.Error("expected different WL hashes for nodes a and c (different call patterns)")
	}
}

func TestReKeyGraph(t *testing.T) {
	b := graph.New(false)
	b.AddNode("x1", map[string]any{"label": "funcX"})
	b.AddNode("x2", map[string]any{"label": "funcY"})
	b.AddEdge("x1", "x2", map[string]any{"relation": "calls"})

	alignment := &AlignmentResult{
		Matched: map[string]string{
			"a1": "x1", // Map A's a1 → B's x1
			"a2": "x2", // Map A's a2 → B's x2
		},
	}

	rekeyed := ReKeyGraph(b, alignment)

	// x1 should now be a1, x2 should now be a2.
	if !rekeyed.HasNode("a1") {
		t.Error("expected rekeyed graph to have node a1")
	}
	if !rekeyed.HasNode("a2") {
		t.Error("expected rekeyed graph to have node a2")
	}
	if rekeyed.HasNode("x1") {
		t.Error("expected x1 to be rekeyed to a1")
	}
}
