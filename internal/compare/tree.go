package compare

import (
	"sort"

	"github.com/qiangli/gfy/internal/graph"
)

// TreeNode represents a node in the containment tree extracted from a
// knowledge graph. The containment tree is formed by "contains" and "method"
// edges, capturing the file → class → function/method hierarchy.
type TreeNode struct {
	ID       string      // original graph node ID
	NodeType string      // inferred: file/function/method/class/import/entity
	Degree   int         // degree in the full graph (not the tree)
	Tags     []string    // behavioral tags (throws, logs, fs, net, etc.)
	Arity    int         // outgoing "calls" edge count in full graph
	Parent   *TreeNode   // nil for roots
	Children []*TreeNode // sorted by ID for determinism

	// Populated by AHU algorithm.
	AHUHash uint64 // canonical structural hash
	SubSize int    // number of nodes in this subtree (including self)

	// Post-order index, populated during traversal.
	PostOrder int
}

// ContainmentTree is a forest of TreeNode roots extracted from a knowledge graph.
type ContainmentTree struct {
	Roots   []*TreeNode          // root nodes (typically file nodes)
	NodeMap map[string]*TreeNode // graph node ID → tree node
	Size    int                  // total node count
}

// AllNodes returns all nodes in the tree in post-order.
func (ct *ContainmentTree) AllNodes() []*TreeNode {
	var nodes []*TreeNode
	visited := make(map[*TreeNode]bool)
	for _, root := range ct.Roots {
		collectPostOrder(root, &nodes, visited)
	}
	return nodes
}

func collectPostOrder(node *TreeNode, result *[]*TreeNode, visited map[*TreeNode]bool) {
	if visited[node] {
		return
	}
	visited[node] = true
	for _, child := range node.Children {
		collectPostOrder(child, result, visited)
	}
	*result = append(*result, node)
}

// ExtractContainmentTree builds a containment tree from a knowledge graph
// by filtering to "contains" and "method" edges. Nodes not connected by
// these edges become singleton roots.
func ExtractContainmentTree(g *graph.Graph) *ContainmentTree {
	ct := &ContainmentTree{
		NodeMap: make(map[string]*TreeNode),
	}

	// Single-pass edge scan: precompute containment, call arity, and
	// per-node directed edges for type inference. O(e) total.
	type parentEdge struct {
		parent   string
		relation string
	}
	childToParent := make(map[string]parentEdge)
	parentToChildren := make(map[string][]string)
	callArity := make(map[string]int)
	nodeInEdges := make(map[string][]dirEdge)
	nodeOutEdges := make(map[string][]dirEdge)

	for _, e := range g.Edges() {
		rel, _ := e.Attrs["relation"].(string)
		src, tgt := e.Source, e.Target
		if s, ok := e.Attrs["_src"].(string); ok {
			src = s
		}
		if t, ok := e.Attrs["_tgt"].(string); ok {
			tgt = t
		}

		// Track directed edges for type inference.
		nodeOutEdges[src] = append(nodeOutEdges[src], dirEdge{tgt, rel})
		nodeInEdges[tgt] = append(nodeInEdges[tgt], dirEdge{src, rel})

		if rel == "contains" || rel == "method" {
			childToParent[tgt] = parentEdge{src, rel}
			parentToChildren[src] = append(parentToChildren[src], tgt)
		}
		if rel == "calls" {
			callArity[src]++
		}
	}

	// Build all tree nodes. O(n).
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		label, _ := attrs["label"].(string)

		tn := &TreeNode{
			ID:       id,
			NodeType: inferNodeType(label, nodeInEdges[id], nodeOutEdges[id]),
			Degree:   g.Degree(id),
			Arity:    callArity[id],
		}

		if tags, ok := attrs["tags"]; ok {
			tn.Tags = extractStringSlice(tags)
			sort.Strings(tn.Tags)
		}

		ct.NodeMap[id] = tn
	}

	// Wire parent-child relationships, detecting and breaking cycles.
	for childID, pe := range childToParent {
		child := ct.NodeMap[childID]
		parent := ct.NodeMap[pe.parent]
		if child == nil || parent == nil {
			continue
		}
		// Cycle check: walk up from parent to ensure child isn't an ancestor.
		cycle := false
		for p := parent; p != nil; p = p.Parent {
			if p == child {
				cycle = true
				break
			}
		}
		if cycle {
			continue
		}
		child.Parent = parent
		parent.Children = append(parent.Children, child)
	}

	// Sort children by ID for determinism.
	for _, tn := range ct.NodeMap {
		sort.Slice(tn.Children, func(i, j int) bool {
			return tn.Children[i].ID < tn.Children[j].ID
		})
	}

	// Find roots (nodes with no parent in the containment tree).
	for _, tn := range ct.NodeMap {
		if tn.Parent == nil {
			ct.Roots = append(ct.Roots, tn)
		}
	}
	sort.Slice(ct.Roots, func(i, j int) bool {
		return ct.Roots[i].ID < ct.Roots[j].ID
	})

	ct.Size = len(ct.NodeMap)

	// Assign post-order indices.
	idx := 0
	poVisited := make(map[*TreeNode]bool)
	for _, root := range ct.Roots {
		assignPostOrder(root, &idx, poVisited)
	}

	return ct
}

func assignPostOrder(node *TreeNode, idx *int, visited map[*TreeNode]bool) {
	if visited[node] {
		return
	}
	visited[node] = true
	for _, child := range node.Children {
		assignPostOrder(child, idx, visited)
	}
	node.PostOrder = *idx
	*idx++
}
