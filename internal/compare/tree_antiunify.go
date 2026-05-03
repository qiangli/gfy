package compare

import (
	"math"
	"sort"
)

// AntiUnifyCoverage computes the anti-unification coverage score between
// two containment trees. Anti-unification finds the most specific
// generalization (MSG) — the largest tree structure common to both inputs.
// Where subtrees differ, they are replaced by wildcards.
//
// Score: |anti-unifier| / max(|A|, |B|) → [0,1].
// 1.0 means identical structure, 0.0 means completely different.
//
// Complexity: O(n * m) in the worst case.
func AntiUnifyCoverage(a, b *ContainmentTree) float64 {
	if a.Size == 0 && b.Size == 0 {
		return 1.0
	}
	if a.Size == 0 || b.Size == 0 {
		return 0
	}

	auSize := 0
	rootsA := a.Roots
	rootsB := b.Roots

	// Match roots by AHU hash first, then greedily by size.
	matched := matchTreeNodesByHash(rootsA, rootsB)
	for _, pair := range matched {
		auSize += antiUnifySize(pair[0], pair[1])
	}

	maxSize := math.Max(float64(a.Size), float64(b.Size))
	if maxSize == 0 {
		return 1.0
	}
	return float64(auSize) / maxSize
}

// antiUnifySize computes the size of the anti-unifier of two subtrees.
// The anti-unifier keeps a node if both trees have it (structurally),
// and replaces differing subtrees with wildcards (size 0).
func antiUnifySize(a, b *TreeNode) int {
	// The root nodes match (they're both present).
	size := 1

	if len(a.Children) == 0 || len(b.Children) == 0 {
		return size
	}

	// Match children: prefer exact AHU hash matches, then greedy by size.
	matched := matchTreeNodesByHash(a.Children, b.Children)
	for _, pair := range matched {
		size += antiUnifySize(pair[0], pair[1])
	}

	return size
}

// matchTreeNodesByHash matches nodes from two lists by AHU hash.
// Exact hash matches are paired first (largest subtrees preferred).
// Remaining unmatched nodes are paired greedily by subtree size similarity.
func matchTreeNodesByHash(nodesA, nodesB []*TreeNode) [][2]*TreeNode {
	var pairs [][2]*TreeNode

	// Group by AHU hash.
	bByHash := make(map[uint64][]*TreeNode)
	for _, n := range nodesB {
		bByHash[n.AHUHash] = append(bByHash[n.AHUHash], n)
	}

	usedA := make(map[*TreeNode]bool)
	usedB := make(map[*TreeNode]bool)

	// Phase 1: exact hash matches, largest first.
	sortedA := make([]*TreeNode, len(nodesA))
	copy(sortedA, nodesA)
	sort.Slice(sortedA, func(i, j int) bool {
		return sortedA[i].SubSize > sortedA[j].SubSize
	})

	for _, na := range sortedA {
		candidates := bByHash[na.AHUHash]
		for _, nb := range candidates {
			if !usedB[nb] {
				pairs = append(pairs, [2]*TreeNode{na, nb})
				usedA[na] = true
				usedB[nb] = true
				break
			}
		}
	}

	// Phase 2: greedy match remaining by similar subtree size.
	var remainA, remainB []*TreeNode
	for _, n := range nodesA {
		if !usedA[n] {
			remainA = append(remainA, n)
		}
	}
	for _, n := range nodesB {
		if !usedB[n] {
			remainB = append(remainB, n)
		}
	}

	// Sort remaining by size.
	sort.Slice(remainA, func(i, j int) bool {
		return remainA[i].SubSize > remainA[j].SubSize
	})
	sort.Slice(remainB, func(i, j int) bool {
		return remainB[i].SubSize > remainB[j].SubSize
	})

	usedB2 := make(map[int]bool)
	for _, na := range remainA {
		bestJ := -1
		bestDiff := math.MaxInt32
		for j, nb := range remainB {
			if usedB2[j] {
				continue
			}
			diff := na.SubSize - nb.SubSize
			if diff < 0 {
				diff = -diff
			}
			if diff < bestDiff {
				bestDiff = diff
				bestJ = j
			}
		}
		if bestJ >= 0 {
			pairs = append(pairs, [2]*TreeNode{na, remainB[bestJ]})
			usedB2[bestJ] = true
		}
	}

	return pairs
}
