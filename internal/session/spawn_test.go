package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpawnDaemonCleansSocketWhenDaemonCrashesAfterBinding reproduces issue
// #12: libghostty-vt's dynamic-linker error aborts the daemon process via
// _exit(127) after net.Listen has already created the socket, so the
// daemon's deferred cleanup never fires and a stale socket leaks. The fix
// in SpawnDaemonInDir runs CleanStaleSocket on every "daemon exited" path.
func TestSpawnDaemonCleansSocketWhenDaemonCrashesAfterBinding(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv("TSM_DIR", dir)

	fake := buildFakeDaemonBindThenExit(t, 127)
	restore := daemonExecutable
	daemonExecutable = func() (string, error) { return fake, nil }
	t.Cleanup(func() { daemonExecutable = restore })

	err := SpawnDaemon("crashy", nil)
	if err == nil {
		t.Fatal("SpawnDaemon: expected error from crashing daemon, got nil")
	}
	if !strings.Contains(err.Error(), "daemon exited") {
		t.Fatalf("SpawnDaemon: error = %v, want substring \"daemon exited\"", err)
	}

	cfg := DefaultConfig()
	sockPath := cfg.SocketPath("crashy")
	if _, err := os.Lstat(sockPath); !os.IsNotExist(err) {
		t.Fatalf("expected socket %q to be cleaned up after daemon crash, lstat err=%v", sockPath, err)
	}
}

// TestSpawnDaemonCleansRuntimeFilesWhenDaemonCrashesAfterBinding asserts that
// runtime sidecars (daemon-build info, shell integration dirs) left by a
// half-started daemon are removed alongside the socket. Without cleanup,
// `tsm doctor` keeps listing them as orphaned artifacts.
func TestSpawnDaemonCleansRuntimeFilesWhenDaemonCrashesAfterBinding(t *testing.T) {
	dir := shortTempDir(t)
	t.Setenv("TSM_DIR", dir)

	fake := buildFakeDaemonBindThenExit(t, 127)
	restore := daemonExecutable
	daemonExecutable = func() (string, error) { return fake, nil }
	t.Cleanup(func() { daemonExecutable = restore })

	cfg := DefaultConfig()
	// Seed the kinds of sidecars StartDaemon would create before its
	// terminal backend panics. The cleanup must remove these even though
	// our fake daemon didn't write them itself.
	if err := writeDaemonBuildInfo(cfg, "crashy"); err != nil {
		t.Fatalf("seed daemon build info: %v", err)
	}
	zshDir := shellIntegrationDir(cfg, "zsh", "crashy")
	if err := os.MkdirAll(zshDir, 0o750); err != nil {
		t.Fatalf("seed zsh integration dir: %v", err)
	}

	if err := SpawnDaemon("crashy", nil); err == nil {
		t.Fatal("SpawnDaemon: expected error from crashing daemon")
	}

	if _, err := os.Lstat(daemonBuildInfoPath(cfg, "crashy")); !os.IsNotExist(err) {
		t.Fatalf("expected daemon build info cleaned up, lstat err=%v", err)
	}
	if _, err := os.Lstat(zshDir); !os.IsNotExist(err) {
		t.Fatalf("expected zsh integration dir cleaned up, lstat err=%v", err)
	}
}

// shortTempDir creates a short temp directory under /tmp so the macOS
// sun_path limit (104 bytes) does not reject Unix socket paths. t.TempDir
// returns deeply-nested paths under /var/folders that easily exceed the
// limit during these tests.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tsm-s-")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// buildFakeDaemonBindThenExit compiles a tiny Go program that binds the
// session's Unix socket and then aborts with the given exit code, skipping
// any deferred cleanup. This mirrors what happens when libghostty-vt's
// dynamic linker fails to resolve a symbol inside cgo init.
func buildFakeDaemonBindThenExit(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	bin := filepath.Join(dir, "fakebin")

	source := fmt.Sprintf(`package main

import (
	"net"
	"os"
	"path/filepath"
)

func main() {
	// tsm invokes the daemon as: <exe> --daemon <name> [shellCmd...]
	if len(os.Args) < 3 || os.Args[1] != "--daemon" {
		os.Exit(2)
	}
	name := os.Args[2]
	socketDir := os.Getenv("TSM_DIR")
	if socketDir == "" {
		os.Exit(3)
	}
	sockPath := filepath.Join(socketDir, name)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		os.Exit(4)
	}
	// Intentionally leak the listener: simulate a crash after binding.
	_ = ln
	os.Exit(%d)
}
`, exitCode)
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatalf("write fake daemon source: %v", err)
	}
	build := exec.Command("go", "build", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake daemon: %v\n%s", err, out)
	}
	return bin
}
