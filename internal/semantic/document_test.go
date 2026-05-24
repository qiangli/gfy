package semantic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentNodeID(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"README.md", "readme_doc"},
		{"docs/architecture.md", "architecture_doc"},
		{"My Notes.pdf", "my_notes_doc"},
		{"/abs/path/Design Doc v2.docx", "design_doc_v2_doc"},
		{"123!@#.txt", "123_doc"},
		{"____.md", "doc_doc"}, // trims to empty stem → fallback
	}
	for _, tt := range tests {
		got := documentNodeID(tt.path)
		if got != tt.want {
			t.Errorf("documentNodeID(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestReadFileAsText_PlainText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.md")
	if err := os.WriteFile(p, []byte("# Hello\nBody."), 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := readFileAsText(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if text != "# Hello\nBody." {
		t.Errorf("text mismatch: got %q", text)
	}
}

func TestReadFileAsText_UnsupportedBinaryGracefulError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bogus.pdf")
	// Write non-PDF bytes; ExtractPDFText returns "" → readFileAsText errors.
	if err := os.WriteFile(p, []byte("not a pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readFileAsText(p)
	if err == nil {
		t.Fatal("expected error for invalid PDF")
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet("short", 100); got != "short" {
		t.Errorf("got %q", got)
	}
	got := snippet("abcdef", 3)
	if got != "abc…" {
		t.Errorf("got %q want %q", got, "abc…")
	}
}
