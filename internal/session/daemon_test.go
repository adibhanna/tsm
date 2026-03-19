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
}
