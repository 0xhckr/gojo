package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpBinding struct {
	key  string
	desc string
}

type helpSection struct {
	title    string
	color    lipgloss.TerminalColor
	bindings []helpBinding
}

var helpSections = []helpSection{
	{title: "Global", color: colWhite, bindings: []helpBinding{
		{"?", "this help"},
		{"q", "quit / close panel"},
		{"ctrl+c", "force quit"},
	}},
	{title: "Log View", color: colBlue, bindings: []helpBinding{
		{"↑/k, ↓/j", "navigate commits"},
		{"Home", "first commit"},
		{"G", "last commit"},
		{"enter", "open diff panel"},
		{"f", "file view  (browse / blame / history)"},
		{"d", "jj describe  ($EDITOR)"},
		{"D", "AI generate commit message"},
		{"e", "jj edit  (set working copy)"},
		{"n", "jj new  (create change)"},
		{"a", "jj abandon  (remove commit)"},
		{"A", "toggle all revisions  (all())"},
		{"b", "bookmark mode"},
		{"g", "git mode"},
		{"u", "jj undo"},
		{"R", "jj redo"},
		{"r", "rebase mode"},
		{"s", "squash mode"},
	}},
	{title: "Rebase Mode", color: colYellow, bindings: []helpBinding{
		{"r", "pick up selected commit"},
		{"↑/k, ↓/j", "move destination"},
		{"Home / G", "destination to top / bottom"},
		{"s", "toggle scope  (-r single ⇄ -s subtree)"},
		{"tab", "cycle placement  (onto / after / before)"},
		{"enter", "confirm rebase"},
		{"esc / q", "cancel"},
	}},
	{title: "Squash Mode", color: colYellow, bindings: []helpBinding{
		{"s", "pick selected commit to squash"},
		{"↑/k, ↓/j", "move destination"},
		{"Home / G", "destination to top / bottom"},
		{"enter", "confirm  (fold changes into destination)"},
		{"esc / q", "cancel"},
	}},
	{title: "Diff Panel", color: colGreen, bindings: []helpBinding{
		{"↑/k, ↓/j", "scroll diff"},
		{"pgup/b", "scroll up half page"},
		{"pgdn/f", "scroll down half page"},
		{"g / G", "jump top / bottom"},
		{"d", "edit description"},
		{"D", "AI describe"},
		{"n", "jj new  (create change on top)"},
		{"enter / q", "close diff"},
	}},
	{title: "File View", color: colMagenta, bindings: []helpBinding{
		{"f (from log)", "open the file browser"},
		{"↑/k, ↓/j", "navigate tree / lines"},
		{"l / →", "expand directory"},
		{"h / ←", "collapse directory / up"},
		{"⏎ / space", "open file  (or toggle dir)"},
		{"type any char", "launch fzf fuzzy picker"},
		{"h", "file history  (all())"},
		{"⏎", "open the line's commit"},
		{"g / G", "jump top / bottom"},
		{"esc / q", "back a step / quit"},
	}},
	{title: "Help View", color: colPurple, bindings: []helpBinding{
		{"↑/k, ↓/j", "scroll help"},
		{"pgup/b", "scroll up half page"},
		{"pgdn/f", "scroll down half page"},
		{"g / Home", "jump to top"},
		{"G / End", "jump to bottom"},
		{"? / q", "close help"},
	}},
	{title: "Bookmark Mode", color: colCyan, bindings: []helpBinding{
		{"c", "create bookmark"},
		{"d", "delete bookmark"},
		{"f", "forget bookmark"},
		{"l", "list bookmarks"},
		{"m", "move bookmark"},
		{"r", "rename bookmark"},
		{"s", "set bookmark"},
		{"t", "track bookmark"},
		{"T", "untrack bookmark"},
		{"tab", "autocomplete  (cycle suggestions)"},
		{"esc", "dismiss / cancel / exit"},
	}},
	{title: "Git Mode", color: colOrange, bindings: []helpBinding{
		{"f", "git fetch"},
		{"p", "git push"},
		{"r", "remote mode"},
		{"esc / q", "cancel / exit"},
	}},
	{title: "Remote Mode", color: colPink, bindings: []helpBinding{
		{"a", "add remote  (name url)"},
		{"l", "list remotes"},
		{"r", "remove remote  (name)"},
		{"m", "rename remote  (old new)"},
		{"s", "set-url  (name url)"},
		{"esc / q", "cancel / exit"},
	}},
}

const helpKeyCol = 16

type helpRowKind int

const (
	helpBlank helpRowKind = iota
	helpTitle
	helpSep
	helpBindingRow
)

type helpRow struct {
	kind    helpRowKind
	section *helpSection
	binding helpBinding
}

func helpRows() []helpRow {
	var rows []helpRow
	for i := range helpSections {
		s := &helpSections[i]
		rows = append(rows, helpRow{kind: helpBlank})
		rows = append(rows, helpRow{kind: helpTitle, section: s})
		rows = append(rows, helpRow{kind: helpSep})
		for _, b := range s.bindings {
			rows = append(rows, helpRow{kind: helpBindingRow, section: s, binding: b})
		}
	}
	return rows
}

func helpTotalRows() int { return len(helpRows()) }

func helpMaxScroll(contentHeight int) int {
	m := helpTotalRows() - contentHeight
	if m < 0 {
		return 0
	}
	return m
}

// renderHelp produces exactly height lines (including the title bar).
// A scrollbar on the right edge shows position when the help content overflows.
func renderHelp(width, height, scrollY int) []string {
	rows := helpRows()
	total := len(rows)
	contentH := height - 1 // minus title bar
	if contentH < 0 {
		contentH = 0
	}
	maxScroll := max(0, total-contentH)
	clampedY := min(max(0, scrollY), maxScroll)

	end := min(clampedY+contentH, total)
	sliced := rows[clampedY:end]
	visLines := end - clampedY

	// Title bar.
	titleLeft := " gojo help"
	titleRight := fmt.Sprintf("(%d-%d/%d) ?/q close ", clampedY+1, min(clampedY+contentH, total), total)
	titlePad := max(1, width-len(titleLeft)-len(titleRight))
	title := bgRow(width, colElement, seg{text: titleLeft + strings.Repeat(" ", titlePad) + titleRight, fg: colPurple, bg: colElement})

	out := []string{title}

	// Scrollbar: reserve columns when content overflows.
	scrollW := width
	thumbStart, thumbEnd := scrollbarThumb(total, clampedY, visLines, contentH)
	hasBar := thumbStart >= 0
	if hasBar {
		scrollW -= scrollbarWidth
	}

	for i, row := range sliced {
		lineIdx := i // 0-based within the visible window
		var rowStr string
		switch row.kind {
		case helpBlank:
			rowStr = blankRow(scrollW, colPanel)
		case helpTitle:
			rowStr = bgRow(scrollW, colPanel, seg{text: "┃ ", fg: row.section.color, bold: true, bg: colPanel}, seg{text: row.section.title, fg: row.section.color, bg: colPanel})
		case helpSep:
			sep := "  " + strings.Repeat("─", min(scrollW-4, 30))
			rowStr = bgRow(scrollW, colPanel, seg{text: sep, fg: colBorder, bg: colPanel})
		case helpBindingRow:
			b := row.binding
			keyPad := max(0, helpKeyCol-len([]rune(b.key)))
			line := "    " + b.key + strings.Repeat(" ", keyPad) + b.desc
			rowStr = bgRow(scrollW, colPanel, seg{text: line, fg: colTextMuted, bg: colPanel})
		}
		out = append(out, renderRowWithBarFromString(scrollW, width, colPanel, hasBar, lineIdx, thumbStart, thumbEnd, rowStr))
	}

	// Pad to full height.
	for len(out) < height {
		out = append(out, "")
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}
