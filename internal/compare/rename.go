package compare

import (
	"sort"
	"strings"

	"github.com/qiangli/gfy/pkg/graph"
)

// detectRenames finds likely rename/move candidates among removed and added nodes.
// It uses Levenshtein distance on labels plus structural neighborhood overlap.
func detectRenames(a, b *graph.Graph, removed, added []NodeInfo, threshold float64, topK int) []RenameCandidate {
	if len(removed) == 0 || len(added) == 0 {
		return nil
	}

	var candidates []RenameCandidate

	for _, old := range removed {
		for _, neu := range added {
			// Quick filter: same file type.
			if old.FileType != neu.FileType {
				continue
			}

			// Levenshtein distance on labels.
			dist := levenshtein(strings.ToLower(old.Label), strings.ToLower(neu.Label))
			maxLen := len(old.Label)
			if len(neu.Label) > maxLen {
				maxLen = len(neu.Label)
			}
			if maxLen == 0 {
				continue
			}

			// Normalized label similarity [0,1].
			labelSim := 1.0 - float64(dist)/float64(maxLen)
			if labelSim < 0.3 {
				continue // too different
			}

			// Neighborhood overlap: Jaccard on neighbor labels.
			neighborsA := neighborLabels(a, old.ID)
			neighborsB := neighborLabels(b, neu.ID)
			neighborOverlap := jaccardStringSlices(neighborsA, neighborsB)

			// Combined confidence: 60% label similarity, 40% neighbor overlap.
			confidence := 0.6*labelSim + 0.4*neighborOverlap
			if confidence < threshold {
				continue
			}

			candidates = append(candidates, RenameCandidate{
				OldID:           old.ID,
				OldLabel:        old.Label,
				NewID:           neu.ID,
				NewLabel:        neu.Label,
				EditDistance:    dist,
				NeighborOverlap: neighborOverlap,
				Confidence:      confidence,
			})
		}
	}

	// Sort by confidence descending.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	if len(candidates) > topK {
		candidates = candidates[:topK]
	}
	return candidates
}

// neighborLabels returns sorted labels of a node's neighbors.
func neighborLabels(g *graph.Graph, id string) []string {
	neighbors := g.Neighbors(id)
	labels := make([]string, 0, len(neighbors))
	for _, nid := range neighbors {
		attrs := g.NodeAttrs(nid)
		if l, ok := attrs["label"].(string); ok {
			labels = append(labels, l)
		}
	}
	sort.Strings(labels)
	return labels
}

// levenshtein computes the edit distance between two strings.
// Single-row DP, O(len(a)*len(b)) time, O(len(b)) space.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = min(ins, min(del, sub))
		}
		prev = curr
	}
	return prev[lb]
}
