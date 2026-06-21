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
		{"d", "jj describe  ($EDITOR)"},
		{"D", "AI generate commit message"},
		{"e", "jj edit  (set working copy)"},
		{"n", "jj new  (create change)"},
		{"a", "jj abandon  (remove commit)"},
		{"b", "bookmark mode"},
		{"g", "git mode"},
		{"u", "jj undo"},
		{"r", "jj redo"},
	}},
	{title: "Diff Panel", color: colGreen, bindings: []helpBinding{
		{"↑/k, ↓/j", "scroll diff"},
		{"pgup/b", "scroll up half page"},
		{"pgdn/f", "scroll down half page"},
		{"g / G", "jump top / bottom"},
		{"enter / q", "close diff"},
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

	// Title bar.
	titleLeft := " gojo help"
	titleRight := fmt.Sprintf("(%d-%d/%d) ?/q close ", clampedY+1, min(clampedY+contentH, total), total)
	titlePad := max(1, width-len(titleLeft)-len(titleRight))
	title := bgRow(width, colDarkPurple, seg{text: titleLeft + strings.Repeat(" ", titlePad) + titleRight, fg: colPurple, bg: colDarkPurple})

	out := []string{title}

	for _, row := range sliced {
		switch row.kind {
		case helpBlank:
			out = append(out, "")
		case helpTitle:
			out = append(out, plainRow(width, seg{text: "  " + row.section.title, fg: row.section.color}))
		case helpSep:
			sep := "  " + strings.Repeat("─", min(width-4, 30))
			out = append(out, plainRow(width, seg{text: sep, fg: colDarkGray}))
		case helpBindingRow:
			b := row.binding
			keyPad := max(0, helpKeyCol-len([]rune(b.key)))
			line := "    " + b.key + strings.Repeat(" ", keyPad) + b.desc
			out = append(out, plainRow(width, seg{text: line, fg: colGray}))
		}
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
