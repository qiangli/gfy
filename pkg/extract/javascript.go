package extract

import (
	"os"
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/pkg/types"
)

// ExtractJS extracts classes, functions, imports, and call graphs from a .js/.jsx/.mjs file.
func ExtractJS(path string) *types.ExtractionResult {
	return extractJSWith(path, grammars.JavascriptLanguage())
}

// ExtractTS extracts from .ts/.tsx files using the TypeScript grammar.
func ExtractTS(path string) *types.ExtractionResult {
	return extractJSWith(path, grammars.TypescriptLanguage())
}

func extractJSWith(path string, lang *ts.Language) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}

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
	type bodyEntry struct {
		callerNID string
		node      *ts.Node
	}
	var functionBodies []bodyEntry

	addNode := func(nid, label string, line int, declNode *ts.Node) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{
				ID: nid, Label: label, FileType: string(types.Code),
				SourceFile: strPath, SourceLocation: SourceLoc(line),
			})
			// Extract preceding comment (including JSDoc).
			if declNode != nil {
				if prev := declNode.PrevSibling(); prev != nil && prev.Type(lang) == "comment" {
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

	rdText := func(n *ts.Node) string {
		return ReadText(source, n.StartByte(), n.EndByte())
	}

	var walk func(node *ts.Node, parentClassNID string)
	walk = func(node *ts.Node, parentClassNID string) {
		t := node.Type(lang)

		// Imports
		if t == "import_statement" {
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type(lang) == "string" {
					raw := strings.Trim(rdText(child), "'\"` ")
					if raw == "" {
						break
					}
					var tgtNID string
					if strings.HasPrefix(raw, ".") {
						resolved := filepath.Clean(filepath.Join(filepath.Dir(strPath), raw))
						ext := filepath.Ext(resolved)
						if ext == ".js" {
							resolved = resolved[:len(resolved)-3] + ".ts"
						} else if ext == ".jsx" {
							resolved = resolved[:len(resolved)-4] + ".tsx"
						}
						tgtNID = MakeID(resolved)
					} else {
						parts := strings.Split(raw, "/")
						moduleName := parts[len(parts)-1]
						if moduleName == "" {
							break
						}
						tgtNID = MakeID(moduleName)
					}
					addEdge(fileNID, tgtNID, "imports_from", int(node.StartPoint().Row)+1)
					break
				}
			}
			return
		}

		// Classes
		if t == "class_declaration" {
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode == nil {
				return
			}
			className := rdText(nameNode)
			classNID := MakeID(stem, className)
			line := int(node.StartPoint().Row) + 1
			addNode(classNID, className, line, node)
			addEdge(fileNID, classNID, "contains", line)

			body := node.ChildByFieldName("body", lang)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), classNID)
				}
			}
			return
		}

		// Functions and methods
		if t == "function_declaration" || t == "method_definition" {
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode == nil {
				return
			}
			funcName := rdText(nameNode)
			line := int(node.StartPoint().Row) + 1

			var funcNID string
			if parentClassNID != "" {
				funcNID = MakeID(parentClassNID, funcName)
				addNode(funcNID, "."+funcName+"()", line, node)
				addEdge(parentClassNID, funcNID, "method", line)
			} else {
				funcNID = MakeID(stem, funcName)
				addNode(funcNID, funcName+"()", line, node)
				addEdge(fileNID, funcNID, "contains", line)
			}
			if strings.HasPrefix(funcName, "test") || strings.HasPrefix(funcName, "Test") {
				TagNode(nodes, funcNID, "test")
			}

			body := node.ChildByFieldName("body", lang)
			if body != nil {
				functionBodies = append(functionBodies, bodyEntry{funcNID, body})
			}
			return
		}

		// Arrow functions in lexical declarations: const foo = () => { ... }
		if t == "lexical_declaration" {
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type(lang) == "variable_declarator" {
					value := child.ChildByFieldName("value", lang)
					if value != nil && value.Type(lang) == "arrow_function" {
						nameNode := child.ChildByFieldName("name", lang)
						if nameNode != nil {
							funcName := rdText(nameNode)
							line := int(child.StartPoint().Row) + 1
							funcNID := MakeID(stem, funcName)
							addNode(funcNID, funcName+"()", line, node)
							addEdge(fileNID, funcNID, "contains", line)
							body := value.ChildByFieldName("body", lang)
							if body != nil {
								functionBodies = append(functionBodies, bodyEntry{funcNID, body})
							}
						}
					}
				}
			}
			return
		}

		// Default: recurse.
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
		nt := node.Type(lang)
		if nt == "function_declaration" || nt == "arrow_function" || nt == "method_definition" {
			return
		}

		// Behavioral tagging.
		if nt == "throw_statement" {
			TagNode(nodes, callerNID, "throws")
			// Extract throw argument (skip "throw " keyword).
			throwText := strings.TrimSpace(strings.TrimPrefix(rdText(node), "throw"))
			throwText = strings.TrimSuffix(throwText, ";")
			AddThrowMessage(nodes, callerNID, throwText)
		}
		if nt == "catch_clause" {
			TagNode(nodes, callerNID, "catches")
		}
		if nt == "await_expression" {
			TagNode(nodes, callerNID, "async")
		}

		if nt == "call_expression" {
			funcNode := node.ChildByFieldName("function", lang)
			var calleeName string
			isMemberCall := false

			// Extract argument text for log messages.
			callArgText := ""
			if argsNode := node.ChildByFieldName("arguments", lang); argsNode != nil {
				callArgText = rdText(argsNode)
				if len(callArgText) >= 2 && callArgText[0] == '(' && callArgText[len(callArgText)-1] == ')' {
					callArgText = strings.TrimSpace(callArgText[1 : len(callArgText)-1])
				}
			}

			if funcNode != nil {
				switch funcNode.Type(lang) {
				case "identifier":
					calleeName = rdText(funcNode)
					// JS exec/spawn.
					if calleeName == "exec" || calleeName == "execSync" || calleeName == "spawn" || calleeName == "spawnSync" {
						TagNode(nodes, callerNID, "exec")
					}
					if calleeName == "fetch" || calleeName == "require" {
						if calleeName == "fetch" {
							TagNode(nodes, callerNID, "net")
						}
					}
				case "member_expression":
					isMemberCall = true
					prop := funcNode.ChildByFieldName("property", lang)
					if prop != nil {
						calleeName = rdText(prop)
					}
					obj := funcNode.ChildByFieldName("object", lang)
					if obj != nil {
						objName := rdText(obj)
						if objName == "console" {
							TagNode(nodes, callerNID, "logs")
							AddLogMessage(nodes, callerNID, "console."+calleeName+"("+callArgText+")")
						}
						if objName == "fs" || objName == "fsp" || objName == "fsPromises" {
							TagNode(nodes, callerNID, "fs")
						}
						if objName == "http" || objName == "https" || objName == "axios" {
							TagNode(nodes, callerNID, "net")
						}
						if objName == "child_process" || objName == "cp" {
							TagNode(nodes, callerNID, "exec")
						}
						if objName == "tracer" || objName == "meter" || objName == "metrics" ||
							objName == "trace" || objName == "dd" || objName == "ddTrace" ||
							objName == "statsd" || objName == "promClient" || objName == "prometheus" {
							TagNode(nodes, callerNID, "otel")
						}
					}
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

	return &types.ExtractionResult{
		Nodes:    nodes,
		Edges:    edges,
		RawCalls: rawCalls,
	}
}
