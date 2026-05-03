package compare

import (
	"sort"

	"github.com/qiangli/gfy/internal/graph"
)

// rankImpact ranks changed nodes by how many transitive dependents they affect.
// It uses BFS on graph B (for added/modified) and graph A (for removed) to count
// nodes reachable within maxDepth hops.
func rankImpact(a, b *graph.Graph, nodeDiff NodeDiff, maxDepth, topK int) []ImpactEntry {
	var entries []ImpactEntry

	// Added nodes: impact measured in graph B.
	for _, n := range nodeDiff.Added {
		affected := bfsCount(b, n.ID, maxDepth)
		if len(affected) > 0 {
			entries = append(entries, ImpactEntry{
				NodeID:        n.ID,
				Label:         n.Label,
				Change:        "added",
				AffectedCount: len(affected),
				AffectedNodes: affected,
			})
		}
	}

	// Removed nodes: impact measured in graph A (who depended on this?).
	for _, n := range nodeDiff.Removed {
		affected := bfsCount(a, n.ID, maxDepth)
		if len(affected) > 0 {
			entries = append(entries, ImpactEntry{
				NodeID:        n.ID,
				Label:         n.Label,
				Change:        "removed",
				AffectedCount: len(affected),
				AffectedNodes: affected,
			})
		}
	}

	// Modified nodes: impact measured in graph B.
	for _, n := range nodeDiff.Modified {
		affected := bfsCount(b, n.ID, maxDepth)
		if len(affected) > 0 {
			entries = append(entries, ImpactEntry{
				NodeID:        n.ID,
				Label:         n.Label,
				Change:        "modified",
				AffectedCount: len(affected),
				AffectedNodes: affected,
			})
		}
	}

	// Sort by affected count descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].AffectedCount > entries[j].AffectedCount
	})

	if len(entries) > topK {
		entries = entries[:topK]
	}

	// Trim affected node lists to keep output compact.
	for i := range entries {
		if len(entries[i].AffectedNodes) > 10 {
			entries[i].AffectedNodes = entries[i].AffectedNodes[:10]
		}
	}

	return entries
}

// bfsCount returns node IDs reachable from startID within maxDepth hops,
// excluding the start node itself.
func bfsCount(g *graph.Graph, startID string, maxDepth int) []string {
	if !g.HasNode(startID) {
		return nil
	}
	visited, _ := g.BFS([]string{startID}, maxDepth)

	// Remove the start node from results.
	result := make([]string, 0, len(visited)-1)
	for _, id := range visited {
		if id != startID {
			result = append(result, id)
		}
	}
	return result
}
