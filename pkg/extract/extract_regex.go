package extract

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiangli/gfy/pkg/types"
)

// --- Dart (regex-based) ---

var (
	dartClassRe  = regexp.MustCompile(`(?m)^(?:abstract\s+)?(?:class|mixin)\s+(\w+)`)
	dartFuncRe   = regexp.MustCompile(`(?m)^\s*(?:static\s+|async\s+)?(?:\w+\s+)+(\w+)\s*\(`)
	dartImportRe = regexp.MustCompile(`(?m)^import\s+['"]([^'"]+)['"]`)
	dartSkipKW   = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "return": true}
)

// ExtractDart extracts classes, functions, and imports from a .dart file using regex.
func ExtractDart(path string) *types.ExtractionResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	source := string(data)
	stem := FileStem(path)
	strPath := path

	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)

	fileNID := MakeID(path)
	nodes = append(nodes, types.Node{
		ID: fileNID, Label: filepath.Base(path), FileType: string(types.Code),
		SourceFile: strPath, SourceLocation: SourceLoc(1),
	})
	seenIDs[fileNID] = true

	// Classes/mixins
	for _, m := range dartClassRe.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		nid := MakeID(stem, name)
		line := strings.Count(source[:m[0]], "\n") + 1
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{
				ID: nid, Label: name, FileType: string(types.Code),
				SourceFile: strPath, SourceLocation: SourceLoc(line),
			})
			edges = append(edges, types.Edge{
				Source: fileNID, Target: nid, Relation: "contains",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(line), Weight: 1.0,
			})
		}
	}

	// Functions
	for _, m := range dartFuncRe.FindAllStringSubmatchIndex(source, -1) {
		name := source[m[2]:m[3]]
		if dartSkipKW[name] {
			continue
		}
		nid := MakeID(stem, name)
		line := strings.Count(source[:m[0]], "\n") + 1
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{
				ID: nid, Label: name + "()", FileType: string(types.Code),
				SourceFile: strPath, SourceLocation: SourceLoc(line),
			})
			edges = append(edges, types.Edge{
				Source: fileNID, Target: nid, Relation: "contains",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(line), Weight: 1.0,
			})
		}
	}

	// Imports
	for _, m := range dartImportRe.FindAllStringSubmatchIndex(source, -1) {
		raw := source[m[2]:m[3]]
		parts := strings.Split(raw, "/")
		moduleName := parts[len(parts)-1]
		moduleName = strings.TrimSuffix(moduleName, ".dart")
		if moduleName != "" {
			line := strings.Count(source[:m[0]], "\n") + 1
			edges = append(edges, types.Edge{
				Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(line), Weight: 1.0,
			})
		}
	}

	return &types.ExtractionResult{Nodes: nodes, Edges: edges}
}

// --- Blade (regex-based) ---

var (
	bladeIncludeRe   = regexp.MustCompile(`@include\s*\(\s*'([^']+)'`)
	bladeLivewireRe  = regexp.MustCompile(`<livewire:([a-zA-Z0-9._-]+)`)
	bladeWireClickRe = regexp.MustCompile(`wire:click="(\w+)"`)
)

// ExtractBlade extracts includes, livewire components, and wire bindings from a .blade.php file.
func ExtractBlade(path string) *types.ExtractionResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	source := string(data)
	stem := FileStem(path)
	strPath := path

	var nodes []types.Node
	var edges []types.Edge

	fileNID := MakeID(path)
	nodes = append(nodes, types.Node{
		ID: fileNID, Label: filepath.Base(path), FileType: string(types.Code),
		SourceFile: strPath, SourceLocation: SourceLoc(1),
	})

	// @include('path.to.partial')
	for _, m := range bladeIncludeRe.FindAllStringSubmatchIndex(source, -1) {
		raw := source[m[2]:m[3]]
		resolved := strings.ReplaceAll(raw, ".", "/")
		tgtNID := MakeID(stem, resolved)
		line := strings.Count(source[:m[0]], "\n") + 1
		edges = append(edges, types.Edge{
			Source: fileNID, Target: tgtNID, Relation: "includes",
			Confidence: types.Extracted, SourceFile: strPath,
			SourceLocation: SourceLoc(line), Weight: 1.0,
		})
	}

	// <livewire:component.name />
	for _, m := range bladeLivewireRe.FindAllStringSubmatchIndex(source, -1) {
		component := source[m[2]:m[3]]
		tgtNID := MakeID(stem, component)
		line := strings.Count(source[:m[0]], "\n") + 1
		edges = append(edges, types.Edge{
			Source: fileNID, Target: tgtNID, Relation: "uses_component",
			Confidence: types.Extracted, SourceFile: strPath,
			SourceLocation: SourceLoc(line), Weight: 1.0,
		})
	}

	// wire:click="methodName"
	for _, m := range bladeWireClickRe.FindAllStringSubmatchIndex(source, -1) {
		method := source[m[2]:m[3]]
		tgtNID := MakeID(stem, method)
		line := strings.Count(source[:m[0]], "\n") + 1
		edges = append(edges, types.Edge{
			Source: fileNID, Target: tgtNID, Relation: "binds_method",
			Confidence: types.Extracted, SourceFile: strPath,
			SourceLocation: SourceLoc(line), Weight: 1.0,
		})
	}

	return &types.ExtractionResult{Nodes: nodes, Edges: edges}
}
