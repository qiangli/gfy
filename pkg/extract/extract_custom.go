package extract

import (
	"os"
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/pkg/types"
)

// --- Zig ---

func ExtractZig(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.ZigLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	var funcBodies []bodyEntry

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(n *ts.Node, parentNID string)
	walk = func(n *ts.Node, parentNID string) {
		t := n.Type(lang)
		switch t {
		case "function_declaration":
			nameNode := n.ChildByFieldName("name", lang)
			if nameNode != nil {
				name := rdText(nameNode)
				line := int(n.StartPoint().Row) + 1
				nid := MakeID(stem, name)
				if parentNID != "" {
					nid = MakeID(parentNID, name)
					addN(nid, "."+name+"()", line)
					addE(parentNID, nid, "method", line)
				} else {
					addN(nid, name+"()", line)
					addE(fileNID, nid, "contains", line)
				}
				// Comment extraction.
				if prev := n.PrevSibling(); prev != nil && prev.Type(lang) == "comment" {
					SetComment(nodes, nid, rdText(prev))
				}
				body := n.ChildByFieldName("body", lang)
				if body != nil {
					funcBodies = append(funcBodies, bodyEntry{nid, body})
				}
			}
			return
		case "variable_declaration":
			// Detect struct/enum/union declarations
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				ct := child.Type(lang)
				if ct == "identifier" {
					// Check if next meaningful child is a struct/enum/union
					name := rdText(child)
					for j := i + 1; j < int(n.ChildCount()); j++ {
						valChild := n.Child(j)
						vt := valChild.Type(lang)
						if vt == "struct_declaration" || vt == "enum_declaration" || vt == "union_declaration" {
							nid := MakeID(stem, name)
							line := int(n.StartPoint().Row) + 1
							addN(nid, name, line)
							addE(fileNID, nid, "contains", line)
							// Walk struct body for methods
							body := valChild.ChildByFieldName("body", lang)
							if body != nil {
								for k := 0; k < int(body.ChildCount()); k++ {
									walk(body.Child(k), nid)
								}
							}
							return
						}
					}
				}
			}
		}
		// Check for @import
		if t == "builtin_function" {
			text := rdText(n)
			if strings.Contains(text, "@import") || strings.Contains(text, "@cImport") {
				// Extract string argument
				for i := 0; i < int(n.ChildCount()); i++ {
					child := n.Child(i)
					if child.Type(lang) == "string_literal" || child.Type(lang) == "string" {
						raw := strings.Trim(rdText(child), "\"")
						raw = strings.TrimSuffix(raw, ".zig")
						if raw != "" {
							addE(fileNID, MakeID(raw), "imports", int(n.StartPoint().Row)+1)
						}
					}
				}
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), "")
		}
	}
	walk(tree.RootNode(), "")

	labelToNID := buildLabelIndex(nodes)
	seenPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall
	var walkC func(n *ts.Node, caller string)
	walkC = func(n *ts.Node, caller string) {
		nt := n.Type(lang)
		if nt == "function_declaration" {
			return
		}
		if nt == "call_expression" {
			funcNode := n.ChildByFieldName("function", lang)
			if funcNode != nil {
				name := rdText(funcNode)
				if strings.Contains(name, ".") {
					parts := strings.Split(name, ".")
					name = parts[len(parts)-1]
				}
				// Zig behavioral tagging.
				if name == "@panic" {
					TagNode(nodes, caller, "throws")
				}
				if strings.HasPrefix(name, "std.debug.") || name == "std.log" {
					TagNode(nodes, caller, "logs")
				}
				resolveCall(name, false, caller, labelToNID, seenPairs, &edges, &rawCalls, strPath, int(n.StartPoint().Row)+1)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkC(n.Child(i), caller)
		}
	}
	for _, fb := range funcBodies {
		walkC(fb.node, fb.callerNID)
	}
	return &types.ExtractionResult{Nodes: nodes, Edges: edges, RawCalls: rawCalls}
}

// --- PowerShell ---

func ExtractPowerShell(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.PowershellLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	var funcBodies []bodyEntry

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(n *ts.Node, parentClassNID string)
	walk = func(n *ts.Node, parentClassNID string) {
		t := n.Type(lang)
		switch t {
		case "function_statement":
			// Find function_name child
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "function_name" {
					name := rdText(child)
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, name+"()", line)
					addE(fileNID, nid, "contains", line)
					// Find script_block body
					body := findScriptBlockBody(n, lang)
					if body != nil {
						funcBodies = append(funcBodies, bodyEntry{nid, body})
					}
					break
				}
			}
			return
		case "class_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "simple_name" || child.Type(lang) == "identifier" {
					name := rdText(child)
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, name, line)
					addE(fileNID, nid, "contains", line)
					// Recurse body
					for j := i + 1; j < int(n.ChildCount()); j++ {
						walk(n.Child(j), nid)
					}
					return
				}
			}
			return
		case "class_method_definition":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "simple_name" || child.Type(lang) == "identifier" {
					name := rdText(child)
					line := int(n.StartPoint().Row) + 1
					if parentClassNID != "" {
						nid := MakeID(parentClassNID, name)
						addN(nid, "."+name+"()", line)
						addE(parentClassNID, nid, "method", line)
					} else {
						nid := MakeID(stem, name)
						addN(nid, name+"()", line)
						addE(fileNID, nid, "contains", line)
					}
					break
				}
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClassNID)
		}
	}
	walk(tree.RootNode(), "")

	return &types.ExtractionResult{Nodes: nodes, Edges: edges}
}

func findScriptBlockBody(n *ts.Node, lang *ts.Language) *ts.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(lang) == "script_block" {
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				if sub.Type(lang) == "script_block_body" {
					return sub
				}
			}
		}
	}
	return nil
}

// --- Elixir ---

func ExtractElixir(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.ElixirLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	var funcBodies []bodyEntry

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	elixirKWSkip := map[string]bool{
		"defmodule": true, "def": true, "defp": true, "alias": true, "import": true,
		"require": true, "use": true, "if": true, "case": true, "cond": true,
		"for": true, "with": true, "unless": true, "raise": true, "try": true,
		"receive": true, "send": true, "spawn": true,
	}

	var walk func(n *ts.Node, moduleNID string)
	walk = func(n *ts.Node, moduleNID string) {
		t := n.Type(lang)
		if t == "call" {
			// Check first child for keyword
			if n.ChildCount() > 0 {
				first := n.Child(0)
				if first.Type(lang) == "identifier" {
					kw := rdText(first)
					switch kw {
					case "defmodule":
						// Module name from arguments
						if n.ChildCount() > 1 {
							args := n.Child(1)
							modName := getElixirAlias(args, lang, source)
							if modName != "" {
								nid := MakeID(stem, modName)
								line := int(n.StartPoint().Row) + 1
								addN(nid, modName, line)
								addE(fileNID, nid, "contains", line)
								// Walk do block
								for i := 0; i < int(n.ChildCount()); i++ {
									child := n.Child(i)
									if child.Type(lang) == "do_block" {
										for j := 0; j < int(child.ChildCount()); j++ {
											walk(child.Child(j), nid)
										}
									}
								}
								return
							}
						}
					case "def", "defp":
						if n.ChildCount() > 1 {
							args := n.Child(1)
							funcName := getElixirFuncName(args, lang, source)
							if funcName != "" {
								line := int(n.StartPoint().Row) + 1
								parent := moduleNID
								if parent == "" {
									parent = fileNID
								}
								nid := MakeID(parent, funcName)
								addN(nid, funcName+"()", line)
								if moduleNID != "" {
									addE(moduleNID, nid, "method", line)
								} else {
									addE(fileNID, nid, "contains", line)
								}
								// Find do_block for call analysis
								for i := 0; i < int(n.ChildCount()); i++ {
									child := n.Child(i)
									if child.Type(lang) == "do_block" {
										funcBodies = append(funcBodies, bodyEntry{nid, child})
									}
								}
								return
							}
						}
					case "alias", "import", "require", "use":
						if n.ChildCount() > 1 {
							args := n.Child(1)
							modName := getElixirAlias(args, lang, source)
							if modName != "" {
								addE(fileNID, MakeID(modName), "imports", int(n.StartPoint().Row)+1)
								return
							}
						}
					}
					// Skip other keywords
					if elixirKWSkip[kw] {
						return
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), moduleNID)
		}
	}
	walk(tree.RootNode(), "")

	// Call resolution
	labelToNID := buildLabelIndex(nodes)
	seenPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall
	var walkC func(n *ts.Node, caller string)
	walkC = func(n *ts.Node, caller string) {
		if n.Type(lang) == "call" && n.ChildCount() > 0 {
			first := n.Child(0)
			if first.Type(lang) == "identifier" {
				name := rdText(first)
				// Elixir behavioral tagging.
				if name == "raise" || name == "throw" {
					TagNode(nodes, caller, "throws")
				}
				if name == "rescue" {
					TagNode(nodes, caller, "catches")
				}
				if name == "IO.puts" || name == "IO.inspect" || name == "IO.warn" {
					TagNode(nodes, caller, "logs")
				}
				if !elixirKWSkip[name] {
					resolveCall(name, false, caller, labelToNID, seenPairs, &edges, &rawCalls, strPath, int(n.StartPoint().Row)+1)
				}
			}
			// Check for Logger.* calls.
			if first.Type(lang) == "dot" {
				raw := rdText(first)
				if strings.HasPrefix(raw, "Logger.") || strings.HasPrefix(raw, "IO.") {
					TagNode(nodes, caller, "logs")
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkC(n.Child(i), caller)
		}
	}
	for _, fb := range funcBodies {
		walkC(fb.node, fb.callerNID)
	}

	return &types.ExtractionResult{Nodes: nodes, Edges: edges, RawCalls: rawCalls}
}

func getElixirAlias(n *ts.Node, lang *ts.Language, source []byte) string {
	// Walk children looking for alias or identifier nodes
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		ct := child.Type(lang)
		if ct == "alias" || ct == "identifier" {
			return ReadText(source, child.StartByte(), child.EndByte())
		}
		// Recurse for nested aliases (e.g., MyApp.Accounts.User)
		if ct == "dot" {
			return ReadText(source, n.StartByte(), n.EndByte())
		}
	}
	// Fallback: read entire node text and extract last segment
	text := ReadText(source, n.StartByte(), n.EndByte())
	text = strings.TrimSpace(text)
	if text != "" {
		parts := strings.Split(text, ".")
		return parts[len(parts)-1]
	}
	return ""
}

func getElixirFuncName(n *ts.Node, lang *ts.Language, source []byte) string {
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		ct := child.Type(lang)
		if ct == "identifier" {
			return ReadText(source, child.StartByte(), child.EndByte())
		}
		if ct == "call" && child.ChildCount() > 0 {
			first := child.Child(0)
			if first.Type(lang) == "identifier" {
				return ReadText(source, first.StartByte(), first.EndByte())
			}
		}
	}
	return ""
}

// --- Julia ---

func ExtractJulia(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.JuliaLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	var funcBodies []bodyEntry

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(n *ts.Node, moduleNID string)
	walk = func(n *ts.Node, moduleNID string) {
		t := n.Type(lang)
		parent := moduleNID
		if parent == "" {
			parent = fileNID
		}

		switch t {
		case "module_definition":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name := rdText(child)
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, name, line)
					addE(fileNID, nid, "contains", line)
					for j := i + 1; j < int(n.ChildCount()); j++ {
						walk(n.Child(j), nid)
					}
					return
				}
			}

		case "struct_definition":
			nameNode := n.ChildByFieldName("name", lang)
			if nameNode != nil {
				name := rdText(nameNode)
				nid := MakeID(stem, name)
				line := int(n.StartPoint().Row) + 1
				addN(nid, name, line)
				addE(parent, nid, "contains", line)
			}
			return

		case "abstract_definition":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name := rdText(child)
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, name, line)
					addE(parent, nid, "contains", line)
					return
				}
			}

		case "function_definition":
			// Extract name from signature
			sig := n.ChildByFieldName("signature", lang)
			if sig == nil {
				// Try children
				for i := 0; i < int(n.ChildCount()); i++ {
					child := n.Child(i)
					if child.Type(lang) == "call_expression" || child.Type(lang) == "identifier" {
						sig = child
						break
					}
				}
			}
			if sig != nil {
				var name string
				if sig.Type(lang) == "identifier" {
					name = rdText(sig)
				} else {
					// call_expression: get identifier child
					for i := 0; i < int(sig.ChildCount()); i++ {
						child := sig.Child(i)
						if child.Type(lang) == "identifier" {
							name = rdText(child)
							break
						}
					}
				}
				if name != "" {
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, name+"()", line)
					addE(parent, nid, "contains", line)
					// Store body for call analysis
					body := n.ChildByFieldName("body", lang)
					if body != nil {
						funcBodies = append(funcBodies, bodyEntry{nid, body})
					}
				}
			}
			return

		case "assignment":
			// Short function syntax: foo(x) = expr
			if n.ChildCount() >= 2 {
				lhs := n.Child(0)
				if lhs.Type(lang) == "call_expression" {
					for i := 0; i < int(lhs.ChildCount()); i++ {
						child := lhs.Child(i)
						if child.Type(lang) == "identifier" {
							name := rdText(child)
							nid := MakeID(stem, name)
							line := int(n.StartPoint().Row) + 1
							addN(nid, name+"()", line)
							addE(parent, nid, "contains", line)
							return
						}
					}
				}
			}

		case "using_statement", "import_statement":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name := rdText(child)
					addE(fileNID, MakeID(name), "imports", int(n.StartPoint().Row)+1)
				}
			}
			return
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), moduleNID)
		}
	}
	walk(tree.RootNode(), "")

	// Call graph
	labelToNID := buildLabelIndex(nodes)
	seenPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall
	var walkC func(n *ts.Node, caller string)
	walkC = func(n *ts.Node, caller string) {
		if n.Type(lang) == "function_definition" {
			return
		}
		if n.Type(lang) == "call_expression" {
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name := rdText(child)
					// Julia behavioral tagging.
					if name == "error" || name == "throw" {
						TagNode(nodes, caller, "throws")
					}
					if name == "println" || name == "print" || name == "@info" || name == "@warn" || name == "@error" || name == "@debug" {
						TagNode(nodes, caller, "logs")
					}
					resolveCall(name, false, caller, labelToNID, seenPairs, &edges, &rawCalls, strPath, int(n.StartPoint().Row)+1)
					break
				}
			}
		}
		// Julia try/catch.
		if n.Type(lang) == "try_statement" || n.Type(lang) == "catch_clause" {
			TagNode(nodes, caller, "catches")
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walkC(n.Child(i), caller)
		}
	}
	for _, fb := range funcBodies {
		walkC(fb.node, fb.callerNID)
	}

	return &types.ExtractionResult{Nodes: nodes, Edges: edges, RawCalls: rawCalls}
}

// --- Objective-C ---

func ExtractObjC(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.ObjcLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(n *ts.Node, classNID string)
	walk = func(n *ts.Node, classNID string) {
		t := n.Type(lang)
		switch t {
		case "preproc_include":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				ct := child.Type(lang)
				if ct == "system_lib_string" || ct == "string_literal" {
					raw := strings.Trim(rdText(child), "\"<> ")
					parts := strings.Split(raw, "/")
					modName := strings.Split(parts[len(parts)-1], ".")[0]
					if modName != "" {
						addE(fileNID, MakeID(modName), "imports", int(n.StartPoint().Row)+1)
					}
				}
			}
			return

		case "class_interface":
			// First identifier is class name, second (after :) is superclass
			identifiers := collectChildrenByType(n, lang, "identifier")
			if len(identifiers) > 0 {
				className := rdText(identifiers[0])
				classID := MakeID(stem, className)
				line := int(n.StartPoint().Row) + 1
				addN(classID, className, line)
				addE(fileNID, classID, "contains", line)
				if len(identifiers) > 1 {
					superName := rdText(identifiers[1])
					superNID := MakeID(superName)
					addN(superNID, superName, line)
					addE(classID, superNID, "inherits", line)
				}
			}
			return

		case "class_implementation":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					className := rdText(child)
					classID := MakeID(stem, className)
					line := int(n.StartPoint().Row) + 1
					addN(classID, className, line)
					// Walk body for methods
					for j := i + 1; j < int(n.ChildCount()); j++ {
						walk(n.Child(j), classID)
					}
					return
				}
			}

		case "protocol_declaration":
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" {
					name := rdText(child)
					nid := MakeID(stem, name)
					line := int(n.StartPoint().Row) + 1
					addN(nid, "<"+name+">", line)
					addE(fileNID, nid, "contains", line)
					return
				}
			}

		case "method_declaration", "method_definition":
			// Compound method name from selector
			selector := n.ChildByFieldName("selector", lang)
			var methodName string
			if selector != nil {
				methodName = rdText(selector)
			} else {
				// Fallback: collect identifier children
				ids := collectChildrenByType(n, lang, "identifier")
				if len(ids) > 0 {
					methodName = rdText(ids[0])
				}
			}
			if methodName != "" {
				methodName = strings.ReplaceAll(methodName, ":", "_")
				line := int(n.StartPoint().Row) + 1
				if classNID != "" {
					nid := MakeID(classNID, methodName)
					addN(nid, "."+methodName+"()", line)
					addE(classNID, nid, "method", line)
				} else {
					nid := MakeID(stem, methodName)
					addN(nid, methodName+"()", line)
					addE(fileNID, nid, "contains", line)
				}
			}
			return
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), classNID)
		}
	}
	walk(tree.RootNode(), "")

	return &types.ExtractionResult{Nodes: nodes, Edges: edges}
}

// --- Verilog ---

func ExtractVerilog(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}
	lang := grammars.VerilogLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "parse: " + err.Error()}
	}

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)

	addN := func(nid, label string, line int) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{ID: nid, Label: label, FileType: string(types.Code), SourceFile: strPath, SourceLocation: SourceLoc(line)})
		}
	}
	addE := func(src, tgt, rel string, line int) {
		edges = append(edges, types.Edge{Source: src, Target: tgt, Relation: rel, Confidence: types.Extracted, SourceFile: strPath, SourceLocation: SourceLoc(line), Weight: 1.0})
	}
	fileNID := MakeID(path)
	addN(fileNID, filepath.Base(path), 1)
	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(n *ts.Node, moduleNID string)
	walk = func(n *ts.Node, moduleNID string) {
		t := n.Type(lang)
		parent := moduleNID
		if parent == "" {
			parent = fileNID
		}

		switch t {
		case "module_declaration":
			nameNode := n.ChildByFieldName("name", lang)
			if nameNode == nil {
				// Fallback: first identifier child
				for i := 0; i < int(n.ChildCount()); i++ {
					child := n.Child(i)
					if child.Type(lang) == "identifier" || child.Type(lang) == "simple_identifier" {
						nameNode = child
						break
					}
				}
			}
			if nameNode != nil {
				name := rdText(nameNode)
				nid := MakeID(stem, name)
				line := int(n.StartPoint().Row) + 1
				addN(nid, name, line)
				addE(fileNID, nid, "contains", line)
				for i := 0; i < int(n.ChildCount()); i++ {
					walk(n.Child(i), nid)
				}
				return
			}

		case "function_declaration", "function_prototype":
			nameNode := n.ChildByFieldName("name", lang)
			if nameNode != nil {
				name := rdText(nameNode)
				nid := MakeID(stem, name)
				line := int(n.StartPoint().Row) + 1
				addN(nid, name+"()", line)
				addE(parent, nid, "contains", line)
			}
			return

		case "task_declaration":
			nameNode := n.ChildByFieldName("name", lang)
			if nameNode != nil {
				name := rdText(nameNode)
				nid := MakeID(stem, name)
				line := int(n.StartPoint().Row) + 1
				addN(nid, name+"()", line)
				addE(parent, nid, "contains", line)
			}
			return

		case "module_instantiation":
			// First identifier child is module type
			for i := 0; i < int(n.ChildCount()); i++ {
				child := n.Child(i)
				if child.Type(lang) == "identifier" || child.Type(lang) == "simple_identifier" {
					modType := rdText(child)
					addE(parent, MakeID(modType), "instantiates", int(n.StartPoint().Row)+1)
					return
				}
			}
		}

		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), moduleNID)
		}
	}
	walk(tree.RootNode(), "")

	return &types.ExtractionResult{Nodes: nodes, Edges: edges}
}

// collectChildrenByType returns all direct children with the given type.
func collectChildrenByType(n *ts.Node, lang *ts.Language, childType string) []*ts.Node {
	var result []*ts.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type(lang) == childType {
			result = append(result, child)
		}
	}
	return result
}
