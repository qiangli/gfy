package semantic

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/qiangli/gfy/internal/types"
)

// stopWords contains common English words filtered during tokenization.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"has": true, "have": true, "been": true, "will": true, "with": true,
	"this": true, "that": true, "from": true, "they": true, "most": true,
	"said": true, "each": true, "which": true, "their": true, "what": true,
	"more": true, "when": true, "make": true, "like": true, "than": true,
	"into": true, "just": true, "over": true, "such": true, "take": true,
	"other": true, "some": true, "could": true, "them": true, "then": true,
	"does": true, "also": true, "after": true, "would": true, "about": true,
	"these": true, "only": true, "should": true, "very": true, "where": true,
}

// tokenize splits text into lowercase, stemmed word tokens, splitting on
// whitespace, punctuation, underscores, and camelCase boundaries. Filters
// stop words and tokens shorter than 3 characters.
func tokenize(text string) []string {
	// First split camelCase, then split on non-alpha boundaries.
	expanded := splitCamelCase(text)
	words := splitOnBoundaries(expanded)

	var tokens []string
	for _, w := range words {
		w = strings.ToLower(w)
		if len(w) < 3 {
			continue
		}
		if stopWords[w] {
			continue
		}
		w = simpleStem(w)
		if len(w) < 3 {
			continue
		}
		tokens = append(tokens, w)
	}
	return tokens
}

// simpleStem applies basic English suffix stripping to normalize word forms.
// Focuses on high-confidence suffixes that rarely produce false stems.
// Not a full Porter stemmer — just enough to unify plurals, gerunds, and
// common nominalizations.
func simpleStem(w string) string {
	// Order matters: strip longer suffixes first.
	switch {
	case strings.HasSuffix(w, "ation") && len(w) > 7:
		return w[:len(w)-5]
	case strings.HasSuffix(w, "ment") && len(w) > 7:
		return w[:len(w)-4]
	case strings.HasSuffix(w, "ness") && len(w) > 7:
		return w[:len(w)-4]
	case strings.HasSuffix(w, "ting") && len(w) > 5:
		return w[:len(w)-4]
	case strings.HasSuffix(w, "ing") && len(w) > 5:
		return w[:len(w)-3]
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ed") && len(w) > 4:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "es") && len(w) > 4:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && len(w) > 4 && !strings.HasSuffix(w, "ss"):
		return w[:len(w)-1]
	}
	return w
}

// splitCamelCase inserts spaces at camelCase boundaries.
// "AuthService" -> "Auth Service", "validateToken" -> "validate Token"
func splitCamelCase(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 10)
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			// Insert space before uppercase if preceded by lowercase,
			// or if preceded by uppercase followed by lowercase (e.g., "HTMLParser" -> "HTML Parser").
			if unicode.IsLower(prev) {
				b.WriteRune(' ')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// splitOnBoundaries splits text on whitespace, punctuation, underscores, hyphens, dots.
func splitOnBoundaries(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// buildNodeDocument creates a text document from a node's rich fields.
// For code nodes, combines label + comment + log messages + throw messages.
// For semantic nodes, uses just the label.
func buildNodeDocument(n types.Node) string {
	var parts []string
	parts = append(parts, n.Label)
	if n.Comment != "" {
		parts = append(parts, n.Comment)
	}
	for _, msg := range n.LogMessages {
		parts = append(parts, msg)
	}
	for _, msg := range n.ThrowMessages {
		parts = append(parts, msg)
	}
	return strings.Join(parts, " ")
}

// tfidfIndex holds precomputed TF-IDF vectors for a corpus of documents.
type tfidfIndex struct {
	idf     map[string]float64   // inverse document frequency per term
	vectors []map[string]float64 // TF-IDF sparse vector per document
}

// newTFIDF builds a TF-IDF index from tokenized documents.
func newTFIDF(docs [][]string) *tfidfIndex {
	n := len(docs)
	if n == 0 {
		return &tfidfIndex{
			idf:     map[string]float64{},
			vectors: nil,
		}
	}

	// Compute document frequency for each term.
	df := make(map[string]int)
	for _, doc := range docs {
		seen := make(map[string]bool)
		for _, term := range doc {
			if !seen[term] {
				df[term]++
				seen[term] = true
			}
		}
	}

	// Compute IDF: log(N / (1 + df)).
	idf := make(map[string]float64, len(df))
	for term, count := range df {
		idf[term] = math.Log(float64(n) / float64(1+count))
	}

	// Compute TF-IDF vector per document.
	vectors := make([]map[string]float64, n)
	for i, doc := range docs {
		if len(doc) == 0 {
			vectors[i] = map[string]float64{}
			continue
		}
		// Term frequency: count / total terms.
		tf := make(map[string]int)
		for _, term := range doc {
			tf[term]++
		}
		vec := make(map[string]float64, len(tf))
		docLen := float64(len(doc))
		for term, count := range tf {
			vec[term] = (float64(count) / docLen) * idf[term]
		}
		vectors[i] = vec
	}

	return &tfidfIndex{idf: idf, vectors: vectors}
}

// cosineSimilarity computes the cosine similarity between two sparse vectors.
// Returns 0.0 for zero vectors, otherwise a value in [0.0, 1.0].
func cosineSimilarity(a, b map[string]float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// Iterate over the smaller map for efficiency.
	if len(a) > len(b) {
		a, b = b, a
	}

	var dot, magA, magB float64
	for term, va := range a {
		if vb, ok := b[term]; ok {
			dot += va * vb
		}
		magA += va * va
	}
	for _, vb := range b {
		magB += vb * vb
	}

	denom := math.Sqrt(magA) * math.Sqrt(magB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// topMatch holds a similarity match result.
type topMatch struct {
	index      int
	similarity float64
}

// findTopMatches returns the top-k AST node indices most similar to the query vector.
// Only includes matches above the given threshold.
func findTopMatches(query map[string]float64, astVectors []map[string]float64, astIndices []int, k int, threshold float64) []topMatch {
	var matches []topMatch
	for i, astIdx := range astIndices {
		sim := cosineSimilarity(query, astVectors[i])
		if sim >= threshold {
			matches = append(matches, topMatch{index: astIdx, similarity: sim})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].similarity > matches[j].similarity
	})

	if len(matches) > k {
		matches = matches[:k]
	}
	return matches
}
