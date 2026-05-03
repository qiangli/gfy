// Package build assembles extraction results into a graph.
package build

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/qiangli/gfy/internal/graph"
	"github.com/qiangli/gfy/internal/types"
	"github.com/qiangli/gfy/internal/validate"
)

var reNonAlphaNum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// normalizeID normalizes a node ID the same way extract.MakeID does.
func normalizeID(s string) string {
	cleaned := reNonAlphaNum.ReplaceAllString(s, "_")
	return strings.ToLower(strings.Trim(cleaned, "_"))
}

// BuildFromResult builds a graph from an ExtractionResult.
func BuildFromResult(result *types.ExtractionResult, directed bool) *graph.Graph {
	// Validate and warn about real errors (not dangling edge references).
	errors := validate.Validate(result)
	var realErrors []string
	for _, e := range errors {
		if !strings.Contains(e, "does not match any node id") {
			realErrors = append(realErrors, e)
		}
	}
	if len(realErrors) > 0 {
		fmt.Fprintf(os.Stderr, "[gfy] Extraction warning (%d issues): %s\n", len(realErrors), realErrors[0])
	}

	g := graph.New(directed)

	// Add nodes.
	for _, node := range result.Nodes {
		attrs := map[string]any{
			"label":       node.Label,
			"file_type":   node.FileType,
			"source_file": node.SourceFile,
		}
		if node.SourceLocation != "" {
			attrs["source_location"] = node.SourceLocation
		}
		if len(node.Tags) > 0 {
			attrs["tags"] = node.Tags
		}
		if node.Comment != "" {
			attrs["comment"] = node.Comment
		}
		if len(node.LogMessages) > 0 {
			attrs["log_messages"] = node.LogMessages
		}
		if len(node.ThrowMessages) > 0 {
			attrs["throw_messages"] = node.ThrowMessages
		}
		g.AddNode(node.ID, attrs)
	}

	// Build normalized ID map for edge endpoint reconciliation.
	nodeSet := make(map[string]bool)
	for _, id := range g.Nodes() {
		nodeSet[id] = true
	}
	normToID := make(map[string]string)
	for id := range nodeSet {
		normToID[normalizeID(id)] = id
	}

	// Add edges.
	for _, edge := range result.Edges {
		src, tgt := edge.Source, edge.Target

		// Remap mismatched IDs via normalization.
		if !nodeSet[src] {
			if remapped, ok := normToID[normalizeID(src)]; ok {
				src = remapped
			}
		}
		if !nodeSet[tgt] {
			if remapped, ok := normToID[normalizeID(tgt)]; ok {
				tgt = remapped
			}
		}

		// Skip edges to external/stdlib nodes.
		if !nodeSet[src] || !nodeSet[tgt] {
			continue
		}

		attrs := map[string]any{
			"relation":   edge.Relation,
			"confidence": string(edge.Confidence),
			"weight":     edge.Weight,
			"_src":       src,
			"_tgt":       tgt,
		}
		if edge.SourceFile != "" {
			attrs["source_file"] = edge.SourceFile
		}
		if edge.SourceLocation != "" {
			attrs["source_location"] = edge.SourceLocation
		}
		if edge.ConfidenceScore > 0 {
			attrs["confidence_score"] = edge.ConfidenceScore
		}

		g.AddEdge(src, tgt, attrs)
	}

	return g
}

// Build merges multiple extraction results into one graph.
func Build(results []*types.ExtractionResult, directed bool) *graph.Graph {
	combined := &types.ExtractionResult{}
	for _, r := range results {
		combined.Nodes = append(combined.Nodes, r.Nodes...)
		combined.Edges = append(combined.Edges, r.Edges...)
	}
	return BuildFromResult(combined, directed)
}
