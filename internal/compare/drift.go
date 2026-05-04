package compare

import (
	"sort"

	"github.com/qiangli/gfy/pkg/graph"
)

// computeDrift analyzes import edge changes between two graphs,
// grouped by source file.
func computeDrift(a, b *graph.Graph) []DriftEntry {
	// Collect import edges from both graphs.
	importsA := importsByFile(a)
	importsB := importsByFile(b)

	// All files that have imports in either graph.
	allFiles := make(map[string]bool)
	for f := range importsA {
		allFiles[f] = true
	}
	for f := range importsB {
		allFiles[f] = true
	}

	var entries []DriftEntry
	for file := range allFiles {
		aSet := importsA[file]
		bSet := importsB[file]

		var added, removed []string
		for target := range bSet {
			if !aSet[target] {
				added = append(added, target)
			}
		}
		for target := range aSet {
			if !bSet[target] {
				removed = append(removed, target)
			}
		}

		if len(added) == 0 && len(removed) == 0 {
			continue
		}

		sort.Strings(added)
		sort.Strings(removed)
		entries = append(entries, DriftEntry{
			SourceFile:     file,
			AddedImports:   added,
			RemovedImports: removed,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].SourceFile < entries[j].SourceFile
	})
	return entries
}

// importsByFile returns a map from source file to set of import targets.
func importsByFile(g *graph.Graph) map[string]map[string]bool {
	result := make(map[string]map[string]bool)
	for _, e := range g.Edges() {
		rel, _ := e.Attrs["relation"].(string)
		if rel != "imports" {
			continue
		}
		// Determine the directed source and target.
		// In undirected graphs, the build package stores _src/_tgt to
		// preserve edge direction.
		src, tgt := e.Source, e.Target
		if s, ok := e.Attrs["_src"].(string); ok {
			src = s
		}
		if t, ok := e.Attrs["_tgt"].(string); ok {
			tgt = t
		}

		// Get the source file from the source node.
		srcAttrs := g.NodeAttrs(src)
		file, _ := srcAttrs["source_file"].(string)
		if file == "" {
			// Try the other endpoint in case of undirected edge reversal.
			srcAttrs = g.NodeAttrs(tgt)
			file, _ = srcAttrs["source_file"].(string)
			if file != "" {
				// Swap: the actual source is tgt.
				src, tgt = tgt, src
			}
		}
		if file == "" {
			continue
		}

		// Target label is the import target.
		tgtAttrs := g.NodeAttrs(tgt)
		tgtLabel, _ := tgtAttrs["label"].(string)
		if tgtLabel == "" {
			tgtLabel = tgt
		}
		if result[file] == nil {
			result[file] = make(map[string]bool)
		}
		result[file][tgtLabel] = true
	}
	return result
}
