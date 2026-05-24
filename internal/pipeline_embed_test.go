package pipeline_test

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/gfy/internal/semantic"
	"github.com/qiangli/gfy/pkg/build"
	"github.com/qiangli/gfy/pkg/detect"
	"github.com/qiangli/gfy/pkg/extract"
	"github.com/qiangli/gfy/pkg/search"
	"github.com/qiangli/gfy/pkg/types"
)

const e2eEmbedDim = 8

func startMockEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/embed"):
			var req struct {
				Model string   `json:"model"`
				Input []string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			out := make([][]float64, len(req.Input))
			for i, text := range req.Input {
				out[i] = deterministicEmbed(text)
			}
			_ = json.NewEncoder(w).Encode(struct {
				Embeddings [][]float64 `json:"embeddings"`
			}{Embeddings: out})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func deterministicEmbed(text string) []float64 {
	vec := make([]float64, e2eEmbedDim)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		sum := h.Sum32()
		for i := 0; i < e2eEmbedDim; i++ {
			vec[i] += float64((sum>>uint(i*4))&0xF) / 15.0
		}
	}
	if len(vec) > 0 && vec[0] == 0 {
		vec[0] = 1
	}
	return vec
}

// TestPipeline_EmbeddingsSidecar exercises the full build-time path:
// detect → extract → build → embed → save → reload → query.
// It runs against the gfy testdata fixtures with a mock Ollama embedding
// server so the test is deterministic and offline-safe.
func TestPipeline_EmbeddingsSidecar(t *testing.T) {
	root := testdataDir()
	codeFiles := detect.Detect(root, false).Files[types.Code]
	if len(codeFiles) == 0 {
		t.Fatal("no testdata code files")
	}
	extraction := extract.Extract(codeFiles, "")
	g := build.BuildFromResult(extraction, false)
	if g.NodeCount() == 0 {
		t.Fatal("graph is empty")
	}

	mock := startMockEmbedServer(t)
	client := &semantic.Client{BaseURL: mock.URL}

	store, err := semantic.EmbedGraph(g, client, "mock-embed", nil)
	if err != nil {
		t.Fatalf("EmbedGraph: %v", err)
	}
	if store == nil {
		t.Fatal("EmbedGraph returned nil store")
	}
	if len(store.Vectors) == 0 {
		t.Fatal("no vectors produced")
	}
	if store.Dim != e2eEmbedDim {
		t.Errorf("dim: got %d want %d", store.Dim, e2eEmbedDim)
	}

	// Round-trip through the binary sidecar format.
	outDir := t.TempDir()
	path := filepath.Join(outDir, "embeddings.bin")
	if err := search.SaveEmbeddings(path, store); err != nil {
		t.Fatalf("SaveEmbeddings: %v", err)
	}
	loaded, err := search.LoadEmbeddings(path)
	if err != nil {
		t.Fatalf("LoadEmbeddings: %v", err)
	}
	if loaded.Model != store.Model || loaded.Dim != store.Dim {
		t.Errorf("metadata mismatch: got (%s,%d) want (%s,%d)",
			loaded.Model, loaded.Dim, store.Model, store.Dim)
	}
	if len(loaded.Vectors) != len(store.Vectors) {
		t.Errorf("vector count: got %d want %d", len(loaded.Vectors), len(store.Vectors))
	}

	// Embed a query the same way and verify ranking returns some results.
	qvec, err := client.Embed(store.Model, "add node")
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}
	results := search.RankByEmbedding(loaded, qvec, 5)
	if len(results) == 0 {
		t.Fatal("RankByEmbedding returned 0 results")
	}
	for i, r := range results {
		if r.Score <= 0 {
			t.Errorf("result[%d] %s: score %v should be > 0", i, r.ID, r.Score)
		}
	}
}

// TestPipeline_EmbeddingsRobustToOllamaOutage verifies that when the embedding
// server is unreachable, EmbedGraph surfaces a clear error and the caller can
// continue with an empty store.
func TestPipeline_EmbeddingsRobustToOllamaOutage(t *testing.T) {
	root := testdataDir()
	codeFiles := detect.Detect(root, false).Files[types.Code]
	extraction := extract.Extract(codeFiles, "")
	g := build.BuildFromResult(extraction, false)

	// Point at a closed port — connection refused.
	client := &semantic.Client{BaseURL: "http://127.0.0.1:1"}
	_, err := semantic.EmbedGraph(g, client, "mock-embed", nil)
	if err == nil {
		t.Fatal("expected error when Ollama unreachable")
	}
}
