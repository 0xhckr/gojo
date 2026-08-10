package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

// BenchmarkRenderSegs measures the core styled-segment renderer.
func BenchmarkRenderSegs(b *testing.B) {
	segs := []seg{
		{text: " ", bg: colPanel},
		{text: "@ ", fg: colGraph, bg: colPanel},
		{text: "kq", fg: colMagenta, bold: true, bg: colPanel},
		{text: "xvqrpn", fg: colTextMuted, bg: colPanel},
		{text: " ", bg: colPanel},
		{text: "user@example.com", fg: colBlue, bg: colPanel},
		{text: " ", bg: colPanel},
		{text: "2024-01-01 12:00", fg: colTextMuted, bg: colPanel},
		{text: " ", bg: colPanel},
		{text: "main", fg: colGreen, bold: true, bg: colPanel},
		{text: "fix thing", fg: colText, bg: colPanel},
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = renderSegs(segs)
	}
}

// BenchmarkBgRow measures a full-width filled row.
func BenchmarkBgRow(b *testing.B) {
	segs := []seg{
		{text: "┃", fg: colYellow, bold: true, bg: colElement},
		{text: "   1   2 ", fg: diffLineNumber, bg: colElement},
		{text: " +  ", fg: diffAddedSign, bg: diffAddedBg},
		{text: strings.Repeat("some highlighted content ", 6), fg: colText, bg: diffAddedBg},
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = bgRow(160, colPanel, segs...)
	}
}

// BenchmarkWrapSegs measures wrapping of a long highlighted line.
func BenchmarkWrapSegs(b *testing.B) {
	var segs []seg
	for i := 0; i < 50; i++ {
		segs = append(segs, seg{text: "word" + strconv.Itoa(i) + " = some_function(argument); ", fg: lipgloss.Color("#ff0000"), bg: diffAddedBg})
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = wrapSegs(segs, 100)
	}
}

// benchModel returns a model with a realistic 50-entry log, bootstrapped as
// ready, at a common terminal size.
func benchModel() Model {
	m := Model{width: 120, height: 40, ready: true, focused: true, view: viewLog}
	for i := 0; i < 50; i++ {
		m.entries = append(m.entries, jj.LogEntry{
			ChangeID:          fmt.Sprintf("kq%06d", i),
			ChangeIDPrefixLen: 2,
			CommitID:          fmt.Sprintf("abc%05d", i),
			CommitIDPrefixLen: 4,
			Authors:           "user@example.com",
			Date:              "2024-01-02 12:34",
			Subject:           fmt.Sprintf("refactor: rework the frobnicate pipeline stage %d", i),
			Bookmarks:         []string{"main", fmt.Sprintf("feature-%d", i)},
			HeaderPrefix:      "@ ",
			BodyPrefix:        "│ ",
		})
	}
	m.cursor = 10
	return m
}

// BenchmarkViewLog measures a full frame render of the default log view.
func BenchmarkViewLog(b *testing.B) {
	m := benchModel()
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkViewDiff measures a full frame render with the diff panel open.
func BenchmarkViewDiff(b *testing.B) {
	m := benchModel()
	var sb strings.Builder
	for f := 0; f < 8; f++ {
		fmt.Fprintf(&sb, "diff --git a/file%d.go b/file%d.go\n--- a/file%d.go\n+++ b/file%d.go\n@@ -1,6 +1,6 @@\n package p%d\n", f, f, f, f, f)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(&sb, "-old value %d = compute()\n+new value %d = recompute()\n", i, i)
		}
		sb.WriteString(" ctx tail\n")
	}
	rows := renderDiff(sb.String())
	m.diffOpen = true
	m.diffIsRevision = true
	m.diffRev = "kq000000"
	m.diffDesc = "subject"
	m.diffSrcRaw = sb.String()
	m.diffRows = rows
	m.diffDigits = maxLineDigits(rows)
	m.diffStatus = []jj.StatusEntry{{Path: "file0.go", Status: jj.StatusModified}}
	m.computeDiffLayout()
	m.diffChunks = computeDiffChunks(rows, m.diffHeadLen(), nil)
	m.diffCurChunk, m.diffCurLine = 1, 2
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View()
	}
}

// BenchmarkThemeSwitch measures a picker move: apply the next theme and
// render a full log frame in it. Theme edits reject anything above a small
// constant — a slow switch makes live-preview browsing feel broken.
func BenchmarkThemeSwitch(b *testing.B) {
	m := benchModel()
	m.themes = loadThemes("", "", "../..")
	m.themeOpen = true
	defer applyTheme(gojoTheme())
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		m.themeMove(i % len(m.themes))
		_ = m.View()
	}
}

// BenchmarkRenderDiff measures end-to-end git-diff parsing + highlighting.
func BenchmarkRenderDiff(b *testing.B) {
	var sb strings.Builder
	for f := 0; f < 8; f++ {
		fmt.Fprintf(&sb, "diff --git a/file%d.go b/file%d.go\n--- a/file%d.go\n+++ b/file%d.go\n@@ -1,6 +1,6 @@\n package p%d\n", f, f, f, f, f)
		for i := 0; i < 30; i++ {
			fmt.Fprintf(&sb, "-old value %d = compute(x%d)\n+new value %d = recompute(x%d)\n", i, i, i, i)
		}
		sb.WriteString(" ctx tail\n")
	}
	raw := sb.String()
	b.ReportAllocs()
	for b.Loop() {
		_ = renderDiff(raw)
	}
}
