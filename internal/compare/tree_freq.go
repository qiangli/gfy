package compare

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"
)

// SubtreeFrequencyCosine computes the cosine similarity between subtree
// frequency vectors of two containment trees. Each vector is built by
// enumerating all depth-limited subtree shapes (identified by their AHU hash
// at each depth from 1 to maxDepth) and counting occurrences.
//
// This is the approach used by Deckard (Jiang et al., 2007) for code clone
// detection. It captures the distribution of local structural patterns.
//
// When semantic is true, leaf hashes incorporate NodeType and Tags for
// rename-invariant comparison.
//
// Complexity: O(n * maxDepth) per tree.
func SubtreeFrequencyCosine(a, b *ContainmentTree, maxDepth int, semantic bool) float64 {
	if maxDepth <= 0 {
		maxDepth = 4
	}

	vecA := buildFrequencyVector(a, maxDepth, semantic)
	vecB := buildFrequencyVector(b, maxDepth, semantic)

	return cosineSimVec(vecA, vecB)
}

// buildFrequencyVector builds a frequency vector keyed by depth-limited AHU
// hashes. For each node and each depth d in [1, maxDepth], computes the
// canonical hash of the subtree rooted at that node truncated at depth d.
func buildFrequencyVector(ct *ContainmentTree, maxDepth int, semantic bool) map[uint64]int {
	vec := make(map[uint64]int)
	for _, node := range ct.AllNodes() {
		for d := 1; d <= maxDepth; d++ {
			h := depthLimitedHash(node, d, 0, semantic)
			vec[h]++
		}
	}
	return vec
}

// depthLimitedHash computes an AHU-style hash for a subtree truncated at
// the given maximum depth. At the depth limit, all nodes are treated as leaves.
// When the tree's SemanticAHU flag is set (via ComputeSemanticAHU), leaf hashes
// incorporate NodeType and Tags for rename-invariant comparison.
func depthLimitedHash(node *TreeNode, maxDepth, currentDepth int, semantic bool) uint64 {
	if currentDepth >= maxDepth || len(node.Children) == 0 {
		if semantic {
			return semanticLeafHash(node)
		}
		return 0 // leaf or depth limit reached
	}

	childHashes := make([]uint64, 0, len(node.Children))
	for _, child := range node.Children {
		if child == node {
			continue // self-loop guard
		}
		childHashes = append(childHashes, depthLimitedHash(child, maxDepth, currentDepth+1, semantic))
	}
	if len(childHashes) == 0 {
		if semantic {
			return semanticLeafHash(node)
		}
		return 0
	}

	sort.Slice(childHashes, func(i, j int) bool {
		return childHashes[i] < childHashes[j]
	})

	h := fnv.New64a()
	// Include the depth in the hash to distinguish same shapes at different depths.
	if semantic {
		h.Write([]byte(node.NodeType))
		h.Write([]byte("|"))
		for _, tag := range node.Tags {
			h.Write([]byte(tag))
			h.Write([]byte(","))
		}
		h.Write([]byte("|"))
	}
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(currentDepth))
	h.Write(buf)
	for _, ch := range childHashes {
		binary.LittleEndian.PutUint64(buf, ch)
		h.Write(buf)
	}
	return h.Sum64()
}

// semanticLeafHash computes a hash for a leaf node based on its NodeType and Tags.
func semanticLeafHash(node *TreeNode) uint64 {
	h := fnv.New64a()
	h.Write([]byte(node.NodeType))
	h.Write([]byte("|"))
	for _, tag := range node.Tags {
		h.Write([]byte(tag))
		h.Write([]byte(","))
	}
	return h.Sum64()
}

// cosineSimVec computes cosine similarity between two sparse vectors.
func cosineSimVec(a, b map[uint64]int) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0

	for k, v := range a {
		normA += float64(v) * float64(v)
		if bv, ok := b[k]; ok {
			dot += float64(v) * float64(bv)
		}
	}
	for _, v := range b {
		normB += float64(v) * float64(v)
	}

	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)

	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}
