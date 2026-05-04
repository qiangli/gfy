package compare

import (
	"math"
	"testing"

	"github.com/qiangli/gfy/pkg/graph"
)

// makeTreeGraph creates a graph with containment edges forming a tree.
// nodes: list of (id, label, nodeType) tuples.
// edges: list of (parent, child) containment edges.
func makeTreeGraph(nodes [][3]string, edges [][2]string) *graph.Graph {
	g := graph.New(false)
	for _, n := range nodes {
		g.AddNode(n[0], map[string]any{"label": n[1], "file_type": "code"})
	}
	for _, e := range edges {
		g.AddEdge(e[0], e[1], map[string]any{
			"relation": "contains",
			"_src":     e[0],
			"_tgt":     e[1],
		})
	}
	return g
}

func TestExtractContainmentTree(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{
			{"file1", "main.go", "file"},
			{"func1", "funcA()", "function"},
			{"func2", "funcB()", "function"},
		},
		[][2]string{
			{"file1", "func1"},
			{"file1", "func2"},
		},
	)

	ct := ExtractContainmentTree(g)
	if ct.Size != 3 {
		t.Errorf("expected 3 nodes, got %d", ct.Size)
	}
	if len(ct.Roots) != 1 {
		t.Errorf("expected 1 root, got %d", len(ct.Roots))
	}
	root := ct.Roots[0]
	if root.ID != "file1" {
		t.Errorf("expected root ID 'file1', got %q", root.ID)
	}
	if len(root.Children) != 2 {
		t.Errorf("expected 2 children, got %d", len(root.Children))
	}
}

func TestAHU_Isomorphic(t *testing.T) {
	// Two trees with identical structure but different IDs.
	// Tree A: root → [a, b]
	// Tree B: root → [x, y]
	gA := makeTreeGraph(
		[][3]string{{"r1", "root", "file"}, {"a1", "a()", "fn"}, {"b1", "b()", "fn"}},
		[][2]string{{"r1", "a1"}, {"r1", "b1"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r2", "root", "file"}, {"x2", "x()", "fn"}, {"y2", "y()", "fn"}},
		[][2]string{{"r2", "x2"}, {"r2", "y2"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	// Isomorphic trees should have same root hash.
	if ctA.Roots[0].AHUHash != ctB.Roots[0].AHUHash {
		t.Error("expected identical AHU hashes for isomorphic trees")
	}

	score := AHUSubtreeMatchScore(ctA, ctB)
	if score != 1.0 {
		t.Errorf("expected AHU score 1.0, got %.2f", score)
	}
}

func TestAHU_NonIsomorphic(t *testing.T) {
	// Tree A: root → [a, b]
	// Tree B: root → [x, y, z]
	gA := makeTreeGraph(
		[][3]string{{"r1", "root", "file"}, {"a1", "a()", "fn"}, {"b1", "b()", "fn"}},
		[][2]string{{"r1", "a1"}, {"r1", "b1"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r2", "root", "file"}, {"x2", "x()", "fn"}, {"y2", "y()", "fn"}, {"z2", "z()", "fn"}},
		[][2]string{{"r2", "x2"}, {"r2", "y2"}, {"r2", "z2"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	if ctA.Roots[0].AHUHash == ctB.Roots[0].AHUHash {
		t.Error("expected different AHU hashes for non-isomorphic trees")
	}

	score := AHUSubtreeMatchScore(ctA, ctB)
	if score >= 1.0 {
		t.Errorf("expected AHU score < 1.0 for non-isomorphic trees, got %.2f", score)
	}
	if score <= 0 {
		t.Errorf("expected AHU score > 0 (leaves still match), got %.2f", score)
	}
}

func TestTreeEditDistance_Identical(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	ct := ExtractContainmentTree(g)
	ComputeAHU(ct)

	ted := TreeEditDistance(ct, ct, false)
	if ted != 0 {
		t.Errorf("expected TED 0 for identical trees, got %d", ted)
	}

	sim := TreeEditDistanceSimilarity(ct, ct, false)
	if sim != 1.0 {
		t.Errorf("expected TED similarity 1.0, got %.2f", sim)
	}
}

func TestTreeEditDistance_SingleInsert(t *testing.T) {
	gA := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}},
		[][2]string{{"r", "a"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	ted := TreeEditDistance(ctA, ctB, false)
	if ted != 1 {
		t.Errorf("expected TED 1 for single insertion, got %d", ted)
	}
}

func TestTreeEditDistance_Empty(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}},
		[][2]string{{"r", "a"}},
	)
	ct := ExtractContainmentTree(g)
	empty := &ContainmentTree{NodeMap: make(map[string]*TreeNode)}
	ComputeAHU(ct)

	ted := TreeEditDistance(ct, empty, false)
	if ted != ct.Size {
		t.Errorf("expected TED %d for tree vs empty, got %d", ct.Size, ted)
	}
}

func TestMaxCommonSubtree_Identical(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	ct := ExtractContainmentTree(g)
	ComputeAHU(ct)

	score := MaxCommonSubtreeScore(ct, ct)
	if score != 1.0 {
		t.Errorf("expected MCS score 1.0, got %.2f", score)
	}
}

func TestMaxCommonSubtree_Partial(t *testing.T) {
	gA := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}, {"c", "c()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}, {"r", "c"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	score := MaxCommonSubtreeScore(ctA, ctB)
	if score < 0.5 || score > 1.0 {
		t.Errorf("expected MCS score in (0.5, 1.0), got %.2f", score)
	}
}

func TestSubtreeFrequencyCosine_Identical(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	ct := ExtractContainmentTree(g)
	ComputeAHU(ct)

	sim := SubtreeFrequencyCosine(ct, ct, 4, false)
	if math.Abs(sim-1.0) > 1e-10 {
		t.Errorf("expected cosine 1.0 for identical trees, got %f", sim)
	}
}

func TestTreeKernel_Identical(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	ct := ExtractContainmentTree(g)
	ComputeAHU(ct)

	score := TreeKernelScore(ct, ct, 0.5, nil)
	if math.Abs(score-1.0) > 1e-6 {
		t.Errorf("expected kernel score 1.0 for identical trees, got %f", score)
	}
}

func TestTreeKernel_Partial(t *testing.T) {
	gA := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}},
		[][2]string{{"r", "a"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	score := TreeKernelScore(ctA, ctB, 0.5, nil)
	if score <= 0 || score >= 1.0 {
		t.Errorf("expected kernel score in (0, 1), got %f", score)
	}
}

func TestAntiUnification_Identical(t *testing.T) {
	g := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}},
	)
	ct := ExtractContainmentTree(g)
	ComputeAHU(ct)

	score := AntiUnifyCoverage(ct, ct)
	if score != 1.0 {
		t.Errorf("expected AU score 1.0, got %.2f", score)
	}
}

func TestAntiUnification_Partial(t *testing.T) {
	gA := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}},
		[][2]string{{"r", "a"}},
	)
	gB := makeTreeGraph(
		[][3]string{{"r", "root", "file"}, {"a", "a()", "fn"}, {"b", "b()", "fn"}, {"c", "c()", "fn"}},
		[][2]string{{"r", "a"}, {"r", "b"}, {"r", "c"}},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)
	ComputeAHU(ctA)
	ComputeAHU(ctB)

	score := AntiUnifyCoverage(ctA, ctB)
	if score <= 0 || score >= 1.0 {
		t.Errorf("expected AU score in (0, 1), got %.2f", score)
	}
}

func TestComputeTreeScores(t *testing.T) {
	gA := makeTreeGraph(
		[][3]string{
			{"file1", "main.go", "file"},
			{"func1", "funcA()", "fn"},
			{"func2", "funcB()", "fn"},
			{"func3", "funcC()", "fn"},
		},
		[][2]string{
			{"file1", "func1"},
			{"file1", "func2"},
			{"file1", "func3"},
		},
	)
	gB := makeTreeGraph(
		[][3]string{
			{"file1", "app.go", "file"},
			{"func1", "handler()", "fn"},
			{"func2", "helper()", "fn"},
			{"func3", "util()", "fn"},
		},
		[][2]string{
			{"file1", "func1"},
			{"file1", "func2"},
			{"file1", "func3"},
		},
	)

	ctA := ExtractContainmentTree(gA)
	ctB := ExtractContainmentTree(gB)

	scores := ComputeTreeScores(ctA, ctB, 4, 0.5, false, nil)

	// Isomorphic trees should get high scores.
	if scores.AHUSubtreeMatch < 0.9 {
		t.Errorf("expected high AHU score for isomorphic trees, got %.2f", scores.AHUSubtreeMatch)
	}
	if scores.TreeEditDistSim < 0.9 {
		t.Errorf("expected high TED score for isomorphic trees, got %.2f", scores.TreeEditDistSim)
	}
	if scores.MaxCommonSubtree < 0.9 {
		t.Errorf("expected high MCS score for isomorphic trees, got %.2f", scores.MaxCommonSubtree)
	}

	t.Logf("Tree scores: AHU=%.2f TED=%.2f MCS=%.2f Freq=%.2f Kernel=%.2f AU=%.2f",
		scores.AHUSubtreeMatch, scores.TreeEditDistSim, scores.MaxCommonSubtree,
		scores.SubtreeFreqCos, scores.TreeKernelNorm, scores.AntiUnifCoverage)
}

func TestCompositeScore(t *testing.T) {
	sim := SimilarityMetrics{
		NodeJaccard:  0.8,
		EdgeJaccard:  0.7,
		DegreeJSD:    0.1,
		CommunityNMI: 0.9,
		TreeScores: &TreeScores{
			AHUSubtreeMatch:  0.85,
			TreeEditDistSim:  0.75,
			MaxCommonSubtree: 0.80,
			SubtreeFreqCos:   0.70,
			TreeKernelNorm:   0.65,
			AntiUnifCoverage: 0.60,
		},
	}

	composite := ComputeComposite(sim, nil)
	if composite <= 0 || composite >= 1.0 {
		t.Errorf("expected composite in (0, 1), got %f", composite)
	}
	t.Logf("Composite score: %.3f", composite)
}

func TestCompositeScore_NoTree(t *testing.T) {
	sim := SimilarityMetrics{
		NodeJaccard:  0.8,
		EdgeJaccard:  0.7,
		DegreeJSD:    0.1,
		CommunityNMI: 0.9,
	}

	composite := ComputeComposite(sim, nil)
	if composite <= 0 || composite >= 1.0 {
		t.Errorf("expected composite in (0, 1), got %f", composite)
	}
}

func TestCompareWithTreeScores(t *testing.T) {
	a := graph.New(false)
	a.AddNode("file1", map[string]any{"label": "main.go", "file_type": "code"})
	a.AddNode("f1", map[string]any{"label": "funcA()", "file_type": "code"})
	a.AddNode("f2", map[string]any{"label": "funcB()", "file_type": "code"})
	a.AddEdge("file1", "f1", map[string]any{"relation": "contains", "_src": "file1", "_tgt": "f1"})
	a.AddEdge("file1", "f2", map[string]any{"relation": "contains", "_src": "file1", "_tgt": "f2"})
	a.AddEdge("f1", "f2", map[string]any{"relation": "calls", "_src": "f1", "_tgt": "f2"})

	b := graph.New(false)
	b.AddNode("file1", map[string]any{"label": "app.go", "file_type": "code"})
	b.AddNode("f1", map[string]any{"label": "handler()", "file_type": "code"})
	b.AddNode("f2", map[string]any{"label": "helper()", "file_type": "code"})
	b.AddEdge("file1", "f1", map[string]any{"relation": "contains", "_src": "file1", "_tgt": "f1"})
	b.AddEdge("file1", "f2", map[string]any{"relation": "contains", "_src": "file1", "_tgt": "f2"})
	b.AddEdge("f1", "f2", map[string]any{"relation": "calls", "_src": "f1", "_tgt": "f2"})

	result := Compare(a, b, "v1", "v2", Options{SkipCommunities: true, SkipImpact: true})

	if result.Similarity.TreeScores == nil {
		t.Fatal("expected tree scores to be computed")
	}
	if result.Summary.CompositeScore <= 0 {
		t.Errorf("expected positive composite score, got %f", result.Summary.CompositeScore)
	}

	t.Logf("Composite: %.2f", result.Summary.CompositeScore)
	t.Logf("Tree: AHU=%.2f TED=%.2f MCS=%.2f Freq=%.2f Kernel=%.2f AU=%.2f",
		result.Similarity.TreeScores.AHUSubtreeMatch,
		result.Similarity.TreeScores.TreeEditDistSim,
		result.Similarity.TreeScores.MaxCommonSubtree,
		result.Similarity.TreeScores.SubtreeFreqCos,
		result.Similarity.TreeScores.TreeKernelNorm,
		result.Similarity.TreeScores.AntiUnifCoverage)

	// Report should contain tree scores.
	report := GenerateReport(result)
	if !containsSubstr(report, "Tree Comparison Scores") {
		t.Error("expected report to contain tree comparison section")
	}
	if !containsSubstr(report, "Composite similarity") {
		t.Error("expected report to contain composite score")
	}
}

func TestTreeCycleSafety(t *testing.T) {
	// Create a graph where containment edges form a cycle: A contains B, B contains A.
	// This should NOT cause infinite loops — cycles must be broken during extraction.
	g := graph.New(false)
	g.AddNode("a", map[string]any{"label": "A", "file_type": "code"})
	g.AddNode("b", map[string]any{"label": "B", "file_type": "code"})
	g.AddNode("c", map[string]any{"label": "C()", "file_type": "code"})
	// Cycle: a contains b AND b contains a
	g.AddEdge("a", "b", map[string]any{"relation": "contains", "_src": "a", "_tgt": "b"})
	g.AddEdge("b", "a", map[string]any{"relation": "contains", "_src": "b", "_tgt": "a"})
	g.AddEdge("a", "c", map[string]any{"relation": "contains", "_src": "a", "_tgt": "c"})

	// This must not hang.
	ct := ExtractContainmentTree(g)
	if ct.Size == 0 {
		t.Fatal("expected non-empty tree")
	}

	// AHU must not hang.
	ComputeAHU(ct)

	// All algorithms must complete without infinite loop.
	scores := ComputeTreeScores(ct, ct, 4, 0.5, false, nil)
	t.Logf("Cycle safety test: AHU=%.2f TED=%.2f MCS=%.2f Freq=%.2f Kernel=%.2f AU=%.2f",
		scores.AHUSubtreeMatch, scores.TreeEditDistSim, scores.MaxCommonSubtree,
		scores.SubtreeFreqCos, scores.TreeKernelNorm, scores.AntiUnifCoverage)

	// Identical tree comparison should score 1.0 even with cycle-breaking.
	if scores.AHUSubtreeMatch != 1.0 {
		t.Errorf("expected AHU 1.0 for self-comparison, got %.2f", scores.AHUSubtreeMatch)
	}
}
