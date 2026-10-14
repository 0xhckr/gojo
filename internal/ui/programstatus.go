package ui

import (
	tea "charm.land/bubbletea/v2"

	"gojo/internal/programstatus"
)

type programStatusWrittenMsg struct{}

func backgroundError(msg tea.Msg) error {
	switch msg := msg.(type) {
	case actionDoneMsg:
		return msg.err
	case aiGeneratedMsg:
		return msg.err
	case aiAppliedMsg:
		return msg.err
	case listLoadedMsg:
		return msg.err
	case fileListMsg:
		return msg.err
	case fileAnnotateMsg:
		return msg.err
	case fileHistoryMsg:
		return msg.err
	case workspaceListMsg:
		return msg.err
	case workspaceDoneMsg:
		return msg.err
	case newCreatedMsg:
		return msg.err
	}
	return nil
}

func (m Model) programError() string {
	if m.bootErr != "" {
		return m.bootErr
	}
	if m.bootInitErr != "" {
		return m.bootInitErr
	}
	if m.errMsg != "" {
		return m.errMsg
	}
	return m.fileView.err
}

func (m *Model) syncProgramStatus(msg tea.Msg, wasBusy bool, previousError string) tea.Cmd {
	if _, ok := msg.(programStatusWrittenMsg); ok {
		m.programWriting = false
	}
	active := m.spinnerActive() || m.bootInitStage == 3
	if active && !wasBusy {
		m.programFailure = ""
	}
	report := m.programReport
	if _, ok := msg.(tea.KeyPressMsg); ok && m.ready && !active {
		m.programFailure = ""
		if report.State == "done" || report.State == "error" {
			report = programstatus.Report{State: "idle"}
		}
	}
	if err := m.programError(); err != "" && err != previousError {
		m.programFailure = err
	}
	if err := backgroundError(msg); err != nil && m.pendingElev == nil {
		m.programFailure = err.Error()
	}

	switch {
	case m.pendingElev != nil:
		report = programstatus.Report{State: "blocked", Kind: "permission", Message: m.pendingElev.reason}
	case m.bootInitStage == 1:
		report = programstatus.Report{State: "blocked", Kind: "question", Message: "Initialize a jj repository here?"}
	case m.bootInitStage == 2:
		report = programstatus.Report{State: "blocked", Kind: "question", Message: "Colocate with Git?"}
	case m.bootInitStage == 3:
		report = programstatus.Report{State: "working", Message: "Initializing repository"}
	case len(m.aiLoading) > 0:
		report = programstatus.Report{State: "working", Message: "Generating AI commit messages"}
	case len(m.busy) > 0:
		report = programstatus.Report{State: "working", Message: m.busy[len(m.busy)-1]}
	case m.programFailure != "":
		report = programstatus.Report{State: "error", Message: m.programFailure}
	case wasBusy:
		report = programstatus.Report{State: "done", Message: "Work completed"}
	case report.State == "blocked" || report.State == "":
		report = programstatus.Report{State: "idle"}
	}
	m.programReport = report
	if m.programSent.State == "" && report.State == "idle" {
		m.programSent = report
		return nil
	}
	// Serialize reports so a fast completion cannot overtake its working report.
	if m.programWriting || report == m.programSent {
		return nil
	}
	m.programSent = report
	m.programWriting = true
	return tea.Sequence(tea.Raw(report.Sequence()), func() tea.Msg {
		return programStatusWrittenMsg{}
	})
}
