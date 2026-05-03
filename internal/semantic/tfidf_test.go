package semantic

import (
	"math"
	"testing"

	"github.com/qiangli/gfy/internal/types"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "camelCase splitting",
			input: "validateToken",
			want:  []string{"validate", "token"},
		},
		{
			name:  "PascalCase splitting",
			input: "AuthService",
			want:  []string{"auth", "service"},
		},
		{
			name:  "snake_case splitting",
			input: "auth_service",
			want:  []string{"auth", "service"},
		},
		{
			name:  "acronym handling",
			input: "HTMLParser",
			want:  []string{"html", "parser"},
		},
		{
			name:  "stop words filtered",
			input: "the authentication for this service",
			want:  []string{"authentic", "service"},
		},
		{
			name:  "short tokens filtered",
			input: "a go db authentication",
			want:  []string{"authentic"},
		},
		{
			name:  "mixed punctuation",
			input: "auth.service-token_validate",
			want:  []string{"auth", "service", "token", "validate"},
		},
		{
			name:  "stemming unifies plurals",
			input: "credentials credential",
			want:  []string{"credential", "credential"},
		},
		{
			name:  "stemming -ation suffix",
			input: "authentication validation",
			want:  []string{"authentic", "valid"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"AuthService", "Auth Service"},
		{"validateToken", "validate Token"},
		{"HTMLParser", "HTML Parser"},
		{"simpleword", "simpleword"},
		{"ABCDef", "ABC Def"},
	}

	for _, tt := range tests {
		got := splitCamelCase(tt.input)
		if got != tt.want {
			t.Errorf("splitCamelCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildNodeDocument(t *testing.T) {
	n := types.Node{
		Label:         "validateToken",
		Comment:       "checks authentication credentials",
		LogMessages:   []string{"token validated for user"},
		ThrowMessages: []string{"authentication failed"},
	}
	got := buildNodeDocument(n)
	want := "validateToken checks authentication credentials token validated for user authentication failed"
	if got != want {
		t.Errorf("buildNodeDocument() = %q, want %q", got, want)
	}
}

func TestBuildNodeDocumentLabelOnly(t *testing.T) {
	n := types.Node{Label: "SimpleFunc"}
	got := buildNodeDocument(n)
	if got != "SimpleFunc" {
		t.Errorf("buildNodeDocument() = %q, want %q", got, "SimpleFunc")
	}
}

func TestNewTFIDF(t *testing.T) {
	docs := [][]string{
		{"auth", "token", "validate"},
		{"auth", "session", "create"},
		{"database", "query", "execute"},
	}

	idx := newTFIDF(docs)

	// "auth" appears in 2/3 docs, should have lower IDF than "database" (1/3 docs).
	if idx.idf["auth"] >= idx.idf["database"] {
		t.Errorf("IDF(auth)=%f should be < IDF(database)=%f", idx.idf["auth"], idx.idf["database"])
	}

	// Each document should have a vector.
	if len(idx.vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(idx.vectors))
	}

	// Terms unique to one doc should have nonzero TF-IDF weight.
	// "token" appears only in doc 0, so its IDF > 0 and thus TF-IDF > 0.
	for _, term := range []string{"token", "validate"} {
		if idx.vectors[0][term] <= 0 {
			t.Errorf("vector[0][%q] = %f, want > 0", term, idx.vectors[0][term])
		}
	}

	// "auth" appears in 2/3 docs → IDF = log(3/3) = 0, so TF-IDF = 0.
	// This is expected behavior: ubiquitous terms get zero weight.
	if idx.vectors[0]["auth"] != 0 {
		t.Errorf("vector[0][\"auth\"] = %f, want 0 (appears in 2/3 docs)", idx.vectors[0]["auth"])
	}
}

func TestNewTFIDFEmpty(t *testing.T) {
	idx := newTFIDF(nil)
	if idx.vectors != nil {
		t.Error("expected nil vectors for empty input")
	}
}

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors.
	a := map[string]float64{"auth": 1.0, "token": 1.0}
	if sim := cosineSimilarity(a, a); math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("identical vectors: cosine = %f, want 1.0", sim)
	}

	// Orthogonal vectors.
	b := map[string]float64{"database": 1.0, "query": 1.0}
	if sim := cosineSimilarity(a, b); sim != 0 {
		t.Errorf("orthogonal vectors: cosine = %f, want 0.0", sim)
	}

	// Partial overlap.
	c := map[string]float64{"auth": 1.0, "session": 1.0}
	sim := cosineSimilarity(a, c)
	if sim <= 0 || sim >= 1.0 {
		t.Errorf("partial overlap: cosine = %f, want (0, 1)", sim)
	}

	// Empty vectors.
	if sim := cosineSimilarity(map[string]float64{}, a); sim != 0 {
		t.Errorf("empty vector: cosine = %f, want 0", sim)
	}
}

func TestFindTopMatches(t *testing.T) {
	// Build a small corpus.
	docs := [][]string{
		{"auth", "token", "validate", "credential"},       // 0: auth validator
		{"auth", "session", "create", "user"},             // 1: session creator
		{"database", "query", "execute", "transaction"},   // 2: db executor
		{"auth", "error", "handle", "credential", "fail"}, // 3: auth error handler
	}
	idx := newTFIDF(docs)

	// Query similar to auth concepts.
	query := idx.vectors[0]
	astIndices := []int{1, 2, 3}
	astVectors := []map[string]float64{idx.vectors[1], idx.vectors[2], idx.vectors[3]}

	matches := findTopMatches(query, astVectors, astIndices, 2, 0.0)

	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}
	if len(matches) > 2 {
		t.Fatalf("expected at most 2 matches, got %d", len(matches))
	}

	// Auth-related docs should rank higher than database doc.
	for _, m := range matches {
		if m.index == 2 {
			t.Error("database doc should not be in top matches for auth query")
		}
	}

	// Matches should be sorted by similarity descending.
	if len(matches) == 2 && matches[0].similarity < matches[1].similarity {
		t.Error("matches should be sorted by similarity descending")
	}
}
