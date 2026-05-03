package extract

import (
	"regexp"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/qiangli/gfy/internal/types"
)

// --- Import handlers ---

func importJava(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		ct := child.Type(lang)
		if ct == "scoped_identifier" || ct == "identifier" {
			pathStr := walkScopedJava(child, lang, source)
			parts := strings.Split(pathStr, ".")
			moduleName := parts[len(parts)-1]
			moduleName = strings.Trim(moduleName, "*. ")
			if moduleName == "" && len(parts) > 1 {
				moduleName = parts[len(parts)-2]
			}
			if moduleName != "" {
				*edges = append(*edges, types.Edge{
					Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
					Confidence: types.Extracted, SourceFile: strPath,
					SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
				})
			}
			break
		}
	}
}

func walkScopedJava(node *ts.Node, lang *ts.Language, source []byte) string {
	var parts []string
	cur := node
	for cur != nil {
		if cur.Type(lang) == "scoped_identifier" {
			nameNode := cur.ChildByFieldName("name", lang)
			if nameNode != nil {
				parts = append(parts, ReadText(source, nameNode.StartByte(), nameNode.EndByte()))
			}
			cur = cur.ChildByFieldName("scope", lang)
		} else if cur.Type(lang) == "identifier" {
			parts = append(parts, ReadText(source, cur.StartByte(), cur.EndByte()))
			break
		} else {
			break
		}
	}
	// Reverse
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, ".")
}

func importC(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		ct := child.Type(lang)
		if ct == "string_literal" || ct == "system_lib_string" || ct == "string" {
			raw := strings.Trim(ReadText(source, child.StartByte(), child.EndByte()), "\"<> ")
			parts := strings.Split(raw, "/")
			moduleName := strings.Split(parts[len(parts)-1], ".")[0]
			if moduleName != "" {
				*edges = append(*edges, types.Edge{
					Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
					Confidence: types.Extracted, SourceFile: strPath,
					SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
				})
			}
			break
		}
	}
}

func importCSharp(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		ct := child.Type(lang)
		if ct == "qualified_name" || ct == "identifier" || ct == "name_equals" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			parts := strings.Split(raw, ".")
			moduleName := strings.TrimSpace(parts[len(parts)-1])
			if moduleName != "" {
				*edges = append(*edges, types.Edge{
					Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
					Confidence: types.Extracted, SourceFile: strPath,
					SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
				})
			}
			break
		}
	}
}

func importKotlin(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	pathNode := node.ChildByFieldName("path", lang)
	if pathNode != nil {
		raw := ReadText(source, pathNode.StartByte(), pathNode.EndByte())
		parts := strings.Split(raw, ".")
		moduleName := strings.TrimSpace(parts[len(parts)-1])
		if moduleName != "" {
			*edges = append(*edges, types.Edge{
				Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
			})
		}
		return
	}
	// Fallback: find identifier child
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			*edges = append(*edges, types.Edge{
				Source: fileNID, Target: MakeID(raw), Relation: "imports",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
			})
			break
		}
	}
}

func importScala(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		ct := child.Type(lang)
		if ct == "stable_id" || ct == "identifier" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			parts := strings.Split(raw, ".")
			moduleName := strings.Trim(parts[len(parts)-1], "{} ")
			if moduleName != "" && moduleName != "_" {
				*edges = append(*edges, types.Edge{
					Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
					Confidence: types.Extracted, SourceFile: strPath,
					SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
				})
			}
			break
		}
	}
}

func importPHP(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		ct := child.Type(lang)
		if ct == "qualified_name" || ct == "name" || ct == "identifier" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			parts := strings.Split(raw, "\\")
			moduleName := strings.TrimSpace(parts[len(parts)-1])
			if moduleName != "" {
				*edges = append(*edges, types.Edge{
					Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
					Confidence: types.Extracted, SourceFile: strPath,
					SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
				})
			}
			break
		}
	}
}

var luaRequireRe = regexp.MustCompile(`require\s*[(\'"]\s*[\'"]?([^\'")\s]+)`)

func importLua(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	text := ReadText(source, node.StartByte(), node.EndByte())
	m := luaRequireRe.FindStringSubmatch(text)
	if m != nil {
		parts := strings.Split(m[1], ".")
		moduleName := parts[len(parts)-1]
		if moduleName != "" {
			*edges = append(*edges, types.Edge{
				Source: fileNID, Target: MakeID(moduleName), Relation: "imports",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
			})
		}
	}
}

func importSwift(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge) {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			*edges = append(*edges, types.Edge{
				Source: fileNID, Target: MakeID(raw), Relation: "imports",
				Confidence: types.Extracted, SourceFile: strPath,
				SourceLocation: SourceLoc(int(node.StartPoint().Row) + 1), Weight: 1.0,
			})
			break
		}
	}
}

// --- C/C++ function name unwrapping ---

func getCFuncName(node *ts.Node, lang *ts.Language, source []byte) string {
	if node.Type(lang) == "identifier" {
		return ReadText(source, node.StartByte(), node.EndByte())
	}
	decl := node.ChildByFieldName("declarator", lang)
	if decl != nil {
		return getCFuncName(decl, lang, source)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			return ReadText(source, child.StartByte(), child.EndByte())
		}
	}
	return ""
}

func getCppFuncName(node *ts.Node, lang *ts.Language, source []byte) string {
	if node.Type(lang) == "identifier" {
		return ReadText(source, node.StartByte(), node.EndByte())
	}
	if node.Type(lang) == "qualified_identifier" {
		nameNode := node.ChildByFieldName("name", lang)
		if nameNode != nil {
			return ReadText(source, nameNode.StartByte(), nameNode.EndByte())
		}
	}
	decl := node.ChildByFieldName("declarator", lang)
	if decl != nil {
		return getCppFuncName(decl, lang, source)
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(lang) == "identifier" {
			return ReadText(source, child.StartByte(), child.EndByte())
		}
	}
	return ""
}

// --- Inheritance handlers ---

func javaInheritance(ctx *GenericContext, classNode *ts.Node, classNID string, line int) {
	emitParent := func(baseName, rel string) {
		if baseName == "" {
			return
		}
		baseNID := MakeID(ctx.Stem, baseName)
		if !ctx.SeenIDs[baseNID] {
			baseNID = MakeID(baseName)
			ctx.AddExternalNode(baseNID, baseName)
		}
		ctx.AddEdge(classNID, baseNID, rel, line)
	}

	sup := classNode.ChildByFieldName("superclass", ctx.Lang)
	if sup != nil {
		for i := 0; i < int(sup.ChildCount()); i++ {
			sub := sup.Child(i)
			if sub.Type(ctx.Lang) == "type_identifier" {
				emitParent(ctx.RT(sub), "extends")
				break
			}
		}
	}

	ifs := classNode.ChildByFieldName("interfaces", ctx.Lang)
	if ifs != nil {
		for i := 0; i < int(ifs.ChildCount()); i++ {
			sub := ifs.Child(i)
			if sub.Type(ctx.Lang) == "type_list" {
				for j := 0; j < int(sub.ChildCount()); j++ {
					tid := sub.Child(j)
					if tid.Type(ctx.Lang) == "type_identifier" {
						emitParent(ctx.RT(tid), "implements")
					}
				}
			}
		}
	}

	// Interface extends
	t := classNode.Type(ctx.Lang)
	if t == "interface_declaration" {
		for i := 0; i < int(classNode.ChildCount()); i++ {
			child := classNode.Child(i)
			if child.Type(ctx.Lang) == "extends_interfaces" {
				for j := 0; j < int(child.ChildCount()); j++ {
					sub := child.Child(j)
					if sub.Type(ctx.Lang) == "type_list" {
						for k := 0; k < int(sub.ChildCount()); k++ {
							tid := sub.Child(k)
							if tid.Type(ctx.Lang) == "type_identifier" {
								emitParent(ctx.RT(tid), "extends")
							}
						}
					}
				}
			}
		}
	}
}

func csharpInheritance(ctx *GenericContext, classNode *ts.Node, classNID string, line int) {
	for i := 0; i < int(classNode.ChildCount()); i++ {
		child := classNode.Child(i)
		if child.Type(ctx.Lang) == "base_list" {
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				st := sub.Type(ctx.Lang)
				var baseName string
				if st == "generic_name" {
					nameChild := sub.ChildByFieldName("name", ctx.Lang)
					if nameChild != nil {
						baseName = ctx.RT(nameChild)
					}
				} else if st == "identifier" {
					baseName = ctx.RT(sub)
				}
				if baseName != "" {
					baseNID := MakeID(ctx.Stem, baseName)
					if !ctx.SeenIDs[baseNID] {
						baseNID = MakeID(baseName)
						ctx.AddExternalNode(baseNID, baseName)
					}
					ctx.AddEdge(classNID, baseNID, "inherits", line)
				}
			}
		}
	}
}

func swiftInheritance(ctx *GenericContext, classNode *ts.Node, classNID string, line int) {
	for i := 0; i < int(classNode.ChildCount()); i++ {
		child := classNode.Child(i)
		if child.Type(ctx.Lang) == "inheritance_specifier" {
			for j := 0; j < int(child.ChildCount()); j++ {
				sub := child.Child(j)
				st := sub.Type(ctx.Lang)
				if st == "user_type" || st == "type_identifier" {
					baseName := ctx.RT(sub)
					baseNID := MakeID(ctx.Stem, baseName)
					if !ctx.SeenIDs[baseNID] {
						baseNID = MakeID(baseName)
						ctx.AddExternalNode(baseNID, baseName)
					}
					ctx.AddEdge(classNID, baseNID, "inherits", line)
				}
			}
		}
	}
}

// --- Call resolution handlers ---

func kotlinCallResolve(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool) {
	if node.ChildCount() == 0 {
		return "", false, false
	}
	first := node.Child(0)
	ft := first.Type(lang)
	if ft == "simple_identifier" {
		return ReadText(source, first.StartByte(), first.EndByte()), false, true
	}
	if ft == "navigation_expression" {
		for i := int(first.ChildCount()) - 1; i >= 0; i-- {
			child := first.Child(i)
			if child.Type(lang) == "simple_identifier" {
				return ReadText(source, child.StartByte(), child.EndByte()), true, true
			}
		}
	}
	return "", false, false
}

func scalaCallResolve(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool) {
	if node.ChildCount() == 0 {
		return "", false, false
	}
	first := node.Child(0)
	ft := first.Type(lang)
	if ft == "identifier" {
		return ReadText(source, first.StartByte(), first.EndByte()), false, true
	}
	if ft == "field_expression" {
		field := first.ChildByFieldName("field", lang)
		if field != nil {
			return ReadText(source, field.StartByte(), field.EndByte()), true, true
		}
		// Fallback: reversed child scan
		for i := int(first.ChildCount()) - 1; i >= 0; i-- {
			child := first.Child(i)
			if child.Type(lang) == "identifier" {
				return ReadText(source, child.StartByte(), child.EndByte()), true, true
			}
		}
	}
	return "", false, false
}

func swiftCallResolve(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool) {
	if node.ChildCount() == 0 {
		return "", false, false
	}
	first := node.Child(0)
	ft := first.Type(lang)
	if ft == "simple_identifier" {
		return ReadText(source, first.StartByte(), first.EndByte()), false, true
	}
	if ft == "navigation_expression" {
		for i := 0; i < int(first.ChildCount()); i++ {
			child := first.Child(i)
			if child.Type(lang) == "navigation_suffix" {
				for j := 0; j < int(child.ChildCount()); j++ {
					sc := child.Child(j)
					if sc.Type(lang) == "simple_identifier" {
						return ReadText(source, sc.StartByte(), sc.EndByte()), true, true
					}
				}
			}
		}
	}
	return "", false, false
}

func cppCallResolve(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool) {
	funcNode := node.ChildByFieldName("function", lang)
	if funcNode == nil {
		return "", false, false
	}
	ft := funcNode.Type(lang)
	if ft == "identifier" {
		return ReadText(source, funcNode.StartByte(), funcNode.EndByte()), false, true
	}
	if ft == "field_expression" || ft == "qualified_identifier" {
		nameNode := funcNode.ChildByFieldName("field", lang)
		if nameNode == nil {
			nameNode = funcNode.ChildByFieldName("name", lang)
		}
		if nameNode != nil {
			return ReadText(source, nameNode.StartByte(), nameNode.EndByte()), true, true
		}
	}
	return "", false, false
}

func csharpCallResolve(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool) {
	nameNode := node.ChildByFieldName("name", lang)
	if nameNode != nil {
		return ReadText(source, nameNode.StartByte(), nameNode.EndByte()), false, true
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type(lang) != "" {
			raw := ReadText(source, child.StartByte(), child.EndByte())
			if strings.Contains(raw, ".") {
				parts := strings.Split(raw, ".")
				return parts[len(parts)-1], true, true
			}
			return raw, false, true
		}
	}
	return "", false, false
}

// --- Extra walk handlers ---

func csharpExtraWalk(ctx *GenericContext, node *ts.Node, parentClassNID string) bool {
	if node.Type(ctx.Lang) == "namespace_declaration" {
		nameNode := node.ChildByFieldName("name", ctx.Lang)
		if nameNode != nil {
			nsName := ctx.RT(nameNode)
			nsNID := MakeID(ctx.Stem, nsName)
			line := int(node.StartPoint().Row) + 1
			ctx.AddNode(nsNID, nsName, line)
			ctx.AddEdge(ctx.FileNID, nsNID, "contains", line)
		}
		body := node.ChildByFieldName("body", ctx.Lang)
		if body != nil {
			// Return false to let the generic walker recurse into body children.
			return false
		}
		return true
	}
	return false
}

// --- Language configs ---

func JavaConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                  grammars.JavaLanguage(),
		ClassTypes:            map[string]bool{"class_declaration": true, "interface_declaration": true},
		FunctionTypes:         map[string]bool{"method_declaration": true, "constructor_declaration": true},
		ImportTypes:           map[string]bool{"import_declaration": true},
		CallTypes:             map[string]bool{"method_invocation": true},
		NameField:             "name",
		BodyField:             "body",
		CallFunctionField:     "name",
		CallArgumentsField:    "arguments",
		CallAccessorNodeTypes: map[string]bool{},
		FunctionBoundaryTypes: map[string]bool{"method_declaration": true, "constructor_declaration": true},
		FunctionLabelParens:   true,
		ImportHandler:         importJava,
		InheritanceFn:         javaInheritance,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_statement"},
			CatchNodeTypes: []string{"catch_clause"},
			LogObjects:     []string{"System.out.", "System.err.", "Logger.", "logger.", "log.", "Log."},
			FSObjects:      []string{"File.", "Files.", "FileReader.", "FileWriter.", "BufferedReader.", "BufferedWriter."},
			NetObjects:     []string{"HttpClient.", "URL.", "Socket.", "HttpURLConnection."},
			ExecCallNames:  []string{"exec"},
		},
	}
}

func CConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                  grammars.CLanguage(),
		ClassTypes:            map[string]bool{},
		FunctionTypes:         map[string]bool{"function_definition": true},
		ImportTypes:           map[string]bool{"preproc_include": true},
		CallTypes:             map[string]bool{"call_expression": true},
		NameField:             "name",
		BodyField:             "body",
		CallFunctionField:     "function",
		CallArgumentsField:    "arguments",
		CallAccessorNodeTypes: map[string]bool{"field_expression": true},
		CallAccessorField:     "field",
		FunctionBoundaryTypes: map[string]bool{"function_definition": true},
		FunctionLabelParens:   true,
		ImportHandler:         importC,
		ResolveFuncNameFn:     getCFuncName,
		Behavior: &BehaviorConfig{
			ThrowCallNames: []string{"exit", "abort", "assert"},
			LogCallNames:   []string{"printf", "fprintf", "syslog", "perror", "puts", "fputs"},
			ExecCallNames:  []string{"system", "popen", "execl", "execv", "execvp", "fork"},
			FSCallNames:    []string{"fopen", "fclose", "fread", "fwrite", "remove", "rename", "mkdir", "opendir"},
		},
	}
}

func CppConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                  grammars.CppLanguage(),
		ClassTypes:            map[string]bool{"class_specifier": true},
		FunctionTypes:         map[string]bool{"function_definition": true},
		ImportTypes:           map[string]bool{"preproc_include": true},
		CallTypes:             map[string]bool{"call_expression": true},
		NameField:             "name",
		BodyField:             "body",
		CallFunctionField:     "function",
		CallArgumentsField:    "arguments",
		CallAccessorNodeTypes: map[string]bool{"field_expression": true, "qualified_identifier": true},
		CallAccessorField:     "field",
		FunctionBoundaryTypes: map[string]bool{"function_definition": true},
		FunctionLabelParens:   true,
		ImportHandler:         importC,
		ResolveFuncNameFn:     getCppFuncName,
		CallResolveFn:         cppCallResolve,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_statement"},
			CatchNodeTypes: []string{"catch_clause"},
			LogObjects:     []string{"std::cout", "std::cerr", "std::clog"},
			ExecCallNames:  []string{"system", "popen", "execl", "execv"},
			FSCallNames:    []string{"fopen", "fclose", "fread", "fwrite", "remove", "rename"},
		},
	}
}

func RubyConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.RubyLanguage(),
		ClassTypes:             map[string]bool{"class": true},
		FunctionTypes:          map[string]bool{"method": true, "singleton_method": true},
		ImportTypes:            map[string]bool{},
		CallTypes:              map[string]bool{"call": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"constant", "scope_resolution", "identifier"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"body_statement"},
		CallFunctionField:      "method",
		CallArgumentsField:     "arguments",
		CallAccessorNodeTypes:  map[string]bool{},
		FunctionBoundaryTypes:  map[string]bool{"method": true, "singleton_method": true},
		FunctionLabelParens:    true,
		Behavior: &BehaviorConfig{
			CatchNodeTypes: []string{"rescue"},
			ThrowCallNames: []string{"raise", "fail"},
			LogCallNames:   []string{"puts", "p", "pp", "warn"},
			LogObjects:     []string{"logger.", "Logger.", "Rails.logger."},
		},
	}
}

func CSharpConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.CSharpLanguage(),
		ClassTypes:             map[string]bool{"class_declaration": true, "interface_declaration": true},
		FunctionTypes:          map[string]bool{"method_declaration": true},
		ImportTypes:            map[string]bool{"using_directive": true},
		CallTypes:              map[string]bool{"invocation_expression": true},
		NameField:              "name",
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"declaration_list"},
		CallFunctionField:      "function",
		CallArgumentsField:     "arguments",
		CallAccessorNodeTypes:  map[string]bool{"member_access_expression": true},
		CallAccessorField:      "name",
		FunctionBoundaryTypes:  map[string]bool{"method_declaration": true},
		FunctionLabelParens:    true,
		ImportHandler:          importCSharp,
		InheritanceFn:          csharpInheritance,
		ExtraWalkFn:            csharpExtraWalk,
		CallResolveFn:          csharpCallResolve,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes:  []string{"throw_statement", "throw_expression"},
			CatchNodeTypes:  []string{"catch_clause"},
			LogObjects:      []string{"Console.", "Debug.", "Trace.", "Logger."},
			ExecCallNames:   []string{"Start"},
			FSObjects:       []string{"File.", "Directory.", "Path.", "StreamReader.", "StreamWriter."},
			NetObjects:      []string{"HttpClient.", "WebClient.", "TcpClient.", "WebRequest."},
			AsyncNodeTypes:  []string{"await_expression"},
			UnsafeNodeTypes: []string{"unsafe_statement"},
		},
	}
}

func KotlinConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.KotlinLanguage(),
		ClassTypes:             map[string]bool{"class_declaration": true, "object_declaration": true},
		FunctionTypes:          map[string]bool{"function_declaration": true},
		ImportTypes:            map[string]bool{"import_header": true},
		CallTypes:              map[string]bool{"call_expression": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"simple_identifier"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"function_body", "class_body"},
		CallFunctionField:      "",
		CallAccessorNodeTypes:  map[string]bool{"navigation_expression": true},
		FunctionBoundaryTypes:  map[string]bool{"function_declaration": true},
		FunctionLabelParens:    true,
		ImportHandler:          importKotlin,
		CallResolveFn:          kotlinCallResolve,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_expression"},
			CatchNodeTypes: []string{"catch_block"},
			LogCallNames:   []string{"println", "print"},
			LogObjects:     []string{"Log.", "logger.", "Logger."},
			FSObjects:      []string{"File(", "Files."},
			NetObjects:     []string{"HttpClient.", "URL(", "OkHttpClient."},
		},
	}
}

func ScalaConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.ScalaLanguage(),
		ClassTypes:             map[string]bool{"class_definition": true, "object_definition": true},
		FunctionTypes:          map[string]bool{"function_definition": true},
		ImportTypes:            map[string]bool{"import_declaration": true},
		CallTypes:              map[string]bool{"call_expression": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"identifier"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"template_body"},
		CallFunctionField:      "",
		CallAccessorNodeTypes:  map[string]bool{"field_expression": true},
		CallAccessorField:      "field",
		FunctionBoundaryTypes:  map[string]bool{"function_definition": true},
		FunctionLabelParens:    true,
		ImportHandler:          importScala,
		CallResolveFn:          scalaCallResolve,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_expression"},
			CatchNodeTypes: []string{"catch_clause"},
			LogCallNames:   []string{"println", "print"},
			LogObjects:     []string{"logger.", "Logger.", "log."},
		},
	}
}

func PHPConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.PhpLanguage(),
		ClassTypes:             map[string]bool{"class_declaration": true},
		FunctionTypes:          map[string]bool{"function_definition": true, "method_declaration": true},
		ImportTypes:            map[string]bool{"namespace_use_clause": true},
		CallTypes:              map[string]bool{"function_call_expression": true, "member_call_expression": true, "scoped_call_expression": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"name"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"declaration_list", "compound_statement"},
		CallFunctionField:      "function",
		CallArgumentsField:     "arguments",
		CallAccessorNodeTypes:  map[string]bool{"member_call_expression": true},
		CallAccessorField:      "name",
		FunctionBoundaryTypes:  map[string]bool{"function_definition": true, "method_declaration": true},
		FunctionLabelParens:    true,
		ImportHandler:          importPHP,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_expression", "throw_statement"},
			CatchNodeTypes: []string{"catch_clause"},
			LogCallNames:   []string{"echo", "var_dump", "print_r", "error_log"},
			LogObjects:     []string{"$logger->", "$this->logger->"},
			ExecCallNames:  []string{"exec", "shell_exec", "system", "passthru", "popen", "proc_open"},
			FSCallNames:    []string{"fopen", "fclose", "fread", "fwrite", "file_get_contents", "file_put_contents", "unlink", "mkdir"},
			NetObjects:     []string{"$client->", "$http->"},
		},
	}
}

func LuaConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.LuaLanguage(),
		ClassTypes:             map[string]bool{},
		FunctionTypes:          map[string]bool{"function_declaration": true},
		ImportTypes:            map[string]bool{"variable_declaration": true},
		CallTypes:              map[string]bool{"function_call": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"identifier", "method_index_expression"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"block"},
		CallFunctionField:      "name",
		CallArgumentsField:     "arguments",
		CallAccessorNodeTypes:  map[string]bool{"method_index_expression": true},
		CallAccessorField:      "name",
		FunctionBoundaryTypes:  map[string]bool{"function_declaration": true},
		FunctionLabelParens:    true,
		ImportHandler:          importLua,
		Behavior: &BehaviorConfig{
			ThrowCallNames: []string{"error"},
			LogCallNames:   []string{"print", "io.write"},
		},
	}
}

func SwiftConfig() *LanguageConfig {
	return &LanguageConfig{
		Lang:                   grammars.SwiftLanguage(),
		ClassTypes:             map[string]bool{"class_declaration": true, "protocol_declaration": true},
		FunctionTypes:          map[string]bool{"function_declaration": true, "init_declaration": true, "deinit_declaration": true, "subscript_declaration": true},
		ImportTypes:            map[string]bool{"import_declaration": true},
		CallTypes:              map[string]bool{"call_expression": true},
		NameField:              "name",
		NameFallbackChildTypes: []string{"simple_identifier", "type_identifier", "user_type"},
		BodyField:              "body",
		BodyFallbackChildTypes: []string{"class_body", "protocol_body", "function_body", "enum_class_body"},
		CallFunctionField:      "",
		CallAccessorNodeTypes:  map[string]bool{"navigation_expression": true},
		FunctionBoundaryTypes:  map[string]bool{"function_declaration": true, "init_declaration": true, "deinit_declaration": true, "subscript_declaration": true},
		FunctionLabelParens:    true,
		ImportHandler:          importSwift,
		InheritanceFn:          swiftInheritance,
		CallResolveFn:          swiftCallResolve,
		Behavior: &BehaviorConfig{
			ThrowNodeTypes: []string{"throw_statement"},
			CatchNodeTypes: []string{"catch_block"},
			LogCallNames:   []string{"print", "debugPrint", "NSLog"},
			LogObjects:     []string{"Logger.", "logger.", "os_log."},
			FSObjects:      []string{"FileManager.", "FileHandle."},
			NetObjects:     []string{"URLSession.", "URLRequest."},
			AsyncNodeTypes: []string{"await_expression"},
		},
	}
}
