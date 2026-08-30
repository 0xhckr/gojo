package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gojo/internal/jj"
)

func workspaceModel() Model {
	m := NewModel()
	m.width, m.height = 100, 30
	m.ready = true
	m.cfg = jj.Config{JJPath: "jj", RepoRoot: "/repo/main"}
	m.runner = jj.NewRunner(m.cfg)
	m.cwd = m.cfg.RepoRoot
	m.entries = []jj.LogEntry{{ChangeID: "change", CommitID: "commit"}}
	m.workspaceOpen = true
	m.workspaces = []jj.Workspace{
		{Name: "main", Root: "/repo/main", ChangeID: "abcdefghijk"},
		{Name: "feature", Root: "/repo/feature", ChangeID: "lmnopqrstuv"},
		{Name: "missing", ChangeID: "wxyz"},
	}
	return m
}

func TestWorkspacePickerRenderAndNavigation(t *testing.T) {
	m := workspaceModel()
	view := stripView(m)
	for _, want := range []string{"gojo workspaces (3)", "main", "/repo/feature", "(missing)", "a add", "f forget"} {
		if !strings.Contains(view, want) {
			t.Fatalf("workspace picker missing %q:\n%s", want, view)
		}
	}

	m = step(t, m, keyCode(tea.KeyDown))
	if m.workspaceCursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.workspaceCursor)
	}
	next, cmd := m.Update(keyCode(tea.KeyEnter))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("switch did not request a refresh")
	}
	if m.workspaceOpen || m.cfg.RepoRoot != "/repo/feature" || m.cwd != "/repo/feature" {
		t.Fatalf("switch state = open:%v root:%q cwd:%q", m.workspaceOpen, m.cfg.RepoRoot, m.cwd)
	}
}

func TestWorkspaceForgetProtectsActiveAndConfirms(t *testing.T) {
	m := workspaceModel()
	m = step(t, m, keyPress("f"))
	if m.workspaceAction != "" || !strings.Contains(m.errMsg, "active workspace") {
		t.Fatalf("active forget was not blocked: action=%q err=%q", m.workspaceAction, m.errMsg)
	}

	m.errMsg = ""
	m.workspaceCursor = 1
	m = step(t, m, keyPress("f"))
	if m.workspaceAction != actForget {
		t.Fatalf("forget action = %q", m.workspaceAction)
	}
	if !strings.Contains(stripView(m), "files stay on disk") {
		t.Fatal("forget confirmation does not explain disk behavior")
	}
	m = step(t, m, keyCode(tea.KeyEscape))
	if m.workspaceAction != "" || !m.workspaceOpen {
		t.Fatalf("cancel should return to picker: action=%q open=%v", m.workspaceAction, m.workspaceOpen)
	}
}

func TestWorkspaceAddAndRenameInputs(t *testing.T) {
	m := workspaceModel()
	m = step(t, m, keyPress("a"))
	m = step(t, m, tea.PasteMsg{Content: "~/trees/feature\n"})
	if m.workspaceInput != "~/trees/feature " {
		t.Fatalf("add paste = %q", m.workspaceInput)
	}
	m = step(t, m, keyCode(tea.KeyEscape))

	m.workspaceCursor = 1
	m = step(t, m, keyPress("r"))
	if m.workspaceAction != actRename || m.workspaceInput != "feature" {
		t.Fatalf("rename prompt = action:%q input:%q", m.workspaceAction, m.workspaceInput)
	}
	m = step(t, m, keyCode(tea.KeyBackspace))
	if m.workspaceInput != "featur" {
		t.Fatalf("rename backspace = %q", m.workspaceInput)
	}
}
