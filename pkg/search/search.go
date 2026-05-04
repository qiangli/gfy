// Package search provides graph node search with fuzzy matching,
// diacritics normalization, and degree-weighted ranking.
package search

import (
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/qiangli/gfy/pkg/graph"
)

// Result holds a search match.
type Result struct {
	ID    string
	Label string
	Score float64
}

// ScoreNodes searches graph nodes by query string and returns ranked results.
//
// Scoring:
//   - Exact label match: +10
//   - Label starts with a term: +5
//   - Label contains a term: +2
//   - Source file contains a term: +1
//   - Fuzzy match (Levenshtein ≤ 2 on any word): +1
//   - Tiebreaker: degree / maxDegree (0–1 bonus for more-connected nodes)
func ScoreNodes(g *graph.Graph, query string) []Result {
	terms := strings.Fields(NormalizeLabel(query))
	if len(terms) == 0 {
		return nil
	}

	// Precompute max degree for tiebreaker normalization.
	maxDeg := 1
	for _, id := range g.Nodes() {
		if d := g.Degree(id); d > maxDeg {
			maxDeg = d
		}
	}

	var results []Result
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		label := attrStr(attrs, "label")
		normLabel := NormalizeLabel(label)
		sourceFile := strings.ToLower(attrStr(attrs, "source_file"))
		// Strip parens and dots for matching: ".Start()" → "start"
		cleanLabel := strings.TrimRight(normLabel, "()")
		cleanLabel = strings.TrimLeft(cleanLabel, ".")

		score := 0.0
		for _, t := range terms {
			// Exact match (full label equals term).
			if cleanLabel == t {
				score += 10
			} else if strings.HasPrefix(cleanLabel, t) {
				score += 5
			} else if strings.Contains(normLabel, t) {
				score += 2
			} else {
				// Fuzzy: check each word in the label.
				matched := false
				for _, word := range strings.FieldsFunc(cleanLabel, func(r rune) bool {
					return r == '_' || r == '.' || r == '/' || r == '-'
				}) {
					if levenshtein(word, t) <= 2 {
						score += 1
						matched = true
						break
					}
				}
				// Also try fuzzy against the whole clean label for short labels.
				if !matched && len(cleanLabel) <= 20 && levenshtein(cleanLabel, t) <= 2 {
					score += 1
				}
			}

			if strings.Contains(sourceFile, t) {
				score += 1
			}
		}

		if score > 0 {
			// Degree tiebreaker: +0 to +1 based on relative connectivity.
			deg := float64(g.Degree(id)) / float64(maxDeg)
			score += deg
			results = append(results, Result{ID: id, Label: label, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	return results
}

// FindNode looks up a node by label or ID with diacritics-insensitive fallback.
func FindNode(g *graph.Graph, label string) string {
	// Exact ID match.
	if g.HasNode(label) {
		return label
	}
	// Case-insensitive label match.
	lower := strings.ToLower(label)
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		if strings.ToLower(attrStr(attrs, "label")) == lower {
			return id
		}
	}
	// Diacritics-insensitive fallback.
	normQuery := NormalizeLabel(label)
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		if NormalizeLabel(attrStr(attrs, "label")) == normQuery {
			return id
		}
	}
	// Fuzzy fallback: find closest match.
	bestID := ""
	bestDist := 999
	for _, id := range g.Nodes() {
		attrs := g.NodeAttrs(id)
		nl := NormalizeLabel(attrStr(attrs, "label"))
		nl = strings.TrimRight(nl, "()")
		nl = strings.TrimLeft(nl, ".")
		d := levenshtein(nl, normQuery)
		if d < bestDist && d <= 2 {
			bestDist = d
			bestID = id
		}
	}
	return bestID
}

// StripDiacritics removes combining marks (accents) from Unicode text.
func StripDiacritics(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range norm.NFKD.String(s) {
		if !unicode.In(r, unicode.Mn) { // Mn = Mark, Nonspacing (combining marks)
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeLabel strips diacritics and lowercases.
func NormalizeLabel(s string) string {
	return strings.ToLower(StripDiacritics(s))
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Single-row DP.
	prev := make([]int, lb+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr := make([]int, lb+1)
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			curr[j] = min(ins, min(del, sub))
		}
		prev = curr
	}
	return prev[lb]
}

func attrStr(attrs map[string]any, key string) string {
	if v, ok := attrs[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
