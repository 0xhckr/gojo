package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hackr/gojo/internal/jj"
)

// View represents a top-level view in the TUI.
type View int

const (
	ViewLog View = iota
	ViewStatus
	ViewDiff
	ViewHelp
)

func (v View) String() string {
	switch v {
	case ViewLog:
		return "log"
	case ViewStatus:
		return "status"
	case ViewDiff:
		return "diff"
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

	// Diff view state.
	diffContent string
	diffRev     string
	diffVP      viewport.Model
	diffReady   bool

	// Error/status message.
	err     string
	message string
}

// Messages for async operations.
type logLoadedMsg struct{ entries []jj.LogEntry }
type statusLoadedMsg struct{ entries []jj.StatusEntry }
type diffLoadedMsg struct{ content string }
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
		m.diffVP = viewport.New(msg.Width, msg.Height-3) // title + status + help
		m.diffVP.HighPerformanceRendering = false
		m.diffReady = true
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

	case diffLoadedMsg:
		m.diffContent = msg.content
		if m.diffReady {
			m.diffVP.SetContent(msg.content)
			m.diffVP.GotoTop()
		}
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
		case "q", "ctrl+c":
			if m.view == ViewHelp || m.view == ViewDiff {
				m.view = ViewLog
				return m, nil
			}
			return m, tea.Quit
		case "1":
			m.view = ViewLog
			m.message = "refreshing…"
			return m, m.loadLog()
		case "2":
			m.view = ViewStatus
			return m, m.loadStatus()
		case "?":
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
		case ViewDiff:
			return m.updateDiff(msg)
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}

	// ── Bottom bars (always present) ──
	helpBar := styleHelpBar.Width(m.width).Render(" 1:log  2:status  ?:help  r:refresh  q:quit ")

	var statusBar string
	if m.err != "" {
		statusBar = styleError.Width(m.width).Render(" ✖ " + truncate(m.err, m.width-4))
	} else if m.message != "" {
		statusBar = styleMuted.Width(m.width).Render(" " + m.message)
	} else {
		// Show current view + repo info
		statusBar = styleMuted.Width(m.width).Render(fmt.Sprintf(" %s view", m.view))
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
	case ViewStatus:
		content = m.viewStatus(contentHeight)
	case ViewDiff:
		content = m.viewDiff()
	case ViewHelp:
		content = m.viewHelp(contentHeight)
	}

	// Ensure content fills exactly contentHeight lines.
	content = padToHeight(content, contentHeight)

	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar, helpBar)
}

// ── Log View ────────────────────────────────────────────────────────────────

func (m Model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.view = ViewDiff
			return m, m.loadDiff(entry.CommitID)
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

func (m Model) viewLog(contentHeight int) string {
	if len(m.logEntries) == 0 {
		return styleMuted.Render("  no revisions found")
	}

	// Each entry takes 2 lines (subject + meta).
	linesPerEntry := 2
	visibleEntries := contentHeight / linesPerEntry
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

	var b strings.Builder
	for i := m.offset; i < end; i++ {
		e := m.logEntries[i]

		// Cursor indicator.
		cursor := " "
		if i == m.cursor {
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

		line := fmt.Sprintf("%s %s %s %s%s",
			cursor,
			style.Render(symbol),
			styleChangeID.Render(e.ChangeID),
			style.Render(subject),
			bookmarkStr,
		)

		meta := fmt.Sprintf("    %s  %s  %s",
			styleAuthor.Render(e.Authors),
			styleDate.Render(e.Date),
			styleCommitID.Render(e.CommitID),
		)

		if i == m.cursor {
			line = styleSelected.Width(m.width).Render(line)
			meta = styleSelected.Width(m.width).Render(meta)
		}

		b.WriteString(line)
		b.WriteString("\n")
		b.WriteString(meta)
		b.WriteString("\n")
	}

	return b.String()
}

// ── Status View ─────────────────────────────────────────────────────────────

func (m Model) viewStatus(contentHeight int) string {
	title := styleTitle.Width(m.width).Render(" Status ")

	if len(m.statusEntries) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			styleMuted.Render("  working copy clean ✓"),
		)
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")

	for _, e := range m.statusEntries {
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

	return b.String()
}

// ── Diff View ───────────────────────────────────────────────────────────────

func (m Model) updateDiff(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.diffVP.LineUp(1)
	case "down", "j":
		m.diffVP.LineDown(1)
	case "pgup", "b":
		m.diffVP.HalfViewUp()
	case "pgdown", "f":
		m.diffVP.HalfViewDown()
	case "G":
		m.diffVP.GotoBottom()
	case "g":
		m.diffVP.GotoTop()
	}
	return m, nil
}

func (m Model) viewDiff() string {
	title := styleTitle.Width(m.width).Render(" Diff: " + m.diffRev + " ")
	if m.diffReady {
		return lipgloss.JoinVertical(lipgloss.Left, title, m.diffVP.View())
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, styleMuted.Render("  loading diff…"))
}

// ── Help View ───────────────────────────────────────────────────────────────

func (m Model) viewHelp(contentHeight int) string {
	title := styleTitle.Width(m.width).Render(" gojo — keybindings ")

	help := []struct{ key, desc string }{
		{"Global", ""},
		{"  1", "log view"},
		{"  2", "status view"},
		{"  ?", "this help"},
		{"  r", "refresh current view"},
		{"  q", "quit / go back"},
		{"", ""},
		{"Log View", ""},
		{"  ↑/k, ↓/j", "navigate commits"},
		{"  g", "jump to first commit"},
		{"  G", "jump to last commit"},
		{"  enter/d", "show diff for selected commit"},
		{"  e", "edit (checkout) selected commit"},
		{"  n", "create new change"},
		{"", ""},
		{"Diff View", ""},
		{"  ↑/k, ↓/j", "scroll diff"},
		{"  pgup/b", "scroll up half page"},
		{"  pgdn/f", "scroll down half page"},
		{"  g", "scroll to top"},
		{"  G", "scroll to bottom"},
		{"  q", "back to log"},
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
			b.WriteString(fmt.Sprintf("  %-12s %s\n", styleCommitID.Render(h.key), h.desc))
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

func (m Model) loadDiff(rev string) tea.Cmd {
	return func() tea.Msg {
		content, err := m.runner.Diff(context.Background(), rev)
		if err != nil {
			return errMsg{err}
		}
		return diffLoadedMsg{content}
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
	switch m.view {
	case ViewLog:
		return m.loadLog()
	case ViewStatus:
		return m.loadStatus()
	default:
		return m.loadLog()
	}
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
