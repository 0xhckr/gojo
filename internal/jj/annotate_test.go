package jj

import "testing"

func TestParseAnnotate(t *testing.T) {
	// Each record is blame-fields + '\x01' + content, where content includes
	// its own trailing newline (so a final newline yields a trailing "").
	raw := "mwqwmwpp|b2fe214a|hackr@hackr.sh|1|\x01package jj\n" +
		"mwqwmwpp|b2fe214a|hackr@hackr.sh|2|\x01\n" +
		"kxmyusxx|aa0100ff|al@ice.gg|3|\x01import \"fmt\"\n"
	lines := parseAnnotate(raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	want := AnnotateLine{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 1, Text: "package jj"}
	if lines[0] != want {
		t.Errorf("line 0 = %+v, want %+v", lines[0], want)
	}
	if lines[2].ChangeID != "kxmyusxx" || lines[2].LineNo != 3 || lines[2].Text != "import \"fmt\"" {
		t.Errorf("line 2 = %+v", lines[2])
	}
}

func TestParseAnnotateEmpty(t *testing.T) {
	if got := parseAnnotate(""); len(got) != 0 {
		t.Fatalf("expected 0 lines, got %d", len(got))
	}
	// A line without a marker byte is dropped, not crashed on.
	if got := parseAnnotate("garbage no marker\n"); len(got) != 0 {
		t.Fatalf("expected 0 lines, got %d", len(got))
	}
}
