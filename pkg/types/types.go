// Package types defines shared data structures for the gfy pipeline.
package types

// Confidence indicates how a relationship was determined.
type Confidence string

const (
	Extracted Confidence = "EXTRACTED"
	Inferred  Confidence = "INFERRED"
	Ambiguous Confidence = "AMBIGUOUS"
)

// FileType classifies detected files.
type FileType string

const (
	Code     FileType = "code"
	Document FileType = "document"
	Paper    FileType = "paper"
	Image    FileType = "image"
	Video    FileType = "video"
	// Used in extraction output for special node types.
	Rationale FileType = "rationale"
	Concept   FileType = "concept"
)

// Node represents a single entity in the knowledge graph.
type Node struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	FileType       string   `json:"file_type"`
	SourceFile     string   `json:"source_file"`
	SourceLocation string   `json:"source_location,omitempty"`
	Tags           []string `json:"tags,omitempty"`
	Comment        string   `json:"comment,omitempty"`
	LogMessages    []string `json:"log_messages,omitempty"`
	ThrowMessages  []string `json:"throw_messages,omitempty"`
}

// Edge represents a relationship between two nodes.
type Edge struct {
	Source          string     `json:"source"`
	Target          string     `json:"target"`
	Relation        string     `json:"relation"`
	Confidence      Confidence `json:"confidence"`
	ConfidenceScore float64    `json:"confidence_score,omitempty"`
	SourceFile      string     `json:"source_file"`
	SourceLocation  string     `json:"source_location,omitempty"`
	Weight          float64    `json:"weight"`
}

// RawCall records an unresolved function call for cross-file resolution.
type RawCall struct {
	CallerNID    string `json:"caller_nid"`
	Callee       string `json:"callee"`
	IsMemberCall bool   `json:"is_member_call"`
	SourceFile   string `json:"source_file"`
	SourceLoc    string `json:"source_location"`
}

// ExtractionResult holds the output of extracting one or more files.
type ExtractionResult struct {
	Nodes        []Node    `json:"nodes"`
	Edges        []Edge    `json:"edges"`
	RawCalls     []RawCall `json:"raw_calls,omitempty"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Error        string    `json:"error,omitempty"`
}

// DetectionResult holds the output of file discovery and classification.
type DetectionResult struct {
	Files               map[FileType][]string `json:"files"`
	TotalFiles          int                   `json:"total_files"`
	TotalWords          int                   `json:"total_words"`
	NeedsGraph          bool                  `json:"needs_graph"`
	Warning             string                `json:"warning,omitempty"`
	SkippedSensitive    []string              `json:"skipped_sensitive"`
	GfyIgnoreCount int                   `json:"gfyignore_patterns"`
}
