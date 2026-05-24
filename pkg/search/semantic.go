package search

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// EmbeddingStore holds dense vector embeddings for graph nodes, keyed by ID.
// All vectors share the same dimension and model.
type EmbeddingStore struct {
	Model   string               `json:"model"`
	Dim     int                  `json:"dim"`
	Vectors map[string][]float32 `json:"-"`
}

// embeddingFileMagic is the 4-byte header for embedding files.
var embeddingFileMagic = [4]byte{'G', 'F', 'Y', 'E'}

const embeddingFileVersion uint32 = 1

// SaveEmbeddings serialises the store to a compact binary file.
//
// Format:
//
//	[4]byte    magic ("GFYE")
//	uint32     version
//	uint32     dim
//	uint32     model name byte length
//	[N]byte    model name
//	uint32     node count
//	repeated:
//	  uint32   id byte length
//	  [N]byte  id
//	  dim*4    float32 little-endian vector
func SaveEmbeddings(path string, store *EmbeddingStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()

	w := f
	if _, err := w.Write(embeddingFileMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, embeddingFileVersion); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(store.Dim)); err != nil {
		return err
	}
	modelBytes := []byte(store.Model)
	if err := binary.Write(w, binary.LittleEndian, uint32(len(modelBytes))); err != nil {
		return err
	}
	if _, err := w.Write(modelBytes); err != nil {
		return err
	}

	// Sort IDs so the file is deterministic across runs.
	ids := make([]string, 0, len(store.Vectors))
	for id, v := range store.Vectors {
		if len(v) == store.Dim {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	if err := binary.Write(w, binary.LittleEndian, uint32(len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		idBytes := []byte(id)
		if err := binary.Write(w, binary.LittleEndian, uint32(len(idBytes))); err != nil {
			return err
		}
		if _, err := w.Write(idBytes); err != nil {
			return err
		}
		vec := store.Vectors[id]
		if err := binary.Write(w, binary.LittleEndian, vec); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadEmbeddings reads a binary embedding file written by SaveEmbeddings.
func LoadEmbeddings(path string) (*EmbeddingStore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return nil, err
	}
	if magic != embeddingFileMagic {
		return nil, errors.New("embeddings: bad magic")
	}
	var version, dim, modelLen uint32
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != embeddingFileVersion {
		return nil, fmt.Errorf("embeddings: unsupported version %d", version)
	}
	if err := binary.Read(f, binary.LittleEndian, &dim); err != nil {
		return nil, err
	}
	if err := binary.Read(f, binary.LittleEndian, &modelLen); err != nil {
		return nil, err
	}
	modelBytes := make([]byte, modelLen)
	if _, err := io.ReadFull(f, modelBytes); err != nil {
		return nil, err
	}

	var nodeCount uint32
	if err := binary.Read(f, binary.LittleEndian, &nodeCount); err != nil {
		return nil, err
	}

	store := &EmbeddingStore{
		Model:   string(modelBytes),
		Dim:     int(dim),
		Vectors: make(map[string][]float32, nodeCount),
	}

	for i := uint32(0); i < nodeCount; i++ {
		var idLen uint32
		if err := binary.Read(f, binary.LittleEndian, &idLen); err != nil {
			return nil, err
		}
		idBytes := make([]byte, idLen)
		if _, err := io.ReadFull(f, idBytes); err != nil {
			return nil, err
		}
		vec := make([]float32, dim)
		if err := binary.Read(f, binary.LittleEndian, vec); err != nil {
			return nil, err
		}
		store.Vectors[string(idBytes)] = vec
	}
	return store, nil
}

// Cosine returns the cosine similarity of two vectors of equal length.
// Returns 0 if either is zero-length or zero-norm.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// RankByEmbedding returns the top-N nodes most similar to the query vector.
// Cosine score is exposed via Result.Score (range [-1, 1], typically [0, 1]).
func RankByEmbedding(store *EmbeddingStore, query []float32, topN int) []Result {
	if store == nil || len(query) == 0 {
		return nil
	}
	results := make([]Result, 0, len(store.Vectors))
	for id, vec := range store.Vectors {
		score := Cosine(query, vec)
		if score <= 0 {
			continue
		}
		results = append(results, Result{ID: id, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if topN > 0 && len(results) > topN {
		results = results[:topN]
	}
	return results
}
