package cluster

import (
	"math"

	"github.com/qiangli/gfy/pkg/graph"
)

// CohesionScore computes the ratio of actual intra-community edges to maximum possible.
func CohesionScore(g *graph.Graph, communityNodes []string) float64 {
	n := len(communityNodes)
	if n <= 1 {
		return 1.0
	}
	sub := g.Subgraph(communityNodes)
	actual := float64(sub.EdgeCount())
	possible := float64(n*(n-1)) / 2.0
	if possible == 0 {
		return 0.0
	}
	return math.Round(actual/possible*100) / 100
}

// ScoreAll computes cohesion scores for all communities.
func ScoreAll(g *graph.Graph, communities map[int][]string) map[int]float64 {
	scores := make(map[int]float64, len(communities))
	for cid, nodes := range communities {
		scores[cid] = CohesionScore(g, nodes)
	}
	return scores
}
