package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// TestBlameLineAlignment verifies the `│` separator (and thus the source
// code) lands at the same column regardless of row kind (email / desc /
// none), so the description row no longer indents the code.
func TestBlameLineAlignment(t *testing.T) {
	l := jj.AnnotateLine{ChangeID: "mwqwmwpp", CommitID: "b", Author: "hackr@hackr.sh", LineNo: 1, Description: "Rewrite gojo", Text: "x"}
	const width, digits, blameW = 80, 3, 21

	sepCol := func(kind blameKind, desc string) int {
		s := ansi.Strip(renderBlameLine(width, digits, blameW, l, kind, false, blameSectionBgA, desc, nil))
		return strings.Index(s, "│")
	}
	emailCol := sepCol(blameEmail, l.Description)
	descCol := sepCol(blameDesc, l.Description)
	noneCol := sepCol(blameNone, "")
	if emailCol < 0 || descCol < 0 || noneCol < 0 {
		t.Fatalf("missing separator: email=%d desc=%d none=%d", emailCol, descCol, noneCol)
	}
	if emailCol != descCol || descCol != noneCol {
		t.Fatalf("separator misaligned: email=%d desc=%d none=%d", emailCol, descCol, noneCol)
	}
}

// TestBlameVisibleRangeMargin checks that the configured bottom margin is
// respected: the cursor stays at least `margin` rows above the viewport
// bottom, and clamps gracefully when the margin exceeds the height.
func TestBlameVisibleRangeMargin(t *testing.T) {
	lines := make([]jj.AnnotateLine, 100)
	for i := range lines {
		lines[i] = jj.AnnotateLine{LineNo: i + 1, ChangeID: "a"}
	}

	cases := []struct {
		name    string
		height  int
		margin  int
		cursor  int
		wantTop int
	}{
		// margin 0: cursor can reach the last visible line (height=10).
		{"margin0", 10, 0, 25, 16},
		// margin 8 (default): cursor needs >=8 spare rows below.
		{"margin8", 10, 8, 25, 24},
		// margin >= height: clamps to keeping one row below the cursor.
		{"margin_clamped", 10, 50, 25, 25},
		// cursor near the top: viewport scrolls just enough to honor the
		// margin (line 0 may leave the top).
		{"near_top", 10, 8, 2, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fv := fileViewState{lines: lines, cursorY: tc.cursor, scrollY: 0}
			top, end := fv.blameVisibleRange(tc.height, tc.margin)
			if top != tc.wantTop {
				t.Fatalf("got top=%d want %d", top, tc.wantTop)
			}
			if end-top > tc.height {
				t.Fatalf("window %d taller than height %d", end-top, tc.height)
			}
			// The cursor must be within [top, end) and at least `margin` above
			// the bottom (unless clamped).
			if tc.cursor < top || tc.cursor >= end {
				t.Fatalf("cursor %d outside [%d,%d)", tc.cursor, top, end)
			}
			wantMinMargin := tc.margin
			if wantMinMargin >= tc.height {
				wantMinMargin = tc.height - 1
			}
			if end-1-tc.cursor < wantMinMargin {
				t.Fatalf("margin not respected: %d spare rows, want >= %d", end-1-tc.cursor, wantMinMargin)
			}
		})
	}
}

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
