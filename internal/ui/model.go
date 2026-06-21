// Package ui implements the gojo terminal interface with Bubble Tea + Lip Gloss.
package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gojo/internal/jj"
)

type viewMode int

const (
	viewLog viewMode = iota
	viewHelp
)

// Model is the root Bubble Tea model.
type Model struct {
	width, height int

	cfg      jj.Config
	runner   *jj.Runner
	ready    bool
	bootErr  string
	repoRoot string
	cwd      string
	home     string

	view viewMode

	entries       []jj.LogEntry
	cursor        int
	offset        int
	statusEntries []jj.StatusEntry
	message       string
	errMsg        string

	// Diff panel.
	diffOpen    bool
	diffRev     string
	diffLoading bool
	diffStatus  []jj.StatusEntry
	diffRows    []diffRow
	diffDigits  int // gutter width, computed once when the diff loads
	diffRaw     string
	diffScrollY int

	helpScrollY int

	// Bookmark mode.
	bookmarkMode   bool
	bookmarkAction string // "" | c d f m r s t T l
	bookmarkInput  string
	acOriginal     *string
	acIdx          int

	// Git / remote mode.
	gitMode      bool
	remoteMode   bool
	remoteAction string // "" | a l r m s
	remoteInput  string

	// AI describe.
	aiLoading      map[string]bool
	spinnerFrame   int
	spinnerRunning bool
}

// NewModel builds the initial model.
func NewModel() Model {
	cwd, _ := os.Getwd()
	return Model{
		view:      viewLog,
		cwd:       cwd,
		home:      os.Getenv("HOME"),
		aiLoading: map[string]bool{},
	}
}

// ── Messages ────────────────────────────────────────────────────────────────

type bootMsg struct {
	cfg jj.Config
	err error
}

type refreshMsg struct {
	entries []jj.LogEntry
	logErr  error
	status  []jj.StatusEntry
	statErr error
}

type diffLoadedMsg struct {
	rev    string
	status []jj.StatusEntry
	rows   []diffRow
	err    error
}

type actionDoneMsg struct {
	message string
	err     error
	refresh bool
}

type aiDoneMsg struct {
	changeID string
	message  string
	err      error
}

type describeFinishedMsg struct {
	changeID string
	err      error
}

type listLoadedMsg struct {
	title   string
	content string
	err     error
}

type spinnerTickMsg struct{}

// ── Init ────────────────────────────────────────────────────────────────────

// Init kicks off configuration loading.
func (m Model) Init() tea.Cmd {
	return boot
}

func boot() tea.Msg {
	cfg, err := jj.LoadConfig()
	return bootMsg{cfg: cfg, err: err}
}

// ── Commands ────────────────────────────────────────────────────────────────

func (m Model) refreshCmd() tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		entries, logErr := r.Log(50)
		status, statErr := r.Status()
		return refreshMsg{entries: entries, logErr: logErr, status: status, statErr: statErr}
	}
}

func (m Model) openDiffCmd(commitID, changeID string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		status, _ := r.DiffSummary(commitID)
		diff, err := r.Diff(commitID)
		if err != nil {
			return diffLoadedMsg{rev: changeID, err: err}
		}
		return diffLoadedMsg{rev: changeID, status: status, rows: renderDiff(diff)}
	}
}

func (m Model) simpleCmd(fn func() error, okMsg string) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: okMsg, refresh: true}
	}
}

func (m Model) aiCmd(changeID string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		msg, err := r.AIDescribe(changeID)
		if err != nil {
			return aiDoneMsg{changeID: changeID, err: err}
		}
		if err := r.Describe(changeID, msg); err != nil {
			return aiDoneMsg{changeID: changeID, err: err}
		}
		return aiDoneMsg{changeID: changeID, message: msg}
	}
}

func (m Model) describeCmd(changeID string) tea.Cmd {
	c := exec.Command(m.cfg.JJPath, "describe", "-r", changeID)
	c.Dir = m.cfg.RepoRoot
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return describeFinishedMsg{changeID: changeID, err: err}
	})
}

func listCmd(fn func() (string, error), title string) tea.Cmd {
	return func() tea.Msg {
		out, err := fn()
		return listLoadedMsg{title: title, content: out, err: err}
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// ── Update ──────────────────────────────────────────────────────────────────

// Update handles incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.recomputeOffset()
		return m, nil

	case bootMsg:
		if msg.err != nil {
			m.bootErr = msg.err.Error()
			return m, nil
		}
		m.cfg = msg.cfg
		m.runner = jj.NewRunner(msg.cfg)
		m.repoRoot = msg.cfg.RepoRoot
		m.ready = true
		m.message = "refreshing…"
		return m, m.refreshCmd()

	case refreshMsg:
		if msg.logErr != nil {
			m.errMsg = msg.logErr.Error()
		} else {
			m.entries = msg.entries
			m.errMsg = ""
			m.message = ""
		}
		if msg.statErr != nil {
			m.errMsg = msg.statErr.Error()
		} else {
			m.statusEntries = msg.status
		}
		if m.cursor >= len(m.entries) {
			m.cursor = len(m.entries) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.recomputeOffset()
		return m, nil

	case diffLoadedMsg:
		m.diffLoading = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.diffStatus = msg.status
		m.diffRows = msg.rows
		m.diffDigits = maxLineDigits(msg.rows)
		return m, nil

	case actionDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.message = msg.message
		if msg.refresh {
			return m, m.refreshCmd()
		}
		return m, nil

	case listLoadedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.diffOpen = true
		m.diffRev = msg.title
		m.diffRaw = msg.content
		m.diffRows = nil
		m.diffStatus = nil
		m.diffLoading = false
		m.diffScrollY = 0
		return m, nil

	case aiDoneMsg:
		delete(m.aiLoading, msg.changeID)
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.message = "AI described " + msg.changeID + ": " + msg.message
		return m, m.refreshCmd()

	case describeFinishedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.message = "described " + msg.changeID
		}
		return m, m.refreshCmd()

	case spinnerTickMsg:
		m.spinnerFrame++
		if len(m.aiLoading) > 0 {
			return m, spinnerTick()
		}
		m.spinnerRunning = false
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) recomputeOffset() {
	if len(m.entries) == 0 {
		m.offset = 0
		return
	}
	if m.cursor >= len(m.entries) {
		m.cursor = len(m.entries) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	avail := m.contentHeight() - 1
	m.offset, _ = logWindow(m.entries, m.cursor, m.offset, avail)
}

func (m Model) contentHeight() int {
	h := m.height - 4
	if m.suggestionsVisible() {
		h--
	}
	if h < 0 {
		h = 0
	}
	return h
}

// diffMaxScroll is the furthest scroll offset that still shows content.
func (m Model) diffMaxScroll() int {
	total := len(m.diffRows)
	if total == 0 && m.diffRaw != "" {
		total = strings.Count(m.diffRaw, "\n") + 1
	}
	return max(0, total-1)
}

func (m Model) selectedEntry() *jj.LogEntry {
	if len(m.entries) == 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}

// ── Keyboard ────────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	k := msg.String()

	if k == "ctrl+c" {
		return m, tea.Quit
	}

	if m.bookmarkMode {
		return m.handleBookmarkKey(msg, k)
	}

	if m.gitMode {
		return m.handleGitKey(msg, k)
	}

	// Global keys.
	if k == "q" {
		if m.view == viewHelp {
			m.view = viewLog
			return m, nil
		}
		if m.diffOpen {
			m.diffOpen = false
			return m, nil
		}
		return m, tea.Quit
	}
	if k == "?" {
		if m.diffOpen {
			m.diffOpen = false
			return m, nil
		}
		if m.view != viewHelp {
			m.helpScrollY = 0
			m.view = viewHelp
		} else {
			m.view = viewLog
		}
		return m, nil
	}

	if m.view == viewHelp {
		return m.handleHelpKey(k), nil
	}

	// Log view.
	if m.diffOpen {
		switch k {
		case "enter", "q", "esc":
			m.diffOpen = false
		case "up", "k":
			if m.diffScrollY > 0 {
				m.diffScrollY--
			}
		case "down", "j":
			if m.diffScrollY < m.diffMaxScroll() {
				m.diffScrollY++
			}
		}
		return m, nil
	}

	return m.handleLogKey(msg, k)
}

func (m Model) handleHelpKey(k string) Model {
	contentH := m.height - 5
	maxS := helpMaxScroll(contentH)
	half := max(1, contentH/2)
	switch k {
	case "up", "k":
		m.helpScrollY = max(0, m.helpScrollY-1)
	case "down", "j":
		m.helpScrollY = min(maxS, m.helpScrollY+1)
	case "home", "g":
		m.helpScrollY = 0
	case "end", "G":
		m.helpScrollY = maxS
	case "pgup", "b":
		m.helpScrollY = max(0, m.helpScrollY-half)
	case "pgdown", "f":
		m.helpScrollY = min(maxS, m.helpScrollY+half)
	}
	return m
}

func (m Model) handleLogKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	switch k {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.recomputeOffset()
		return m, nil
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
		m.recomputeOffset()
		return m, nil
	case "home":
		m.cursor = 0
		m.recomputeOffset()
		return m, nil
	case "end", "G":
		m.cursor = len(m.entries) - 1
		m.recomputeOffset()
		return m, nil
	case "enter":
		if e := m.selectedEntry(); e != nil {
			m.diffOpen = true
			m.diffRev = e.ChangeID
			m.diffLoading = true
			m.diffScrollY = 0
			m.diffRaw = ""
			m.diffRows = nil
			m.diffStatus = nil
			return m, m.openDiffCmd(e.CommitID, e.ChangeID)
		}
		return m, nil
	case "d":
		if e := m.selectedEntry(); e != nil {
			return m, m.describeCmd(e.ChangeID)
		}
		return m, nil
	case "D":
		if e := m.selectedEntry(); e != nil {
			m.aiLoading[e.ChangeID] = true
			m.errMsg = ""
			m.message = "AI generating message for " + e.ChangeID + "…"
			cmds := []tea.Cmd{m.aiCmd(e.ChangeID)}
			if !m.spinnerRunning {
				m.spinnerRunning = true
				cmds = append(cmds, spinnerTick())
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil
	case "e":
		if e := m.selectedEntry(); e != nil {
			rev := e.ChangeID
			r := m.runner
			return m, m.simpleCmd(func() error { return r.Edit(rev) }, "editing "+rev)
		}
		return m, nil
	case "n":
		rev := ""
		if e := m.selectedEntry(); e != nil {
			rev = e.ChangeID
		}
		r := m.runner
		return m, m.simpleCmd(func() error { return r.New(rev) }, "created new change")
	case "a":
		if e := m.selectedEntry(); e != nil {
			if e.IsWorkingCopy {
				m.errMsg = "cannot abandon the working copy"
				return m, nil
			}
			rev := e.ChangeID
			r := m.runner
			return m, m.simpleCmd(func() error { return r.Abandon(rev) }, "abandoned "+rev)
		}
		return m, nil
	case "b":
		m.bookmarkMode = true
		m.bookmarkAction = ""
		m.bookmarkInput = ""
		m.acOriginal = nil
		m.acIdx = 0
		m.errMsg = ""
		m.message = ""
		return m, nil
	case "g":
		m.gitMode = true
		m.errMsg = ""
		m.message = ""
		return m, nil
	case "u":
		r := m.runner
		return m, m.simpleCmd(func() error { return r.Undo() }, "undone")
	case "r":
		r := m.runner
		return m, m.simpleCmd(func() error { return r.Redo() }, "redone")
	}
	return m, nil
}

func (m Model) handleBookmarkKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	if m.bookmarkAction != "" {
		switch k {
		case "esc":
			if m.acOriginal != nil {
				m.bookmarkInput = *m.acOriginal
				m.acOriginal = nil
				m.acIdx = 0
				return m, nil
			}
			m.bookmarkAction = ""
			m.bookmarkInput = ""
			m.acOriginal = nil
			m.acIdx = 0
			return m, nil
		case "enter":
			action := m.bookmarkAction
			input := m.bookmarkInput
			m.acOriginal = nil
			m.acIdx = 0
			m.bookmarkMode = false
			m.bookmarkAction = ""
			m.bookmarkInput = ""
			return m, m.execBookmark(action, input)
		case "tab":
			prefix := m.bookmarkInput
			if m.acOriginal != nil {
				prefix = *m.acOriginal
			}
			filtered := filterPrefix(m.candidates(), prefix)
			if len(filtered) > 0 {
				if m.acOriginal == nil {
					orig := m.bookmarkInput
					m.acOriginal = &orig
					m.acIdx = 0
					m.bookmarkInput = filtered[0]
				} else {
					m.acIdx = (m.acIdx + 1) % len(filtered)
					m.bookmarkInput = filtered[m.acIdx]
				}
			}
			return m, nil
		case "backspace", "delete":
			m.bookmarkInput = trimLastRune(m.bookmarkInput)
			m.acOriginal = nil
			m.acIdx = 0
			return m, nil
		}
		if s, ok := typed(msg); ok {
			m.bookmarkInput += s
			m.acOriginal = nil
			m.acIdx = 0
		}
		return m, nil
	}

	// Bookmark menu.
	switch k {
	case "esc", "q":
		m.bookmarkMode = false
		m.acOriginal = nil
		m.acIdx = 0
		return m, nil
	case "c", "d", "f", "m", "r", "s", "t", "T":
		m.bookmarkAction = k
		m.bookmarkInput = ""
		m.acOriginal = nil
		m.acIdx = 0
		return m, nil
	case "l":
		m.bookmarkMode = false
		return m, m.execBookmark("l", "")
	}
	return m, nil
}

func (m Model) handleGitKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	if m.remoteMode {
		if m.remoteAction != "" {
			switch k {
			case "esc":
				m.remoteAction = ""
				m.remoteInput = ""
				return m, nil
			case "enter":
				action := m.remoteAction
				input := m.remoteInput
				m.remoteMode = false
				m.remoteAction = ""
				m.remoteInput = ""
				m.gitMode = false
				return m, m.execRemote(action, input)
			case "backspace", "delete":
				m.remoteInput = trimLastRune(m.remoteInput)
				return m, nil
			}
			if s, ok := typed(msg); ok {
				m.remoteInput += s
			}
			return m, nil
		}
		// Remote menu.
		switch k {
		case "esc", "q":
			m.remoteMode = false
			return m, nil
		case "l":
			m.remoteMode = false
			m.gitMode = false
			return m, m.execRemote("l", "")
		case "a", "r", "m", "s":
			m.remoteAction = k
			m.remoteInput = ""
			return m, nil
		}
		return m, nil
	}

	switch k {
	case "esc", "q":
		m.gitMode = false
		return m, nil
	case "f":
		m.gitMode = false
		r := m.runner
		return m, m.simpleCmd(func() error { return r.GitFetch() }, "fetched")
	case "p":
		m.gitMode = false
		r := m.runner
		return m, m.simpleCmd(func() error { return r.GitPush() }, "pushed")
	case "r":
		m.remoteMode = true
		m.remoteAction = ""
		m.remoteInput = ""
		return m, nil
	}
	return m, nil
}

func (m Model) execBookmark(action, input string) tea.Cmd {
	r := m.runner
	rev := ""
	if e := m.selectedEntry(); e != nil {
		rev = e.ChangeID
	}
	if action == "l" {
		return listCmd(r.BookmarkList, "bookmark list")
	}
	return func() tea.Msg {
		var err error
		switch action {
		case "c":
			err = r.BookmarkCreate(input, rev)
		case "d":
			err = r.BookmarkDelete(input)
		case "f":
			err = r.BookmarkForget(input)
		case "m":
			err = r.BookmarkMove(input, rev)
		case "r":
			parts := strings.Fields(input)
			if len(parts) < 2 {
				err = errors.New("rename requires: <old> <new>")
			} else {
				err = r.BookmarkRename(parts[0], parts[1])
			}
		case "s":
			err = r.BookmarkSet(input, rev)
		case "t":
			err = r.BookmarkTrack(input)
		case "T":
			err = r.BookmarkUntrack(input)
		}
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: "bookmark " + action + ": " + input, refresh: true}
	}
}

func (m Model) execRemote(action, input string) tea.Cmd {
	r := m.runner
	if action == "l" {
		return listCmd(r.RemoteList, "remote list")
	}
	return func() tea.Msg {
		var err error
		switch action {
		case "a":
			parts := strings.Fields(input)
			if len(parts) < 2 {
				err = errors.New("add requires: <name> <url>")
			} else {
				err = r.RemoteAdd(parts[0], strings.Join(parts[1:], " "))
			}
		case "r":
			err = r.RemoteRemove(input)
		case "m":
			parts := strings.Fields(input)
			if len(parts) < 2 {
				err = errors.New("rename requires: <old> <new>")
			} else {
				err = r.RemoteRename(parts[0], parts[1])
			}
		case "s":
			parts := strings.Fields(input)
			if len(parts) < 2 {
				err = errors.New("set-url requires: <name> <url>")
			} else {
				err = r.RemoteSetURL(parts[0], strings.Join(parts[1:], " "))
			}
		}
		if err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: "remote " + action + ": " + input, refresh: true}
	}
}

// ── Autocomplete helpers ────────────────────────────────────────────────────

func (m Model) candidates() []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, e := range m.entries {
		for _, bm := range e.Bookmarks {
			if bm != "" && !strings.Contains(bm, "@") {
				add(bm)
			}
		}
		add(e.ChangeID)
		add(e.CommitID)
	}
	return out
}

func filterPrefix(cands []string, prefix string) []string {
	if prefix == "" {
		return cands
	}
	var out []string
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (m Model) displaySuggestions() []string {
	if m.bookmarkAction == "" {
		return nil
	}
	if m.bookmarkAction == "r" && strings.Contains(m.bookmarkInput, " ") {
		return nil
	}
	prefix := m.bookmarkInput
	if m.acOriginal != nil {
		prefix = *m.acOriginal
	}
	filtered := filterPrefix(m.candidates(), prefix)
	if len(filtered) > 10 {
		filtered = filtered[:10]
	}
	return filtered
}

func (m Model) suggestionsVisible() bool {
	return m.bookmarkAction != "" && len(m.displaySuggestions()) > 0
}

func typed(msg tea.KeyMsg) (string, bool) {
	switch msg.Type {
	case tea.KeySpace:
		return " ", true
	case tea.KeyRunes:
		if msg.Alt || len(msg.Runes) == 0 {
			return "", false
		}
		return string(msg.Runes), true
	}
	return "", false
}

func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// ── View ────────────────────────────────────────────────────────────────────

// View renders the full screen.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.bootErr != "" {
		lines := []string{plainRow(m.width, seg{text: " error: " + m.bootErr + " ", fg: colRed})}
		return strings.Join(padLines(lines, m.height), "\n")
	}
	if !m.ready || (len(m.entries) == 0 && m.errMsg == "") {
		lines := []string{plainRow(m.width, seg{text: " loading…", fg: colGray})}
		return strings.Join(padLines(lines, m.height), "\n")
	}

	var lines []string

	// Top bar (2 lines).
	dp := m.cwd
	if m.home != "" && strings.HasPrefix(m.cwd, m.home) {
		dp = "~" + m.cwd[len(m.home):]
	}
	titleBarPad := max(0, m.width-10-len([]rune(dp)))
	lines = append(lines, bgRow(m.width, colDarkPurple,
		seg{text: " ◉ gojo", fg: colPurple},
		seg{text: " " + strings.Repeat("─", titleBarPad) + " ", fg: colDarkGray},
		seg{text: dp + " ", fg: colWhite},
	))
	lines = append(lines, blankRow(m.width, colDarkPurple))

	// Content area.
	ch := m.contentHeight()
	switch {
	case m.view == viewHelp:
		lines = append(lines, renderHelp(m.width, ch, m.helpScrollY)...)
	case m.diffOpen:
		lines = append(lines, renderDiffPanel(m.width, ch, m.diffRev, m.diffLoading, m.diffRows, m.diffDigits, m.diffStatus, m.diffRaw, m.diffScrollY)...)
	default:
		lines = append(lines, renderLog(m.width, ch, m.entries, m.cursor, m.offset, m.aiLoading, m.spinnerFrame)...)
	}

	// Autocomplete suggestions.
	if m.suggestionsVisible() {
		lines = append(lines, m.renderSuggestions())
	}

	// Status bar + help bar.
	lines = append(lines, m.renderStatusBar())
	lines = append(lines, m.renderHelpBar())

	return strings.Join(padLines(lines, m.height), "\n")
}

func (m Model) renderSuggestions() string {
	sugg := m.displaySuggestions()
	segs := []seg{{text: " tab:", fg: colDarkGray}}
	activeIdx := -1
	if m.acOriginal != nil {
		activeIdx = m.acIdx
	}
	for i, s := range sugg {
		color := colCyan
		if i == activeIdx {
			color = colYellow
		}
		prefix := " "
		if i > 0 {
			prefix = " · "
		}
		segs = append(segs, seg{text: prefix + s, fg: color})
	}
	return bgRow(m.width, colDarkerGray, segs...)
}

func (m Model) renderStatusBar() string {
	switch {
	case m.bookmarkMode:
		if m.bookmarkAction != "" {
			prompts := map[string]string{
				"c": "create: ", "d": "delete: ", "f": "forget: ",
				"m": "move to " + m.selChangeID() + ": ",
				"r": "rename (old new): ",
				"s": "set to " + m.selChangeID() + ": ",
				"t": "track: ", "T": "untrack: ",
			}
			text := " [bookmark] " + prompts[m.bookmarkAction] + m.bookmarkInput + "█"
			return bgRow(m.width, colDarkerGray, seg{text: text, fg: colCyan})
		}
		segs := []seg{{text: " [bookmark mode] ", fg: colCyan}}
		segs = append(segs, hlSegs([][2]string{
			{"create", "c"}, {"delete", "d"}, {"forget", "f"},
			{"list", "l"}, {"move", "m"}, {"rename", "r"},
			{"set", "s"}, {"track", "t"}, {"untrack", "T"},
		}, colCyan, colPurple, " ")...)
		segs = append(segs, seg{text: "  ", fg: colCyan})
		segs = append(segs, hlSegs([][2]string{{"cancel", "esc"}}, colCyan, colPurple, " ")...)
		segs = append(segs, seg{text: " ", fg: colCyan})
		return bgRow(m.width, colDarkerGray, segs...)

	case m.gitMode:
		if m.remoteMode {
			if m.remoteAction != "" {
				prompts := map[string]string{
					"a": "add (name url): ",
					"r": "remove (name): ",
					"m": "rename (old new): ",
					"s": "set-url (name url): ",
				}
				text := " [git > remote] " + prompts[m.remoteAction] + m.remoteInput + "█"
				return bgRow(m.width, colDarkerGray, seg{text: text, fg: colPink})
			}
			segs := []seg{{text: " [git > remote] ", fg: colPink}}
			segs = append(segs, hlSegs([][2]string{
				{"add", "a"}, {"list", "l"}, {"remove", "r"},
				{"rename", "m"}, {"set-url", "s"},
			}, colPink, colPurple, " ")...)
			segs = append(segs, seg{text: "  ", fg: colPink})
			segs = append(segs, hlSegs([][2]string{{"cancel", "esc"}}, colPink, colPurple, " ")...)
			segs = append(segs, seg{text: " ", fg: colPink})
			return bgRow(m.width, colDarkerGray, segs...)
		}
		segs := []seg{{text: " [git mode] ", fg: colDarkOrange}}
		segs = append(segs, hlSegs([][2]string{
			{"fetch", "f"}, {"push", "p"}, {"remote", "r"},
		}, colDarkOrange, colPurple, " ")...)
		segs = append(segs, seg{text: "  ", fg: colDarkOrange})
		segs = append(segs, hlSegs([][2]string{{"cancel", "esc"}}, colDarkOrange, colPurple, " ")...)
		segs = append(segs, seg{text: " ", fg: colDarkOrange})
		return bgRow(m.width, colDarkerGray, segs...)

	case m.errMsg != "":
		msg := m.errMsg
		limit := m.width - 4
		if limit > 0 && len(msg) > limit {
			msg = msg[:limit]
		}
		return bgRow(m.width, colDarkerGray, seg{text: " ✖ " + msg, fg: colRed})

	case m.message != "":
		return bgRow(m.width, colDarkerGray, seg{text: " " + m.message, fg: colGray})

	case len(m.statusEntries) > 0:
		return bgRow(m.width, colDarkerGray, seg{text: fmt.Sprintf(" %d changed file(s)", len(m.statusEntries)), fg: colGray})

	default:
		return bgRow(m.width, colDarkerGray, seg{text: " clean working copy ✓", fg: colGray})
	}
}

func (m Model) selChangeID() string {
	if e := m.selectedEntry(); e != nil {
		return e.ChangeID
	}
	return ""
}

func (m Model) renderHelpBar() string {
	segs := []seg{{text: " ", fg: colGray}}
	segs = append(segs, hlSegs([][2]string{
		{"⏎diff", "⏎"}, {"describe", "d"},
		{"AI Desc", "D"}, {"bookmark", "b"}, {"git", "g"},
		{"undo", "u"}, {"redo", "r"}, {"edit", "e"}, {"new", "n"},
		{"abandon", "a"}, {"?help", "?"}, {"quit", "q"},
	}, colGray, colPurple, "  ")...)
	return bgRow(m.width, colDarkerGray, segs...)
}

func hlSegs(items [][2]string, base, hlc lipgloss.TerminalColor, sep string) []seg {
	var out []seg
	for i, it := range items {
		text, match := it[0], it[1]
		idx := strings.Index(text, match)
		if idx < 0 {
			out = append(out, seg{text: text, fg: base})
		} else {
			if idx > 0 {
				out = append(out, seg{text: text[:idx], fg: base})
			}
			out = append(out, seg{text: match, fg: hlc, underline: true})
			if idx+len(match) < len(text) {
				out = append(out, seg{text: text[idx+len(match):], fg: base})
			}
		}
		if i < len(items)-1 {
			out = append(out, seg{text: sep, fg: base})
		}
	}
	return out
}
