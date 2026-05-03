// Package compare provides graph comparison algorithms for measuring
// structural similarity, detecting changes, and ranking impact between
// two or more knowledge graphs.
package compare

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/qiangli/gfy/internal/graph"
)

// Options configures comparison behavior.
type Options struct {
	RenameThreshold float64 // min combined similarity for rename detection (default 0.6)
	ImpactDepth     int     // BFS depth for impact analysis (default 3)
	TopK            int     // max entries per result section (default 20)
	SkipCommunities bool    // skip community comparison
	SkipImpact      bool    // skip impact analysis

	// Normalize enables structural alignment before comparison.
	Normalize        bool
	NormalizeOptions NormalizeOptions

	// Tree comparison options.
	SkipTreeComparison bool          // skip all tree algorithms (faster)
	TreeDepthLimit     int           // max depth for subtree frequency vectors (default 4)
	TreeKernelLambda   float64       // decay factor for tree kernel (default 0.5)
	Weights            *ScoreWeights // custom composite score weights (nil = defaults)

	// Sensitivity controls how aggressively the tool looks through surface
	// differences (renames, refactors) to find intrinsic functional similarity.
	// Range [0, 1]. Default: 0.5.
	//   < 0.3 (strict):   standard mode, good for same-project branch comparison
	//   0.3-0.7 (balanced): semantic AHU + cross-project weights (auto with --normalize)
	//   > 0.7 (generous):  above + lower match threshold for more permissive alignment
	// When Sensitivity is 0 and no explicit value was set, it defaults to 0.5
	// if Normalize is true, otherwise stays at 0 (strict).
	Sensitivity    float64
	SensitivitySet bool // true when the user explicitly provided --sensitivity

	// Progress callback. Called with a short status message at each step.
	// Nil means no progress reporting.
	OnProgress func(step string)

	// EstimateMode enables N-way estimation: only compute full comparisons
	// against the first graph (N-1 pairs), then estimate the remaining
	// C(N-1,2) pairs via triangle inequality. Much faster for large N.
	EstimateMode bool
}

func (o Options) withDefaults() Options {
	if o.RenameThreshold == 0 {
		o.RenameThreshold = 0.6
	}
	if o.ImpactDepth == 0 {
		o.ImpactDepth = 3
	}
	if o.TopK == 0 {
		o.TopK = 20
	}
	return o
}

// Summary holds headline statistics for a pairwise comparison.
type Summary struct {
	NodesA            int     `json:"nodes_a"`
	NodesB            int     `json:"nodes_b"`
	EdgesA            int     `json:"edges_a"`
	EdgesB            int     `json:"edges_b"`
	NodesAdded        int     `json:"nodes_added"`
	NodesRemoved      int     `json:"nodes_removed"`
	NodesModified     int     `json:"nodes_modified"`
	EdgesAdded        int     `json:"edges_added"`
	EdgesRemoved      int     `json:"edges_removed"`
	EdgesModified     int     `json:"edges_modified"`
	ApproxGED         int     `json:"approx_ged"` // lower bound on graph edit distance
	OverallSimilarity float64 `json:"overall_similarity"`
	CompositeScore    float64 `json:"composite_score"` // weighted composite of all metrics [0,1]
}

// NodeInfo describes a node for diff output.
type NodeInfo struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	FileType string `json:"file_type"`
	File     string `json:"source_file"`
	Degree   int    `json:"degree"`
}

// NodeModification records attribute changes for a node present in both graphs.
type NodeModification struct {
	ID        string            `json:"id"`
	Label     string            `json:"label"`
	Changes   map[string][2]any `json:"changes"`
	DegreeOld int               `json:"degree_old"`
	DegreeNew int               `json:"degree_new"`
}

// NodeDiff holds added, removed, and modified nodes.
type NodeDiff struct {
	Added    []NodeInfo         `json:"added"`
	Removed  []NodeInfo         `json:"removed"`
	Modified []NodeModification `json:"modified"`
}

// EdgeInfo describes an edge for diff output.
type EdgeInfo struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Confidence string `json:"confidence"`
}

// EdgeModification records attribute changes for an edge present in both graphs.
type EdgeModification struct {
	Source   string            `json:"source"`
	Target   string            `json:"target"`
	Relation string            `json:"relation"`
	Changes  map[string][2]any `json:"changes"`
}

// EdgeDiff holds added, removed, and modified edges.
type EdgeDiff struct {
	Added    []EdgeInfo         `json:"added"`
	Removed  []EdgeInfo         `json:"removed"`
	Modified []EdgeModification `json:"modified"`
}

// SimilarityMetrics holds computed similarity/distance scores.
type SimilarityMetrics struct {
	NodeJaccard  float64 `json:"node_jaccard"`  // [0,1] higher = more similar
	EdgeJaccard  float64 `json:"edge_jaccard"`  // [0,1] higher = more similar
	DegreeJSD    float64 `json:"degree_jsd"`    // [0,1] lower = more similar
	CommunityNMI float64 `json:"community_nmi"` // [0,1] higher = more similar; -1 if skipped

	TreeScores *TreeScores `json:"tree_scores,omitempty"` // 6 tree comparison scores
}

// CommunityComparison describes how communities changed.
type CommunityComparison struct {
	CommunitiesA int               `json:"communities_a"`
	CommunitiesB int               `json:"communities_b"`
	Splits       []CommunitySplit  `json:"splits,omitempty"`
	Merges       []CommunityMerge  `json:"merges,omitempty"`
	Stable       []CommunityStable `json:"stable,omitempty"`
}

// CommunitySplit describes an old community that split into multiple new ones.
type CommunitySplit struct {
	OldNodes []string   `json:"old_nodes"`
	NewParts [][]string `json:"new_parts"`
}

// CommunityMerge describes multiple old communities that merged.
type CommunityMerge struct {
	OldParts [][]string `json:"old_parts"`
	NewNodes []string   `json:"new_nodes"`
}

// CommunityStable describes a community that remained largely intact.
type CommunityStable struct {
	NodesA  []string `json:"nodes_a"`
	NodesB  []string `json:"nodes_b"`
	Overlap float64  `json:"overlap"` // Jaccard of node sets
}

// RenameCandidate pairs an old and new node suspected to be a rename/move.
type RenameCandidate struct {
	OldID           string  `json:"old_id"`
	OldLabel        string  `json:"old_label"`
	NewID           string  `json:"new_id"`
	NewLabel        string  `json:"new_label"`
	EditDistance    int     `json:"edit_distance"`
	NeighborOverlap float64 `json:"neighbor_overlap"`
	Confidence      float64 `json:"confidence"`
}

// ImpactEntry ranks a changed node by how many transitive dependents it affects.
type ImpactEntry struct {
	NodeID        string   `json:"node_id"`
	Label         string   `json:"label"`
	Change        string   `json:"change"` // "added", "removed", "modified"
	AffectedCount int      `json:"affected_count"`
	AffectedNodes []string `json:"affected_nodes,omitempty"`
}

// DriftEntry summarizes import changes for a single source file.
type DriftEntry struct {
	SourceFile     string   `json:"source_file"`
	AddedImports   []string `json:"added_imports,omitempty"`
	RemovedImports []string `json:"removed_imports,omitempty"`
}

// Result holds the complete output of a pairwise comparison.
type Result struct {
	Labels      [2]string            `json:"labels"`
	Summary     Summary              `json:"summary"`
	NodeDiff    NodeDiff             `json:"node_diff"`
	EdgeDiff    EdgeDiff             `json:"edge_diff"`
	Similarity  SimilarityMetrics    `json:"similarity"`
	Communities *CommunityComparison `json:"communities,omitempty"`
	Renames     []RenameCandidate    `json:"renames,omitempty"`
	Impact      []ImpactEntry        `json:"impact,omitempty"`
	Drift       []DriftEntry         `json:"drift,omitempty"`
	Alignment   *AlignmentResult     `json:"alignment,omitempty"`
}

// ToJSON serializes the result as indented JSON.
func (r *Result) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// CoreSummary describes entities common to all N graphs.
type CoreSummary struct {
	NodeCount int      `json:"node_count"`
	EdgeCount int      `json:"edge_count"`
	Nodes     []string `json:"nodes,omitempty"` // IDs (capped at TopK)
}

// UniqueSummary describes entities present in only one graph.
type UniqueSummary struct {
	Label     string   `json:"label"`
	NodeCount int      `json:"node_count"`
	EdgeCount int      `json:"edge_count"`
	Nodes     []string `json:"nodes,omitempty"` // IDs (capped at TopK)
}

// NWayResult holds results for comparing N>2 graphs.
type NWayResult struct {
	Labels          []string         `json:"labels"`
	PairwiseResults [][]*Result      `json:"pairwise_results"` // [i][j] for i<j, nil for i>=j
	Heatmap         [][]float64      `json:"heatmap"`          // [i][j] = composite score
	Core            CoreSummary      `json:"core"`
	Unique          []UniqueSummary  `json:"unique"`
	Estimates       []EstimatedRange `json:"estimates,omitempty"` // triangle-inequality bounds for all pairs
}

// ToJSON serializes the N-way result as indented JSON.
func (r *NWayResult) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Compare performs a full pairwise comparison between two graphs.
// When opts.Normalize is true, graph B is structurally aligned to graph A
// before computing diffs and similarity — this handles cases where nodes
// represent the same entities but have different IDs/labels.
func Compare(a, b *graph.Graph, labelA, labelB string, opts Options) *Result {
	opts = opts.withDefaults()

	r := &Result{
		Labels: [2]string{labelA, labelB},
	}

	progress := opts.OnProgress
	if progress == nil {
		progress = func(string) {} // no-op
	}

	// Apply sensitivity defaults.
	sensitivity := opts.Sensitivity
	if !opts.SensitivitySet && opts.Normalize && sensitivity == 0 {
		sensitivity = 0.5 // default to balanced mode when normalizing
	}

	// Sensitivity affects alignment permissiveness.
	if sensitivity > 0.7 && opts.Normalize {
		if opts.NormalizeOptions.MinMatchScore == 0 || opts.NormalizeOptions.MinMatchScore == 0.4 {
			opts.NormalizeOptions.MinMatchScore = 0.25
		}
	}

	// Select weights based on sensitivity and normalization mode.
	if opts.Weights == nil && opts.Normalize && sensitivity >= 0.3 {
		w := CrossProjectWeights()
		opts.Weights = &w
	}

	semanticAHU := sensitivity >= 0.3

	if sensitivity > 0 {
		progress(fmt.Sprintf("Sensitivity: %.1f (semantic AHU: %v, cross-project weights: %v)",
			sensitivity, semanticAHU, opts.Normalize && sensitivity >= 0.3))
	}

	// Structural alignment: re-key graph B to match graph A's node IDs
	// based on structural fingerprints rather than label equality.
	if opts.Normalize {
		progress("Aligning nodes by structural fingerprint...")
		alignment := AlignGraphs(a, b, opts.NormalizeOptions)
		r.Alignment = alignment
		b = ReKeyGraph(b, alignment)
	}

	// Diff
	progress("Computing node/edge diffs...")
	r.NodeDiff = diffNodes(a, b)
	r.EdgeDiff = diffEdges(a, b)

	// Summary counts
	r.Summary = Summary{
		NodesA:        a.NodeCount(),
		NodesB:        b.NodeCount(),
		EdgesA:        a.EdgeCount(),
		EdgesB:        b.EdgeCount(),
		NodesAdded:    len(r.NodeDiff.Added),
		NodesRemoved:  len(r.NodeDiff.Removed),
		NodesModified: len(r.NodeDiff.Modified),
		EdgesAdded:    len(r.EdgeDiff.Added),
		EdgesRemoved:  len(r.EdgeDiff.Removed),
		EdgesModified: len(r.EdgeDiff.Modified),
	}
	r.Summary.ApproxGED = r.Summary.NodesAdded + r.Summary.NodesRemoved + r.Summary.NodesModified +
		r.Summary.EdgesAdded + r.Summary.EdgesRemoved + r.Summary.EdgesModified
	progress(fmt.Sprintf("  Nodes: +%d / -%d / ~%d | Edges: +%d / -%d / ~%d",
		r.Summary.NodesAdded, r.Summary.NodesRemoved, r.Summary.NodesModified,
		r.Summary.EdgesAdded, r.Summary.EdgesRemoved, r.Summary.EdgesModified))

	// Similarity
	progress("Computing graph similarity (Jaccard, JSD)...")
	r.Similarity = computeSimilarity(a, b)
	r.Summary.OverallSimilarity = r.Similarity.NodeJaccard
	progress(fmt.Sprintf("  Jaccard=%.2f  JSD=%.4f", r.Similarity.NodeJaccard, r.Similarity.DegreeJSD))

	// Community comparison
	if !opts.SkipCommunities {
		progress("Detecting communities (Louvain + NMI)...")
		cc := compareCommunities(a, b)
		r.Communities = &cc.CommunityComparison
		r.Similarity.CommunityNMI = cc.nmi
		progress(fmt.Sprintf("  NMI=%.2f (%d vs %d communities)", cc.nmi, cc.CommunitiesA, cc.CommunitiesB))
	} else {
		r.Similarity.CommunityNMI = -1
	}

	// Rename detection
	progress("Detecting renames...")
	r.Renames = detectRenames(a, b, r.NodeDiff.Removed, r.NodeDiff.Added, opts.RenameThreshold, opts.TopK)
	if len(r.Renames) > 0 {
		progress(fmt.Sprintf("  Found %d rename candidate(s)", len(r.Renames)))
	}

	// Impact analysis
	if !opts.SkipImpact {
		progress("Ranking change impact...")
		r.Impact = rankImpact(a, b, r.NodeDiff, opts.ImpactDepth, opts.TopK)
	}

	// Dependency drift
	progress("Analyzing dependency drift...")
	r.Drift = computeDrift(a, b)
	if len(r.Drift) > 0 {
		progress(fmt.Sprintf("  %d file(s) with import changes", len(r.Drift)))
	}

	// Tree comparison
	if !opts.SkipTreeComparison {
		progress("Extracting containment trees...")
		treeA := ExtractContainmentTree(a)
		treeB := ExtractContainmentTree(b)
		if treeA.Size > 0 && treeB.Size > 0 {
			ts := ComputeTreeScores(treeA, treeB, opts.TreeDepthLimit, opts.TreeKernelLambda, semanticAHU, progress)
			r.Similarity.TreeScores = &ts
		}
	}

	// Composite score (always computed from available metrics).
	progress("Computing composite score...")
	r.Summary.CompositeScore = ComputeComposite(r.Similarity, opts.Weights)

	return r
}

// CompareN compares N graphs pairwise and produces an N-way result.
//
// When opts.EstimateMode is true, only N-1 full comparisons are computed
// (all against the first graph as pivot). The remaining C(N-1,2) pairs
// are estimated via triangle inequality. The heatmap uses actual scores
// for computed pairs and midpoint estimates for the rest.
func CompareN(graphs []*graph.Graph, labels []string, opts Options) *NWayResult {
	opts = opts.withDefaults()
	n := len(graphs)

	progress := opts.OnProgress
	if progress == nil {
		progress = func(string) {}
	}

	r := &NWayResult{
		Labels:          labels,
		PairwiseResults: make([][]*Result, n),
		Heatmap:         make([][]float64, n),
	}

	for i := 0; i < n; i++ {
		r.PairwiseResults[i] = make([]*Result, n)
		r.Heatmap[i] = make([]float64, n)
		r.Heatmap[i][i] = 1.0
	}

	if opts.EstimateMode && n > 2 {
		// Estimate mode: compute only N-1 pairs (pivot = first graph).
		progress(fmt.Sprintf("Estimate mode: %d full comparisons (pivot: %s), %d estimated",
			n-1, labels[0], (n-1)*(n-2)/2))
		for j := 1; j < n; j++ {
			progress(fmt.Sprintf("Comparing %s vs %s (%d/%d)...", labels[0], labels[j], j, n-1))
			res := Compare(graphs[0], graphs[j], labels[0], labels[j], opts)
			r.PairwiseResults[0][j] = res
			r.Heatmap[0][j] = res.Summary.CompositeScore
			r.Heatmap[j][0] = res.Summary.CompositeScore
		}
	} else {
		// Full mode: compute all C(N,2) pairs.
		totalPairs := n * (n - 1) / 2
		pair := 0
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				pair++
				progress(fmt.Sprintf("Comparing %s vs %s (%d/%d)...", labels[i], labels[j], pair, totalPairs))
				res := Compare(graphs[i], graphs[j], labels[i], labels[j], opts)
				r.PairwiseResults[i][j] = res
				r.Heatmap[i][j] = res.Summary.CompositeScore
				r.Heatmap[j][i] = res.Summary.CompositeScore
			}
		}
	}

	// Core: nodes present in ALL graphs
	r.Core = computeCore(graphs, opts.TopK)

	// Unique: nodes present in only one graph
	r.Unique = computeUnique(graphs, labels, opts.TopK)

	// Estimates: triangle-inequality bounds for all pairs.
	r.Estimates = EstimateFromNWay(r)

	// In estimate mode, fill heatmap gaps with midpoint estimates.
	if opts.EstimateMode {
		for _, e := range r.Estimates {
			i := labelIndex(labels, e.LabelA)
			j := labelIndex(labels, e.LabelB)
			if i >= 0 && j >= 0 && r.PairwiseResults[min(i, j)][max(i, j)] == nil {
				r.Heatmap[i][j] = e.Mid
				r.Heatmap[j][i] = e.Mid
			}
		}
	}

	return r
}

func labelIndex(labels []string, label string) int {
	for i, l := range labels {
		if l == label {
			return i
		}
	}
	return -1
}

// computeCore finds nodes present in every graph.
func computeCore(graphs []*graph.Graph, topK int) CoreSummary {
	if len(graphs) == 0 {
		return CoreSummary{}
	}

	// Start with nodes from the first graph.
	common := make(map[string]bool)
	for _, id := range graphs[0].Nodes() {
		common[id] = true
	}
	for _, g := range graphs[1:] {
		gNodes := make(map[string]bool)
		for _, id := range g.Nodes() {
			gNodes[id] = true
		}
		for id := range common {
			if !gNodes[id] {
				delete(common, id)
			}
		}
	}

	nodes := make([]string, 0, len(common))
	for id := range common {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	// Count edges common to all graphs.
	type edgeKey struct{ src, tgt, rel string }
	commonEdges := make(map[edgeKey]bool)
	for _, e := range graphs[0].Edges() {
		rel, _ := e.Attrs["relation"].(string)
		commonEdges[edgeKey{e.Source, e.Target, rel}] = true
	}
	for _, g := range graphs[1:] {
		gEdges := make(map[edgeKey]bool)
		for _, e := range g.Edges() {
			rel, _ := e.Attrs["relation"].(string)
			gEdges[edgeKey{e.Source, e.Target, rel}] = true
		}
		for k := range commonEdges {
			if !gEdges[k] {
				delete(commonEdges, k)
			}
		}
	}

	cs := CoreSummary{
		NodeCount: len(nodes),
		EdgeCount: len(commonEdges),
	}
	if len(nodes) <= topK {
		cs.Nodes = nodes
	} else {
		cs.Nodes = nodes[:topK]
	}
	return cs
}

// computeUnique finds nodes present in only one graph.
func computeUnique(graphs []*graph.Graph, labels []string, topK int) []UniqueSummary {
	// Count how many graphs contain each node.
	nodeCounts := make(map[string]int)
	nodeOwner := make(map[string]int) // which graph index
	for i, g := range graphs {
		for _, id := range g.Nodes() {
			nodeCounts[id]++
			nodeOwner[id] = i
		}
	}

	// Group unique nodes by owning graph.
	uniqueByGraph := make([][]string, len(graphs))
	for id, count := range nodeCounts {
		if count == 1 {
			owner := nodeOwner[id]
			uniqueByGraph[owner] = append(uniqueByGraph[owner], id)
		}
	}

	result := make([]UniqueSummary, len(graphs))
	for i, nodes := range uniqueByGraph {
		sort.Strings(nodes)

		// Count edges unique to this graph whose both endpoints are unique.
		edgeCount := 0
		uniqueSet := make(map[string]bool, len(nodes))
		for _, id := range nodes {
			uniqueSet[id] = true
		}
		for _, e := range graphs[i].Edges() {
			if uniqueSet[e.Source] || uniqueSet[e.Target] {
				edgeCount++
			}
		}

		us := UniqueSummary{
			Label:     labels[i],
			NodeCount: len(nodes),
			EdgeCount: edgeCount,
		}
		if len(nodes) <= topK {
			us.Nodes = nodes
		} else {
			us.Nodes = nodes[:topK]
		}
		result[i] = us
	}
	return result
}
