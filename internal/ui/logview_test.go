package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"gojo/internal/jj"
)

// lineIndex returns the index of the first rendered line containing sub, or -1.
func lineIndex(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), sub) {
			return i
		}
	}
	return -1
}

// TestRenderLogElidedPlacement locks the graph layout: a graph-only edge line
// (e.g. jj's "~  (elided revisions)") trails the commit it is attached to,
// matching jj's own output where such rows are drawn below a node's text.
func TestRenderLogElidedPlacement(t *testing.T) {
	// Mirrors `jj log -r 'base | D | M | @'`: the elided row sits between D's
	// body and base's header, so parseLog attaches it to D as an edge line.
	entries := []jj.LogEntry{
		{ChangeID: "topaaaaa", HeaderPrefix: "@  ", BodyPrefix: "│  ", Subject: "top"},
		{ChangeID: "mergeaaa", HeaderPrefix: "○  ", BodyPrefix: "│  ", Subject: "MERGE"},
		{ChangeID: "dddddddd", HeaderPrefix: "○  ", BodyPrefix: "│  ", Subject: "D",
			EdgeLines: []string{"~  (elided revisions)"}},
		{ChangeID: "basebbbb", HeaderPrefix: "○  ", BodyPrefix: "│  ", Subject: "root",
			EdgeLines: []string{"~"}},
	}

	lines := renderLog(80, 20, entries, 0, 0, nil, 0, rebaseView{}, squashView{})

	dBody := lineIndex(lines, "D")
	elided := lineIndex(lines, "(elided revisions)")
	baseHeader := lineIndex(lines, "basebbbb")

	if dBody < 0 || elided < 0 || baseHeader < 0 {
		t.Fatalf("missing lines: dBody=%d elided=%d baseHeader=%d", dBody, elided, baseHeader)
	}
	// The elided row must fall below D's body and above base's header.
	if !(dBody < elided && elided < baseHeader) {
		t.Errorf("elided row misplaced: dBody=%d elided=%d baseHeader=%d (want dBody < elided < baseHeader)",
			dBody, elided, baseHeader)
	}
}
