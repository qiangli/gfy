package extract

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func TestExtractGo(t *testing.T) {
	path := filepath.Join(testdataDir(), "sample.go")
	result := ExtractGo(path)

	if result.Error != "" {
		t.Fatalf("extraction error: %s", result.Error)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes, got none")
	}

	labels := make(map[string]bool)
	for _, n := range result.Nodes {
		labels[n.Label] = true
	}

	// Should find file node, type Server, functions.
	for _, want := range []string{"Server", "NewServer()", ".Start()", ".validate()", "Helper()"} {
		if !labels[want] {
			t.Errorf("missing node label %q", want)
		}
	}

	// Check for edges.
	relations := make(map[string]bool)
	for _, e := range result.Edges {
		relations[e.Relation] = true
	}
	for _, want := range []string{"contains", "method", "imports_from"} {
		if !relations[want] {
			t.Errorf("missing relation %q", want)
		}
	}

	// Should have calls edges (within-file).
	hasCalls := false
	for _, e := range result.Edges {
		if e.Relation == "calls" {
			hasCalls = true
			break
		}
	}
	if !hasCalls {
		t.Error("expected at least one 'calls' edge")
	}

	// No dangling edges.
	nodeIDs := make(map[string]bool)
	for _, n := range result.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, e := range result.Edges {
		// Allow edges to import targets (which may not have nodes).
		if e.Relation == "imports_from" || e.Relation == "imports" {
			continue
		}
		if !nodeIDs[e.Source] {
			t.Errorf("dangling edge source: %s", e.Source)
		}
		if !nodeIDs[e.Target] {
			t.Errorf("dangling edge target: %s -> %s (relation: %s)", e.Source, e.Target, e.Relation)
		}
	}
}

func TestExtractPython(t *testing.T) {
	path := filepath.Join(testdataDir(), "sample.py")
	result := ExtractPython(path)

	if result.Error != "" {
		t.Fatalf("extraction error: %s", result.Error)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("expected nodes, got none")
	}

	labels := make(map[string]bool)
	for _, n := range result.Nodes {
		labels[n.Label] = true
	}

	for _, want := range []string{"Animal", "Dog", ".__init__()", ".speak()", "create_dog()"} {
		if !labels[want] {
			t.Errorf("missing node label %q", want)
		}
	}

	// Check inheritance.
	hasInherits := false
	for _, e := range result.Edges {
		if e.Relation == "inherits" {
			hasInherits = true
			break
		}
	}
	if !hasInherits {
		t.Error("expected 'inherits' edge for Dog -> Animal")
	}
}

func TestMakeID(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{[]string{"hello", "world"}, "hello_world"},
		{[]string{"My.Class", "Method"}, "my_class_method"},
		{[]string{"", "name", ""}, "name"},
		{[]string{"path/to/file.py"}, "path_to_file_py"},
	}
	for _, tt := range tests {
		got := MakeID(tt.parts...)
		if got != tt.want {
			t.Errorf("MakeID(%v) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}
