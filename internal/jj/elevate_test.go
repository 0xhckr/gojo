package jj

import "testing"

func TestAppendExtraSkipsEmpty(t *testing.T) {
	got := appendExtra([]string{"bookmark", "set", "foo"}, []string{"", "--allow-backwards", ""})
	want := []string{"bookmark", "set", "foo", "--allow-backwards"}
	if len(got) != len(want) {
		t.Fatalf("appendExtra len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("appendExtra[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDetectElevation(t *testing.T) {
	cases := []struct {
		name    string
		err     string
		wantFl  string
		wantWhy string
	}{
		{
			name:   "immutable root",
			err:    "Error: The root commit 000000000000 is immutable",
			wantFl: "--ignore-immutable", wantWhy: "target is immutable",
		},
		{
			name:   "immutable configured",
			err:    "Error: Commit 12345 is immutable",
			wantFl: "--ignore-immutable", wantWhy: "target is immutable",
		},
		{
			name:   "backwards bookmark",
			err:    "Error: Refusing to move bookmark backwards or sideways: bm1\nHint: Use --allow-backwards to allow it.",
			wantFl: "--allow-backwards", wantWhy: "bookmark moves backwards/sideways",
		},
		{
			name:   "unrelated error",
			err:    "Error: Revision `nope` doesn't exist",
			wantFl: "", wantWhy: "",
		},
		{
			name:   "empty",
			err:    "",
			wantFl: "", wantWhy: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fl, why := DetectElevation(c.err)
			if fl != c.wantFl || why != c.wantWhy {
				t.Errorf("DetectElevation(%q) = (%q, %q), want (%q, %q)", c.err, fl, why, c.wantFl, c.wantWhy)
			}
		})
	}
}
