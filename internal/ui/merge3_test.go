package ui

import (
	"strings"
	"testing"
)

func mergeBlockKinds(blocks []mergeBlock) []mergeBlockKind {
	kinds := make([]mergeBlockKind, len(blocks))
	for i, b := range blocks {
		kinds[i] = b.kind
	}
	return kinds
}

func lines(ss ...string) []string { return ss }

func TestMerge3DisjointChangesAuto(t *testing.T) {
	base := "one\ntwo\nthree\nfour\nfive\n"
	left := "one\nTWO-LEFT\nthree\nfour\nfive\n"
	right := "one\ntwo\nthree\nFOUR-RIGHT\nfive\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 0 {
		t.Fatalf("conflictCount = %d, want 0; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	want := "one\nTWO-LEFT\nthree\nFOUR-RIGHT\nfive\n"
	if got != want {
		t.Fatalf("composed = %q, want %q", got, want)
	}
}

func TestMerge3OverlapConflicts(t *testing.T) {
	base := "one\ntwo\nthree\n"
	left := "one\nTWO-LEFT\nthree\n"
	right := "one\ntwo-right\nthree\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1", conflictCount(blocks))
	}
	var cb *mergeBlock
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			cb = &blocks[i]
		}
	}
	if cb == nil {
		t.Fatal("no conflict block")
	}
	if !linesEqual(cb.left, lines("TWO-LEFT\n")) {
		t.Errorf("left slice = %q", cb.left)
	}
	if !linesEqual(cb.right, lines("two-right\n")) {
		t.Errorf("right slice = %q", cb.right)
	}
	if !linesEqual(cb.base, lines("two\n")) {
		t.Errorf("base slice = %q", cb.base)
	}

	// Unset choice must fail composition.
	if _, err := composeResolved(blocks); err == nil {
		t.Fatal("composeResolved should fail with unresolved block")
	}

	cb.choice = resolutionLeft
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != left {
		t.Fatalf("left choice composed = %q, want %q", got, left)
	}

	cb.choice = resolutionBoth
	got, err = composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	want := "one\nTWO-LEFT\ntwo-right\nthree\n"
	if got != want {
		t.Fatalf("both choice composed = %q, want %q", got, want)
	}
}

func TestMerge3IdenticalChangeAuto(t *testing.T) {
	base := "one\ntwo\nthree\n"
	left := "one\nSAME\nthree\n"
	right := "one\nSAME\nthree\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 0 {
		t.Fatalf("conflictCount = %d, want 0", conflictCount(blocks))
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != left {
		t.Fatalf("composed = %q, want %q", got, left)
	}
}

func TestMerge3BothInsertSamePointConflicts(t *testing.T) {
	base := "a\nb\n"
	left := "a\nLEFT-NEW\nb\n"
	right := "a\nRIGHT-NEW\nb\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			cb := &blocks[i]
			if !linesEqual(cb.left, lines("LEFT-NEW\n")) {
				t.Errorf("left slice = %q", cb.left)
			}
			if !linesEqual(cb.right, lines("RIGHT-NEW\n")) {
				t.Errorf("right slice = %q", cb.right)
			}
			cb.choice = resolutionLeft
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != left {
		t.Fatalf("composed = %q, want %q", got, left)
	}
}

func TestMerge3AdjacentChangeAndInsertAuto(t *testing.T) {
	// Left replaces line 2; right inserts after line 3. Non-overlapping:
	// both changes auto-merge.
	base := "one\ntwo\nthree\n"
	left := "one\nTWO-LEFT\nthree\n"
	right := "one\ntwo\nthree\nRIGHT-NEW\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 0 {
		t.Fatalf("conflictCount = %d, want 0; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	want := "one\nTWO-LEFT\nthree\nRIGHT-NEW\n"
	if got != want {
		t.Fatalf("composed = %q, want %q", got, want)
	}
}

func TestMerge3DeleteVersusEditConflicts(t *testing.T) {
	base := "one\ntwo\nthree\n"
	left := "one\nTWO-LEFT\nthree\n"
	right := "one\nthree\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			cb := &blocks[i]
			if !linesEqual(cb.left, lines("TWO-LEFT\n")) {
				t.Errorf("left slice = %q", cb.left)
			}
			if len(cb.right) != 0 {
				t.Errorf("right slice = %q, want empty", cb.right)
			}
			cb.choice = resolutionRight
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != right {
		t.Fatalf("composed = %q, want %q", got, right)
	}
}

func TestMerge3AddAddEmptyBaseConflicts(t *testing.T) {
	blocks := merge3Strings("", "left file\n", "right file\n")
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			blocks[i].choice = resolutionBoth
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != "left file\nright file\n" {
		t.Fatalf("composed = %q", got)
	}
}

func TestMerge3MultiHunkMixed(t *testing.T) {
	base := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	left := "one\nTWO-L\nthree\nfour\nFIVE-L\nsix\nseven\neight\n"
	right := "one\nTWO-R\nthree\nfour\nfive\nsix\nSEVEN-R\neight\n"

	blocks := merge3Strings(base, left, right)
	// Expect: ctx one / conflict two / ctx three+four / auto five /
	// ctx six / auto seven / ctx eight
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			blocks[i].choice = resolutionRight
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	want := "one\nTWO-R\nthree\nfour\nFIVE-L\nsix\nSEVEN-R\neight\n"
	if got != want {
		t.Fatalf("composed = %q, want %q", got, want)
	}
}

func TestMerge3TrailingChanges(t *testing.T) {
	base := "one\ntwo\n"
	left := "one\ntwo-LEFT-edge" // no trailing newline on left
	right := "one\ntwo-right\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1", conflictCount(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			blocks[i].choice = resolutionLeft
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != left {
		t.Fatalf("composed = %q, want %q (newline edge preserved)", got, left)
	}
}

func TestMerge3InsertInsideOtherSideChange(t *testing.T) {
	// Right replaces lines 2-3; left inserts a line between them (base pos 2).
	base := "one\ntwo\nthree\nfour\n"
	left := "one\ntwo\nINSERTED\nthree\nfour\n"
	right := "one\nREPLACED\nfour\n"

	blocks := merge3Strings(base, left, right)
	if conflictCount(blocks) != 1 {
		t.Fatalf("conflictCount = %d, want 1; kinds=%v", conflictCount(blocks), mergeBlockKinds(blocks))
	}
	for i := range blocks {
		if blocks[i].kind == mergeConflict {
			cb := &blocks[i]
			// Left's slice is base content plus the inserted line in place.
			if strings.Join(cb.left, "") != "two\nINSERTED\nthree\n" {
				t.Errorf("left slice = %q", cb.left)
			}
			if strings.Join(cb.right, "") != "REPLACED\n" {
				t.Errorf("right slice = %q", cb.right)
			}
			cb.choice = resolutionRight
		}
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != right {
		t.Fatalf("composed = %q, want %q", got, right)
	}
}

func TestMerge3IdenticalFiles(t *testing.T) {
	blocks := merge3Strings("a\nb\n", "a\nb\n", "a\nb\n")
	if conflictCount(blocks) != 0 {
		t.Fatalf("conflictCount = %d", conflictCount(blocks))
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	if got != "a\nb\n" {
		t.Fatalf("composed = %q", got)
	}
}

func TestMerge3BothSidesPrefixSuffixTrim(t *testing.T) {
	// Larger file where changes cluster in the middle, exercising the
	// prefix/suffix trim and mapIdx bookkeeping across many keep gaps.
	var b, l, r []string
	for i := 0; i < 50; i++ {
		b = append(b, "line-"+string(rune('a'+i%26))+"\n")
	}
	l = append(l, b...)
	r = append(r, b...)
	l[10] = "LEFT-10\n"
	r[30] = "RIGHT-30\n"
	blocks := merge3(splitLinesKeepEnds(strings.Join(b, "")), splitLinesKeepEnds(strings.Join(l, "")), splitLinesKeepEnds(strings.Join(r, "")))
	if conflictCount(blocks) != 0 {
		t.Fatalf("conflictCount = %d, want 0", conflictCount(blocks))
	}
	got, err := composeResolved(blocks)
	if err != nil {
		t.Fatalf("composeResolved: %v", err)
	}
	want := strings.Join(rJoin(b, 10, "LEFT-10\n", 30, "RIGHT-30\n"), "")
	if got != want {
		t.Fatalf("composed mismatch:\n got=%q\nwant=%q", got, want)
	}
}

func rJoin(base []string, reps ...interface{}) []string {
	out := append([]string{}, base...)
	for i := 0; i < len(reps); i += 2 {
		out[reps[i].(int)] = reps[i+1].(string)
	}
	return out
}
