package search

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestCosine(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{"empty", []float32{}, []float32{}, 0.0},
		{"different lengths", []float32{1, 0}, []float32{1, 0, 0}, 0.0},
		{"zero norm", []float32{0, 0, 0}, []float32{1, 0, 0}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Cosine(tt.a, tt.b)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("Cosine(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestRankByEmbedding(t *testing.T) {
	store := &EmbeddingStore{
		Model: "test",
		Dim:   3,
		Vectors: map[string][]float32{
			"exact":  {1, 0, 0},
			"close":  {0.9, 0.1, 0},
			"far":    {0, 1, 0},
			"negate": {-1, 0, 0}, // cosine = -1, excluded
		},
	}
	results := RankByEmbedding(store, []float32{1, 0, 0}, 5)
	// Orthogonal (cos=0) and opposite (cos=-1) vectors are filtered.
	if len(results) != 2 {
		t.Fatalf("expected 2 positive matches (exact, close), got %d: %+v", len(results), results)
	}
	if results[0].ID != "exact" {
		t.Errorf("expected exact match first, got %s", results[0].ID)
	}
	if results[1].ID != "close" {
		t.Errorf("expected close match second, got %s", results[1].ID)
	}
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted: [%d]=%v > [%d]=%v", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

func TestSaveLoadEmbeddings_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.bin")

	original := &EmbeddingStore{
		Model: "nomic-embed-text",
		Dim:   4,
		Vectors: map[string][]float32{
			"node_a": {0.1, 0.2, 0.3, 0.4},
			"node_b": {0.5, -0.5, 1.0, 0.0},
			"hello":  {1, 2, 3, 4},
		},
	}
	if err := SaveEmbeddings(path, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadEmbeddings(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Model != original.Model {
		t.Errorf("model mismatch: got %q want %q", loaded.Model, original.Model)
	}
	if loaded.Dim != original.Dim {
		t.Errorf("dim mismatch: got %d want %d", loaded.Dim, original.Dim)
	}
	if len(loaded.Vectors) != len(original.Vectors) {
		t.Errorf("vector count: got %d want %d", len(loaded.Vectors), len(original.Vectors))
	}
	for id, want := range original.Vectors {
		got, ok := loaded.Vectors[id]
		if !ok {
			t.Errorf("missing vector for %s", id)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s: dim %d, want %d", id, len(got), len(want))
			continue
		}
		for i := range got {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("%s[%d]: got %v want %v", id, i, got[i], want[i])
			}
		}
	}
}

func TestLoadEmbeddings_BadMagic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(path, []byte("NOPE0000"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEmbeddings(path); err == nil {
		t.Fatal("expected error on bad magic")
	}
}

func TestLoadEmbeddings_Missing(t *testing.T) {
	if _, err := LoadEmbeddings("/nonexistent/path"); err == nil {
		t.Fatal("expected error on missing file")
	}
}
