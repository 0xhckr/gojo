package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"gojo/internal/jj"
)

func searchTestModel() Model {
	return Model{
		ready:  true,
		width:  100,
		height: 30,
		view:   viewLog,
		entries: []jj.LogEntry{
			{ChangeID: "kxmytsvx", CommitID: "deadbeef", Authors: "hackr@hackr.sh", Subject: "Rewrite gojo", Bookmarks: []string{"main"}, Tags: []string{"v1.0"}},
			{ChangeID: "abc12345", CommitID: "cafebabe", Authors: "al@ice.gg", Subject: "add search feature"},
			{ChangeID: "xyz99999", CommitID: "11112222", Authors: "bo@b.io", Subject: "fix bugs", Bookmarks: []string{"dev", "wip"}},
		},
	}
}

// TestSearchEnter verifies that / activates search mode and shows all entries
// (empty query matches everything in original order).
func TestSearchEnter(t *testing.T) {
	m := searchTestModel()

	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.searchMode {
		t.Fatal("/ did not enter search mode")
	}
	if m.searchQuery != "" {
		t.Errorf("searchQuery = %q, want empty", m.searchQuery)
	}
	if len(m.searchResults) != 3 {
		t.Fatalf("searchResults = %d, want 3 (empty query matches all)", len(m.searchResults))
	}

	view := stripView(m)
	if !strings.Contains(view, "search revisions") {
		t.Error("view missing search title")
	}
	if !strings.Contains(view, "3 matches") {
		t.Error("view missing match count")
	}
}

// TestSearchFilter verifies that typing filters results by matching against
// change ID, commit ID, description, author, bookmark, and tag fields.
func TestSearchFilter(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	// Type "hackr" — should match the author of entry 0.
	for _, r := range "hackr" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("after 'hackr': searchResults = %d, want 1", len(m.searchResults))
	}
	if m.searchResults[0].entryIdx != 0 {
		t.Errorf("matched entry %d, want 0", m.searchResults[0].entryIdx)
	}

	// Clear and try matching by bookmark "dev" — entry 2.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.searchQuery != "" {
		t.Fatalf("ctrl+u did not clear query, got %q", m.searchQuery)
	}
	for _, r := range "dev" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("after 'dev': searchResults = %d, want 1", len(m.searchResults))
	}
	if m.searchResults[0].entryIdx != 2 {
		t.Errorf("matched entry %d, want 2", m.searchResults[0].entryIdx)
	}
}

// TestSearchFilterByTag verifies matching against git tags.
func TestSearchFilterByTag(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	for _, r := range "v1" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("after 'v1': searchResults = %d, want 1", len(m.searchResults))
	}
	if m.searchResults[0].entryIdx != 0 {
		t.Errorf("matched entry %d, want 0 (has tag v1.0)", m.searchResults[0].entryIdx)
	}
}

// TestSearchFilterByCommitID verifies matching against git commit IDs.
func TestSearchFilterByCommitID(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	for _, r := range "cafe" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("after 'cafe': searchResults = %d, want 1", len(m.searchResults))
	}
	if m.searchResults[0].entryIdx != 1 {
		t.Errorf("matched entry %d, want 1 (commitID cafebabe)", m.searchResults[0].entryIdx)
	}
}

// TestSearchNavigation verifies j/k moves the search cursor through results.
func TestSearchNavigation(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if m.searchCursor != 0 {
		t.Fatalf("initial searchCursor = %d, want 0", m.searchCursor)
	}

	// Move down.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.searchCursor != 1 {
		t.Errorf("after down: searchCursor = %d, want 1", m.searchCursor)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.searchCursor != 2 {
		t.Errorf("after down: searchCursor = %d, want 2", m.searchCursor)
	}

	// Move up.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.searchCursor != 1 {
		t.Errorf("after up: searchCursor = %d, want 1", m.searchCursor)
	}

	// Home / End.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyHome})
	if m.searchCursor != 0 {
		t.Errorf("after home: searchCursor = %d, want 0", m.searchCursor)
	}
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.searchCursor != 2 {
		t.Errorf("after end: searchCursor = %d, want 2", m.searchCursor)
	}
}

// TestSearchEnterJumpsCursor verifies that enter moves the log cursor to the
// selected result and exits search mode.
func TestSearchEnterJumpsCursor(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	// Move to result index 1 (entry 1).
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.searchMode {
		t.Fatal("enter did not exit search mode")
	}
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (jumped to selected entry)", m.cursor)
	}
}

// TestSearchEscCancels verifies that esc exits search mode without moving the
// cursor.
func TestSearchEscCancels(t *testing.T) {
	m := searchTestModel()
	m.cursor = 0
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	// Move down in search results.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})

	if m.searchMode {
		t.Fatal("esc did not exit search mode")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (esc should not move cursor)", m.cursor)
	}
}

// TestSearchJKAreText verifies that j and k are typed into the search query
// instead of navigating (so you can search for "jk" etc.).
func TestSearchJKAreText(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	startCursor := m.searchCursor

	// Typing 'j' should add to the query, not move the cursor.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.searchQuery != "j" {
		t.Errorf("after typing 'j': searchQuery = %q, want 'j'", m.searchQuery)
	}
	if m.searchCursor != startCursor {
		t.Errorf("typing 'j' moved cursor to %d, want %d", m.searchCursor, startCursor)
	}

	// Typing 'k' should also add to the query.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.searchQuery != "jk" {
		t.Errorf("after typing 'k': searchQuery = %q, want 'jk'", m.searchQuery)
	}
}

// TestSearchBackspace verifies that backspace removes the last query character
// and re-filters.
func TestSearchBackspace(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	// Type "add" — matches entry 1 (subject "add search feature").
	for _, r := range "add" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 1 {
		t.Fatalf("after 'add': searchResults = %d, want 1", len(m.searchResults))
	}

	// Backspace removes 'd' → "ad" — may match more entries.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.searchQuery != "ad" {
		t.Errorf("after backspace: searchQuery = %q, want 'ad'", m.searchQuery)
	}
}

// TestSearchNoMatches verifies the UI shows a no-matches message.
func TestSearchNoMatches(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	for _, r := range "zzzzz" {
		m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.searchResults) != 0 {
		t.Fatalf("searchResults = %d, want 0", len(m.searchResults))
	}
	view := stripView(m)
	if !strings.Contains(view, "no matches") {
		t.Error("view missing 'no matches' message")
	}
}

// TestSearchStatusHelpBars verifies the status bar and help bar render
// search-specific content while search is active.
func TestSearchStatusHelpBars(t *testing.T) {
	m := searchTestModel()
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	view := stripView(m)
	if !strings.Contains(view, "search") {
		t.Error("view missing search status bar hint")
	}
	helpItems := m.helpBarItems()
	if helpItems == nil {
		t.Fatal("helpBarItems returned nil during search mode")
	}
	foundJump := false
	for _, it := range helpItems {
		if strings.Contains(it[0], "jump") {
			foundJump = true
		}
	}
	if !foundJump {
		t.Error("help bar missing 'jump' hint during search")
	}
}

// TestSearchInHelpView verifies the help view includes a search section.
func TestSearchInHelpView(t *testing.T) {
	found := false
	for _, s := range helpSections {
		if s.title == "Search Mode" {
			found = true
			for _, b := range s.bindings {
				if strings.Contains(b.desc, "fuzzy") {
					return
				}
			}
		}
	}
	if !found {
		t.Error("helpSections missing Search Mode section")
	}
}
