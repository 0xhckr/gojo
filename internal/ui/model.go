package ui

import (
	"context"
	"fmt"
	"os/exec"
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
	offset     int
	width      int
	height     int

	// Working copy status (for status bar).
	statusEntries []jj.StatusEntry

	// Diff panel state.
	diffOpen         bool
	diffRev          string
	diffVP           viewport.Model
	diffReady        bool
	diffLoading      bool
	revStatusEntries []jj.StatusEntry

	// Error/status message.
	err     string
	message string
}

// Messages for async operations.
type logLoadedMsg struct{ entries []jj.LogEntry }
type statusLoadedMsg struct{ entries []jj.StatusEntry }
type diffLoadedMsg struct{ content string }
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
		m.diffVP = viewport.New(msg.Width, 1)
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
		m.diffVP.SetContent(msg.content)
		m.diffVP.GotoTop()
		m.diffLoading = false
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

	case editorDoneMsg:
		m.message = "described " + msg.rev
		m.err = ""
		return m, m.loadLog()

	case editorErrorMsg:
		m.err = msg.err.Error()
		m.message = ""
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
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

	helpBar := styleHelpBar.Width(m.width).Render(" enter:diff  d:describe  e:edit  n:new  ?:help  r:refresh  q:quit ")

	var statusBar string
	if m.err != "" {
		statusBar = styleError.Width(m.width).Render(" ✖ " + truncate(m.err, m.width-4))
	} else if m.message != "" {
		statusBar = styleMuted.Width(m.width).Render(" " + m.message)
	} else {
		if len(m.statusEntries) > 0 {
			statusBar = styleMuted.Width(m.width).Render(fmt.Sprintf(" %d changed file(s)", len(m.statusEntries)))
		} else {
			statusBar = styleMuted.Width(m.width).Render(" clean working copy ✓")
		}
	}

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

	content = padToHeight(content, contentHeight)
	return lipgloss.JoinVertical(lipgloss.Left, content, statusBar, helpBar)
}

// ── Log View ────────────────────────────────────────────────────────────────

func (m Model) updateLog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "enter":
		if len(m.logEntries) > 0 && m.cursor < len(m.logEntries) {
			entry := m.logEntries[m.cursor]
			m.diffRev = entry.ChangeID
			m.revStatusEntries = nil
			m.diffOpen = true
			m.diffLoading = true
			m.message = ""
			return m, tea.Batch(m.loadDiff(entry.CommitID), m.loadRevStatus(entry.CommitID))
		}
	case "d":
		if len(m.logEntries) > 0 && m.cursor < len(m.logEntries) {
			entry := m.logEntries[m.cursor]
			m.message = "opening editor…"
			return m, m.describeRev(entry.ChangeID)
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
	case "enter":
		m.diffOpen = false
	}
	return m, nil
}

// diffPanelHeight returns how many lines the bottom diff panel takes.
func (m Model) diffPanelHeight(contentHeight int) int {
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

	// ── Panel (replaces commit list when open) ──
	if m.diffOpen {
		return m.viewDiffPanel(contentHeight)
	}

	// ── Commit list (full screen) ──
	logHeight := contentHeight

	visibleEntries := logHeight - 1 // -1 for top padding
	if visibleEntries < 1 {
		visibleEntries = 1
	}

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

		cursor := " "
		if i == m.cursor {
			cursor = styleCursor.Render("▸")
		}

		symbol := "○"
		style := styleSubject
		if e.IsWorkingCopy {
			symbol = "@"
			style = styleWorkingCopy
		} else if e.IsImmutable {
			symbol = "◆"
			style = styleImmutable
		}

		var bookmarkStr string
		if len(e.Bookmarks) > 0 {
			var bms []string
			for _, bm := range e.Bookmarks {
				bms = append(bms, styleBookmark.Render(bm))
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

	return b.String()
}

// ── Help View ───────────────────────────────────────────────────────────────

// viewDiffPanel shows status summary (top) and diff (bottom) for the selected revision.
func (m Model) viewDiffPanel(contentHeight int) string {
	var b strings.Builder
	b.WriteString("\n")

	// ── Title bar ──
	label := " " + m.diffRev
	if m.diffLoading {
		label += "  loading…"
	}
	label += "  (enter/q to close) "
	title := styleTitle.Width(m.width).Render(label)
	b.WriteString(title)
	b.WriteString("\n")

	// ── Status summary (top) ──
	statusTitle := styleMuted.Width(m.width).Render(" status ")
	b.WriteString(statusTitle)
	b.WriteString("\n")
	if len(m.revStatusEntries) == 0 {
		b.WriteString(styleMuted.Render("  (no changes)"))
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

	// ── Separator ──
	b.WriteString(styleMuted.Render(strings.Repeat("─", m.width)))
	b.WriteString("\n")

	// ── Diff (bottom, fills remaining space) ──
	linesUsed := strings.Count(b.String(), "\n")
	remaining := contentHeight - linesUsed
	if remaining < 1 {
		remaining = 1
	}
	m.diffVP.Width = m.width
	m.diffVP.Height = remaining
	b.WriteString(m.diffVP.View())
	b.WriteString("\n")

	return b.String()
}

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
		{"  enter", "open diff panel"},
		{"  d", "jj describe ($EDITOR)"},
		{"  e", "jj edit (checkout commit)"},
		{"  n", "jj new (create change)"},
		{"", ""},
		{"Diff Panel", ""},
		{"  ↑/k, ↓/j", "scroll diff"},
		{"  pgup/b, pgdn/f", "half-page scroll"},
		{"  g / G", "top / bottom"},
		{"  enter/q", "close diff"},
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

func (m Model) loadDiff(rev string) tea.Cmd {
	return func() tea.Msg {
		content, err := m.runner.Diff(context.Background(), rev)
		if err != nil {
			return errMsg{err}
		}
		return diffLoadedMsg{content}
	}
}

type editorDoneMsg struct{ rev string }

type editorErrorMsg struct{ err error }

func (m Model) describeRev(rev string) tea.Cmd {
	return tea.ExecProcess(
		exec.Command("jj", "describe", "-r", rev),
		func(err error) tea.Msg {
			if err != nil {
				return editorErrorMsg{err}
			}
			return editorDoneMsg{rev}
		},
	)
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

func highlightLine(line string, bg lipgloss.Color, width int) string {
	bgCode := fmt.Sprintf("\x1b[48;5;%sm", bg)
	replaced := strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bgCode)
	replaced = bgCode + replaced
	visible := stripAnsi(replaced)
	if pad := width - len(visible); pad > 0 {
		replaced += strings.Repeat(" ", pad)
	}
	replaced += "\x1b[0m"
	return replaced
}

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
