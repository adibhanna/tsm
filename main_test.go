package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	t.Chdir(workdir)

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
	t.Chdir(workdir)

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
	t.Chdir(workdir)

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

func TestMarkSessionFocusedTracksCurrentAndPrevious(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}
	if err := markSessionFocused(cfg, "alpha", ""); err != nil {
		t.Fatalf("markSessionFocused alpha: %v", err)
	}
	if err := markSessionFocused(cfg, "beta", ""); err != nil {
		t.Fatalf("markSessionFocused beta: %v", err)
	}

	state, err := loadFocusState(cfg)
	if err != nil {
		t.Fatalf("loadFocusState: %v", err)
	}
	if state.Current != "beta" || state.Previous != "alpha" {
		t.Fatalf("state = %+v, want current beta previous alpha", state)
	}
}

func TestResolveToggleTargetUsesPreviousWhenInsideCurrent(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}
	if err := saveFocusState(cfg, focusState{Current: "beta", Previous: "alpha"}); err != nil {
		t.Fatalf("saveFocusState: %v", err)
	}

	origList := listSessionsForFocus
	t.Cleanup(func() { listSessionsForFocus = origList })
	listSessionsForFocus = func(session.Config) ([]session.Session, error) {
		return []session.Session{{Name: "alpha"}, {Name: "beta"}}, nil
	}

	target, err := resolveToggleTarget(cfg, "beta")
	if err != nil {
		t.Fatalf("resolveToggleTarget: %v", err)
	}
	if target != "alpha" {
		t.Fatalf("target = %q, want alpha", target)
	}
}

func TestRemoveFocusSessionDropsKilledSession(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}
	if err := saveFocusState(cfg, focusState{Current: "beta", Previous: "alpha"}); err != nil {
		t.Fatalf("saveFocusState: %v", err)
	}
	if err := removeFocusSession(cfg, "beta"); err != nil {
		t.Fatalf("removeFocusSession: %v", err)
	}
	state, err := loadFocusState(cfg)
	if err != nil {
		t.Fatalf("loadFocusState: %v", err)
	}
	if state.Current != "alpha" || state.Previous != "" {
		t.Fatalf("state = %+v, want current alpha previous empty", state)
	}
}

func TestLoadFocusStateHandlesEmptyFile(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}
	if err := os.WriteFile(focusStatePath(cfg), nil, 0o640); err != nil {
		t.Fatalf("write focus file: %v", err)
	}
	state, err := loadFocusState(cfg)
	if err != nil {
		t.Fatalf("loadFocusState: %v", err)
	}
	if state != (focusState{}) {
		t.Fatalf("state = %+v, want zero value", state)
	}
}

func TestSaveFocusStateWritesJSON(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}
	want := focusState{Current: "beta", Previous: "alpha"}
	if err := saveFocusState(cfg, want); err != nil {
		t.Fatalf("saveFocusState: %v", err)
	}
	data, err := os.ReadFile(focusStatePath(cfg))
	if err != nil {
		t.Fatalf("read focus file: %v", err)
	}
	var got focusState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal focus json: %v", err)
	}
	if got != want {
		t.Fatalf("focus json = %+v, want %+v", got, want)
	}
}

func TestResolveTUIOptionsSimplifiedFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing.toml")
	opts, err := resolveTUIOptions([]string{"--simplified"}, func(key string) string {
		if key == appconfig.DefaultConfigPathEnv {
			return configPath
		}
		return ""
	})
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
	configPath := filepath.Join(t.TempDir(), "missing.toml")
	env := func(key string) string {
		switch key {
		case appconfig.DefaultConfigPathEnv:
			return configPath
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
	configPath := filepath.Join(t.TempDir(), "missing.toml")
	env := func(key string) string {
		switch key {
		case appconfig.DefaultConfigPathEnv:
			return configPath
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

func TestResolveTUIOptionsRejectsBindingConflicts(t *testing.T) {
	cfg := appconfig.Config{
		TUI: appconfig.TUIConfig{
			Keymaps: map[string]map[string][]string{
				"default": {
					"attach": []string{"enter"},
					"detach": []string{"enter"},
				},
				"palette": {},
			},
		},
	}

	_, err := resolveTUIOptionsWithConfig(nil, func(string) string { return "" }, cfg)
	if err == nil || !strings.Contains(err.Error(), `binding "enter" conflicts between attach and detach`) {
		t.Fatalf("resolveTUIOptionsWithConfig() error = %v, want binding conflict error", err)
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

func TestFormatClaudeStatusline(t *testing.T) {
	got := formatClaudeStatusline([]byte(`{
		"model":{"display_name":"Opus"},
		"workspace":{"current_dir":"/Users/test/work"},
		"context_window":{"used_percentage":8},
		"cost":{"total_cost_usd":0.01234}
	}`))
	want := "[Opus]  work  8% context  $0.01"
	if got != want {
		t.Fatalf("formatClaudeStatusline() = %q, want %q", got, want)
	}
}

func TestFormatSessionActionErrorNotFound(t *testing.T) {
	msg := formatSessionActionError("attach", "demo", fmt.Errorf("%w: %q", session.ErrSessionNotFound, "demo"))
	if !strings.Contains(msg, `Cannot attach session "demo": session not found.`) {
		t.Fatalf("message missing not-found summary: %q", msg)
	}
	if !strings.Contains(msg, "Run 'tsm ls' to list sessions.") {
		t.Fatalf("message missing next step: %q", msg)
	}
}

func TestFormatSessionActionErrorConnectionRefused(t *testing.T) {
	msg := formatSessionActionError("kill", "demo", &net.OpError{Err: syscall.ECONNREFUSED})
	if !strings.Contains(msg, `Cannot kill session "demo": the session socket exists but the daemon is not responding.`) {
		t.Fatalf("message missing stale summary: %q", msg)
	}
	if !strings.Contains(msg, "tsm doctor clean-stale") {
		t.Fatalf("message missing clean-stale guidance: %q", msg)
	}
}

func TestFormatSessionActionErrorTimeout(t *testing.T) {
	msg := formatSessionActionError("detach", "demo", timeoutError{})
	if !strings.Contains(msg, `Cannot detach session "demo": the daemon timed out.`) {
		t.Fatalf("message missing timeout summary: %q", msg)
	}
	if !strings.Contains(msg, "tsm debug session demo") {
		t.Fatalf("message missing debug guidance: %q", msg)
	}
}

func TestDaemonBuildWarningForOlderSessionBuild(t *testing.T) {
	cfg := session.Config{LogDir: t.TempDir()}
	dir := filepath.Join(cfg.LogDir, "daemon-build")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	current, err := session.CurrentBuildInfo()
	if err != nil {
		t.Fatalf("CurrentBuildInfo: %v", err)
	}
	data, err := json.Marshal(session.DaemonBuildInfo{
		Executable:  current.Executable,
		ModTimeUnix: current.ModTimeUnix - 60,
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got := daemonBuildWarning(cfg, "demo")
	if !strings.Contains(got, `session "demo" is running an older tsm daemon build`) {
		t.Fatalf("daemonBuildWarning() = %q", got)
	}
}

func TestDaemonBuildWarningEmptyWhenBuildMatches(t *testing.T) {
	cfg := session.Config{LogDir: t.TempDir()}
	dir := filepath.Join(cfg.LogDir, "daemon-build")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	current, err := session.CurrentBuildInfo()
	if err != nil {
		t.Fatalf("CurrentBuildInfo: %v", err)
	}
	data, err := json.Marshal(current)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := daemonBuildWarning(cfg, "demo"); got != "" {
		t.Fatalf("daemonBuildWarning() = %q, want empty", got)
	}
}

func TestDoctorReportNoSessions(t *testing.T) {
	socketDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorExecutable = func() (string, error) { return "/tmp/tsm", nil }
	doctorLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	doctorGhosttyStatus = func(backend string, pkgConfigErr error) string { return "ok" }

	report, err := doctorReport(os.Getenv)
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}

	if !strings.Contains(report, "tsm doctor\n") {
		t.Fatalf("report missing header: %q", report)
	}
	if !strings.Contains(report, "libghostty-vt: ok") {
		t.Fatalf("report missing ghostty status: %q", report)
	}
	if !strings.Contains(report, "sessions:\n  none") {
		t.Fatalf("report missing empty sessions section: %q", report)
	}
	if !strings.Contains(report, "artifacts:\n  none") {
		t.Fatalf("report missing empty artifacts section: %q", report)
	}
	if !strings.Contains(report, filepath.Join(configHome, "tsm", "config.toml")+" (missing)") {
		t.Fatalf("report missing config path/state: %q", report)
	}
}

func TestDoctorReportLiveAndStaleSockets(t *testing.T) {
	socketDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	for _, name := range []string{"live", "stale"} {
		if err := os.WriteFile(filepath.Join(socketDir, name), nil, 0o600); err != nil {
			t.Fatalf("write fake socket %s: %v", name, err)
		}
	}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorExecutable = func() (string, error) { return "/tmp/tsm", nil }
	doctorLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	doctorGhosttyStatus = func(backend string, pkgConfigErr error) string { return "ok" }
	doctorIsSocket = func(path string) bool { return true }
	doctorProbe = func(path string) (*session.InfoPayload, error) {
		if strings.HasSuffix(path, "/live") {
			info := &session.InfoPayload{
				ClientsLen: 2,
				PID:        1234,
				CmdLen:     4,
				CwdLen:     5,
			}
			copy(info.Cmd[:], "zsh ")
			copy(info.Cwd[:], "/repo")
			return info, nil
		}
		return nil, errors.New("connect: connection refused")
	}

	report, err := doctorReport(os.Getenv)
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}

	if !strings.Contains(report, `- live: live pid=1234 clients=2 cmd="zsh " dir="/repo"`) {
		t.Fatalf("report missing live session details: %q", report)
	}
	if !strings.Contains(report, `- stale: stale (connect: connection refused)`) {
		t.Fatalf("report missing stale session details: %q", report)
	}
}

func TestDoctorReportFlagsOlderDaemonBuild(t *testing.T) {
	socketDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	cfg := session.DefaultConfig()
	if err := os.WriteFile(cfg.SocketPath("live"), nil, 0o600); err != nil {
		t.Fatalf("write fake socket: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(session.ClaudeStatuslinePath(cfg, "live")), 0o750); err != nil {
		t.Fatalf("mkdir sidecar dir: %v", err)
	}
	infoPath := filepath.Join(cfg.LogDir, "daemon-build", "live.json")
	if err := os.MkdirAll(filepath.Dir(infoPath), 0o750); err != nil {
		t.Fatalf("mkdir build dir: %v", err)
	}
	if err := os.WriteFile(infoPath, []byte(`{"executable":"/old/tsm","mod_time_unix":1}`), 0o644); err != nil {
		t.Fatalf("write daemon build info: %v", err)
	}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorExecutable = func() (string, error) { return "/tmp/tsm", nil }
	doctorLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	doctorGhosttyStatus = func(backend string, pkgConfigErr error) string { return "ok" }
	doctorIsSocket = func(path string) bool { return strings.HasSuffix(path, "/live") }
	doctorProbe = func(path string) (*session.InfoPayload, error) {
		return &session.InfoPayload{}, nil
	}

	report, err := doctorReport(os.Getenv)
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	if !strings.Contains(report, `[older daemon build]`) {
		t.Fatalf("report missing build mismatch marker: %q", report)
	}
}

func TestDoctorReportOrphanedArtifacts(t *testing.T) {
	socketDir := t.TempDir()
	configHome := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)
	t.Setenv("XDG_CONFIG_HOME", configHome)

	cfg := session.DefaultConfig()
	logDir := cfg.LogDir
	for _, path := range []string{
		filepath.Join(logDir, "daemon-build", "orphan.json"),
		filepath.Join(logDir, "claude-statusline", "orphan.json"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir sidecar dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
	}
	if err := os.WriteFile(cfg.SocketPath("live"), nil, 0o600); err != nil {
		t.Fatalf("write fake socket: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(session.ClaudeStatuslinePath(cfg, "live")), 0o750); err != nil {
		t.Fatalf("mkdir live sidecar dir: %v", err)
	}
	if err := os.WriteFile(session.ClaudeStatuslinePath(cfg, "live"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write live sidecar: %v", err)
	}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorExecutable = func() (string, error) { return "/tmp/tsm", nil }
	doctorLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	doctorGhosttyStatus = func(backend string, pkgConfigErr error) string { return "ok" }
	doctorIsSocket = func(path string) bool { return strings.HasSuffix(path, "/live") }
	doctorProbe = func(path string) (*session.InfoPayload, error) {
		if strings.HasSuffix(path, "/live") {
			return &session.InfoPayload{}, nil
		}
		return nil, errors.New("connect: connection refused")
	}

	report, err := doctorReport(os.Getenv)
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	if !strings.Contains(report, "artifacts:\n  - orphan: orphaned claude-statusline, daemon-build") {
		t.Fatalf("report missing orphaned artifacts: %q", report)
	}
	if strings.Contains(report, "live: orphaned") {
		t.Fatalf("report should not mark live session artifacts as orphaned: %q", report)
	}
}

func TestDoctorReportMissingPkgConfig(t *testing.T) {
	if session.RestoreBackendName() == "stub" {
		t.Skip("requires cgo (libghostty-vt)")
	}
	socketDir := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorExecutable = func() (string, error) { return "/tmp/tsm", nil }
	doctorLookPath = func(file string) (string, error) { return "", errors.New("not found") }
	doctorGhosttyStatus = detectGhosttyStatus

	report, err := doctorReport(os.Getenv)
	if err != nil {
		t.Fatalf("doctorReport: %v", err)
	}
	if !strings.Contains(report, "pkg-config: missing") {
		t.Fatalf("report missing pkg-config error: %q", report)
	}
	if !strings.Contains(report, "libghostty-vt: loaded (pkg-config not found)") {
		t.Fatalf("report missing libghostty-vt fallback status: %q", report)
	}
}

func TestDetectGhosttyStatusLoadedWithoutPkgConfig(t *testing.T) {
	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorRunCommand = func(name string, args ...string) error { return errors.New("missing") }

	if got := detectGhosttyStatus("libghostty-vt", errors.New("not found")); got != "loaded (pkg-config not found)" {
		t.Fatalf("detectGhosttyStatus() = %q", got)
	}
	if got := detectGhosttyStatus("libghostty-vt", nil); got != "loaded (pkg-config not configured)" {
		t.Fatalf("detectGhosttyStatus() = %q", got)
	}
	if got := detectGhosttyStatus("other", nil); got != "missing" {
		t.Fatalf("detectGhosttyStatus() = %q", got)
	}
}

func TestDebugSessionReportLive(t *testing.T) {
	socketDir := t.TempDir()
	cfg := session.Config{SocketDir: socketDir}
	path := cfg.SocketPath("demo")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write fake socket: %v", err)
	}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorIsSocket = func(got string) bool { return got == path }
	doctorProbe = func(got string) (*session.InfoPayload, error) {
		info := &session.InfoPayload{
			ClientsLen:   1,
			PID:          4242,
			CmdLen:       4,
			CwdLen:       5,
			CreatedAt:    1700000000,
			TaskEndedAt:  1700000100,
			TaskExitCode: 7,
		}
		copy(info.Cmd[:], "zsh ")
		copy(info.Cwd[:], "/repo")
		return info, nil
	}
	debugFetchPreview = func(name string, lines int) string {
		if name != "demo" || lines != 12 {
			t.Fatalf("unexpected preview request: %s %d", name, lines)
		}
		return "hello\nworld"
	}

	report, healthy, err := debugSessionReport(cfg, "demo")
	if err != nil {
		t.Fatalf("debugSessionReport: %v", err)
	}
	if !healthy {
		t.Fatalf("expected healthy live session report: %q", report)
	}
	if !strings.Contains(report, "state: live") {
		t.Fatalf("report missing live state: %q", report)
	}
	if !strings.Contains(report, "pid: 4242") || !strings.Contains(report, "clients: 1") {
		t.Fatalf("report missing process details: %q", report)
	}
	if !strings.Contains(report, "preview:\nhello\nworld\n") {
		t.Fatalf("report missing preview: %q", report)
	}
}

func TestDebugSessionReportMissing(t *testing.T) {
	cfg := session.Config{SocketDir: t.TempDir()}

	report, healthy, err := debugSessionReport(cfg, "missing")
	if err != nil {
		t.Fatalf("debugSessionReport: %v", err)
	}
	if healthy {
		t.Fatalf("expected missing session to be unhealthy: %q", report)
	}
	if !strings.Contains(report, "state: missing") {
		t.Fatalf("report missing missing-state: %q", report)
	}
}

func TestDebugSessionReportStale(t *testing.T) {
	socketDir := t.TempDir()
	cfg := session.Config{SocketDir: socketDir}
	path := cfg.SocketPath("stale")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write fake socket: %v", err)
	}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorIsSocket = func(got string) bool { return got == path }
	doctorProbe = func(string) (*session.InfoPayload, error) {
		return nil, errors.New("connect: connection refused")
	}

	report, healthy, err := debugSessionReport(cfg, "stale")
	if err != nil {
		t.Fatalf("debugSessionReport: %v", err)
	}
	if healthy {
		t.Fatalf("expected stale session to be unhealthy: %q", report)
	}
	if !strings.Contains(report, "state: stale") || !strings.Contains(report, "connection refused") {
		t.Fatalf("report missing stale details: %q", report)
	}
}

func TestCleanStaleSocketsRemovesOnlyStaleEntries(t *testing.T) {
	socketDir := t.TempDir()
	cfg := session.Config{SocketDir: socketDir}

	restoreDoctorFns := stubDoctorFns()
	defer restoreDoctorFns()
	doctorReadDir = func(string) ([]os.DirEntry, error) {
		for _, name := range []string{"live", "stale"} {
			if err := os.WriteFile(filepath.Join(socketDir, name), nil, 0o600); err != nil {
				t.Fatalf("write fake socket %s: %v", name, err)
			}
		}
		return os.ReadDir(socketDir)
	}
	doctorIsSocket = func(path string) bool { return true }
	doctorProbe = func(path string) (*session.InfoPayload, error) {
		if strings.HasSuffix(path, "/live") {
			return &session.InfoPayload{}, nil
		}
		return nil, errors.New("connection refused")
	}
	var removedPaths []string
	doctorCleanSocket = func(path string) error {
		removedPaths = append(removedPaths, path)
		return nil
	}

	removed, err := cleanStaleSockets(cfg)
	if err != nil {
		t.Fatalf("cleanStaleSockets: %v", err)
	}
	if len(removed) != 1 || removed[0] != "stale" {
		t.Fatalf("removed = %#v, want [stale]", removed)
	}
	if len(removedPaths) != 1 || removedPaths[0] != cfg.SocketPath("stale") {
		t.Fatalf("removedPaths = %#v, want stale socket path", removedPaths)
	}
}

func TestCleanStaleArtifactsRemovesOnlyOrphanedSidecars(t *testing.T) {
	socketDir := t.TempDir()
	t.Setenv("TSM_DIR", socketDir)
	cfg := session.DefaultConfig()

	for _, path := range []string{
		session.ClaudeStatuslinePath(cfg, "orphan"),
		filepath.Join(cfg.LogDir, "daemon-build", "orphan.json"),
		session.ClaudeStatuslinePath(cfg, "live"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir sidecar dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write sidecar: %v", err)
		}
	}
	if err := os.WriteFile(cfg.SocketPath("live"), nil, 0o600); err != nil {
		t.Fatalf("write fake live socket: %v", err)
	}

	removed, err := cleanStaleArtifacts(cfg)
	if err != nil {
		t.Fatalf("cleanStaleArtifacts: %v", err)
	}
	if len(removed) != 1 || removed[0].Name != "orphan" {
		t.Fatalf("removed = %#v, want orphan only", removed)
	}
	for _, path := range []string{
		session.ClaudeStatuslinePath(cfg, "orphan"),
		filepath.Join(cfg.LogDir, "daemon-build", "orphan.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected orphan sidecar %q removed, err=%v", path, err)
		}
	}
	if _, err := os.Stat(session.ClaudeStatuslinePath(cfg, "live")); err != nil {
		t.Fatalf("expected live sidecar to remain: %v", err)
	}
}

func stubDoctorFns() func() {
	prevExecutable := doctorExecutable
	prevLookPath := doctorLookPath
	prevRunCommand := doctorRunCommand
	prevReadDir := doctorReadDir
	prevProbe := doctorProbe
	prevIsSocket := doctorIsSocket
	prevGhosttyStatus := doctorGhosttyStatus
	prevCleanSocket := doctorCleanSocket
	prevFetchPreview := debugFetchPreview

	return func() {
		doctorExecutable = prevExecutable
		doctorLookPath = prevLookPath
		doctorRunCommand = prevRunCommand
		doctorReadDir = prevReadDir
		doctorProbe = prevProbe
		doctorIsSocket = prevIsSocket
		doctorGhosttyStatus = prevGhosttyStatus
		doctorCleanSocket = prevCleanSocket
		debugFetchPreview = prevFetchPreview
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
