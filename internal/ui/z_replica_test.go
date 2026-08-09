package ui

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// TestReplicaRealDiff replicates the live scenario against the actual
// repository working copy: open the @ diff at 170x55 (the observed geometry)
// and report every internal width parameter plus any over-wide output row.
func TestReplicaRealDiff(t *testing.T) {
	cwd, _ := os.Getwd()
	repoRoot := cwd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(repoRoot + "/.jj"); err == nil {
			break
		}
		repoRoot = repoRoot + "/.."
	}
	if _, err := os.Stat(repoRoot + "/.jj"); err != nil {
		t.Skip("not in a jj repo")
	}
	cfg := jj.Config{JJPath: "jj", RepoRoot: repoRoot}
	r := jj.NewRunner(cfg)

	entries, err := r.Log(50)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	status, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	diff, err := r.Diff(entries[0].CommitID)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	desc, _ := r.Description(entries[0].CommitID)

	for _, sz := range [][2]int{{170, 55}, {155, 50}, {120, 40}} {
		width, height := sz[0], sz[1]
		rows := renderDiff(diff)
		m := Model{width: width, height: height, view: viewLog, ready: true, focused: true}
		m.entries = entries
		m.cursor = 0
		m.statusEntries = status
		m.diffOpen = true
		m.diffIsRevision = true
		m.diffRev = entries[0].ChangeID
		m.diffDesc = desc
		m.diffSrcRaw = diff
		m.diffRows = rows
		m.diffStatus = statusEntries(t, diff)
		m.diffDigits = maxLineDigits(rows)
		m.computeDiffLayout()
		m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
		m.diffChunksHead = m.diffHeadLen()

		t.Logf("size %dx%d: contentH=%d headLen=%d layout.total=%d layout.scrollW=%d statusBar=%d helpBar=%d",
			width, height, m.contentHeight(), m.diffHeadLen(), m.diffLayout.total, m.diffLayout.scrollW,
			m.statusBarHeight(), m.helpBarHeight())

		out := m.View()
		lines := splitLines(out)
		if len(lines) != height {
			t.Errorf("size %dx%d: rendered %d lines, want %d", width, height, len(lines), height)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != width {
				t.Errorf("size %dx%d line %d: width %d, want %d", width, height, i, w, width)
			}
		}
		// scroll to bottom too
		m.diffMoveBottom()
		out = m.View()
		for i, l := range splitLines(out) {
			if w := lipgloss.Width(l); w != width && i < height {
				t.Errorf("size %dx%d bottom line %d: width %d, want %d", width, height, i, w, width)
			}
		}
	}
}

func statusEntries(t *testing.T, _ string) []jj.StatusEntry {
	t.Helper()
	cwd, _ := os.Getwd()
	repoRoot := cwd
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(repoRoot + "/.jj"); err == nil {
			break
		}
		repoRoot += "/.."
	}
	r := jj.NewRunner(jj.Config{JJPath: "jj", RepoRoot: repoRoot})
	sum, err := r.DiffSummary("@")
	if err != nil {
		t.Fatalf("DiffSummary: %v", err)
	}
	return sum
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
