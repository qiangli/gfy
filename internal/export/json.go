// Package export provides graph export in various formats.
package export

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/gfy/pkg/graph"
)

// ToJSON writes the graph to a JSON file in NetworkX node_link_data format.
// Returns true if the file was written, false if it already exists and force is false.
func ToJSON(g *graph.Graph, outputPath string, force bool) (bool, error) {
	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return false, fmt.Errorf("create output directory: %w", err)
	}
	if err := g.SaveJSON(outputPath); err != nil {
		return false, err
	}
	return true, nil
}
