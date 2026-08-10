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

// helpSections builds the keybinding reference from the configured keymap,
// so the help view always shows the actually-active bindings.
func (m Model) helpSections() []helpSection {
	kv := m.hk
	kvs := func(ctx, a string) string { return m.hkN(ctx, a, 2, "/") } // up to 2 keys
	return []helpSection{
		{title: "Global", color: colWhite, bindings: []helpBinding{
			{kv(ctxGlobal, actHelp), "this help"},
			{kv(ctxGlobal, actQuit), "quit / close panel"},
			{kv(ctxGlobal, actForceQuit), "force quit"},
		}},
		{title: "Mouse", color: colWhite, bindings: []helpBinding{
			{"wheel", "scroll / move cursor"},
			{"click", "select row / move cursor"},
			{"click selected", "activate  (open diff / file / commit)"},
			{"click ~ row", "toggle all revisions  (all())"},
			{"click file header", "diff: toggle collapse · split: toggle file"},
			{"click diff line", "split: toggle mark · diff: move cursor"},
			{"drag scrollbar", "scroll any view"},
			{"right-click", "open context menu"},
		}},
		{title: "Log View", color: colBlue, bindings: []helpBinding{
			{kvs(ctxLog, actUp) + ", " + kvs(ctxLog, actDown), "navigate commits  (↓ onto ~ elided rows)"},
			{kv(ctxLog, actTop), "first commit"},
			{kv(ctxLog, actBottom), "last commit"},
			{kv(ctxLog, actOpen), "open diff panel  · toggle all() on ~ row"},
			{kv(ctxLog, actSearch), "search revisions  (fzf-style fuzzy finder)"},
			{kv(ctxLog, actFiles), "file view  (browse / blame / history)"},
			{kv(ctxLog, actDescribe), "jj describe  ($EDITOR)"},
			{kv(ctxLog, actAIDescribe), "AI generate commit message"},
			{kv(ctxLog, actEdit), "jj edit  (set working copy)"},
			{kv(ctxLog, actNew), "jj new  (create change)"},
			{kv(ctxLog, actAbandon), "jj abandon  (remove commit)"},
			{kv(ctxLog, actAllRev), "toggle all revisions  (all())"},
			{kv(ctxLog, actBookmark), "bookmark mode"},
			{kv(ctxLog, actTag), "tag mode"},
			{kv(ctxLog, actGit), "git mode"},
			{kv(ctxLog, actUndo), "jj undo"},
			{kv(ctxLog, actRedo), "jj redo"},
			{kv(ctxLog, actRebase), "rebase mode"},
			{kv(ctxLog, actSquash), "squash mode"},
			{kv(ctxLog, actAbsorb), "jj absorb  (move changes into ancestors)"},
			{kv(ctxLog, actTheme), "theme picker  (live preview, saves to gojo.toml)"},
		}},
		{title: "Theme Picker", color: colMagenta, bindings: []helpBinding{
			{kv(ctxLog, actTheme) + " (from log)", "open the theme picker"},
			{kvs(ctxTheme, actUp) + ", " + kvs(ctxTheme, actDown), "browse themes  (live-previewed)"},
			{kv(ctxTheme, actApply), "apply & save theme to ~/.config/gojo/gojo.toml"},
			{m.hkN(ctxTheme, actCancel, 2, " / "), "cancel  (restore previous theme)"},
			{"terminal", "built-in theme that follows your terminal colors"},
			{"~/.config/gojo/themes/*.toml", "drop in your own themes  (one file each)"},
		}},
		{title: "Search Mode", color: colCyan, bindings: []helpBinding{
			{kv(ctxLog, actSearch) + " (from log)", "open the revision search"},
			{"type", "fuzzy-filter by change/git ID, description, author, bookmark, tag"},
			{kv(ctxSearch, actErase), "remove last character"},
			{kv(ctxSearch, actClear), "clear query"},
			{kv(ctxSearch, actUp) + "/" + kv(ctxSearch, actDown), "navigate results"},
			{kv(ctxSearch, actTop) + " / " + kv(ctxSearch, actBottom), "first / last result"},
			{kv(ctxSearch, actAccept), "jump cursor to selected revision"},
			{m.hkN(ctxSearch, actCancel, 2, " / "), "cancel  (return to log)"},
		}},
		{title: "Rebase Mode", color: colYellow, bindings: []helpBinding{
			{kv(ctxLog, actRebase), "pick up selected commit"},
			{kvs(ctxRebase, actUp) + ", " + kvs(ctxRebase, actDown), "move destination"},
			{kv(ctxRebase, actTop) + " / " + kv(ctxRebase, actBottom), "destination to top / bottom"},
			{kv(ctxRebase, actScope), "toggle scope  (-r single ⇄ -s subtree)"},
			{kv(ctxRebase, actPlace), "cycle placement  (onto / after / before)"},
			{kv(ctxRebase, actConfirm), "confirm rebase"},
			{m.hkN(ctxRebase, actCancel, 2, " / "), "cancel"},
		}},
		{title: "Squash Mode", color: colYellow, bindings: []helpBinding{
			{kv(ctxLog, actSquash), "pick selected commit to squash"},
			{kvs(ctxSquash, actUp) + ", " + kvs(ctxSquash, actDown), "move destination"},
			{kv(ctxSquash, actTop) + " / " + kv(ctxSquash, actBottom), "destination to top / bottom"},
			{kv(ctxSquash, actConfirm), "confirm  (fold changes into destination)"},
			{m.hkN(ctxSquash, actCancel, 2, " / "), "cancel"},
		}},
		{title: "Diff Panel", color: colGreen, bindings: []helpBinding{
			{kvs(ctxDiff, actUp) + ", " + kvs(ctxDiff, actDown), "move chunk cursor  (view centers)"},
			{"wheel / " + kvs(ctxDiff, actPageUp) + " / " + kvs(ctxDiff, actPageDown), "scroll view  (cursor stays)"},
			{kv(ctxDiff, actTop) + " / " + kv(ctxDiff, actBottom), "jump top / bottom"},
			{m.hkN(ctxDiff, actCollapse, 2, "/") + " or " + m.hkN(ctxDiff, actExpand, 2, "/"), "collapse / expand file"},
			{"click file", "toggle collapse"},
			{kv(ctxDiff, actDescribe), "edit description"},
			{kv(ctxDiff, actAIDescribe), "AI describe"},
			{kv(ctxDiff, actNew), "jj new  (create change on top)"},
			{kv(ctxDiff, actSplit), "jj split  (interactive split mode)"},
			{kv(ctxDiff, actAbsorb), "jj absorb  (move changes into ancestors)"},
			{m.hkN(ctxDiff, actClose, 2, " / "), "close diff"},
		}},
		{title: "Split Mode", color: colYellow, bindings: []helpBinding{
			{kv(ctxDiff, actSplit) + " (in diff)", "enter split mode"},
			{kv(ctxSplit, actToggle), "toggle file / line selection"},
			{kv(ctxSplit, actConfirm), "confirm split"},
			{m.hkN(ctxSplit, actCancel, 2, " / "), "cancel  (back to diff)"},
			{kvs(ctxSplit, actUp) + ", " + kvs(ctxSplit, actDown), "navigate chunks"},
			{kv(ctxSplit, actTop) + " / " + kv(ctxSplit, actBottom), "jump top / bottom"},
			{m.hkN(ctxSplit, actCollapse, 2, "/") + " or " + m.hkN(ctxSplit, actExpand, 2, "/"), "collapse / expand file"},
			{"[x]", "marked  (keep in current revision)"},
			{"[~]", "partial  (some lines marked)"},
			{"[ ]", "unmarked  (split into preceding revision)"},
		}},
		{title: "File View", color: colMagenta, bindings: []helpBinding{
			{kv(ctxLog, actFiles) + " (from log)", "open the file browser"},
			{kvs(ctxPicker, actUp) + ", " + kvs(ctxPicker, actDown), "navigate tree / lines"},
			{m.hkN(ctxPicker, actExpand, 2, " / "), "expand directory"},
			{m.hkN(ctxPicker, actCollapse, 2, " / "), "collapse directory / up"},
			{m.hkN(ctxPicker, actOpen, 0, " / "), "open file  (or toggle dir)"},
			{"type any char", "launch inline fuzzy picker"},
			{kv(ctxBlame, actHistory), "file history  (all())"},
			{kv(ctxBlame, actOpen), "open the line's commit"},
			{kv(ctxBlame, actTop) + " / " + kv(ctxBlame, actBottom), "jump top / bottom"},
			{m.hkN(ctxPicker, actQuit, 2, " / "), "back a step / quit"},
		}},
		{title: "Help View", color: colPurple, bindings: []helpBinding{
			{kvs(ctxHelp, actUp) + ", " + kvs(ctxHelp, actDown), "scroll help"},
			{kvs(ctxHelp, actPageUp), "scroll up half page"},
			{kvs(ctxHelp, actPageDown), "scroll down half page"},
			{kv(ctxHelp, actTop), "jump to top"},
			{kv(ctxHelp, actBottom), "jump to bottom"},
			{kv(ctxGlobal, actHelp) + " / " + kv(ctxGlobal, actQuit), "close help"},
		}},
		{title: "Conflict View", color: colRed, bindings: []helpBinding{
			{kv(ctxLog, actConflict), "open conflict resolution  (from log / diff)"},
			{"⚡ conflict", "log badge marking conflicted commits"},
			{kvs(ctxConflict, actUp) + ", " + kvs(ctxConflict, actDown), "previous / next conflict hunk"},
			{m.hkN(ctxConflict, actPickLeft, 0, " / "), "take the left side  (side 1)"},
			{m.hkN(ctxConflict, actPickRight, 0, " / "), "take the right side  (side 2)"},
			{kv(ctxConflict, actPickBoth), "take both  (left lines, then right)"},
			{kv(ctxConflict, actPickUnset), "undo the pick for this hunk"},
			{kv(ctxConflict, actPrevFile) + " / " + kv(ctxConflict, actNextFile), "switch conflicted file"},
			{"click pane", "pick that side for the hunk under the mouse"},
			{kv(ctxConflict, actPageUp) + "/" + kv(ctxConflict, actPageDown), "scroll half a page"},
			{kv(ctxConflict, actApply), "apply the file's resolution  (jj resolve)"},
			{m.hkN(ctxConflict, actClose, 2, " / "), "abort  (leave the view, nothing applied)"},
		}},
		{title: "Bookmark Mode", color: colCyan, bindings: []helpBinding{
			{kv(ctxBookmark, actCreate), "create bookmark"},
			{kv(ctxBookmark, actDelete), "delete bookmark"},
			{kv(ctxBookmark, actForget), "forget bookmark"},
			{kv(ctxBookmark, actList), "list bookmarks"},
			{kv(ctxBookmark, actMove), "move bookmark"},
			{kv(ctxBookmark, actRename), "rename bookmark"},
			{kv(ctxBookmark, actSet), "set bookmark"},
			{kv(ctxBookmark, actTrack), "track bookmark"},
			{kv(ctxBookmark, actUntrack), "untrack bookmark"},
			{kv(ctxInput, actComplete), "autocomplete  (cycle suggestions)"},
			{kv(ctxBookmark, actCancel), "dismiss / cancel / exit"},
		}},
		{title: "Tag Mode", color: colTeal, bindings: []helpBinding{
			{kv(ctxTag, actSet), "set tag  (create at selected revision)"},
			{kv(ctxTag, actMove), "move tag  (move existing to selected revision)"},
			{kv(ctxTag, actDelete), "delete tag"},
			{kv(ctxTag, actList), "list tags"},
			{kv(ctxTag, actPush), "push tags  (git push --tags)"},
			{kv(ctxInput, actComplete), "autocomplete  (cycle suggestions)"},
			{kv(ctxTag, actCancel), "dismiss / cancel / exit"},
		}},
		{title: "Git Mode", color: colOrange, bindings: []helpBinding{
			{kv(ctxGit, actFetch), "git fetch"},
			{kv(ctxGit, actPush), "git push"},
			{kv(ctxGit, actPushMark), "push bookmark  (bookmark [remote]; tab completes)"},
			{kv(ctxGit, actRemote), "remote mode"},
			{m.hkN(ctxGit, actCancel, 2, " / "), "cancel / exit"},
		}},
		{title: "Remote Mode", color: colPink, bindings: []helpBinding{
			{kv(ctxRemote, actAdd), "add remote  (name url)"},
			{kv(ctxRemote, actList), "list remotes"},
			{kv(ctxRemote, actRemove), "remove remote  (name)"},
			{kv(ctxRemote, actRename), "rename remote  (old new)"},
			{kv(ctxRemote, actSetURL), "set-url  (name url)"},
			{m.hkN(ctxRemote, actCancel, 2, " / "), "cancel / exit"},
		}},
	}
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

// helpRows expands the model's help sections into the flat row table. Built
// per call; help content is small (~150 rows) and renders at most once per
// frame, so a rebuild keeps it in sync with the configured keymap.
func (m Model) helpRows() []helpRow {
	sections := m.helpSections()
	var rows []helpRow
	for i := range sections {
		s := &sections[i]
		rows = append(rows, helpRow{kind: helpBlank})
		rows = append(rows, helpRow{kind: helpTitle, section: s})
		rows = append(rows, helpRow{kind: helpSep})
		for _, b := range s.bindings {
			rows = append(rows, helpRow{kind: helpBindingRow, section: s, binding: b})
		}
	}
	return rows
}

func (m Model) helpTotalRows() int { return len(m.helpRows()) }

func (m Model) helpMaxScroll(contentHeight int) int {
	if mm := m.helpTotalRows() - contentHeight; mm > 0 {
		return mm
	}
	return 0
}

// renderHelp produces exactly height lines (including the title bar).
// A scrollbar on the right edge shows position when the help content overflows.
func (m Model) renderHelp(width, height, scrollY int) []string {
	rows := m.helpRows()
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
	titleRight := fmt.Sprintf("(%d-%d/%d) %s/%s close ", clampedY+1, min(clampedY+contentH, total), total,
		m.hk(ctxGlobal, actHelp), m.hk(ctxGlobal, actQuit))
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
		out = append(out, blankRow(width, colPanel))
	}
	if len(out) > height {
		out = out[:height]
	}
	return out
}
