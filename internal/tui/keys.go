package tui

import tea "charm.land/bubbletea/v2"

func isRune(msg tea.KeyPressMsg, r string) bool {
	return msg.Text == r
}

func (m Model) matchesAction(action Action, msg tea.KeyPressMsg) bool {
	return m.bindings().Matches(action, msg)
}

func (m Model) actionLabel(action Action) string {
	return m.bindings().PrimaryLabel(action)
}

func (m Model) bindings() Bindings {
	return m.normalizedOpts.Bindings
}

func (m Model) isQuitKey(msg tea.KeyPressMsg) bool {
	if m.matchesAction(ActionForceQuit, msg) {
		return true
	}
	return !m.inlineFilterEnabled() && m.matchesAction(ActionQuit, msg)
}

func (m Model) isMoveUpKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionMoveUp, msg)
}

func (m Model) isMoveDownKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionMoveDown, msg)
}

func (m Model) isMoveLeftKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionMoveLeft, msg)
}

func (m Model) isMoveRightKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionMoveRight, msg)
}

func (m Model) isToggleSelectAllKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionToggleSelectAll, msg)
}

func (m Model) isToggleSelectKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionToggleSelect, msg)
}

func (m Model) isAttachKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionAttach, msg)
}

func (m Model) isKillKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionKill, msg)
}

func (m Model) isDetachKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionDetach, msg)
}

func (m Model) isCopyKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionCopyCommand, msg)
}

func (m Model) isNewKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionNewSession, msg)
}

func (m Model) isRenameKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionRename, msg)
}

func (m Model) isRefreshKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionRefresh, msg)
}

func (m Model) isFilterKey(msg tea.KeyPressMsg) bool {
	if m.inlineFilterEnabled() {
		return false
	}
	return m.matchesAction(ActionFilter, msg)
}

func (m Model) isSortKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionSort, msg)
}

func (m Model) isToggleLayoutKey(msg tea.KeyPressMsg) bool {
	return m.matchesAction(ActionToggleLayout, msg)
}

func (m Model) isTextInput(msg tea.KeyPressMsg) bool {
	return msg.Text != "" && !msg.Mod.Contains(tea.ModCtrl)
}

func (m Model) selectKeyLabel() string {
	return m.actionLabel(ActionToggleSelect)
}

func (m Model) selectAllKeyLabel() string {
	return m.actionLabel(ActionToggleSelectAll)
}

func (m Model) attachKeyLabel() string {
	return m.actionLabel(ActionAttach)
}

func (m Model) detachKeyLabel() string {
	return m.actionLabel(ActionDetach)
}

func (m Model) newKeyLabel() string {
	return m.actionLabel(ActionNewSession)
}

func (m Model) killKeyLabel() string {
	return m.actionLabel(ActionKill)
}

func (m Model) renameKeyLabel() string {
	return m.actionLabel(ActionRename)
}

func (m Model) copyKeyLabel() string {
	return m.actionLabel(ActionCopyCommand)
}

func (m Model) sortKeyLabel() string {
	return m.actionLabel(ActionSort)
}

func (m Model) toggleLayoutKeyLabel() string {
	return m.actionLabel(ActionToggleLayout)
}

func (m Model) refreshKeyLabel() string {
	return m.actionLabel(ActionRefresh)
}

func (m Model) filterKeyLabel() string {
	return m.actionLabel(ActionFilter)
}

func (m Model) quitKeyLabel() string {
	if m.inlineFilterEnabled() {
		return m.actionLabel(ActionForceQuit)
	}
	return m.actionLabel(ActionQuit)
}

func (m Model) navKeyLabel() string {
	return joinBindingLabels(m.actionLabel(ActionMoveUp), m.actionLabel(ActionMoveDown))
}

func (m Model) previewScrollKeyLabel() string {
	return joinBindingLabels(m.actionLabel(ActionMoveLeft), m.actionLabel(ActionMoveRight))
}

func (m Model) logScrollKeyLabel() string {
	return joinBindingLabels(m.actionLabel(ActionLogUp), m.actionLabel(ActionLogDown))
}

func joinBindingLabels(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" {
		return left
	}
	if isDirectionalLabel(left) && isDirectionalLabel(right) {
		return left + right
	}
	return left + "/" + right
}

func isDirectionalLabel(label string) bool {
	switch label {
	case "↑", "↓", "←", "→":
		return true
	default:
		return false
	}
}
