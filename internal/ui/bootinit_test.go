package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"gojo/internal/jj"
)

// fakeJJ writes an executable shell script that records its argv (one arg per
// line) into argsFile, then exits 0. Used to observe the exact command gojo
// runs during the boot init flow.
func fakeJJ(t *testing.T) (script, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script = filepath.Join(dir, "jj")
	body := "#!/bin/sh\nprintf '%s\n' \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, argsFile
}

// failJJ writes an executable shell script that prints to stderr and fails.
func failJJ(t *testing.T) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "jj")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// bootPromptModel returns a model that failed to boot with jj.ErrNoRepo.
func bootPromptModel(t *testing.T, jjPath string) Model {
	t.Helper()
	m := NewModel()
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = step(t, m, bootMsg{err: jj.ErrNoRepo, cfg: jj.Config{JJPath: jjPath}})
	return m
}

// TestBootInitPromptAppears verifies that booting outside a jj repo shows the
// init question instead of the bare error screen.
func TestBootInitPromptAppears(t *testing.T) {
	m := bootPromptModel(t, "/bin/true")
	if m.bootInitStage != 1 {
		t.Fatalf("bootInitStage = %d, want 1", m.bootInitStage)
	}
	if m.bootErr != "" {
		t.Fatalf("bootErr = %q, want empty while prompting", m.bootErr)
	}
	if m.ready {
		t.Fatal("model must not be ready while the prompt is open")
	}
	plain := stripView(m)
	if !strings.Contains(plain, "not inside a jj repository") {
		t.Fatalf("view missing repo hint: %s", plain)
	}
	if !strings.Contains(plain, "initialize a new repo here?") {
		t.Fatalf("view missing init question: %s", plain)
	}
}

// TestBootInitDecline verifies that answering n (or q/esc) to the init
// question falls back to the classic boot-error screen with quit shortcuts.
func TestBootInitDecline(t *testing.T) {
	m := bootPromptModel(t, "/bin/true")
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.bootInitStage != 0 {
		t.Fatalf("bootInitStage = %d, want 0 after decline", m.bootInitStage)
	}
	if m.bootErr == "" {
		t.Fatal("bootErr should be set after declining init")
	}
	plain := stripView(m)
	if !strings.Contains(plain, "no .jj directory found") || !strings.Contains(plain, "ctrl+c") {
		t.Fatalf("fallback error view missing text: %s", plain)
	}
	// q still quits from the fallback screen.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Fatal("q did not quit from the fallback error screen")
	}
}

// TestBootInitFlowEndToEnd drives both questions: y to init, y to colocate,
// and verifies via a fake jj script that `jj git init --colocate` runs; the
// reported success then re-runs boot.
func TestBootInitFlowEndToEnd(t *testing.T) {
	jjPath, argsFile := fakeJJ(t)
	m := bootPromptModel(t, jjPath)

	// Question 1: initialize? → y advances to the colocate question.
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.bootInitStage != 2 {
		t.Fatalf("bootInitStage = %d, want 2", m.bootInitStage)
	}
	if !strings.Contains(stripView(m), "--colocate") {
		t.Fatalf("stage-2 view missing colocate question: %s", stripView(m))
	}

	// Question 2: colocate? → y starts the init (stage 3) and returns the
	// init command. Run it synchronously and feed the result back in.
	m1, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = m1.(Model)
	if m.bootInitStage != 3 {
		t.Fatalf("bootInitStage = %d, want 3 (init running)", m.bootInitStage)
	}
	if cmd == nil {
		t.Fatal("no init command returned")
	}
	if !strings.Contains(stripView(m), "initializing repo") {
		t.Fatalf("stage-3 view missing progress line: %s", stripView(m))
	}
	done, ok := cmd().(initDoneMsg)
	if !ok {
		t.Fatalf("init command returned %T, want initDoneMsg", cmd())
	}
	if done.err != nil {
		t.Fatalf("init failed against fake jj: %v", done.err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(args))
	want := []string{"git", "init", "--colocate"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("jj argv = %v, want %v", got, want)
	}

	// Success re-runs boot so the new repo is picked up.
	m1, cmd = m.Update(done)
	m = m1.(Model)
	if cmd == nil {
		t.Fatal("successful init should re-run boot")
	}
	if m.bootInitStage != 0 || m.bootInitErr != "" {
		t.Fatalf("after init: stage=%d err=%q, want 0/empty", m.bootInitStage, m.bootInitErr)
	}
}

// TestBootInitNoColocate verifies answering n at stage 2 runs plain
// `jj git init` (no --colocate).
func TestBootInitNoColocate(t *testing.T) {
	jjPath, argsFile := fakeJJ(t)
	m := bootPromptModel(t, jjPath)
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m1, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m1.(Model)
	if m.bootInitStage != 3 || cmd == nil {
		t.Fatalf("stage=%d cmd=%v, want 3 + init command", m.bootInitStage, cmd)
	}
	done := cmd().(initDoneMsg)
	if done.err != nil {
		t.Fatalf("init failed against fake jj: %v", done.err)
	}
	args, _ := os.ReadFile(argsFile)
	got := strings.Fields(string(args))
	if strings.Join(got, " ") != "git init --no-colocate" {
		t.Fatalf("jj argv = %v, want [git init --no-colocate]", got)
	}
}

// TestBootInitFailure verifies a failed init returns to stage 1 with the
// error rendered, so the user can retry or bail.
func TestBootInitFailure(t *testing.T) {
	m := bootPromptModel(t, failJJ(t))
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m1, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m1.(Model)
	done := cmd().(initDoneMsg)
	if done.err == nil {
		t.Fatal("init should have failed against failing jj")
	}
	m = step(t, m, done)
	if m.bootInitStage != 1 {
		t.Fatalf("bootInitStage = %d, want 1 after failure", m.bootInitStage)
	}
	plain := stripView(m)
	if !strings.Contains(plain, "boom") {
		t.Fatalf("view missing init error: %s", plain)
	}
	if !strings.Contains(plain, "initialize a new repo here?") {
		t.Fatalf("view should re-ask the init question after failure: %s", plain)
	}
}

// TestBootInitEscFromColocate verifies esc at the colocate question steps
// back to the init question instead of cancelling outright.
func TestBootInitEscFromColocate(t *testing.T) {
	m := bootPromptModel(t, "/bin/true")
	m = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = step(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.bootInitStage != 1 {
		t.Fatalf("bootInitStage = %d, want 1 after esc", m.bootInitStage)
	}
}
