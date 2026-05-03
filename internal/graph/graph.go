// Package graph provides an adjacency-list graph with string-keyed nodes and
// arbitrary attribute maps, matching the NetworkX dict-of-dicts semantics.
package graph

import (
	"fmt"
	"sort"
)

// Graph is a string-keyed graph with node and edge attributes.
type Graph struct {
	nodes    map[string]map[string]any            // nodeID → attributes
	adj      map[string]map[string]map[string]any // src → tgt → edge attrs
	directed bool
	Metadata map[string]any // graph-level metadata (hyperedges, etc.)
}

// New creates an empty graph (undirected by default).
func New(directed bool) *Graph {
	return &Graph{
		nodes:    make(map[string]map[string]any),
		adj:      make(map[string]map[string]map[string]any),
		directed: directed,
		Metadata: make(map[string]any),
	}
}

// IsDirected returns whether the graph is directed.
func (g *Graph) IsDirected() bool { return g.directed }

// AddNode adds a node with attributes. If the node already exists, attributes
// are overwritten (matching NetworkX behavior).
func (g *Graph) AddNode(id string, attrs map[string]any) {
	if _, ok := g.nodes[id]; !ok {
		g.nodes[id] = make(map[string]any)
		if g.adj[id] == nil {
			g.adj[id] = make(map[string]map[string]any)
		}
	}
	for k, v := range attrs {
		g.nodes[id][k] = v
	}
}

// AddEdge adds an edge between source and target with attributes.
// For undirected graphs, the edge is stored in both directions.
func (g *Graph) AddEdge(source, target string, attrs map[string]any) {
	// Ensure both nodes exist.
	if _, ok := g.nodes[source]; !ok {
		g.AddNode(source, nil)
	}
	if _, ok := g.nodes[target]; !ok {
		g.AddNode(target, nil)
	}

	if g.adj[source] == nil {
		g.adj[source] = make(map[string]map[string]any)
	}
	g.adj[source][target] = copyAttrs(attrs)

	if !g.directed {
		if g.adj[target] == nil {
			g.adj[target] = make(map[string]map[string]any)
		}
		g.adj[target][source] = copyAttrs(attrs)
	}
}

// HasNode returns true if the node exists.
func (g *Graph) HasNode(id string) bool {
	_, ok := g.nodes[id]
	return ok
}

// NodeAttrs returns the attribute map for a node (nil if not found).
func (g *Graph) NodeAttrs(id string) map[string]any {
	return g.nodes[id]
}

// Nodes returns all node IDs in sorted order.
func (g *Graph) Nodes() []string {
	ids := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// NodeCount returns the number of nodes.
func (g *Graph) NodeCount() int { return len(g.nodes) }

// EdgeCount returns the number of edges. For undirected graphs, each edge is
// counted once.
func (g *Graph) EdgeCount() int {
	count := 0
	for _, targets := range g.adj {
		count += len(targets)
	}
	if !g.directed {
		count /= 2
	}
	return count
}

// EdgeData represents an edge with its source, target, and attributes.
type EdgeData struct {
	Source string
	Target string
	Attrs  map[string]any
}

// Edges returns all edges. For undirected graphs, each edge appears once
// (with source < target lexicographically).
func (g *Graph) Edges() []EdgeData {
	seen := make(map[string]bool)
	var edges []EdgeData
	for src, targets := range g.adj {
		for tgt, attrs := range targets {
			key := src + "\x00" + tgt
			if !g.directed {
				if src > tgt {
					key = tgt + "\x00" + src
				}
				if seen[key] {
					continue
				}
				seen[key] = true
			}
			edges = append(edges, EdgeData{Source: src, Target: tgt, Attrs: attrs})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		return edges[i].Target < edges[j].Target
	})
	return edges
}

// EdgeAttrs returns the attributes for a specific edge (nil if not found).
func (g *Graph) EdgeAttrs(source, target string) map[string]any {
	if targets, ok := g.adj[source]; ok {
		return targets[target]
	}
	return nil
}

// Neighbors returns sorted IDs of all neighbors of a node.
func (g *Graph) Neighbors(id string) []string {
	targets, ok := g.adj[id]
	if !ok {
		return nil
	}
	neighbors := make([]string, 0, len(targets))
	for tgt := range targets {
		neighbors = append(neighbors, tgt)
	}
	sort.Strings(neighbors)
	return neighbors
}

// Degree returns the degree of a node.
func (g *Graph) Degree(id string) int {
	return len(g.adj[id])
}

// RemoveNode removes a node and all its edges.
func (g *Graph) RemoveNode(id string) {
	if _, ok := g.nodes[id]; !ok {
		return
	}
	// Remove edges from this node.
	for tgt := range g.adj[id] {
		if !g.directed {
			delete(g.adj[tgt], id)
		}
	}
	delete(g.adj, id)
	// Remove edges to this node (directed only).
	if g.directed {
		for src := range g.adj {
			delete(g.adj[src], id)
		}
	}
	delete(g.nodes, id)
}

// Subgraph returns a new graph containing only the specified nodes and edges
// between them.
func (g *Graph) Subgraph(nodeIDs []string) *Graph {
	sub := New(g.directed)
	keep := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		keep[id] = true
	}
	for _, id := range nodeIDs {
		if attrs, ok := g.nodes[id]; ok {
			sub.AddNode(id, attrs)
		}
	}
	for src, targets := range g.adj {
		if !keep[src] {
			continue
		}
		for tgt, attrs := range targets {
			if !keep[tgt] {
				continue
			}
			if !g.directed && src > tgt {
				continue // avoid double-adding in undirected
			}
			sub.AddEdge(src, tgt, attrs)
		}
	}
	return sub
}

// ToUndirected returns an undirected copy of this graph.
func (g *Graph) ToUndirected() *Graph {
	if !g.directed {
		return g
	}
	u := New(false)
	for id, attrs := range g.nodes {
		u.AddNode(id, attrs)
	}
	for src, targets := range g.adj {
		for tgt, attrs := range targets {
			u.AddEdge(src, tgt, attrs)
		}
	}
	for k, v := range g.Metadata {
		u.Metadata[k] = v
	}
	return u
}

// BFS performs breadth-first search from start nodes up to the given depth.
// Returns visited node IDs and edges traversed.
func (g *Graph) BFS(startNodes []string, depth int) (visited []string, edges []EdgeData) {
	seen := make(map[string]bool)
	edgeSeen := make(map[string]bool)
	queue := make([]string, 0)
	depthMap := make(map[string]int)

	for _, s := range startNodes {
		if g.HasNode(s) {
			queue = append(queue, s)
			seen[s] = true
			depthMap[s] = 0
		}
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		d := depthMap[current]
		if d >= depth {
			continue
		}
		for _, nb := range g.Neighbors(current) {
			edgeKey := current + "\x00" + nb
			if !g.directed && current > nb {
				edgeKey = nb + "\x00" + current
			}
			if !edgeSeen[edgeKey] {
				edgeSeen[edgeKey] = true
				edges = append(edges, EdgeData{Source: current, Target: nb, Attrs: g.EdgeAttrs(current, nb)})
			}
			if !seen[nb] {
				seen[nb] = true
				depthMap[nb] = d + 1
				queue = append(queue, nb)
			}
		}
	}

	visited = make([]string, 0, len(seen))
	for id := range seen {
		visited = append(visited, id)
	}
	sort.Strings(visited)
	return visited, edges
}

// ShortestPath returns the shortest path between source and target using BFS.
// Returns nil if no path exists. maxHops limits the search depth (0 = unlimited).
func (g *Graph) ShortestPath(source, target string, maxHops int) []string {
	if source == target {
		return []string{source}
	}
	if !g.HasNode(source) || !g.HasNode(target) {
		return nil
	}

	type entry struct {
		node string
		path []string
	}

	seen := map[string]bool{source: true}
	queue := []entry{{node: source, path: []string{source}}}

	for len(queue) > 0 {
		e := queue[0]
		queue = queue[1:]
		if maxHops > 0 && len(e.path)-1 >= maxHops {
			continue
		}
		for _, nb := range g.Neighbors(e.node) {
			if nb == target {
				return append(e.path, nb)
			}
			if !seen[nb] {
				seen[nb] = true
				newPath := make([]string, len(e.path)+1)
				copy(newPath, e.path)
				newPath[len(e.path)] = nb
				queue = append(queue, entry{node: nb, path: newPath})
			}
		}
	}
	return nil
}

// String returns a short summary of the graph.
func (g *Graph) String() string {
	kind := "undirected"
	if g.directed {
		kind = "directed"
	}
	return fmt.Sprintf("Graph(%s, %d nodes, %d edges)", kind, g.NodeCount(), g.EdgeCount())
}

func copyAttrs(attrs map[string]any) map[string]any {
	if attrs == nil {
		return make(map[string]any)
	}
	cp := make(map[string]any, len(attrs))
	for k, v := range attrs {
		cp[k] = v
	}
	return cp
}
