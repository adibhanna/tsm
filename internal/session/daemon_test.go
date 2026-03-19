package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonStartAndProbe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TSM_DIR", dir)

	cfg := DefaultConfig()
	name := "test-daemon"

	// Start daemon in a goroutine (it blocks until the shell exits).
	errCh := make(chan error, 1)
	go func() {
		errCh <- StartDaemon(name, []string{"sleep", "5"})
	}()

	// Wait for socket to appear.
	sockPath := cfg.SocketPath(name)
	var found bool
	for range 30 {
		if IsSocket(sockPath) {
			found = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatalf("socket never appeared at %s", sockPath)
	}

	// Probe the session.
	info, err := ProbeSession(sockPath)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	t.Logf("PID=%d Clients=%d Cmd=%q Cwd=%q", info.PID, info.ClientsLen, info.CmdString(), info.CwdString())
	if info.PID <= 0 {
		t.Error("expected positive PID")
	}

	// List sessions.
	sessions, err := ListSessions(cfg)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Name != name {
		t.Errorf("session name = %q, want %q", sessions[0].Name, name)
	}

	// Kill the session.
	err = KillSession(cfg, name)
	if err != nil {
		t.Fatalf("KillSession: %v", err)
	}

	// Wait for daemon to exit.
	select {
	case err := <-errCh:
		if err != nil {
			t.Logf("daemon exited with: %v (expected after kill)", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("daemon didn't exit after kill")
	}
}

func TestBuildDaemonEnvAddsZshIntegration(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0750); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("TSM_DIR", dir)

	cfg := DefaultConfig()
	env, err := buildDaemonEnv(cfg, "demo", "/bin/zsh", nil)
	if err != nil {
		t.Fatalf("buildDaemonEnv: %v", err)
	}

	values := map[string]string{}
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		values[key] = value
	}

	if got := values["TSM_SESSION"]; got != "demo" {
		t.Fatalf("TSM_SESSION = %q, want demo", got)
	}
	if got := values["TSM_SHELL_INTEGRATION"]; got != "zsh" {
		t.Fatalf("TSM_SHELL_INTEGRATION = %q, want zsh", got)
	}
	if got := values["TSM_SESSION_FILE"]; got == "" {
		t.Fatal("expected TSM_SESSION_FILE to be set")
	}
	if got := values["TSM_ORIG_ZDOTDIR"]; got != home {
		t.Fatalf("TSM_ORIG_ZDOTDIR = %q, want %q", got, home)
	}

	zdotdir := values["ZDOTDIR"]
	if zdotdir == "" {
		t.Fatal("expected ZDOTDIR to be set")
	}
	if _, err := os.Stat(filepath.Join(zdotdir, ".zshrc")); err != nil {
		t.Fatalf("expected generated .zshrc: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(zdotdir, ".zshrc"))
	if err != nil {
		t.Fatalf("read generated .zshrc: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "bindkey '^P' _tsm_session_palette") {
		t.Fatalf("generated .zshrc missing Ctrl+P palette binding: %q", text)
	}
	if !strings.Contains(text, "_tsm_refresh_session_name") {
		t.Fatalf("generated .zshrc missing dynamic session refresh: %q", text)
	}
}

func TestBuildDaemonEnvAddsBashIntegration(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	env, err := buildDaemonEnv(cfg, "demo", "/bin/bash", nil)
	if err != nil {
		t.Fatalf("buildDaemonEnv: %v", err)
	}

	values := map[string]string{}
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		values[key] = value
	}

	if got := values["TSM_SHELL_INTEGRATION"]; got != "bash" {
		t.Fatalf("TSM_SHELL_INTEGRATION = %q, want bash", got)
	}
	rcfile := bashRcFilePath(cfg, "demo")
	content, err := os.ReadFile(rcfile)
	if err != nil {
		t.Fatalf("read generated bash rc: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `bind -x '"\C-p":"tsm p"'`) {
		t.Fatalf("generated bash rc missing Ctrl+P palette binding: %q", text)
	}
	if got := values["TSM_SESSION_FILE"]; got != sessionNameFilePath(cfg, "bash", "demo") {
		t.Fatalf("TSM_SESSION_FILE = %q, want %q", got, sessionNameFilePath(cfg, "bash", "demo"))
	}
}

func TestBuildDaemonEnvAddsFishIntegration(t *testing.T) {
	dir := t.TempDir()
	xdg := filepath.Join(dir, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "fish"), 0750); err != nil {
		t.Fatalf("mkdir xdg fish: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg := Config{SocketDir: t.TempDir()}
	env, err := buildDaemonEnv(cfg, "demo", "/opt/homebrew/bin/fish", nil)
	if err != nil {
		t.Fatalf("buildDaemonEnv: %v", err)
	}

	values := map[string]string{}
	for _, kv := range env {
		key, value, _ := strings.Cut(kv, "=")
		values[key] = value
	}

	if got := values["TSM_SHELL_INTEGRATION"]; got != "fish" {
		t.Fatalf("TSM_SHELL_INTEGRATION = %q, want fish", got)
	}
	if got := values["XDG_CONFIG_HOME"]; got != shellIntegrationDir(cfg, "fish", "demo") {
		t.Fatalf("XDG_CONFIG_HOME = %q, want %q", got, shellIntegrationDir(cfg, "fish", "demo"))
	}
	content, err := os.ReadFile(filepath.Join(shellIntegrationDir(cfg, "fish", "demo"), "fish", "config.fish"))
	if err != nil {
		t.Fatalf("read generated fish config: %v", err)
	}
	if !strings.Contains(string(content), `bind \cp __tsm_session_palette`) {
		t.Fatalf("generated fish config missing Ctrl+P palette binding: %q", string(content))
	}
}

func TestResolveShellArgsDefaultZshLoginShell(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	argv, err := resolveShellArgs(cfg, "demo", "/bin/zsh", nil)
	if err != nil {
		t.Fatalf("resolveShellArgs: %v", err)
	}
	if len(argv) != 1 || argv[0] != "-zsh" {
		t.Fatalf("argv = %#v, want [-zsh]", argv)
	}
}

func TestResolveShellArgsDefaultBashUsesRcfile(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	argv, err := resolveShellArgs(cfg, "demo", "/bin/bash", nil)
	if err != nil {
		t.Fatalf("resolveShellArgs: %v", err)
	}
	want := []string{"bash", "--rcfile", bashRcFilePath(cfg, "demo"), "-i"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
}

func TestResolveShellArgsDefaultFishIsInteractive(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	argv, err := resolveShellArgs(cfg, "demo", "/opt/homebrew/bin/fish", nil)
	if err != nil {
		t.Fatalf("resolveShellArgs: %v", err)
	}
	want := []string{"fish", "-i"}
	if strings.Join(argv, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("argv = %#v, want %#v", argv, want)
	}
}

func TestResolveShellArgsExplicitCommand(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	argv, err := resolveShellArgs(cfg, "demo", "nvim", []string{"nvim", "-u", "NONE"})
	if err != nil {
		t.Fatalf("resolveShellArgs: %v", err)
	}
	if len(argv) != 3 || argv[1] != "-u" || argv[2] != "NONE" {
		t.Fatalf("argv = %#v, want explicit command preserved", argv)
	}
}

func TestBuildDaemonEnvSkipsShellIntegrationForExplicitCommand(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir()}
	env, err := buildDaemonEnv(cfg, "demo", "/bin/zsh", []string{"nvim"})
	if err != nil {
		t.Fatalf("buildDaemonEnv: %v", err)
	}

	for _, kv := range env {
		if strings.HasPrefix(kv, "ZDOTDIR=") {
			t.Fatalf("unexpected ZDOTDIR for explicit command: %q", kv)
		}
		if strings.HasPrefix(kv, "TSM_SESSION_FILE=") {
			t.Fatalf("unexpected TSM_SESSION_FILE for explicit command: %q", kv)
		}
		if strings.HasPrefix(kv, "XDG_CONFIG_HOME=") {
			t.Fatalf("unexpected XDG_CONFIG_HOME for explicit command: %q", kv)
		}
		if strings.HasPrefix(kv, "TSM_ORIG_XDG_CONFIG_HOME=") {
			t.Fatalf("unexpected TSM_ORIG_XDG_CONFIG_HOME for explicit command: %q", kv)
		}
		if strings.HasPrefix(kv, "TSM_SHELL_INTEGRATION=") {
			t.Fatalf("unexpected TSM_SHELL_INTEGRATION for explicit command: %q", kv)
		}
	}
}
