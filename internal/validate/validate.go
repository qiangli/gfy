// Package validate enforces the graphify extraction schema.
package validate

import (
	"fmt"

	"github.com/qiangli/gfy/internal/types"
)

var validFileTypes = map[string]bool{
	"code": true, "document": true, "paper": true,
	"image": true, "rationale": true, "concept": true,
}

var validConfidences = map[types.Confidence]bool{
	types.Extracted: true,
	types.Inferred:  true,
	types.Ambiguous: true,
}

// Validate checks an ExtractionResult against the graphify schema.
// Returns a list of error strings — empty means valid.
func Validate(result *types.ExtractionResult) []string {
	var errors []string

	nodeIDs := make(map[string]bool, len(result.Nodes))
	for i, node := range result.Nodes {
		if node.ID == "" {
			errors = append(errors, fmt.Sprintf("Node %d missing required field 'id'", i))
		}
		if node.Label == "" {
			errors = append(errors, fmt.Sprintf("Node %d (id=%q) missing required field 'label'", i, node.ID))
		}
		if node.FileType == "" {
			errors = append(errors, fmt.Sprintf("Node %d (id=%q) missing required field 'file_type'", i, node.ID))
		} else if !validFileTypes[node.FileType] {
			errors = append(errors, fmt.Sprintf("Node %d (id=%q) has invalid file_type %q", i, node.ID, node.FileType))
		}
		// SourceFile may be empty for external reference nodes (e.g., base
		// classes or interfaces defined in other packages).
		nodeIDs[node.ID] = true
	}

	for i, edge := range result.Edges {
		if edge.Source == "" {
			errors = append(errors, fmt.Sprintf("Edge %d missing required field 'source'", i))
		}
		if edge.Target == "" {
			errors = append(errors, fmt.Sprintf("Edge %d missing required field 'target'", i))
		}
		if edge.Relation == "" {
			errors = append(errors, fmt.Sprintf("Edge %d missing required field 'relation'", i))
		}
		if edge.Confidence == "" {
			errors = append(errors, fmt.Sprintf("Edge %d missing required field 'confidence'", i))
		} else if !validConfidences[edge.Confidence] {
			errors = append(errors, fmt.Sprintf("Edge %d has invalid confidence %q", i, edge.Confidence))
		}
		if edge.SourceFile == "" {
			errors = append(errors, fmt.Sprintf("Edge %d missing required field 'source_file'", i))
		}
		if edge.Source != "" && len(nodeIDs) > 0 && !nodeIDs[edge.Source] {
			errors = append(errors, fmt.Sprintf("Edge %d source %q does not match any node id", i, edge.Source))
		}
		if edge.Target != "" && len(nodeIDs) > 0 && !nodeIDs[edge.Target] {
			errors = append(errors, fmt.Sprintf("Edge %d target %q does not match any node id", i, edge.Target))
		}
	}

	return errors
}
