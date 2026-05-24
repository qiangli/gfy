package semantic

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockOllama is a deterministic stand-in for an Ollama server. It implements
// the three endpoints gfy talks to: /api/tags, /api/embed, and the
// OpenAI-compatible /v1/chat/completions. Embeddings are derived from a hash
// of the input so identical strings always map to identical vectors and tests
// can assert on relative similarity.
type mockOllama struct {
	server *httptest.Server
	// Optional override hooks for tests that need to exercise error paths.
	chatHandler  http.HandlerFunc
	tagsHandler  http.HandlerFunc
	embedHandler http.HandlerFunc
	// Default embedding dimension. Keep small so tests are quick.
	dim int
	// Default model returned by /api/tags.
	models []string
}

func newMockOllama(t *testing.T) *mockOllama {
	t.Helper()
	m := &mockOllama{
		dim:    8,
		models: []string{"qwen3:8b", "nomic-embed-text:latest"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		if m.tagsHandler != nil {
			m.tagsHandler(w, r)
			return
		}
		m.defaultTags(w)
	})
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		if m.embedHandler != nil {
			m.embedHandler(w, r)
			return
		}
		m.defaultEmbed(w, r)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if m.chatHandler != nil {
			m.chatHandler(w, r)
			return
		}
		m.defaultChat(w, r)
	})
	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockOllama) URL() string { return m.server.URL }

func (m *mockOllama) defaultTags(w http.ResponseWriter) {
	type model struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		Details struct {
			ParamSize string `json:"parameter_size"`
		} `json:"details"`
	}
	out := struct {
		Models []model `json:"models"`
	}{}
	for _, name := range m.models {
		mod := model{Name: name, Model: name}
		// Synthesise a plausible parameter_size so SelectModel can rank.
		if strings.Contains(name, "embed") {
			mod.Details.ParamSize = "137M"
		} else {
			mod.Details.ParamSize = "8B"
		}
		out.Models = append(out.Models, mod)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (m *mockOllama) defaultEmbed(w http.ResponseWriter, r *http.Request) {
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
		vecs[i] = m.deterministicEmbed(text)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Embeddings [][]float64 `json:"embeddings"`
	}{Embeddings: vecs})
}

// deterministicEmbed hashes the text into the embedding dim. Identical input
// always yields identical output, and the bag-of-words structure means texts
// sharing words land closer in cosine space — enough signal for tests.
func (m *mockOllama) deterministicEmbed(text string) []float64 {
	vec := make([]float64, m.dim)
	for _, word := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		sum := h.Sum32()
		// Distribute the hash across dimensions.
		for i := 0; i < m.dim; i++ {
			bit := float64((sum>>uint(i*4))&0xF) / 15.0
			vec[i] += bit
		}
	}
	if len(vec) > 0 && vec[0] == 0 {
		// Avoid all-zero vectors (cosine undefined).
		vec[0] = 1
	}
	return vec
}

func (m *mockOllama) defaultChat(w http.ResponseWriter, r *http.Request) {
	// Return a minimal valid extraction so semantic.Extract has work to do.
	resp := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": `{"n":[{"id":"mock_concept","l":"Mock Concept","t":"concept","f":"test.md"}],"e":[]}`,
				},
			},
		},
		"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// embedErrorHandler returns a handler that responds with the given status.
func embedErrorHandler(status int, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, msg, status)
	}
}

// vecToString formats a vector for diagnostic logs.
func vecToString(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprintf("%.3f", x)
	}
	return "[" + strings.Join(parts, " ") + "]"
}
