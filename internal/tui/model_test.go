package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello..."},
		{"hello world", 3, "hel"},
		{"hello world", 2, "he"},
		{"hello world", 1, "h"},
		{"hello world", 0, ""},
		{"hi", 4, "hi"},
		{"abcdefgh", 7, "abcd..."},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestHighlightMatch(t *testing.T) {
	// Use unstyled styles so we can verify the string content
	noStyle := lipgloss.NewStyle()

	tests := []struct {
		s     string
		query string
		want  string
	}{
		{"my-session", "ses", "my-session"},  // match present
		{"my-session", "xyz", "my-session"},  // no match
		{"My-Session", "my-s", "My-Session"}, // case insensitive
		{"frontend", "front", "frontend"},    // match at start
		{"backend", "end", "backend"},        // match at end
	}
	for _, tt := range tests {
		got := highlightMatch(tt.s, tt.query, noStyle, noStyle)
		// Strip any ANSI sequences for comparison since unstyled lipgloss
		// may still produce reset sequences
		plain := stripStyleCodes(got)
		if plain != tt.want {
			t.Errorf("highlightMatch(%q, %q) plain = %q, want %q", tt.s, tt.query, plain, tt.want)
		}
	}
}

func TestHighlightMatch_ContainsQuery(t *testing.T) {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	hl := lipgloss.NewStyle().Bold(true)

	result := highlightMatch("my-session", "ses", base, hl)
	// The highlighted portion should be present unsplit
	if !strings.Contains(result, "ses") {
		t.Errorf("expected highlighted result to contain 'ses', got %q", result)
	}
}

func TestPadRight(t *testing.T) {
	tests := []struct {
		s     string
		width int
		want  string
	}{
		{"hi", 5, "hi   "},
		{"hello", 5, "hello"},
		{"toolong", 3, "toolong"}, // doesn't truncate
		{"", 3, "   "},
	}
	for _, tt := range tests {
		got := padRight(tt.s, tt.width)
		if got != tt.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
		}
	}
}

func TestPadWidthUnicode(t *testing.T) {
	left := padLeft("界", 4)
	if got := lipgloss.Width(left); got != 4 {
		t.Fatalf("padLeft unicode width=%d want 4 (%q)", got, left)
	}
	right := padRight("界", 4)
	if got := lipgloss.Width(right); got != 4 {
		t.Fatalf("padRight unicode width=%d want 4 (%q)", got, right)
	}
}

func TestTruncateUnicodeWidth(t *testing.T) {
	got := truncate("你好世界", 5)
	if w := lipgloss.Width(got); w > 5 {
		t.Fatalf("truncate width=%d exceeds max: %q", w, got)
	}
}

func TestPreviewMsgIgnoresStaleSession(t *testing.T) {
	m := initialModel()
	m.sessions = []Session{{Name: "alpha"}, {Name: "beta"}}
	m.cursor = 1

	updated, _ := m.Update(previewMsg{name: "alpha", content: "stale"})
	got := updated.(Model)
	if got.preview != "" {
		t.Fatalf("stale preview should be ignored, got %q", got.preview)
	}

	updated, _ = got.Update(previewMsg{name: "beta", content: "fresh"})
	got = updated.(Model)
	if got.preview != "fresh" {
		t.Fatalf("current preview should be applied, got %q", got.preview)
	}
}

func TestPreviewMsgAppliesForCurrentSessionDuringResize(t *testing.T) {
	m := initialModel()
	m.sessions = []Session{{Name: "alpha"}}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := updated.(Model)

	updated, _ = got.Update(previewMsg{name: "alpha", content: "ok"})
	got = updated.(Model)
	if got.preview != "ok" {
		t.Fatalf("preview should be updated for current session, got %q", got.preview)
	}
}

func TestVisibleSessionsInvalidatesAfterFilterAndSortChange(t *testing.T) {
	m := initialModel()
	m.sessions = []Session{
		{Name: "beta", PID: "2"},
		{Name: "alpha", PID: "1"},
	}
	m.markSessionsChanged()

	visible := m.visibleSessions()
	if len(visible) != 2 || visible[0].Name != "alpha" {
		t.Fatalf("unexpected initial ordering: %+v", visible)
	}

	m.filterText = "bet"
	m.markVisibleChanged()
	visible = m.visibleSessions()
	if len(visible) != 1 || visible[0].Name != "beta" {
		t.Fatalf("filter invalidation failed: %+v", visible)
	}

	m.filterText = ""
	m.sortAsc = false
	m.markVisibleChanged()
	visible = m.visibleSessions()
	if len(visible) != 2 || visible[0].Name != "beta" {
		t.Fatalf("sort invalidation failed: %+v", visible)
	}
}

func TestActionTargetsUsesSelectedSessions(t *testing.T) {
	m := initialModel()
	m.sessions = []Session{{Name: "alpha"}, {Name: "beta"}}
	m.selected["beta"] = true
	m.selected["alpha"] = true

	got := m.actionTargets()
	slices.Sort(got)
	want := []string{"alpha", "beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("actionTargets = %#v, want %#v", got, want)
	}
}

func TestActionTargetsFallsBackToCursor(t *testing.T) {
	m := initialModel()
	m.sessions = []Session{{Name: "alpha"}, {Name: "beta"}}
	m.cursor = 1

	got := m.actionTargets()
	want := []string{"beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("actionTargets = %#v, want %#v", got, want)
	}
}

func TestToggleSelectAllSelectsAndClearsVisible(t *testing.T) {
	m := initialModel()
	visible := []Session{{Name: "alpha"}, {Name: "beta"}}

	m.toggleSelectAll(visible)
	if !m.selected["alpha"] || !m.selected["beta"] {
		t.Fatalf("expected both sessions selected, got %#v", m.selected)
	}

	m.toggleSelectAll(visible)
	if len(m.selected) != 0 {
		t.Fatalf("expected visible selections cleared, got %#v", m.selected)
	}
}

func TestRenderHelpIncludesDetachAction(t *testing.T) {
	m := initialModel()
	m.width = 200

	plain := stripStyleCodes(m.renderHelp())
	if !strings.Contains(plain, "d detach") {
		t.Fatalf("renderHelp() = %q, want detach action", plain)
	}
	if !strings.Contains(plain, "^r refresh") {
		t.Fatalf("renderHelp() = %q, want refresh action", plain)
	}
}

func TestNewModelDefaultsToDefaultKeymap(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	if m.keymap() != KeymapDefault {
		t.Fatalf("keymap = %v, want default", m.keymap())
	}
	if m.inlineFilterEnabled() {
		t.Fatal("default keymap should not enable inline palette filtering")
	}
}

func TestPaletteKeymapEnablesInlineFilteringInAllLayouts(t *testing.T) {
	full := NewModel(Options{Mode: ModeFull, Keymap: KeymapPalette})
	if !full.inlineFilterEnabled() {
		t.Fatal("full layout should honor palette keymap behavior")
	}

	simplified := NewModel(Options{Mode: ModeSimplified, Keymap: KeymapPalette})
	if !simplified.inlineFilterEnabled() {
		t.Fatal("simplified layout should honor palette keymap behavior")
	}
}

func TestSimplifiedViewOmitsPreviewAndLog(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	m.width = 100
	m.height = 24
	m.sessions = []Session{{Name: "alpha", PID: "1"}}
	m.markSessionsChanged()

	view := stripStyleCodes(m.View().Content)
	if strings.Contains(view, "Preview") {
		t.Fatalf("simplified view should not render preview pane: %q", view)
	}
	if strings.Contains(view, "Activity Log") {
		t.Fatalf("simplified view should not render log pane: %q", view)
	}
	if !strings.Contains(view, "sessions") {
		t.Fatalf("simplified view missing sessions title: %q", view)
	}
}

func TestSimplifiedPaletteTypingFiltersSessions(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified, Keymap: KeymapPalette})
	m.sessions = []Session{{Name: "alpha"}, {Name: "beta"}}
	m.markSessionsChanged()

	updated, _ := m.Update(tea.KeyPressMsg{Text: "b"})
	got := updated.(Model)
	if got.filterText != "b" {
		t.Fatalf("filterText = %q, want %q", got.filterText, "b")
	}
	visible := got.visibleSessions()
	if len(visible) != 1 || visible[0].Name != "beta" {
		t.Fatalf("visible sessions = %+v, want only beta", visible)
	}
}

func TestAttachKeySetsAttachTarget(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	m.sessions = []Session{{Name: "alpha"}}
	m.markSessionsChanged()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.AttachTarget() != "alpha" {
		t.Fatalf("AttachTarget() = %q, want %q", got.AttachTarget(), "alpha")
	}
}

func TestPaletteKeymapHelpShowsCtrlBindings(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified, Keymap: KeymapPalette})
	m.width = 200

	plain := stripStyleCodes(m.renderHelp())
	if !strings.Contains(plain, "^d detach") || !strings.Contains(plain, "^x kill") || !strings.Contains(plain, "^o layout") {
		t.Fatalf("renderHelp() = %q, want palette ctrl bindings", plain)
	}
}

func TestToggleLayoutSwitchesModes(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	got := updated.(Model)
	if got.Options().Mode != ModeFull {
		t.Fatalf("Mode = %v, want full after toggle", got.Options().Mode)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	got = updated.(Model)
	if got.Options().Mode != ModeSimplified {
		t.Fatalf("Mode = %v, want simplified after second toggle", got.Options().Mode)
	}
}

func TestSimplifiedViewCanHideHelp(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified, ShowHelp: false, ShowHelpSet: true})
	m.width = 100
	m.height = 24
	m.sessions = []Session{{Name: "alpha", PID: "1"}}
	m.markSessionsChanged()

	view := stripStyleCodes(m.View().Content)
	if strings.Contains(view, "attach") || strings.Contains(view, "detach") {
		t.Fatalf("simplified view should hide help block: %q", view)
	}
}

func TestSimplifiedOuterWidthIsCompact(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	m.width = 160
	m.sessions = []Session{{Name: "alpha", PID: "1"}}
	m.markSessionsChanged()

	got := m.simplifiedOuterWidth()
	if got > paletteMaxWidth {
		t.Fatalf("simplifiedOuterWidth() = %d, want <= %d", got, paletteMaxWidth)
	}
	if got < paletteMinWidth {
		t.Fatalf("simplifiedOuterWidth() = %d, want >= %d", got, paletteMinWidth)
	}
}

func TestSimplifiedRowsHeightCapsVisibleRows(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	for i := 0; i < 20; i++ {
		m.sessions = append(m.sessions, Session{Name: "s"})
	}
	m.markSessionsChanged()

	if got := m.simplifiedRowsHeight(); got != paletteMaxRows {
		t.Fatalf("simplifiedRowsHeight() = %d, want %d", got, paletteMaxRows)
	}
}

func TestSimplifiedHelpWrapsToPickerWidth(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified, Keymap: KeymapPalette})
	m.width = 160
	m.sessions = []Session{{Name: "alpha", PID: "1"}}
	m.markSessionsChanged()

	outerWidth := m.simplifiedOuterWidth()
	helpModel := m
	helpModel.width = outerWidth
	help := lipgloss.NewStyle().Width(outerWidth).Render(helpModel.renderHelp())
	for _, line := range strings.Split(stripStyleCodes(help), "\n") {
		if lipgloss.Width(line) > outerWidth {
			t.Fatalf("help line width = %d, want <= %d: %q", lipgloss.Width(line), outerWidth, line)
		}
	}
}

func TestInitSchedulesAutoRefresh(t *testing.T) {
	m := NewModel()
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() should schedule startup work and auto-refresh")
	}
}

func TestAutoRefreshReschedules(t *testing.T) {
	m := NewModel()
	updated, cmd := m.Update(autoRefreshMsg{})
	if _, ok := updated.(Model); !ok {
		t.Fatal("Update(autoRefreshMsg) should keep model type")
	}
	if cmd == nil {
		t.Fatal("auto refresh should reschedule itself")
	}
}

func TestSessionsRefreshPreservesSelectedAgentMetadata(t *testing.T) {
	m := NewModel(Options{Mode: ModeFull})
	m.width = 120
	m.height = 30
	m.sessions = []Session{{
		Name:         "alpha",
		PID:          "1",
		StartedIn:    "/tmp",
		AgentKind:    "claude",
		AgentState:   "done",
		AgentSummary: "Here are the files",
		AgentUpdated: 1,
		AgentModel:   "claude-opus-4-6",
	}}
	m.markSessionsChanged()

	updated, _ := m.Update(sessionsMsg{sessions: []Session{{
		Name:      "alpha",
		PID:       "1",
		StartedIn: "/tmp",
	}}})
	got := updated.(Model)
	if got.sessions[0].AgentKind != "claude" {
		t.Fatalf("AgentKind = %q, want preserved claude", got.sessions[0].AgentKind)
	}
	if got.sessions[0].AgentSummary != "Here are the files" {
		t.Fatalf("AgentSummary = %q, want preserved summary", got.sessions[0].AgentSummary)
	}

	view := stripStyleCodes(got.View().Content)
	if !strings.Contains(view, "claude") || !strings.Contains(view, "Here are the files") {
		t.Fatalf("full view should keep agent pane during sessions refresh: %q", view)
	}
}

func TestSimplifiedViewShowsSelectedAgentStatus(t *testing.T) {
	m := NewModel(Options{Mode: ModeSimplified})
	m.width = 100
	m.height = 24
	m.sessions = []Session{{
		Name:         "alpha",
		PID:          "1",
		AgentKind:    "codex",
		AgentState:   "working",
		AgentSummary: "exec: make test",
		AgentUpdated: 1,
	}}
	m.markSessionsChanged()

	view := stripStyleCodes(m.View().Content)
	if !strings.Contains(view, "codex") || !strings.Contains(view, "exec: make test") {
		t.Fatalf("simplified view missing agent status: %q", view)
	}
}

func TestFullViewShowsSelectedAgentStatus(t *testing.T) {
	m := NewModel(Options{Mode: ModeFull})
	m.width = 120
	m.height = 30
	m.sessions = []Session{{
		Name:         "alpha",
		PID:          "1",
		AgentKind:    "claude",
		AgentState:   "done",
		AgentSummary: "Here are the files",
		AgentUpdated: 1,
		AgentModel:   "claude-opus-4-6",
		AgentVersion: "2.1.79",
		AgentPrompt:  "list all files",
		AgentInput:   10,
		AgentOutput:  20,
		AgentCached:  30,
		AgentTotal:   60,
	}}
	m.preview = "preview"
	m.markSessionsChanged()

	view := stripStyleCodes(m.View().Content)
	if !strings.Contains(view, "claude") || !strings.Contains(view, "Here are the files") {
		t.Fatalf("full view missing agent status: %q", view)
	}
	if !strings.Contains(view, "last prompt") || !strings.Contains(view, "list all files") || !strings.Contains(view, "tokens: 60 total") {
		t.Fatalf("full view missing agent preview details: %q", view)
	}
}

// stripStyleCodes removes ANSI escape sequences for test comparison.
func stripStyleCodes(s string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(s, "")
}
