// Package analyze provides graph analysis: god nodes, surprising connections, suggested questions.
package analyze

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/qiangli/gfy/internal/graph"
)

// GodNode represents a highly-connected entity in the graph.
type GodNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Degree int    `json:"degree"`
}

// GodNodes returns the top N most-connected real entities (excluding file-level hubs).
func GodNodes(g *graph.Graph, topN int) []GodNode {
	type entry struct {
		id     string
		label  string
		degree int
	}
	var candidates []entry
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		label, _ := attrs["label"].(string)
		if isFileNode(label) {
			continue
		}
		candidates = append(candidates, entry{id, label, g.Degree(id)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].degree > candidates[j].degree
	})
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}
	result := make([]GodNode, len(candidates))
	for i, c := range candidates {
		result[i] = GodNode{ID: c.id, Label: c.label, Degree: c.degree}
	}
	return result
}

// SurprisingConnection represents an unexpected cross-file or cross-community edge.
type SurprisingConnection struct {
	Source      string   `json:"source"`
	Target      string   `json:"target"`
	SourceFiles []string `json:"source_files"`
	Confidence  string   `json:"confidence"`
	Relation    string   `json:"relation"`
	Why         string   `json:"why"`
}

// SurprisingConnections returns the top N most surprising cross-file/cross-community edges.
func SurprisingConnections(g *graph.Graph, communities map[int][]string, topN int) []SurprisingConnection {
	nodeCommunity := make(map[string]int)
	for cid, nodes := range communities {
		for _, n := range nodes {
			nodeCommunity[n] = cid
		}
	}

	// Structural relations to skip.
	skipRelations := map[string]bool{
		"imports": true, "imports_from": true, "contains": true, "method": true,
	}

	type scored struct {
		sc    SurprisingConnection
		score int
	}
	var candidates []scored

	for _, e := range g.Edges() {
		rel, _ := e.Attrs["relation"].(string)
		if skipRelations[rel] {
			continue
		}

		srcAttrs := g.NodeAttrs(e.Source)
		tgtAttrs := g.NodeAttrs(e.Target)
		srcLabel, _ := srcAttrs["label"].(string)
		tgtLabel, _ := tgtAttrs["label"].(string)
		if isFileNode(srcLabel) || isFileNode(tgtLabel) {
			continue
		}

		srcFile, _ := srcAttrs["source_file"].(string)
		tgtFile, _ := tgtAttrs["source_file"].(string)

		score := 0
		var reasons []string

		// Confidence bonus.
		conf, _ := e.Attrs["confidence"].(string)
		switch conf {
		case "AMBIGUOUS":
			score += 3
			reasons = append(reasons, "tagged AMBIGUOUS")
		case "INFERRED":
			score += 2
			reasons = append(reasons, "inferred connection")
		default:
			score += 1
		}

		// Cross-file bonus.
		if srcFile != "" && tgtFile != "" && srcFile != tgtFile {
			score += 2
			reasons = append(reasons, "crosses files")
		}

		// Cross-community bonus.
		srcComm, hasSrc := nodeCommunity[e.Source]
		tgtComm, hasTgt := nodeCommunity[e.Target]
		if hasSrc && hasTgt && srcComm != tgtComm {
			score += 1
			reasons = append(reasons, "bridges separate communities")
		}

		var sourceFiles []string
		if srcFile != "" {
			sourceFiles = append(sourceFiles, srcFile)
		}
		if tgtFile != "" && tgtFile != srcFile {
			sourceFiles = append(sourceFiles, tgtFile)
		}

		candidates = append(candidates, scored{
			sc: SurprisingConnection{
				Source:      srcLabel,
				Target:      tgtLabel,
				SourceFiles: sourceFiles,
				Confidence:  conf,
				Relation:    rel,
				Why:         strings.Join(reasons, "; "),
			},
			score: score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	result := make([]SurprisingConnection, len(candidates))
	for i, c := range candidates {
		result[i] = c.sc
	}
	return result
}

// SuggestedQuestion represents a question the graph can help answer.
type SuggestedQuestion struct {
	Type     string `json:"type"`
	Question string `json:"question"`
	Why      string `json:"why"`
}

// SuggestQuestions generates questions based on graph structure.
func SuggestQuestions(g *graph.Graph, communities map[int][]string, topN int) []SuggestedQuestion {
	var questions []SuggestedQuestion

	// Find ambiguous edges.
	for _, e := range g.Edges() {
		conf, _ := e.Attrs["confidence"].(string)
		if conf == "AMBIGUOUS" {
			srcLabel := nodeLabel(g, e.Source)
			tgtLabel := nodeLabel(g, e.Target)
			rel, _ := e.Attrs["relation"].(string)
			questions = append(questions, SuggestedQuestion{
				Type:     "ambiguous_edge",
				Question: "What is the exact relationship between " + srcLabel + " and " + tgtLabel + "?",
				Why:      "Edge tagged AMBIGUOUS (relation: " + rel + ") - confidence is low.",
			})
		}
		if len(questions) >= topN {
			break
		}
	}

	// Find isolated nodes.
	var isolated []string
	for _, id := range g.Nodes() {
		if g.Degree(id) == 0 {
			isolated = append(isolated, nodeLabel(g, id))
		}
	}
	if len(isolated) > 0 && len(questions) < topN {
		questions = append(questions, SuggestedQuestion{
			Type:     "isolated_nodes",
			Question: "Why are these entities disconnected: " + strings.Join(isolated[:min(3, len(isolated))], ", ") + "?",
			Why:      "Isolated nodes may indicate missing relationships or dead code.",
		})
	}

	if len(questions) > topN {
		questions = questions[:topN]
	}
	return questions
}

// isFileNode checks if a label looks like a filename.
func isFileNode(label string) bool {
	ext := filepath.Ext(label)
	return ext != "" && len(ext) <= 5 && !strings.Contains(label, "()")
}

func nodeLabel(g *graph.Graph, id string) string {
	attrs := g.NodeAttrs(id)
	if label, ok := attrs["label"].(string); ok {
		return label
	}
	return id
}
