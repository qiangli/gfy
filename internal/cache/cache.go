// Package cache provides SHA256-based caching for extraction results.
package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/gfy/internal/types"
)

// FileHash computes a SHA256 hex digest of a file's contents combined with its
// relative path (for portability across machines).
func FileHash(filePath, root string) (string, error) {
	return fileHashStream(filePath, root)
}

// fileHashStream computes the hash by streaming the file instead of reading
// it entirely into memory. For .md files, falls back to the buffered path
// to strip YAML frontmatter.
func fileHashStream(filePath, root string) (string, error) {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		rel = filePath
	}
	rel = filepath.ToSlash(rel)

	// .md files need frontmatter stripping — fall back to buffered read.
	if strings.HasSuffix(strings.ToLower(filePath), ".md") {
		return fileHashBuffered(filePath, rel)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	h.Write([]byte(rel))
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func fileHashBuffered(filePath, rel string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	data = stripFrontmatter(data)

	h := sha256.New()
	h.Write([]byte(rel))
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// stripFrontmatter removes YAML frontmatter (--- delimited) from markdown.
func stripFrontmatter(data []byte) []byte {
	s := string(data)
	if !strings.HasPrefix(s, "---") {
		return data
	}
	end := strings.Index(s[3:], "\n---")
	if end < 0 {
		return data
	}
	return []byte(s[end+7:]) // skip past closing ---\n
}

// cacheEntry is the on-disk format for cached extraction results.
type cacheEntry struct {
	Hash   string                 `json:"hash"`
	Result types.ExtractionResult `json:"result"`
}

// cachePath returns the path for a cached entry.
func cachePath(root, kind, hash string) string {
	return filepath.Join(root, ".gfy-out", "cache", kind, hash+".json")
}

// Load retrieves a cached extraction result if the file hash matches.
// Returns nil if not cached or hash mismatch.
func Load(filePath, root, kind string) *types.ExtractionResult {
	hash, err := FileHash(filePath, root)
	if err != nil {
		return nil
	}
	path := cachePath(root, kind, hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if entry.Hash != hash {
		return nil
	}
	return &entry.Result
}

// Save writes an extraction result to the cache.
func Save(filePath string, result *types.ExtractionResult, root, kind string) error {
	hash, err := FileHash(filePath, root)
	if err != nil {
		return err
	}
	// Use a pointer to avoid copying the ExtractionResult.
	entry := &cacheEntryRef{Hash: hash, Result: result}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := cachePath(root, kind, hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Atomic write: write to temp then rename.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// cacheEntryRef is like cacheEntry but holds a pointer to avoid copying.
type cacheEntryRef struct {
	Hash   string                  `json:"hash"`
	Result *types.ExtractionResult `json:"result"`
}

// Clear removes all cached entries.
func Clear(root string) error {
	dir := filepath.Join(root, ".gfy-out", "cache")
	return os.RemoveAll(dir)
}
