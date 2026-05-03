// Package detect handles file discovery, type classification, and corpus health checks.
package detect

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/qiangli/gfy/internal/types"
)

// isSensitive returns true if the filename matches secret patterns.
func isSensitive(name string) bool {
	for _, p := range sensitivePatterns {
		if p.MatchString(name) {
			return true
		}
	}
	return false
}

// looksLikePaper checks if a text file reads like an academic paper.
func looksLikePaper(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 3000)
	n, _ := f.Read(buf)
	text := string(buf[:n])

	hits := 0
	for _, p := range paperSignals {
		if p.MatchString(text) {
			hits++
		}
	}
	return hits >= paperSignalThreshold
}

// ClassifyFile determines the FileType for a given path.
// Returns empty string if the file type is unrecognized.
func ClassifyFile(path string) types.FileType {
	base := filepath.Base(path)
	// Compound extension check: .blade.php
	if strings.HasSuffix(strings.ToLower(base), ".blade.php") {
		return types.Code
	}

	ext := strings.ToLower(filepath.Ext(path))

	if CodeExtensions[ext] {
		return types.Code
	}
	if PaperExtensions[ext] {
		// PDFs inside Xcode asset catalogs are vector icons, not papers.
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			pext := filepath.Ext(part)
			if assetDirMarkers[pext] {
				return ""
			}
		}
		return types.Paper
	}
	if ImageExtensions[ext] {
		return types.Image
	}
	if DocExtensions[ext] {
		if looksLikePaper(path) {
			return types.Paper
		}
		return types.Document
	}
	if OfficeExtensions[ext] {
		return types.Document
	}
	if VideoExtensions[ext] {
		return types.Video
	}
	return ""
}

// isNoiseDir returns true if the directory name should be skipped.
func isNoiseDir(name string) bool {
	if SkipDirs[name] {
		return true
	}
	if strings.HasSuffix(name, "_venv") || strings.HasSuffix(name, "_env") {
		return true
	}
	if strings.HasSuffix(name, ".egg-info") {
		return true
	}
	return false
}

// isSubmodule returns true if a directory is a git submodule.
// Submodules have a .git file (not directory) containing "gitdir:".
func isSubmodule(dir string) bool {
	gitPath := filepath.Join(dir, ".git")
	info, err := os.Lstat(gitPath)
	if err != nil || info.IsDir() {
		return false
	}
	// It's a file — read first line to confirm it's a submodule marker.
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), "gitdir:")
}

// ignorePattern represents a single ignore or include rule.
type ignorePattern struct {
	anchor  string
	pattern string
	negate  bool   // true for !pattern (re-include)
	source  string // "gitignore" or "graphifyignore"
}

// ignoreStack tracks ignore patterns encountered during directory traversal.
// Patterns from parent directories apply to all descendants. When entering a
// directory that contains .gitignore or .gfyignore, those patterns are
// pushed onto the stack. .gfyignore patterns are loaded after .gitignore
// at each level so they can override with !pattern negations.
type ignoreStack struct {
	root     string
	patterns []ignorePattern
}

func newIgnoreStack(root string) *ignoreStack {
	s := &ignoreStack{root: root}
	// Load ancestor patterns (above root, up to .git root).
	s.loadAncestors(root)
	// Load patterns in root itself.
	s.loadDir(root)
	return s
}

// loadAncestors loads .gitignore/.gfyignore from directories above root,
// up to (and including) the nearest .git root.
//
// Only patterns from the root's OWN .git repository are loaded. Ancestor
// gitignores above a different .git boundary are skipped because they may
// contain patterns (like "priorart/") that would exclude the entire scan
// root when it lives inside another project's ignored directory.
func (s *ignoreStack) loadAncestors(root string) {
	// Only load ancestors if root itself is inside a git repo.
	// If root has its own .git, ancestor patterns don't apply.
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return // root is a git repo root; no ancestors to load
	}

	var ancestors []string
	current := filepath.Dir(root)
	for {
		ancestors = append(ancestors, current)
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// Load from outermost ancestor to innermost so inner patterns override.
	for i := len(ancestors) - 1; i >= 0; i-- {
		s.loadDir(ancestors[i])
	}
}

// loadDir reads .gitignore and .gfyignore from a single directory.
// .gitignore is loaded first, then .gfyignore so it can override.
func (s *ignoreStack) loadDir(dir string) {
	for _, name := range []string{".gitignore", ".gfyignore"} {
		source := strings.TrimPrefix(name, ".")
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			negate := false
			if strings.HasPrefix(line, "!") {
				// Only .gfyignore supports negation (re-include).
				if source == "gitignore" {
					continue
				}
				negate = true
				line = line[1:]
			}
			s.patterns = append(s.patterns, ignorePattern{
				anchor:  dir,
				pattern: line,
				negate:  negate,
				source:  source,
			})
		}
	}
}

// isIgnored checks if a path is excluded by the accumulated ignore patterns.
// Patterns are evaluated in order. A later negation pattern (!pattern)
// from .gfyignore can re-include a path excluded by .gitignore.
func (s *ignoreStack) isIgnored(path string) bool {
	ignored := false
	basename := filepath.Base(path)
	for _, ip := range s.patterns {
		p := strings.Trim(ip.pattern, "/")
		if p == "" {
			continue
		}
		matched := false
		// Match relative to the pattern's anchor directory.
		if rel, err := filepath.Rel(ip.anchor, path); err == nil {
			rel = filepath.ToSlash(rel)
			if matchPattern(rel, basename, p) {
				matched = true
			}
		}
		// Also try relative to scan root.
		if !matched && ip.anchor != s.root {
			if rel, err := filepath.Rel(s.root, path); err == nil {
				rel = filepath.ToSlash(rel)
				if matchPattern(rel, basename, p) {
					matched = true
				}
			}
		}
		if matched {
			ignored = !ip.negate
		}
	}
	return ignored
}

// count returns the total number of loaded patterns.
func (s *ignoreStack) count() int {
	return len(s.patterns)
}

// matchPattern checks if a relative path or filename matches a glob pattern.
func matchPattern(rel, basename, pattern string) bool {
	if matched, _ := filepath.Match(pattern, rel); matched {
		return true
	}
	if matched, _ := filepath.Match(pattern, basename); matched {
		return true
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if matched, _ := filepath.Match(pattern, part); matched {
			return true
		}
		prefix := strings.Join(parts[:i+1], "/")
		if matched, _ := filepath.Match(pattern, prefix); matched {
			return true
		}
	}
	return false
}

// countWords returns the approximate word count for a text file.
func countWords(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	if !utf8.Valid(data) {
		return 0
	}
	return len(strings.Fields(string(data)))
}

// Detect walks a directory tree and classifies all files.
// It respects .gitignore and .gfyignore files in every directory,
// not just the root. .gfyignore has higher priority and can use
// !pattern to re-include files excluded by .gitignore.
func Detect(root string, followSymlinks bool) *types.DetectionResult {
	root, _ = filepath.Abs(root)

	result := &types.DetectionResult{
		Files: map[types.FileType][]string{
			types.Code:     {},
			types.Document: {},
			types.Paper:    {},
			types.Image:    {},
			types.Video:    {},
		},
	}

	ignore := newIgnoreStack(root)

	seen := make(map[string]bool)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			name := info.Name()
			if name != filepath.Base(root) {
				if strings.HasPrefix(name, ".") || isNoiseDir(name) {
					return filepath.SkipDir
				}
				if ignore.isIgnored(path) {
					return filepath.SkipDir
				}
				// Skip git submodules — they have a .git file (not directory)
				// containing "gitdir:" pointing to the parent's .git/modules/.
				if isSubmodule(path) {
					return filepath.SkipDir
				}
				// Load .gitignore/.gfyignore from this subdirectory
				// so its patterns apply to descendants.
				ignore.loadDir(path)
			}
			return nil
		}

		// Skip hidden files, lock files.
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		if SkipFiles[info.Name()] {
			return nil
		}
		if seen[path] {
			return nil
		}
		seen[path] = true

		if ignore.isIgnored(path) {
			return nil
		}
		if isSensitive(info.Name()) {
			result.SkippedSensitive = append(result.SkippedSensitive, path)
			return nil
		}

		ftype := ClassifyFile(path)
		if ftype == "" {
			return nil
		}

		result.Files[ftype] = append(result.Files[ftype], path)
		if ftype != types.Video {
			result.TotalWords += countWords(path)
		}
		return nil
	})
	if err != nil {
		// Non-fatal: return what we found.
	}

	for _, files := range result.Files {
		result.TotalFiles += len(files)
	}

	result.GraphifyIgnoreCount = ignore.count()
	result.NeedsGraph = result.TotalWords >= CorpusWarnThreshold

	if !result.NeedsGraph {
		result.Warning = fmt.Sprintf(
			"Corpus is ~%d words - fits in a single context window. You may not need a graph.",
			result.TotalWords,
		)
	} else if result.TotalWords >= CorpusUpperThreshold || result.TotalFiles >= FileCountUpper {
		result.Warning = fmt.Sprintf(
			"Large corpus: %d files · ~%d words. Extraction may be slow.",
			result.TotalFiles, result.TotalWords,
		)
	}

	return result
}
