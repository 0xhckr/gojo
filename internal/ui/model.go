package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hackr/gojo/internal/jj"
)

// View represents a top-level view in the TUI.
type View int

const (
	ViewLog View = iota
	ViewHelp
)

func (v View) String() string {
	switch v {
	case ViewLog:
		return "log"
	case ViewHelp:
		return "help"
	default:
		return "?"
	}
}

// Model is the top-level Bubble Tea model.
type Model struct {
	runner *jj.Runner
	view   View

	// Log view state.
	logEntries []jj.LogEntry
	cursor     int
	offset     int // scroll offset for log list
	width      int
	height     int

	// Status view state.
	statusEntries []jj.StatusEntry

	// Inline status panel state.
	diffOpen         bool
	diffRev          string
	revStatusEntries []jj.StatusEntry

	// Error/status message.
	err     string
	message string
}

// Messages for async operations.
type logLoadedMsg struct{ entries []jj.LogEntry }
type statusLoadedMsg struct{ entries []jj.StatusEntry }
type revStatusLoadedMsg struct{ entries []jj.StatusEntry }
type errMsg struct{ err error }

func NewModel(runner *jj.Runner) Model {
	return Model{
		runner: runner,
		view:   ViewLog,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadLog(), m.loadStatus())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case logLoadedMsg:
		m.logEntries = msg.entries
		if m.cursor >= len(m.logEntries) {
			m.cursor = len(m.logEntries) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.err = ""
		m.message = ""
		return m, nil

	case statusLoadedMsg:
		m.statusEntries = msg.entries
		m.err = ""
		m.message = ""
		return m, nil

	case revStatusLoadedMsg:
		m.revStatusEntries = msg.entries
		m.err = ""
		m.message = ""
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		m.message = ""
		return m, nil
	}

	// Handle keys based on current view.
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global keys.
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.view == ViewHelp {
				m.view = ViewLog
				return m, nil
			}
			if m.diffOpen {
				m.diffOpen = false
				return m, nil
			}
			return m, tea.Quit
		case "?":
			if m.diffOpen {
				m.diffOpen = false
				return m, nil
			}
			m.view = ViewHelp
			return m, nil
		case "r":
			m.message = "refreshing…"
			return m, m.refresh()
		}

		// View-specific keys.
		switch m.view {
		case ViewLog:
			return m.updateLog(msg)
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	// ── Help bar (always at the bottom) ──
	helpBar := styleHelpBar.Width(m.width).Render(" enter:diff  e:edit  n:new  ?:help  r:refresh  q:quit ")

	// ── Status bar (second from bottom) ──
	var statusBar string
	if m.err != "" {
		statusBar = styleError.Width(m.width).Render(" ✖ " + truncate(m.err, m.width-4))
	} else if m.message != "" {
		statusBar = styleMuted.Width(m.width).Render(" " + m.message)
	} else {
		// Inline status: show changed file count
		if len(m.statusEntries) > 0 {
			statusBar = styleMuted.Width(m.width).Render(fmt.Sprintf(" %d changed file(s)", len(m.statusEntries)))
		} else {
			statusBar = styleMuted.Width(m.width).Render(" clean working copy ✓")
		}
	}

	// Content area = total height minus the two bars.
	contentHeight := m.height - lipgloss.Height(helpBar) - lipgloss.Height(statusBar)
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.view {
	case ViewLog:
		content = m.viewLog(contentHeight)
	case ViewHelp:
		content = m.viewHelp(contentHeight)
	}

	// Ensure content fills exactly contentHeight lines.
	content = padToHeight(content, contentHeight)

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar, helpBar)
}

// ── Log View ────────────────────────────────────────────────────────────────

func (m Model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If diff panel is open, keys control the diff.
	if m.diffOpen {
		return m.updateDiffPanel(msg)
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.logEntries)-1 {
			m.cursor++
		}
	case "enter", "d":
		if len(m.logEntries) > 0 && m.cursor < len(m.logEntries) {
			entry := m.logEntries[m.cursor]
			m.diffRev = entry.CommitID
			m.revStatusEntries = nil
			m.diffOpen = true
			m.message = ""
			return m, m.loadRevStatus(entry.CommitID)
		}
	case "e":
		if len(m.logEntries) > 0 {
			entry := m.logEntries[m.cursor]
			m.message = "editing " + entry.ChangeID + "…"
			return m, m.editRev(entry.ChangeID)
		}
	case "n":
		m.message = "creating new change…"
		return m, m.newRev()
	case "G":
		m.cursor = len(m.logEntries) - 1
	case "g":
		m.cursor = 0
	}
	return m, nil
}

func (m Model) updateDiffPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "d":
		m.diffOpen = false
	}
	return m, nil
}

// statusPanelHeight returns how many lines the bottom panel should take.
// Roughly half the screen, min 3.
func (m Model) statusPanelHeight(contentHeight int) int {
	h := contentHeight / 2
	if h < 3 {
		h = 3
	}
	return h
}

func (m Model) viewLog(contentHeight int) string {
	if len(m.logEntries) == 0 {
		return styleMuted.Render("  no revisions found")
	}

	var b strings.Builder
	b.WriteString("\n")

	// ── Commit list (top) ──
	// If status panel is open, log gets half the screen.
	logHeight := contentHeight
	if m.diffOpen {
		logHeight = contentHeight - m.statusPanelHeight(contentHeight)
	}

	visibleEntries := logHeight - 1 // -1 for top padding
	if visibleEntries < 1 {
		visibleEntries = 1
	}

	// Adjust scroll offset to keep cursor visible.
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visibleEntries {
		m.offset = m.cursor - visibleEntries + 1
	}

	end := m.offset + visibleEntries
	if end > len(m.logEntries) {
		end = len(m.logEntries)
	}

	for i := m.offset; i < end; i++ {
		e := m.logEntries[i]

		// Cursor indicator.
		cursor := " "
		if i == m.cursor && !m.diffOpen {
			cursor = styleCursor.Render("▸")
		}

		// Symbols.
		symbol := "○"
		style := styleSubject
		if e.IsWorkingCopy {
			symbol = "@"
			style = styleWorkingCopy
		} else if e.IsImmutable {
			symbol = "◆"
			style = styleImmutable
		}

		// Bookmarks.
		var bookmarkStr string
		if len(e.Bookmarks) > 0 {
			var bms []string
			for _, b := range e.Bookmarks {
				bms = append(bms, styleBookmark.Render(b))
			}
			bookmarkStr = " " + strings.Join(bms, " ")
		}

		subject := e.Subject
		if subject == "" {
			subject = "(no description set)"
		}

		line := fmt.Sprintf("%s %s %s %s  %s %s %s%s",
			cursor,
			style.Render(symbol),
			styleChangeID.Render(e.ChangeID),
			style.Render(subject),
			styleAuthor.Render(e.Authors),
			styleDate.Render(e.Date),
			styleCommitID.Render(e.CommitID),
			bookmarkStr,
		)

		if i == m.cursor {
			line = highlightLine(line, colorDarkPurple, m.width)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// ── Status panel (bottom) ──
	if m.diffOpen {
		b.WriteString(styleMuted.Render(strings.Repeat("─", m.width)))
		b.WriteString("\n")
		title := styleTitle.Width(m.width).Render(" Status: " + m.diffRev + "  (enter/q to close) ")
		b.WriteString(title)
		b.WriteString("\n")

		if len(m.revStatusEntries) == 0 {
			b.WriteString(styleMuted.Render("  clean"))
			b.WriteString("\n")
		} else {
			for _, e := range m.revStatusEntries {
				var statusStr string
				switch e.Status {
				case "Added":
					statusStr = styleAdded.Render("  A ")
				case "Modified":
					statusStr = styleModified.Render("  M ")
				case "Removed":
					statusStr = styleRemoved.Render("  D ")
				case "Conflicted":
					statusStr = styleConflict.Render("  C ")
				default:
					statusStr = "  ? "
				}
				b.WriteString(statusStr)
				b.WriteString(stylePath.Render(e.Path))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

// ── Help View ───────────────────────────────────────────────────────────────

func (m Model) viewHelp(contentHeight int) string {
	title := styleTitle.Width(m.width).Render(" gojo — keybindings ")

	help := []struct{ key, desc string }{
		{"Global", ""},
		{"  ?", "this help"},
		{"  r", "refresh"},
		{"  q", "quit / close diff"},
		{"", ""},
		{"Log View", ""},
		{"  ↑/k, ↓/j", "navigate commits"},
		{"  g / G", "first / last commit"},
		{"  enter/d", "open diff panel"},
		{"  e", "jj edit (checkout commit)"},
		{"  n", "jj new (create change)"},
		{"", ""},
		{"Diff Panel", ""},
		{"  ↑/k, ↓/j", "scroll diff"},
		{"  pgup/b, pgdn/f", "half-page scroll"},
		{"  g / G", "top / bottom"},
		{"  enter/d/q", "close diff"},
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	for _, h := range help {
		if h.desc == "" && h.key != "" {
			b.WriteString(styleChangeID.Render("\n " + h.key + "\n"))
		} else if h.key == "" {
			b.WriteString("\n")
		} else {
			b.WriteString(fmt.Sprintf("  %-16s %s\n", styleCommitID.Render(h.key), h.desc))
		}
	}

	return b.String()
}

// ── Async Commands ──────────────────────────────────────────────────────────

func (m Model) loadLog() tea.Cmd {
	return func() tea.Msg {
		entries, err := m.runner.Log(context.Background(), "", 50)
		if err != nil {
			return errMsg{err}
		}
		return logLoadedMsg{entries}
	}
}

func (m Model) loadStatus() tea.Cmd {
	return func() tea.Msg {
		entries, err := m.runner.Status(context.Background())
		if err != nil {
			return errMsg{err}
		}
		return statusLoadedMsg{entries}
	}
}

func (m Model) loadRevStatus(rev string) tea.Cmd {
	return func() tea.Msg {
		entries, err := m.runner.DiffSummary(context.Background(), rev)
		if err != nil {
			return errMsg{err}
		}
		return revStatusLoadedMsg{entries}
	}
}

func (m Model) editRev(rev string) tea.Cmd {
	return func() tea.Msg {
		if err := m.runner.Edit(context.Background(), rev); err != nil {
			return errMsg{err}
		}
		entries, err := m.runner.Log(context.Background(), "", 50)
		if err != nil {
			return errMsg{err}
		}
		return logLoadedMsg{entries}
	}
}

func (m Model) newRev() tea.Cmd {
	return func() tea.Msg {
		if err := m.runner.New(context.Background(), ""); err != nil {
			return errMsg{err}
		}
		entries, err := m.runner.Log(context.Background(), "", 50)
		if err != nil {
			return errMsg{err}
		}
		return logLoadedMsg{entries}
	}
}

func (m Model) refresh() tea.Cmd {
	return tea.Batch(m.loadLog(), m.loadStatus())
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "…"
}

// padToHeight ensures the output fills exactly h lines by padding with blank lines.
func padToHeight(s string, h int) string {
	lines := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") && s != "" {
		lines++
	}
	if lines >= h {
		return s
	}
	return s + strings.Repeat("\n", h-lines)
}

// highlightLine re-applies a background color after every ESC[0m reset in the line.
// This ensures the selection background persists behind all styled segments.
func highlightLine(line string, bg lipgloss.Color, width int) string {
	bgCode := fmt.Sprintf("\x1b[48;5;%sm", bg)
	// Replace every ESC[0m (full reset) with reset+reapply background.
	replaced := strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bgCode)
	// Prepend the background to the start.
	replaced = bgCode + replaced
	// Pad with spaces to fill width, then reset at the end.
	// Measure visible width by stripping ANSI.
	visible := stripAnsi(replaced)
	if pad := width - len(visible); pad > 0 {
		replaced += strings.Repeat(" ", pad)
	}
	replaced += "\x1b[0m"
	return replaced
}

// stripAnsi removes ANSI escape sequences to get visible character count.
func stripAnsi(s string) string {
	var b strings.Builder
	inEscape := false
	for _, ch := range s {
		if ch == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
				inEscape = false
			}
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}
