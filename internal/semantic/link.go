package semantic

import (
	"fmt"

	"github.com/qiangli/gfy/internal/types"
)

const (
	// Similarity thresholds for edge creation.
	referencesThreshold          = 0.5
	conceptuallyRelatedThreshold = 0.25

	// Maximum edges created per semantic node.
	maxMatchesPerNode = 5
)

// semanticFileTypes are file_type values that identify semantic (non-code) nodes.
var semanticFileTypes = map[string]bool{
	"document":  true,
	"paper":     true,
	"concept":   true,
	"rationale": true,
	"image":     true,
}

// LinkSemanticToAST creates inferred edges between semantic nodes and
// AST nodes using TF-IDF cosine similarity. AST nodes are represented
// as documents built from their label, comment, log messages, and throw
// messages. This runs as a post-merge pass — no LLM calls.
func LinkSemanticToAST(merged *types.ExtractionResult) *types.ExtractionResult {
	// Classify nodes into AST and semantic.
	var astIndices []int
	var semIndices []int
	for i, n := range merged.Nodes {
		if semanticFileTypes[n.FileType] {
			semIndices = append(semIndices, i)
		} else {
			astIndices = append(astIndices, i)
		}
	}

	if len(astIndices) == 0 || len(semIndices) == 0 {
		return merged
	}

	// Build text documents for all nodes.
	allDocs := make([][]string, len(merged.Nodes))
	for i := range merged.Nodes {
		allDocs[i] = tokenize(buildNodeDocument(merged.Nodes[i]))
	}

	// Build TF-IDF index over all documents so IDF reflects full corpus.
	idx := newTFIDF(allDocs)

	// Collect AST vectors for matching.
	astVectors := make([]map[string]float64, len(astIndices))
	for i, ai := range astIndices {
		astVectors[i] = idx.vectors[ai]
	}

	// Build existing edge pair set for dedup.
	existingPairs := make(map[[2]string]bool)
	for _, e := range merged.Edges {
		existingPairs[[2]string{e.Source, e.Target}] = true
		existingPairs[[2]string{e.Target, e.Source}] = true
	}

	// For each semantic node, find top AST matches.
	var newEdges []types.Edge
	for _, si := range semIndices {
		semNode := merged.Nodes[si]
		semVec := idx.vectors[si]

		matches := findTopMatches(semVec, astVectors, astIndices, maxMatchesPerNode, conceptuallyRelatedThreshold)

		for _, m := range matches {
			astNode := merged.Nodes[m.index]

			// Skip self-links.
			if semNode.ID == astNode.ID {
				continue
			}

			// Skip duplicates.
			pair := [2]string{semNode.ID, astNode.ID}
			if existingPairs[pair] {
				continue
			}
			existingPairs[pair] = true
			existingPairs[[2]string{astNode.ID, semNode.ID}] = true

			relation := "conceptually_related_to"
			if m.similarity >= referencesThreshold {
				relation = "references"
			}

			newEdges = append(newEdges, types.Edge{
				Source:          semNode.ID,
				Target:          astNode.ID,
				Relation:        relation,
				Confidence:      types.Inferred,
				ConfidenceScore: m.similarity,
				SourceFile:      semNode.SourceFile,
				Weight:          1.0,
			})
		}
	}

	if len(newEdges) > 0 {
		fmt.Printf("  TF-IDF linking: +%d edges between semantic and code nodes\n", len(newEdges))
		merged.Edges = append(merged.Edges, newEdges...)
	}

	// Resolve dangling edge endpoints.
	merged = resolveDanglingEdges(merged, idx, astIndices)

	return merged
}

// resolveDanglingEdges attempts to fix edges whose source or target ID
// doesn't match any node, by finding the most similar AST node.
func resolveDanglingEdges(merged *types.ExtractionResult, idx *tfidfIndex, astIndices []int) *types.ExtractionResult {
	// Build node ID set.
	nodeIDs := make(map[string]bool, len(merged.Nodes))
	for _, n := range merged.Nodes {
		nodeIDs[n.ID] = true
	}

	// Build AST vectors for matching.
	astVectors := make([]map[string]float64, len(astIndices))
	for i, ai := range astIndices {
		astVectors[i] = idx.vectors[ai]
	}

	resolved := 0
	for i := range merged.Edges {
		e := &merged.Edges[i]

		if !nodeIDs[e.Source] {
			if newID := resolveID(e.Source, idx, astVectors, astIndices, merged.Nodes); newID != "" {
				e.Source = newID
				resolved++
			}
		}
		if !nodeIDs[e.Target] {
			if newID := resolveID(e.Target, idx, astVectors, astIndices, merged.Nodes); newID != "" {
				e.Target = newID
				resolved++
			}
		}
	}

	if resolved > 0 {
		fmt.Printf("  TF-IDF linking: resolved %d dangling edge endpoints\n", resolved)
	}
	return merged
}

// resolveID tokenizes a dangling ID and finds the best-matching AST node.
const danglingThreshold = 0.4

func resolveID(id string, idx *tfidfIndex, astVectors []map[string]float64, astIndices []int, nodes []types.Node) string {
	tokens := tokenize(id)
	if len(tokens) == 0 {
		return ""
	}

	// Build a query vector using the corpus IDF.
	tf := make(map[string]int)
	for _, t := range tokens {
		tf[t]++
	}
	query := make(map[string]float64, len(tf))
	docLen := float64(len(tokens))
	for term, count := range tf {
		if idfVal, ok := idx.idf[term]; ok {
			query[term] = (float64(count) / docLen) * idfVal
		}
	}

	matches := findTopMatches(query, astVectors, astIndices, 1, danglingThreshold)
	if len(matches) == 0 {
		return ""
	}
	return nodes[matches[0].index].ID
}
