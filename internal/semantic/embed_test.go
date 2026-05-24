package semantic

import (
	"strings"
	"testing"

	"github.com/qiangli/gfy/pkg/graph"
)

func TestSelectEmbedModel(t *testing.T) {
	models := []ModelInfo{
		{Name: "qwen3:8b"},
		{Name: "nomic-embed-text:latest"},
		{Name: "llama3.2:3b"},
	}
	got := SelectEmbedModel(models)
	if got != "nomic-embed-text:latest" {
		t.Errorf("expected nomic-embed-text, got %q", got)
	}

	// No embedding models in list.
	got = SelectEmbedModel([]ModelInfo{{Name: "qwen3:8b"}})
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Multiple embed models — first preference wins.
	got = SelectEmbedModel([]ModelInfo{
		{Name: "mxbai-embed-large:latest"},
		{Name: "nomic-embed-text:v1.5"},
	})
	if got != "nomic-embed-text:v1.5" {
		t.Errorf("expected nomic-embed-text:v1.5 (first preference), got %q", got)
	}
}

func TestClient_EmbedBatch_RoundTrip(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	vecs, err := c.EmbedBatch("nomic-embed-text", []string{"hello world", "goodbye world", "hello world"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != mock.dim {
			t.Errorf("vec %d: dim %d want %d", i, len(v), mock.dim)
		}
	}
	// Identical text → identical vector.
	if vecToString(vecs[0]) != vecToString(vecs[2]) {
		t.Errorf("same input differs: %s vs %s", vecToString(vecs[0]), vecToString(vecs[2]))
	}
	// Different text → different vector.
	if vecToString(vecs[0]) == vecToString(vecs[1]) {
		t.Errorf("different inputs collide: %s", vecToString(vecs[0]))
	}
}

func TestClient_EmbedBatch_FiltersEmpty(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	vecs, err := c.EmbedBatch("nomic-embed-text", []string{"hello", "", "world", "   "})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 4 {
		t.Fatalf("want 4 slots, got %d", len(vecs))
	}
	if vecs[0] == nil || vecs[2] == nil {
		t.Error("expected vectors for non-empty inputs")
	}
	if vecs[1] != nil || vecs[3] != nil {
		t.Error("expected nil vectors for empty inputs")
	}
}

func TestClient_EmbedBatch_AllEmpty(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	vecs, err := c.EmbedBatch("nomic-embed-text", []string{"", "  ", ""})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("want 3 slots, got %d", len(vecs))
	}
	for i, v := range vecs {
		if v != nil {
			t.Errorf("vec %d: want nil, got %v", i, v)
		}
	}
}

func TestClient_EmbedBatch_EmptyModelRejected(t *testing.T) {
	c := &Client{BaseURL: "http://localhost:11434"}
	_, err := c.EmbedBatch("", []string{"x"})
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}

func TestClient_EmbedBatch_ServerError(t *testing.T) {
	mock := newMockOllama(t)
	mock.embedHandler = embedErrorHandler(500, "boom")
	c := &Client{BaseURL: mock.URL()}

	_, err := c.EmbedBatch("nomic-embed-text", []string{"hello"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error should mention status: %v", err)
	}
}

func TestClient_Embed_Single(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	vec, err := c.Embed("nomic-embed-text", "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != mock.dim {
		t.Errorf("dim %d want %d", len(vec), mock.dim)
	}
}

func TestEmbedGraph_E2E(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	g := graph.New(false)
	g.AddNode("a", map[string]any{"label": "addNode", "file_type": "code", "source_file": "graph.go"})
	g.AddNode("b", map[string]any{"label": "removeNode", "file_type": "code", "source_file": "graph.go"})
	g.AddNode("c", map[string]any{"label": "parseToken", "file_type": "code", "source_file": "lexer.go"})

	store, err := EmbedGraph(g, c, "nomic-embed-text", nil)
	if err != nil {
		t.Fatalf("EmbedGraph: %v", err)
	}
	if store == nil {
		t.Fatal("expected store, got nil")
	}
	if len(store.Vectors) != 3 {
		t.Errorf("want 3 vectors, got %d", len(store.Vectors))
	}
	if store.Dim != mock.dim {
		t.Errorf("dim: got %d want %d", store.Dim, mock.dim)
	}
	if store.Model != "nomic-embed-text" {
		t.Errorf("model: got %q want %q", store.Model, "nomic-embed-text")
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, ok := store.Vectors[id]; !ok {
			t.Errorf("missing vector for node %s", id)
		}
	}
}

func TestEmbedGraph_EmptyModel_NoOp(t *testing.T) {
	g := graph.New(false)
	g.AddNode("a", map[string]any{"label": "x"})

	store, err := EmbedGraph(g, &Client{BaseURL: "unused"}, "", nil)
	if err != nil {
		t.Fatalf("EmbedGraph: %v", err)
	}
	if store != nil {
		t.Errorf("expected nil store for empty model, got %+v", store)
	}
}

func TestEmbedGraph_ProgressCallback(t *testing.T) {
	mock := newMockOllama(t)
	c := &Client{BaseURL: mock.URL()}

	g := graph.New(false)
	for i := 0; i < 10; i++ {
		g.AddNode(string(rune('a'+i)), map[string]any{"label": "node"})
	}

	var lastDone, lastTotal int
	var calls int
	store, err := EmbedGraph(g, c, "nomic-embed-text", func(done, total int) {
		calls++
		lastDone = done
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("EmbedGraph: %v", err)
	}
	if calls == 0 {
		t.Error("progress callback never fired")
	}
	if lastDone != 10 || lastTotal != 10 {
		t.Errorf("final progress: %d/%d want 10/10", lastDone, lastTotal)
	}
	if len(store.Vectors) != 10 {
		t.Errorf("want 10 vectors, got %d", len(store.Vectors))
	}
}
