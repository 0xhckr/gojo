package ui

import (
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gojo/internal/jj"
)

// Exercise the actual search → select → preview → abandon path against two
// live commits with one change ID. No fake jj output or command assertions.
func TestDivergentCleanup(t *testing.T) {
	jjPath, err := exec.LookPath("jj")
	if err != nil {
		t.Skip("jj is not installed")
	}
	repo := t.TempDir()
	t.Setenv("JJ_CONFIG", t.TempDir())
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(jjPath, args...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("git", "init", "--no-colocate")
	run("describe", "-m", "original")
	baseOp := run("op", "log", "--no-graph", "-n", "1", "-T", "id")
	run("describe", "-m", "keep this version")
	run("--at-op", baseOp, "describe", "-m", "discard this version")
	run("log", "--no-graph") // resolve the concurrent operations
	run("new", "root()", "-m", "working copy")

	m := NewModel()
	m.ready, m.width, m.height = true, 100, 30
	m.cfg = jj.Config{JJPath: jjPath, RepoRoot: repo}
	m.runner = jj.NewRunner(m.cfg)
	m.cwd = repo
	m.showAllRev = true
	entries, err := m.runner.LogRevset("all()", 0)
	if err != nil {
		t.Fatal(err)
	}
	m = step(t, m, refreshMsg{entries: entries})

	var discard, keep jj.LogEntry
	for _, e := range entries {
		switch e.Subject {
		case "discard this version":
			discard = e
		case "keep this version":
			keep = e
		}
	}
	if !discard.IsDivergent || !keep.IsDivergent || discard.ChangeID != keep.ChangeID || discard.CommitID == keep.CommitID {
		t.Fatalf("expected distinct divergent versions: discard=%+v keep=%+v", discard, keep)
	}
	// Divergence is repository metadata, even when the sibling is filtered out.
	one, err := m.runner.LogRevset(discard.CommitID, 1)
	if err != nil || len(one) != 1 || !one[0].IsDivergent {
		t.Fatalf("single-version log lost divergence: %v, %v", one, err)
	}
	for _, width := range []int{60, 100} {
		m.width = width
		view := stripView(m)
		for _, e := range []jj.LogEntry{discard, keep} {
			if !strings.Contains(view, "divergent "+e.CommitID) {
				t.Fatalf("width %d: missing divergent version %s:\n%s", width, e.CommitID, view)
			}
		}
	}
	m.width = 100
	m = step(t, m, keyPress("/"))
	for _, ch := range "divergent" {
		m = step(t, m, keyPress(string(ch)))
	}
	if len(m.searchResults) != 2 {
		t.Fatalf("divergent search matched %d entries", len(m.searchResults))
	}
	if view := stripView(m); !strings.Contains(view, discard.CommitID) || !strings.Contains(view, keep.CommitID) {
		t.Fatalf("search must distinguish versions:\n%s", view)
	}
	if m.entries[m.searchResults[0].entryIdx].CommitID != discard.CommitID {
		m = step(t, m, keyCode(tea.KeyDown))
	}
	// A poll can reorder the log while the search is open.
	reordered := append([]jj.LogEntry(nil), entries...)
	for i, j := 0, len(reordered)-1; i < j; i, j = i+1, j-1 {
		reordered[i], reordered[j] = reordered[j], reordered[i]
	}
	m = step(t, m, refreshMsg{entries: reordered})
	m = step(t, m, keyCode(tea.KeyEnter))
	if m.selectedEntry().CommitID != discard.CommitID {
		t.Fatal("search jumped to another version after refresh")
	}
	m = step(t, m, refreshMsg{entries: entries})
	if m.selectedEntry().CommitID != discard.CommitID {
		t.Fatal("log selection moved to another version after refresh")
	}
	m = step(t, m, keyCode(tea.KeyEnter))
	if m.errMsg != "" || m.diffDesc != discard.Subject || m.diffRev != discard.CommitID {
		t.Fatalf("wrong preview: rev=%s desc=%s err=%s", m.diffRev, m.diffDesc, m.errMsg)
	}
	// The focus/poll command must also use this exact version.
	cmds := m.refreshFocusedCmds()
	loaded := cmds[1]().(diffLoadedMsg)
	if loaded.err != nil || loaded.rev != discard.CommitID {
		t.Fatalf("divergent preview refresh: %+v", loaded)
	}
	m = step(t, m, keyPress("q"))
	// A stale log may not yet know this change is divergent.
	m.entries[m.cursor].IsDivergent = false
	next, cmd := m.Update(keyPress("a"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("abandon did not run")
	}
	done := false
	var checkResult func(tea.Msg)
	checkResult = func(result tea.Msg) {
		if commands, ok := result.(tea.BatchMsg); ok {
			for _, command := range commands {
				checkResult(command())
			}
		}
		if msg, ok := result.(actionDoneMsg); ok {
			if msg.err != nil {
				t.Fatalf("abandon failed: %v", msg.err)
			}
			done = true
		}
	}
	checkResult(cmd())
	if !done {
		t.Fatal("no abandon result")
	}
	remaining, err := m.runner.LogRevset("all()", 0)
	if err != nil {
		t.Fatal(err)
	}
	foundKeep := false
	for _, e := range remaining {
		if e.CommitID == discard.CommitID {
			t.Fatal("selected version remains visible after abandon")
		}
		if e.CommitID == keep.CommitID {
			foundKeep = true
			if e.IsDivergent {
				t.Fatal("remaining version still marked divergent")
			}
		}
	}
	if !foundKeep {
		t.Fatal("abandon removed the other version")
	}

	// The existing working-copy protection still applies.
	m.entries = remaining
	for i, e := range remaining {
		if e.IsWorkingCopy {
			m.cursor = i
		}
	}
	next, cmd = m.Update(keyPress("a"))
	if cmd != nil || next.(Model).errMsg != "cannot abandon the working copy" {
		t.Fatal("working-copy abandon was not blocked")
	}
}
