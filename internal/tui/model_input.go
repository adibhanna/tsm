package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/adibhanna/tsm/internal/engine"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.state == stateConfirmKill {
		return m.handleConfirmKey(msg)
	}

	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	if m.handleLogScroll(msg) {
		return m, nil
	}

	if m.inlineFilterEnabled() {
		if msg.Code == tea.KeyEscape {
			if m.filterText == "" {
				return m, tea.Quit
			}
			m.filterText = ""
			m.markVisibleChanged()
			m.cursor = 0
			m.listOffset = 0
			return m, nil
		}
		if msg.Code == tea.KeyBackspace && m.filterText != "" {
			m.filterText = m.filterText[:len(m.filterText)-1]
			m.markVisibleChanged()
			m.pruneSelections()
			m.cursor = 0
			m.listOffset = 0
			return m, nil
		}
	}

	// Escape or Backspace clears active filter in normal mode
	if (msg.Code == tea.KeyEscape || msg.Code == tea.KeyBackspace) && m.filterText != "" {
		m.filterText = ""
		m.markVisibleChanged()
		m.cursor = 0
		m.listOffset = 0
		return m, m.previewCmd()
	}

	visible := m.visibleSessions()

	// Ctrl+A toggles select all
	if m.isToggleSelectAllKey(msg) {
		m.toggleSelectAll(visible)
		return m, nil
	}

	switch {
	case m.isMoveUpKey(msg):
		if m.cursor > 0 {
			m.cursor--
			m.previewScrollX = 0
			m.ensureVisible()
			return m, m.previewCmd()
		}

	case m.isMoveDownKey(msg):
		if m.cursor < len(visible)-1 {
			m.cursor++
			m.previewScrollX = 0
			m.ensureVisible()
			return m, m.previewCmd()
		}

	case m.isMoveLeftKey(msg):
		if m.previewScrollX > 0 {
			m.previewScrollX -= 4
			if m.previewScrollX < 0 {
				m.previewScrollX = 0
			}
		}

	case m.isMoveRightKey(msg):
		maxW := previewMaxWidth(m.preview)
		limit := maxW - m.previewInnerWidth()
		if limit < 0 {
			limit = 0
		}
		if m.previewScrollX+4 <= limit {
			m.previewScrollX += 4
		} else {
			m.previewScrollX = limit
		}

	case m.isToggleSelectKey(msg):
		if m.cursor < len(visible) {
			name := visible[m.cursor].Name
			if m.selected[name] {
				delete(m.selected, name)
			} else {
				m.selected[name] = true
			}
		}

	case m.isAttachKey(msg):
		if m.cursor < len(visible) {
			m.attachTarget = visible[m.cursor].Name
			return m, tea.Quit
		}

	default:
		switch {
		case m.isKillKey(msg):
			targets := m.killTargets()
			if len(targets) > 0 {
				m.state = stateConfirmKill
			}
		case m.isDetachKey(msg):
			targets := m.detachTargets()
			if len(targets) == 0 {
				return m, nil
			}
			m.addLog(titleStyle.Render(fmt.Sprintf("Detaching %d session(s)...", len(targets))))
			cmds := make([]tea.Cmd, 0, len(targets))
			for _, name := range targets {
				cmds = append(cmds, detachOneCmd(name))
			}
			return m, tea.Batch(cmds...)
		case m.isCopyKey(msg):
			if m.cursor < len(visible) {
				name := visible[m.cursor].Name
				text := fmt.Sprintf("tsm attach %s", name)
				if err := engine.CopyToClipboard(text); err != nil {
					m.status = fmt.Sprintf("Copy failed: %v", err)
					m.addLog(confirmStyle.Render(fmt.Sprintf("  ✗ Copy failed: %v", err)))
				} else {
					m.status = "Copied!"
					m.addLog(statusStyle.Render(fmt.Sprintf("  Copied: %s", text)))
				}
				return m, clearStatusAfter(2 * time.Second)
			}
		case m.isNewKey(msg):
			m.state = stateNewSession
			m.newSessionName = ""
		case m.isRenameKey(msg):
			if m.cursor < len(visible) {
				m.state = stateRenameSession
				m.renameOldName = visible[m.cursor].Name
				m.renameNewName = visible[m.cursor].Name
			}
		case m.isRefreshKey(msg):
			m.refreshing = true
			m.refreshFrame = 0
			m.status = "Refreshing"
			return m, tea.Batch(fetchSessionsCmd, refreshSpinnerCmd())
		case m.isFilterKey(msg):
			m.state = stateFilter
		case m.isSortKey(msg):
			if m.sortAsc {
				m.sortAsc = false
			} else {
				m.sortAsc = true
				m.sortMode = (m.sortMode + 1) % sortModeCount
			}
			m.markVisibleChanged()
			m.cursor = 0
			m.listOffset = 0
			return m, m.previewCmd()
		case m.isToggleLayoutKey(msg):
			return m, m.toggleLayout()
		case m.isMuxOpenKey(msg):
			if len(m.workspaceNames) > 0 {
				m.state = stateMuxOpen
				m.workspaceCursor = 0
			} else {
				m.status = "No workspace manifests found"
				return m, clearStatusAfter(2 * time.Second)
			}
		case m.isProjectPickKey(msg):
			if len(m.projectWorktrees) > 0 {
				m.state = stateProjectPick
				m.projectCursor = 0
			} else {
				m.status = "No projects configured"
				return m, clearStatusAfter(2 * time.Second)
			}
		case m.inlineFilterEnabled() && m.isTextInput(msg):
			m.filterText += msg.Text
			m.markVisibleChanged()
			m.pruneSelections()
			m.cursor = 0
			m.listOffset = 0
		}
	}

	return m, nil
}

func (m *Model) toggleSelectAll(visible []Session) {
	if len(visible) == 0 {
		return
	}
	allSelected := true
	for _, s := range visible {
		if !m.selected[s.Name] {
			allSelected = false
			break
		}
	}
	if allSelected {
		for _, s := range visible {
			delete(m.selected, s.Name)
		}
	} else {
		for _, s := range visible {
			m.selected[s.Name] = true
		}
	}
}

// pruneSelections removes selections for sessions not in the current visible set.
func (m *Model) pruneSelections() {
	visible := m.visibleSessions()
	allowed := make(map[string]bool, len(visible))
	for _, s := range visible {
		allowed[s.Name] = true
	}
	for name := range m.selected {
		if !allowed[name] {
			delete(m.selected, name)
		}
	}
}

func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.filterText = ""
		m.markVisibleChanged()
		m.state = stateNormal
		m.cursor = 0
		m.listOffset = 0
		return m, m.previewCmd()

	case tea.KeyEnter:
		m.state = stateNormal
		m.clampCursor()
		return m, m.previewCmd()

	case tea.KeyBackspace:
		if len(m.filterText) > 0 {
			m.filterText = m.filterText[:len(m.filterText)-1]
			m.markVisibleChanged()
			m.pruneSelections()
			m.cursor = 0
			m.listOffset = 0
		} else {
			// Backspace on empty filter exits filter mode
			m.state = stateNormal
			return m, m.previewCmd()
		}

	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.ensureVisible()
			return m, m.previewCmd()
		}

	case tea.KeyDown:
		visible := m.visibleSessions()
		if m.cursor < len(visible)-1 {
			m.cursor++
			m.ensureVisible()
			return m, m.previewCmd()
		}

	default:
		if msg.Text != "" {
			m.filterText += msg.Text
			m.markVisibleChanged()
			m.pruneSelections()
			m.cursor = 0
			m.listOffset = 0
		}
	}

	return m, nil
}

func (m Model) handleNewSessionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.state = stateNormal
		m.newSessionName = ""
		return m, nil

	case tea.KeyEnter:
		name := strings.TrimSpace(m.newSessionName)
		if name == "" {
			m.state = stateNormal
			m.newSessionName = ""
			return m, nil
		}
		m.state = stateNormal
		m.newSessionName = ""
		m.addLog(helpStyle.Render(fmt.Sprintf("  ⋯ Creating session: %s", name)))
		return m, createSessionCmd(name)

	case tea.KeyBackspace:
		if len(m.newSessionName) > 0 {
			m.newSessionName = m.newSessionName[:len(m.newSessionName)-1]
		} else {
			m.state = stateNormal
			return m, nil
		}

	default:
		if msg.Text != "" {
			// Disallow spaces and tabs in session names
			ch := msg.Text
			if ch != " " && ch != "\t" {
				m.newSessionName += ch
			}
		}
	}

	return m, nil
}

func (m Model) handleRenameSessionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.state = stateNormal
		m.renameOldName = ""
		m.renameNewName = ""
		return m, nil

	case tea.KeyEnter:
		newName := strings.TrimSpace(m.renameNewName)
		oldName := m.renameOldName
		m.state = stateNormal
		m.renameOldName = ""
		m.renameNewName = ""
		if newName == "" || newName == oldName {
			return m, nil
		}
		m.addLog(helpStyle.Render(fmt.Sprintf("  ⋯ Renaming: %s → %s", oldName, newName)))
		return m, renameSessionCmd(oldName, newName)

	case tea.KeyBackspace:
		if len(m.renameNewName) > 0 {
			m.renameNewName = m.renameNewName[:len(m.renameNewName)-1]
		} else {
			m.state = stateNormal
			return m, nil
		}

	default:
		if msg.Text != "" {
			ch := msg.Text
			if ch != " " && ch != "\t" {
				m.renameNewName += ch
			}
		}
	}

	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}
	if msg.Code == tea.KeyEscape || msg.Code == tea.KeyBackspace {
		m.state = stateNormal
		return m, nil
	}
	if isRune(msg, "y") {
		targets := m.killTargets()
		m.state = stateNormal
		m.selected = make(map[string]bool)

		m.addLog(titleStyle.Render(fmt.Sprintf("Killing %d session(s)...", len(targets))))

		// Optimistically remove killed sessions from the list
		removing := make(map[string]bool, len(targets))
		for _, name := range targets {
			removing[name] = true
		}
		kept := m.sessions[:0:0]
		for _, s := range m.sessions {
			if !removing[s.Name] {
				kept = append(kept, s)
			}
		}
		m.sessions = kept
		m.markSessionsChanged()
		m.clampCursor()

		// Fire all kills in parallel + update preview
		cmds := make([]tea.Cmd, 0, len(targets)+1)
		for _, name := range targets {
			cmds = append(cmds, killOneCmd(name))
		}
		if cmd := m.previewCmd(); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if isRune(msg, "n") {
		m.state = stateNormal
	}
	return m, nil
}

func (m *Model) handleLogScroll(msg tea.KeyPressMsg) bool {
	if m.isSimplified() {
		return false
	}
	if !m.matchesAction(ActionLogUp, msg) && !m.matchesAction(ActionLogDown, msg) {
		return false
	}
	maxOffset := len(m.logLines) - logContentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.matchesAction(ActionLogUp, msg) && m.logOffset > 0 {
		m.logOffset--
	}
	if m.matchesAction(ActionLogDown, msg) && m.logOffset < maxOffset {
		m.logOffset++
	}
	return true
}

func (m Model) handleMuxOpenKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.state = stateNormal
		return m, nil

	case tea.KeyEnter:
		if m.workspaceCursor < len(m.workspaceNames) {
			m.muxOpenTarget = m.workspaceNames[m.workspaceCursor]
			m.state = stateNormal
			return m, tea.Quit
		}
		m.state = stateNormal
		return m, nil

	default:
		switch {
		case m.isMoveUpKey(msg):
			if m.workspaceCursor > 0 {
				m.workspaceCursor--
			}
		case m.isMoveDownKey(msg):
			if m.workspaceCursor < len(m.workspaceNames)-1 {
				m.workspaceCursor++
			}
		}
	}

	return m, nil
}

func (m Model) handleProjectPickKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.isQuitKey(msg) {
		return m, tea.Quit
	}

	switch msg.Code {
	case tea.KeyEscape:
		m.state = stateNormal
		return m, nil

	case tea.KeyEnter:
		if m.projectCursor < len(m.projectWorktrees) {
			item := m.projectWorktrees[m.projectCursor]
			m.projectPickTarget = item.Project + "\t" + item.Branch
			m.state = stateNormal
			return m, tea.Quit
		}
		m.state = stateNormal
		return m, nil

	default:
		switch {
		case m.isMoveUpKey(msg):
			if m.projectCursor > 0 {
				m.projectCursor--
			}
		case m.isMoveDownKey(msg):
			if m.projectCursor < len(m.projectWorktrees)-1 {
				m.projectCursor++
			}
		}
	}

	return m, nil
}

func (m *Model) ensureVisible() {
	h := m.listContentHeight(1)
	if h <= 0 {
		return
	}
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+h {
		m.listOffset = m.cursor - h + 1
	}
}
