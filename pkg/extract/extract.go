package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/qiangli/gfy/pkg/cache"
	"github.com/qiangli/gfy/pkg/types"
)

// Extractor is a function that extracts nodes and edges from a single file.
type Extractor func(path string) *types.ExtractionResult

// dispatch maps file extensions to extractors.
// Initialized lazily to avoid loading all grammars at import time.
var dispatch map[string]Extractor

func getDispatch() map[string]Extractor {
	if dispatch != nil {
		return dispatch
	}

	// Generic config-driven extractors (configs loaded lazily).
	extractJava := func(p string) *types.ExtractionResult { return ExtractGeneric(p, JavaConfig()) }
	extractC := func(p string) *types.ExtractionResult { return ExtractGeneric(p, CConfig()) }
	extractCpp := func(p string) *types.ExtractionResult { return ExtractGeneric(p, CppConfig()) }
	extractRuby := func(p string) *types.ExtractionResult { return ExtractGeneric(p, RubyConfig()) }
	extractCSharp := func(p string) *types.ExtractionResult { return ExtractGeneric(p, CSharpConfig()) }
	extractKotlin := func(p string) *types.ExtractionResult { return ExtractGeneric(p, KotlinConfig()) }
	extractScala := func(p string) *types.ExtractionResult { return ExtractGeneric(p, ScalaConfig()) }
	extractPHP := func(p string) *types.ExtractionResult { return ExtractGeneric(p, PHPConfig()) }
	extractLua := func(p string) *types.ExtractionResult { return ExtractGeneric(p, LuaConfig()) }
	extractSwift := func(p string) *types.ExtractionResult { return ExtractGeneric(p, SwiftConfig()) }

	dispatch = map[string]Extractor{
		// Already ported (custom extractors)
		".py":  ExtractPython,
		".js":  ExtractJS,
		".jsx": ExtractJS,
		".mjs": ExtractJS,
		".ejs": ExtractJS,
		".ts":  ExtractTS,
		".tsx": ExtractTS,
		".go":  ExtractGo,
		".rs":  ExtractRust,

		// Generic config-driven
		".java":  extractJava,
		".c":     extractC,
		".h":     extractC,
		".cpp":   extractCpp,
		".cc":    extractCpp,
		".cxx":   extractCpp,
		".hpp":   extractCpp,
		".rb":    extractRuby,
		".cs":    extractCSharp,
		".kt":    extractKotlin,
		".kts":   extractKotlin,
		".scala": extractScala,
		".php":   extractPHP,
		".lua":   extractLua,
		".toc":   extractLua,
		".swift": extractSwift,

		// Custom extractors
		".zig": ExtractZig,
		".ps1": ExtractPowerShell,
		".ex":  ExtractElixir,
		".exs": ExtractElixir,
		".jl":  ExtractJulia,
		".m":   ExtractObjC,
		".mm":  ExtractObjC,
		".v":   ExtractVerilog,
		".sv":  ExtractVerilog,

		// Regex-based
		".dart": ExtractDart,

		// Vue/Svelte treated as JS
		".vue":    ExtractJS,
		".svelte": ExtractJS,
	}
	return dispatch
}

const batchSize = 100

// Extract runs AST extraction on a list of file paths and merges results.
// It performs cross-file call resolution after all files are processed.
// The root parameter is the project root directory, used for cache keying.
//
// Files are processed in batches to bound peak memory usage. Each batch's
// results are flushed to a temporary file, then all batches are merged
// from disk for the final cross-file resolution pass.
func Extract(paths []string, root string) *types.ExtractionResult {
	if len(paths) == 0 {
		return &types.ExtractionResult{}
	}

	if root == "" {
		root = inferRoot(paths)
	}

	total := len(paths)
	progressInterval := 100
	cacheHits := 0

	// Temporary directory for batch files.
	batchDir, err := os.MkdirTemp("", "gfy-extract-*")
	if err != nil {
		// Fall back to in-memory if we can't create temp dir.
		fmt.Printf("  Warning: cannot create temp dir: %v, using in-memory\n", err)
		return extractInMemory(paths, root)
	}
	defer os.RemoveAll(batchDir)

	var batchFiles []string

	// Process files in batches.
	for batchStart := 0; batchStart < total; batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > total {
			batchEnd = total
		}
		batchPaths := paths[batchStart:batchEnd]

		var batchNodes []types.Node
		var batchEdges []types.Edge
		var batchRawCalls []types.RawCall

		for i, path := range batchPaths {
			globalIdx := batchStart + i
			if total >= progressInterval && globalIdx%progressInterval == 0 && globalIdx > 0 {
				fmt.Printf("  AST extraction: %d/%d files (%d%%)\n", globalIdx, total, globalIdx*100/total)
			}

			// Blade templates have compound extension .blade.php
			var extractor Extractor
			if strings.HasSuffix(strings.ToLower(path), ".blade.php") {
				extractor = ExtractBlade
			} else {
				ext := strings.ToLower(filepath.Ext(path))
				var ok bool
				extractor, ok = getDispatch()[ext]
				if !ok {
					continue
				}
			}

			// Check cache before running extractor.
			if cached := cache.Load(path, root, "ast"); cached != nil {
				batchNodes = append(batchNodes, cached.Nodes...)
				batchEdges = append(batchEdges, cached.Edges...)
				batchRawCalls = append(batchRawCalls, cached.RawCalls...)
				cacheHits++
				continue
			}

			result := extractor(path)
			batchNodes = append(batchNodes, result.Nodes...)
			batchEdges = append(batchEdges, result.Edges...)
			batchRawCalls = append(batchRawCalls, result.RawCalls...)

			// Save to cache (best-effort).
			_ = cache.Save(path, result, root, "ast")
		}

		// Flush this batch to disk.
		batchResult := &types.ExtractionResult{
			Nodes:    batchNodes,
			Edges:    batchEdges,
			RawCalls: batchRawCalls,
		}
		batchFile := filepath.Join(batchDir, fmt.Sprintf("batch_%04d.json", batchStart/batchSize))
		if err := writeBatch(batchFile, batchResult); err != nil {
			fmt.Printf("  Warning: failed to write batch: %v\n", err)
		}
		batchFiles = append(batchFiles, batchFile)

		// Release batch memory and return pages to OS.
		batchNodes = nil
		batchEdges = nil
		batchRawCalls = nil
		batchResult = nil
		debug.FreeOSMemory()
	}

	if total >= progressInterval {
		fmt.Printf("  AST extraction: %d/%d files (100%%)\n", total, total)
	}
	if cacheHits > 0 {
		fmt.Printf("  Cache: %d/%d files from cache\n", cacheHits, total)
	}

	// Merge all batches from disk.
	var allNodes []types.Node
	var allEdges []types.Edge
	var allRawCalls []types.RawCall

	for _, bf := range batchFiles {
		batch, err := readBatch(bf)
		if err != nil {
			fmt.Printf("  Warning: failed to read batch: %v\n", err)
			continue
		}
		allNodes = append(allNodes, batch.Nodes...)
		allEdges = append(allEdges, batch.Edges...)
		allRawCalls = append(allRawCalls, batch.RawCalls...)

		// Remove batch file immediately to free disk space.
		os.Remove(bf)
	}

	return finalize(allNodes, allEdges, allRawCalls, paths, root)
}

// extractInMemory is the fallback path when temp dir creation fails.
func extractInMemory(paths []string, root string) *types.ExtractionResult {
	var allNodes []types.Node
	var allEdges []types.Edge
	var allRawCalls []types.RawCall

	total := len(paths)
	for i, path := range paths {
		if total >= 100 && i%100 == 0 && i > 0 {
			fmt.Printf("  AST extraction: %d/%d files (%d%%)\n", i, total, i*100/total)
		}

		var extractor Extractor
		if strings.HasSuffix(strings.ToLower(path), ".blade.php") {
			extractor = ExtractBlade
		} else {
			ext := strings.ToLower(filepath.Ext(path))
			var ok bool
			extractor, ok = getDispatch()[ext]
			if !ok {
				continue
			}
		}

		if cached := cache.Load(path, root, "ast"); cached != nil {
			allNodes = append(allNodes, cached.Nodes...)
			allEdges = append(allEdges, cached.Edges...)
			allRawCalls = append(allRawCalls, cached.RawCalls...)
			continue
		}

		result := extractor(path)
		allNodes = append(allNodes, result.Nodes...)
		allEdges = append(allEdges, result.Edges...)
		allRawCalls = append(allRawCalls, result.RawCalls...)
		_ = cache.Save(path, result, root, "ast")

		if (i+1)%500 == 0 {
			debug.FreeOSMemory()
		}
	}

	return finalize(allNodes, allEdges, allRawCalls, paths, root)
}

// finalize performs ID remapping, cross-file call resolution, and path relativization.
func finalize(allNodes []types.Node, allEdges []types.Edge, allRawCalls []types.RawCall, paths []string, root string) *types.ExtractionResult {
	// Remap file node IDs from absolute-path-derived to project-relative
	// so graph.json edge endpoints are stable across machines.
	idRemap := make(map[string]string)
	for _, path := range paths {
		oldID := MakeID(path)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		newID := MakeID(rel)
		if oldID != newID {
			idRemap[oldID] = newID
		}
	}
	if len(idRemap) > 0 {
		for i := range allNodes {
			if newID, ok := idRemap[allNodes[i].ID]; ok {
				allNodes[i].ID = newID
			}
		}
		for i := range allEdges {
			if newID, ok := idRemap[allEdges[i].Source]; ok {
				allEdges[i].Source = newID
			}
			if newID, ok := idRemap[allEdges[i].Target]; ok {
				allEdges[i].Target = newID
			}
		}
	}

	// Cross-file call resolution: resolve raw_calls from all files against
	// the global node label index.
	globalLabelToNID := buildLabelIndex(allNodes)
	existingPairs := make(map[[2]string]bool)
	for _, e := range allEdges {
		existingPairs[[2]string{e.Source, e.Target}] = true
	}

	for _, rc := range allRawCalls {
		if rc.Callee == "" || rc.IsMemberCall {
			continue
		}
		tgt, ok := globalLabelToNID[strings.ToLower(rc.Callee)]
		if !ok || tgt == rc.CallerNID {
			continue
		}
		pair := [2]string{rc.CallerNID, tgt}
		if existingPairs[pair] {
			continue
		}
		existingPairs[pair] = true
		allEdges = append(allEdges, types.Edge{
			Source:          rc.CallerNID,
			Target:          tgt,
			Relation:        "calls",
			Confidence:      types.Inferred,
			ConfidenceScore: 0.8,
			SourceFile:      rc.SourceFile,
			SourceLocation:  rc.SourceLoc,
			Weight:          1.0,
		})
	}

	// Relativize source_file fields for portability.
	for i := range allNodes {
		allNodes[i].SourceFile = relativize(allNodes[i].SourceFile, root)
	}
	for i := range allEdges {
		allEdges[i].SourceFile = relativize(allEdges[i].SourceFile, root)
	}

	return &types.ExtractionResult{
		Nodes: allNodes,
		Edges: allEdges,
	}
}

func writeBatch(path string, result *types.ExtractionResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readBatch(path string) (*types.ExtractionResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result types.ExtractionResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// inferRoot finds the common directory prefix for a set of paths.
func inferRoot(paths []string) string {
	if len(paths) == 0 {
		return "."
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	abs := make([]string, len(paths))
	for i, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			abs[i] = p
		} else {
			abs[i] = a
		}
	}
	parts0 := strings.Split(abs[0], string(filepath.Separator))
	commonLen := len(parts0)
	for _, p := range abs[1:] {
		partsN := strings.Split(p, string(filepath.Separator))
		n := commonLen
		if len(partsN) < n {
			n = len(partsN)
		}
		match := 0
		for i := 0; i < n; i++ {
			if parts0[i] != partsN[i] {
				break
			}
			match++
		}
		if match < commonLen {
			commonLen = match
		}
	}
	if commonLen == 0 {
		return "."
	}
	return strings.Join(parts0[:commonLen], string(filepath.Separator))
}

// relativize converts an absolute path to relative if possible.
func relativize(path, root string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
