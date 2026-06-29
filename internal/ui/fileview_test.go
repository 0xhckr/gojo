package ui

import (
	"testing"

	"gojo/internal/jj"
)

// TestEnsureSectionsNoPanic guards against the index-out-of-range [-1] panic
// that occurred when the first line fell into the `else` branch and indexed
// parity[i-1].
func TestEnsureSectionsNoPanic(t *testing.T) {
	cases := []struct {
		name  string
		lines []jj.AnnotateLine
	}{
		{"empty", nil},
		{"single", []jj.AnnotateLine{{ChangeID: "a", LineNo: 1}}},
		{
			"mixed",
			[]jj.AnnotateLine{
				{ChangeID: "a", LineNo: 1},
				{ChangeID: "a", LineNo: 2},
				{ChangeID: "b", LineNo: 3},
				{ChangeID: "a", LineNo: 4},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fv := fileViewState{lines: tc.lines}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ensureSections panicked: %v", r)
				}
			}()
			fv.ensureSections()
			if len(fv.lineParity) != len(tc.lines) {
				t.Fatalf("expected %d parities, got %d", len(tc.lines), len(fv.lineParity))
			}
			// Parity must flip exactly when the change id changes.
			for i := 1; i < len(tc.lines); i++ {
				changed := tc.lines[i].ChangeID != tc.lines[i-1].ChangeID
				flipped := fv.lineParity[i] != fv.lineParity[i-1]
				if changed != flipped {
					t.Errorf("line %d: changed=%v but parity flipped=%v", i, changed, flipped)
				}
			}
			// Idempotent: a second call is a no-op.
			before := append([]int(nil), fv.lineParity...)
			fv.ensureSections()
			if len(fv.lineParity) != len(before) {
				t.Fatalf("second call changed parity length")
			}
			for i := range before {
				if fv.lineParity[i] != before[i] {
					t.Fatalf("second call mutated parity at %d", i)
				}
			}
		})
	}
}
