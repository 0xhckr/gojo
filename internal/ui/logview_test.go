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

	lines := renderLog(80, 20, entries, 0, 0, -1, nil, 0, rebaseView{}, squashView{}, bookmarkDragView{}, -1, -1, "", "")

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

// TestRenderLogMarkersSurviveClipping verifies that rebase/squash/drag mode
// markers stay visible even on rows whose author/bookmark tail is wider than
// the terminal: they render right after the change ID, ahead of the clip-prone
// metadata at the row end.
func TestRenderLogMarkersSurviveClipping(t *testing.T) {
	dest := jj.LogEntry{
		ChangeID: "destdest", CommitID: "c0ffee01",
		Authors:      strings.Repeat("longauthor", 12), // far wider than the view
		Bookmarks:    []string{"main", strings.Repeat("b", 30)},
		HeaderPrefix: "○  ", BodyPrefix: "│  ", Subject: "dest",
	}
	src := jj.LogEntry{
		ChangeID: "srccsrcc", CommitID: "c0ffee02",
		HeaderPrefix: "@  ", BodyPrefix: "│  ", Subject: "src",
	}
	entries := []jj.LogEntry{src, dest}

	plain := func(lines []string) string {
		return ansi.Strip(strings.Join(lines, "\n"))
	}

	rb := renderLog(40, 10, entries, 0, 0, -1, nil, 0, rebaseView{active: true, source: 0, dest: 1}, squashView{}, bookmarkDragView{}, -1, -1, "", "")
	if got := plain(rb); !strings.Contains(got, "● moving") || !strings.Contains(got, "◀ onto") {
		t.Errorf("rebase markers clipped on a wide row:\n%s", got)
	}

	sq := renderLog(40, 10, entries, 0, 0, -1, nil, 0, rebaseView{}, squashView{active: true, source: 0, dest: 1}, bookmarkDragView{}, -1, -1, "", "")
	if got := plain(sq); !strings.Contains(got, "● squashing") || !strings.Contains(got, "◀ into") {
		t.Errorf("squash markers clipped on a wide row:\n%s", got)
	}

	bd := renderLog(40, 10, entries, 0, 0, -1, nil, 0, rebaseView{}, squashView{}, bookmarkDragView{active: true, name: "main", sourceIdx: 0, destIdx: 1}, -1, -1, "", "")
	if got := plain(bd); !strings.Contains(got, "● dragging main") || !strings.Contains(got, "◀ drop") {
		t.Errorf("bookmark-drag markers clipped on a wide row:\n%s", got)
	}
}
