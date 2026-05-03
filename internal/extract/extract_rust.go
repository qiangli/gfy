package extract

import (
	"os"
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/internal/types"
)

// ExtractRust extracts structs, impl blocks, traits, functions, use statements from a .rs file.
func ExtractRust(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}

	lang := grammars.RustLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "gotreesitter: parse: " + err.Error()}
	}
	root := tree.RootNode()

	stem := FileStem(path)
	strPath := path
	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	var functionBodies []bodyEntry

	addNode := func(nid, label string, line int, declNode *ts.Node) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{
				ID: nid, Label: label, FileType: string(types.Code),
				SourceFile: strPath, SourceLocation: SourceLoc(line),
			})
			if declNode != nil {
				if prev := declNode.PrevSibling(); prev != nil && prev.Type(lang) == "line_comment" {
					SetComment(nodes, nid, ReadText(source, prev.StartByte(), prev.EndByte()))
				}
			}
		}
	}
	addEdge := func(src, tgt, relation string, line int) {
		edges = append(edges, types.Edge{
			Source: src, Target: tgt, Relation: relation,
			Confidence: types.Extracted, SourceFile: strPath,
			SourceLocation: SourceLoc(line), Weight: 1.0,
		})
	}

	fileNID := MakeID(path)
	addNode(fileNID, filepath.Base(path), 1, nil)

	rdText := func(n *ts.Node) string { return ReadText(source, n.StartByte(), n.EndByte()) }

	var walk func(node *ts.Node, parentImplNID string)
	walk = func(node *ts.Node, parentImplNID string) {
		t := node.Type(lang)

		switch t {
		case "struct_item", "enum_item", "trait_item":
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode != nil {
				name := rdText(nameNode)
				nid := MakeID(stem, name)
				line := int(node.StartPoint().Row) + 1
				addNode(nid, name, line, node)
				addEdge(fileNID, nid, "contains", line)
			}
			return

		case "impl_item":
			typeNode := node.ChildByFieldName("type", lang)
			if typeNode == nil {
				return
			}
			typeName := rdText(typeNode)
			implNID := MakeID(stem, typeName)
			line := int(node.StartPoint().Row) + 1
			addNode(implNID, typeName, line, node)
			body := node.ChildByFieldName("body", lang)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), implNID)
				}
			}
			return

		case "function_item":
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode == nil {
				return
			}
			funcName := rdText(nameNode)
			line := int(node.StartPoint().Row) + 1
			var funcNID string
			if parentImplNID != "" {
				funcNID = MakeID(parentImplNID, funcName)
				addNode(funcNID, "."+funcName+"()", line, node)
				addEdge(parentImplNID, funcNID, "method", line)
			} else {
				funcNID = MakeID(stem, funcName)
				addNode(funcNID, funcName+"()", line, node)
				addEdge(fileNID, funcNID, "contains", line)
			}
			body := node.ChildByFieldName("body", lang)
			if body != nil {
				functionBodies = append(functionBodies, bodyEntry{funcNID, body})
			}
			return

		case "use_declaration":
			arg := node.ChildByFieldName("argument", lang)
			if arg != nil {
				raw := rdText(arg)
				raw = strings.Trim(raw, "{} ")
				// Take the last non-empty segment.
				parts := strings.Split(raw, "::")
				moduleName := ""
				for i := len(parts) - 1; i >= 0; i-- {
					seg := strings.Trim(parts[i], "{} *")
					if seg != "" {
						moduleName = seg
						break
					}
				}
				if moduleName != "" {
					addEdge(fileNID, MakeID(moduleName), "imports_from", int(node.StartPoint().Row)+1)
				}
			}
			return
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i), "")
		}
	}

	walk(root, "")

	// Call graph pass.
	labelToNID := buildLabelIndex(nodes)
	seenCallPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall

	var walkCalls func(node *ts.Node, callerNID string)
	walkCalls = func(node *ts.Node, callerNID string) {
		t := node.Type(lang)
		if t == "function_item" {
			return
		}
		// Behavioral tagging for Rust macros.
		if t == "macro_invocation" && node.ChildCount() > 0 {
			macroName := rdText(node.Child(0))
			switch macroName {
			case "panic", "unreachable", "todo", "unimplemented":
				TagNode(nodes, callerNID, "throws")
			case "println", "eprintln", "print", "eprint",
				"log", "info", "warn", "error", "debug", "trace":
				TagNode(nodes, callerNID, "logs")
			case "info_span", "debug_span", "warn_span", "error_span", "trace_span", "instrument":
				TagNode(nodes, callerNID, "otel")
			}
		}

		// Behavioral tagging for ? operator (error handling).
		if t == "try_expression" {
			TagNode(nodes, callerNID, "catches")
		}
		if t == "unsafe_block" {
			TagNode(nodes, callerNID, "unsafe")
		}
		if t == "await_expression" {
			TagNode(nodes, callerNID, "async")
		}

		if t == "call_expression" {
			funcNode := node.ChildByFieldName("function", lang)
			var calleeName string
			isMemberCall := false
			if funcNode != nil {
				ft := funcNode.Type(lang)
				switch ft {
				case "identifier":
					calleeName = rdText(funcNode)
				case "field_expression":
					isMemberCall = true
					field := funcNode.ChildByFieldName("field", lang)
					if field != nil {
						calleeName = rdText(field)
					}
				case "scoped_identifier":
					nameNode := funcNode.ChildByFieldName("name", lang)
					if nameNode != nil {
						calleeName = rdText(nameNode)
					}
				}
			}
			if funcNode != nil {
				fullPath := rdText(funcNode)
				if strings.Contains(fullPath, "opentelemetry") || strings.Contains(fullPath, "prometheus") ||
					strings.Contains(fullPath, "tracing::") {
					TagNode(nodes, callerNID, "otel")
				}
			}
			if calleeName != "" {
				resolveCall(calleeName, isMemberCall, callerNID, labelToNID, seenCallPairs,
					&edges, &rawCalls, strPath, int(node.StartPoint().Row)+1)
			}
		}
		for i := 0; i < int(node.ChildCount()); i++ {
			walkCalls(node.Child(i), callerNID)
		}
	}

	for _, fb := range functionBodies {
		walkCalls(fb.node, fb.callerNID)
	}

	return &types.ExtractionResult{Nodes: nodes, Edges: edges, RawCalls: rawCalls}
}
