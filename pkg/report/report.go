// Package report generates GRAPH_REPORT.md from analysis results.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/qiangli/gfy/pkg/analyze"
	"github.com/qiangli/gfy/pkg/graph"
)

// Generate produces a markdown report string.
func Generate(
	g *graph.Graph,
	communities map[int][]string,
	cohesionScores map[int]float64,
	godNodes []analyze.GodNode,
	surprises []analyze.SurprisingConnection,
	questions []analyze.SuggestedQuestion,
	root string,
) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# Graph Report — %s (%s)\n\n", root, time.Now().Format("2006-01-02")))

	// Summary
	b.WriteString("## Summary\n\n")

	// Confidence breakdown.
	confCounts := map[string]int{"EXTRACTED": 0, "INFERRED": 0, "AMBIGUOUS": 0}
	for _, e := range g.Edges() {
		if c, ok := e.Attrs["confidence"].(string); ok {
			confCounts[c]++
		}
	}

	b.WriteString(fmt.Sprintf("- **Nodes:** %d\n", g.NodeCount()))
	b.WriteString(fmt.Sprintf("- **Edges:** %d\n", g.EdgeCount()))
	b.WriteString(fmt.Sprintf("- **Communities:** %d\n", len(communities)))
	b.WriteString(fmt.Sprintf("- **Confidence:** %d extracted, %d inferred, %d ambiguous\n\n",
		confCounts["EXTRACTED"], confCounts["INFERRED"], confCounts["AMBIGUOUS"]))

	// God Nodes
	if len(godNodes) > 0 {
		b.WriteString("## God Nodes (Most Connected)\n\n")
		b.WriteString("| Rank | Entity | Connections |\n")
		b.WriteString("|------|--------|-------------|\n")
		for i, gn := range godNodes {
			b.WriteString(fmt.Sprintf("| %d | %s | %d |\n", i+1, gn.Label, gn.Degree))
		}
		b.WriteString("\n")
	}

	// Surprising Connections
	if len(surprises) > 0 {
		b.WriteString("## Surprising Connections\n\n")
		for i, s := range surprises {
			b.WriteString(fmt.Sprintf("%d. **%s** ↔ **%s** (%s, %s)\n",
				i+1, s.Source, s.Target, s.Confidence, s.Relation))
			b.WriteString(fmt.Sprintf("   - %s\n", s.Why))
		}
		b.WriteString("\n")
	}

	// Communities
	if len(communities) > 0 {
		b.WriteString("## Communities\n\n")
		// Sort by community ID.
		ids := make([]int, 0, len(communities))
		for cid := range communities {
			ids = append(ids, cid)
		}
		sort.Ints(ids)

		for _, cid := range ids {
			nodes := communities[cid]
			if len(nodes) == 0 {
				continue
			}
			cohesion := cohesionScores[cid]
			b.WriteString(fmt.Sprintf("### Community %d (%d nodes, cohesion: %.2f)\n\n", cid, len(nodes), cohesion))

			// Show top nodes by degree.
			type nodeInfo struct {
				id     string
				label  string
				degree int
			}
			var topNodes []nodeInfo
			for _, nid := range nodes {
				attrs := g.NodeAttrs(nid)
				label, _ := attrs["label"].(string)
				topNodes = append(topNodes, nodeInfo{nid, label, g.Degree(nid)})
			}
			sort.Slice(topNodes, func(i, j int) bool {
				return topNodes[i].degree > topNodes[j].degree
			})
			limit := 10
			if len(topNodes) < limit {
				limit = len(topNodes)
			}
			for _, tn := range topNodes[:limit] {
				b.WriteString(fmt.Sprintf("- %s (degree: %d)\n", tn.label, tn.degree))
			}
			b.WriteString("\n")
		}
	}

	// Suggested Questions
	if len(questions) > 0 {
		b.WriteString("## Suggested Questions\n\n")
		for _, q := range questions {
			b.WriteString(fmt.Sprintf("- **%s**\n  %s\n", q.Question, q.Why))
		}
		b.WriteString("\n")
	}

	return b.String()
}
