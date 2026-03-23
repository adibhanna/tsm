package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type Action string

const (
	ActionMoveUp          Action = "move_up"
	ActionMoveDown        Action = "move_down"
	ActionMoveLeft        Action = "move_left"
	ActionMoveRight       Action = "move_right"
	ActionToggleSelectAll Action = "toggle_select_all"
	ActionToggleSelect    Action = "toggle_select"
	ActionAttach          Action = "attach"
	ActionKill            Action = "kill"
	ActionDetach          Action = "detach"
	ActionCopyCommand     Action = "copy_command"
	ActionNewSession      Action = "new_session"
	ActionRename          Action = "rename"
	ActionRefresh         Action = "refresh"
	ActionFilter          Action = "filter"
	ActionSort            Action = "sort"
	ActionToggleLayout    Action = "toggle_layout"
	ActionQuit            Action = "quit"
	ActionForceQuit       Action = "force_quit"
	ActionLogUp           Action = "log_up"
	ActionLogDown         Action = "log_down"
	ActionMuxOpen         Action = "mux_open"
)

var allActions = []Action{
	ActionMoveUp,
	ActionMoveDown,
	ActionMoveLeft,
	ActionMoveRight,
	ActionToggleSelectAll,
	ActionToggleSelect,
	ActionAttach,
	ActionKill,
	ActionDetach,
	ActionCopyCommand,
	ActionNewSession,
	ActionRename,
	ActionRefresh,
	ActionFilter,
	ActionSort,
	ActionToggleLayout,
	ActionQuit,
	ActionForceQuit,
	ActionLogUp,
	ActionLogDown,
	ActionMuxOpen,
}

var actionAliases = map[string]Action{
	"attach":            ActionAttach,
	"copy":              ActionCopyCommand,
	"copy_command":      ActionCopyCommand,
	"detach":            ActionDetach,
	"filter":            ActionFilter,
	"force_quit":        ActionForceQuit,
	"hard_quit":         ActionForceQuit,
	"kill":              ActionKill,
	"log_down":          ActionLogDown,
	"log_up":            ActionLogUp,
	"move_down":         ActionMoveDown,
	"move_left":         ActionMoveLeft,
	"move_right":        ActionMoveRight,
	"move_up":           ActionMoveUp,
	"new":               ActionNewSession,
	"new_session":       ActionNewSession,
	"quit":              ActionQuit,
	"refresh":           ActionRefresh,
	"rename":            ActionRename,
	"rename_session":    ActionRename,
	"select":            ActionToggleSelect,
	"select_all":        ActionToggleSelectAll,
	"sort":              ActionSort,
	"toggle_layout":     ActionToggleLayout,
	"layout":            ActionToggleLayout,
	"toggle_select":     ActionToggleSelect,
	"toggle_select_all": ActionToggleSelectAll,
	"mux_open":          ActionMuxOpen,
	"workspace":         ActionMuxOpen,
}

type KeyBinding struct {
	Code    rune
	Text    string
	Stroke  string
	Ctrl    bool
	Display string
}

func (k KeyBinding) Matches(msg tea.KeyPressMsg) bool {
	if k.Ctrl != msg.Mod.Contains(tea.ModCtrl) {
		return false
	}
	if msg.Mod.Contains(tea.ModAlt) {
		return false
	}
	if k.Stroke != "" && msg.Keystroke() == k.Stroke {
		return true
	}
	if k.Text != "" {
		return msg.Text == k.Text
	}
	return msg.Code == k.Code
}

type Bindings map[Action][]KeyBinding

func (b Bindings) Clone() Bindings {
	cloned := make(Bindings, len(b))
	for action, bindings := range b {
		cloned[action] = slices.Clone(bindings)
	}
	return cloned
}

func (b Bindings) IsZero() bool {
	return len(b) == 0
}

func (b Bindings) Matches(action Action, msg tea.KeyPressMsg) bool {
	for _, binding := range b[action] {
		if binding.Matches(msg) {
			return true
		}
	}
	return false
}

func (b Bindings) PrimaryLabel(action Action) string {
	bindings := b[action]
	if len(bindings) == 0 {
		return ""
	}
	return bindings[0].Display
}

func DefaultBindings(keymap Keymap) Bindings {
	bindings := make(Bindings, len(allActions))

	mustSet := func(action Action, raw ...string) {
		parsed, err := parseBindingList(raw)
		if err != nil {
			panic(err)
		}
		bindings[action] = parsed
	}

	mustSet(ActionMoveUp, "up")
	mustSet(ActionMoveDown, "down")
	mustSet(ActionMoveLeft, "left")
	mustSet(ActionMoveRight, "right")
	mustSet(ActionToggleSelectAll, "ctrl+a")
	mustSet(ActionAttach, "enter")
	mustSet(ActionToggleLayout, "ctrl+o")
	mustSet(ActionForceQuit, "ctrl+c")
	mustSet(ActionLogUp, "[")
	mustSet(ActionLogDown, "]")

	switch keymap {
	case KeymapPalette:
		mustSet(ActionMoveUp, "up", "ctrl+p")
		mustSet(ActionMoveDown, "down", "ctrl+n")
		mustSet(ActionToggleSelect, "tab")
		mustSet(ActionKill, "ctrl+x")
		mustSet(ActionDetach, "ctrl+d")
		mustSet(ActionCopyCommand, "ctrl+y")
		mustSet(ActionNewSession, "ctrl+t")
		mustSet(ActionRename, "r")
		mustSet(ActionRefresh, "ctrl+r")
		mustSet(ActionFilter, "ctrl+f")
		mustSet(ActionSort, "ctrl+s")
		mustSet(ActionMuxOpen, "ctrl+w")
		mustSet(ActionQuit, "q")
	default:
		mustSet(ActionToggleSelect, "space")
		mustSet(ActionKill, "k")
		mustSet(ActionDetach, "d")
		mustSet(ActionCopyCommand, "c")
		mustSet(ActionNewSession, "n")
		mustSet(ActionRename, "r")
		mustSet(ActionRefresh, "ctrl+r")
		mustSet(ActionFilter, "/")
		mustSet(ActionSort, "s")
		mustSet(ActionMuxOpen, "w")
		mustSet(ActionQuit, "q")
	}

	return bindings
}

func BuildBindings(keymap Keymap, overrides map[string][]string) (Bindings, error) {
	bindings := DefaultBindings(keymap)
	for rawAction, rawBindings := range overrides {
		action, err := ParseAction(rawAction)
		if err != nil {
			return nil, err
		}
		parsed, err := parseBindingList(rawBindings)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", action, err)
		}
		bindings[action] = parsed
	}
	if err := validateBindingConflicts(bindings); err != nil {
		return nil, err
	}
	return bindings, nil
}

func validateBindingConflicts(bindings Bindings) error {
	owners := map[string]Action{}
	for _, action := range allActions {
		for _, binding := range bindings[action] {
			key := bindingConflictKey(binding)
			if prev, ok := owners[key]; ok && prev != action {
				return fmt.Errorf("binding %q conflicts between %s and %s", binding.Display, prev, action)
			}
			owners[key] = action
		}
	}
	return nil
}

func bindingConflictKey(binding KeyBinding) string {
	if binding.Stroke != "" {
		return binding.Stroke
	}
	var b strings.Builder
	if binding.Ctrl {
		b.WriteString("ctrl+")
	}
	if binding.Text != "" {
		b.WriteString(binding.Text)
		return b.String()
	}
	b.WriteRune(binding.Code)
	return b.String()
}

func ParseAction(raw string) (Action, error) {
	action, ok := actionAliases[strings.ToLower(strings.TrimSpace(raw))]
	if !ok {
		return "", fmt.Errorf("unknown action %q", raw)
	}
	return action, nil
}

func parseBindingList(raw []string) ([]KeyBinding, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("binding list cannot be empty")
	}
	parsed := make([]KeyBinding, 0, len(raw))
	for _, item := range raw {
		binding, err := ParseBinding(item)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, binding)
	}
	return parsed, nil
}

func ParseBinding(raw string) (KeyBinding, error) {
	original := strings.TrimSpace(raw)
	if original == "" {
		return KeyBinding{}, fmt.Errorf("empty binding")
	}

	ctrl := false
	normalized := strings.ToLower(original)
	switch {
	case strings.HasPrefix(normalized, "ctrl+"):
		ctrl = true
		original = strings.TrimSpace(original[len("ctrl+"):])
	case strings.HasPrefix(original, "^"):
		ctrl = true
		original = strings.TrimSpace(strings.TrimPrefix(original, "^"))
	}

	if original == "" {
		return KeyBinding{}, fmt.Errorf("invalid binding %q", raw)
	}

	if code, display, ok := namedBinding(strings.ToLower(original)); ok {
		stroke := display
		if ctrl {
			stroke = "ctrl+" + stroke
		}
		return KeyBinding{Code: code, Ctrl: ctrl, Stroke: stroke, Display: renderBindingLabel(display, ctrl)}, nil
	}

	if len([]rune(original)) == 1 {
		r := []rune(original)[0]
		stroke := strings.ToLower(original)
		if ctrl {
			stroke = "ctrl+" + stroke
		}
		return KeyBinding{Code: r, Text: original, Ctrl: ctrl, Stroke: stroke, Display: renderBindingLabel(original, ctrl)}, nil
	}

	return KeyBinding{}, fmt.Errorf("unsupported binding %q", raw)
}

func namedBinding(raw string) (rune, string, bool) {
	switch raw {
	case "up":
		return tea.KeyUp, "↑", true
	case "down":
		return tea.KeyDown, "↓", true
	case "left":
		return tea.KeyLeft, "←", true
	case "right":
		return tea.KeyRight, "→", true
	case "enter", "return":
		return tea.KeyEnter, "enter", true
	case "tab":
		return tea.KeyTab, "tab", true
	case "space":
		return tea.KeySpace, "space", true
	case "esc", "escape":
		return tea.KeyEscape, "esc", true
	case "backspace":
		return tea.KeyBackspace, "backspace", true
	}
	return 0, "", false
}

func renderBindingLabel(label string, ctrl bool) string {
	if !ctrl {
		return label
	}
	if len([]rune(label)) == 1 {
		return "^" + strings.ToLower(label)
	}
	if label == "↑" || label == "↓" || label == "←" || label == "→" {
		return "^" + label
	}
	return "ctrl+" + label
}
