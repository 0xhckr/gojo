package jj

import "testing"

func TestParseConflicts(t *testing.T) {
	raw := `file.txt    2-sided conflict
src/deep/my file.go  2-sided conflict
weird dir/x.sh  3-sided conflict
`
	got := parseConflicts(raw)
	want := []ConflictEntry{
		{Path: "file.txt", Sides: 2},
		{Path: "src/deep/my file.go", Sides: 2},
		{Path: "weird dir/x.sh", Sides: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("parseConflicts returned %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseConflictsEmpty(t *testing.T) {
	if got := parseConflicts(""); got != nil {
		t.Errorf("parseConflicts(%q) = %v, want nil", "", got)
	}
}
