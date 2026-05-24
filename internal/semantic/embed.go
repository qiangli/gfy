package semantic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// embedModelPreference lists embedding models ranked by general capability.
// Picked in order; first locally available match wins.
var embedModelPreference = []string{
	"nomic-embed-text",
	"mxbai-embed-large",
	"snowflake-arctic-embed",
	"all-minilm",
	"bge-large",
	"bge-small",
}

// SelectEmbedModel picks the first available embedding model from the local
// Ollama installation. Returns "" if none of the preferred models are present.
func SelectEmbedModel(models []ModelInfo) string {
	for _, family := range embedModelPreference {
		for _, m := range models {
			base, _, _ := strings.Cut(m.Name, ":")
			if base == family {
				return m.Name
			}
		}
	}
	return ""
}

// embedRequest is the Ollama /api/embed payload.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse is the Ollama /api/embed reply.
type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

// Embed produces a single embedding vector for the given text.
func (c *Client) Embed(model, text string) ([]float32, error) {
	vecs, err := c.EmbedBatch(model, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed: empty response")
	}
	return vecs[0], nil
}

// EmbedBatch produces embedding vectors for a batch of texts.
// Empty strings in the batch yield zero-length vectors at the corresponding index.
func (c *Client) EmbedBatch(model string, texts []string) ([][]float32, error) {
	if model == "" {
		return nil, fmt.Errorf("embed: model is empty")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	// Filter empties; track original indices so we can re-expand.
	nonEmpty := make([]string, 0, len(texts))
	idxMap := make([]int, 0, len(texts))
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, t)
		idxMap = append(idxMap, i)
	}
	if len(nonEmpty) == 0 {
		out := make([][]float32, len(texts))
		return out, nil
	}

	body, err := json.Marshal(embedRequest{Model: model, Input: nonEmpty})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/api/embed"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: status %d", resp.StatusCode)
	}

	var er embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if er.Error != "" {
		return nil, fmt.Errorf("embed: %s", er.Error)
	}
	if len(er.Embeddings) != len(nonEmpty) {
		return nil, fmt.Errorf("embed: got %d vectors, expected %d", len(er.Embeddings), len(nonEmpty))
	}

	out := make([][]float32, len(texts))
	for k, vec := range er.Embeddings {
		f32 := make([]float32, len(vec))
		for i, v := range vec {
			f32[i] = float32(v)
		}
		out[idxMap[k]] = f32
	}
	return out, nil
}
