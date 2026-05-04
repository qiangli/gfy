package extract

import (
	"path/filepath"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/qiangli/gfy/pkg/types"
)

// ExtractGeneric is a config-driven AST extractor that handles most languages.
func ExtractGeneric(path string, config *LanguageConfig) *types.ExtractionResult {
	source, err := readFileBytes(path)
	if err != nil {
		return &types.ExtractionResult{Error: err.Error()}
	}

	lang := config.Lang
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

	ctx := &GenericContext{
		Lang: lang, Config: config, Source: source,
		Stem: stem, StrPath: strPath,
		FileNID: MakeID(path),
		Nodes:   &nodes, Edges: &edges,
		SeenIDs: seenIDs, FunctionBodies: &functionBodies,
	}

	ctx.AddNode(ctx.FileNID, filepath.Base(path), 1)

	// --- Structural walk ---
	var walk func(node *ts.Node, parentClassNID string)
	walk = func(node *ts.Node, parentClassNID string) {
		t := node.Type(lang)

		// Import types
		if config.ImportTypes[t] {
			if config.ImportHandler != nil {
				config.ImportHandler(node, lang, source, ctx.FileNID, stem, strPath, &edges)
			}
			return
		}

		// Class types
		if config.ClassTypes[t] {
			nameNode := resolveNameNode(node, lang, config)
			if nameNode == nil {
				return
			}
			className := ctx.RT(nameNode)
			classNID := MakeID(stem, className)
			line := int(node.StartPoint().Row) + 1
			ctx.AddNodeWithDecl(classNID, className, line, node)
			ctx.AddEdge(ctx.FileNID, classNID, "contains", line)

			// Language-specific inheritance
			if config.InheritanceFn != nil {
				config.InheritanceFn(ctx, node, classNID, line)
			}

			// Find body and recurse
			body := findBody(node, lang, config)
			if body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), classNID)
				}
			}
			return
		}

		// Function types
		if config.FunctionTypes[t] {
			var funcName string

			// Special cases
			if t == "deinit_declaration" {
				funcName = "deinit"
			} else if t == "subscript_declaration" {
				funcName = "subscript"
			} else if config.ResolveFuncNameFn != nil {
				// C/C++ declarator unwrapping
				declarator := node.ChildByFieldName("declarator", lang)
				if declarator != nil {
					funcName = config.ResolveFuncNameFn(declarator, lang, source)
				}
			} else {
				nameNode := resolveNameNode(node, lang, config)
				if nameNode != nil {
					funcName = ctx.RT(nameNode)
				}
			}

			if funcName == "" {
				return
			}

			line := int(node.StartPoint().Row) + 1
			var funcNID string
			if parentClassNID != "" {
				funcNID = MakeID(parentClassNID, funcName)
				label := "." + funcName
				if config.FunctionLabelParens {
					label += "()"
				}
				ctx.AddNodeWithDecl(funcNID, label, line, node)
				ctx.AddEdge(parentClassNID, funcNID, "method", line)
			} else {
				funcNID = MakeID(stem, funcName)
				label := funcName
				if config.FunctionLabelParens {
					label += "()"
				}
				ctx.AddNodeWithDecl(funcNID, label, line, node)
				ctx.AddEdge(ctx.FileNID, funcNID, "contains", line)
			}

			body := findBody(node, lang, config)
			if body != nil {
				functionBodies = append(functionBodies, bodyEntry{funcNID, body})
			}
			return
		}

		// Extra walk hook
		if config.ExtraWalkFn != nil {
			if config.ExtraWalkFn(ctx, node, parentClassNID) {
				return
			}
		}

		// Default: recurse
		for i := 0; i < int(node.ChildCount()); i++ {
			walk(node.Child(i), "")
		}
	}

	walk(root, "")

	// --- Call graph pass ---
	labelToNID := buildLabelIndex(nodes)
	seenCallPairs := make(map[[2]string]bool)
	var rawCalls []types.RawCall

	var walkCalls func(node *ts.Node, callerNID string)
	walkCalls = func(node *ts.Node, callerNID string) {
		t := node.Type(lang)
		if config.FunctionBoundaryTypes[t] {
			return
		}

		// Behavioral tagging from BehaviorConfig.
		if bc := config.Behavior; bc != nil {
			for _, nt := range bc.ThrowNodeTypes {
				if t == nt {
					TagNode(nodes, callerNID, "throws")
					nodeText := ReadText(source, node.StartByte(), node.EndByte())
					AddThrowMessage(nodes, callerNID, nodeText)
					break
				}
			}
			for _, nt := range bc.CatchNodeTypes {
				if t == nt {
					TagNode(nodes, callerNID, "catches")
					break
				}
			}
			for _, nt := range bc.AsyncNodeTypes {
				if t == nt {
					TagNode(nodes, callerNID, "async")
					break
				}
			}
			for _, nt := range bc.UnsafeNodeTypes {
				if t == nt {
					TagNode(nodes, callerNID, "unsafe")
					break
				}
			}
		}

		if config.CallTypes[t] {
			var calleeName string
			isMemberCall := false

			// Try language-specific call resolution first
			if config.CallResolveFn != nil {
				name, member, handled := config.CallResolveFn(node, lang, source)
				if handled {
					calleeName = name
					isMemberCall = member
				}
			}

			// Generic call resolution
			if calleeName == "" && config.CallFunctionField != "" {
				funcNode := node.ChildByFieldName(config.CallFunctionField, lang)
				if funcNode != nil {
					ft := funcNode.Type(lang)
					if ft == "identifier" {
						calleeName = ReadText(source, funcNode.StartByte(), funcNode.EndByte())
					} else if config.CallAccessorNodeTypes[ft] {
						isMemberCall = true
						if config.CallAccessorField != "" {
							attr := funcNode.ChildByFieldName(config.CallAccessorField, lang)
							if attr != nil {
								calleeName = ReadText(source, attr.StartByte(), attr.EndByte())
							}
						}
					} else {
						// Read the node directly (e.g., Java method name)
						calleeName = ReadText(source, funcNode.StartByte(), funcNode.EndByte())
					}
				}
			}

			// Extract argument text for log/throw messages.
			callArgText := ""
			if config.CallArgumentsField != "" {
				if argsNode := node.ChildByFieldName(config.CallArgumentsField, lang); argsNode != nil {
					callArgText = ReadText(source, argsNode.StartByte(), argsNode.EndByte())
					if len(callArgText) >= 2 && callArgText[0] == '(' && callArgText[len(callArgText)-1] == ')' {
						callArgText = strings.TrimSpace(callArgText[1 : len(callArgText)-1])
					}
				}
			}

			// Behavioral tagging for call expressions.
			if bc := config.Behavior; bc != nil && calleeName != "" {
				for _, name := range bc.ThrowCallNames {
					if calleeName == name && !isMemberCall {
						TagNode(nodes, callerNID, "throws")
						AddThrowMessage(nodes, callerNID, calleeName+"("+callArgText+")")
						break
					}
				}
				for _, name := range bc.LogCallNames {
					if calleeName == name && !isMemberCall {
						TagNode(nodes, callerNID, "logs")
						AddLogMessage(nodes, callerNID, calleeName+"("+callArgText+")")
						break
					}
				}
				for _, name := range bc.ExecCallNames {
					if calleeName == name && !isMemberCall {
						TagNode(nodes, callerNID, "exec")
						break
					}
				}
				for _, name := range bc.FSCallNames {
					if calleeName == name && !isMemberCall {
						TagNode(nodes, callerNID, "fs")
						break
					}
				}
				for _, name := range bc.OtelCallNames {
					if calleeName == name && !isMemberCall {
						TagNode(nodes, callerNID, "otel")
						break
					}
				}
				if isMemberCall {
					funcNode := node.ChildByFieldName(config.CallFunctionField, lang)
					if funcNode != nil {
						raw := ReadText(source, funcNode.StartByte(), funcNode.EndByte())
						matchObjectPrefix(raw, bc.LogObjects, func() {
							TagNode(nodes, callerNID, "logs")
							AddLogMessage(nodes, callerNID, raw+"("+callArgText+")")
						})
						matchObjectPrefix(raw, bc.FSObjects, func() { TagNode(nodes, callerNID, "fs") })
						matchObjectPrefix(raw, bc.NetObjects, func() { TagNode(nodes, callerNID, "net") })
						matchObjectPrefix(raw, bc.OtelObjects, func() { TagNode(nodes, callerNID, "otel") })
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

// matchObjectPrefix checks if raw text starts with any of the given prefixes.
func matchObjectPrefix(raw string, prefixes []string, onMatch func()) {
	for _, obj := range prefixes {
		if len(raw) >= len(obj) && raw[:len(obj)] == obj {
			onMatch()
			return
		}
	}
}

// resolveNameNode extracts the name node from a class/function declaration.
func resolveNameNode(node *ts.Node, lang *ts.Language, config *LanguageConfig) *ts.Node {
	if config.NameField != "" {
		n := node.ChildByFieldName(config.NameField, lang)
		if n != nil {
			return n
		}
	}
	for _, childType := range config.NameFallbackChildTypes {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type(lang) == childType {
				return child
			}
		}
	}
	return nil
}

// findBody finds the body node of a class/function.
func findBody(node *ts.Node, lang *ts.Language, config *LanguageConfig) *ts.Node {
	if config.BodyField != "" {
		b := node.ChildByFieldName(config.BodyField, lang)
		if b != nil {
			return b
		}
	}
	for _, childType := range config.BodyFallbackChildTypes {
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child.Type(lang) == childType {
				return child
			}
		}
	}
	return nil
}
