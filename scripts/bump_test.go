package main

import "testing"

func TestBumpPart(t *testing.T) {
	cases := []struct {
		version, part, want string
	}{
		{"1.2.3", "patch", "1.2.4"},
		{"1.2.3", "minor", "1.3.0"},
		{"1.2.3", "major", "2.0.0"},
		{"0.0.0", "patch", "0.0.1"},
		{"2.5.7", "minor", "2.6.0"},
		{"2.5.7", "major", "3.0.0"},
	}
	for _, c := range cases {
		if got := bumpPart(c.version, c.part); got != c.want {
			t.Errorf("bumpPart(%q, %q) = %q, want %q", c.version, c.part, got, c.want)
		}
	}
}

func TestSemverRE(t *testing.T) {
	for _, ok := range []string{"1.2.3", "0.0.0", "10.20.30"} {
		if !semverRE.MatchString(ok) {
			t.Errorf("semverRE rejected %q", ok)
		}
	}
	for _, bad := range []string{"1.2", "1.2.a", "v1.2.3", "1.2.3.4", "1.2.3-rc1"} {
		if semverRE.MatchString(bad) {
			t.Errorf("semverRE accepted %q", bad)
		}
	}
}
