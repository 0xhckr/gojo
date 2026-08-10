package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeymapDefaultsResolve(t *testing.T) {
	km := DefaultKeyMap()
	cases := []struct{ ctx, key, want string }{
		{ctxLog, "j", actDown},
		{ctxLog, "down", actDown},
		{ctxLog, "k", actUp},
		{ctxLog, "d", actDescribe},
		{ctxLog, "/", actSearch},
		{ctxDiff, "x", actAbsorb},
		{ctxDiff, "pgup", actPageUp},
		{ctxDiff, "ctrl+d", actPageDown},
		{ctxConflict, "b", actPickBoth},
		{ctxConflict, "[", actPrevFile},
		{ctxBookmark, "T", actUntrack},
		{ctxGit, "P", actPushMark},
		{ctxSplit, " ", actToggle},
		{ctxGlobal, "ctrl+c", actForceQuit},
		{ctxGlobal, "?", actHelp},
		{ctxInput, "tab", actComplete},
		{ctxSearch, "pgup", actPageUp},
	}
	for _, c := range cases {
		if got := km.resolve(c.ctx, c.key); got != c.want {
			t.Errorf("resolve(%s, %q) = %q, want %q", c.ctx, c.key, got, c.want)
		}
	}
	// Unbound keys resolve to "".
	if got := km.resolve(ctxLog, "F9"); got != "" {
		t.Errorf("resolve(log, F9) = %q, want empty", got)
	}
	// Unknown contexts never resolve.
	if got := km.resolve("bogus", "j"); got != "" {
		t.Errorf("resolve(bogus, j) = %q, want empty", got)
	}
}

func TestKeymapOverrides(t *testing.T) {
	km := newKeyMap(map[string]string{
		"log.down":     "n",   // replace the binding entirely
		"diff.split":   "S",   // rebind to a shifted key
		"diff.absorb":  "",    // unbind
		"log.describe": "o,ó", // multiple alternates
		"madeup.act":   "x",   // unknown context: ignored
		"log.madeup":   "y",   // unknown action: ignored
	})

	if got := km.resolve(ctxLog, "n"); got != actDown {
		t.Errorf("rebound n → %q, want %q", got, actDown)
	}
	// The old default is fully replaced.
	if km.resolve(ctxLog, "j") != "" || km.resolve(ctxLog, "down") != "" {
		t.Error("replacing log.down should drop the default j/down bindings")
	}
	if got := km.resolve(ctxDiff, "S"); got != actSplit {
		t.Errorf("rebound S → %q", got)
	}
	if got := km.resolve(ctxDiff, "s"); got != "" {
		t.Errorf("diff.split rebind should free the old key, got %q", got)
	}
	if km.resolve(ctxDiff, "x") != "" || len(km.keys(ctxDiff, actAbsorb)) != 0 {
		t.Error("empty override should unbind diff.absorb")
	}
	if km.resolve(ctxLog, "o") != actDescribe || km.resolve(ctxLog, "ó") != actDescribe {
		t.Error("multi-key override alternates should all resolve")
	}
	// Unknown entries must not explode into stray bindings.
	if km.resolve(ctxLog, "y") != "" {
		t.Error("unknown action log.madeup should be ignored")
	}
}

func TestKeymapCollisionFirstDeclaredWins(t *testing.T) {
	km := newKeyMap(map[string]string{
		"log.up":   "x",
		"log.down": "x",
	})
	if got := km.resolve(ctxLog, "x"); got != actUp {
		t.Errorf("collided key resolved to %q, want the action declared first (%q)", got, actUp)
	}
}

func TestNormalizeKeyName(t *testing.T) {
	cases := map[string]string{
		"space":    " ",
		"escape":   "esc",
		"return":   "enter",
		"pgdn":     "pgdown", // old bubbletea spelling must still match
		"pagedown": "pgdown",
		"pageup":   "pgup",
		"del":      "delete",
		"j":        "j", // runes untouched
		"J":        "J",
		"ctrl+u":   "ctrl+u",
	}
	for in, want := range cases {
		if got := normalizeKeyName(in); got != want {
			t.Errorf("normalizeKeyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrettyKey(t *testing.T) {
	cases := map[string]string{
		" ": "space", "enter": "⏎", "up": "↑", "down": "↓",
		"left": "←", "right": "→", "home": "Home", "end": "End",
		"pgdown": "pgdn", "backspace": "⌫", "delete": "del",
		"esc": "esc", "d": "d", "ctrl+c": "ctrl+c",
	}
	for in, want := range cases {
		if got := prettyKey(in); got != want {
			t.Errorf("prettyKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeyMsgFromNameRoundTrip(t *testing.T) {
	for _, k := range []string{"enter", "esc", " ", "tab", "backspace", "delete",
		"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
		"d", "D", "?", "/", "[", "]", "ctrl+u", "ctrl+c"} {
		if got := keyMsgFromName(k).String(); got != k {
			t.Errorf("keyMsgFromName(%q).String() = %q", k, got)
		}
	}
}

// TestReboundLogKeysDrivesModel verifies a user override actually changes the
// key the model responds to end to end.
func TestReboundLogKeysDrivesModel(t *testing.T) {
	m := mouseTestModel()
	m.keys = newKeyMap(map[string]string{"log.down": "n"})

	// "j" no longer moves the cursor.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = nm.(Model)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after unbound j, want 0", m.cursor)
	}
	// "n" now moves down.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = nm.(Model)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after rebound n, want 1", m.cursor)
	}

	// Hint helpers reflect the rebound binding.
	if got := m.hk(ctxLog, actDown); got != "n" {
		t.Fatalf("hk(log.down) = %q, want n", got)
	}
	if got := m.hk(ctxLog, actUp); got != "↑" {
		t.Fatalf("hk(log.up) = %q, want ↑", got)
	}
}

// TestZeroModelUsesDefaults guards the lazy-default fallback: Models built as
// bare struct literals (unit tests) have a zero KeyMap and must still work.
func TestZeroModelUsesDefaults(t *testing.T) {
	var m Model
	if got := m.keys.resolve(ctxLog, "j"); got != actDown {
		t.Fatalf("zero KeyMap resolve = %q, want %q", got, actDown)
	}
	if got := m.hk(ctxLog, actDown); got != "↓" {
		t.Fatalf("zero KeyMap hk = %q, want ↓", got)
	}
}
