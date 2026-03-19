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

	if isQuit(msg) {
		return m, tea.Quit
	}

	m.handleLogScroll(msg)

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
	if msg.Code == 'a' && msg.Mod.Contains(tea.ModCtrl) {
		m.toggleSelectAll(visible)
		return m, nil
	}

	switch msg.Code {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
			m.previewScrollX = 0
			m.ensureVisible()
			return m, m.previewCmd()
		}

	case tea.KeyDown:
		if m.cursor < len(visible)-1 {
			m.cursor++
			m.previewScrollX = 0
			m.ensureVisible()
			return m, m.previewCmd()
		}

	case tea.KeyLeft:
		if m.previewScrollX > 0 {
			m.previewScrollX -= 4
			if m.previewScrollX < 0 {
				m.previewScrollX = 0
			}
		}

	case tea.KeyRight:
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

	case tea.KeySpace:
		if m.cursor < len(visible) {
			name := visible[m.cursor].Name
			if m.selected[name] {
				delete(m.selected, name)
			} else {
				m.selected[name] = true
			}
		}

	case tea.KeyEnter:
		if m.cursor < len(visible) {
			m.attachTarget = visible[m.cursor].Name
			return m, tea.Quit
		}

	default:
		if msg.Text != "" {
			switch msg.Text {
			case "k":
				targets := m.killTargets()
				if len(targets) > 0 {
					m.state = stateConfirmKill
				}
			case "c":
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
			case "n":
				m.state = stateNewSession
				m.newSessionName = ""
			case "R":
				if m.cursor < len(visible) {
					m.state = stateRenameSession
					m.renameOldName = visible[m.cursor].Name
					m.renameNewName = visible[m.cursor].Name
				}
			case "r":
				return m, fetchSessionsCmd
			case "/":
				m.state = stateFilter
			case "s":
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
			}
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
	if isQuit(msg) {
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
	if isQuit(msg) {
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
	if isQuit(msg) {
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
	if isQuit(msg) {
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

func (m *Model) handleLogScroll(msg tea.KeyPressMsg) {
	if !isRune(msg, "[") && !isRune(msg, "]") {
		return
	}
	maxOffset := len(m.logLines) - logContentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if isRune(msg, "[") && m.logOffset > 0 {
		m.logOffset--
	}
	if isRune(msg, "]") && m.logOffset < maxOffset {
		m.logOffset++
	}
}

func (m *Model) ensureVisible() {
	h := m.mainContentHeight(1)
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
