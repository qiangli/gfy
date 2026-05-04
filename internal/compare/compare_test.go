package compare

import (
	"math"
	"testing"

	"github.com/qiangli/gfy/pkg/graph"
)

func TestDiffNodes(t *testing.T) {
	a := graph.New(false)
	a.AddNode("f1", map[string]any{"label": "funcA", "file_type": "function", "source_file": "a.go"})
	a.AddNode("f2", map[string]any{"label": "funcB", "file_type": "function", "source_file": "a.go"})
	a.AddNode("f3", map[string]any{"label": "funcC", "file_type": "function", "source_file": "a.go"})

	b := graph.New(false)
	b.AddNode("f1", map[string]any{"label": "funcA", "file_type": "function", "source_file": "a.go"})
	b.AddNode("f2", map[string]any{"label": "funcB_renamed", "file_type": "function", "source_file": "b.go"})
	b.AddNode("f4", map[string]any{"label": "funcD", "file_type": "function", "source_file": "b.go"})

	diff := diffNodes(a, b)

	if len(diff.Added) != 1 || diff.Added[0].ID != "f4" {
		t.Errorf("expected 1 added node (f4), got %v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].ID != "f3" {
		t.Errorf("expected 1 removed node (f3), got %v", diff.Removed)
	}
	if len(diff.Modified) != 1 || diff.Modified[0].ID != "f2" {
		t.Errorf("expected 1 modified node (f2), got %v", diff.Modified)
	}
}

func TestDiffEdges(t *testing.T) {
	a := graph.New(false)
	a.AddNode("f1", map[string]any{"label": "funcA"})
	a.AddNode("f2", map[string]any{"label": "funcB"})
	a.AddNode("f3", map[string]any{"label": "funcC"})
	a.AddEdge("f1", "f2", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})
	a.AddEdge("f2", "f3", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})

	b := graph.New(false)
	b.AddNode("f1", map[string]any{"label": "funcA"})
	b.AddNode("f2", map[string]any{"label": "funcB"})
	b.AddNode("f4", map[string]any{"label": "funcD"})
	b.AddEdge("f1", "f2", map[string]any{"relation": "calls", "confidence": "INFERRED"})
	b.AddEdge("f1", "f4", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})

	diff := diffEdges(a, b)

	if len(diff.Added) != 1 {
		t.Errorf("expected 1 added edge, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 1 {
		t.Errorf("expected 1 removed edge, got %d", len(diff.Removed))
	}
	if len(diff.Modified) != 1 {
		t.Errorf("expected 1 modified edge, got %d", len(diff.Modified))
	}
}

func TestJaccardNodes(t *testing.T) {
	a := graph.New(false)
	a.AddNode("n1", nil)
	a.AddNode("n2", nil)
	a.AddNode("n3", nil)

	b := graph.New(false)
	b.AddNode("n2", nil)
	b.AddNode("n3", nil)
	b.AddNode("n4", nil)

	j := jaccardNodes(a, b)
	// Intersection: {n2, n3} = 2, Union: {n1, n2, n3, n4} = 4
	expected := 0.5
	if math.Abs(j-expected) > 0.001 {
		t.Errorf("expected Jaccard %.2f, got %.2f", expected, j)
	}
}

func TestJaccardIdentical(t *testing.T) {
	a := graph.New(false)
	a.AddNode("n1", nil)
	a.AddNode("n2", nil)

	j := jaccardNodes(a, a)
	if j != 1.0 {
		t.Errorf("expected Jaccard 1.0 for identical graphs, got %.2f", j)
	}
}

func TestJaccardDisjoint(t *testing.T) {
	a := graph.New(false)
	a.AddNode("n1", nil)

	b := graph.New(false)
	b.AddNode("n2", nil)

	j := jaccardNodes(a, b)
	if j != 0.0 {
		t.Errorf("expected Jaccard 0.0 for disjoint graphs, got %.2f", j)
	}
}

func TestJensenShannonDivergence(t *testing.T) {
	// Identical distributions should have JSD = 0.
	p := []float64{0.5, 0.3, 0.2}
	jsd := jensenShannonDivergence(p, p)
	if math.Abs(jsd) > 1e-10 {
		t.Errorf("expected JSD 0 for identical distributions, got %f", jsd)
	}

	// Completely disjoint distributions should have JSD = 1.
	a := []float64{1.0, 0.0}
	b := []float64{0.0, 1.0}
	jsd = jensenShannonDivergence(a, b)
	if math.Abs(jsd-1.0) > 1e-10 {
		t.Errorf("expected JSD 1.0 for disjoint distributions, got %f", jsd)
	}
}

func TestNMI(t *testing.T) {
	// Identical partitions should have NMI = 1.
	labelsA := []int{0, 0, 1, 1, 2, 2}
	labelsB := []int{0, 0, 1, 1, 2, 2}
	nmi := normalizedMutualInformation(labelsA, labelsB)
	if math.Abs(nmi-1.0) > 1e-10 {
		t.Errorf("expected NMI 1.0 for identical partitions, got %f", nmi)
	}

	// Empty should return 0.
	nmi = normalizedMutualInformation(nil, nil)
	if nmi != 0 {
		t.Errorf("expected NMI 0 for empty partitions, got %f", nmi)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"", "hello", 5},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDetectRenames(t *testing.T) {
	a := graph.New(false)
	a.AddNode("f1", map[string]any{"label": "processRequest", "file_type": "function"})
	a.AddNode("f2", map[string]any{"label": "helperA", "file_type": "function"})
	a.AddNode("f3", map[string]any{"label": "utils", "file_type": "function"})
	a.AddEdge("f1", "f2", map[string]any{"relation": "calls"})
	a.AddEdge("f1", "f3", map[string]any{"relation": "calls"})

	b := graph.New(false)
	b.AddNode("f1b", map[string]any{"label": "handleRequest", "file_type": "function"})
	b.AddNode("f2", map[string]any{"label": "helperA", "file_type": "function"})
	b.AddNode("f3", map[string]any{"label": "utils", "file_type": "function"})
	b.AddEdge("f1b", "f2", map[string]any{"relation": "calls"})
	b.AddEdge("f1b", "f3", map[string]any{"relation": "calls"})

	removed := []NodeInfo{{ID: "f1", Label: "processRequest", FileType: "function"}}
	added := []NodeInfo{{ID: "f1b", Label: "handleRequest", FileType: "function"}}

	renames := detectRenames(a, b, removed, added, 0.5, 10)
	if len(renames) != 1 {
		t.Fatalf("expected 1 rename candidate, got %d", len(renames))
	}
	if renames[0].OldLabel != "processRequest" || renames[0].NewLabel != "handleRequest" {
		t.Errorf("unexpected rename: %+v", renames[0])
	}
	if renames[0].Confidence < 0.5 {
		t.Errorf("expected confidence >= 0.5, got %f", renames[0].Confidence)
	}
}

func TestCompare(t *testing.T) {
	a := graph.New(false)
	a.AddNode("f1", map[string]any{"label": "main", "file_type": "function", "source_file": "main.go"})
	a.AddNode("f2", map[string]any{"label": "helper", "file_type": "function", "source_file": "main.go"})
	a.AddNode("f3", map[string]any{"label": "old_func", "file_type": "function", "source_file": "old.go"})
	a.AddEdge("f1", "f2", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})
	a.AddEdge("f1", "f3", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})

	b := graph.New(false)
	b.AddNode("f1", map[string]any{"label": "main", "file_type": "function", "source_file": "main.go"})
	b.AddNode("f2", map[string]any{"label": "helper", "file_type": "function", "source_file": "main.go"})
	b.AddNode("f4", map[string]any{"label": "new_func", "file_type": "function", "source_file": "new.go"})
	b.AddEdge("f1", "f2", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})
	b.AddEdge("f1", "f4", map[string]any{"relation": "calls", "confidence": "EXTRACTED"})

	result := Compare(a, b, "v1", "v2", Options{SkipCommunities: true, SkipImpact: true})

	if result.Summary.NodesA != 3 || result.Summary.NodesB != 3 {
		t.Errorf("unexpected node counts: A=%d, B=%d", result.Summary.NodesA, result.Summary.NodesB)
	}
	if result.Summary.NodesAdded != 1 {
		t.Errorf("expected 1 added node, got %d", result.Summary.NodesAdded)
	}
	if result.Summary.NodesRemoved != 1 {
		t.Errorf("expected 1 removed node, got %d", result.Summary.NodesRemoved)
	}
	if result.Similarity.NodeJaccard < 0.4 || result.Similarity.NodeJaccard > 0.8 {
		t.Errorf("unexpected Jaccard: %f", result.Similarity.NodeJaccard)
	}
}

func TestCompareIdentical(t *testing.T) {
	g := graph.New(false)
	g.AddNode("f1", map[string]any{"label": "main", "file_type": "function"})
	g.AddNode("f2", map[string]any{"label": "helper", "file_type": "function"})
	g.AddEdge("f1", "f2", map[string]any{"relation": "calls"})

	result := Compare(g, g, "same", "same", Options{SkipCommunities: true})

	if result.Similarity.NodeJaccard != 1.0 {
		t.Errorf("expected Jaccard 1.0 for identical graphs, got %f", result.Similarity.NodeJaccard)
	}
	if result.Summary.NodesAdded != 0 || result.Summary.NodesRemoved != 0 {
		t.Errorf("expected no diffs for identical graphs, got +%d/-%d",
			result.Summary.NodesAdded, result.Summary.NodesRemoved)
	}
}

func TestCompareN(t *testing.T) {
	g1 := graph.New(false)
	g1.AddNode("a", map[string]any{"label": "a"})
	g1.AddNode("b", map[string]any{"label": "b"})

	g2 := graph.New(false)
	g2.AddNode("b", map[string]any{"label": "b"})
	g2.AddNode("c", map[string]any{"label": "c"})

	g3 := graph.New(false)
	g3.AddNode("a", map[string]any{"label": "a"})
	g3.AddNode("b", map[string]any{"label": "b"})
	g3.AddNode("c", map[string]any{"label": "c"})

	result := CompareN([]*graph.Graph{g1, g2, g3}, []string{"g1", "g2", "g3"},
		Options{SkipCommunities: true, SkipImpact: true})

	// Core: only "b" is in all three.
	if result.Core.NodeCount != 1 {
		t.Errorf("expected 1 core node, got %d", result.Core.NodeCount)
	}

	// Heatmap diagonal should be 1.0.
	for i := range result.Labels {
		if result.Heatmap[i][i] != 1.0 {
			t.Errorf("expected diagonal 1.0, got %f at [%d][%d]", result.Heatmap[i][i], i, i)
		}
	}
}

func TestEstimateSimilarity(t *testing.T) {
	// Create 3 graphs: g1 ≈ g2 ≈ g3, with g1-g2 very similar and g1-g3 moderately similar.
	g1 := graph.New(false)
	g1.AddNode("a", map[string]any{"label": "a()", "file_type": "code"})
	g1.AddNode("b", map[string]any{"label": "b()", "file_type": "code"})
	g1.AddNode("c", map[string]any{"label": "c()", "file_type": "code"})
	g1.AddEdge("a", "b", map[string]any{"relation": "calls"})
	g1.AddEdge("a", "c", map[string]any{"relation": "calls"})

	g2 := graph.New(false)
	g2.AddNode("a", map[string]any{"label": "a()", "file_type": "code"})
	g2.AddNode("b", map[string]any{"label": "b()", "file_type": "code"})
	g2.AddNode("c", map[string]any{"label": "c()", "file_type": "code"})
	g2.AddNode("d", map[string]any{"label": "d()", "file_type": "code"})
	g2.AddEdge("a", "b", map[string]any{"relation": "calls"})
	g2.AddEdge("a", "c", map[string]any{"relation": "calls"})
	g2.AddEdge("a", "d", map[string]any{"relation": "calls"})

	g3 := graph.New(false)
	g3.AddNode("a", map[string]any{"label": "a()", "file_type": "code"})
	g3.AddNode("b", map[string]any{"label": "b()", "file_type": "code"})
	g3.AddNode("x", map[string]any{"label": "x()", "file_type": "code"})
	g3.AddNode("y", map[string]any{"label": "y()", "file_type": "code"})
	g3.AddEdge("a", "b", map[string]any{"relation": "calls"})
	g3.AddEdge("a", "x", map[string]any{"relation": "calls"})

	opts := Options{SkipCommunities: true, SkipImpact: true}

	// Compute g1-g2 and g1-g3.
	r12 := Compare(g1, g2, "g1", "g2", opts)
	r13 := Compare(g1, g3, "g1", "g3", opts)

	t.Logf("g1 vs g2: composite=%.2f", r12.Summary.CompositeScore)
	t.Logf("g1 vs g3: composite=%.2f", r13.Summary.CompositeScore)

	// Estimate g2-g3 using g1 as pivot.
	est := EstimateSimilarity("g2", "g3", []PivotResult{
		{PivotLabel: "g1", ResultPA: r12, ResultPB: r13},
	})

	t.Logf("Estimated g2 vs g3: %.2f–%.2f (mid=%.2f, via %s)", est.Lower, est.Upper, est.Mid, est.Via)

	// Now compute the actual g2-g3.
	r23 := Compare(g2, g3, "g2", "g3", opts)
	actual := r23.Summary.CompositeScore
	t.Logf("Actual g2 vs g3: composite=%.2f", actual)

	// The actual score should fall within or near the estimated range.
	margin := 0.15 // allow some slack for non-metric components
	if actual < est.Lower-margin || actual > est.Upper+margin {
		t.Errorf("actual score %.2f outside estimated range [%.2f, %.2f] (with %.2f margin)",
			actual, est.Lower, est.Upper, margin)
	}
}

func TestEstimateFromNWay(t *testing.T) {
	g1 := graph.New(false)
	g1.AddNode("a", map[string]any{"label": "a"})
	g1.AddNode("b", map[string]any{"label": "b"})

	g2 := graph.New(false)
	g2.AddNode("b", map[string]any{"label": "b"})
	g2.AddNode("c", map[string]any{"label": "c"})

	g3 := graph.New(false)
	g3.AddNode("a", map[string]any{"label": "a"})
	g3.AddNode("b", map[string]any{"label": "b"})
	g3.AddNode("c", map[string]any{"label": "c"})

	result := CompareN([]*graph.Graph{g1, g2, g3}, []string{"g1", "g2", "g3"},
		Options{SkipCommunities: true, SkipImpact: true})

	if len(result.Estimates) == 0 {
		t.Fatal("expected estimates from N-way comparison")
	}

	for _, e := range result.Estimates {
		t.Logf("  %s", FormatEstimate(&e))
		if e.Lower > e.Upper {
			t.Errorf("lower > upper: %.2f > %.2f", e.Lower, e.Upper)
		}
	}
}

func TestDriftAnalysis(t *testing.T) {
	a := graph.New(false)
	a.AddNode("file1", map[string]any{"label": "main.go", "file_type": "file", "source_file": "main.go"})
	a.AddNode("imp1", map[string]any{"label": "fmt", "file_type": "import"})
	a.AddNode("imp2", map[string]any{"label": "os", "file_type": "import"})
	a.AddEdge("file1", "imp1", map[string]any{"relation": "imports"})
	a.AddEdge("file1", "imp2", map[string]any{"relation": "imports"})

	b := graph.New(false)
	b.AddNode("file1", map[string]any{"label": "main.go", "file_type": "file", "source_file": "main.go"})
	b.AddNode("imp1", map[string]any{"label": "fmt", "file_type": "import"})
	b.AddNode("imp3", map[string]any{"label": "io", "file_type": "import"})
	b.AddEdge("file1", "imp1", map[string]any{"relation": "imports"})
	b.AddEdge("file1", "imp3", map[string]any{"relation": "imports"})

	drift := computeDrift(a, b)
	if len(drift) != 1 {
		t.Fatalf("expected 1 drift entry, got %d", len(drift))
	}
	if len(drift[0].AddedImports) != 1 || drift[0].AddedImports[0] != "io" {
		t.Errorf("expected added import 'io', got %v", drift[0].AddedImports)
	}
	if len(drift[0].RemovedImports) != 1 || drift[0].RemovedImports[0] != "os" {
		t.Errorf("expected removed import 'os', got %v", drift[0].RemovedImports)
	}
}

func TestGenerateReport(t *testing.T) {
	a := graph.New(false)
	a.AddNode("f1", map[string]any{"label": "main", "file_type": "function", "source_file": "main.go"})
	a.AddNode("f2", map[string]any{"label": "helper", "file_type": "function", "source_file": "main.go"})
	a.AddEdge("f1", "f2", map[string]any{"relation": "calls"})

	b := graph.New(false)
	b.AddNode("f1", map[string]any{"label": "main", "file_type": "function", "source_file": "main.go"})
	b.AddNode("f3", map[string]any{"label": "newFunc", "file_type": "function", "source_file": "new.go"})
	b.AddEdge("f1", "f3", map[string]any{"relation": "calls"})

	result := Compare(a, b, "old", "new", Options{SkipCommunities: true, SkipImpact: true})
	report := GenerateReport(result)

	if report == "" {
		t.Error("expected non-empty report")
	}
	if !contains(report, "Graph Comparison Report") {
		t.Error("report missing title")
	}
	if !contains(report, "old") || !contains(report, "new") {
		t.Error("report missing labels")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
