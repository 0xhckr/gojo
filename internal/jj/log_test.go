package jj

import "testing"

func TestParseLogWorkspaces(t *testing.T) {
	raw := "@  \x01abcdefgh|abc|12345678|123|me@example.com|2026-08-30 12:00|Y|N|main|v1|N|default,feature\n" +
		"│  \x01workspace commit\n"
	entries := parseLog(raw)
	if len(entries) != 1 {
		t.Fatalf("parseLog returned %d entries", len(entries))
	}
	got := entries[0]
	if len(got.Workspaces) != 2 || got.Workspaces[0] != "default" || got.Workspaces[1] != "feature" {
		t.Fatalf("workspaces = %#v", got.Workspaces)
	}
}

func TestParseLogWithoutWorkspaceField(t *testing.T) {
	raw := "@  \x01abcdefgh|abc|12345678|123|me@example.com|2026-08-30 12:00|Y|N|||N\n" +
		"│  \x01old-format entry\n"
	entries := parseLog(raw)
	if len(entries) != 1 || len(entries[0].Workspaces) != 0 {
		t.Fatalf("entries = %#v", entries)
	}
}
