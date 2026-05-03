// Package cluster implements community detection using the Louvain algorithm.
package cluster

import (
	"math/rand"
	"sort"

	"github.com/qiangli/gfy/internal/graph"
)

const (
	maxCommunityFraction = 0.25
	minSplitSize         = 10
)

// Cluster runs Louvain community detection on the graph.
// Returns {communityID: [nodeIDs]} with IDs ordered by community size descending.
func Cluster(g *graph.Graph) map[int][]string {
	if g.NodeCount() == 0 {
		return map[int][]string{}
	}

	// Louvain requires undirected.
	if g.IsDirected() {
		g = g.ToUndirected()
	}

	if g.EdgeCount() == 0 {
		result := make(map[int][]string)
		for i, n := range g.Nodes() {
			result[i] = []string{n}
		}
		return result
	}

	// Separate isolates.
	var isolates, connected []string
	for _, n := range g.Nodes() {
		if g.Degree(n) == 0 {
			isolates = append(isolates, n)
		} else {
			connected = append(connected, n)
		}
	}

	raw := make(map[int][]string)
	if len(connected) > 0 {
		sub := g.Subgraph(connected)
		partition := louvain(sub)
		for node, cid := range partition {
			raw[cid] = append(raw[cid], node)
		}
	}

	// Each isolate becomes its own community.
	nextCID := 0
	for cid := range raw {
		if cid >= nextCID {
			nextCID = cid + 1
		}
	}
	for _, node := range isolates {
		raw[nextCID] = []string{node}
		nextCID++
	}

	// Split oversized communities.
	maxSize := int(float64(g.NodeCount()) * maxCommunityFraction)
	if maxSize < minSplitSize {
		maxSize = minSplitSize
	}

	var finalCommunities [][]string
	for _, nodes := range raw {
		if len(nodes) > maxSize {
			sub := g.Subgraph(nodes)
			split := splitCommunity(sub, nodes)
			finalCommunities = append(finalCommunities, split...)
		} else {
			finalCommunities = append(finalCommunities, nodes)
		}
	}

	// Re-index by size descending.
	sort.Slice(finalCommunities, func(i, j int) bool {
		return len(finalCommunities[i]) > len(finalCommunities[j])
	})

	result := make(map[int][]string, len(finalCommunities))
	for i, nodes := range finalCommunities {
		sort.Strings(nodes)
		result[i] = nodes
	}
	return result
}

// splitCommunity runs a second Louvain pass on a community subgraph.
func splitCommunity(sub *graph.Graph, nodes []string) [][]string {
	if sub.EdgeCount() == 0 {
		result := make([][]string, len(nodes))
		for i, n := range nodes {
			result[i] = []string{n}
		}
		return result
	}

	partition := louvain(sub)
	communities := make(map[int][]string)
	for node, cid := range partition {
		communities[cid] = append(communities[cid], node)
	}
	if len(communities) <= 1 {
		sorted := make([]string, len(nodes))
		copy(sorted, nodes)
		sort.Strings(sorted)
		return [][]string{sorted}
	}
	result := make([][]string, 0, len(communities))
	for _, v := range communities {
		sort.Strings(v)
		result = append(result, v)
	}
	return result
}

// louvain implements the Louvain community detection algorithm.
// Returns {nodeID: communityID}.
func louvain(g *graph.Graph) map[string]int {
	nodes := g.Nodes()
	n := len(nodes)
	nodeIndex := make(map[string]int, n)
	for i, id := range nodes {
		nodeIndex[id] = i
	}

	// Initialize: each node in its own community.
	community := make([]int, n)
	for i := range community {
		community[i] = i
	}

	// Compute total weight (sum of all edge weights).
	totalWeight := 0.0
	edges := g.Edges()
	for _, e := range edges {
		w := edgeWeight(e.Attrs)
		totalWeight += w
	}
	if totalWeight == 0 {
		totalWeight = float64(len(edges))
	}
	// For undirected, each edge contributes weight to both endpoints.
	m2 := 2.0 * totalWeight

	// Precompute adjacency with weights.
	type neighbor struct {
		idx    int
		weight float64
	}
	adj := make([][]neighbor, n)
	for i := range adj {
		adj[i] = make([]neighbor, 0)
	}
	for _, e := range edges {
		si, ti := nodeIndex[e.Source], nodeIndex[e.Target]
		w := edgeWeight(e.Attrs)
		adj[si] = append(adj[si], neighbor{ti, w})
		adj[ti] = append(adj[ti], neighbor{si, w})
	}

	// Weighted degree of each node.
	kDeg := make([]float64, n)
	for i := range kDeg {
		for _, nb := range adj[i] {
			kDeg[i] += nb.weight
		}
	}

	// Precompute sigmaTot: sum of weighted degrees per community.
	// Maintained incrementally when nodes move between communities.
	sigmaTot := make(map[int]float64, n)
	for i := 0; i < n; i++ {
		sigmaTot[community[i]] += kDeg[i]
	}

	// Iterative modularity optimization.
	rng := rand.New(rand.NewSource(42))
	maxIter := 10
	for iter := 0; iter < maxIter; iter++ {
		improved := false
		order := rng.Perm(n)
		for _, i := range order {
			currentComm := community[i]
			ki := kDeg[i]

			// Compute sum of weights to each neighboring community.
			commWeights := make(map[int]float64)
			for _, nb := range adj[i] {
				commWeights[community[nb.idx]] += nb.weight
			}

			// sigmaTot for currentComm excludes node i.
			sigmaCurrent := sigmaTot[currentComm] - ki

			bestComm := currentComm
			bestDelta := 0.0

			for c, wic := range commWeights {
				if c == currentComm {
					continue
				}
				// Modularity gain of moving node i from currentComm to c.
				sigmaC := sigmaTot[c]
				delta := (wic - commWeights[currentComm]) / totalWeight
				delta -= ki * (sigmaC - sigmaCurrent) / (m2 * totalWeight)
				if delta > bestDelta {
					bestDelta = delta
					bestComm = c
				}
			}

			if bestComm != currentComm {
				// Update sigmaTot incrementally.
				sigmaTot[currentComm] -= ki
				sigmaTot[bestComm] += ki
				community[i] = bestComm
				improved = true
			}
		}
		if !improved {
			break
		}
	}

	// Compact community IDs.
	result := make(map[string]int, n)
	compactMap := make(map[int]int)
	nextID := 0
	for i, c := range community {
		if _, ok := compactMap[c]; !ok {
			compactMap[c] = nextID
			nextID++
		}
		result[nodes[i]] = compactMap[c]
	}
	return result
}

func edgeWeight(attrs map[string]any) float64 {
	if w, ok := attrs["weight"]; ok {
		switch v := w.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return 1.0
}
