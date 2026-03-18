package session

import (
	"os"
	"strconv"
	"testing"
)

func TestDefaultConfigUsesTSMDIR(t *testing.T) {
	t.Setenv("TSM_DIR", "/custom/path")
	cfg := DefaultConfig()
	if cfg.SocketDir != "/custom/path" {
		t.Errorf("SocketDir = %q, want %q", cfg.SocketDir, "/custom/path")
	}
}

func TestDefaultConfigUsesXDGRuntimeDir(t *testing.T) {
	t.Setenv("TSM_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	cfg := DefaultConfig()
	if cfg.SocketDir != "/run/user/1000/tsm" {
		t.Errorf("SocketDir = %q, want %q", cfg.SocketDir, "/run/user/1000/tsm")
	}
}

func TestDefaultConfigUsesTMPDIR(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	t.Setenv("TSM_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "/my/tmp/")
	cfg := DefaultConfig()
	want := "/my/tmp/tsm-" + uid
	if cfg.SocketDir != want {
		t.Errorf("SocketDir = %q, want %q", cfg.SocketDir, want)
	}
}

func TestDefaultConfigFallback(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	t.Setenv("TSM_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "")
	cfg := DefaultConfig()
	want := "/tmp/tsm-" + uid
	if cfg.SocketDir != want {
		t.Errorf("SocketDir = %q, want %q", cfg.SocketDir, want)
	}
}

func TestSocketPath(t *testing.T) {
	cfg := Config{SocketDir: "/tmp/tsm-501"}
	got := cfg.SocketPath("mysession")
	if got != "/tmp/tsm-501/mysession" {
		t.Errorf("SocketPath = %q, want %q", got, "/tmp/tsm-501/mysession")
	}
}

func TestMaxSessionNameLen(t *testing.T) {
	cfg := Config{SocketDir: "/tmp/tsm-501"}
	maxLen := cfg.MaxSessionNameLen()
	if maxLen <= 0 {
		t.Errorf("MaxSessionNameLen = %d, want > 0", maxLen)
	}
}
