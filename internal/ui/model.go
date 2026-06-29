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

// Rebase placement options, indexed by Model.rebasePlace.
var (
	rebasePlaceFlags  = []string{"--onto", "--insert-after", "--insert-before"}
	rebasePlaceLabels = []string{"onto", "after", "before"}
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

	// showAllRev widens the log revset to "all()" instead of jj's default
	// (visible heads, minus remote-bookmark-only commits).
	showAllRev bool

	// Diff panel.
	diffOpen       bool
	diffRev        string
	diffIsRevision bool // true: showing a revision diff (reloadable); false: a list view
	diffLoading    bool
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

	// Rebase mode. Pick up the selected commit, then move a destination
	// indicator through the log to choose where it lands.
	rebaseMode    bool
	rebaseSource  int  // index into entries of the picked-up commit
	rebaseDest    int  // index into entries of the drop target (moves with j/k)
	rebaseSubtree bool // false → -r (single), true → -s (commit + descendants)
	rebasePlace   int  // index into rebasePlaceFlags: 0 onto, 1 after, 2 before

	// Squash mode. Pick the selected commit, then move a destination indicator
	// through the log to choose which commit to fold its changes into.
	squashMode   bool
	squashSource int // index into entries of the commit being squashed
	squashDest   int // index into entries of the target (moves with j/k)

	// AI describe.
	aiLoading      map[string]bool
	spinnerFrame   int
	spinnerRunning bool

	// Auto-refresh poll. Runs only while the terminal is focused so an idle or
	// backgrounded gojo isn't firing jj subprocesses every couple seconds.
	focused bool
	polling bool
}

// NewModel builds the initial model.
func NewModel() Model {
	cwd, _ := os.Getwd()
	return Model{
		view: viewLog,
		cwd:  cwd,
		home: os.Getenv("HOME"),
		// Terminal is focused at launch and the OS may not emit an initial
		// FocusMsg, so the poll loop (started in Init) runs from the start.
		focused:   true,
		polling:   true,
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

type squashFinishedMsg struct {
	from string
	into string
	err  error
}

type listLoadedMsg struct {
	title   string
	content string
	err     error
}

type spinnerTickMsg struct{}

type pollMsg struct{}

// ── Init ────────────────────────────────────────────────────────────────────

// Init kicks off configuration loading and the auto-refresh poll loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(boot, pollTick())
}

func boot() tea.Msg {
	cfg, err := jj.LoadConfig()
	return bootMsg{cfg: cfg, err: err}
}

// ── Commands ────────────────────────────────────────────────────────────────

func (m Model) refreshCmd() tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		var (
			entries []jj.LogEntry
			logErr  error
		)
		if m.showAllRev {
			// No -n cap: stream every revision down to the root. Rendering is
			// windowed (logview.go), so only visible rows are styled.
			entries, logErr = r.LogRevset("all()", 0)
		} else {
			entries, logErr = r.Log(50)
		}
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

// squashCmd folds the changes of `from` into `into`. Run via ExecProcess (not a
// captured subprocess) because jj opens $EDITOR to combine descriptions when
// both revisions have one — a captured run with no TTY would fail that case.
func (m Model) squashCmd(from, into string) tea.Cmd {
	c := exec.Command(m.cfg.JJPath, "squash", "--from", from, "--into", into)
	c.Dir = m.cfg.RepoRoot
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return squashFinishedMsg{from: from, into: into, err: err}
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

func pollTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return pollMsg{}
	})
}

// refreshFocusedCmds builds the refresh work shared by focus and poll: reload
// the log + status, plus the open diff when it's a revision view.
func (m Model) refreshFocusedCmds() []tea.Cmd {
	cmds := []tea.Cmd{m.refreshCmd()}
	if m.diffOpen && m.diffIsRevision {
		// diffRev is the change ID, a stable revset across working-copy edits.
		cmds = append(cmds, m.openDiffCmd(m.diffRev, m.diffRev))
	}
	return cmds
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
		m.diffIsRevision = false
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

	case squashFinishedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.message = "squashed " + msg.from + " into " + msg.into
		}
		return m, m.refreshCmd()

	case spinnerTickMsg:
		m.spinnerFrame++
		if len(m.aiLoading) > 0 {
			return m, spinnerTick()
		}
		m.spinnerRunning = false
		return m, nil

	case tea.FocusMsg:
		// Terminal regained focus: the working copy may have changed underneath
		// us (edits in another window, builds, etc.). Refresh immediately and
		// (re)start the poll loop if it isn't already running.
		if !m.ready {
			return m, nil
		}
		m.focused = true
		cmds := m.refreshFocusedCmds()
		if !m.polling {
			m.polling = true
			cmds = append(cmds, pollTick())
		}
		return m, tea.Batch(cmds...)

	case tea.BlurMsg:
		// Terminal lost focus: stop refreshing. The poll loop self-terminates on
		// its next tick when it sees !focused.
		m.focused = false
		return m, nil

	case pollMsg:
		// Drop the loop when unfocused or not ready; FocusMsg restarts it.
		if !m.ready || !m.focused {
			m.polling = false
			return m, nil
		}
		return m, tea.Batch(append(m.refreshFocusedCmds(), pollTick())...)

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
	// In rebase mode the destination indicator drives scrolling, so it stays
	// on screen as the user moves it.
	cur := m.cursor
	if m.rebaseMode {
		cur = min(max(0, m.rebaseDest), len(m.entries)-1)
	}
	if m.squashMode {
		cur = min(max(0, m.squashDest), len(m.entries)-1)
	}
	avail := m.contentHeight() - 1
	m.offset, _ = logWindow(m.entries, cur, m.offset, avail)
}

func (m Model) contentHeight() int {
	h := m.height - 2 - m.statusBarHeight() - m.helpBarHeight()
	if m.suggestionsVisible() {
		h--
	}
	if h < 0 {
		h = 0
	}
	return h
}

// statusBarHeight returns the number of terminal rows the status bar occupies.
// Most states are a single row; the subcommand menus (bookmark/git/remote)
// wrap onto extra rows when the terminal is narrow.
func (m Model) statusBarHeight() int {
	switch {
	case m.bookmarkMode && m.bookmarkAction == "":
		return len(wrapMenu(m.width, " [bookmark mode] ", colCyan, colPurple, " ", bookmarkMenuItems))
	case m.gitMode && m.remoteMode && m.remoteAction == "":
		return len(wrapMenu(m.width, " [git > remote] ", colPink, colPurple, " ", remoteMenuItems))
	case m.gitMode && !m.remoteMode:
		return len(wrapMenu(m.width, " [git mode] ", colDarkOrange, colPurple, " ", gitMenuItems))
	default:
		return 1
	}
}

// diffMaxScroll is the furthest scroll offset that still keeps the last
// screenful of the (status + diff) body in view.
func (m Model) diffMaxScroll() int {
	statusCount := len(m.diffStatus)
	if statusCount == 0 {
		statusCount = 1
	}
	headLen := statusCount + 2 // status header + items + separator
	bodyTotal := headLen + diffBodyLen(m.diffRows, m.diffRaw)
	bodyH := m.contentHeight() - 1 // minus the sticky title bar
	return max(0, bodyTotal-bodyH)
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

	if m.rebaseMode {
		return m.handleRebaseKey(k)
	}

	if m.squashMode {
		return m.handleSquashKey(k)
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
	contentH := m.contentHeight() - 1
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
			m.diffIsRevision = true
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
	case "A":
		m.showAllRev = !m.showAllRev
		m.cursor = 0
		m.offset = 0
		if m.showAllRev {
			m.message = "showing all revisions"
		} else {
			m.message = "showing default revisions"
		}
		return m, m.refreshCmd()
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
	case "R":
		r := m.runner
		return m, m.simpleCmd(func() error { return r.Redo() }, "redone")
	case "r":
		if len(m.entries) < 2 {
			m.errMsg = "need at least two revisions to rebase"
			return m, nil
		}
		if e := m.selectedEntry(); e != nil {
			m.rebaseMode = true
			m.rebaseSource = m.cursor
			m.rebaseSubtree = false
			m.rebasePlace = 0
			// Start the destination on a neighbouring commit so it is never
			// equal to the source on entry.
			m.rebaseDest = m.cursor + 1
			if m.rebaseDest >= len(m.entries) {
				m.rebaseDest = m.cursor - 1
			}
			m.errMsg = ""
			m.message = ""
			m.recomputeOffset()
		}
		return m, nil
	case "s":
		if len(m.entries) < 2 {
			m.errMsg = "need at least two revisions to squash"
			return m, nil
		}
		if e := m.selectedEntry(); e != nil {
			m.squashMode = true
			m.squashSource = m.cursor
			// Start the destination on a neighbouring commit so it is never
			// equal to the source on entry.
			m.squashDest = m.cursor + 1
			if m.squashDest >= len(m.entries) {
				m.squashDest = m.cursor - 1
			}
			m.errMsg = ""
			m.message = ""
			m.recomputeOffset()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSquashKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.squashMode = false
		m.message = "squash cancelled"
		m.recomputeOffset()
		return m, nil
	case "up", "k":
		if m.squashDest > 0 {
			m.squashDest--
		}
		m.recomputeOffset()
		return m, nil
	case "down", "j":
		if m.squashDest < len(m.entries)-1 {
			m.squashDest++
		}
		m.recomputeOffset()
		return m, nil
	case "home":
		m.squashDest = 0
		m.recomputeOffset()
		return m, nil
	case "end", "G":
		m.squashDest = len(m.entries) - 1
		m.recomputeOffset()
		return m, nil
	case "enter":
		return m.execSquash()
	}
	return m, nil
}

func (m Model) execSquash() (tea.Model, tea.Cmd) {
	if m.squashSource < 0 || m.squashSource >= len(m.entries) ||
		m.squashDest < 0 || m.squashDest >= len(m.entries) {
		m.squashMode = false
		return m, nil
	}
	if m.squashSource == m.squashDest {
		m.errMsg = "squash source and destination are the same"
		return m, nil
	}
	from := m.entries[m.squashSource].ChangeID
	into := m.entries[m.squashDest].ChangeID
	m.squashMode = false
	return m, m.squashCmd(from, into)
}

func (m Model) handleRebaseKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.rebaseMode = false
		m.message = "rebase cancelled"
		m.recomputeOffset()
		return m, nil
	case "up", "k":
		if m.rebaseDest > 0 {
			m.rebaseDest--
		}
		m.recomputeOffset()
		return m, nil
	case "down", "j":
		if m.rebaseDest < len(m.entries)-1 {
			m.rebaseDest++
		}
		m.recomputeOffset()
		return m, nil
	case "home":
		m.rebaseDest = 0
		m.recomputeOffset()
		return m, nil
	case "end", "G":
		m.rebaseDest = len(m.entries) - 1
		m.recomputeOffset()
		return m, nil
	case "s":
		m.rebaseSubtree = !m.rebaseSubtree
		return m, nil
	case "tab":
		m.rebasePlace = (m.rebasePlace + 1) % len(rebasePlaceFlags)
		return m, nil
	case "enter":
		return m.execRebase()
	}
	return m, nil
}

func (m Model) execRebase() (tea.Model, tea.Cmd) {
	if m.rebaseSource < 0 || m.rebaseSource >= len(m.entries) ||
		m.rebaseDest < 0 || m.rebaseDest >= len(m.entries) {
		m.rebaseMode = false
		return m, nil
	}
	if m.rebaseSource == m.rebaseDest {
		m.errMsg = "rebase destination is the source"
		return m, nil
	}
	srcFlag := "-r"
	if m.rebaseSubtree {
		srcFlag = "-s"
	}
	src := m.entries[m.rebaseSource].ChangeID
	dest := m.entries[m.rebaseDest].ChangeID
	placeFlag := rebasePlaceFlags[m.rebasePlace]
	label := rebasePlaceLabels[m.rebasePlace]
	m.rebaseMode = false
	r := m.runner
	return m, m.simpleCmd(
		func() error { return r.Rebase(srcFlag, src, placeFlag, dest) },
		"rebased "+src+" "+label+" "+dest,
	)
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
		rb := rebaseView{
			active:  m.rebaseMode,
			source:  m.rebaseSource,
			dest:    m.rebaseDest,
			subtree: m.rebaseSubtree,
			place:   m.rebasePlace,
		}
		sq := squashView{
			active: m.squashMode,
			source: m.squashSource,
			dest:   m.squashDest,
		}
		lines = append(lines, renderLog(m.width, ch, m.entries, m.cursor, m.offset, m.aiLoading, m.spinnerFrame, rb, sq)...)
	}

	// Autocomplete suggestions.
	if m.suggestionsVisible() {
		lines = append(lines, m.renderSuggestions())
	}

	// Status bar + help bar.
	lines = append(lines, m.renderStatusBar()...)
	lines = append(lines, m.renderHelpBar()...)

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

func (m Model) renderStatusBar() []string {
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
			return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colCyan})}
		}
		return m.renderMenuRows(" [bookmark mode] ", colCyan, colPurple, bookmarkMenuItems)

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
				return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colPink})}
			}
			return m.renderMenuRows(" [git > remote] ", colPink, colPurple, remoteMenuItems)
		}
		return m.renderMenuRows(" [git mode] ", colDarkOrange, colPurple, gitMenuItems)

	case m.rebaseMode:
		scope := "-r"
		if m.rebaseSubtree {
			scope = "-s (+descendants)"
		}
		src, dest := "?", "?"
		if m.rebaseSource >= 0 && m.rebaseSource < len(m.entries) {
			src = m.entries[m.rebaseSource].ChangeID
		}
		if m.rebaseDest >= 0 && m.rebaseDest < len(m.entries) {
			dest = m.entries[m.rebaseDest].ChangeID
		}
		segs := []seg{{text: " [rebase] ", fg: colYellow, bold: true}}
		segs = append(segs, seg{text: scope + " ", fg: colMagenta})
		segs = append(segs, seg{text: src, fg: colMagenta, bold: true})
		segs = append(segs, seg{text: " " + rebasePlaceLabels[m.rebasePlace] + " ", fg: colYellow})
		segs = append(segs, seg{text: dest, fg: colMagenta, bold: true})
		segs = append(segs, seg{text: "   j/k move · s scope · tab place · ⏎ confirm · esc cancel", fg: colGray})
		return []string{bgRow(m.width, colDarkerGray, segs...)}

	case m.squashMode:
		src, dest := "?", "?"
		if m.squashSource >= 0 && m.squashSource < len(m.entries) {
			src = m.entries[m.squashSource].ChangeID
		}
		if m.squashDest >= 0 && m.squashDest < len(m.entries) {
			dest = m.entries[m.squashDest].ChangeID
		}
		segs := []seg{{text: " [squash] ", fg: colYellow, bold: true}}
		segs = append(segs, seg{text: src, fg: colMagenta, bold: true})
		segs = append(segs, seg{text: " into ", fg: colYellow})
		segs = append(segs, seg{text: dest, fg: colMagenta, bold: true})
		segs = append(segs, seg{text: "   j/k move · ⏎ confirm · esc cancel", fg: colGray})
		return []string{bgRow(m.width, colDarkerGray, segs...)}

	case m.errMsg != "":
		msg := m.errMsg
		limit := m.width - 4
		if limit > 0 && len(msg) > limit {
			msg = msg[:limit]
		}
		return []string{bgRow(m.width, colDarkerGray, seg{text: " ✖ " + msg, fg: colRed})}

	case m.message != "":
		return []string{bgRow(m.width, colDarkerGray, seg{text: m.revsetBadge() + m.message, fg: colGray})}

	case len(m.statusEntries) > 0:
		return []string{bgRow(m.width, colDarkerGray, seg{text: m.revsetBadge() + fmt.Sprintf("%d changed file(s)", len(m.statusEntries)), fg: colGray})}

	default:
		return []string{bgRow(m.width, colDarkerGray, seg{text: m.revsetBadge() + "clean working copy ✓", fg: colGray})}
	}
}

// renderMenuRows renders a subcommand menu, wrapping onto extra rows when the
// terminal is too narrow to fit all items on one line.
func (m Model) renderMenuRows(prefix string, base, hl lipgloss.TerminalColor, items [][2]string) []string {
	packed := wrapMenu(m.width, prefix, base, hl, " ", items)
	out := make([]string, len(packed))
	for i, row := range packed {
		out[i] = bgRow(m.width, colDarkerGray, row...)
	}
	return out
}

// revsetBadge returns a leading status-bar marker indicating the active log
// revset: "[all] " when showing every revision, otherwise a single space.
func (m Model) revsetBadge() string {
	if m.showAllRev {
		return " [all] "
	}
	return " "
}

func (m Model) selChangeID() string {
	if e := m.selectedEntry(); e != nil {
		return e.ChangeID
	}
	return ""
}

// defaultHelpBarItems is the ordered list of global shortcut hints shown in
// the bottom help bar while browsing the log (the default context).
var defaultHelpBarItems = [][2]string{
	{"⏎diff", "⏎"}, {"describe", "d"},
	{"AI Desc", "D"}, {"bookmark", "b"}, {"git", "g"},
	{"undo", "u"}, {"rebase", "r"}, {"squash", "s"}, {"edit", "e"}, {"new", "n"},
	{"abandon", "a"}, {"?help", "?"}, {"quit", "q"},
}

// helpBarItems returns the shortcut hints shown in the bottom help bar for the
// current context. It returns nil when the help bar should be hidden entirely
// (e.g. subcommand modes whose key hints are already surfaced in the status
// bar), so the content area can reclaim that row.
func (m Model) helpBarItems() [][2]string {
	switch {
	case m.diffOpen:
		return [][2]string{
			{"⏎ close", "⏎"}, {"↑/k", "↑"}, {"↓/j", "↓"},
			{"q close", "q"},
		}
	case m.view == viewHelp:
		return [][2]string{
			{"↑/k", "↑"}, {"↓/j", "↓"}, {"?/q close", "?"},
		}
	case m.rebaseMode, m.squashMode,
		m.bookmarkMode, m.gitMode:
		// Keys for these modes are shown inline in the status bar.
		return nil
	default:
		return defaultHelpBarItems
	}
}

// Menu item lists for the status-bar subcommand menus. Each entry is
// {label, key}; "cancel" is folded in so it wraps with the rest.
var (
	bookmarkMenuItems = [][2]string{
		{"create", "c"}, {"delete", "d"}, {"forget", "f"},
		{"list", "l"}, {"move", "m"}, {"rename", "r"},
		{"set", "s"}, {"track", "t"}, {"untrack", "T"},
		{"cancel", "esc"},
	}
	gitMenuItems = [][2]string{
		{"fetch", "f"}, {"push", "p"}, {"remote", "r"},
		{"cancel", "esc"},
	}
	remoteMenuItems = [][2]string{
		{"add", "a"}, {"list", "l"}, {"remove", "r"},
		{"rename", "m"}, {"set-url", "s"},
		{"cancel", "esc"},
	}
)

// wrapMenu greedily packs highlightable menu items into rows no wider than
// width, returning the segment slices per row. The first row is prefixed with
// `prefix`; every subsequent (wrapped) row begins with a single leading space.
// Items are separated by `sep`. base colors the item text, hl colors (and
// underlines) the matched key substring. A lone item wider than the terminal is
// allowed to overflow and is clipped by the caller.
func wrapMenu(width int, prefix string, base, hl lipgloss.TerminalColor, sep string, items [][2]string) [][]seg {
	if width <= 1 {
		return [][]seg{{}}
	}
	prefixW := lipgloss.Width(prefix)
	var rows [][]seg
	// cur is the in-progress row's segments; curW its visible width; hasItem
	// whether an item has already been placed on cur (so a separator is needed
	// before the next one).
	cur := []seg{{text: prefix, fg: base}}
	curW := prefixW
	hasItem := false
	for _, it := range items {
		itemW := lipgloss.Width(it[0])
		addW := itemW
		if hasItem {
			addW += len(sep)
		}
		// Flush when the item won't fit — but only if cur already holds an item;
		// otherwise the item alone is wider than the terminal and we let it
		// overflow (clipped) rather than emitting an empty row.
		if curW+addW > width && hasItem {
			rows = append(rows, cur)
			cur = []seg{{text: " ", fg: base}}
			curW = 1
			hasItem = false
			addW = itemW
		}
		if hasItem {
			cur = append(cur, seg{text: sep, fg: base})
			curW += len(sep)
		}
		cur = append(cur, hlSegs([][2]string{it}, base, hl, "")...)
		curW += itemW
		hasItem = true
	}
	rows = append(rows, cur)
	return rows
}

// helpBarHeight returns the number of terminal rows the wrapped help bar
// needs at the current width. Returns 0 when the help bar is hidden for the
// active context.
func (m Model) helpBarHeight() int {
	items := m.helpBarItems()
	if items == nil {
		return 0
	}
	return len(wrapMenu(m.width, " ", colGray, colPurple, "  ", items))
}

// renderHelpBar renders the context-specific shortcut hints, wrapping onto
// extra rows when the terminal is too narrow to fit them all on one line.
func (m Model) renderHelpBar() []string {
	items := m.helpBarItems()
	if items == nil {
		return nil
	}
	packed := wrapMenu(m.width, " ", colGray, colPurple, "  ", items)
	out := make([]string, len(packed))
	for i, row := range packed {
		out[i] = bgRow(m.width, colDarkerGray, row...)
	}
	return out
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
