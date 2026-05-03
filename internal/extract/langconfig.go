package extract

import (
	ts "github.com/odvcencio/gotreesitter"
	"github.com/qiangli/gfy/internal/types"
)

// ImportHandler processes an import node and appends edges.
type ImportHandler func(node *ts.Node, lang *ts.Language, source []byte, fileNID, stem, strPath string, edges *[]types.Edge)

// ResolveFuncNameFn extracts a function name from a declarator node (C/C++).
type ResolveFuncNameFn func(node *ts.Node, lang *ts.Language, source []byte) string

// ExtraWalkFn handles language-specific node types during the generic walk.
// Returns true if the node was handled.
type ExtraWalkFn func(ctx *GenericContext, node *ts.Node, parentClassNID string) bool

// CallResolveFn handles language-specific call resolution in walk_calls.
// Returns (calleeName, isMemberCall, handled).
type CallResolveFn func(node *ts.Node, lang *ts.Language, source []byte) (string, bool, bool)

// InheritanceFn handles language-specific inheritance extraction for class nodes.
type InheritanceFn func(ctx *GenericContext, classNode *ts.Node, classNID string, line int)

// LanguageConfig drives the generic AST extractor for a given language.
type LanguageConfig struct {
	// Language grammar loaded via grammars package.
	Lang *ts.Language

	// AST node types.
	ClassTypes    map[string]bool
	FunctionTypes map[string]bool
	ImportTypes   map[string]bool
	CallTypes     map[string]bool

	// Name extraction.
	NameField              string
	NameFallbackChildTypes []string

	// Body detection.
	BodyField              string
	BodyFallbackChildTypes []string

	// Call name extraction.
	CallFunctionField     string
	CallArgumentsField    string
	CallAccessorNodeTypes map[string]bool
	CallAccessorField     string
	FunctionBoundaryTypes map[string]bool

	// Whether to add "()" to function labels.
	FunctionLabelParens bool

	// Optional handlers.
	ImportHandler     ImportHandler
	ResolveFuncNameFn ResolveFuncNameFn
	ExtraWalkFn       ExtraWalkFn
	CallResolveFn     CallResolveFn
	InheritanceFn     InheritanceFn

	// Behavioral tagging config.
	Behavior *BehaviorConfig
}

// BehaviorConfig declares patterns for tagging function nodes with behavioral labels.
type BehaviorConfig struct {
	ThrowNodeTypes  []string // Node types that indicate throwing (e.g., "throw_statement")
	CatchNodeTypes  []string // Node types that indicate error handling (e.g., "catch_clause")
	ThrowCallNames  []string // Bare function calls that indicate throwing (e.g., "exit", "abort")
	LogCallNames    []string // Bare function calls that indicate logging (e.g., "printf")
	LogObjects      []string // Object names whose method calls indicate logging (e.g., "Console")
	ExecCallNames   []string // Bare function calls that run external processes (e.g., "system")
	FSCallNames     []string // Bare function calls for file system access (e.g., "fopen")
	FSObjects       []string // Object prefixes for FS access (e.g., "File.")
	NetObjects      []string // Object prefixes for network access (e.g., "HttpClient.")
	AsyncNodeTypes  []string // Node types indicating async (e.g., "await_expression")
	UnsafeNodeTypes []string // Node types indicating unsafe code (e.g., "unsafe_block")
}

// GenericContext holds mutable state during generic extraction.
type GenericContext struct {
	Lang           *ts.Language
	Config         *LanguageConfig
	Source         []byte
	Stem           string
	StrPath        string
	FileNID        string
	Nodes          *[]types.Node
	Edges          *[]types.Edge
	SeenIDs        map[string]bool
	FunctionBodies *[]bodyEntry
}

type bodyEntry struct {
	callerNID string
	node      *ts.Node
}

// AddNode adds a node if not already seen.
func (ctx *GenericContext) AddNode(nid, label string, line int) {
	ctx.AddNodeWithDecl(nid, label, line, nil)
}

// AddNodeWithDecl adds a node and extracts any preceding comment.
func (ctx *GenericContext) AddNodeWithDecl(nid, label string, line int, declNode *ts.Node) {
	if !ctx.SeenIDs[nid] {
		ctx.SeenIDs[nid] = true
		*ctx.Nodes = append(*ctx.Nodes, types.Node{
			ID: nid, Label: label, FileType: string(types.Code),
			SourceFile: ctx.StrPath, SourceLocation: SourceLoc(line),
		})
		if declNode != nil {
			if prev := declNode.PrevSibling(); prev != nil && prev.Type(ctx.Lang) == "comment" {
				SetComment(*ctx.Nodes, nid, ctx.RT(prev))
			}
		}
	}
}

// AddEdge adds an edge.
func (ctx *GenericContext) AddEdge(src, tgt, relation string, line int) {
	*ctx.Edges = append(*ctx.Edges, types.Edge{
		Source: src, Target: tgt, Relation: relation,
		Confidence: types.Extracted, SourceFile: ctx.StrPath,
		SourceLocation: SourceLoc(line), Weight: 1.0,
	})
}

// AddExternalNode adds a node that may come from outside the current file (e.g., base classes).
func (ctx *GenericContext) AddExternalNode(nid, label string) {
	if !ctx.SeenIDs[nid] {
		ctx.SeenIDs[nid] = true
		*ctx.Nodes = append(*ctx.Nodes, types.Node{
			ID: nid, Label: label, FileType: string(types.Code),
			SourceFile: "", SourceLocation: "",
		})
	}
}

// RT reads text from a node.
func (ctx *GenericContext) RT(n *ts.Node) string {
	return ReadText(ctx.Source, n.StartByte(), n.EndByte())
}
