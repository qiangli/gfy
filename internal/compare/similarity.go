package compare

import (
	"math"
	"sort"

	"github.com/qiangli/gfy/pkg/cluster"
	"github.com/qiangli/gfy/pkg/graph"
)

// computeSimilarity calculates Jaccard and JSD metrics.
func computeSimilarity(a, b *graph.Graph) SimilarityMetrics {
	return SimilarityMetrics{
		NodeJaccard: jaccardNodes(a, b),
		EdgeJaccard: jaccardEdges(a, b),
		DegreeJSD:   degreeJSD(a, b),
	}
}

// jaccardNodes computes Jaccard similarity on node ID sets.
func jaccardNodes(a, b *graph.Graph) float64 {
	aSet := make(map[string]bool)
	for _, id := range a.Nodes() {
		aSet[id] = true
	}
	bSet := make(map[string]bool)
	for _, id := range b.Nodes() {
		bSet[id] = true
	}
	return jaccardSets(aSet, bSet)
}

// jaccardEdges computes Jaccard similarity on edge key sets.
func jaccardEdges(a, b *graph.Graph) float64 {
	aSet := make(map[edgeKey]bool)
	for _, e := range a.Edges() {
		aSet[makeEdgeKey(e)] = true
	}
	bSet := make(map[edgeKey]bool)
	for _, e := range b.Edges() {
		bSet[makeEdgeKey(e)] = true
	}

	if len(aSet) == 0 && len(bSet) == 0 {
		return 1.0
	}

	intersection := 0
	for k := range aSet {
		if bSet[k] {
			intersection++
		}
	}
	union := len(aSet) + len(bSet) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// jaccardSets computes Jaccard similarity between two string sets.
func jaccardSets(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// jaccardStringSlices computes Jaccard similarity between two string slices.
func jaccardStringSlices(a, b []string) float64 {
	aSet := make(map[string]bool, len(a))
	for _, s := range a {
		aSet[s] = true
	}
	bSet := make(map[string]bool, len(b))
	for _, s := range b {
		bSet[s] = true
	}
	return jaccardSets(aSet, bSet)
}

// degreeJSD computes Jensen-Shannon divergence between degree distributions.
func degreeJSD(a, b *graph.Graph) float64 {
	aDist := degreeDistribution(a)
	bDist := degreeDistribution(b)

	// Find the max degree across both distributions.
	maxDeg := 0
	for d := range aDist {
		if d > maxDeg {
			maxDeg = d
		}
	}
	for d := range bDist {
		if d > maxDeg {
			maxDeg = d
		}
	}
	if maxDeg == 0 {
		return 0
	}

	// Build probability vectors.
	p := make([]float64, maxDeg+1)
	q := make([]float64, maxDeg+1)
	aN := float64(a.NodeCount())
	bN := float64(b.NodeCount())
	if aN == 0 {
		aN = 1
	}
	if bN == 0 {
		bN = 1
	}
	for d, c := range aDist {
		p[d] = float64(c) / aN
	}
	for d, c := range bDist {
		q[d] = float64(c) / bN
	}

	return jensenShannonDivergence(p, q)
}

// degreeDistribution returns a histogram of node degrees.
func degreeDistribution(g *graph.Graph) map[int]int {
	dist := make(map[int]int)
	for _, id := range g.Nodes() {
		dist[g.Degree(id)]++
	}
	return dist
}

// jensenShannonDivergence computes JSD between two probability distributions.
// Returns a value in [0, 1] (using log base 2).
func jensenShannonDivergence(p, q []float64) float64 {
	n := len(p)
	if len(q) > n {
		n = len(q)
	}

	// Extend shorter slice with zeros.
	pp := make([]float64, n)
	qq := make([]float64, n)
	copy(pp, p)
	copy(qq, q)

	// M = (P + Q) / 2
	m := make([]float64, n)
	for i := range m {
		m[i] = (pp[i] + qq[i]) / 2.0
	}

	return (klDivergence(pp, m) + klDivergence(qq, m)) / 2.0
}

// klDivergence computes KL(P || Q) using log base 2.
// Returns 0 for terms where p[i] == 0 (0 * log(0) = 0 by convention).
func klDivergence(p, q []float64) float64 {
	sum := 0.0
	for i := range p {
		if p[i] > 0 && q[i] > 0 {
			sum += p[i] * math.Log2(p[i]/q[i])
		}
	}
	return sum
}

// communityComparisonResult extends CommunityComparison with NMI.
type communityComparisonResult struct {
	CommunityComparison
	nmi float64
}

// compareCommunities runs Louvain on both graphs and compares partitions.
func compareCommunities(a, b *graph.Graph) communityComparisonResult {
	commA := cluster.Cluster(a)
	commB := cluster.Cluster(b)

	result := communityComparisonResult{
		CommunityComparison: CommunityComparison{
			CommunitiesA: len(commA),
			CommunitiesB: len(commB),
		},
	}

	// Build node-to-community maps for common nodes.
	nodeToCommA := make(map[string]int)
	for cid, nodes := range commA {
		for _, n := range nodes {
			nodeToCommA[n] = cid
		}
	}
	nodeToCommB := make(map[string]int)
	for cid, nodes := range commB {
		for _, n := range nodes {
			nodeToCommB[n] = cid
		}
	}

	// Compute NMI on common nodes.
	var labelsA, labelsB []int
	for node, ca := range nodeToCommA {
		if cb, ok := nodeToCommB[node]; ok {
			labelsA = append(labelsA, ca)
			labelsB = append(labelsB, cb)
		}
	}
	result.nmi = normalizedMutualInformation(labelsA, labelsB)

	// Detect splits, merges, and stable communities.
	detectSplitsMerges(&result, commA, commB, nodeToCommA, nodeToCommB)

	return result
}

// normalizedMutualInformation computes NMI between two partitions.
// Returns 0 if either partition is empty.
func normalizedMutualInformation(labelsA, labelsB []int) float64 {
	n := len(labelsA)
	if n == 0 {
		return 0
	}

	// Build contingency table.
	contingency := make(map[[2]int]int)
	countA := make(map[int]int)
	countB := make(map[int]int)
	for i := 0; i < n; i++ {
		contingency[[2]int{labelsA[i], labelsB[i]}]++
		countA[labelsA[i]]++
		countB[labelsB[i]]++
	}

	// Compute mutual information.
	nf := float64(n)
	mi := 0.0
	for pair, nij := range contingency {
		if nij == 0 {
			continue
		}
		ni := float64(countA[pair[0]])
		nj := float64(countB[pair[1]])
		mi += float64(nij) / nf * math.Log2(nf*float64(nij)/(ni*nj))
	}

	// Compute entropies.
	ha := entropy(countA, nf)
	hb := entropy(countB, nf)

	denom := (ha + hb) / 2.0
	if denom == 0 {
		return 0
	}
	return mi / denom
}

func entropy(counts map[int]int, n float64) float64 {
	h := 0.0
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

// detectSplitsMerges identifies communities that split, merged, or remained stable.
func detectSplitsMerges(result *communityComparisonResult, commA, commB map[int][]string,
	nodeToCommA, nodeToCommB map[string]int) {

	// For each community in A, see which B communities its members ended up in.
	for _, nodesA := range commA {
		bComms := make(map[int][]string) // B community ID → nodes from this A community
		for _, n := range nodesA {
			if cb, ok := nodeToCommB[n]; ok {
				bComms[cb] = append(bComms[cb], n)
			}
		}
		if len(bComms) <= 1 {
			continue
		}
		// This A community split into multiple B communities.
		// Only report if there are at least 2 significant parts.
		var parts [][]string
		for _, nodes := range bComms {
			if len(nodes) >= 2 {
				sort.Strings(nodes)
				parts = append(parts, nodes)
			}
		}
		if len(parts) >= 2 {
			sort.Strings(nodesA)
			result.Splits = append(result.Splits, CommunitySplit{
				OldNodes: nodesA,
				NewParts: parts,
			})
		}
	}

	// For each community in B, see which A communities its members came from.
	for _, nodesB := range commB {
		aComms := make(map[int][]string)
		for _, n := range nodesB {
			if ca, ok := nodeToCommA[n]; ok {
				aComms[ca] = append(aComms[ca], n)
			}
		}
		if len(aComms) <= 1 {
			continue
		}
		var parts [][]string
		for _, nodes := range aComms {
			if len(nodes) >= 2 {
				sort.Strings(nodes)
				parts = append(parts, nodes)
			}
		}
		if len(parts) >= 2 {
			sort.Strings(nodesB)
			result.Merges = append(result.Merges, CommunityMerge{
				OldParts: parts,
				NewNodes: nodesB,
			})
		}
	}

	// Stable: find best-matching community pairs with high Jaccard overlap.
	for _, nodesA := range commA {
		aSet := make(map[string]bool, len(nodesA))
		for _, n := range nodesA {
			aSet[n] = true
		}
		bestOverlap := 0.0
		var bestNodesB []string
		for _, nodesB := range commB {
			bSet := make(map[string]bool, len(nodesB))
			for _, n := range nodesB {
				bSet[n] = true
			}
			j := jaccardSets(aSet, bSet)
			if j > bestOverlap {
				bestOverlap = j
				bestNodesB = nodesB
			}
		}
		if bestOverlap >= 0.5 {
			sortedA := make([]string, len(nodesA))
			copy(sortedA, nodesA)
			sort.Strings(sortedA)
			sortedB := make([]string, len(bestNodesB))
			copy(sortedB, bestNodesB)
			sort.Strings(sortedB)
			result.Stable = append(result.Stable, CommunityStable{
				NodesA:  sortedA,
				NodesB:  sortedB,
				Overlap: bestOverlap,
			})
		}
	}

	sort.Slice(result.Stable, func(i, j int) bool {
		return result.Stable[i].Overlap > result.Stable[j].Overlap
	})
}
