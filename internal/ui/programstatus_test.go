package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"gojo/internal/jj"
)

func statusStep(t *testing.T, m Model, msg tea.Msg, state string) Model {
	t.Helper()
	next, _ := m.Update(msg)
	m = next.(Model)
	if m.programReport.State != state {
		t.Fatalf("after %T: got %+v, want %s", msg, m.programReport, state)
	}
	// Acknowledge the serialized raw report without running subprocesses or ticks.
	next, _ = m.Update(programStatusWrittenMsg{})
	return next.(Model)
}

func TestAIProgramStatus(t *testing.T) {
	m := NewModel()
	m.ready = true
	m.entries = []jj.LogEntry{{ChangeID: "abcd1234"}}
	m = statusStep(t, m, keyPress("D"), "working")
	if !m.aiLoading["abcd1234"] {
		t.Fatal("AI key did not start generation")
	}
	m = statusStep(t, m, aiGeneratedMsg{changeID: "abcd1234", message: "A message"}, "working")
	m = statusStep(t, m, aiAppliedMsg{changeID: "abcd1234", message: "A message"}, "done")
	m = statusStep(t, m, refreshMsg{}, "done")
	m = statusStep(t, m, tea.BlurMsg{}, "done")
	m = statusStep(t, m, spinnerTickMsg{}, "done")
	statusStep(t, m, keyPress("j"), "idle")
}

func TestConcurrentAIProgramStatus(t *testing.T) {
	m := NewModel()
	m.ready = true
	m.aiLoading["one"] = true
	m.aiLoading["two"] = true
	m = statusStep(t, m, tea.BlurMsg{}, "working")
	m = statusStep(t, m, aiGeneratedMsg{changeID: "one", err: errors.New("provider failed")}, "working")
	m = statusStep(t, m, refreshMsg{}, "working")
	m = statusStep(t, m, aiAppliedMsg{changeID: "two", message: "Ready"}, "error")
	if m.programReport.Message != "provider failed" {
		t.Fatalf("lost concurrent failure: %+v", m.programReport)
	}
	m = statusStep(t, m, refreshMsg{}, "error")
	statusStep(t, m, keyPress("j"), "idle")
}

func TestBackgroundProgramStatus(t *testing.T) {
	for _, failure := range []error{nil, errors.New("fetch failed")} {
		m := NewModel()
		m.ready = true
		m, _ = m.startBusy("fetching…")
		m = statusStep(t, m, tea.BlurMsg{}, "working")
		want := "done"
		if failure != nil {
			want = "error"
		}
		m = statusStep(t, m, actionDoneMsg{err: failure}, want)
		statusStep(t, m, refreshMsg{}, want)
	}
}

func TestBlockedProgramStatus(t *testing.T) {
	m := NewModel()
	m.ready = true
	m, _ = m.startBusy("pushing…")
	m = statusStep(t, m, tea.BlurMsg{}, "working")
	m = statusStep(t, m, actionDoneMsg{
		err:  errors.New("needs approval"),
		elev: &elevReq{reason: "Allow new bookmarks?"},
	}, "blocked")
	if m.programReport.Kind != "permission" {
		t.Fatal("missing permission kind")
	}
	statusStep(t, m, keyPress("n"), "idle")

	m = NewModel()
	m = statusStep(t, m, bootMsg{err: jj.ErrNoRepo}, "blocked")
	if m.programReport.Kind != "question" {
		t.Fatal("missing question kind")
	}
	m = statusStep(t, m, keyPress("y"), "blocked")
	m = statusStep(t, m, keyPress("n"), "working")
	statusStep(t, m, initDoneMsg{err: errors.New("cannot initialize")}, "blocked")
}

func TestProgramStatusSerialization(t *testing.T) {
	m := NewModel()
	m.ready = true
	m.entries = []jj.LogEntry{{ChangeID: "abcd1234"}}
	next, _ := m.Update(keyPress("D"))
	m = next.(Model)
	if !m.programWriting || m.programSent.State != "working" {
		t.Fatal("working report not queued")
	}
	next, _ = m.Update(aiGeneratedMsg{changeID: "abcd1234", err: errors.New("failed")})
	m = next.(Model)
	if m.programReport.State != "error" || m.programSent.State != "working" {
		t.Fatal("completion overtook in-flight working report")
	}
	next, cmd := m.Update(programStatusWrittenMsg{})
	m = next.(Model)
	if cmd == nil || m.programSent.State != "error" {
		t.Fatal("pending error not sent after acknowledgment")
	}
}

func TestRepeatedBackgroundError(t *testing.T) {
	m := NewModel()
	m.ready = true
	failure := errors.New("fetch failed")
	for range 2 {
		m, _ = m.startBusy("fetching…")
		m = statusStep(t, m, tea.BlurMsg{}, "working")
		m = statusStep(t, m, actionDoneMsg{err: failure}, "error")
		m = statusStep(t, m, keyPress("j"), "idle")
	}
}
