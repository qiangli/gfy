package compare

import (
	"math"
	"sort"
)

// MaxCommonSubtreeSize computes the size of the maximum common subtree
// between two containment trees. Leverages AHU hashes: subtrees with
// identical hashes are isomorphic. Greedily matches largest subtrees first,
// excluding descendants of already-matched subtrees.
//
// AHU hashes must be computed before calling this function.
//
// Complexity: O(n log n) dominated by sorting.
func MaxCommonSubtreeSize(a, b *ContainmentTree) int {
	// Group nodes by AHU hash.
	groupA := groupByAHUHash(a)
	groupB := groupByAHUHash(b)

	// Find common hashes.
	commonHashes := make([]uint64, 0)
	for h := range groupA {
		if _, ok := groupB[h]; ok {
			commonHashes = append(commonHashes, h)
		}
	}

	// Sort by subtree size descending (match largest subtrees first).
	sort.Slice(commonHashes, func(i, j int) bool {
		sizeI := groupA[commonHashes[i]][0].SubSize
		sizeJ := groupA[commonHashes[j]][0].SubSize
		return sizeI > sizeJ
	})

	usedA := make(map[*TreeNode]bool)
	usedB := make(map[*TreeNode]bool)
	commonSize := 0

	for _, h := range commonHashes {
		nodesA := filterUnused(groupA[h], usedA)
		nodesB := filterUnused(groupB[h], usedB)

		// Greedily match available nodes.
		matches := min(len(nodesA), len(nodesB))
		for i := 0; i < matches; i++ {
			commonSize += nodesA[i].SubSize
			markDescendants(nodesA[i], usedA)
			markDescendants(nodesB[i], usedB)
		}
	}

	return commonSize
}

// MaxCommonSubtreeScore returns the MCS ratio as a [0,1] similarity score.
func MaxCommonSubtreeScore(a, b *ContainmentTree) float64 {
	if a.Size == 0 && b.Size == 0 {
		return 1.0
	}
	mcs := MaxCommonSubtreeSize(a, b)
	maxSize := math.Max(float64(a.Size), float64(b.Size))
	if maxSize == 0 {
		return 1.0
	}
	return float64(mcs) / maxSize
}

func groupByAHUHash(ct *ContainmentTree) map[uint64][]*TreeNode {
	groups := make(map[uint64][]*TreeNode)
	for _, node := range ct.AllNodes() {
		groups[node.AHUHash] = append(groups[node.AHUHash], node)
	}
	// Sort each group by SubSize descending.
	for _, nodes := range groups {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].SubSize > nodes[j].SubSize
		})
	}
	return groups
}

func filterUnused(nodes []*TreeNode, used map[*TreeNode]bool) []*TreeNode {
	var result []*TreeNode
	for _, n := range nodes {
		if !used[n] {
			result = append(result, n)
		}
	}
	return result
}

func markDescendants(node *TreeNode, used map[*TreeNode]bool) {
	if used[node] {
		return // already marked or cycle
	}
	used[node] = true
	for _, child := range node.Children {
		markDescendants(child, used)
	}
}
