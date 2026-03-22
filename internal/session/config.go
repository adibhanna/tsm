package session

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Config holds paths for session socket and log directories.
type Config struct {
	SocketDir string
	LogDir    string
}

// DefaultConfig resolves the socket directory:
//  1. $TSM_DIR
//  2. $XDG_RUNTIME_DIR/tsm
//  3. $TMPDIR/tsm-{uid} (trailing slash stripped)
//  4. /tmp/tsm-{uid}
func DefaultConfig() Config {
	uid := strconv.Itoa(os.Getuid())
	suffix := "tsm-" + uid

	var dir string
	switch {
	case os.Getenv("TSM_DIR") != "":
		dir = os.Getenv("TSM_DIR")
	case os.Getenv("XDG_RUNTIME_DIR") != "":
		dir = os.Getenv("XDG_RUNTIME_DIR") + "/tsm"
	case os.Getenv("TMPDIR") != "":
		dir = strings.TrimRight(os.Getenv("TMPDIR"), "/") + "/" + suffix
	default:
		dir = "/tmp/" + suffix
	}

	return Config{
		SocketDir: dir,
		LogDir:    dir + "/logs",
	}
}

// SocketPath returns the full path to a session's Unix domain socket.
func (c Config) SocketPath(name string) string {
	return c.SocketDir + "/" + name
}

// ValidateSessionName checks that a session name is safe for use in file paths.
func ValidateSessionName(name string) error {
	if name == "" {
		return fmt.Errorf("session name must not be empty")
	}
	if strings.Contains(name, "/") || strings.Contains(name, string(filepath.Separator)) {
		return fmt.Errorf("session name must not contain path separators")
	}
	if strings.Contains(name, "\x00") {
		return fmt.Errorf("session name must not contain null bytes")
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "..") {
		return fmt.Errorf("session name must not be or start with '..'")
	}
	return nil
}

// MaxSessionNameLen returns the maximum session name length based on
// the platform's sockaddr_un.sun_path limit minus the socket dir path.
func (c Config) MaxSessionNameLen() int {
	// macOS sun_path is 104 bytes, Linux is 108
	limit := 108
	if runtime.GOOS == "darwin" {
		limit = 104
	}
	return limit - len(c.SocketDir) - 1 // -1 for the "/" separator
}
