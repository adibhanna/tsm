package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/adibhanna/tsm/internal/appconfig"
	"github.com/adibhanna/tsm/internal/session"
	"github.com/adibhanna/tsm/internal/tui"
)

func TestSuggestSessionNameUsesCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.DefaultConfig()
	name, err := suggestSessionName(cfg, nil)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name != "demo" {
		t.Fatalf("name = %q, want demo", name)
	}
}

func TestSuggestSessionNameAddsSuffixForCollisions(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.DefaultConfig()
	sessions := []session.Session{{Name: "demo"}, {Name: "demo-2"}}
	name, err := suggestSessionName(cfg, sessions)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name != "demo-3" {
		t.Fatalf("name = %q, want demo-3", name)
	}
}

func TestSuggestSessionNameSkipsExistingSocketPathConflicts(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.Config{SocketDir: dir}
	name, err := suggestSessionName(cfg, nil)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name == "demo" {
		t.Fatalf("name = %q, want conflict-avoiding variant", name)
	}
	if !strings.HasSuffix(name, "-2") {
		t.Fatalf("name = %q, want suffix -2", name)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	if got := sanitizeSessionName("  my project\tname "); got != "my-project-name" {
		t.Fatalf("sanitizeSessionName = %q", got)
	}
}

func TestTruncateSessionName(t *testing.T) {
	if got := truncateSessionName("abcdefgh", 5); got != "abcde" {
		t.Fatalf("truncateSessionName = %q, want abcde", got)
	}
}

func TestResolveDetachTargetUsesCurrentSessionEnv(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	if got := resolveDetachTarget([]string{"tsm", "detach"}); got != "demo" {
		t.Fatalf("resolveDetachTarget = %q, want demo", got)
	}
}

func TestResolveDetachTargetPrefersExplicitName(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	if got := resolveDetachTarget([]string{"tsm", "detach", "other"}); got != "other" {
		t.Fatalf("resolveDetachTarget = %q, want other", got)
	}
}

func TestResolveKillTargetsUsesCurrentSessionEnv(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	got := resolveKillTargets([]string{"tsm", "kill"})
	if len(got) != 1 || got[0] != "demo" {
		t.Fatalf("resolveKillTargets = %#v, want [demo]", got)
	}
}

func TestResolveKillTargetsPrefersExplicitNames(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	got := resolveKillTargets([]string{"tsm", "kill", "one", "two"})
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("resolveKillTargets = %#v, want [one two]", got)
	}
}

func TestResolveTUIOptionsSimplifiedFlag(t *testing.T) {
	opts, err := resolveTUIOptions([]string{"--simplified"}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("resolveTUIOptions: %v", err)
	}
	if opts.Mode != tui.ModeSimplified {
		t.Fatalf("Mode = %v, want simplified", opts.Mode)
	}
	if opts.Keymap != tui.KeymapDefault {
		t.Fatalf("Keymap = %v, want default", opts.Keymap)
	}
}

func TestResolveTUIOptionsUsesGlobalEnv(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "TSM_TUI_MODE":
			return "simplified"
		case "TSM_TUI_KEYMAP":
			return "palette"
		default:
			return ""
		}
	}

	opts, err := resolveTUIOptions(nil, env)
	if err != nil {
		t.Fatalf("resolveTUIOptions: %v", err)
	}
	if opts.Mode != tui.ModeSimplified {
		t.Fatalf("Mode = %v, want simplified", opts.Mode)
	}
	if opts.Keymap != tui.KeymapPalette {
		t.Fatalf("Keymap = %v, want palette", opts.Keymap)
	}
}

func TestResolveTUIOptionsUsesConfigDefaults(t *testing.T) {
	showHelp := false
	cfg := appconfig.Config{
		TUI: appconfig.TUIConfig{
			Mode:     "simplified",
			Keymap:   "palette",
			ShowHelp: &showHelp,
		},
	}

	opts, err := resolveTUIOptionsWithConfig(nil, func(string) string { return "" }, cfg)
	if err != nil {
		t.Fatalf("resolveTUIOptionsWithConfig: %v", err)
	}
	if opts.Mode != tui.ModeSimplified {
		t.Fatalf("Mode = %v, want simplified", opts.Mode)
	}
	if opts.Keymap != tui.KeymapPalette {
		t.Fatalf("Keymap = %v, want palette", opts.Keymap)
	}
	if opts.ShowHelp {
		t.Fatal("ShowHelp = true, want false from config")
	}
}

func TestResolveTUIOptionsExplicitKeymapOverridesEnv(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "TSM_TUI_MODE":
			return "full"
		case "TSM_TUI_KEYMAP":
			return "default"
		default:
			return ""
		}
	}

	opts, err := resolveTUIOptions([]string{"--keymap=palette"}, env)
	if err != nil {
		t.Fatalf("resolveTUIOptions: %v", err)
	}
	if opts.Mode != tui.ModeFull {
		t.Fatalf("Mode = %v, want full", opts.Mode)
	}
	if opts.Keymap != tui.KeymapPalette {
		t.Fatalf("Keymap = %v, want palette", opts.Keymap)
	}
}

func TestResolveTUIOptionsAppliesConfigBindingsForSelectedKeymap(t *testing.T) {
	cfg := appconfig.Config{
		TUI: appconfig.TUIConfig{
			Keymap: "default",
			Keymaps: map[string]map[string][]string{
				"default": {
					"detach": []string{"x"},
				},
				"palette": {},
			},
		},
	}

	opts, err := resolveTUIOptionsWithConfig(nil, func(string) string { return "" }, cfg)
	if err != nil {
		t.Fatalf("resolveTUIOptionsWithConfig: %v", err)
	}
	msg := tea.KeyPressMsg{Text: "x"}
	if !opts.Bindings.Matches(tui.ActionDetach, msg) {
		t.Fatal("expected config detach override to be applied")
	}
}

func TestResolveTUIOptionsCLIKeymapSelectsMatchingConfigOverrides(t *testing.T) {
	cfg := appconfig.Config{
		TUI: appconfig.TUIConfig{
			Keymap: "default",
			Keymaps: map[string]map[string][]string{
				"default": {
					"detach": []string{"x"},
				},
				"palette": {
					"detach": []string{"ctrl+k"},
				},
			},
		},
	}

	opts, err := resolveTUIOptionsWithConfig([]string{"--keymap=palette"}, func(string) string { return "" }, cfg)
	if err != nil {
		t.Fatalf("resolveTUIOptionsWithConfig: %v", err)
	}
	if !opts.Bindings.Matches(tui.ActionDetach, tea.KeyPressMsg{Text: "k", Mod: tea.ModCtrl}) {
		t.Fatal("expected palette detach override to be applied")
	}
	if opts.Bindings.Matches(tui.ActionDetach, tea.KeyPressMsg{Text: "x"}) {
		t.Fatal("unexpected default-keymap detach override leaked into palette bindings")
	}
}

func TestVersionStringForDevBuild(t *testing.T) {
	prevVersion, prevCommit, prevDate, prevDirty := version, commit, date, dirty
	t.Cleanup(func() {
		version, commit, date, dirty = prevVersion, prevCommit, prevDate, prevDirty
	})

	version = "dev"
	commit = "9d35718"
	date = "2026-03-19T15:28:20Z"
	dirty = "true"

	got := versionString("libghostty-vt")
	want := "tsm dev (commit 9d35718, dirty, built 2026-03-19T15:28:20Z) backend=libghostty-vt"
	if got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}

func TestVersionStringForReleaseBuild(t *testing.T) {
	prevVersion, prevCommit, prevDate, prevDirty := version, commit, date, dirty
	t.Cleanup(func() {
		version, commit, date, dirty = prevVersion, prevCommit, prevDate, prevDirty
	})

	version = "v0.4.0"
	commit = "9d35718"
	date = "2026-03-19T15:28:20Z"
	dirty = "false"

	got := versionString("libghostty-vt")
	want := "tsm v0.4.0 (commit 9d35718, built 2026-03-19T15:28:20Z) backend=libghostty-vt"
	if got != want {
		t.Fatalf("versionString() = %q, want %q", got, want)
	}
}
