package compare

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
)

// ComputeAHU performs AHU (Aho-Hopcroft-Ullman) canonical hashing on a
// containment tree. Leaves get hash 0. Internal nodes get
// FNV-64a(sorted(child_hashes)). Two subtrees are isomorphic iff their
// AHU hashes are equal. Also computes SubSize for each node.
//
// Complexity: O(n) where n is the total number of nodes.
func ComputeAHU(ct *ContainmentTree) {
	visited := make(map[*TreeNode]bool)
	for _, root := range ct.Roots {
		computeAHUNode(root, visited)
	}
}

func computeAHUNode(node *TreeNode, visited map[*TreeNode]bool) {
	if visited[node] {
		return
	}
	visited[node] = true

	if len(node.Children) == 0 {
		node.AHUHash = 0
		node.SubSize = 1
		return
	}

	childHashes := make([]uint64, len(node.Children))
	node.SubSize = 1
	for i, child := range node.Children {
		computeAHUNode(child, visited)
		childHashes[i] = child.AHUHash
		node.SubSize += child.SubSize
	}

	sort.Slice(childHashes, func(i, j int) bool {
		return childHashes[i] < childHashes[j]
	})

	h := fnv.New64a()
	buf := make([]byte, 8)
	for _, ch := range childHashes {
		binary.LittleEndian.PutUint64(buf, ch)
		h.Write(buf)
	}
	node.AHUHash = h.Sum64()
}

// ComputeSemanticAHU performs semantic-aware AHU hashing. Unlike standard AHU
// where all leaves hash to 0, this variant incorporates NodeType and behavioral
// Tags into the hash. This makes the hashing rename-invariant while still
// distinguishing nodes with different functional roles:
//   - A leaf function tagged [logs, fs] hashes differently from one tagged [throws, net]
//   - Two functions with identical tags and tree position hash the same regardless of name
//
// This improves cross-project comparison where nodes have different names but
// the same behavioral patterns. All AHU-dependent algorithms (AHU subtree match,
// MCS, kernel, anti-unification, subtree frequency) benefit automatically.
func ComputeSemanticAHU(ct *ContainmentTree) {
	visited := make(map[*TreeNode]bool)
	for _, root := range ct.Roots {
		computeSemanticAHUNode(root, visited)
	}
}

func computeSemanticAHUNode(node *TreeNode, visited map[*TreeNode]bool) {
	if visited[node] {
		return
	}
	visited[node] = true

	// Compute children first.
	node.SubSize = 1
	childHashes := make([]uint64, 0, len(node.Children))
	for _, child := range node.Children {
		computeSemanticAHUNode(child, visited)
		childHashes = append(childHashes, child.AHUHash)
		node.SubSize += child.SubSize
	}

	// Build semantic hash: NodeType + sorted Tags + sorted child hashes.
	h := fnv.New64a()
	h.Write([]byte(node.NodeType))
	h.Write([]byte("|"))
	for _, tag := range node.Tags { // Tags are already sorted in ExtractContainmentTree
		h.Write([]byte(tag))
		h.Write([]byte(","))
	}

	if len(childHashes) > 0 {
		sort.Slice(childHashes, func(i, j int) bool {
			return childHashes[i] < childHashes[j]
		})
		h.Write([]byte("|"))
		buf := make([]byte, 8)
		for _, ch := range childHashes {
			binary.LittleEndian.PutUint64(buf, ch)
			h.Write(buf)
		}
	}

	node.AHUHash = h.Sum64()
}

// AHUSubtreeMatchScore computes the fraction of matching subtree hashes
// between two trees. Uses multiset Jaccard on the subtree hash distributions.
//
// Score range: [0, 1]. 1.0 means identical tree structure.
func AHUSubtreeMatchScore(a, b *ContainmentTree) float64 {
	freqA := subtreeHashFreq(a)
	freqB := subtreeHashFreq(b)

	if len(freqA) == 0 && len(freqB) == 0 {
		return 1.0
	}

	// Multiset Jaccard: intersection / union.
	allHashes := make(map[uint64]bool)
	for h := range freqA {
		allHashes[h] = true
	}
	for h := range freqB {
		allHashes[h] = true
	}

	intersection := 0
	union := 0
	for h := range allHashes {
		ca, cb := freqA[h], freqB[h]
		if ca < cb {
			intersection += ca
		} else {
			intersection += cb
		}
		if ca > cb {
			union += ca
		} else {
			union += cb
		}
	}

	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// subtreeHashFreq builds a frequency map of AHU hashes for all subtrees.
func subtreeHashFreq(ct *ContainmentTree) map[uint64]int {
	freq := make(map[uint64]int)
	for _, node := range ct.AllNodes() {
		freq[node.AHUHash]++
	}
	return freq
}
