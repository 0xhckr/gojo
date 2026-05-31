package ui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hackr/gojo/internal/ai"
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

// Spinner frames — braille dot rotation (muload-style).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Model is the top-level Bubble Tea model.
type Model struct {
	runner   *jj.Runner
	aiClient *ai.Client
	view     View

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

	// AI describe state.
	aiSpinnerFrame int
	aiLoading      map[string]bool // set of revs currently being AI-described

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

// AI describe messages.
type aiDescribeTickMsg struct{}
type aiDescribeDoneMsg struct {
	rev     string
	message string
}
type aiDescribeErrorMsg struct {
	rev string
	err error
}

func NewModel(runner *jj.Runner, aiClient *ai.Client) Model {
	return Model{
		runner:   runner,
		aiClient: aiClient,
		view:     ViewLog,
		aiLoading: make(map[string]bool),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadLog(), m.loadStatus(), tea.EnableReportFocus)
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

	case tea.FocusMsg:
		m.message = "refreshing…"
		return m, m.refresh()

	// ── AI describe messages ──
	case aiDescribeTickMsg:
		if len(m.aiLoading) > 0 {
			m.aiSpinnerFrame = (m.aiSpinnerFrame + 1) % len(spinnerFrames)
			return m, m.aiSpinnerTick()
		}
		return m, nil

	case aiDescribeDoneMsg:
		delete(m.aiLoading, msg.rev)
		m.message = fmt.Sprintf("AI described %s: %s", msg.rev, truncate(msg.message, 60))
		m.err = ""
		// Reload log to show new descriptions; keep spinner if others in-flight.
		return m, m.loadLog()

	case aiDescribeErrorMsg:
		delete(m.aiLoading, msg.rev)
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

	helpBar := styleHelpBar.Width(m.width).Render(" enter:diff  d:describe  D:AI msg  ↑↓+D:multi  e:edit  n:new  a:abandon  ?:help  r:refresh  q:quit ")

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
	case "up", "k", "shift+up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "shift+down":
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
	case "D":
		if m.aiClient == nil {
			m.err = "no OpenRouter API key — add openrouter_api_key to ~/.config/gojo/gojo.toml"
			m.message = ""
			return m, nil
		}
		if len(m.logEntries) > 0 && m.cursor < len(m.logEntries) {
			entry := m.logEntries[m.cursor]
			if m.aiLoading[entry.ChangeID] {
				return m, nil // already generating for this rev
			}
			m.aiLoading[entry.ChangeID] = true
			m.err = ""
			m.message = ""
			cmds := []tea.Cmd{m.aiDescribe(entry.ChangeID)}
			// Start spinner if this is the first in-flight request.
			if len(m.aiLoading) == 1 {
				m.aiSpinnerFrame = 0
				cmds = append(cmds, m.aiSpinnerTick())
			}
			return m, tea.Batch(cmds...)
		}
	case "e":
		if len(m.logEntries) > 0 {
			entry := m.logEntries[m.cursor]
			m.message = "editing " + entry.ChangeID + "…"
			return m, m.editRev(entry.ChangeID)
		}
	case "a":
		if len(m.logEntries) > 0 && m.cursor < len(m.logEntries) {
			entry := m.logEntries[m.cursor]
			if entry.IsWorkingCopy {
				m.err = "cannot abandon the working copy"
				return m, nil
			}
			m.message = "abandoning " + entry.ChangeID + "…"
			return m, m.abandonRev(entry.ChangeID)
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
	// Each entry is 2 lines + optional edge lines from jj's graph.
	logHeight := contentHeight
	availableLines := logHeight - 1 // -1 for top padding
	if availableLines < 2 {
		availableLines = 2
	}

	// commitDisplayLines returns how many terminal lines a commit occupies.
	commitDisplayLines := func(idx int) int {
		return 2 + len(m.logEntries[idx].EdgeLines)
	}

	// Ensure cursor is within offset range.
	if m.cursor < m.offset {
		m.offset = m.cursor
	}

	// Walk from offset, count how many commits fit.
	end := m.offset
	usedLines := 0
	for end < len(m.logEntries) {
		h := commitDisplayLines(end)
		if usedLines+h > availableLines && end > m.offset {
			break
		}
		usedLines += h
		end++
	}

	// If cursor is past the visible range, recalculate from cursor.
	if m.cursor >= end {
		end = m.cursor + 1
		usedLines = commitDisplayLines(m.cursor)
		m.offset = m.cursor
		for m.offset > 0 {
			h := commitDisplayLines(m.offset - 1)
			if usedLines+h > availableLines {
				break
			}
			usedLines += h
			m.offset--
		}
	}

	for i := m.offset; i < end; i++ {
		e := m.logEntries[i]

		// Edge lines from graph branching (attached to this commit from parsing).
		for _, edge := range e.EdgeLines {
			b.WriteString(styleGraph.Render(edge))
			b.WriteString("\n")
		}

		// Style the node character in the header graph prefix.
		headerPrefix := styleNodeInPrefix(e)

		var bookmarkStr string
		if len(e.Bookmarks) > 0 {
			var bms []string
			for _, bm := range e.Bookmarks {
				bms = append(bms, styleBookmark.Render(bm))
			}
			bookmarkStr = " " + strings.Join(bms, " ")
		}

		// Line 1: graph_prefix + change_id author date commit_id bookmarks
		header := fmt.Sprintf("%s%s %s %s %s%s",
			headerPrefix,
			styleChangeID.Render(e.ChangeID),
			styleAuthor.Render(e.Authors),
			styleDate.Render(e.Date),
			styleCommitID.Render(e.CommitID),
			bookmarkStr,
		)

		// Line 2: graph body_prefix + subject
		entryStyle := styleSubject
		if e.IsWorkingCopy {
			entryStyle = styleWorkingCopy
		} else if e.IsImmutable {
			entryStyle = styleImmutable
		}

		var body string
		if m.aiLoading[e.ChangeID] {
			frame := spinnerFrames[m.aiSpinnerFrame]
			body = fmt.Sprintf("%s%s", styleGraph.Render(e.BodyPrefix), styleSpinner.Render(fmt.Sprintf(" %s generating…", frame)))
		} else {
			subject := e.Subject
			if subject == "" {
				subject = "(no description set)"
			}
			body = fmt.Sprintf("%s %s", styleGraph.Render(e.BodyPrefix), entryStyle.Render(subject))
		}

		if i == m.cursor {
			header = highlightLine(header, colorDarkPurple, m.width)
			body = highlightLine(body, colorDarkPurple, m.width)
		}

		b.WriteString(header)
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}

	return b.String()
}

// styleNodeInPrefix replaces the node character (@/○) in the graph prefix
// with a styled version based on commit type. Graph edge characters before
// the node are dimmed.
func styleNodeInPrefix(e jj.LogEntry) string {
	prefix := e.HeaderPrefix
	runes := []rune(prefix)

	// Find the last node character in the prefix.
	nodeIdx := -1
	for i := len(runes) - 1; i >= 0; i-- {
		r := runes[i]
		if r == '@' || r == '○' || r == '◆' {
			nodeIdx = i
			break
		}
	}

	if nodeIdx < 0 {
		return styleGraph.Render(prefix)
	}

	// Determine styled node.
	var styledNode string
	switch {
	case e.IsWorkingCopy:
		styledNode = styleWorkingCopy.Render("@")
	case e.IsImmutable:
		styledNode = styleImmutable.Render("◆")
	default:
		styledNode = styleGraph.Render("○")
	}

	before := string(runes[:nodeIdx])
	after := string(runes[nodeIdx+1:])

	var result string
	if before != "" {
		result = styleGraph.Render(before)
	}
	result += styledNode
	result += after // trailing spaces, no styling needed
	return result
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
		{"  D", "AI generate commit msg (multi)",},
		{"  e", "jj edit (checkout commit)"},
		{"  n", "jj new (create change)"},
		{"  a", "jj abandon (remove commit)"},
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

func (m Model) abandonRev(rev string) tea.Cmd {
	return func() tea.Msg {
		if err := m.runner.Abandon(context.Background(), rev); err != nil {
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

// aiSpinnerTick returns a command that ticks the spinner every 100ms.
func (m Model) aiSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return aiDescribeTickMsg{}
	})
}

// aiDescribe fetches the diff, sends it to OpenRouter, and applies the result.
func (m Model) aiDescribe(rev string) tea.Cmd {
	return func() tea.Msg {
		diff, err := m.runner.Diff(context.Background(), rev)
		if err != nil {
			return aiDescribeErrorMsg{rev: rev, err: err}
		}

		msg, err := m.aiClient.GenerateCommitMessage(context.Background(), diff)
		if err != nil {
			return aiDescribeErrorMsg{rev: rev, err: err}
		}

		if err := m.runner.Describe(context.Background(), rev, msg); err != nil {
			return aiDescribeErrorMsg{rev: rev, err: err}
		}

		return aiDescribeDoneMsg{rev: rev, message: msg}
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
	if pad := width - lipgloss.Width(visible); pad > 0 {
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
