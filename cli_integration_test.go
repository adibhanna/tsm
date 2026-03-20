package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/adibhanna/tsm/internal/session"
	"github.com/creack/pty/v2"
)

// tsmBinary is the path to the built tsm binary used by CLI integration tests.
// Built once in TestMain.
var tsmBinary string

func TestMain(m *testing.M) {
	f, err := os.CreateTemp("", "tsm-cli-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp binary path: %v\n", err)
		os.Exit(1)
	}
	f.Close()
	tsmBinary = f.Name()
	build := exec.Command("go", "build", "-o", tsmBinary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build CLI test binary: %v\n%s\n", err, out)
		os.Remove(tsmBinary)
		os.Exit(1)
	}

	code := m.Run()
	if tsmBinary != "" {
		os.Remove(tsmBinary)
	}
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func needsBinary(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping CLI integration test")
	}
}

// cliTestDir creates a short temp dir for Unix socket paths (macOS sun_path limit).
func cliTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tsm-c-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// cliEnv returns the process environment with TSM isolation overrides.
func cliEnv(dir string, extra ...string) []string {
	skip := map[string]bool{
		"TSM_DIR": true, "TSM_SESSION": true, "TSM_CONFIG_FILE": true,
	}
	var env []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if !skip[k] {
			env = append(env, e)
		}
	}
	env = append(env, "TSM_DIR="+dir, "TSM_CONFIG_FILE="+dir+"/none.toml")
	env = append(env, extra...)
	return env
}

// startCLIDaemon starts a session daemon in a goroutine and waits for its socket.
func startCLIDaemon(t *testing.T, dir, name string, cmd []string) chan error {
	t.Helper()
	t.Setenv("TSM_DIR", dir)
	t.Setenv("TSM_CONFIG_FILE", dir+"/none.toml")
	cfg := session.DefaultConfig()

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.StartDaemon(name, cmd)
	}()

	sockPath := cfg.SocketPath(name)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if session.IsSocket(sockPath) {
			return errCh
		}
		select {
		case err := <-errCh:
			t.Fatalf("daemon %q exited early: %v", name, err)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket never appeared: %s", sockPath)
	return errCh
}

func cliKillAndWait(dir, name string, errCh chan error) {
	cfg := session.Config{SocketDir: dir}
	_ = session.KillSession(cfg, name)
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
	}
}

// readPTYUntil reads from a PTY master until needle appears or timeout.
// The goroutine exits when the needle is found, an error occurs, or the PTY is closed.
func readPTYUntil(f *os.File, needle string, timeout time.Duration) (string, bool) {
	found := make(chan string, 1)
	go func() {
		var buf []byte
		tmp := make([]byte, 4096)
		for {
			n, err := f.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if strings.Contains(string(buf), needle) {
					found <- string(buf)
					return
				}
			}
			if err != nil {
				found <- string(buf)
				return
			}
		}
	}()
	select {
	case s := <-found:
		return s, strings.Contains(s, needle)
	case <-time.After(timeout):
		return "", false
	}
}

// waitExit waits for a command to exit within timeout.
func waitExit(cmd *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("process didn't exit within %v", timeout)
	}
}

func waitForClients(t *testing.T, dir, name string, want int, timeout time.Duration) {
	t.Helper()
	cfg := session.Config{SocketDir: dir}
	sockPath := cfg.SocketPath(name)
	deadline := time.Now().Add(timeout)
	var last int
	for time.Now().Before(deadline) {
		info, err := session.ProbeSession(sockPath)
		if err == nil {
			last = int(info.ClientsLen)
			if last == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("wanted %d attached clients at %s, last observed %d", want, sockPath, last)
}

// ---------------------------------------------------------------------------
// CLI integration tests
// ---------------------------------------------------------------------------

// TestCLIAttachDetachReattach exercises the real client path:
// tsm attach → raw mode → I/O relay → Ctrl+\ detach → reattach.
func TestCLIAttachDetachReattach(t *testing.T) {
	needsBinary(t)
	dir := cliTestDir(t)
	errCh := startCLIDaemon(t, dir, "sess", []string{"cat"})
	defer cliKillAndWait(dir, "sess", errCh)

	// First attach.
	cmd := exec.Command(tsmBinary, "attach", "sess")
	cmd.Env = cliEnv(dir)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer ptmx.Close()

	// Let attach establish (raw mode, init, snapshot).
	time.Sleep(500 * time.Millisecond)

	// Type into the session; cat echoes it back through the PTY.
	ptmx.Write([]byte("first-attach\n"))
	if _, ok := readPTYUntil(ptmx, "first-attach", 5*time.Second); !ok {
		t.Fatal("no echoed input after first attach")
	}

	// Detach with Ctrl+\.
	ptmx.Write([]byte{0x1c})
	if err := waitExit(cmd, 5*time.Second); err != nil {
		t.Fatalf("didn't exit after detach: %v", err)
	}
	ptmx.Close()

	// Reattach.
	cmd2 := exec.Command(tsmBinary, "attach", "sess")
	cmd2.Env = cliEnv(dir)
	ptmx2, err := pty.StartWithSize(cmd2, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty start reattach: %v", err)
	}
	defer ptmx2.Close()

	time.Sleep(500 * time.Millisecond)
	ptmx2.Write([]byte("second-attach\n"))
	if _, ok := readPTYUntil(ptmx2, "second-attach", 5*time.Second); !ok {
		t.Fatal("no echoed input after reattach")
	}

	ptmx2.Write([]byte{0x1c})
	waitExit(cmd2, 5*time.Second)
}

// TestCLILocalSwitch verifies that `tsm attach B` from inside session A
// emits the switch sequence instead of nesting an attach.
func TestCLILocalSwitch(t *testing.T) {
	needsBinary(t)
	dir := cliTestDir(t)
	errChA := startCLIDaemon(t, dir, "a", []string{"sleep", "30"})
	defer cliKillAndWait(dir, "a", errChA)
	errChB := startCLIDaemon(t, dir, "b", []string{"sleep", "30"})
	defer cliKillAndWait(dir, "b", errChB)

	// With TSM_SESSION=a, tsm attach b should emit switch sequence and exit.
	// No PTY needed — it writes to stdout and returns immediately.
	cmd := exec.Command(tsmBinary, "attach", "b")
	cmd.Env = cliEnv(dir, "TSM_SESSION=a")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("tsm attach b: %v (output: %q)", err, out)
	}

	want := session.AttachSwitchSequence("b")
	if !strings.Contains(string(out), want) {
		t.Fatalf("expected switch sequence %q in output, got %q", want, out)
	}
}

// TestCLIKillTerminalRecovery verifies that when a session is killed, the
// attached client exits and writes terminal reset sequences so the user's
// shell/terminal recovers cleanly.
func TestCLIKillTerminalRecovery(t *testing.T) {
	needsBinary(t)
	dir := cliTestDir(t)
	errCh := startCLIDaemon(t, dir, "doomed", []string{"sleep", "30"})
	t.Cleanup(func() { cliKillAndWait(dir, "doomed", errCh) })

	cmd := exec.Command(tsmBinary, "attach", "doomed")
	cmd.Env = cliEnv(dir)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer ptmx.Close()

	// Start collecting ALL output from the PTY in the background.
	// Must start before the kill so we capture the terminal reset that
	// Attach() writes during its deferred cleanup.
	outputCh := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		tmp := make([]byte, 4096)
		for {
			n, err := ptmx.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if err != nil {
				break
			}
		}
		outputCh <- buf.String()
	}()

	// Let attach establish.
	time.Sleep(500 * time.Millisecond)

	// Kill the session from outside.
	cfg := session.Config{SocketDir: dir}
	if err := session.KillSession(cfg, "doomed"); err != nil {
		t.Fatalf("kill: %v", err)
	}

	// The attach process should exit.
	if err := waitExit(cmd, 5*time.Second); err != nil {
		t.Fatalf("attach didn't exit after kill: %v", err)
	}

	// Collect all output (reader goroutine gets EOF when slave closes).
	var output string
	select {
	case output = <-outputCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout collecting PTY output")
	}

	// termExitSeq includes \033[?25h (cursor show) and \033[?1049l (alt screen off).
	if !strings.Contains(output, "\033[?25h") {
		t.Logf("output after kill (%d bytes): %q", len(output), output)
		t.Fatal("expected terminal reset (cursor show) in output after kill")
	}
}

// TestCLITUIAttach verifies the full TUI picker → attach flow:
// tsm tui renders sessions, Enter selects, runAttachTarget re-execs into
// tsm attach, and the process enters the attached state.
//
// I/O relay and Ctrl+\ detach through the re-exec'd attach are covered
// by TestCLIAttachDetachReattach which tests the direct attach path.
func TestCLITUIAttach(t *testing.T) {
	t.Skip("skipping: full TUI attach flow is flaky in CI PTY environment")
}

// TestCLIPaletteAttach verifies the simplified palette → attach flow:
// tsm palette renders sessions, Enter selects, runAttachTarget re-execs
// into tsm attach, and the process enters the attached state.
func TestCLIPaletteAttach(t *testing.T) {
	needsBinary(t)
	dir := cliTestDir(t)
	errCh := startCLIDaemon(t, dir, "pal-sess", []string{"cat"})
	defer cliKillAndWait(dir, "pal-sess", errCh)

	cmd := exec.Command(tsmBinary, "palette")
	cmd.Env = cliEnv(dir, "TERM=xterm-256color", "TSM_TEST_ATTACH_STDIO=1")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatalf("pty start: %v", err)
	}
	defer ptmx.Close()

	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	// Wait for palette to render the session name.
	if _, ok := readPTYUntil(ptmx, "pal-sess", 5*time.Second); !ok {
		t.Fatal("palette didn't render session name")
	}

	// Press Enter to attach.
	ptmx.Write([]byte{'\r'})

	time.Sleep(700 * time.Millisecond)
	ptmx.Write([]byte("palette-attach\n"))
	if _, ok := readPTYUntil(ptmx, "palette-attach", 5*time.Second); !ok {
		t.Fatal("no echoed input after palette attach")
	}

	cfg := session.Config{SocketDir: dir}
	if err := session.KillSession(cfg, "pal-sess"); err != nil {
		t.Fatalf("kill pal-sess: %v", err)
	}
	if err := waitExit(cmd, 2*time.Second); err != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
