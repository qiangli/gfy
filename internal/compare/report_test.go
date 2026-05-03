package compare

import (
	"strings"
	"testing"
)

func TestInterpretResults_Identical_ZeroChanges(t *testing.T) {
	// When there are zero structural changes, report "identical" regardless
	// of composite score (which can be < 1.0 due to non-deterministic Louvain).
	tests := []struct {
		name      string
		composite float64
	}{
		{"exact 1.0", 1.0},
		{"0.9996 with no changes", 0.9996},
		{"0.99 with no changes", 0.99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{
				Summary: Summary{
					CompositeScore: tt.composite,
					NodesAdded:     0,
					NodesRemoved:   0,
					NodesModified:  0,
					EdgesAdded:     0,
					EdgesRemoved:   0,
				},
			}
			got := InterpretResults(r)

			if !strings.Contains(got, "**identical**") {
				t.Errorf("zero changes at composite=%.4f: expected **identical**, got:\n%s", tt.composite, got)
			}
			if strings.Contains(got, "nearly identical") {
				t.Errorf("zero changes: should NOT say nearly identical, got:\n%s", got)
			}
			// Should only have the Overall Assessment section, no follow-up.
			if strings.Contains(got, "### What Changed") {
				t.Errorf("zero changes: should NOT have What Changed section, got:\n%s", got)
			}
			if strings.Contains(got, "### Graph-Level") {
				t.Errorf("zero changes: should NOT have Graph-Level section, got:\n%s", got)
			}
		})
	}
}

func TestInterpretResults_NonIdentical_HasChanges(t *testing.T) {
	// When there ARE structural changes, use composite score for wording.
	tests := []struct {
		name       string
		composite  float64
		added      int
		wantPhrase string
		wantNot    string
	}{
		{
			name:       "high score but has changes is nearly identical",
			composite:  0.9996,
			added:      2,
			wantPhrase: "**nearly identical**",
			wantNot:    "No structural differences",
		},
		{
			name:       "0.96 with changes is nearly identical",
			composite:  0.96,
			added:      5,
			wantPhrase: "**nearly identical**",
		},
		{
			name:       "0.85 is highly similar",
			composite:  0.85,
			added:      20,
			wantPhrase: "**highly similar**",
		},
		{
			name:       "0.65 is moderately similar",
			composite:  0.65,
			added:      50,
			wantPhrase: "**moderately similar**",
		},
		{
			name:       "0.40 is loosely related",
			composite:  0.40,
			added:      100,
			wantPhrase: "**loosely related**",
		},
		{
			name:       "0.20 is structurally unrelated",
			composite:  0.20,
			added:      200,
			wantPhrase: "**structurally unrelated**",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{
				Summary: Summary{
					CompositeScore: tt.composite,
					NodesAdded:     tt.added,
				},
			}
			got := InterpretResults(r)

			if !strings.Contains(got, tt.wantPhrase) {
				t.Errorf("composite=%.4f: expected %q, got:\n%s", tt.composite, tt.wantPhrase, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("composite=%.4f: did NOT expect %q, got:\n%s", tt.composite, tt.wantNot, got)
			}
			// Should have follow-up sections when there are changes.
			if !strings.Contains(got, "### What Changed") {
				t.Errorf("has changes: expected What Changed section, got:\n%s", got)
			}
		})
	}
}
