package tui

import tea "charm.land/bubbletea/v2"

func isQuit(msg tea.KeyPressMsg) bool {
	if msg.Code == 'c' && msg.Mod.Contains(tea.ModCtrl) {
		return true
	}
	return msg.Text == "q"
}

func isRune(msg tea.KeyPressMsg, r string) bool {
	return msg.Text == r
}
