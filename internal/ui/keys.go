package ui

// Configurable keybindings. Every key gojo reacts to lives in a KeyMap: a
// per-context table from key name (as reported by tea.KeyMsg.String()) to a
// named action. Handlers resolve the incoming key through the active context
// and switch on the action, so any binding can be changed via the [keymap]
// section of ~/.config/gojo/gojo.toml (or [tools.gojo.keymap] in jj's
// config.toml), e.g.
//
//	[keymap]
//	log.down = "j,down"      # comma-separated alternates (this is the default)
//	global.quit = "Q"        # rebind quit to Q
//	diff.absorb = ""         # unbind entirely
//
// Overrides replace an action's whole binding list; unmentioned actions keep
// their defaults. Unknown context/action names are ignored.

import (
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// Key contexts — the config-key prefix before the dot.
const (
	ctxGlobal   = "global"   // always active (quit, help)
	ctxBoot     = "boot"     // boot init prompt / boot error screen
	ctxElev     = "elevate"  // elevation retry prompt (any other key cancels)
	ctxMenu     = "menu"     // right-click context menu
	ctxInput    = "input"    // shared text-input editing (bookmark/tag/push/remote inputs)
	ctxLog      = "log"      // commit list
	ctxDiff     = "diff"     // diff panel
	ctxSplit    = "split"    // interactive split mode
	ctxHelp     = "help"     // help view scrolling
	ctxSearch   = "search"   // revision search overlay
	ctxRebase   = "rebase"   // rebase destination picking
	ctxSquash   = "squash"   // squash destination picking
	ctxBookmark = "bookmark" // bookmark menu
	ctxTag      = "tag"      // tag menu
	ctxGit      = "git"      // git menu
	ctxRemote   = "remote"   // git > remote menu
	ctxRename   = "rename"   // bookmark/tag rename input
	ctxConflict = "conflict" // conflict resolution view
	ctxPicker   = "picker"   // file tree picker
	ctxFzf      = "fzf"      // inline fuzzy file finder
	ctxBlame    = "blame"    // file blame view
	ctxHist     = "history"  // file history view
)

// Action names — the config-key suffix after the dot.
const (
	actQuit       = "quit"
	actForceQuit  = "force_quit"
	actHelp       = "help"
	actYes        = "yes"
	actNo         = "no"
	actBack       = "back"
	actConfirm    = "confirm"
	actCancel     = "cancel"
	actAccept     = "accept"
	actComplete   = "complete" // tab autocomplete in text inputs
	actErase      = "erase"    // delete last char in text inputs
	actClear      = "clear"    // clear whole input (ctrl+u)
	actUp         = "up"
	actDown       = "down"
	actTop        = "top"
	actBottom     = "bottom"
	actPageUp     = "page_up"
	actPageDown   = "page_down"
	actOpen       = "open"
	actClose      = "close"
	actCollapse   = "collapse" // collapse a diff file / tree dir
	actExpand     = "expand"   // expand a diff file / tree dir
	actSearch     = "search"
	actDescribe   = "describe"
	actAIDescribe = "ai_describe"
	actEdit       = "edit"
	actNew        = "new"
	actAbandon    = "abandon"
	actAllRev     = "all_rev" // toggle all() revset
	actConflict   = "conflict"
	actBookmark   = "bookmark"
	actTag        = "tag"
	actFiles      = "files"
	actGit        = "git"
	actUndo       = "undo"
	actRedo       = "redo"
	actRebase     = "rebase"
	actSquash     = "squash"
	actAbsorb     = "absorb"
	actSplit      = "split"
	actToggle     = "toggle"  // split-mode mark toggle
	actScope      = "scope"   // rebase -r ⇄ -s
	actPlace      = "place"   // rebase onto/after/before
	actHistory    = "history" // blame → file history
	actFetch      = "fetch"
	actPush       = "push"
	actPushMark   = "push_bookmark"
	actRemote     = "remote"
	actPickLeft   = "left"  // conflict: take side 1
	actPickRight  = "right" // conflict: take side 2
	actPickBoth   = "both"  // conflict: take both sides
	actPickUnset  = "unset" // conflict: undo the pick
	actApply      = "apply" // conflict: jj resolve the file
	actPrevFile   = "prev_file"
	actNextFile   = "next_file"
	actCreate     = "create"
	actDelete     = "delete"
	actForget     = "forget"
	actList       = "list"
	actMove       = "move"
	actRename     = "rename"
	actSet        = "set"
	actTrack      = "track"
	actUntrack    = "untrack"
	actSetURL     = "set_url"
	actAdd        = "add"
	actRemove     = "remove"
)

// keyBind declares the default keys for one action.
type keyBind struct {
	action string
	keys   []string
}

// defaultKeymap is the full default binding table, ordered per context.
// Declaration order settles collisions deterministically: when two actions in
// a context end up bound to the same key (via user overrides), the action
// declared earlier here claims it. Within each binding, the FIRST key is the
// primary one shown in the UI hints.
var defaultKeymap = []struct {
	ctx   string
	binds []keyBind
}{
	{ctxGlobal, []keyBind{
		{actQuit, []string{"q"}},
		{actForceQuit, []string{"ctrl+c"}},
		{actHelp, []string{"?"}},
	}},
	{ctxBoot, []keyBind{
		{actYes, []string{"y", "Y"}},
		{actNo, []string{"n", "N"}},
		{actQuit, []string{"q"}},
		{actBack, []string{"esc"}}, // stage 2 steps back; stage 1 / error screen quits
	}},
	{ctxElev, []keyBind{
		{actConfirm, []string{"y", "Y", "enter"}},
	}},
	{ctxMenu, []keyBind{
		{actClose, []string{"esc", "q"}},
		{actAccept, []string{"enter"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home", "g"}},
		{actBottom, []string{"end", "G"}},
	}},
	{ctxInput, []keyBind{
		{actCancel, []string{"esc"}},
		{actAccept, []string{"enter"}},
		{actComplete, []string{"tab"}},
		{actErase, []string{"backspace", "delete"}},
		{actClear, []string{"ctrl+u"}},
	}},
	{ctxLog, []keyBind{
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home"}},
		{actBottom, []string{"G", "end"}},
		{actOpen, []string{"enter"}},
		{actSearch, []string{"/"}},
		{actDescribe, []string{"d"}},
		{actAIDescribe, []string{"D"}},
		{actEdit, []string{"e"}},
		{actNew, []string{"n"}},
		{actAbandon, []string{"a"}},
		{actAllRev, []string{"A"}},
		{actConflict, []string{"c"}},
		{actBookmark, []string{"b"}},
		{actTag, []string{"t"}},
		{actFiles, []string{"f"}},
		{actGit, []string{"g"}},
		{actUndo, []string{"u"}},
		{actRedo, []string{"U"}},
		{actRebase, []string{"r"}},
		{actSquash, []string{"s"}},
		{actAbsorb, []string{"x"}},
	}},
	{ctxDiff, []keyBind{
		{actClose, []string{"enter", "q", "esc"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"g", "home"}},
		{actBottom, []string{"G", "end"}},
		{actPageUp, []string{"pgup", "b", "ctrl+u"}},
		{actPageDown, []string{"pgdown", "f", "ctrl+d"}},
		{actCollapse, []string{"left", "h"}},
		{actExpand, []string{"right", "l"}},
		{actConflict, []string{"c"}},
		{actDescribe, []string{"d"}},
		{actAIDescribe, []string{"D"}},
		{actNew, []string{"n"}},
		{actSplit, []string{"s"}},
		{actAbsorb, []string{"x"}},
	}},
	{ctxSplit, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actConfirm, []string{"c"}},
		{actToggle, []string{" "}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"g", "home"}},
		{actBottom, []string{"G", "end"}},
		{actPageUp, []string{"pgup", "b", "ctrl+u"}},
		{actPageDown, []string{"pgdown", "f", "ctrl+d"}},
		{actCollapse, []string{"left", "h"}},
		{actExpand, []string{"right", "l"}},
	}},
	{ctxHelp, []keyBind{
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"g", "home"}},
		{actBottom, []string{"G", "end"}},
		{actPageUp, []string{"pgup", "b"}},
		{actPageDown, []string{"pgdown", "f"}},
	}},
	{ctxSearch, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actAccept, []string{"enter"}},
		{actUp, []string{"up"}},
		{actDown, []string{"down"}},
		{actTop, []string{"home"}},
		{actBottom, []string{"end"}},
		{actPageUp, []string{"pgup"}},
		{actPageDown, []string{"pgdown"}},
		{actErase, []string{"backspace", "delete"}},
		{actClear, []string{"ctrl+u"}},
	}},
	{ctxRebase, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home"}},
		{actBottom, []string{"G", "end"}},
		{actScope, []string{"s"}},
		{actPlace, []string{"tab"}},
		{actConfirm, []string{"enter"}},
	}},
	{ctxSquash, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home"}},
		{actBottom, []string{"G", "end"}},
		{actConfirm, []string{"enter"}},
	}},
	{ctxBookmark, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actCreate, []string{"c"}},
		{actDelete, []string{"d"}},
		{actForget, []string{"f"}},
		{actList, []string{"l"}},
		{actMove, []string{"m"}},
		{actRename, []string{"r"}},
		{actSet, []string{"s"}},
		{actTrack, []string{"t"}},
		{actUntrack, []string{"T"}},
	}},
	{ctxTag, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actSet, []string{"s"}},
		{actMove, []string{"m"}},
		{actDelete, []string{"d"}},
		{actList, []string{"l"}},
		{actPush, []string{"p"}},
	}},
	{ctxGit, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actFetch, []string{"f"}},
		{actPush, []string{"p"}},
		{actPushMark, []string{"P"}},
		{actRemote, []string{"r"}},
	}},
	{ctxRemote, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actAdd, []string{"a"}},
		{actList, []string{"l"}},
		{actRemove, []string{"r"}},
		{actRename, []string{"m"}},
		{actSetURL, []string{"s"}},
	}},
	{ctxRename, []keyBind{
		{actCancel, []string{"esc", "q"}},
		{actAccept, []string{"enter"}},
		{actErase, []string{"backspace", "delete"}},
	}},
	{ctxConflict, []keyBind{
		{actClose, []string{"esc", "q"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home", "g"}},
		{actBottom, []string{"end", "G"}},
		{actPageUp, []string{"pgup", "ctrl+u"}},
		{actPageDown, []string{"pgdown", "ctrl+d"}},
		{actPrevFile, []string{"["}},
		{actNextFile, []string{"]"}},
		{actPickLeft, []string{"l", "left", "h"}},
		{actPickRight, []string{"r", "right"}},
		{actPickBoth, []string{"b"}},
		{actPickUnset, []string{"u"}},
		{actApply, []string{"enter"}},
	}},
	{ctxPicker, []keyBind{
		{actQuit, []string{"esc", "q"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home", "g"}},
		{actBottom, []string{"end", "G"}},
		{actPageUp, []string{"pgup"}},
		{actPageDown, []string{"pgdown"}},
		{actExpand, []string{"l", "right"}},
		{actCollapse, []string{"h", "left"}},
		{actOpen, []string{"enter", " "}},
	}},
	{ctxFzf, []keyBind{
		{actCancel, []string{"esc"}},
		{actAccept, []string{"enter"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home", "g"}},
		{actBottom, []string{"end", "G"}},
		{actPageUp, []string{"pgup"}},
		{actPageDown, []string{"pgdown"}},
		{actErase, []string{"backspace"}},
		{actClear, []string{"ctrl+u"}},
	}},
	{ctxBlame, []keyBind{
		{actBack, []string{"esc", "q"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"g", "home"}},
		{actBottom, []string{"G", "end"}},
		{actPageUp, []string{"pgup"}},
		{actPageDown, []string{"pgdown"}},
		{actHistory, []string{"h"}},
		{actOpen, []string{"enter"}},
	}},
	{ctxHist, []keyBind{
		{actBack, []string{"esc", "q", "backspace"}},
		{actUp, []string{"up", "k"}},
		{actDown, []string{"down", "j"}},
		{actTop, []string{"home", "g"}},
		{actBottom, []string{"end", "G"}},
		{actOpen, []string{"enter"}},
	}},
}

// normalizeKeyName maps user-friendly spellings to the exact strings
// tea.KeyMsg.String() produces.
func normalizeKeyName(s string) string {
	switch strings.ToLower(s) {
	case "space":
		return " "
	case "escape":
		return "esc"
	case "return":
		return "enter"
	case "pgdn", "pagedown", "page_down":
		return "pgdown"
	case "pageup", "page_up":
		return "pgup"
	case "del":
		return "delete"
	}
	return s
}

// parseKeyList parses a config value ("j,down") into normalized key names.
func parseKeyList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = normalizeKeyName(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// KeyMap holds the resolved bindings for every context.
type KeyMap struct {
	lookup   map[string]string   // ctx + "\x00" + key → action
	bindings map[string][]string // ctx + "\x00" + action → keys
}

var (
	defaultKeysOnce sync.Once
	defaultKeys     KeyMap
)

// DefaultKeyMap returns the shared, immutable default key table.
func DefaultKeyMap() KeyMap {
	defaultKeysOnce.Do(func() { defaultKeys = newKeyMap(nil) })
	return defaultKeys
}

// newKeyMap builds a KeyMap from the defaults plus overrides keyed by
// "<context>.<action>" with comma-separated key names as values. An empty
// value unbinds the action.
func newKeyMap(overrides map[string]string) KeyMap {
	km := KeyMap{
		lookup:   map[string]string{},
		bindings: map[string][]string{},
	}
	for _, cd := range defaultKeymap {
		for _, b := range cd.binds {
			keys := b.keys
			if ov, ok := overrides[cd.ctx+"."+b.action]; ok {
				keys = parseKeyList(ov)
			}
			bkey := cd.ctx + "\x00" + b.action
			km.bindings[bkey] = keys
			for _, k := range keys {
				lkey := cd.ctx + "\x00" + k
				// First claim within the context wins (declaration order).
				if _, taken := km.lookup[lkey]; !taken {
					km.lookup[lkey] = b.action
				}
			}
		}
	}
	return km
}

// resolve returns the action bound to key in ctx, or "".
func (km KeyMap) resolve(ctx, key string) string {
	if km.lookup == nil {
		return DefaultKeyMap().resolve(ctx, key)
	}
	return km.lookup[ctx+"\x00"+key]
}

// keys returns the (normalized) key list bound to ctx.action.
func (km KeyMap) keys(ctx, action string) []string {
	if km.lookup == nil {
		return DefaultKeyMap().keys(ctx, action)
	}
	return km.bindings[ctx+"\x00"+action]
}

// primary returns the first key bound to ctx.action, or "" when unbound.
func (km KeyMap) primary(ctx, action string) string {
	if ks := km.keys(ctx, action); len(ks) > 0 {
		return ks[0]
	}
	return ""
}

// prettyKey renders a key name for display in hints.
func prettyKey(k string) string {
	switch k {
	case " ":
		return "space"
	case "enter":
		return "⏎"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "home":
		return "Home"
	case "end":
		return "End"
	case "pgdown":
		return "pgdn"
	case "backspace":
		return "⌫"
	case "delete":
		return "del"
	}
	return k
}

// displayKey renders a raw key name for word-style hints (context menus,
// help rows): the space key becomes "space", everything else stays literal.
func displayKey(k string) string {
	if k == " " {
		return "space"
	}
	return k
}

// ── Model-facing helpers ────────────────────────────────────────────────────

// hk is the pretty display form of an action's primary key ("⏎", "esc", ↑…).
func (m Model) hk(ctx, action string) string {
	return prettyKey(m.keys.primary(ctx, action))
}

// hkRaw is the literal display form of an action's primary key
// ("enter", "esc", "space"…).
func (m Model) hkRaw(ctx, action string) string {
	return displayKey(m.keys.primary(ctx, action))
}

// hkLast is the pretty display form of an action's LAST bound key — used
// where the UI historically shows the tail of a multi-key binding (e.g. the
// picker's quit hint shows "q" for "esc,q").
func (m Model) hkLast(ctx, action string) string {
	ks := m.keys.keys(ctx, action)
	if len(ks) == 0 {
		return ""
	}
	return prettyKey(ks[len(ks)-1])
}

// hkN joins the pretty forms of an action's first n keys with sep.
func (m Model) hkN(ctx, action string, n int, sep string) string {
	return joinKeys(prettyKey, m.keys.keys(ctx, action), n, sep)
}

func joinKeys(show func(string) string, keys []string, n int, sep string) string {
	if n > 0 && len(keys) > n {
		keys = keys[:n]
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = show(k)
	}
	return strings.Join(parts, sep)
}

// modeHints renders the shared destination-picking hint line used by the
// rebase/squash status bars: "<down>/<up> move[ · extra…] · ⏎ confirm ·
// esc cancel" with primary keys substituted (e.g. "j/k move · s scope · tab
// place · ⏎ confirm · esc cancel").
func (m Model) modeHints(ctx string, extra ...string) string {
	s := m.hk(ctx, actDown) + "/" + m.hk(ctx, actUp) + " move"
	for _, e := range extra {
		s += " · " + m.hk(ctx, e) + " " + e
	}
	return s + " · " + m.hk(ctx, actConfirm) + " confirm · " + m.hk(ctx, actCancel) + " cancel"
}

// keyMsg returns a KeyMsg and its string form for an action's primary key
// (both zero when unbound), for synthetic dispatch (context-menu activation).
func (m Model) keyMsg(ctx, action string) (tea.KeyMsg, string) {
	k := m.keys.primary(ctx, action)
	if k == "" {
		return tea.KeyMsg{}, ""
	}
	return keyMsgFromName(k), k
}

// keyMsgFromName constructs a KeyMsg whose String() equals the given key
// name. Named keys map to their KeyType; everything else (including "ctrl+x"
// combos, which round-trip through Runes) is produced as runes.
func keyMsgFromName(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}
