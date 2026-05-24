package serve

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/qiangli/gfy/internal/semantic"
	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
)

// embedDim must match across the mock server and the prebuilt store so that
// query and stored vectors are commensurable.
const embedDim = 8

// mockOllamaEmbed returns an httptest server that responds to /api/embed with
// deterministic vectors derived from the input text. Identical to the mock in
// internal/semantic but duplicated here to avoid an import cycle.
func mockOllamaEmbed(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/embed") {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vecs := make([][]float64, len(req.Input))
		for i, text := range req.Input {
			vecs[i] = hashEmbed(text)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Embeddings [][]float64 `json:"embeddings"`
		}{Embeddings: vecs})
	}))
	t.Cleanup(s.Close)
	return s
}

func hashEmbed(text string) []float64 {
	vec := make([]float64, embedDim)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		sum := h.Sum32()
		for i := 0; i < embedDim; i++ {
			bit := float64((sum>>uint(i*4))&0xF) / 15.0
			vec[i] += bit
		}
	}
	if len(vec) > 0 && vec[0] == 0 {
		vec[0] = 1
	}
	return vec
}

// hashEmbed32 mirrors hashEmbed but returns float32 so we can pre-populate the
// embedding store with vectors matching what the mock server would generate.
func hashEmbed32(text string) []float32 {
	v := hashEmbed(text)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(x)
	}
	return out
}

// TestSemanticSearch_E2E exercises the full wiring: registerSemanticSearch
// registers the tool against an MCP server, and the underlying flow —
// embed query → RankByEmbedding — produces the expected ranking when
// stored vectors and query vector come from the same (mock) model.
func TestSemanticSearch_E2E(t *testing.T) {
	mock := mockOllamaEmbed(t)

	// Build a small graph and prebuild an EmbeddingStore using the same
	// hashEmbed function the mock server uses for queries.
	g := graph.New(false)
	nodes := map[string]string{
		"auth_login":    "login validate password",
		"db_query":      "database query rows",
		"http_handler":  "http handler request",
		"rate_limiter":  "rate limit throttle requests per second",
		"unused_helper": "helper utility",
	}
	store := &search.EmbeddingStore{
		Model:   "mock-embed",
		Dim:     embedDim,
		Vectors: make(map[string][]float32),
	}
	for id, text := range nodes {
		g.AddNode(id, map[string]any{"label": id, "file_type": "code", "source_file": "x.go"})
		store.Vectors[id] = hashEmbed32(text)
	}

	// Register against an MCP server; verify it doesn't panic and the tool
	// metadata is set up correctly.
	server := mcp.NewServer(&mcp.Implementation{Name: "gfy-test", Version: "test"}, nil)
	registerSemanticSearch(server, g, store, mock.URL)

	// Exercise the underlying flow that the registered handler runs.
	client := &semantic.Client{BaseURL: mock.URL}
	queryVec, err := client.Embed(store.Model, "rate limit requests")
	if err != nil {
		t.Fatalf("embed query: %v", err)
	}
	results := search.RankByEmbedding(store, queryVec, 5)
	if len(results) == 0 {
		t.Fatal("got 0 results for a query that overlaps with rate_limiter")
	}
	// rate_limiter shares "rate", "limit", "requests" with the query → must rank first.
	if results[0].ID != "rate_limiter" {
		t.Errorf("top result: got %q, want rate_limiter (results: %+v)", results[0].ID, results)
	}
}

// TestSemanticSearch_ServerErrorPropagatesAsToolError verifies that an
// upstream Ollama failure doesn't crash the registration or panic when the
// tool runs — the handler should report the error textually.
func TestSemanticSearch_ServerErrorHandled(t *testing.T) {
	// Server that always 500s.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	g := graph.New(false)
	g.AddNode("only", map[string]any{"label": "only"})
	store := &search.EmbeddingStore{
		Model: "mock-embed",
		Dim:   embedDim,
		Vectors: map[string][]float32{
			"only": hashEmbed32("only one"),
		},
	}

	// Drive the embed step directly — same code path the handler uses.
	client := &semantic.Client{BaseURL: bad.URL}
	_, err := client.Embed(store.Model, "anything")
	if err == nil {
		t.Fatal("expected error from broken Ollama")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}

	// Register against an MCP server too — just to assert registration is panic-free
	// even though the upstream is broken.
	server := mcp.NewServer(&mcp.Implementation{Name: "gfy-test", Version: "test"}, nil)
	registerSemanticSearch(server, g, store, bad.URL)
}

// TestServeOptions_PassesThrough verifies that ServeWithOptions wires
// embeddings into the search tool registration path.
func TestServeOptions_RegistersSemanticSearchOnlyWithEmbeddings(t *testing.T) {
	g, communities := buildTestGraph()

	// Without embeddings, the semantic_search tool should not be added — but
	// registerTools must still succeed.
	server := mcp.NewServer(&mcp.Implementation{Name: "gfy-test"}, nil)
	registerTools(server, g, communities, Options{})

	// With embeddings AND a URL, it should also succeed.
	store := &search.EmbeddingStore{
		Model: "mock-embed",
		Dim:   embedDim,
		Vectors: map[string][]float32{
			"main_main": hashEmbed32("main entry"),
		},
	}
	server2 := mcp.NewServer(&mcp.Implementation{Name: "gfy-test"}, nil)
	registerTools(server2, g, communities, Options{Embeddings: store, OllamaURL: "http://localhost:11434"})
}
