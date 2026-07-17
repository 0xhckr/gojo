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
	viewFile
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

	// File view: browse tracked files, open one with git-blame-style
	// annotation, and inspect its history. Driven by fileViewState.
	fileView fileViewState

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
	diffRevPrefix  int  // shortest-unique-prefix length for diffRev (0 = none)
	diffIsRevision bool // true: showing a revision diff (reloadable); false: a list view
	diffLoading    bool
	diffDesc       string // revision description shown above the status section
	diffStatus     []jj.StatusEntry
	diffRows       []diffRow
	diffDigits     int // gutter width, computed once when the diff loads
	diffRaw        string
	diffScrollY    int

	// diffCollapsed tracks which file diffs are collapsed, keyed by file path.
	// Collapsed files show only their header row (with a ▶ indicator); their
	// hunk/content rows are excluded from the layout, rendering, and chunk
	// cursor navigation.
	diffCollapsed map[string]bool

	// diffLayout is the wrapped-line layout of the diff body (the region below
	// the description/status head). Recomputed on diff load, raw-list load and
	// resize. When unset (e.g. in unit tests) the helpers below fall back to a
	// 1:1 mapping (one terminal line per row), preserving the pre-wrap
	// behaviour so scroll math stays correct without a layout.
	diffLayout diffLayout

	// Chunk cursor — navigates change chunks (contiguous add/del runs) in the
	// diff panel. diffChunks holds body-row indices per chunk; diffCurChunk /
	// diffCurLine track the focused line. Empty when the diff has no chunks or
	// is showing raw list output.
	diffChunks   [][]int
	diffCurChunk int
	diffCurLine  int

	helpScrollY int

	// Bookmark mode.
	bookmarkMode   bool
	bookmarkAction string // "" | c d f m r s t T l
	bookmarkInput  string
	acOriginal     *string
	acIdx          int

	// Tag mode.
	tagMode   bool
	tagAction string // "" | s m d l p
	tagInput  string

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

	// Split mode. Entered from the diff panel with `x`. The user marks
	// individual files/lines to keep in the current revision; unmarked
	// changes are split into a new preceding revision via `jj split`.
	splitMode   bool
	splitMarked map[int]bool // diff row indices of marked addition/deletion lines

	// AI describe. Generation (HTTP, read-only) runs concurrently; the jj
	// describe apply step is serialized via aiDescribeQueue so only one
	// mutating subprocess is in flight at a time — concurrent describes on
	// the same repo can corrupt the op log.
	aiLoading         map[string]bool
	aiDescribeQueue   []aiPendingDescribe
	aiDescribeRunning bool
	spinnerFrame      int
	spinnerRunning    bool

	// busy holds labels for in-flight background actions (e.g. "pushing…"),
	// shown as a prominent spinner in the status bar until each completes.
	// It's a stack so overlapping actions all keep the spinner animating.
	busy []string

	// pendingElev is a pending elevation prompt. When an action fails with a
	// recognized "needs --flag" error, gojo asks the user whether to retry with
	// that flag appended; confirming runs pendingElev.retry.
	pendingElev *elevReq

	// Auto-refresh poll. Runs only while the terminal is focused so an idle or
	// backgrounded gojo isn't firing jj subprocesses every couple seconds.
	focused bool
	polling bool

	// scrollDragging is true while the user is click-and-dragging the
	// scrollbar thumb. Set on MouseActionPress inside the scrollbar area,
	// cleared on MouseActionRelease.
	scrollDragging bool
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
		focused:         true,
		polling:         true,
		aiLoading:       map[string]bool{},
		aiDescribeQueue: []aiPendingDescribe{},
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
	desc   string
	status []jj.StatusEntry
	rows   []diffRow
	err    error
}

type actionDoneMsg struct {
	message string
	err     error
	refresh bool
	// elev, when non-nil, means the action failed with an error that an
	// elevation flag could fix; Update stashes it as pendingElev so the user
	// can confirm a retry instead of seeing a bare error.
	elev *elevReq
}

// elevReq describes a pending elevation prompt: re-run a failed operation
// with an extra trailing flag (e.g. --ignore-immutable, --allow-backwards).
// retry produces the tea.Cmd that re-runs the operation with the flag added;
// it may be a captured subprocess (actionDoneMsg result) or an ExecProcess
// (editor flows like describe), which is why it returns a Cmd rather than an
// error.
type elevReq struct {
	flag   string         // the flag a retry appends (e.g. "--ignore-immutable")
	reason string         // short description of why elevation is needed
	retry  func() tea.Cmd // re-run the operation with the flag added
}

// aiPendingDescribe holds a generated AI message waiting to be applied via
// jj describe. Items are queued so describes run one at a time.
type aiPendingDescribe struct {
	changeID string
	message  string
}

// aiGeneratedMsg is returned when the AI has generated a commit message but
// before it has been applied to the repo. The apply step is serialized via
// aiDescribeQueue to prevent concurrent jj describe operations.
type aiGeneratedMsg struct {
	changeID string
	message  string
	err      error
}

// aiAppliedMsg is returned after jj describe has been applied (or failed).
type aiAppliedMsg struct {
	changeID string
	message  string
	err      error
	// elev, when non-nil, means the AI message was generated but applying it
	// failed with an elevatable error; Update stashes it as pendingElev so the
	// user can confirm reapplying the message with the flag appended.
	elev *elevReq
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

// newCreatedMsg is returned after `jj new` creates a revision on top of
// another, carrying the new working-copy entry so the diff can track it.
type newCreatedMsg struct {
	entry *jj.LogEntry
	err   error
	elev  *elevReq
}

type listLoadedMsg struct {
	title   string
	content string
	err     error
}

type spinnerTickMsg struct{}

type pollMsg struct{}

// File-view messages.
type fileListMsg struct {
	files []string
	err   error
}

type fileAnnotateMsg struct {
	path  string
	lines []jj.AnnotateLine
	err   error
}

type fileHistoryMsg struct {
	entries []jj.LogEntry
	err     error
}

// ── Init ────────────────────────────────────────────────────────────────────

// Init kicks off configuration loading, the auto-refresh poll loop, and the
// top-bar animation tick.
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
		desc, _ := r.Description(commitID)
		return diffLoadedMsg{rev: changeID, desc: desc, status: status, rows: renderDiff(diff)}
	}
}

// openRevisionDiff switches the model to the diff panel for a revision and
// returns the command to load it. changeID is the panel's revset (stable
// across working-copy edits); commitID is what jj diff resolves.
func (m Model) openRevisionDiff(changeID, commitID string, prefixLen int, subject string) (tea.Model, tea.Cmd) {
	m.diffOpen = true
	m.diffRev = changeID
	m.diffRevPrefix = prefixLen
	m.diffIsRevision = true
	m.diffLoading = true
	m.diffScrollY = 0
	m.diffDesc = subject
	m.diffRaw = ""
	m.diffRows = nil
	m.diffStatus = nil
	m.diffChunks = nil
	return m, m.openDiffCmd(commitID, changeID)
}

func (m Model) simpleCmd(fn func() error, okMsg string) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: okMsg, refresh: true}
	}
}

// actionSpec describes a runnable jj operation that may be retried with an
// elevation flag. elevate, when non-nil, rebuilds the operation with an extra
// trailing flag appended — used when the first attempt fails with a
// recognized "needs --flag" error (see jj.DetectElevation).
type actionSpec struct {
	run     func() error
	okMsg   string
	elevate func(flag string) func() error
}

// actionCmd runs spec.run. On an elevatable failure it attaches an elevReq to
// the resulting actionDoneMsg so Update can prompt the user; otherwise it
// behaves like simpleCmd. A nil elevate means the operation is never
// elevatable (e.g. undo/redo).
func (m Model) actionCmd(spec actionSpec) tea.Cmd {
	return func() tea.Msg {
		if err := spec.run(); err != nil {
			if spec.elevate != nil {
				if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
					retryFn := spec.elevate(flag)
					return actionDoneMsg{
						err: err,
						elev: &elevReq{
							flag:   flag,
							reason: reason,
							retry:  func() tea.Cmd { return m.syncFnCmd(retryFn, spec.okMsg) },
						},
					}
				}
			}
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: spec.okMsg, refresh: true}
	}
}

// syncFnCmd runs a captured-subprocess operation (fn) and wraps its result in
// an actionDoneMsg. Used for elevation retries: the returned msg has no elev
// attached, so a second failure does not re-prompt (avoids loops).
func (m Model) syncFnCmd(fn func() error, okMsg string) tea.Cmd {
	return func() tea.Msg {
		if err := fn(); err != nil {
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: okMsg, refresh: true}
	}
}

// busyActionCmd runs an actionSpec while showing a prominent spinner labelled
// `label` in the status bar. It pushes the label onto the busy stack, starts
// the spinner tick, and runs the underlying actionCmd.
func (m Model) busyActionCmd(label string, spec actionSpec) (tea.Model, tea.Cmd) {
	m, tick := m.startBusy(label)
	return m, tea.Batch(tick, m.actionCmd(spec))
}

// busySimpleCmd is the simpleCmd variant of busyActionCmd for non-elevatable
// operations (e.g. undo/redo).
func (m Model) busySimpleCmd(label string, fn func() error, okMsg string) (tea.Model, tea.Cmd) {
	m, tick := m.startBusy(label)
	return m, tea.Batch(tick, m.simpleCmd(fn, okMsg))
}

// aiGenerateCmd calls the AI API to generate a commit message for changeID.
// This is safe to run concurrently with other generations (read-only: it
// fetches the diff and calls the AI API but does not mutate the repo).
func (m Model) aiGenerateCmd(changeID string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		msg, err := r.AIDescribe(changeID)
		return aiGeneratedMsg{changeID: changeID, message: msg, err: err}
	}
}

// aiApplyCmd runs jj describe with the AI-generated message. This must be
// serialized — only one describe should be in flight at a time to avoid
// concurrent mutating operations on the same repo.
func (m Model) aiApplyCmd(changeID, message string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		if err := r.Describe(changeID, message); err != nil {
			if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
				return aiAppliedMsg{
					changeID: changeID,
					err:      err,
					elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry: func() tea.Cmd {
							return m.syncFnCmd(func() error { return r.Describe(changeID, message, flag) }, "AI described "+changeID)
						},
					},
				}
			}
			return aiAppliedMsg{changeID: changeID, err: err}
		}
		return aiAppliedMsg{changeID: changeID, message: message}
	}
}

// startNextAIDescribe pops the next pending AI describe from the queue and
// fires aiApplyCmd for it. Returns nil cmd if the queue is empty.
func (m Model) startNextAIDescribe() (Model, tea.Cmd) {
	if len(m.aiDescribeQueue) == 0 {
		m.aiDescribeRunning = false
		return m, nil
	}
	next := m.aiDescribeQueue[0]
	m.aiDescribeQueue = m.aiDescribeQueue[1:]
	m.aiDescribeRunning = true
	return m, m.aiApplyCmd(next.changeID, next.message)
}

// describeCmd runs `jj describe -r <changeID>` (suspending the TUI for
// $EDITOR) with optional extra trailing flags, used for elevation retries on
// immutable commits.
func (m Model) describeCmd(changeID string, extra ...string) tea.Cmd {
	args := append([]string{"describe", "-r", changeID}, extra...)
	c := exec.Command(m.cfg.JJPath, args...)
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

// newOnRevCmd runs `jj new -r <rev>` and, on success, fetches the new working
// copy's log entry so the caller can open a diff on it. Extra flags are
// appended for elevation retries.
func (m Model) newOnRevCmd(rev string, extra ...string) tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		if err := r.New(rev, extra...); err != nil {
			if len(extra) == 0 {
				if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
					return newCreatedMsg{
						err: err,
						elev: &elevReq{
							flag:   flag,
							reason: reason,
							retry:  func() tea.Cmd { return m.newOnRevCmd(rev, flag) },
						},
					}
				}
			}
			return newCreatedMsg{err: err}
		}
		entry, err := r.WorkingCopyEntry()
		if err != nil {
			return newCreatedMsg{err: err}
		}
		return newCreatedMsg{entry: entry}
	}
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

// spinnerActive reports whether any animated spinner is needed right now:
// an AI describe in flight, or any background action on the busy stack.
func (m Model) spinnerActive() bool {
	return len(m.aiLoading) > 0 || len(m.busy) > 0
}

// startBusy pushes a background-action label onto the busy stack and ensures
// the spinner tick loop is running, returning the updated model and a tick
// command (nil if the loop is already going).
func (m Model) startBusy(label string) (Model, tea.Cmd) {
	m.busy = append(m.busy, label)
	if !m.spinnerRunning {
		m.spinnerRunning = true
		return m, spinnerTick()
	}
	return m, nil
}

// popBusy removes the most recent background-action label (LIFO) once its
// action completes. The spinner loop self-stops on its next tick when nothing
// remains active.
func (m *Model) popBusy() {
	if len(m.busy) > 0 {
		m.busy = m.busy[:len(m.busy)-1]
	}
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
	if m.diffOpen && m.diffIsRevision && !m.splitMode {
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
		if m.diffOpen {
			// Wrapping depends on width, so rebuild the layout before clamping.
			m.computeDiffLayout()
			// Preserve the user's scroll position (which may be free-scrolled to
			// show the status section); just clamp into the valid range for the
			// new height.
			m.diffClampMax()
			if r := m.diffCursorBodyRow(); r >= 0 && (r < m.diffScrollY || r >= m.diffScrollY+m.diffBodyHeight()) {
				// Only re-anchor if the cursor itself fell out of view.
				m.diffFollowCursor()
			}
		}
		if m.view == viewFile && m.fileView.phase == fileBlame {
			m.fileView.buildBlameCache(m.width, fileViewContentH(m))
		}
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
		// A revision diff is reloaded on every focus/poll refresh. Treat that as
		// a refresh (not a fresh open) so the user's cursor position survives —
		// otherwise the 2s poll yanks navigation back to the first chunk.
		isRefresh := m.diffIsRevision && msg.rev == m.diffRev && len(m.diffRows) > 0
		if !isRefresh {
			m.diffCollapsed = nil
		}
		// Update the description; keep the instant subject shown during loading
		// when the fetch returned nothing (e.g. a transient jj failure).
		if msg.desc != "" {
			m.diffDesc = msg.desc
		}
		m.diffStatus = msg.status
		m.diffRows = msg.rows
		m.diffDigits = maxLineDigits(msg.rows)
		m.computeDiffLayout()
		m.diffChunks = computeDiffChunks(msg.rows, m.diffHeadLen(), m.diffCollapsed)
		if !isRefresh {
			m.diffCurChunk = 0
			m.diffCurLine = 0
		} else if len(m.diffChunks) > 0 {
			// The diff may have changed shape; clamp the cursor back into range.
			if m.diffCurChunk >= len(m.diffChunks) {
				m.diffCurChunk = len(m.diffChunks) - 1
			}
			if m.diffCurLine >= len(m.diffChunks[m.diffCurChunk]) {
				m.diffCurLine = len(m.diffChunks[m.diffCurChunk]) - 1
			}
		}
		// Preserve the viewport across a refresh (the user may have free-scrolled
		// to the status section); only re-anchor if the cursor fell out of view.
		m.diffClampMax()
		if r := m.diffCursorBodyRow(); r >= 0 && (r < m.diffScrollY || r >= m.diffScrollY+m.diffBodyHeight()) {
			m.diffFollowCursor()
		}
		return m, nil

	case actionDoneMsg:
		// Whatever the outcome, the action is no longer in flight.
		m.popBusy()
		if msg.err != nil {
			if msg.elev != nil {
				// Surface the elevation prompt instead of the bare error.
				m.pendingElev = msg.elev
				m.errMsg = ""
				return m, nil
			}
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.message = msg.message
		if msg.refresh {
			return m, m.refreshCmd()
		}
		return m, nil

	case listLoadedMsg:
		m.popBusy()
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.diffOpen = true
		m.diffRev = msg.title
		m.diffRevPrefix = 0
		m.diffIsRevision = false
		m.diffRaw = msg.content
		m.diffRows = nil
		m.diffStatus = nil
		m.diffChunks = nil
		m.diffDesc = ""
		m.diffLoading = false
		m.diffScrollY = 0
		m.computeDiffLayout()
		return m, nil

	case aiGeneratedMsg:
		if msg.err != nil {
			delete(m.aiLoading, msg.changeID)
			m.errMsg = msg.err.Error()
			return m, nil
		}
		// Queue the describe; run immediately if nothing is in flight.
		if m.aiDescribeRunning {
			m.aiDescribeQueue = append(m.aiDescribeQueue, aiPendingDescribe{
				changeID: msg.changeID,
				message:  msg.message,
			})
			return m, nil
		}
		m.aiDescribeRunning = true
		return m, m.aiApplyCmd(msg.changeID, msg.message)

	case aiAppliedMsg:
		delete(m.aiLoading, msg.changeID)
		if msg.err != nil {
			if msg.elev != nil {
				m.pendingElev = msg.elev
				m.errMsg = ""
				m, next := m.startNextAIDescribe()
				return m, next
			}
			m.errMsg = msg.err.Error()
			m, next := m.startNextAIDescribe()
			if next != nil {
				return m, tea.Batch(append([]tea.Cmd{next}, m.refreshFocusedCmds()...)...)
			}
			return m, tea.Batch(m.refreshFocusedCmds()...)
		}
		m.message = "AI described " + msg.changeID + ": " + msg.message
		cmds := m.refreshFocusedCmds()
		m, next := m.startNextAIDescribe()
		if next != nil {
			cmds = append(cmds, next)
		}
		return m, tea.Batch(cmds...)

	case describeFinishedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.message = "described " + msg.changeID
		}
		return m, tea.Batch(m.refreshFocusedCmds()...)

	case squashFinishedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.message = "squashed " + msg.from + " into " + msg.into
		}
		return m, m.refreshCmd()

	case splitFinishedMsg:
		m.popBusy()
		if msg.err != nil {
			if msg.elev != nil {
				m.pendingElev = msg.elev
				m.errMsg = ""
				return m, nil
			}
			m.errMsg = msg.err.Error()
			return m, m.refreshCmd()
		}
		m.message = "split " + msg.rev
		// Auto-route: open the diff of the newly created (selected) revision
		// so the user can immediately inspect what was split off.
		if msg.selectedRev != "" {
			m.diffRev = msg.selectedRev
			m.diffRevPrefix = 0
			m.diffIsRevision = true
			m.diffLoading = true
			m.diffScrollY = 0
			m.diffDesc = ""
			m.diffRaw = ""
			m.diffRows = nil
			m.diffStatus = nil
			m.diffChunks = nil
			return m, tea.Batch(m.refreshCmd(), m.openDiffCmd(msg.selectedRev, msg.selectedRev))
		}
		return m, m.refreshCmd()

	case newCreatedMsg:
		m.popBusy()
		if msg.err != nil {
			if msg.elev != nil {
				m.pendingElev = msg.elev
				m.errMsg = ""
				return m, nil
			}
			m.errMsg = msg.err.Error()
			return m, m.refreshCmd()
		}
		m.message = "created new change"
		if msg.entry != nil {
			m.diffOpen = true
			m.diffRev = msg.entry.ChangeID
			m.diffRevPrefix = msg.entry.ChangeIDPrefixLen
			m.diffIsRevision = true
			m.diffLoading = true
			m.diffScrollY = 0
			m.diffDesc = msg.entry.Subject
			m.diffRaw = ""
			m.diffRows = nil
			m.diffStatus = nil
			m.diffChunks = nil
			m.cursor = 0
			m.recomputeOffset()
			return m, tea.Batch(m.refreshCmd(), m.openDiffCmd(msg.entry.CommitID, msg.entry.ChangeID))
		}
		return m, m.refreshCmd()

	case spinnerTickMsg:
		m.spinnerFrame++
		if m.spinnerActive() {
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

	case fileListMsg:
		m.popBusy()
		if msg.err != nil {
			m.fileView.err = msg.err.Error()
			return m, nil
		}
		m.fileView.err = ""
		m.fileView = newFileViewState(msg.files)
		return m, nil

	case fileAnnotateMsg:
		m.popBusy()
		if msg.err != nil {
			m.fileView.err = msg.err.Error()
			return m, nil
		}
		m.fileView.err = ""
		m.fileView.path = msg.path
		m.fileView.lines = msg.lines
		m.fileView.highlights = nil // recompute lazily for the new file
		m.fileView.cursorY = 0
		m.fileView.phase = fileBlame
		m.fileView.buildBlameCache(m.width, fileViewContentH(m))
		return m, nil

	case fileHistoryMsg:
		m.popBusy()
		if msg.err != nil {
			m.fileView.err = msg.err.Error()
			return m, nil
		}
		m.fileView.err = ""
		m.fileView.hist = msg.entries
		m.fileView.histCur = 0
		m.fileView.histOff = 0
		m.fileView.phase = fileHistory
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

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

// bottomBarHeight is the 1-line blank strip drawn below the help bar, coloured
// the same as the status/help bars. It prevents Ghostty from extending the last
// content line's background into the remaining terminal rows (which made the
// text-input cursor appear two lines tall).
const bottomBarHeight = 1

func (m Model) contentHeight() int {
	h := m.height - 2 - m.statusBarHeight() - m.helpBarHeight() - bottomBarHeight
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
	case m.tagMode && m.tagAction == "":
		return len(wrapMenu(m.width, " [tag mode] ", colTeal, colPurple, " ", tagMenuItems))
	case m.gitMode && m.remoteMode && m.remoteAction == "":
		return len(wrapMenu(m.width, " [git > remote] ", colPink, colPurple, " ", remoteMenuItems))
	case m.gitMode && !m.remoteMode:
		return len(wrapMenu(m.width, " [git mode] ", colDarkOrange, colPurple, " ", gitMenuItems))
	default:
		return 1
	}
}

// diffMaxScroll is the furthest scroll offset that still keeps the last
// screenful of the (description + status + diff) body in view.
func (m Model) diffMaxScroll() int {
	bodyTotal := m.diffHeadLen() + m.diffBodyTotal()
	bodyH := m.contentHeight() - 1 // minus the sticky title bar
	return max(0, bodyTotal-bodyH)
}

// diffBodyTotal is the number of terminal lines the diff body (below the head)
// occupies, accounting for wrapping. Falls back to 1:1 when no layout is set.
func (m Model) diffBodyTotal() int {
	if len(m.diffLayout.starts) > 0 {
		return m.diffLayout.total
	}
	return diffBodyLen(m.diffRows, m.diffRaw)
}

// rowStartTerm is the 0-based body terminal-line index of the first wrapped
// sub-line of diff row `rowIdx` (a 0-based index into diffRows / raw lines).
// Falls back to rowIdx (1:1) when no layout is set.
func (m Model) rowStartTerm(rowIdx int) int {
	if rowIdx >= 0 && rowIdx < len(m.diffLayout.starts) {
		return m.diffLayout.starts[rowIdx]
	}
	return rowIdx
}

// rowCountTerm is the number of terminal lines diff row `rowIdx` spans.
// Falls back to 1 when no layout is set.
func (m Model) rowCountTerm(rowIdx int) int {
	if rowIdx >= 0 && rowIdx < len(m.diffLayout.counts) {
		return m.diffLayout.counts[rowIdx]
	}
	return 1
}

// computeDiffLayout (re)builds the wrapped-line layout for the current diff
// body from the terminal size and content. Called on diff/raw load and on
// resize so navigation and rendering agree on where wrapped lines land.
func (m *Model) computeDiffLayout() {
	m.diffLayout = computeDiffLayoutPure(m.width, m.contentHeight(), m.diffHeadLen(), m.diffRows, m.diffRaw, m.diffDigits, m.diffCollapsed, m.splitMode, false)
}

// diffHeadLen is the number of body rows occupied by the description header,
// status header, items, separators, and the changes label — everything above
// the first diff/raw line. The description section only appears for revision
// diffs.
func (m Model) diffHeadLen() int {
	statusCount := len(m.diffStatus)
	if statusCount == 0 {
		statusCount = 1 // "(no changes)" row
	}
	descLen := 0
	if m.diffIsRevision {
		if m.aiLoading[m.diffRev] {
			descLen = 3 // label + spinner line + divider
		} else {
			descLen = descHeadLen(m.diffDesc)
		}
	}
	return descLen + statusCount + 2 + 1 // +1 for the "changes" label
}

// diffBodyHeight is the number of visible rows below the sticky diff title.
func (m Model) diffBodyHeight() int {
	h := m.contentHeight() - 1
	if h < 1 {
		h = 1
	}
	return h
}

// diffCursorBodyRow is the terminal body-line index of the focused line's
// first wrapped sub-line, or -1 if the diff has no chunks to navigate. With
// wrapping this is the top terminal line of the cursor's logical row so the
// scroll-follow logic keeps the row visible.
func (m Model) diffCursorBodyRow() int {
	if len(m.diffChunks) == 0 || m.diffCurChunk < 0 || m.diffCurChunk >= len(m.diffChunks) {
		return -1
	}
	cur := m.diffChunks[m.diffCurChunk]
	if m.diffCurLine < 0 || m.diffCurLine >= len(cur) {
		return -1
	}
	headLen := m.diffHeadLen()
	rowIdx := cur[m.diffCurLine] - headLen
	return headLen + m.rowStartTerm(rowIdx)
}

// diffChunkRows returns a set of the body-row indices in the focused chunk,
// for rendering the dim extent bar. Returns nil when there is no cursor.
func (m Model) diffChunkRows() map[int]bool {
	if len(m.diffChunks) == 0 || m.diffCurChunk < 0 || m.diffCurChunk >= len(m.diffChunks) {
		return nil
	}
	out := make(map[int]bool, len(m.diffChunks[m.diffCurChunk]))
	for _, r := range m.diffChunks[m.diffCurChunk] {
		out[r] = true
	}
	return out
}

// diffClampMax keeps diffScrollY within the scrollable range.
func (m *Model) diffClampMax() {
	if m.diffScrollY < 0 {
		m.diffScrollY = 0
	}
	if mx := m.diffMaxScroll(); m.diffScrollY > mx {
		m.diffScrollY = mx
	}
}

// chunkTerminalSpan returns the [first, last] terminal body-line indices
// (inclusive, including the head offset) spanned by the focused chunk. Used by
// the scroll-follow logic to reason about wrapped chunk extents.
func (m Model) chunkTerminalSpan() (int, int) {
	cur := m.diffChunks[m.diffCurChunk]
	headLen := m.diffHeadLen()
	firstIdx := cur[0] - headLen
	lastIdx := cur[len(cur)-1] - headLen
	first := headLen + m.rowStartTerm(firstIdx)
	last := headLen + m.rowStartTerm(lastIdx) + m.rowCountTerm(lastIdx) - 1
	return first, last
}

// diffFollowCursor scrolls the minimum amount needed so the cursor is visible
// AND as much of the focused chunk as possible is shown.
//   - For a chunk that fits in the viewport, the whole chunk is kept visible,
//     so surrounding context (hunk header above, context lines below) stays on
//     screen too.
//   - For a chunk taller than the viewport, only the cursor line is guaranteed
//     visible — so stepping at an edge reveals exactly one new (wrapped) line.
func (m *Model) diffFollowCursor() {
	row := m.diffCursorBodyRow()
	if row < 0 {
		return
	}
	first, last := m.chunkTerminalSpan()
	h := m.diffBodyHeight()
	if last-first+1 <= h {
		// Whole chunk fits: keep it entirely in view (scroll only if needed).
		if first < m.diffScrollY {
			m.diffScrollY = first
		}
		if last >= m.diffScrollY+h {
			m.diffScrollY = last - h + 1
		}
	} else {
		// Chunk too big: minimal reveal of the cursor line only.
		if row < m.diffScrollY {
			m.diffScrollY = row
		}
		if row >= m.diffScrollY+h {
			m.diffScrollY = row - h + 1
		}
	}
	m.diffClampMax()
}

// diffChunkContext is the number of context lines shown above (when entering
// from above) or below (when entering from below) a chunk on snap, so the
// change is seen in its surrounding context.
const diffChunkContext = 3

// diffEnterChunkDown scrolls for entering a chunk from above (cursor on its
// first line). It reveals diffChunkContext lines before the chunk, then as much
// of the chunk as fits in the viewport.
func (m *Model) diffEnterChunkDown() {
	cur := m.diffChunks[m.diffCurChunk]
	headLen := m.diffHeadLen()
	first := headLen + m.rowStartTerm(cur[0]-headLen)
	top := first - diffChunkContext
	if top < 0 {
		top = 0
	}
	m.diffScrollY = top
	m.diffClampMax()
}

// diffEnterChunkUp scrolls for entering a chunk from below (cursor on its last
// line). It reveals diffChunkContext lines after the chunk, then as much of the
// chunk as fits in the viewport.
func (m *Model) diffEnterChunkUp() {
	cur := m.diffChunks[m.diffCurChunk]
	headLen := m.diffHeadLen()
	lastIdx := cur[len(cur)-1] - headLen
	last := headLen + m.rowStartTerm(lastIdx) + m.rowCountTerm(lastIdx) - 1
	h := m.diffBodyHeight()
	top := last + diffChunkContext - h + 1
	if top < 0 {
		top = 0
	}
	m.diffScrollY = top
	m.diffClampMax()
}

// diffMoveDown advances the cursor: steps within the current chunk, revealing
// one line at a time for long chunks, then jumps to the next chunk. Falls back
// to free line-scrolling when there are no chunks (e.g. raw list output). At
// the very bottom it free-scrolls to reveal trailing context.
func (m *Model) diffMoveDown() {
	if len(m.diffChunks) == 0 {
		if m.diffScrollY < m.diffMaxScroll() {
			m.diffScrollY++
		}
		return
	}
	cur := m.diffChunks[m.diffCurChunk]
	if m.diffCurLine < len(cur)-1 {
		m.diffCurLine++
		m.diffFollowCursor()
		return
	}
	if m.diffCurChunk < len(m.diffChunks)-1 {
		m.diffCurChunk++
		m.diffCurLine = 0
		m.diffEnterChunkDown()
		return
	}
	// Last line of the last chunk: free-scroll down to reveal trailing context.
	if m.diffScrollY < m.diffMaxScroll() {
		m.diffScrollY++
	}
}

// diffMoveUp is the upward mirror of diffMoveDown. At the very top it
// free-scrolls upward to reveal the status section / preceding context, with
// the cursor resting on the first chunk line.
func (m *Model) diffMoveUp() {
	if len(m.diffChunks) == 0 {
		if m.diffScrollY > 0 {
			m.diffScrollY--
		}
		return
	}
	if m.diffCurLine > 0 {
		m.diffCurLine--
		m.diffFollowCursor()
		return
	}
	if m.diffCurChunk > 0 {
		m.diffCurChunk--
		m.diffCurLine = len(m.diffChunks[m.diffCurChunk]) - 1
		m.diffEnterChunkUp()
		return
	}
	// First line of the first chunk: free-scroll up to reveal the status header
	// and preceding context. The cursor stays put.
	if m.diffScrollY > 0 {
		m.diffScrollY--
	}
}

// diffMoveTop jumps to the first line of the first chunk.
func (m *Model) diffMoveTop() {
	if len(m.diffChunks) == 0 {
		m.diffScrollY = 0
		return
	}
	m.diffCurChunk = 0
	m.diffCurLine = 0
	m.diffFollowCursor()
}

// diffMoveBottom jumps to the last line of the last chunk.
func (m *Model) diffMoveBottom() {
	if len(m.diffChunks) == 0 {
		m.diffScrollY = m.diffMaxScroll()
		return
	}
	m.diffCurChunk = len(m.diffChunks) - 1
	m.diffCurLine = len(m.diffChunks[m.diffCurChunk]) - 1
	m.diffFollowCursor()
}

// cursorOnFileHeader returns the row index of the file header the cursor is
// currently on, or (-1, false) if the cursor is on a diff chunk (or there are
// no navigable items).
func (m Model) cursorOnFileHeader() (int, bool) {
	if len(m.diffChunks) == 0 || m.diffCurChunk < 0 || m.diffCurChunk >= len(m.diffChunks) {
		return 0, false
	}
	cur := m.diffChunks[m.diffCurChunk]
	// File headers are single-element chunks.
	if len(cur) != 1 {
		return 0, false
	}
	headLen := m.diffHeadLen()
	rowIdx := cur[0] - headLen
	if rowIdx < 0 || rowIdx >= len(m.diffRows) {
		return 0, false
	}
	if m.diffRows[rowIdx].kind != rowFileHeader {
		return 0, false
	}
	return rowIdx, true
}

// cursorFileHeader returns the row index of the file header that owns the
// cursor's current position, whether the cursor is on the file header itself
// or on a code line within that file. Returns (-1, false) if the cursor is
// not within any file's rows.
func (m Model) cursorFileHeader() (int, bool) {
	if len(m.diffChunks) == 0 || m.diffCurChunk < 0 || m.diffCurChunk >= len(m.diffChunks) {
		return 0, false
	}
	cur := m.diffChunks[m.diffCurChunk]
	if m.diffCurLine < 0 || m.diffCurLine >= len(cur) {
		return 0, false
	}
	headLen := m.diffHeadLen()
	rowIdx := cur[m.diffCurLine] - headLen
	if rowIdx < 0 || rowIdx >= len(m.diffRows) {
		return 0, false
	}
	hdrIdx := diffFileHeaderForRow(m.diffRows, rowIdx)
	if hdrIdx < 0 {
		return 0, false
	}
	return hdrIdx, true
}

// toggleDiffCollapse flips the collapsed state of the file at fileHeaderIdx,
// then recomputes the layout and chunk cursor so navigation stays consistent.
// The cursor is kept on the toggled file header.
func (m *Model) toggleDiffCollapse(fileHeaderIdx int) {
	if fileHeaderIdx < 0 || fileHeaderIdx >= len(m.diffRows) {
		return
	}
	if m.diffRows[fileHeaderIdx].kind != rowFileHeader {
		return
	}
	path := m.diffRows[fileHeaderIdx].path
	if m.diffCollapsed == nil {
		m.diffCollapsed = map[string]bool{}
	}
	m.diffCollapsed[path] = !m.diffCollapsed[path]
	m.computeDiffLayout()
	m.diffChunks = computeDiffChunks(m.diffRows, m.diffHeadLen(), m.diffCollapsed)

	// Find the file header in the new chunk list and keep the cursor on it.
	headLen := m.diffHeadLen()
	targetRow := headLen + fileHeaderIdx
	found := false
	for i, chunk := range m.diffChunks {
		if len(chunk) == 1 && chunk[0] == targetRow {
			m.diffCurChunk = i
			m.diffCurLine = 0
			found = true
			break
		}
	}
	if !found {
		if len(m.diffChunks) == 0 {
			m.diffCurChunk = 0
			m.diffCurLine = 0
		} else {
			if m.diffCurChunk >= len(m.diffChunks) {
				m.diffCurChunk = len(m.diffChunks) - 1
			}
			if m.diffCurChunk < 0 {
				m.diffCurChunk = 0
			}
			if m.diffCurLine >= len(m.diffChunks[m.diffCurChunk]) {
				m.diffCurLine = len(m.diffChunks[m.diffCurChunk]) - 1
			}
			if m.diffCurLine < 0 {
				m.diffCurLine = 0
			}
		}
	}
	m.diffClampMax()
	m.diffFollowCursor()
}

// computeDiffChunks groups contiguous addition/deletion lines into chunks,
// recording each line's body-row index. File header rows are included as
// single-element navigable chunks so the cursor can land on them (for
// collapse/expand). Hunk headers and context lines break chunks. Rows inside
// collapsed files are skipped (but the collapsed file's header is kept).
func computeDiffChunks(rows []diffRow, headLen int, collapsed map[string]bool) [][]int {
	hidden := collapsedRowSet(rows, collapsed)
	var chunks [][]int
	var cur []int
	flush := func() {
		if len(cur) > 0 {
			chunks = append(chunks, cur)
			cur = nil
		}
	}
	for i, r := range rows {
		if hidden != nil && hidden[i] {
			flush()
			continue
		}
		if r.kind == rowFileHeader {
			flush()
			chunks = append(chunks, []int{headLen + i})
			continue
		}
		if r.kind == rowLine && (r.lineKind == "addition" || r.lineKind == "deletion") {
			cur = append(cur, headLen+i)
		} else {
			flush()
		}
	}
	flush()
	return chunks
}

func (m Model) selectedEntry() *jj.LogEntry {
	if len(m.entries) == 0 || m.cursor >= len(m.entries) {
		return nil
	}
	return &m.entries[m.cursor]
}

// ── Keyboard ────────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	// ctrl+c always quits, even from an unrecoverable boot error screen.
	if k == "ctrl+c" {
		return m, tea.Quit
	}

	if !m.ready {
		// Boot failed (e.g. no .jj directory). q / esc also quits so the
		// user is never trapped in the alt screen with no escape.
		if m.bootErr != "" && (k == "q" || k == "esc") {
			return m, tea.Quit
		}
		return m, nil
	}

	// A pending elevation prompt captures all keys until answered.
	if m.pendingElev != nil {
		return m.handleElevKey(k)
	}

	if m.bookmarkMode {
		return m.handleBookmarkKey(msg, k)
	}

	if m.tagMode {
		return m.handleTagKey(msg, k)
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

	// Split mode handles its own keys (including q/esc to leave) while the
	// diff panel stays open.
	if m.splitMode {
		return m.handleSplitKey(k)
	}

	// File view handles its own keys (including q/esc to leave) per phase.
	if m.view == viewFile {
		return m.handleFileKey(msg, k)
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

	// Diff panel overlays whichever view opened it (log or file).
	if m.diffOpen {
		return m.handleDiffKey(k)
	}

	return m.handleLogKey(msg, k)
}

// ── Mouse handling ──────────────────────────────────────────────────────────

// contentTopBarHeight is the number of lines above the content area (the gojo
// top bar: label row + blank row).
const contentTopBarHeight = 2

// handleMouse dispatches mouse events: wheel scrolling and click-and-drag on
// the scrollbar. The scrollbar occupies the rightmost scrollbarWidth columns of
// the content area. Each view has a 1-line title/padding row at the top of the
// content area, so the scrollbar track starts at the second content line.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}

	// Wheel events work regardless of cursor position.
	if msg.Action == tea.MouseActionPress {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.handleWheel(-1)
		case tea.MouseButtonWheelDown:
			return m.handleWheel(1)
		}
	}

	// Once a drag is in progress, keep tracking the mouse Y regardless of X
	// so the user can drift left/right without the drag slipping off the
	// narrow scrollbar column. applyScrollBarDrag clamps Y to the track.
	// Only a release ends the drag.
	if m.scrollDragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			return m.applyScrollBarDrag(msg.Y)
		case tea.MouseActionRelease:
			m.scrollDragging = false
		}
		return m, nil
	}

	// Left-click in the content area selects (or activates) the row under the
	// mouse. Clicks inside the scrollbar fall through to drag handling, and
	// modal input modes (menus, elevation prompt) ignore clicks.
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if msg.X < m.width-scrollbarWidth && !m.modalInputActive() {
			return m.handleClick(msg.Y)
		}
	}

	ch := m.contentHeight()
	trackStartY := contentTopBarHeight + 1 // +1 for the view's title/padding row
	trackH := ch - 1

	// Not dragging: a press must land inside the scrollbar to start one.
	if msg.X < m.width-scrollbarWidth || msg.X >= m.width {
		return m, nil
	}
	if msg.Y < trackStartY || msg.Y >= trackStartY+trackH || trackH < 1 {
		return m, nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			m.scrollDragging = true
			return m.applyScrollBarDrag(msg.Y)
		}
	case tea.MouseActionRelease:
		m.scrollDragging = false
	}

	return m, nil
}

// applyScrollBarDrag maps the mouse Y position to a scroll offset for the
// active view and applies it.
func (m Model) applyScrollBarDrag(mouseY int) (tea.Model, tea.Cmd) {
	ch := m.contentHeight()
	trackH := ch - 1
	if trackH < 1 {
		return m, nil
	}

	trackY := mouseY - contentTopBarHeight - 1 // 0-based within the track
	if trackY < 0 {
		trackY = 0
	}
	if trackY >= trackH {
		trackY = trackH - 1
	}

	switch {
	case m.diffOpen:
		maxScroll := m.diffMaxScroll()
		if maxScroll > 0 {
			m.diffScrollY = trackY * maxScroll / max(1, trackH-1)
		} else {
			m.diffScrollY = 0
		}
		m.diffClampMax()

	case m.view == viewHelp:
		maxScroll := helpMaxScroll(trackH)
		if maxScroll > 0 {
			m.helpScrollY = trackY * maxScroll / max(1, trackH-1)
		} else {
			m.helpScrollY = 0
		}

	case m.view == viewFile && m.fileView.phase == fileHistory:
		fv := &m.fileView
		if len(fv.hist) == 0 {
			return m, nil
		}
		var totalLines int
		for i := range fv.hist {
			totalLines += commitLines(fv.hist[i])
		}
		if totalLines <= trackH {
			return m, nil
		}
		maxLineScroll := totalLines - trackH
		targetFirstLine := trackY * maxLineScroll / max(1, trackH-1)
		idx := entryAtLine(fv.hist, targetFirstLine)
		fv.histCur = idx
		fv.histOff = idx
		m.recomputeFileHistOffset()

	case m.view == viewFile && m.fileView.phase == fileBlame:
		fv := &m.fileView
		if len(fv.lines) == 0 {
			return m, nil
		}
		// Use the cached layout (built in Update) instead of recomputing.
		bodyH := fileViewContentH(m)
		layout := fv.blameLayout
		if !fv.blameCacheValid(m.width, bodyH) {
			fv.buildBlameCache(m.width, bodyH)
			layout = fv.blameLayout
		}
		if layout.total <= bodyH {
			return m, nil
		}
		maxScroll := layout.total - bodyH
		targetTermScroll := trackY * maxScroll / max(1, trackH-1)
		// Move the cursor to the source line at the drag position so the
		// centering logic in renderFileBlame doesn't override the scroll.
		fv.cursorY = 0
		for i, s := range layout.starts {
			if s <= targetTermScroll {
				fv.cursorY = i
			} else {
				break
			}
		}

	case m.view == viewFile && m.fileView.phase == filePicker:
		return m, nil

	default:
		// Log view.
		if len(m.entries) == 0 {
			return m, nil
		}
		var totalLines int
		for i := range m.entries {
			totalLines += commitLines(m.entries[i])
		}
		if totalLines <= trackH {
			return m, nil
		}
		maxLineScroll := totalLines - trackH
		targetFirstLine := trackY * maxLineScroll / max(1, trackH-1)
		idx := entryAtLine(m.entries, targetFirstLine)
		if m.rebaseMode {
			m.rebaseDest = idx
		} else if m.squashMode {
			m.squashDest = idx
		} else {
			m.cursor = idx
		}
		m.offset = idx
		m.recomputeOffset()
	}

	return m, nil
}

// handleWheel scrolls the active view by one unit in the given direction
// (−1 = up, +1 = down).
func (m Model) handleWheel(dir int) (tea.Model, tea.Cmd) {
	switch {
	case m.diffOpen:
		if dir > 0 {
			m.diffMoveDown()
		} else {
			m.diffMoveUp()
		}

	case m.view == viewHelp:
		contentH := m.contentHeight() - 1
		maxS := helpMaxScroll(contentH)
		if dir > 0 {
			m.helpScrollY = min(maxS, m.helpScrollY+1)
		} else {
			m.helpScrollY = max(0, m.helpScrollY-1)
		}

	case m.view == viewFile && m.fileView.phase == fileBlame:
		fv := &m.fileView
		if dir > 0 {
			if fv.cursorY < len(fv.lines)-1 {
				fv.cursorY++
			}
		} else {
			if fv.cursorY > 0 {
				fv.cursorY--
			}
		}

	case m.view == viewFile && m.fileView.phase == filePicker:
		fv := &m.fileView
		if dir > 0 {
			if fv.cursor < len(fv.rows)-1 {
				fv.cursor++
			}
		} else {
			if fv.cursor > 0 {
				fv.cursor--
			}
		}

	case m.view == viewFile && m.fileView.phase == fileHistory:
		fv := &m.fileView
		if dir > 0 {
			if fv.histCur < len(fv.hist)-1 {
				fv.histCur++
			}
		} else {
			if fv.histCur > 0 {
				fv.histCur--
			}
		}
		m.recomputeFileHistOffset()

	default:
		// Log view.
		if dir > 0 {
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		} else {
			if m.cursor > 0 {
				m.cursor--
			}
		}
		m.recomputeOffset()
	}

	return m, nil
}

// handleElevKey handles input while an elevation prompt is on screen. 'y'
// or enter retries the failed operation with the suggested flag appended;
// anything else cancels and returns to the log view with the original error
// shown.
func (m Model) handleElevKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "y", "Y", "enter":
		req := m.pendingElev
		m.pendingElev = nil
		return m, req.retry()
	default:
		// Cancel: drop the prompt and return to the previous view.
		m.pendingElev = nil
		return m, nil
	}
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

// handleFilePickerKey drives the tree-style file browser. Any typed
// character launches the inline fuzzy finder (pre-filled with that
// character) as a telescoped overlay; navigation keys move/expand the tree.
func (m Model) handleFilePickerKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	if fv.fzfActive {
		return m.handleFzfKey(msg, k)
	}
	switch k {
	case "esc", "q":
		// Leave the file view entirely.
		m.view = viewLog
		m.fileView = fileViewState{}
		return m, nil
	case "up", "k":
		if fv.cursor > 0 {
			fv.cursor--
		}
		return m, nil
	case "down", "j":
		if fv.cursor < len(fv.rows)-1 {
			fv.cursor++
		}
		return m, nil
	case "home", "g":
		fv.cursor = 0
		return m, nil
	case "end", "G":
		fv.cursor = len(fv.rows) - 1
		return m, nil
	case "pgup":
		fv.cursor = max(0, fv.cursor-10)
		return m, nil
	case "pgdown":
		fv.cursor = min(len(fv.rows)-1, fv.cursor+10)
		return m, nil
	case "l", "right":
		if row := fv.curRow(); row != nil && row.node.isDir {
			row.node.expanded = true
			fv.reflow()
		}
		return m, nil
	case "h", "left":
		if row := fv.curRow(); row != nil {
			if row.node.isDir && row.node.expanded {
				row.node.expanded = false
				fv.reflow()
			} else if row.depth > 0 {
				// Jump to the parent directory.
				for i := fv.cursor; i >= 0; i-- {
					if fv.rows[i].depth < row.depth {
						fv.cursor = i
						break
					}
				}
			}
		}
		return m, nil
	case "enter", " ":
		if row := fv.curRow(); row != nil {
			if row.node.isDir {
				row.node.expanded = !row.node.expanded
				fv.reflow()
				return m, nil
			}
			path := row.node.full
			m, tick := m.startBusy("annotating " + path + "…")
			return m, tea.Batch(tick, m.loadAnnotateCmd(path))
		}
		return m, nil
	}

	// Any typed character activates the inline fuzzy finder, pre-seeded
	// with that character.
	if s, ok := typed(msg); ok && s != "" {
		if len(fv.files) == 0 {
			return m, nil
		}
		fv.fzfActive = true
		fv.fzfQuery = s
		fv.fzfCursor = 0
		fv.fzfOffset = 0
		fv.fzfFilter()
		return m, nil
	}
	return m, nil
}

// handleFzfKey drives the inline fuzzy finder overlay. Typed characters
// append to the query; backspace removes the last character; enter opens
// the selected file; esc returns to the tree.
func (m Model) handleFzfKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	switch k {
	case "esc":
		fv.fzfActive = false
		return m, nil
	case "enter":
		if fv.fzfCursor < len(fv.fzfResults) {
			path := fv.fzfResults[fv.fzfCursor].path
			fv.fzfActive = false
			m, tick := m.startBusy("annotating " + path + "…")
			return m, tea.Batch(tick, m.loadAnnotateCmd(path))
		}
		return m, nil
	case "up", "k":
		if fv.fzfCursor > 0 {
			fv.fzfCursor--
		}
		return m, nil
	case "down", "j":
		if fv.fzfCursor < len(fv.fzfResults)-1 {
			fv.fzfCursor++
		}
		return m, nil
	case "home", "g":
		fv.fzfCursor = 0
		return m, nil
	case "end", "G":
		fv.fzfCursor = max(0, len(fv.fzfResults)-1)
		return m, nil
	case "pgup":
		fv.fzfCursor = max(0, fv.fzfCursor-10)
		return m, nil
	case "pgdown":
		fv.fzfCursor = min(max(0, len(fv.fzfResults)-1), fv.fzfCursor+10)
		return m, nil
	case "backspace":
		fv.fzfQuery = trimLastRune(fv.fzfQuery)
		fv.fzfFilter()
		return m, nil
	case "ctrl+u":
		fv.fzfQuery = ""
		fv.fzfFilter()
		return m, nil
	}
	// Typed characters append to the query and re-filter.
	if s, ok := typed(msg); ok && s != "" {
		fv.fzfQuery += s
		fv.fzfFilter()
		return m, nil
	}
	return m, nil
}

func (m Model) handleFileBlameKey(k string) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	total := len(fv.lines)
	switch k {
	case "esc", "q":
		// Back to the picker.
		fv.phase = filePicker
		fv.err = ""
		return m, nil
	case "up", "k":
		if fv.cursorY > 0 {
			fv.cursorY--
		}
		return m, nil
	case "down", "j":
		if fv.cursorY < total-1 {
			fv.cursorY++
		}
		return m, nil
	case "home", "g":
		fv.cursorY = 0
		return m, nil
	case "end", "G":
		fv.cursorY = total - 1
		return m, nil
	case "pgup":
		fv.cursorY = max(0, fv.cursorY-10)
		return m, nil
	case "pgdown":
		fv.cursorY = min(total-1, fv.cursorY+10)
		return m, nil
	case "h":
		// View file history (commits that touched this file).
		path := fv.path
		m, tick := m.startBusy("loading history for " + path + "…")
		return m, tea.Batch(tick, m.loadFileHistoryCmd(path))
	case "enter":
		// Open the commit that last touched the focused line.
		if fv.cursorY >= 0 && fv.cursorY < total {
			line := fv.lines[fv.cursorY]
			return m.openRevisionDiff(line.ChangeID, line.CommitID, 0, line.Description)
		}
	}
	return m, nil
}

func (m Model) handleFileHistoryKey(k string) (tea.Model, tea.Cmd) {
	fv := &m.fileView
	switch k {
	case "esc", "q", "backspace":
		// Back to the blame view of the same file.
		fv.phase = fileBlame
		fv.err = ""
		return m, nil
	case "up", "k":
		if fv.histCur > 0 {
			fv.histCur--
		}
		m.recomputeFileHistOffset()
		return m, nil
	case "down", "j":
		if fv.histCur < len(fv.hist)-1 {
			fv.histCur++
		}
		m.recomputeFileHistOffset()
		return m, nil
	case "home", "g":
		fv.histCur = 0
		m.recomputeFileHistOffset()
		return m, nil
	case "end", "G":
		fv.histCur = len(fv.hist) - 1
		m.recomputeFileHistOffset()
		return m, nil
	case "enter":
		if fv.histCur >= 0 && fv.histCur < len(fv.hist) {
			e := fv.hist[fv.histCur]
			return m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
		}
	}
	return m, nil
}

// recomputeFileHistOffset keeps the history cursor on screen (variable-height
// commits, same windowing as the main log).
func (m *Model) recomputeFileHistOffset() {
	fv := &m.fileView
	entries := fv.hist
	if len(entries) == 0 {
		fv.histOff = 0
		return
	}
	if fv.histCur >= len(entries) {
		fv.histCur = len(entries) - 1
	}
	if fv.histCur < 0 {
		fv.histCur = 0
	}
	avail := m.contentHeight() - 1
	fv.histOff, _ = logWindow(entries, fv.histCur, fv.histOff, avail)
}

// handleDiffKey drives the diff panel navigation regardless of which view
// (log or file) opened it. Closing the diff returns to that view.
func (m Model) handleDiffKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "enter", "q", "esc":
		m.diffOpen = false
	case "up", "k":
		m.diffMoveUp()
	case "down", "j":
		m.diffMoveDown()
	case "home", "g":
		m.diffMoveTop()
	case "end", "G":
		m.diffMoveBottom()
	case "left", "h":
		if fileIdx, ok := m.cursorFileHeader(); ok {
			path := m.diffRows[fileIdx].path
			if m.diffCollapsed == nil || !m.diffCollapsed[path] {
				m.toggleDiffCollapse(fileIdx)
			}
		}
	case "right", "l":
		if fileIdx, ok := m.cursorFileHeader(); ok {
			path := m.diffRows[fileIdx].path
			if m.diffCollapsed != nil && m.diffCollapsed[path] {
				m.toggleDiffCollapse(fileIdx)
			}
		}
	case "d":
		if m.diffIsRevision && m.diffRev != "" {
			changeID := m.diffRev
			if e := m.findEntryByChangeID(changeID); e != nil && e.IsImmutable {
				m.pendingElev = &elevReq{
					flag:   "--ignore-immutable",
					reason: "target is immutable",
					retry:  func() tea.Cmd { return m.describeCmd(changeID, "--ignore-immutable") },
				}
				return m, nil
			}
			return m, m.describeCmd(changeID)
		}
	case "D":
		if m.diffIsRevision && m.diffRev != "" {
			changeID := m.diffRev
			if m.aiLoading[changeID] {
				return m, nil
			}
			m.aiLoading[changeID] = true
			m.errMsg = ""
			m.message = "AI generating message for " + changeID + "…"
			cmds := []tea.Cmd{m.aiGenerateCmd(changeID)}
			if !m.spinnerRunning {
				m.spinnerRunning = true
				cmds = append(cmds, spinnerTick())
			}
			return m, tea.Batch(cmds...)
		}
	case "n":
		if m.diffIsRevision && m.diffRev != "" {
			rev := m.diffRev
			m, tick := m.startBusy("creating change…")
			return m, tea.Batch(tick, m.newOnRevCmd(rev))
		}
	case "s":
		if m.diffIsRevision && m.diffRev != "" && len(m.diffRows) > 0 {
			m.splitMode = true
			m.splitMarked = map[int]bool{}
			m.errMsg = ""
			m.message = ""
			return m, nil
		}
	case "x":
		if m.diffIsRevision && m.diffRev != "" {
			rev := m.diffRev
			r := m.runner
			return m.busyActionCmd("absorbing "+rev+"…", actionSpec{
				run:     func() error { return r.Absorb(rev) },
				okMsg:   "absorbed " + rev,
				elevate: func(flag string) func() error { return func() error { return r.Absorb(rev, flag) } },
			})
		}
	}
	return m, nil
}

// handleSplitKey drives the split-mode interaction: space toggles file/line
// selection, c confirms, q/esc cancels, and navigation keys (up/down/left/
// right/home/end) work the same as in the diff panel.
func (m Model) handleSplitKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q", "esc":
		m.splitMode = false
		m.splitMarked = nil
		m.message = "split cancelled"
		return m, nil
	case "c":
		return m.execSplit()
	case " ":
		m.splitToggle()
		return m, nil
	case "up", "k":
		m.diffMoveUp()
		return m, nil
	case "down", "j":
		m.diffMoveDown()
		return m, nil
	case "home", "g":
		m.diffMoveTop()
		return m, nil
	case "end", "G":
		m.diffMoveBottom()
		return m, nil
	case "left", "h":
		if fileIdx, ok := m.cursorFileHeader(); ok {
			path := m.diffRows[fileIdx].path
			if m.diffCollapsed == nil || !m.diffCollapsed[path] {
				m.toggleDiffCollapse(fileIdx)
			}
		}
		return m, nil
	case "right", "l":
		if fileIdx, ok := m.cursorFileHeader(); ok {
			path := m.diffRows[fileIdx].path
			if m.diffCollapsed != nil && m.diffCollapsed[path] {
				m.toggleDiffCollapse(fileIdx)
			}
		}
		return m, nil
	}
	return m, nil
}

// findEntryByChangeID returns the log entry matching the given change ID, or
// nil if it isn't in the current log list (e.g. a revision opened from the
// file-view blame/history that isn't among the visible log entries).
func (m Model) findEntryByChangeID(changeID string) *jj.LogEntry {
	for i := range m.entries {
		if m.entries[i].ChangeID == changeID {
			return &m.entries[i]
		}
	}
	return nil
}

func (m Model) handleFileKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	// A diff opened from the blame/history sub-view overlays the file view.
	if m.diffOpen {
		return m.handleDiffKey(k)
	}
	switch m.fileView.phase {
	case fileBlame:
		return m.handleFileBlameKey(k)
	case fileHistory:
		return m.handleFileHistoryKey(k)
	default:
		return m.handleFilePickerKey(msg, k)
	}
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
			return m.openRevisionDiff(e.ChangeID, e.CommitID, e.ChangeIDPrefixLen, e.Subject)
		}
		return m, nil
	case "d":
		if e := m.selectedEntry(); e != nil {
			// The editor flow runs via ExecProcess, which attaches the terminal —
			// so jj's "is immutable" error text isn't captured and can't be
			// detected after the fact. Check the entry's immutability up front
			// and offer an elevation retry with --ignore-immutable instead.
			if e.IsImmutable {
				changeID := e.ChangeID
				m.pendingElev = &elevReq{
					flag:   "--ignore-immutable",
					reason: "target is immutable",
					retry:  func() tea.Cmd { return m.describeCmd(changeID, "--ignore-immutable") },
				}
				return m, nil
			}
			return m, m.describeCmd(e.ChangeID)
		}
		return m, nil
	case "D":
		if e := m.selectedEntry(); e != nil {
			if m.aiLoading[e.ChangeID] {
				return m, nil
			}
			m.aiLoading[e.ChangeID] = true
			m.errMsg = ""
			m.message = "AI generating message for " + e.ChangeID + "…"
			cmds := []tea.Cmd{m.aiGenerateCmd(e.ChangeID)}
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
			return m.busyActionCmd("editing "+rev+"…", actionSpec{
				run:     func() error { return r.Edit(rev) },
				okMsg:   "editing " + rev,
				elevate: func(flag string) func() error { return func() error { return r.Edit(rev, flag) } },
			})
		}
		return m, nil
	case "n":
		rev := ""
		if e := m.selectedEntry(); e != nil {
			rev = e.ChangeID
		}
		r := m.runner
		return m.busyActionCmd("creating change…", actionSpec{
			run:     func() error { return r.New(rev) },
			okMsg:   "created new change",
			elevate: func(flag string) func() error { return func() error { return r.New(rev, flag) } },
		})
	case "a":
		if e := m.selectedEntry(); e != nil {
			if e.IsWorkingCopy {
				m.errMsg = "cannot abandon the working copy"
				return m, nil
			}
			rev := e.ChangeID
			r := m.runner
			return m.busyActionCmd("abandoning "+rev+"…", actionSpec{
				run:     func() error { return r.Abandon(rev) },
				okMsg:   "abandoned " + rev,
				elevate: func(flag string) func() error { return func() error { return r.Abandon(rev, flag) } },
			})
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
	case "t":
		m.tagMode = true
		m.tagAction = ""
		m.tagInput = ""
		m.acOriginal = nil
		m.acIdx = 0
		m.errMsg = ""
		m.message = ""
		return m, nil
	case "f":
		// File view: browse tracked files, open one with blame, inspect history.
		m.view = viewFile
		m.fileView = fileViewState{phase: filePicker}
		m.fileView.err = ""
		m.errMsg = ""
		m.message = ""
		m, tick := m.startBusy("listing files…")
		return m, tea.Batch(tick, m.loadFileListCmd())
	case "g":
		m.gitMode = true
		m.errMsg = ""
		m.message = ""
		return m, nil
	case "u":
		r := m.runner
		return m.busySimpleCmd("undoing…", func() error { return r.Undo() }, "undone")
	case "R":
		r := m.runner
		return m.busySimpleCmd("redoing…", func() error { return r.Redo() }, "redone")
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
	case "x":
		if e := m.selectedEntry(); e != nil {
			rev := e.ChangeID
			r := m.runner
			return m.busyActionCmd("absorbing "+rev+"…", actionSpec{
				run:     func() error { return r.Absorb(rev) },
				okMsg:   "absorbed " + rev,
				elevate: func(flag string) func() error { return func() error { return r.Absorb(rev, flag) } },
			})
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
	return m.busyActionCmd("rebasing…", actionSpec{
		run:   func() error { return r.Rebase(srcFlag, src, placeFlag, dest) },
		okMsg: "rebased " + src + " " + label + " " + dest,
		elevate: func(flag string) func() error {
			return func() error { return r.Rebase(srcFlag, src, placeFlag, dest, flag) }
		},
	})
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
			m, tick := m.startBusy("bookmark " + action + "…")
			return m, tea.Batch(tick, m.execBookmark(action, input))
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
		m, tick := m.startBusy("loading bookmarks…")
		return m, tea.Batch(tick, m.execBookmark("l", ""))
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
				m, tick := m.startBusy("remote " + action + "…")
				return m, tea.Batch(tick, m.execRemote(action, input))
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
			m, tick := m.startBusy("loading remotes…")
			return m, tea.Batch(tick, m.execRemote("l", ""))
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
		return m.busyActionCmd("fetching…", actionSpec{
			run:     func() error { return r.GitFetch() },
			okMsg:   "fetched",
			elevate: func(flag string) func() error { return func() error { return r.GitFetch(flag) } },
		})
	case "p":
		m.gitMode = false
		r := m.runner
		return m.busyActionCmd("pushing…", actionSpec{
			run:     func() error { return r.GitPush() },
			okMsg:   "pushed",
			elevate: func(flag string) func() error { return func() error { return r.GitPush(flag) } },
		})
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
	// run performs the bookmark action; runElevated re-runs it with an extra
	// trailing flag for elevation retries.
	run := func(extra string) error {
		switch action {
		case "c":
			return r.BookmarkCreate(input, rev, extra)
		case "d":
			return r.BookmarkDelete(input, extra)
		case "f":
			return r.BookmarkForget(input, extra)
		case "m":
			return r.BookmarkMove(input, rev, extra)
		case "r":
			parts := strings.Fields(input)
			if len(parts) < 2 {
				return errors.New("rename requires: <old> <new>")
			}
			return r.BookmarkRename(parts[0], parts[1])
		case "s":
			return r.BookmarkSet(input, rev, extra)
		case "t":
			return r.BookmarkTrack(input)
		case "T":
			return r.BookmarkUntrack(input)
		}
		return nil
	}
	okMsg := "bookmark " + action + ": " + input
	return func() tea.Msg {
		if err := run(""); err != nil {
			if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
				return actionDoneMsg{
					err: err,
					elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry:  func() tea.Cmd { return m.syncFnCmd(func() error { return run(flag) }, okMsg) },
					},
				}
			}
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: okMsg, refresh: true}
	}
}

func (m Model) handleTagKey(msg tea.KeyMsg, k string) (tea.Model, tea.Cmd) {
	if m.tagAction != "" {
		switch k {
		case "esc":
			if m.acOriginal != nil {
				m.tagInput = *m.acOriginal
				m.acOriginal = nil
				m.acIdx = 0
				return m, nil
			}
			m.tagAction = ""
			m.tagInput = ""
			m.acOriginal = nil
			m.acIdx = 0
			return m, nil
		case "enter":
			action := m.tagAction
			input := m.tagInput
			m.acOriginal = nil
			m.acIdx = 0
			m.tagMode = false
			m.tagAction = ""
			m.tagInput = ""
			if action == "p" {
				r := m.runner
				return m.busySimpleCmd("pushing tags…", func() error { return r.GitPushTags() }, "pushed tags")
			}
			m, tick := m.startBusy("tag " + action + "…")
			return m, tea.Batch(tick, m.execTag(action, input))
		case "tab":
			prefix := m.tagInput
			if m.acOriginal != nil {
				prefix = *m.acOriginal
			}
			filtered := filterPrefix(m.candidates(), prefix)
			if len(filtered) > 0 {
				if m.acOriginal == nil {
					orig := m.tagInput
					m.acOriginal = &orig
					m.acIdx = 0
					m.tagInput = filtered[0]
				} else {
					m.acIdx = (m.acIdx + 1) % len(filtered)
					m.tagInput = filtered[m.acIdx]
				}
			}
			return m, nil
		case "backspace", "delete":
			m.tagInput = trimLastRune(m.tagInput)
			m.acOriginal = nil
			m.acIdx = 0
			return m, nil
		}
		if s, ok := typed(msg); ok {
			m.tagInput += s
			m.acOriginal = nil
			m.acIdx = 0
		}
		return m, nil
	}

	// Tag menu.
	switch k {
	case "esc", "q":
		m.tagMode = false
		m.acOriginal = nil
		m.acIdx = 0
		return m, nil
	case "s", "m", "d":
		m.tagAction = k
		m.tagInput = ""
		m.acOriginal = nil
		m.acIdx = 0
		return m, nil
	case "l":
		m.tagMode = false
		m, tick := m.startBusy("loading tags…")
		return m, tea.Batch(tick, m.execTag("l", ""))
	case "p":
		m.tagMode = false
		r := m.runner
		return m.busySimpleCmd("pushing tags…", func() error { return r.GitPushTags() }, "pushed tags")
	}
	return m, nil
}

func (m Model) execTag(action, input string) tea.Cmd {
	r := m.runner
	rev := ""
	if e := m.selectedEntry(); e != nil {
		rev = e.ChangeID
	}
	if action == "l" {
		return listCmd(r.TagList, "tag list")
	}
	// run performs the tag action; runElevated re-runs it with an extra
	// trailing flag for elevation retries.
	run := func(extra string) error {
		switch action {
		case "s":
			return r.TagSet(input, rev, extra)
		case "m":
			return r.TagSet(input, rev, "--allow-move", extra)
		case "d":
			return r.TagDelete(input, extra)
		}
		return nil
	}
	okMsg := "tag " + action + ": " + input
	return func() tea.Msg {
		if err := run(""); err != nil {
			if flag, reason := jj.DetectElevation(err.Error()); flag != "" {
				return actionDoneMsg{
					err: err,
					elev: &elevReq{
						flag:   flag,
						reason: reason,
						retry:  func() tea.Cmd { return m.syncFnCmd(func() error { return run(flag) }, okMsg) },
					},
				}
			}
			return actionDoneMsg{err: err}
		}
		return actionDoneMsg{message: okMsg, refresh: true}
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
		for _, tg := range e.Tags {
			if tg != "" && !strings.Contains(tg, "@") {
				add(tg)
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
	if m.bookmarkAction == "" && m.tagAction == "" {
		return nil
	}
	if m.bookmarkAction == "r" && strings.Contains(m.bookmarkInput, " ") {
		return nil
	}
	var prefix string
	if m.tagAction != "" {
		prefix = m.tagInput
	} else {
		prefix = m.bookmarkInput
	}
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
	return (m.bookmarkAction != "" || m.tagAction != "") && len(m.displaySuggestions()) > 0
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

// renderTopBar renders the animated top bar: the Six Eyes glyph (◉) whose
// colour cycles through Gojo's energy palette, occasionally flashing the
// Infinity symbol (∞), with a traveling energy pulse along the separator.
func (m Model) renderTopBar() string {
	dp := m.cwd
	if m.home != "" && strings.HasPrefix(m.cwd, m.home) {
		dp = "~" + m.cwd[len(m.home):]
	}
	label := " ◆ gojo"
	labelW := lipgloss.Width(label)
	pathW := len([]rune(dp)) + 1
	gapW := max(0, m.width-labelW-pathW-1)

	var segs []seg
	segs = append(segs, seg{text: label, fg: colPurple, bold: true, bg: colElement})
	if gapW > 0 {
		segs = append(segs, seg{text: strings.Repeat(" ", gapW), bg: colElement})
	}
	segs = append(segs, seg{text: dp + " ", fg: colTextMuted, bg: colElement})

	return bgRow(m.width, colElement, segs...)
}

// View renders the full screen.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.bootErr != "" {
		lines := []string{
			bgRow(m.width, colPanel, seg{text: " error: " + m.bootErr + " ", fg: colRed}),
			blankRow(m.width, colPanel),
			bgRow(m.width, colPanel, seg{text: " press q or ctrl+c to quit", fg: colGray}),
		}
		return strings.Join(padLines(lines, m.height, m.width), "\n")
	}
	if !m.ready || (len(m.entries) == 0 && m.errMsg == "") {
		lines := []string{bgRow(m.width, colPanel, seg{text: " loading…", fg: colGray})}
		return strings.Join(padLines(lines, m.height, m.width), "\n")
	}

	var lines []string

	// Top bar (2 lines) — subtle panel surface.
	lines = append(lines, m.renderTopBar())
	lines = append(lines, blankRow(m.width, colElement))

	// Content area.
	ch := m.contentHeight()
	switch {
	case m.view == viewHelp:
		lines = append(lines, renderHelp(m.width, ch, m.helpScrollY)...)
	case m.diffOpen:
		sv := splitView{active: m.splitMode, marked: m.splitMarked}
		lines = append(lines, renderDiffPanel(m.width, ch, m.diffRev, m.diffRevPrefix, m.diffLoading, m.aiLoading[m.diffRev], m.spinnerFrame, m.diffDesc, m.diffIsRevision, m.diffRows, m.diffDigits, m.diffStatus, m.diffRaw, m.diffScrollY, m.diffCursorBodyRow(), m.diffChunkRows(), m.diffCollapsed, sv, false, nil, nil)...)
	case m.view == viewFile:
		lines = append(lines, m.renderFileView(m.width, ch)...)
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

	// Status bar + help bar + bottom blank strip.
	lines = append(lines, m.renderStatusBar()...)
	lines = append(lines, m.renderHelpBar()...)
	lines = append(lines, blankRow(m.width, colPanel))

	return strings.Join(padLines(lines, m.height, m.width), "\n")
}

// renderFileStatusBar renders the file-view status bar. In blame phase it
// surfaces the git-blame-style info for the focused line (the commit that
// last edited it and its author).
func (m Model) renderFileStatusBar() []string {
	fv := &m.fileView
	switch fv.phase {
	case filePicker:
		if fv.fzfActive {
			text := fmt.Sprintf(" fzf · %d/%d files", len(fv.fzfResults), len(fv.files))
			return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colGray})}
		}
		text := fmt.Sprintf(" file browser · %d files · type to fzf", len(fv.files))
		if fv.err != "" {
			text = " ✖ " + fv.err
			return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colRed})}
		}
		return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colGray})}
	case fileHistory:
		text := fmt.Sprintf(" history · %d commits · all()", len(fv.hist))
		if fv.err != "" {
			text = " ✖ " + fv.err
			return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colRed})}
		}
		return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colGray})}
	default: // fileBlame
		if fv.err != "" {
			return []string{bgRow(m.width, colDarkerGray, seg{text: " ✖ " + fv.err, fg: colRed})}
		}
		if len(fv.lines) == 0 {
			return []string{bgRow(m.width, colDarkerGray, seg{text: " " + fv.path, fg: colGray})}
		}
		cur := max(0, min(fv.cursorY, len(fv.lines)-1))
		l := fv.lines[cur]
		segs := []seg{
			{text: " blame ", fg: colGray},
			{text: l.ChangeID, fg: colPurple, bold: true},
			{text: " "},
			{text: l.CommitID, fg: colGray},
			{text: " "},
			{text: l.Author, fg: colBlue},
			{text: fmt.Sprintf("  L%d/%d", l.LineNo, len(fv.lines)), fg: colGray},
		}
		return []string{bgRow(m.width, colDarkerGray, segs...)}
	}
}

func (m Model) renderSuggestions() string {
	sugg := m.displaySuggestions()
	segs := []seg{{text: " tab:", fg: colBorderSubtle}}
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
	return bgRow(m.width, colPanel, segs...)
}

func (m Model) renderStatusBar() []string {
	switch {
	case m.view == viewFile:
		return m.renderFileStatusBar()
	case m.pendingElev != nil:
		segs := []seg{
			{text: " ⚠ retry with ", fg: colYellow},
			{text: m.pendingElev.flag, fg: colYellow, bold: true},
			{text: "? (" + m.pendingElev.reason + ")  ", fg: colYellow},
			{text: "y confirm", fg: colPurple, underline: true},
			{text: " · ", fg: colGray},
			{text: "n/esc cancel", fg: colGray},
		}
		return []string{bgRow(m.width, colDarkerGray, segs...)}

	case len(m.busy) > 0:
		// Prominent spinner for in-flight background actions (push, fetch,
		// rebase, …). The most recent label leads; a count badge follows when
		// several overlap.
		label := m.busy[len(m.busy)-1]
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		segs := []seg{
			{text: " " + frame + " ", fg: colMagenta},
			{text: label, fg: colWhite},
		}
		if n := len(m.busy); n > 1 {
			segs = append(segs, seg{text: fmt.Sprintf("  (×%d)", n), fg: colGray})
		}
		return []string{bgRow(m.width, colDarkerGray, segs...)}

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

	case m.tagMode:
		if m.tagAction != "" {
			prompts := map[string]string{
				"s": "set to " + m.selChangeID() + ": ",
				"m": "move to " + m.selChangeID() + ": ",
				"d": "delete: ",
			}
			text := " [tag] " + prompts[m.tagAction] + m.tagInput + "█"
			return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colTeal})}
		}
		return m.renderMenuRows(" [tag mode] ", colTeal, colPurple, tagMenuItems)

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

	case m.splitMode:
		segs := []seg{{text: " [split] ", fg: colYellow, bold: true}}
		segs = append(segs, seg{text: m.diffRev, fg: colMagenta, bold: true})
		segs = append(segs, seg{text: " · space toggle · c confirm · esc cancel · ↑/↓ navigate", fg: colGray})
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
	{"AI Desc", "D"}, {"bookmark", "b"}, {"tag", "t"}, {"git", "g"},
	{"undo", "u"}, {"rebase", "r"}, {"squash", "s"}, {"absorb", "x"}, {"edit", "e"}, {"new", "n"},
	{"abandon", "a"}, {"file", "f"}, {"?help", "?"}, {"quit", "q"},
}

// helpBarItems returns the shortcut hints shown in the bottom help bar for the
// current context. It returns nil when the help bar should be hidden entirely
// (e.g. subcommand modes whose key hints are already surfaced in the status
// bar), so the content area can reclaim that row.
func (m Model) helpBarItems() [][2]string {
	switch {
	case m.splitMode:
		return [][2]string{
			{"space toggle", "space"}, {"c confirm", "c"}, {"esc cancel", "esc"},
			{"↑/k chunk↑", "↑"}, {"↓/j chunk↓", "↓"},
			{"g top", "g"}, {"G bot", "G"}, {"←/→ fold", "←"},
		}
	case m.diffOpen:
		return [][2]string{
			{"⏎ close", "⏎"}, {"↑/k chunk↑", "↑"}, {"↓/j chunk↓", "↓"},
			{"g top", "g"}, {"G bot", "G"}, {"←/→ fold", "←"},
		{"describe", "d"}, {"AI Desc", "D"}, {"new", "n"}, {"split", "s"}, {"absorb", "x"},
		{"q close", "q"},
		}
	case m.view == viewFile:
		switch m.fileView.phase {
		case fileBlame:
			return [][2]string{
				{"↑/k", "↑"}, {"↓/j", "↓"}, {"g/G top/bot", "g"},
				{"history", "h"}, {"open commit", "⏎"}, {"back", "esc/q"},
			}
		case fileHistory:
			return [][2]string{
				{"↑/k", "↑"}, {"↓/j", "↓"}, {"open commit", "⏎"},
				{"back", "esc/q"},
			}
	default:
		if m.fileView.fzfActive {
			return [][2]string{
				{"type", "filter"}, {"⌫ del", "backspace"}, {"⏎ open", "⏎"},
				{"↑/k ↓/j", "nav"}, {"esc back", "esc"},
			}
		}
		return [][2]string{
			{"↑/k", "↑"}, {"↓/j", "↓"}, {"⏎/l open", "⏎"}, {"h collapse", "h"},
			{"type→fzf", "f"}, {"quit", "q"},
		}
		}
	case m.view == viewHelp:
		return [][2]string{
			{"↑/k", "↑"}, {"↓/j", "↓"}, {"?/q close", "?"},
		}
	case m.rebaseMode, m.squashMode,
		m.bookmarkMode, m.tagMode, m.gitMode:
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
	tagMenuItems = [][2]string{
		{"set", "s"}, {"move", "m"}, {"delete", "d"},
		{"list", "l"}, {"push", "p"},
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
	return len(wrapMenu(m.width, " ", colTextMuted, colPurple, "  ", items))
}

// renderHelpBar renders the context-specific shortcut hints, wrapping onto
// extra rows when the terminal is too narrow to fit them all on one line.
func (m Model) renderHelpBar() []string {
	items := m.helpBarItems()
	if items == nil {
		return nil
	}
	packed := wrapMenu(m.width, " ", colTextMuted, colPurple, "  ", items)
	out := make([]string, len(packed))
	for i, row := range packed {
		out[i] = bgRow(m.width, colPanel, row...)
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
