package semantic

import (
	"fmt"
	"strings"

	"github.com/qiangli/gfy/pkg/graph"
	"github.com/qiangli/gfy/pkg/search"
)

// embedBatchSize controls how many node texts are sent per Ollama call.
// Small enough to keep memory bounded for large graphs; large enough to
// amortise HTTP overhead.
const embedBatchSize = 32

// EmbedGraph embeds every node in the graph and returns a populated store.
// Node text is built from label + comment + source file (truncateTextd) — enough
// for semantic queries to land on the right nodes without sending all of
// each function's body to the embedding model.
//
// Returns nil store and no error if model is empty (caller has disabled embeddings).
func EmbedGraph(g *graph.Graph, client *Client, model string, progress func(done, total int)) (*search.EmbeddingStore, error) {
	if model == "" || client == nil {
		return nil, nil
	}

	ids := g.Nodes()
	if len(ids) == 0 {
		return &search.EmbeddingStore{Model: model, Vectors: map[string][]float32{}}, nil
	}

	texts := make([]string, len(ids))
	for i, id := range ids {
		texts[i] = nodeEmbeddingText(g, id)
	}

	store := &search.EmbeddingStore{
		Model:   model,
		Vectors: make(map[string][]float32, len(ids)),
	}

	for start := 0; start < len(ids); start += embedBatchSize {
		end := min(start+embedBatchSize, len(ids))
		vecs, err := client.EmbedBatch(model, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed batch %d-%d: %w", start, end, err)
		}
		for k, vec := range vecs {
			if len(vec) == 0 {
				continue
			}
			if store.Dim == 0 {
				store.Dim = len(vec)
			}
			if len(vec) != store.Dim {
				continue
			}
			store.Vectors[ids[start+k]] = vec
		}
		if progress != nil {
			progress(end, len(ids))
		}
	}
	return store, nil
}

// nodeEmbeddingText assembles the text representation of a node for embedding.
// Order matters: most-discriminating signal first so embeddings degrade
// gracefully if the model truncateTexts.
func nodeEmbeddingText(g *graph.Graph, id string) string {
	attrs := g.NodeAttrs(id)
	if attrs == nil {
		return id
	}

	var parts []string
	if label, _ := attrs["label"].(string); label != "" {
		parts = append(parts, label)
	}
	if ft, _ := attrs["file_type"].(string); ft != "" {
		parts = append(parts, "type: "+ft)
	}
	if src, _ := attrs["source_file"].(string); src != "" {
		parts = append(parts, "file: "+src)
	}
	if c, _ := attrs["comment"].(string); c != "" {
		parts = append(parts, "doc: "+truncateText(c, 800))
	}
	if content, _ := attrs["content"].(string); content != "" {
		parts = append(parts, truncateText(content, 2000))
	}
	if tags := attrTags(attrs); len(tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(tags, ", "))
	}
	return strings.Join(parts, "\n")
}

func attrTags(attrs map[string]any) []string {
	v, ok := attrs["tags"]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
