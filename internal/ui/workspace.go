package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"gojo/internal/jj"
)

func (m Model) loadWorkspacesCmd() tea.Cmd {
	r := m.runner
	return func() tea.Msg {
		workspaces, err := r.WorkspaceList()
		return workspaceListMsg{workspaces: workspaces, err: err}
	}
}

func (m Model) workspaceContentHeight() int {
	return max(1, m.contentHeight()-1)
}

func (m Model) workspaceMaxOffset() int {
	return max(0, len(m.workspaces)-m.workspaceContentHeight())
}

func (m *Model) workspaceClamp() {
	if len(m.workspaces) == 0 {
		m.workspaceCursor = 0
		m.workspaceOffset = 0
		return
	}
	m.workspaceCursor = min(max(0, m.workspaceCursor), len(m.workspaces)-1)
	if m.workspaceCursor < m.workspaceOffset {
		m.workspaceOffset = m.workspaceCursor
	}
	if m.workspaceCursor >= m.workspaceOffset+m.workspaceContentHeight() {
		m.workspaceOffset = m.workspaceCursor - m.workspaceContentHeight() + 1
	}
	m.workspaceOffset = min(max(0, m.workspaceOffset), m.workspaceMaxOffset())
}

func (m *Model) workspaceMove(cursor int) {
	m.workspaceCursor = cursor
	m.workspaceClamp()
}

func (m Model) selectedWorkspace() *jj.Workspace {
	if m.workspaceCursor < 0 || m.workspaceCursor >= len(m.workspaces) {
		return nil
	}
	return &m.workspaces[m.workspaceCursor]
}

func (m Model) workspaceIsCurrent(workspace jj.Workspace) bool {
	return workspace.Root != "" && filepath.Clean(workspace.Root) == filepath.Clean(m.cfg.RepoRoot)
}

func (m Model) runnerForWorkspace(workspace jj.Workspace) (*jj.Runner, error) {
	if workspace.Root == "" {
		return nil, fmt.Errorf("workspace %q has no available directory", workspace.Name)
	}
	cfg := m.cfg
	cfg.RepoRoot = workspace.Root
	return jj.NewRunner(cfg), nil
}

func (m Model) workspaceActionCmd(label, ok string, fn func() error) (tea.Model, tea.Cmd) {
	m.workspaceAction = ""
	m.workspaceInput = ""
	m, tick := m.startBusy(label)
	return m, tea.Batch(tick, func() tea.Msg {
		return workspaceDoneMsg{message: ok, err: fn()}
	})
}

func (m Model) handleWorkspaceKey(msg tea.KeyPressMsg, k string) (tea.Model, tea.Cmd) {
	if m.workspaceAction != "" {
		switch m.keys.resolve(ctxInput, k) {
		case actCancel:
			m.workspaceAction = ""
			m.workspaceInput = ""
			return m, nil
		case actErase:
			m.workspaceInput = trimLastRune(m.workspaceInput)
			return m, nil
		case actClear:
			m.workspaceInput = ""
			return m, nil
		case actAccept:
			workspace := m.selectedWorkspace()
			switch m.workspaceAction {
			case actAdd:
				destination := strings.TrimSpace(m.workspaceInput)
				if strings.HasPrefix(destination, "~/") && m.home != "" {
					destination = filepath.Join(m.home, destination[2:])
				}
				if destination == "" {
					return m, nil
				}
				r := m.runner
				return m.workspaceActionCmd("adding workspace…", "workspace added: "+destination, func() error {
					return r.WorkspaceAdd(destination, "")
				})
			case actRename:
				name := strings.TrimSpace(m.workspaceInput)
				if workspace == nil || name == "" {
					return m, nil
				}
				r, err := m.runnerForWorkspace(*workspace)
				if err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				old := workspace.Name
				return m.workspaceActionCmd("renaming workspace…", "workspace renamed: "+old+" -> "+name, func() error {
					return r.WorkspaceRename(name)
				})
			case actForget:
				if workspace == nil {
					return m, nil
				}
				name := workspace.Name
				r := m.runner
				return m.workspaceActionCmd("forgetting workspace…", "workspace forgotten: "+name, func() error {
					return r.WorkspaceForget(name)
				})
			}
		}
		if m.workspaceAction != actForget {
			if s, ok := typed(msg); ok {
				m.workspaceInput += s
			}
		}
		return m, nil
	}

	switch m.keys.resolve(ctxWorkspace, k) {
	case actCancel:
		m.workspaceOpen = false
		return m, nil
	case actUp:
		m.workspaceMove(m.workspaceCursor - 1)
	case actDown:
		m.workspaceMove(m.workspaceCursor + 1)
	case actTop:
		m.workspaceMove(0)
	case actBottom:
		m.workspaceMove(len(m.workspaces) - 1)
	case actPageUp:
		m.workspaceMove(m.workspaceCursor - m.workspaceContentHeight())
	case actPageDown:
		m.workspaceMove(m.workspaceCursor + m.workspaceContentHeight())
	case actOpen:
		workspace := m.selectedWorkspace()
		if workspace == nil {
			return m, nil
		}
		if m.workspaceIsCurrent(*workspace) {
			m.workspaceOpen = false
			return m, nil
		}
		if workspace.Root == "" {
			m.errMsg = "workspace has no available directory: " + workspace.Name
			return m, nil
		}
		m.cfg.RepoRoot = workspace.Root
		m.runner = jj.NewRunner(m.cfg)
		m.repoRoot = workspace.Root
		m.cwd = workspace.Root
		m.workspaceOpen = false
		m.diffOpen = false
		m.view = viewLog
		m.cursor, m.offset = 0, 0
		m.message = "switched workspace: " + workspace.Name
		m.errMsg = ""
		return m, m.refreshCmd()
	case actAdd:
		m.workspaceAction = actAdd
		m.workspaceInput = ""
	case actRename:
		if workspace := m.selectedWorkspace(); workspace != nil {
			if workspace.Root == "" {
				m.errMsg = "workspace has no available directory: " + workspace.Name
				return m, nil
			}
			m.workspaceAction = actRename
			m.workspaceInput = workspace.Name
		}
	case actForget:
		if workspace := m.selectedWorkspace(); workspace != nil {
			if m.workspaceIsCurrent(*workspace) {
				m.errMsg = "cannot forget the active workspace"
				return m, nil
			}
			m.workspaceAction = actForget
		}
	case actUpdate:
		workspace := m.selectedWorkspace()
		if workspace == nil {
			return m, nil
		}
		r, err := m.runnerForWorkspace(*workspace)
		if err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		name := workspace.Name
		return m.workspaceActionCmd("updating workspace…", "workspace updated: "+name, r.WorkspaceUpdateStale)
	}
	return m, nil
}

func (m Model) renderWorkspacePicker(width, height int) []string {
	contentH := max(1, height-1)
	titleLeft := fmt.Sprintf(" gojo workspaces (%d)", len(m.workspaces))
	titleRight := m.hk(ctxWorkspace, actOpen) + " switch  " + m.hk(ctxWorkspace, actCancel) + " close "
	titlePad := max(1, width-segTextWidth(titleLeft)-segTextWidth(titleRight))
	out := []string{bgRow(width, colElement,
		seg{text: titleLeft, fg: colCyan, bg: colElement},
		seg{text: strings.Repeat(" ", titlePad), bg: colElement},
		seg{text: titleRight, fg: colGray, bg: colElement})}

	offset := min(max(0, m.workspaceOffset), m.workspaceMaxOffset())
	end := min(len(m.workspaces), offset+contentH)
	for i := offset; i < end; i++ {
		workspace := m.workspaces[i]
		rowBg := colPanel
		if i == m.workspaceCursor {
			rowBg = colElement
		} else if i == m.hover.workspaceRow {
			rowBg = colHover
		}
		marker := "  "
		if m.workspaceIsCurrent(workspace) {
			marker = "● "
		}
		root := workspace.Root
		if root == "" {
			root = "(missing)"
		} else if m.home != "" && strings.HasPrefix(root, m.home) {
			root = "~" + root[len(m.home):]
		}
		out = append(out, bgRow(width, rowBg,
			seg{text: marker, fg: colGreen, bg: rowBg},
			seg{text: workspace.Name, fg: colText, bold: i == m.workspaceCursor, bg: rowBg},
			seg{text: "  " + root, fg: colTextMuted, bg: rowBg},
			seg{text: "  " + shortID(workspace.ChangeID), fg: colPurple, bg: rowBg}))
	}
	for len(out) < height {
		out = append(out, blankRow(width, colPanel))
	}
	return out[:min(len(out), height)]
}

func (m Model) workspaceRowAtMouseY(mouseY int) (int, bool) {
	line := mouseY - contentTopBarHeight - 1
	if line < 0 {
		return 0, false
	}
	idx := m.workspaceOffset + line
	if idx < 0 || idx >= len(m.workspaces) || idx >= m.workspaceOffset+m.workspaceContentHeight() {
		return 0, false
	}
	return idx, true
}

func (m Model) handleWorkspaceClick(mouseY int) (tea.Model, tea.Cmd) {
	idx, ok := m.workspaceRowAtMouseY(mouseY)
	if !ok {
		return m, nil
	}
	if idx == m.workspaceCursor {
		return m.handleWorkspaceKey(tea.KeyPressMsg{}, "enter")
	}
	m.workspaceMove(idx)
	return m, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (m Model) renderWorkspaceStatusBar() []string {
	if m.errMsg != "" {
		return []string{bgRow(m.width, colDarkerGray, seg{text: " x " + m.errMsg, fg: colRed})}
	}
	if len(m.busy) > 0 {
		label := m.busy[len(m.busy)-1]
		frame := spinnerFrames[m.spinnerFrame%len(spinnerFrames)]
		return []string{bgRow(m.width, colDarkerGray, seg{text: " " + frame + " " + label, fg: colMagenta})}
	}
	if m.workspaceAction != "" {
		workspace := m.selectedWorkspace()
		switch m.workspaceAction {
		case actAdd:
			return []string{bgRow(m.width, colDarkerGray, seg{text: " [workspace add] destination: " + m.workspaceInput + "█", fg: colCyan})}
		case actRename:
			return []string{bgRow(m.width, colDarkerGray, seg{text: " [workspace rename] new name: " + m.workspaceInput + "█", fg: colCyan})}
		case actForget:
			name := ""
			if workspace != nil {
				name = workspace.Name
			}
			return []string{bgRow(m.width, colDarkerGray, seg{text: " forget " + name + "? " + m.hk(ctxInput, actAccept) + " confirm · " + m.hk(ctxInput, actCancel) + " cancel (files stay on disk)", fg: colYellow})}
		}
	}
	text := " [workspaces] " + m.hk(ctxWorkspace, actAdd) + " add · " + m.hk(ctxWorkspace, actRename) + " rename · " +
		m.hk(ctxWorkspace, actForget) + " forget · " + m.hk(ctxWorkspace, actUpdate) + " update stale · " +
		m.hk(ctxWorkspace, actOpen) + " switch"
	if m.message != "" {
		text += " · " + m.message
	}
	return []string{bgRow(m.width, colDarkerGray, seg{text: text, fg: colGray})}
}
