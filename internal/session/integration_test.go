package session

import (
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// testConfig creates an isolated session config in a short temp directory.
// Uses /tmp directly to keep Unix socket paths under macOS's 104-byte sun_path limit
// (t.TempDir() paths are too long for sockets when combined with test names).
func testConfig(t *testing.T) Config {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tsm-t-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("TSM_DIR", dir)
	t.Setenv("TSM_CONFIG_FILE", dir+"/none.toml")
	return DefaultConfig()
}

// startTestDaemon starts a daemon in a goroutine and waits for its socket.
// Returns the error channel; the daemon writes to it exactly once on exit.
// The caller is responsible for killing the daemon (see killAndWait).
func startTestDaemon(t *testing.T, cfg Config, name string, cmd []string) chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- StartDaemon(name, cmd)
	}()

	sockPath := cfg.SocketPath(name)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if IsSocket(sockPath) {
			return errCh
		}
		select {
		case err := <-errCh:
			t.Fatalf("daemon %q exited before socket appeared: %v", name, err)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket never appeared at %s", sockPath)
	return errCh
}

// killAndWait sends a kill and waits for the daemon goroutine to finish.
// Safe to call on an already-dead daemon.
func killAndWait(cfg Config, name string, errCh chan error) {
	_ = KillSession(cfg, name)
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
	}
}

// initConn connects to a daemon socket and sends TagInit (simulates attach).
func initConn(t *testing.T, path string, rows, cols uint16) net.Conn {
	t.Helper()
	conn, err := Connect(path)
	if err != nil {
		t.Fatalf("connect %s: %v", path, err)
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], rows)
	binary.LittleEndian.PutUint16(payload[2:4], cols)
	if err := SendMessage(conn, TagInit, payload); err != nil {
		conn.Close()
		t.Fatalf("send init: %v", err)
	}
	return conn
}

// collectOutput reads TagOutput messages until timeout, returning all data.
func collectOutput(conn net.Conn, timeout time.Duration) []byte {
	deadline := time.Now().Add(timeout)
	var buf []byte
	for time.Now().Before(deadline) {
		tag, payload, err := ReadMessage(conn, 200*time.Millisecond)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			break
		}
		if tag == TagOutput {
			buf = append(buf, payload...)
		}
	}
	return buf
}

// waitForClients polls ProbeSession until the attached client count matches.
func waitForClients(t *testing.T, path string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last int
	for time.Now().Before(deadline) {
		info, err := ProbeSession(path)
		if err == nil {
			last = int(info.ClientsLen)
			if last == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wanted %d attached clients at %s, last observed %d", want, path, last)
}

// waitForSocketGone polls until the socket file disappears.
func waitForSocketGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsSocket(path) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket still exists at %s after %v", path, timeout)
}

// waitConnClosed waits until the connection is closed by the remote end.
func waitConnClosed(t *testing.T, conn net.Conn, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, _, err := ReadMessage(conn, 200*time.Millisecond)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return
		}
	}
	t.Fatal("connection not closed within timeout")
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestIntegrationAttachDetachReattach(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "lifecycle", []string{"sleep", "30"})
	defer killAndWait(cfg, "lifecycle", errCh)

	sockPath := cfg.SocketPath("lifecycle")

	// Attach.
	conn := initConn(t, sockPath, 24, 80)
	waitForClients(t, sockPath, 1, 2*time.Second)

	// Detach.
	if err := SendMessage(conn, TagDetach, nil); err != nil {
		t.Fatalf("send detach: %v", err)
	}
	conn.Close()
	waitForClients(t, sockPath, 0, 2*time.Second)

	// Daemon should still be alive.
	if !IsSocket(sockPath) {
		t.Fatal("socket gone after detach — daemon should still be running")
	}

	// Reattach.
	conn2 := initConn(t, sockPath, 24, 80)
	defer conn2.Close()
	waitForClients(t, sockPath, 1, 2*time.Second)
}

func TestIntegrationRenameAndReattach(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "old-name", []string{"sleep", "30"})
	// After rename the session is reachable under new-name.
	defer killAndWait(cfg, "new-name", errCh)

	if err := RenameSession(cfg, "old-name", "new-name"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if IsSocket(cfg.SocketPath("old-name")) {
		t.Fatal("old socket still exists after rename")
	}
	if !IsSocket(cfg.SocketPath("new-name")) {
		t.Fatal("new socket not found after rename")
	}

	// Attach via new name.
	conn := initConn(t, cfg.SocketPath("new-name"), 24, 80)
	defer conn.Close()
	waitForClients(t, cfg.SocketPath("new-name"), 1, 2*time.Second)

	// ListSessions should show only the new name.
	sessions, err := ListSessions(cfg)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Name != "new-name" {
		names := make([]string, len(sessions))
		for i, s := range sessions {
			names[i] = s.Name
		}
		t.Fatalf("sessions = %v, want [new-name]", names)
	}
}

func TestIntegrationKillExitsCleanly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "doomed", []string{"sleep", "30"})
	// Best-effort cleanup in case the test fails before the explicit kill.
	t.Cleanup(func() { _ = KillSession(cfg, "doomed") })
	sockPath := cfg.SocketPath("doomed")

	conn := initConn(t, sockPath, 24, 80)
	defer conn.Close()
	waitForClients(t, sockPath, 1, 2*time.Second)

	if err := KillSession(cfg, "doomed"); err != nil {
		t.Fatalf("kill: %v", err)
	}

	// Daemon should exit.
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon didn't exit after kill")
	}

	// Attached client should be disconnected.
	waitConnClosed(t, conn, 3*time.Second)

	// Socket should be removed.
	waitForSocketGone(t, sockPath, 3*time.Second)
}

func TestIntegrationDetachAllMultipleClients(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "multi", []string{"sleep", "30"})
	defer killAndWait(cfg, "multi", errCh)
	sockPath := cfg.SocketPath("multi")

	conn1 := initConn(t, sockPath, 24, 80)
	defer conn1.Close()
	conn2 := initConn(t, sockPath, 24, 80)
	defer conn2.Close()
	waitForClients(t, sockPath, 2, 2*time.Second)

	// DetachAll via the public helper (opens a separate connection).
	if err := DetachSession(cfg, "multi"); err != nil {
		t.Fatalf("detach all: %v", err)
	}

	waitConnClosed(t, conn1, 3*time.Second)
	waitConnClosed(t, conn2, 3*time.Second)

	// Daemon should still be alive with zero attached clients.
	waitForClients(t, sockPath, 0, 2*time.Second)
}

func TestIntegrationInputOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "echo", []string{"cat"})
	defer killAndWait(cfg, "echo", errCh)
	sockPath := cfg.SocketPath("echo")

	conn := initConn(t, sockPath, 24, 80)
	defer conn.Close()

	// Drain initial snapshot / Ctrl+L output.
	collectOutput(conn, 500*time.Millisecond)

	// Write to the PTY through TagInput.
	if err := SendMessage(conn, TagInput, []byte("ping\n")); err != nil {
		t.Fatalf("send input: %v", err)
	}

	output := collectOutput(conn, 2*time.Second)
	if !strings.Contains(string(output), "ping") {
		t.Fatalf("expected output containing 'ping', got %d bytes: %q", len(output), output)
	}
}

func TestIntegrationResize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "resize", []string{"sleep", "30"})
	defer killAndWait(cfg, "resize", errCh)
	sockPath := cfg.SocketPath("resize")

	conn := initConn(t, sockPath, 24, 80)
	defer conn.Close()

	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], 40)
	binary.LittleEndian.PutUint16(payload[2:4], 120)
	if err := SendMessage(conn, TagResize, payload); err != nil {
		t.Fatalf("send resize: %v", err)
	}

	// Daemon should still be healthy and we should still be attached.
	waitForClients(t, sockPath, 1, 2*time.Second)
}

func TestIntegrationFullScreenRestore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	errCh := startTestDaemon(t, cfg, "altscreen", []string{
		"sh", "-c", "printf '\\033[?1049h'; echo in-alt-screen; exec sleep 30",
	})
	defer killAndWait(cfg, "altscreen", errCh)
	sockPath := cfg.SocketPath("altscreen")

	// Let the daemon consume the alt-screen escape before we connect.
	time.Sleep(500 * time.Millisecond)

	conn := initConn(t, sockPath, 24, 80)
	defer conn.Close()

	output := collectOutput(conn, 2*time.Second)
	// Snapshot sent on init should re-enable alt screen.
	if !strings.Contains(string(output), "\033[?1049h") {
		t.Fatalf("snapshot missing alt-screen enable (\\033[?1049h); got %d bytes", len(output))
	}
}

func TestIntegrationSwitchSequenceBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	cfg := testConfig(t)
	// Emit a TSM local-switch escape after a short delay so we can connect first.
	errCh := startTestDaemon(t, cfg, "switcher", []string{
		"sh", "-c", "sleep 1; printf '\\033]633;TSM_ATTACH=target-session\\a'; exec sleep 30",
	})
	defer killAndWait(cfg, "switcher", errCh)
	sockPath := cfg.SocketPath("switcher")

	conn := initConn(t, sockPath, 24, 80)
	defer conn.Close()

	// Collect long enough for the 1-second-delayed switch sequence to arrive.
	output := collectOutput(conn, 3*time.Second)

	var filter outputFilter
	_, target := filter.Filter(output)
	if target != "target-session" {
		t.Fatalf("output filter: target = %q, want %q (%d bytes of output)", target, "target-session", len(output))
	}
}
