package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestBuildBindingsOverridesDefaultAction(t *testing.T) {
	bindings, err := BuildBindings(KeymapDefault, map[string][]string{
		"detach": []string{"x"},
	})
	if err != nil {
		t.Fatalf("BuildBindings() error = %v", err)
	}
	if !bindings.Matches(ActionDetach, tea.KeyPressMsg{Text: "x"}) {
		t.Fatal("expected custom detach binding to match")
	}
	if bindings.Matches(ActionDetach, tea.KeyPressMsg{Text: "d"}) {
		t.Fatal("expected custom detach binding to replace default binding")
	}
}

func TestBuildBindingsSupportsAliases(t *testing.T) {
	bindings, err := BuildBindings(KeymapDefault, map[string][]string{
		"copy": []string{"ctrl+k"},
	})
	if err != nil {
		t.Fatalf("BuildBindings() error = %v", err)
	}
	if !bindings.Matches(ActionCopyCommand, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}) {
		t.Fatal("expected alias override to apply to copy command")
	}
}

func TestBuildBindingsRejectsConflicts(t *testing.T) {
	_, err := BuildBindings(KeymapDefault, map[string][]string{
		"attach": []string{"enter"},
		"detach": []string{"enter"},
	})
	if err == nil {
		t.Fatal("BuildBindings() error = nil, want conflict error")
	}
	if got := err.Error(); got != `binding "enter" conflicts between attach and detach` {
		t.Fatalf("BuildBindings() error = %q", got)
	}
}

func TestParseBindingFormatsLabels(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"space", "space"},
		{"ctrl+y", "^y"},
		{"up", "↑"},
		{"ctrl+up", "^↑"},
		{"R", "R"},
	}
	for _, tt := range tests {
		binding, err := ParseBinding(tt.raw)
		if err != nil {
			t.Fatalf("ParseBinding(%q) error = %v", tt.raw, err)
		}
		if binding.Display != tt.want {
			t.Fatalf("ParseBinding(%q) label = %q, want %q", tt.raw, binding.Display, tt.want)
		}
	}
}

func TestDefaultBindingsIncludePaletteCtrlNavigation(t *testing.T) {
	bindings := DefaultBindings(KeymapPalette)
	if !bindings.Matches(ActionMoveUp, tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}) {
		t.Fatal("expected ctrl+p to navigate up in palette keymap")
	}
	if !bindings.Matches(ActionMoveDown, tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}) {
		t.Fatal("expected ctrl+n to navigate down in palette keymap")
	}
}

func TestDefaultBindingsMatchCtrlOLayoutToggle(t *testing.T) {
	bindings := DefaultBindings(KeymapDefault)
	if !bindings.Matches(ActionToggleLayout, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}) {
		t.Fatal("expected ctrl+o to toggle layout")
	}
}

func TestDefaultBindingsUseRForRenameAndCtrlRForRefresh(t *testing.T) {
	tests := []Keymap{KeymapDefault, KeymapPalette}
	for _, keymap := range tests {
		bindings := DefaultBindings(keymap)
		if !bindings.Matches(ActionRename, tea.KeyPressMsg{Text: "r"}) {
			t.Fatalf("keymap %v: expected plain r to rename", keymap)
		}
		if bindings.Matches(ActionRename, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}) {
			t.Fatalf("keymap %v: expected ctrl+r to stop renaming", keymap)
		}
	}

	for _, keymap := range tests {
		bindings := DefaultBindings(keymap)
		if !bindings.Matches(ActionRefresh, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}) {
			t.Fatalf("keymap %v: expected ctrl+r to refresh", keymap)
		}
		if bindings.Matches(ActionRefresh, tea.KeyPressMsg{Text: "r"}) {
			t.Fatalf("keymap %v: expected plain r to stop refreshing", keymap)
		}
	}
}

func TestJoinBindingLabelsUsesSeparatorForCustomPairs(t *testing.T) {
	if got := joinBindingLabels("j", "k"); got != "j/k" {
		t.Fatalf("joinBindingLabels() = %q, want j/k", got)
	}
	if got := joinBindingLabels("↑", "↓"); got != "↑↓" {
		t.Fatalf("joinBindingLabels() = %q, want arrows to stay compact", got)
	}
}
