// Package trace finds call paths leading to nodes with specific behavioral tags.
package trace

import (
	"github.com/qiangli/gfy/internal/graph"
)

// CallChain represents a path from a root caller to a tagged node.
type CallChain struct {
	Path []ChainNode // from root caller to tagged node
	Tag  string      // the tag that triggered this trace
}

// ChainNode is a single node in a call chain.
type ChainNode struct {
	ID    string
	Label string
}

// TraceTag finds all call paths that lead to nodes tagged with the given tag.
// It walks calls edges backwards from each tagged node to find callers,
// building chains up to maxDepth hops. Returns at most maxResults chains.
func TraceTag(g *graph.Graph, tag string, maxDepth, maxResults int) []CallChain {
	if maxDepth <= 0 {
		maxDepth = 10
	}
	if maxResults <= 0 {
		maxResults = 20
	}

	// Find all nodes matching the requested tag.
	var taggedNodes []string
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		if matchesTag(attrs, tag) {
			taggedNodes = append(taggedNodes, id)
		}
	}

	// Build reverse call graph: callee → []callers.
	// Use _src/_tgt edge attributes for direction in undirected graphs.
	reverseCallGraph := buildReverseCallGraph(g)

	var chains []CallChain
	for _, target := range taggedNodes {
		if len(chains) >= maxResults {
			break
		}
		// BFS backwards from target to find all call paths.
		paths := traceBack(g, reverseCallGraph, target, maxDepth)
		for _, path := range paths {
			if len(chains) >= maxResults {
				break
			}
			chains = append(chains, CallChain{
				Path: path,
				Tag:  tag,
			})
		}
	}

	return chains
}

// buildReverseCallGraph builds a map from callee → list of callers
// by looking at edges with relation "calls".
func buildReverseCallGraph(g *graph.Graph) map[string][]string {
	reverse := make(map[string][]string)
	for _, e := range g.Edges() {
		attrs := e.Attrs
		rel, _ := attrs["relation"].(string)
		if rel != "calls" {
			continue
		}
		// Use _src/_tgt for direction.
		src, _ := attrs["_src"].(string)
		tgt, _ := attrs["_tgt"].(string)
		if src == "" || tgt == "" {
			src = e.Source
			tgt = e.Target
		}
		reverse[tgt] = append(reverse[tgt], src)
	}
	return reverse
}

// traceBack walks backwards from target through the reverse call graph,
// returning all paths from root callers to the target.
func traceBack(g *graph.Graph, reverseCallGraph map[string][]string, target string, maxDepth int) [][]ChainNode {
	type frame struct {
		id   string
		path []ChainNode
	}

	targetAttrs := g.NodeAttrs(target)
	startNode := ChainNode{
		ID:    target,
		Label: nodeLabel(targetAttrs),
	}

	queue := []frame{{id: target, path: []ChainNode{startNode}}}
	var results [][]ChainNode
	visited := map[string]bool{target: true}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		callers := reverseCallGraph[current.id]
		if len(callers) == 0 {
			// This is a root — no one calls it. Output the path (reversed).
			if len(current.path) > 1 {
				reversed := make([]ChainNode, len(current.path))
				for i, n := range current.path {
					reversed[len(current.path)-1-i] = n
				}
				results = append(results, reversed)
			}
			continue
		}

		if len(current.path) >= maxDepth {
			// Hit depth limit — output what we have.
			reversed := make([]ChainNode, len(current.path))
			for i, n := range current.path {
				reversed[len(current.path)-1-i] = n
			}
			results = append(results, reversed)
			continue
		}

		expanded := false
		for _, caller := range callers {
			if visited[caller] {
				continue
			}
			visited[caller] = true
			expanded = true
			callerAttrs := g.NodeAttrs(caller)
			newPath := make([]ChainNode, len(current.path)+1)
			copy(newPath, current.path)
			newPath[len(current.path)] = ChainNode{
				ID:    caller,
				Label: nodeLabel(callerAttrs),
			}
			queue = append(queue, frame{id: caller, path: newPath})
		}
		if !expanded && len(current.path) > 1 {
			reversed := make([]ChainNode, len(current.path))
			for i, n := range current.path {
				reversed[len(current.path)-1-i] = n
			}
			results = append(results, reversed)
		}
	}

	return results
}

func matchesTag(attrs map[string]any, tag string) bool {
	return hasTag(attrs, tag)
}

func hasTag(attrs map[string]any, tag string) bool {
	tags, ok := attrs["tags"]
	if !ok {
		return false
	}
	switch t := tags.(type) {
	case []string:
		for _, v := range t {
			if v == tag {
				return true
			}
		}
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s == tag {
				return true
			}
		}
	}
	return false
}

func nodeLabel(attrs map[string]any) string {
	if attrs == nil {
		return ""
	}
	if label, ok := attrs["label"].(string); ok {
		return label
	}
	return ""
}
