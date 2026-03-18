package session

import (
	"testing"
	"time"
)

func TestDaemonStartAndProbe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZMX_DIR", dir)

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
