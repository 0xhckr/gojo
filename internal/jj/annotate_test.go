package jj

import "testing"

func TestParseAnnotate(t *testing.T) {
	// Each record is blame-fields + '\x01' + content, where content includes
	// its own trailing newline. Fields: change_id|commit_id|email|lineno|desc.
	raw := "mwqwmwpp|b2fe214a|hackr@hackr.sh|1|Rewrite gojo\x01package jj\n" +
		"mwqwmwpp|b2fe214a|hackr@hackr.sh|2|Rewrite gojo\x01\n" +
		"kxmyusxx|aa0100ff|al@ice.gg|3|\x01import \"fmt\"\n"
	lines := parseAnnotate(raw)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	want := AnnotateLine{ChangeID: "mwqwmwpp", CommitID: "b2fe214a", Author: "hackr@hackr.sh", LineNo: 1, Description: "Rewrite gojo", Text: "package jj"}
	if lines[0] != want {
		t.Errorf("line 0 = %+v, want %+v", lines[0], want)
	}
	if lines[2].Description != "" {
		t.Errorf("line 2 description = %q, want empty", lines[2].Description)
	}
	if lines[2].ChangeID != "kxmyusxx" || lines[2].LineNo != 3 || lines[2].Text != "import \"fmt\"" {
		t.Errorf("line 2 = %+v", lines[2])
	}
}

func TestParseAnnotateDescWithPipe(t *testing.T) {
	// A '|' inside the description must survive the split (SplitN limit 5).
	lines := parseAnnotate("aa|bb|cc|1|fix: a | b\x01x\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0].Description != "fix: a | b" {
		t.Errorf("description = %q, want %q", lines[0].Description, "fix: a | b")
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
