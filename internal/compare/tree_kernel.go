package compare

import (
	"fmt"
	"math"
	"sort"
)

// TreeKernelScore computes the normalized Collins & Duffy subset tree kernel
// between two containment trees. The kernel counts the number of common
// subset trees (subtrees where internal nodes can skip children).
//
// Lambda is a decay factor (0 < λ ≤ 1) that down-weights larger subset trees.
// Default: 0.5.
//
// Optimization: The naive approach computes K(n1,n2) for every pair in
// nodesA × nodesB, but most pairs contribute exactly λ (single-node match)
// and cancel in normalization. This implementation only recurses into pairs
// whose children share AHU hashes, computing the "interesting" part of the
// kernel exactly. Trivial λ contributions are counted analytically.
//
// For large trees (>1K nodes), limits to depth ≤ 6.
//
// Score: K(A,B) / √(K(A,A) · K(B,B)) → [0,1].
func TreeKernelScore(a, b *ContainmentTree, lambda float64, progress func(string)) float64 {
	if progress == nil {
		progress = func(string) {}
	}
	if lambda <= 0 {
		lambda = 0.5
	}
	if a.Size == 0 && b.Size == 0 {
		return 1.0
	}
	if a.Size == 0 || b.Size == 0 {
		return 0
	}

	maxDepth := 0 // unlimited
	if a.Size > 1000 || b.Size > 1000 {
		maxDepth = 6
	}

	nodesA := collectNodes(a, maxDepth)
	nodesB := collectNodes(b, maxDepth)
	nA := float64(len(nodesA))
	nB := float64(len(nodesB))

	// Group nodes by AHU hash for fast lookup.
	groupA := groupNodesByHash(nodesA)
	groupB := groupNodesByHash(nodesB)

	// Find hashes that appear in both trees — only these produce
	// kernel values > λ.
	commonHashes := make(map[uint64]bool)
	for h := range groupA {
		if _, ok := groupB[h]; ok {
			commonHashes[h] = true
		}
	}

	// Count the actual pairs we'll recurse into.
	interestingPairs := 0
	for h := range commonHashes {
		interestingPairs += len(groupA[h]) * len(groupB[h])
	}
	progress(fmt.Sprintf("    %d unique hashes in A, %d in B, %d shared → %d interesting pairs",
		len(groupA), len(groupB), len(commonHashes), interestingPairs))

	memo := make(map[nodePairKey]float64)

	// K(A, B): compute exact kernel only for pairs sharing structure.
	// For all other pairs, kernel = λ (single-node match).
	progress("    Computing K(A,B)...")
	kab := lambda * nA * nB // baseline: every pair contributes at least λ
	for h := range commonHashes {
		for _, na := range groupA[h] {
			for _, nb := range groupB[h] {
				actual := subsetTreeKernel(na, nb, lambda, memo)
				kab += actual - lambda // add the excess over the baseline λ
			}
		}
	}

	// K(A, A): self-kernel.
	progress("    Computing K(A,A)...")
	kaa := lambda * nA * nA // baseline
	for _, nodes := range groupA {
		for i, na1 := range nodes {
			for j, na2 := range nodes {
				if i <= j { // symmetric: K(a,b) = K(b,a), compute once
					actual := subsetTreeKernel(na1, na2, lambda, memo)
					excess := actual - lambda
					if i == j {
						kaa += excess
					} else {
						kaa += 2 * excess // counted twice in the full sum
					}
				}
			}
		}
	}

	// K(B, B): self-kernel.
	progress("    Computing K(B,B)...")
	kbb := lambda * nB * nB
	for _, nodes := range groupB {
		for i, nb1 := range nodes {
			for j, nb2 := range nodes {
				if i <= j {
					actual := subsetTreeKernel(nb1, nb2, lambda, memo)
					excess := actual - lambda
					if i == j {
						kbb += excess
					} else {
						kbb += 2 * excess
					}
				}
			}
		}
	}

	progress(fmt.Sprintf("    %d memo entries", len(memo)))

	denom := math.Sqrt(kaa * kbb)
	if denom == 0 {
		return 0
	}
	result := kab / denom
	if result > 1.0 {
		result = 1.0
	}
	if result < 0 {
		result = 0
	}
	return result
}

// nodePairKey uniquely identifies a pair of tree nodes for memoization.
type nodePairKey struct {
	a, b *TreeNode
}

// subsetTreeKernel computes the kernel value between two nodes.
// Uses pointer-based memoization to avoid AHU hash collisions.
// Guaranteed to terminate: recursion always goes from parent to child
// in an acyclic tree, bounded by tree depth.
func subsetTreeKernel(a, b *TreeNode, lambda float64, memo map[nodePairKey]float64) float64 {
	key := nodePairKey{a, b}
	if v, ok := memo[key]; ok {
		return v
	}

	// Both leaves: they match as single-node subset trees.
	if len(a.Children) == 0 && len(b.Children) == 0 {
		memo[key] = lambda
		return lambda
	}

	// One is a leaf, the other is not.
	if len(a.Children) == 0 || len(b.Children) == 0 {
		memo[key] = lambda
		return lambda
	}

	// Both have children. Match children by AHU hash for unordered trees.
	matchedPairs := matchChildrenByHash(a.Children, b.Children)

	if len(matchedPairs) == 0 {
		memo[key] = lambda
		return lambda
	}

	product := 1.0
	for _, pair := range matchedPairs {
		product *= (1.0 + subsetTreeKernel(pair[0], pair[1], lambda, memo))
	}

	result := lambda * product
	memo[key] = result
	return result
}

// matchChildrenByHash matches children of two nodes by their AHU hash.
// Children with the same hash are paired greedily (largest first).
func matchChildrenByHash(childrenA, childrenB []*TreeNode) [][2]*TreeNode {
	if len(childrenA) == 0 || len(childrenB) == 0 {
		return nil
	}

	// Group B children by hash.
	bByHash := make(map[uint64][]*TreeNode, len(childrenB))
	for _, c := range childrenB {
		bByHash[c.AHUHash] = append(bByHash[c.AHUHash], c)
	}

	var pairs [][2]*TreeNode
	bUsed := make(map[*TreeNode]bool)

	// Sort A children by SubSize descending for better matching.
	sortedA := make([]*TreeNode, len(childrenA))
	copy(sortedA, childrenA)
	sort.Slice(sortedA, func(i, j int) bool {
		return sortedA[i].SubSize > sortedA[j].SubSize
	})

	for _, ca := range sortedA {
		candidates := bByHash[ca.AHUHash]
		for _, cb := range candidates {
			if !bUsed[cb] {
				pairs = append(pairs, [2]*TreeNode{ca, cb})
				bUsed[cb] = true
				break
			}
		}
	}

	return pairs
}

// groupNodesByHash groups tree nodes by their AHU hash.
func groupNodesByHash(nodes []*TreeNode) map[uint64][]*TreeNode {
	groups := make(map[uint64][]*TreeNode)
	for _, n := range nodes {
		groups[n.AHUHash] = append(groups[n.AHUHash], n)
	}
	return groups
}

// collectNodes returns all nodes, optionally depth-limited.
func collectNodes(ct *ContainmentTree, maxDepth int) []*TreeNode {
	if maxDepth <= 0 {
		return ct.AllNodes()
	}
	var result []*TreeNode
	visited := make(map[*TreeNode]bool)
	for _, root := range ct.Roots {
		collectDepthLimited(root, 0, maxDepth, &result, visited)
	}
	return result
}

func collectDepthLimited(node *TreeNode, depth, maxDepth int, result *[]*TreeNode, visited map[*TreeNode]bool) {
	if depth > maxDepth || visited[node] {
		return
	}
	visited[node] = true
	*result = append(*result, node)
	for _, child := range node.Children {
		collectDepthLimited(child, depth+1, maxDepth, result, visited)
	}
}
