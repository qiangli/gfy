package compare

import "fmt"

// TreeScores holds the results of all tree comparison algorithms.
// All scores are in [0, 1] where higher means more similar.
type TreeScores struct {
	AHUSubtreeMatch  float64 `json:"ahu_subtree_match"`             // Jaccard on subtree hash multisets
	TreeEditDistSim  float64 `json:"tree_edit_distance_similarity"` // 1 - TED/(|A|+|B|)
	MaxCommonSubtree float64 `json:"max_common_subtree_ratio"`      // |MCS|/max(|A|,|B|)
	SubtreeFreqCos   float64 `json:"subtree_frequency_cosine"`      // cosine similarity of freq vectors
	TreeKernelNorm   float64 `json:"tree_kernel_normalized"`        // K(A,B)/√(K(A,A)·K(B,B))
	AntiUnifCoverage float64 `json:"anti_unification_coverage"`     // |AU|/max(|A|,|B|)
	RoleDistribution float64 `json:"role_distribution"`             // cosine similarity of role vectors

	// TEDApproximate is true when tree edit distance used depth-sampled
	// approximation instead of exact Zhang-Shasha (for trees >5K nodes).
	TEDApproximate bool `json:"ted_approximate,omitempty"`

	// SemanticAHU is true when semantic-aware AHU hashing was used.
	SemanticAHU bool `json:"semantic_ahu,omitempty"`
}

// ScoreWeights controls the contribution of each metric to the composite score.
// Weights are relative (they are normalized to sum to 1.0 internally).
type ScoreWeights struct {
	NodeJaccard      float64 `json:"node_jaccard"`
	EdgeJaccard      float64 `json:"edge_jaccard"`
	DegreeSimilarity float64 `json:"degree_similarity"`
	CommunityNMI     float64 `json:"community_nmi"`
	AHUSubtreeMatch  float64 `json:"ahu_subtree_match"`
	TreeEditDist     float64 `json:"tree_edit_distance"`
	MaxCommonSubtree float64 `json:"max_common_subtree"`
	SubtreeFreqCos   float64 `json:"subtree_frequency_cosine"`
	TreeKernel       float64 `json:"tree_kernel"`
	AntiUnification  float64 `json:"anti_unification"`
	RoleDistribution float64 `json:"role_distribution"`
}

// DefaultWeights returns the default weight configuration.
// Tree-level metrics get 80% total weight (structural depth matters most).
// Graph-level metrics get 20% total weight (set-level overview).
func DefaultWeights() ScoreWeights {
	return ScoreWeights{
		NodeJaccard:      0.05,
		EdgeJaccard:      0.05,
		DegreeSimilarity: 0.05,
		CommunityNMI:     0.05,
		AHUSubtreeMatch:  0.15,
		TreeEditDist:     0.20,
		MaxCommonSubtree: 0.15,
		SubtreeFreqCos:   0.10,
		TreeKernel:       0.10,
		AntiUnification:  0.10,
		RoleDistribution: 0.00, // disabled by default for backward compat
	}
}

// CrossProjectWeights returns weights optimized for cross-project comparison.
// Node/Edge Jaccard are zeroed (useless when node IDs differ across projects).
// Their weight is redistributed to tree-level structural metrics.
// Role distribution is enabled to capture behavioral similarity.
func CrossProjectWeights() ScoreWeights {
	return ScoreWeights{
		NodeJaccard:      0.00,
		EdgeJaccard:      0.00,
		DegreeSimilarity: 0.05,
		CommunityNMI:     0.05,
		AHUSubtreeMatch:  0.15,
		TreeEditDist:     0.20,
		MaxCommonSubtree: 0.15,
		SubtreeFreqCos:   0.10,
		TreeKernel:       0.10,
		AntiUnification:  0.10,
		RoleDistribution: 0.10,
	}
}

// ComputeTreeScores runs all tree comparison algorithms on two
// containment trees and returns the individual scores.
// The optional progress callback reports each algorithm as it runs.
// When semanticAHU is true, uses semantic-aware hashing that incorporates
// NodeType and Tags — this dramatically improves cross-project comparison
// by distinguishing nodes with different behavioral roles.
func ComputeTreeScores(a, b *ContainmentTree, treeDepthLimit int, treeKernelLambda float64, semanticAHU bool, progress func(string)) TreeScores {
	if progress == nil {
		progress = func(string) {}
	}

	// Ensure AHU hashes are computed (foundation for algorithms 3-6).
	if semanticAHU {
		progress("Computing semantic AHU canonical hashes (NodeType + Tags)...")
		ComputeSemanticAHU(a)
		ComputeSemanticAHU(b)
	} else {
		progress("Computing AHU canonical hashes...")
		ComputeAHU(a)
		ComputeAHU(b)
	}

	if treeDepthLimit <= 0 {
		treeDepthLimit = 4
	}
	if treeKernelLambda <= 0 {
		treeKernelLambda = 0.5
	}

	var ts TreeScores

	progress(fmt.Sprintf("Trees extracted: %d nodes vs %d nodes", a.Size, b.Size))

	progress("AHU subtree match (isomorphic subtree ratio)...")
	ts.AHUSubtreeMatch = AHUSubtreeMatchScore(a, b)
	progress(fmt.Sprintf("  AHU = %.2f", ts.AHUSubtreeMatch))

	progress("Tree edit distance (Zhang-Shasha)...")
	ts.TreeEditDistSim = TreeEditDistanceSimilarity(a, b, semanticAHU)
	progress(fmt.Sprintf("  TED = %.2f", ts.TreeEditDistSim))

	progress("Maximum common subtree...")
	ts.MaxCommonSubtree = MaxCommonSubtreeScore(a, b)
	progress(fmt.Sprintf("  MCS = %.2f", ts.MaxCommonSubtree))

	progress("Subtree frequency vectors (cosine similarity)...")
	ts.SubtreeFreqCos = SubtreeFrequencyCosine(a, b, treeDepthLimit, semanticAHU)
	progress(fmt.Sprintf("  Freq = %.2f", ts.SubtreeFreqCos))

	// Kernel — only iterates pairs with shared AHU hashes (exact, no sampling).
	{
		maxDepth := 0
		if a.Size > 1000 || b.Size > 1000 {
			maxDepth = 6
		}
		effectiveA := len(collectNodes(a, maxDepth))
		effectiveB := len(collectNodes(b, maxDepth))
		if maxDepth > 0 {
			progress(fmt.Sprintf("Tree kernel (Collins-Duffy, depth≤%d, %d+%d nodes)...", maxDepth, effectiveA, effectiveB))
		} else {
			progress(fmt.Sprintf("Tree kernel (Collins-Duffy, %d+%d nodes)...", effectiveA, effectiveB))
		}
		ts.TreeKernelNorm = TreeKernelScore(a, b, treeKernelLambda, progress)
		progress(fmt.Sprintf("  Kernel = %.2f", ts.TreeKernelNorm))
	}

	progress("Anti-unification (shared structural template)...")
	ts.AntiUnifCoverage = AntiUnifyCoverage(a, b)
	progress(fmt.Sprintf("  AU = %.2f", ts.AntiUnifCoverage))

	progress("Role distribution (behavioral profile cosine)...")
	ts.RoleDistribution = RoleDistributionSimilarity(a, b)
	progress(fmt.Sprintf("  Role = %.2f", ts.RoleDistribution))

	ts.SemanticAHU = semanticAHU

	return ts
}

// ComputeComposite combines all available metrics into a single [0,1]
// composite score using a weighted arithmetic mean.
func ComputeComposite(sim SimilarityMetrics, weights *ScoreWeights) float64 {
	w := DefaultWeights()
	if weights != nil {
		w = *weights
	}

	type entry struct {
		score  float64
		weight float64
		active bool
	}

	entries := []entry{
		{sim.NodeJaccard, w.NodeJaccard, true},
		{sim.EdgeJaccard, w.EdgeJaccard, true},
		{1.0 - sim.DegreeJSD, w.DegreeSimilarity, true}, // Convert divergence to similarity
		{sim.CommunityNMI, w.CommunityNMI, sim.CommunityNMI >= 0},
	}

	// Tree scores (if available).
	if sim.TreeScores != nil {
		ts := sim.TreeScores
		entries = append(entries,
			entry{ts.AHUSubtreeMatch, w.AHUSubtreeMatch, true},
			entry{ts.TreeEditDistSim, w.TreeEditDist, true},
			entry{ts.MaxCommonSubtree, w.MaxCommonSubtree, true},
			entry{ts.SubtreeFreqCos, w.SubtreeFreqCos, true},
			entry{ts.TreeKernelNorm, w.TreeKernel, true},
			entry{ts.AntiUnifCoverage, w.AntiUnification, true},
			entry{ts.RoleDistribution, w.RoleDistribution, w.RoleDistribution > 0},
		)
	}

	totalWeight := 0.0
	weightedSum := 0.0
	for _, e := range entries {
		if e.active {
			weightedSum += e.score * e.weight
			totalWeight += e.weight
		}
	}

	if totalWeight == 0 {
		return 0
	}
	return weightedSum / totalWeight
}
