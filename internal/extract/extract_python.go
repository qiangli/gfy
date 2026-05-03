package extract

import (
	"os"
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/internal/types"
)

// Python objects whose method calls indicate logging behavior.
var pyLogObjects = map[string]bool{
	"logging": true, "logger": true, "log": true,
	"self.logger": true, "self.log": true,
}

var pyExecFuncs = map[string]bool{
	"subprocess": true, "os.system": true, "os.popen": true,
	"Popen": true, "check_output": true, "check_call": true, "run": true,
}
var pyFSFuncs = map[string]bool{
	"open": true, "Path": true,
}
var pyFSObjects = map[string]bool{
	"os": true, "os.path": true, "shutil": true, "pathlib": true, "glob": true,
}
var pyNetObjects = map[string]bool{
	"requests": true, "urllib": true, "httpx": true, "aiohttp": true,
	"socket": true, "http": true, "urllib3": true,
}

// ExtractPython extracts classes, functions, imports, and call graphs from a .py file.
func ExtractPython(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}

	lang := grammars.PythonLanguage()
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
			// Extract preceding # comment.
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
				ct := child.Type(lang)
				if ct == "dotted_name" || ct == "aliased_import" {
					raw := rdText(child)
					parts := strings.SplitN(raw, " as ", 2)
					moduleName := strings.TrimLeft(strings.TrimSpace(parts[0]), ".")
					tgtNID := MakeID(moduleName)
					addEdge(fileNID, tgtNID, "imports", int(node.StartPoint().Row)+1)
				}
			}
			return
		}

		if t == "import_from_statement" {
			moduleNode := node.ChildByFieldName("module_name", lang)
			if moduleNode != nil {
				raw := rdText(moduleNode)
				var tgtNID string
				if strings.HasPrefix(raw, ".") {
					dots := len(raw) - len(strings.TrimLeft(raw, "."))
					moduleName := strings.TrimLeft(raw, ".")
					base := filepath.Dir(strPath)
					for i := 1; i < dots; i++ {
						base = filepath.Dir(base)
					}
					var rel string
					if moduleName != "" {
						rel = strings.ReplaceAll(moduleName, ".", "/") + ".py"
					} else {
						rel = "__init__.py"
					}
					tgtNID = MakeID(filepath.Join(base, rel))
				} else {
					tgtNID = MakeID(raw)
				}
				addEdge(fileNID, tgtNID, "imports_from", int(node.StartPoint().Row)+1)
			}
			return
		}

		// Classes
		if t == "class_definition" {
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode == nil {
				return
			}
			className := rdText(nameNode)
			classNID := MakeID(stem, className)
			line := int(node.StartPoint().Row) + 1
			addNode(classNID, className, line, node)
			addEdge(fileNID, classNID, "contains", line)

			// Inheritance
			superclasses := node.ChildByFieldName("superclasses", lang)
			if superclasses != nil {
				for i := 0; i < int(superclasses.ChildCount()); i++ {
					arg := superclasses.Child(i)
					if arg.Type(lang) == "identifier" {
						baseName := rdText(arg)
						baseNID := MakeID(stem, baseName)
						if !seenIDs[baseNID] {
							baseNID = MakeID(baseName)
							if !seenIDs[baseNID] {
								nodes = append(nodes, types.Node{
									ID: baseNID, Label: baseName,
									FileType: string(types.Code), SourceFile: "", SourceLocation: "",
								})
								seenIDs[baseNID] = true
							}
						}
						addEdge(classNID, baseNID, "inherits", line)
					}
				}
			}

			body := node.ChildByFieldName("body", lang)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), classNID)
				}
			}
			return
		}

		// Functions/methods
		if t == "function_definition" {
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
			if strings.HasPrefix(funcName, "test_") || strings.HasPrefix(funcName, "Test") {
				TagNode(nodes, funcNID, "test")
			}

			// Extract docstring: first statement in body that is expression_statement > string.
			body := node.ChildByFieldName("body", lang)
			if body != nil && body.ChildCount() > 0 {
				first := body.Child(0)
				if first.Type(lang) == "expression_statement" && first.ChildCount() > 0 {
					strNode := first.Child(0)
					if strNode.Type(lang) == "string" {
						SetComment(nodes, funcNID, rdText(strNode))
					}
				}
				functionBodies = append(functionBodies, bodyEntry{funcNID, body})
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
		if nt == "function_definition" {
			return
		}

		// Behavioral tagging.
		if nt == "raise_statement" {
			TagNode(nodes, callerNID, "throws")
			// Extract raise argument text (e.g. "ValueError('msg')").
			raiseText := strings.TrimSpace(strings.TrimPrefix(rdText(node), "raise"))
			AddThrowMessage(nodes, callerNID, raiseText)
		}
		if nt == "except_clause" {
			TagNode(nodes, callerNID, "catches")
		}
		if nt == "await" {
			TagNode(nodes, callerNID, "async")
		}

		if nt == "call" {
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
					if calleeName == "print" {
						TagNode(nodes, callerNID, "logs")
						AddLogMessage(nodes, callerNID, "print("+callArgText+")")
					}
					if pyFSFuncs[calleeName] {
						TagNode(nodes, callerNID, "fs")
					}
					if pyExecFuncs[calleeName] {
						TagNode(nodes, callerNID, "exec")
					}
				case "attribute":
					isMemberCall = true
					attr := funcNode.ChildByFieldName("attribute", lang)
					obj := funcNode.ChildByFieldName("object", lang)
					if attr != nil {
						calleeName = rdText(attr)
					}
					if obj != nil {
						objName := rdText(obj)
						if pyLogObjects[objName] {
							TagNode(nodes, callerNID, "logs")
							AddLogMessage(nodes, callerNID, objName+"."+calleeName+"("+callArgText+")")
						}
						if pyFSObjects[objName] {
							TagNode(nodes, callerNID, "fs")
						}
						if pyNetObjects[objName] {
							TagNode(nodes, callerNID, "net")
						}
						if objName == "subprocess" {
							TagNode(nodes, callerNID, "exec")
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
