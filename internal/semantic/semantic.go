package semantic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/qiangli/gfy/pkg/cache"
	"github.com/qiangli/gfy/pkg/detect"
	"github.com/qiangli/gfy/pkg/types"
)

// Options configures semantic extraction.
type Options struct {
	BaseURL     string                  // Ollama server URL (default: http://localhost:11434)
	Model       string                  // Model name (e.g., qwen3:8b)
	CodeSummary string                  // Node directory text (from ExtractNodeDirectory)
	ASTNodes    *types.ExtractionResult // AST extraction result for tool-use queries (nil = no tools)
}

const (
	// Maximum chars to read from a single file.
	maxFileChars = 60_000 // ~20K tokens
)

// extractionPrompt tells the LLM to extract concepts, relationships, and
// design rationale from non-code files (docs, papers, images). The LLM
// produces nodes and edges following the {stem}_{entity} ID convention so
// that Louvain clustering can link them to AST-extracted code nodes by
// edge density — no explicit AST index needed.
const extractionPrompt = `You are a semantic extraction agent. Extract a knowledge graph fragment from the document provided.
Output ONLY valid JSON — no explanation, no markdown fences, no preamble.
Use COMPRESSED key names to minimize output tokens.

Rules:
- EXTRACTED: relationship explicit in source (citation, reference, direct mention)
- INFERRED: reasonable inference (shared concept, implied dependency)
- AMBIGUOUS: uncertain — flag for review, do not omit
- Extract concepts, design rationale, and relationships from documents, papers, and notes
- Create edges between entities: references, conceptually_related_to, shares_data_with, semantically_similar_to, rationale_for, cites
- When a document mentions a code entity (class, function, module), create an edge to it using the code entity's expected ID
- IMPORTANT: If a Code Entity Directory is provided, use those exact entity IDs as edge targets when the document references matching concepts
- Use the provided tools (search_nodes, get_node_detail, get_neighbors, similar_nodes) to explore the codebase and create accurate cross-references

Node ID format: lowercase, only [a-z0-9_], no dots or slashes.
Format: {stem}_{entity} where stem = filename without extension, entity = name (both normalised to lowercase, non-alphanumeric replaced with _).
Examples: docs/architecture.md + "AuthService" → architecture_authservice, README.md + "overview" → readme_overview

Output COMPRESSED JSON using these short keys:
Nodes: id, l=label, t=file_type, f=source_file
Edges: s=source, g=target, r=relation, c=confidence, cs=confidence_score, f=source_file, w=weight

Example:
{"n":[{"id":"readme_auth","l":"Auth Design","t":"document","f":"README.md"}],"e":[{"s":"readme_auth","g":"auth_validate","r":"references","c":"EXTRACTED","cs":0.9,"f":"README.md","w":1.0}]}`

// llmResult is the JSON shape returned by the LLM.
// Supports both compressed (short keys) and verbose (full keys) formats.
type llmResult struct {
	// Compressed format.
	CompressedNodes []struct {
		ID       string `json:"id"`
		Label    string `json:"l"`
		FileType string `json:"t"`
		SrcFile  string `json:"f"`
	} `json:"n"`
	CompressedEdges []struct {
		Source    string  `json:"s"`
		Target    string  `json:"g"`
		Relation  string  `json:"r"`
		Conf      string  `json:"c"`
		ConfScore float64 `json:"cs"`
		SrcFile   string  `json:"f"`
		Weight    float64 `json:"w"`
	} `json:"e"`

	// Verbose format (fallback if LLM ignores compression instruction).
	Nodes []struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		FileType   string `json:"file_type"`
		SourceFile string `json:"source_file"`
	} `json:"nodes"`
	Edges []struct {
		Source          string  `json:"source"`
		Target          string  `json:"target"`
		Relation        string  `json:"relation"`
		Confidence      string  `json:"confidence"`
		ConfidenceScore float64 `json:"confidence_score"`
		SourceFile      string  `json:"source_file"`
		Weight          float64 `json:"weight"`
	} `json:"edges"`
}

// ExtractNodeDirectory sends a compact node directory (ID + label pairs) to
// the LLM as the first "document" through the standard extraction pipeline.
// This gives the LLM a map of all code entities. For subsequent per-file calls,
// tools provide on-demand access to node details.
//
// Returns the extraction result (nodes/edges) and the directory text.
func ExtractNodeDirectory(client *Client, nodes []types.Node) (*types.ExtractionResult, string) {
	directory := buildNodeDirectory(nodes)
	if strings.TrimSpace(directory) == "" {
		return &types.ExtractionResult{}, ""
	}

	fmt.Println("  Extracting from node directory...")
	userMsg := fmt.Sprintf("=== Code Entity Directory ===\n%s", directory)

	content, usage, err := client.ChatCompletion(extractionPrompt, userMsg)
	if err != nil {
		fmt.Printf("  Warning: node directory extraction failed: %v\n", err)
		return &types.ExtractionResult{}, directory
	}

	result := parseLLMResponse(content)
	result.InputTokens = usage.PromptTokens
	result.OutputTokens = usage.CompletionTokens

	if len(result.Nodes) > 0 || len(result.Edges) > 0 {
		fmt.Printf("  Node directory: +%d nodes, +%d edges\n",
			len(result.Nodes), len(result.Edges))
	}

	return result, directory
}

// Extract runs semantic extraction on non-code files (docs, papers) via the LLM.
// Each file is processed independently for clear progress reporting and
// per-file caching. Cached results are loaded and skipped.
func Extract(files []string, root string, opts Options) (*types.ExtractionResult, error) {
	merged := &types.ExtractionResult{}

	// Load cached results and filter to uncached files.
	var uncached []string
	cacheHits := 0
	for _, f := range files {
		if cached := cache.Load(f, root, "semantic"); cached != nil {
			merged.Nodes = append(merged.Nodes, cached.Nodes...)
			merged.Edges = append(merged.Edges, cached.Edges...)
			cacheHits++
		} else {
			uncached = append(uncached, f)
		}
	}
	if cacheHits > 0 {
		fmt.Printf("  Semantic cache: %d/%d files from cache\n", cacheHits, len(files))
	}
	if len(uncached) == 0 {
		return merged, nil
	}

	client := &Client{BaseURL: opts.BaseURL, Model: opts.Model}

	// Build tools if AST nodes are provided.
	var tools []ASTTool
	if opts.ASTNodes != nil && len(opts.ASTNodes.Nodes) > 0 {
		tools = NewASTTools(opts.ASTNodes)
	}

	for i, path := range uncached {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}

		text, readErr := readFileAsText(path)
		if readErr != nil {
			fmt.Printf("  [%d/%d] %s — skipped (read error: %v)\n", i+1, len(uncached), rel, readErr)
			continue
		}
		if len(text) > maxFileChars {
			text = text[:maxFileChars]
		}
		if strings.TrimSpace(text) == "" {
			continue
		}

		// Emit a baseline `document` node so the raw content is embeddable.
		// LLM-generated nodes (concepts, rationale) are layered on top via
		// the extraction prompt and link back to this node by source_file.
		docNode := types.Node{
			ID:         documentNodeID(path),
			Label:      filepath.Base(path),
			FileType:   string(types.Document),
			SourceFile: rel,
			Content:    snippet(text, 4000),
		}

		var userMsg string
		if opts.CodeSummary != "" {
			userMsg = fmt.Sprintf("=== Code Entity Directory ===\n%s\n\n=== %s ===\n%s", opts.CodeSummary, rel, text)
		} else {
			userMsg = fmt.Sprintf("=== %s ===\n%s", rel, text)
		}
		fmt.Printf("  [%d/%d] %s...\n", i+1, len(uncached), rel)

		var content string
		var usage Usage
		if len(tools) > 0 {
			content, usage, err = client.ChatCompletionWithTools(extractionPrompt, userMsg, tools)
		} else {
			content, usage, err = client.ChatCompletion(extractionPrompt, userMsg)
		}
		if err != nil {
			fmt.Printf("    Warning: failed: %v\n", err)
			continue
		}

		result := parseLLMResponse(content)
		// Prepend the document node; it gets cached too so embeddings see it on rerun.
		result.Nodes = append([]types.Node{docNode}, result.Nodes...)
		merged.Nodes = append(merged.Nodes, result.Nodes...)
		merged.Edges = append(merged.Edges, result.Edges...)
		merged.InputTokens += usage.PromptTokens
		merged.OutputTokens += usage.CompletionTokens

		// Cache per file.
		_ = cache.Save(path, result, root, "semantic")

		if len(result.Nodes) > 0 || len(result.Edges) > 0 {
			fmt.Printf("    +%d nodes, +%d edges\n", len(result.Nodes), len(result.Edges))
		}
	}

	return merged, nil
}

// Merge combines AST and semantic extraction results.
// AST nodes go first; semantic nodes with the same ID overwrite (richer labels).
// All edges from both sources are combined without deduplication.
func Merge(ast, semantic *types.ExtractionResult) *types.ExtractionResult {
	nodeIndex := make(map[string]int, len(ast.Nodes))
	merged := &types.ExtractionResult{
		Nodes:        make([]types.Node, 0, len(ast.Nodes)+len(semantic.Nodes)),
		Edges:        make([]types.Edge, 0, len(ast.Edges)+len(semantic.Edges)),
		InputTokens:  ast.InputTokens + semantic.InputTokens,
		OutputTokens: ast.OutputTokens + semantic.OutputTokens,
	}

	// AST nodes first.
	for _, n := range ast.Nodes {
		nodeIndex[n.ID] = len(merged.Nodes)
		merged.Nodes = append(merged.Nodes, n)
	}

	// Semantic nodes: overwrite on same ID, append otherwise.
	for _, n := range semantic.Nodes {
		if idx, ok := nodeIndex[n.ID]; ok {
			merged.Nodes[idx] = n
		} else {
			nodeIndex[n.ID] = len(merged.Nodes)
			merged.Nodes = append(merged.Nodes, n)
		}
	}

	// All edges combined.
	merged.Edges = append(merged.Edges, ast.Edges...)
	merged.Edges = append(merged.Edges, semantic.Edges...)

	return merged
}

// stripMarkdownFences removes ```json ... ``` wrapping if present.
func stripMarkdownFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		parts := strings.SplitN(s, "\n", 2)
		if len(parts) == 2 {
			s = parts[1]
		}
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

// stripThinkingTags removes <think>...</think> blocks that some models (e.g., qwen3) emit.
func stripThinkingTags(s string) string {
	re := regexp.MustCompile(`(?s)<think>.*?</think>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

func parseLLMResponse(raw string) *types.ExtractionResult {
	raw = stripThinkingTags(raw)
	raw = stripMarkdownFences(raw)

	var parsed llmResult
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		fmt.Printf("    Warning: LLM returned invalid JSON, skipping: %v\n", err)
		return &types.ExtractionResult{}
	}

	result := &types.ExtractionResult{}

	// Parse compressed format (n/e keys) if present, otherwise fall back to verbose (nodes/edges).
	if len(parsed.CompressedNodes) > 0 {
		for _, n := range parsed.CompressedNodes {
			result.Nodes = append(result.Nodes, types.Node{
				ID:         n.ID,
				Label:      n.Label,
				FileType:   n.FileType,
				SourceFile: n.SrcFile,
			})
		}
	} else {
		for _, n := range parsed.Nodes {
			result.Nodes = append(result.Nodes, types.Node{
				ID:         n.ID,
				Label:      n.Label,
				FileType:   n.FileType,
				SourceFile: n.SourceFile,
			})
		}
	}

	if len(parsed.CompressedEdges) > 0 {
		for _, e := range parsed.CompressedEdges {
			result.Edges = append(result.Edges, normalizeEdge(e.Source, e.Target, e.Relation, e.Conf, e.ConfScore, e.SrcFile, e.Weight))
		}
	} else {
		for _, e := range parsed.Edges {
			result.Edges = append(result.Edges, normalizeEdge(e.Source, e.Target, e.Relation, e.Confidence, e.ConfidenceScore, e.SourceFile, e.Weight))
		}
	}

	return result
}

// readFileAsText converts a file to text by extension. Binary formats
// (PDF/DOCX/XLSX) are routed through the detect package converters; everything
// else is read as UTF-8.
func readFileAsText(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		text := detect.ExtractPDFText(path)
		if text == "" {
			return "", fmt.Errorf("pdf yielded no text (likely scanned/encrypted)")
		}
		return text, nil
	case ".docx":
		text := detect.DocxToMarkdown(path)
		if text == "" {
			return "", fmt.Errorf("docx yielded no text")
		}
		return text, nil
	case ".xlsx":
		text := detect.XlsxToMarkdown(path)
		if text == "" {
			return "", fmt.Errorf("xlsx yielded no text")
		}
		return text, nil
	default:
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// documentNodeID derives a stable, lowercase node ID from a file path, matching
// the {stem}_{kind} convention used by the extraction prompt.
func documentNodeID(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stem = strings.ToLower(reIDNorm.ReplaceAllString(stem, "_"))
	stem = strings.Trim(stem, "_")
	if stem == "" {
		stem = "doc"
	}
	return stem + "_doc"
}

var reIDNorm = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// snippet returns the first n characters of s, with an ellipsis if truncated.
func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// normalizeEdge creates an Edge with validated confidence, score, and weight.
func normalizeEdge(source, target, relation, confidence string, score float64, sourceFile string, weight float64) types.Edge {
	conf := types.Confidence(confidence)
	if conf != types.Extracted && conf != types.Inferred && conf != types.Ambiguous {
		conf = types.Inferred
	}
	if score <= 0 || score > 1 {
		score = 0.8
	}
	if weight <= 0 {
		weight = 1.0
	}
	return types.Edge{
		Source:          source,
		Target:          target,
		Relation:        relation,
		Confidence:      conf,
		ConfidenceScore: score,
		SourceFile:      sourceFile,
		Weight:          weight,
	}
}
