package extract

import (
	"os"
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/pkg/types"
)

// Go packages whose calls indicate logging behavior.
var goLogPackages = map[string]bool{
	"log": true, "slog": true, "fmt": true,
	"zap": true, "logrus": true, "zerolog": true,
}

// Go packages/functions for behavioral tagging.
var goExecPackages = map[string]bool{"exec": true, "syscall": true}
var goFSPackages = map[string]bool{"os": true, "ioutil": true, "bufio": true}
var goFSFuncs = map[string]bool{
	"ReadFile": true, "WriteFile": true, "Create": true, "Open": true,
	"OpenFile": true, "Remove": true, "RemoveAll": true, "Mkdir": true,
	"MkdirAll": true, "Rename": true, "Stat": true, "ReadDir": true,
}
var goNetPackages = map[string]bool{"http": true, "net": true, "rpc": true, "grpc": true}
var goOtelPackages = map[string]bool{
	"otel": true, "metric": true,
	"prometheus": true, "promauto": true,
	"statsd": true,
}

// ExtractGo extracts functions, methods, type declarations, and imports from a .go file.
func ExtractGo(path string) *types.ExtractionResult {
	source, err := os.ReadFile(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}

	lang := grammars.GoLanguage()
	tree, err := getParserPool(lang).Parse(source)
	if err != nil {
		return &types.ExtractionResult{Error: "gotreesitter: parse: " + err.Error()}
	}
	root := tree.RootNode()

	stem := FileStem(path)
	pkgScope := filepath.Base(filepath.Dir(path))
	if pkgScope == "" || pkgScope == "." {
		pkgScope = stem
	}
	strPath := path

	var nodes []types.Node
	var edges []types.Edge
	seenIDs := make(map[string]bool)
	type bodyEntry struct {
		callerNID string
		node      *ts.Node
	}
	var functionBodies []bodyEntry
	importedPkgs := make(map[string]bool)

	addNode := func(nid, label string, line int, declNode *ts.Node) {
		if !seenIDs[nid] {
			seenIDs[nid] = true
			nodes = append(nodes, types.Node{
				ID: nid, Label: label, FileType: string(types.Code),
				SourceFile: strPath, SourceLocation: SourceLoc(line),
			})
			// Extract preceding comment.
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

	rt := func(n *ts.Node) string {
		return ReadText(source, n.StartByte(), n.EndByte())
	}

	var walk func(node *ts.Node)
	walk = func(node *ts.Node) {
		t := node.Type(lang)

		switch t {
		case "function_declaration":
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode != nil {
				funcName := rt(nameNode)
				line := int(node.StartPoint().Row) + 1
				funcNID := MakeID(stem, funcName)
				addNode(funcNID, funcName+"()", line, node)
				addEdge(fileNID, funcNID, "contains", line)
				if strings.HasPrefix(funcName, "Test") || strings.HasPrefix(funcName, "Benchmark") {
					TagNode(nodes, funcNID, "test")
				}
				body := node.ChildByFieldName("body", lang)
				if body != nil {
					functionBodies = append(functionBodies, bodyEntry{funcNID, body})
				}
			}
			return

		case "method_declaration":
			receiver := node.ChildByFieldName("receiver", lang)
			var receiverType string
			if receiver != nil {
				for i := 0; i < int(receiver.ChildCount()); i++ {
					param := receiver.Child(i)
					if param.Type(lang) == "parameter_declaration" {
						typeNode := param.ChildByFieldName("type", lang)
						if typeNode != nil {
							raw := strings.TrimLeft(rt(typeNode), "*")
							receiverType = strings.TrimSpace(raw)
						}
						break
					}
				}
			}
			nameNode := node.ChildByFieldName("name", lang)
			if nameNode != nil {
				methodName := rt(nameNode)
				line := int(node.StartPoint().Row) + 1
				if receiverType != "" {
					parentNID := MakeID(pkgScope, receiverType)
					addNode(parentNID, receiverType, line, nil)
					methodNID := MakeID(parentNID, methodName)
					addNode(methodNID, "."+methodName+"()", line, node)
					addEdge(parentNID, methodNID, "method", line)
				} else {
					methodNID := MakeID(stem, methodName)
					addNode(methodNID, methodName+"()", line, node)
					addEdge(fileNID, methodNID, "contains", line)
				}
				body := node.ChildByFieldName("body", lang)
				if body != nil {
					var callerNID string
					if receiverType != "" {
						callerNID = MakeID(MakeID(pkgScope, receiverType), methodName)
					} else {
						callerNID = MakeID(stem, methodName)
					}
					functionBodies = append(functionBodies, bodyEntry{callerNID, body})
				}
			}
			return

		case "type_declaration":
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				if child.Type(lang) == "type_spec" {
					nameNode := child.ChildByFieldName("name", lang)
					if nameNode != nil {
						typeName := rt(nameNode)
						line := int(child.StartPoint().Row) + 1
						typeNID := MakeID(pkgScope, typeName)
						addNode(typeNID, typeName, line, child)
						addEdge(fileNID, typeNID, "contains", line)
					}
				}
			}
			return

		case "import_declaration":
			for i := 0; i < int(node.ChildCount()); i++ {
				child := node.Child(i)
				ct := child.Type(lang)
				if ct == "import_spec_list" {
					for j := 0; j < int(child.ChildCount()); j++ {
						spec := child.Child(j)
						if spec.Type(lang) == "import_spec" {
							processGoImportSpec(spec, lang, source, fileNID, importedPkgs, &edges, strPath)
						}
					}
				} else if ct == "import_spec" {
					processGoImportSpec(child, lang, source, fileNID, importedPkgs, &edges, strPath)
				}
			}
			return
		}

		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i))
		}
	}

	walk(root)

	labelToNID := buildLabelIndex(nodes)
	seenCallPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall

	var walkCalls func(node *ts.Node, callerNID string)
	walkCalls = func(node *ts.Node, callerNID string) {
		t := node.Type(lang)
		if t == "function_declaration" || t == "method_declaration" {
			return
		}

		// Go statement = async.
		if t == "go_statement" {
			TagNode(nodes, callerNID, "async")
		}

		if t == "call_expression" {
			funcNode := node.ChildByFieldName("function", lang)
			var calleeName string
			isMemberCall := false
			var operandName string

			if funcNode != nil {
				switch funcNode.Type(lang) {
				case "identifier":
					calleeName = rt(funcNode)
				case "selector_expression":
					field := funcNode.ChildByFieldName("field", lang)
					operand := funcNode.ChildByFieldName("operand", lang)
					if operand != nil {
						operandName = rt(operand)
					}
					isMemberCall = !importedPkgs[operandName]
					if field != nil {
						calleeName = rt(field)
					}
				}
			}

			// Extract argument text for log/throw messages.
			argText := ""
			if argsNode := node.ChildByFieldName("arguments", lang); argsNode != nil {
				argText = rt(argsNode)
				// Strip outer parentheses.
				if len(argText) >= 2 && argText[0] == '(' && argText[len(argText)-1] == ')' {
					argText = strings.TrimSpace(argText[1 : len(argText)-1])
				}
			}

			// Behavioral tagging.
			if calleeName == "panic" && !isMemberCall && operandName == "" {
				TagNode(nodes, callerNID, "throws")
				AddThrowMessage(nodes, callerNID, argText)
			}
			if calleeName == "recover" && !isMemberCall && operandName == "" {
				TagNode(nodes, callerNID, "catches")
			}
			if operandName == "unsafe" {
				TagNode(nodes, callerNID, "unsafe")
			}
			if operandName != "" && importedPkgs[operandName] {
				if goLogPackages[operandName] {
					TagNode(nodes, callerNID, "logs")
					AddLogMessage(nodes, callerNID, operandName+"."+calleeName+"("+argText+")")
				}
				if goExecPackages[operandName] {
					TagNode(nodes, callerNID, "exec")
				}
				if goFSPackages[operandName] && goFSFuncs[calleeName] {
					TagNode(nodes, callerNID, "fs")
				}
				if goNetPackages[operandName] {
					TagNode(nodes, callerNID, "net")
				}
				if goOtelPackages[operandName] {
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

	return &types.ExtractionResult{
		Nodes:    nodes,
		Edges:    edges,
		RawCalls: rawCalls,
	}
}

// processGoImportSpec handles a single Go import_spec node.
func processGoImportSpec(spec *ts.Node, lang *ts.Language, source []byte, fileNID string, importedPkgs map[string]bool, edges *[]types.Edge, strPath string) {
	pathNode := spec.ChildByFieldName("path", lang)
	if pathNode == nil {
		return
	}
	raw := strings.Trim(rt(source, pathNode), "\"")
	tgtNID := MakeID("go", "pkg", raw)
	line := int(spec.StartPoint().Row) + 1
	*edges = append(*edges, types.Edge{
		Source: fileNID, Target: tgtNID, Relation: "imports_from",
		Confidence: types.Extracted, SourceFile: strPath,
		SourceLocation: SourceLoc(line), Weight: 1.0,
	})

	alias := spec.ChildByFieldName("name", lang)
	var localName string
	if alias != nil {
		localName = rt(source, alias)
	} else {
		parts := strings.Split(raw, "/")
		localName = parts[len(parts)-1]
	}
	if localName != "" && localName != "_" && localName != "." {
		importedPkgs[localName] = true
	}
}

// rt is a standalone readText helper for use in functions that don't have a closure.
func rt(source []byte, n *ts.Node) string {
	return ReadText(source, n.StartByte(), n.EndByte())
}
