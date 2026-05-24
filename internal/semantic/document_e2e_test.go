package semantic

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestExtract_AppendsBaselineDocumentNode verifies the end-to-end document
// ingestion path: a markdown file is read, the LLM is invoked (mocked), and
// the returned ExtractionResult always carries a baseline `document` node
// with the file's content — independent of what the LLM produces.
func TestExtract_AppendsBaselineDocumentNode(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "README.md")
	mdContent := "# Project\n\nThis describes the authentication subsystem."
	if err := os.WriteFile(mdPath, []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := newMockOllama(t)

	res, err := Extract([]string{mdPath}, root, Options{
		BaseURL: mock.URL(),
		Model:   "qwen3:8b",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// Baseline document node should be present.
	var docNode *struct {
		ID         string
		Label      string
		FileType   string
		SourceFile string
		Content    string
	}
	for _, n := range res.Nodes {
		if n.ID == "readme_doc" {
			docNode = &struct {
				ID         string
				Label      string
				FileType   string
				SourceFile string
				Content    string
			}{n.ID, n.Label, n.FileType, n.SourceFile, n.Content}
			break
		}
	}
	if docNode == nil {
		t.Fatalf("baseline document node not found in %d nodes", len(res.Nodes))
	}
	if docNode.Label != "README.md" {
		t.Errorf("label: got %q want %q", docNode.Label, "README.md")
	}
	if docNode.FileType != "document" {
		t.Errorf("file_type: got %q want %q", docNode.FileType, "document")
	}
	if docNode.SourceFile != "README.md" {
		t.Errorf("source_file: got %q want %q", docNode.SourceFile, "README.md")
	}
	if docNode.Content == "" {
		t.Error("expected content to be populated")
	}

	// LLM-produced node should also be present (mocked to return mock_concept).
	hasLLM := false
	for _, n := range res.Nodes {
		if n.ID == "mock_concept" {
			hasLLM = true
		}
	}
	if !hasLLM {
		t.Error("expected mock LLM-produced node mock_concept")
	}
}

func TestExtract_BinaryFileRoutedThroughConverter(t *testing.T) {
	root := t.TempDir()
	// Write garbage to a .pdf file — ExtractPDFText returns "" → readFileAsText errors → file skipped.
	pdfPath := filepath.Join(root, "bogus.pdf")
	if err := os.WriteFile(pdfPath, []byte("not a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := newMockOllama(t)
	res, err := Extract([]string{pdfPath}, root, Options{
		BaseURL: mock.URL(),
		Model:   "qwen3:8b",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Garbage PDF should be skipped entirely — no nodes, no edges.
	if len(res.Nodes) != 0 {
		t.Errorf("expected 0 nodes for unreadable PDF, got %d", len(res.Nodes))
	}
}

// TestExtract_CachesResultAcrossRuns verifies the per-file SHA256 cache
// short-circuits a second Extract call.
func TestExtract_CachesResultAcrossRuns(t *testing.T) {
	root := t.TempDir()
	mdPath := filepath.Join(root, "design.md")
	if err := os.WriteFile(mdPath, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var chatCalls int
	mock := newMockOllama(t)
	original := mock.defaultChat
	mock.chatHandler = func(w http.ResponseWriter, r *http.Request) {
		chatCalls++
		original(w, r)
	}

	opts := Options{BaseURL: mock.URL(), Model: "qwen3:8b"}

	res1, err := Extract([]string{mdPath}, root, opts)
	if err != nil {
		t.Fatalf("first Extract: %v", err)
	}
	if chatCalls != 1 {
		t.Errorf("first run: want 1 LLM call, got %d", chatCalls)
	}
	if len(res1.Nodes) == 0 {
		t.Fatal("first run produced no nodes")
	}

	// Second run should hit the cache and skip the LLM.
	res2, err := Extract([]string{mdPath}, root, opts)
	if err != nil {
		t.Fatalf("second Extract: %v", err)
	}
	if chatCalls != 1 {
		t.Errorf("second run hit LLM (calls=%d), expected cache short-circuit", chatCalls)
	}
	if len(res2.Nodes) != len(res1.Nodes) {
		t.Errorf("cached node count differs: %d vs %d", len(res2.Nodes), len(res1.Nodes))
	}
}
