package compare

import (
	"strings"
	"testing"
)

func TestInterpretResults_IdenticalAt100(t *testing.T) {
	tests := []struct {
		name       string
		composite  float64
		wantPhrase string
		wantNot    string
	}{
		{
			name:       "exact 1.0 is identical",
			composite:  1.0,
			wantPhrase: "**identical**",
			wantNot:    "nearly identical",
		},
		{
			name:       "0.999999 rounds to 100.00% so identical",
			composite:  0.999999,
			wantPhrase: "**identical**",
			wantNot:    "nearly identical",
		},
		{
			name:       "0.999995 rounds to 100.00% so identical",
			composite:  0.999995,
			wantPhrase: "**identical**",
			wantNot:    "nearly identical",
		},
		{
			name:       "0.99994 displays as 99.99% so nearly identical",
			composite:  0.99994,
			wantPhrase: "**nearly identical**",
			wantNot:    "No structural differences",
		},
		{
			name:       "0.9999 displays as 99.99% so nearly identical",
			composite:  0.9999,
			wantPhrase: "**nearly identical**",
			wantNot:    "No structural differences",
		},
		{
			name:       "0.96 is nearly identical",
			composite:  0.96,
			wantPhrase: "**nearly identical**",
			wantNot:    "No structural differences",
		},
		{
			name:       "0.85 is highly similar",
			composite:  0.85,
			wantPhrase: "**highly similar**",
		},
		{
			name:       "0.65 is moderately similar",
			composite:  0.65,
			wantPhrase: "**moderately similar**",
		},
		{
			name:       "0.40 is loosely related",
			composite:  0.40,
			wantPhrase: "**loosely related**",
		},
		{
			name:       "0.20 is structurally unrelated",
			composite:  0.20,
			wantPhrase: "**structurally unrelated**",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Result{
				Summary: Summary{CompositeScore: tt.composite},
			}
			got := InterpretResults(r)

			if !strings.Contains(got, tt.wantPhrase) {
				t.Errorf("composite=%.4f: expected %q in output, got:\n%s", tt.composite, tt.wantPhrase, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("composite=%.4f: did NOT expect %q in output, got:\n%s", tt.composite, tt.wantNot, got)
			}
		})
	}
}
