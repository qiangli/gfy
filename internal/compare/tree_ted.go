package compare

// SemanticMatchCost computes the match cost between two tree nodes based on
// their behavioral profiles. Returns 0 for identical profiles, up to 1 for
// completely different ones. This makes TED sensitive to behavioral changes
// (not just tree shape) for cross-project comparison.
func SemanticMatchCost(a, b *TreeNode) float64 {
	if a.NodeType != b.NodeType {
		return 1.0
	}
	if len(a.Tags) == 0 && len(b.Tags) == 0 {
		return 0
	}
	// Tag Jaccard distance.
	aSet := make(map[string]bool, len(a.Tags))
	for _, t := range a.Tags {
		aSet[t] = true
	}
	bSet := make(map[string]bool, len(b.Tags))
	for _, t := range b.Tags {
		bSet[t] = true
	}
	inter := 0
	for t := range aSet {
		if bSet[t] {
			inter++
		}
	}
	union := len(aSet) + len(bSet) - inter
	if union == 0 {
		return 0
	}
	return 1.0 - float64(inter)/float64(union)
}

// TreeEditDistance computes the tree edit distance between two containment trees.
// When semantic is false, uses label-agnostic costs (match=0, insert=delete=1).
// When semantic is true, match cost is based on node behavioral similarity
// (NodeType + Tags), making TED sensitive to functional differences.
//
// For forests (multiple roots), we create a virtual root with children = roots,
// then compute TED on the virtual-rooted trees.
//
// For trees where |A|*|B| > tedApproxThreshold, falls back to depth-sampled
// approximation.
//
// Complexity: O(n² * m) for exact computation.
func TreeEditDistance(a, b *ContainmentTree, semantic bool) int {
	if a.Size == 0 && b.Size == 0 {
		return 0
	}
	if a.Size == 0 {
		return b.Size
	}
	if b.Size == 0 {
		return a.Size
	}

	// Create virtual roots for forests.
	rootA := virtualRoot(a)
	rootB := virtualRoot(b)

	// For large trees, approximate.
	if rootA.SubSize*rootB.SubSize > tedApproxThreshold {
		return approxTED(rootA, rootB)
	}

	return zhangShasha(rootA, rootB, semantic)
}

const tedApproxThreshold = 25_000_000 // 5K × 5K

// TreeEditDistanceSimilarity converts TED to a [0,1] similarity score.
func TreeEditDistanceSimilarity(a, b *ContainmentTree, semantic bool) float64 {
	ted := TreeEditDistance(a, b, semantic)
	total := a.Size + b.Size
	if total == 0 {
		return 1.0
	}
	sim := 1.0 - float64(ted)/float64(total)
	if sim < 0 {
		sim = 0
	}
	return sim
}

func virtualRoot(ct *ContainmentTree) *TreeNode {
	if len(ct.Roots) == 1 {
		return ct.Roots[0]
	}
	vr := &TreeNode{
		ID:       "__virtual_root__",
		NodeType: "virtual",
		Children: ct.Roots,
		SubSize:  ct.Size + 1,
	}
	return vr
}

// zhangShasha implements the Zhang-Shasha tree edit distance algorithm.
// When semantic is false, match cost = 0 (label-agnostic).
// When semantic is true, match cost = 1 if nodes have different NodeType or Tags, 0 otherwise.
func zhangShasha(a, b *TreeNode, semantic bool) int {
	// Collect nodes in post-order.
	nodesA := postOrder(a)
	nodesB := postOrder(b)
	nA := len(nodesA)
	nB := len(nodesB)

	// Assign post-order indices.
	indexA := make(map[*TreeNode]int)
	indexB := make(map[*TreeNode]int)
	for i, n := range nodesA {
		indexA[n] = i
	}
	for i, n := range nodesB {
		indexB[n] = i
	}

	// Compute leftmost leaf descendant for each node.
	lA := make([]int, nA)
	lB := make([]int, nB)
	for i, n := range nodesA {
		lA[i] = leftmostLeaf(n, indexA)
	}
	for i, n := range nodesB {
		lB[i] = leftmostLeaf(n, indexB)
	}

	// Find key roots: nodes where l(i) != l(parent(i)).
	keyRootsA := keyRoots(nodesA, lA, indexA)
	keyRootsB := keyRoots(nodesB, lB, indexB)

	// Main DP table.
	td := make([][]int, nA+1)
	for i := range td {
		td[i] = make([]int, nB+1)
	}

	// Forest distance table (reused per key root pair).
	for _, krA := range keyRootsA {
		for _, krB := range keyRootsB {
			computeForestDist(nodesA, nodesB, krA, krB, lA, lB, td, semantic)
		}
	}

	return td[nA][nB]
}

func postOrder(root *TreeNode) []*TreeNode {
	var result []*TreeNode
	visited := make(map[*TreeNode]bool)
	var walk func(*TreeNode)
	walk = func(n *TreeNode) {
		if visited[n] {
			return
		}
		visited[n] = true
		for _, c := range n.Children {
			walk(c)
		}
		result = append(result, n)
	}
	walk(root)
	return result
}

func leftmostLeaf(node *TreeNode, index map[*TreeNode]int) int {
	n := node
	visited := make(map[*TreeNode]bool)
	for len(n.Children) > 0 {
		if visited[n] {
			break // cycle detected
		}
		visited[n] = true
		n = n.Children[0]
	}
	return index[n]
}

func keyRoots(nodes []*TreeNode, l []int, index map[*TreeNode]int) []int {
	visited := make(map[int]bool)
	var roots []int
	for i := len(nodes) - 1; i >= 0; i-- {
		li := l[i]
		if !visited[li] {
			visited[li] = true
			roots = append(roots, i)
		}
	}
	// Reverse to process in ascending order.
	for i, j := 0, len(roots)-1; i < j; i, j = i+1, j-1 {
		roots[i], roots[j] = roots[j], roots[i]
	}
	return roots
}

func computeForestDist(nodesA, nodesB []*TreeNode, iRoot, jRoot int, lA, lB []int, td [][]int, semantic bool) {
	liRoot := lA[iRoot]
	ljRoot := lB[jRoot]

	m := iRoot - liRoot + 2
	n := jRoot - ljRoot + 2

	fd := make([][]int, m)
	for i := range fd {
		fd[i] = make([]int, n)
	}

	fd[0][0] = 0
	for i := 1; i < m; i++ {
		fd[i][0] = fd[i-1][0] + 1 // delete
	}
	for j := 1; j < n; j++ {
		fd[0][j] = fd[0][j-1] + 1 // insert
	}

	for i := 1; i < m; i++ {
		for j := 1; j < n; j++ {
			ai := liRoot + i - 1 // actual index in nodesA
			bj := ljRoot + j - 1 // actual index in nodesB

			costDel := fd[i-1][j] + 1
			costIns := fd[i][j-1] + 1

			if lA[ai] == liRoot && lB[bj] == ljRoot {
				// Both are rooted at the key root's leftmost leaf boundary.
				matchCost := 0
				if semantic && semanticMatchCostInt(nodesA[ai], nodesB[bj]) > 0 {
					matchCost = 1
				}
				costMatch := fd[i-1][j-1] + matchCost
				fd[i][j] = min3(costDel, costIns, costMatch)
				td[ai+1][bj+1] = fd[i][j]
			} else {
				// Use previously computed tree distance.
				costMatch := fd[lA[ai]-liRoot][lB[bj]-ljRoot] + td[ai+1][bj+1]
				fd[i][j] = min3(costDel, costIns, costMatch)
			}
		}
	}

	td[iRoot+1][jRoot+1] = fd[m-1][n-1]
}

// semanticMatchCostInt returns 0 if two nodes have the same NodeType and Tags, 1 otherwise.
func semanticMatchCostInt(a, b *TreeNode) int {
	if a.NodeType != b.NodeType {
		return 1
	}
	if len(a.Tags) != len(b.Tags) {
		return 1
	}
	for i, t := range a.Tags {
		if t != b.Tags[i] {
			return 1
		}
	}
	return 0
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// approxTED approximates tree edit distance for large trees by comparing
// subtrees at depth boundaries.
func approxTED(a, b *TreeNode) int {
	// Flatten both trees to depth-limited summaries.
	sigA := depthSignature(a, 4)
	sigB := depthSignature(b, 4)

	// Count mismatches in depth signatures.
	allKeys := make(map[uint64]bool)
	for k := range sigA {
		allKeys[k] = true
	}
	for k := range sigB {
		allKeys[k] = true
	}

	diff := 0
	for k := range allKeys {
		ca, cb := sigA[k], sigB[k]
		if ca > cb {
			diff += ca - cb
		} else {
			diff += cb - ca
		}
	}
	return diff
}

func depthSignature(node *TreeNode, maxDepth int) map[uint64]int {
	sig := make(map[uint64]int)
	visited := make(map[*TreeNode]bool)
	var walk func(*TreeNode, int)
	walk = func(n *TreeNode, depth int) {
		if visited[n] || depth >= maxDepth {
			sig[n.AHUHash]++
			return
		}
		visited[n] = true
		for _, c := range n.Children {
			walk(c, depth+1)
		}
		if len(n.Children) == 0 {
			sig[n.AHUHash]++
		}
	}
	walk(node, 0)
	return sig
}
