package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

var ErrSessionNotFound = errors.New("session not found")

// Session represents a running session, compatible with the TUI's Session type.
type Session struct {
	Name           string
	PID            string
	Clients        int
	StartedIn      string
	Cmd            string
	Memory         uint64 // filled later by process info
	Uptime         int    // filled later by process info
	AgentKind      string
	AgentState     string
	AgentSummary   string
	AgentUpdated   int64
	AgentModel     string
	AgentVersion   string
	AgentPrompt    string
	AgentPlan      string
	AgentApproval  string
	AgentSandbox   string
	AgentBranch    string
	AgentGitSHA    string
	AgentGitOrigin string
	AgentName      string
	AgentRole      string
	AgentMemory    string
	AgentSessionID string
	AgentSubagent  bool
	AgentInput     int64
	AgentOutput    int64
	AgentCached    int64
	AgentTotal     int64
	AgentContext   int64
	CreatedAt      uint64
	TaskEndedAt    uint64
	TaskExitCode   uint8
}

// DisplayDir returns StartedIn with $HOME replaced by ~.
func (s Session) DisplayDir() string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(s.StartedIn, home) {
		return "~" + s.StartedIn[len(home):]
	}
	return s.StartedIn
}

// ListSessions discovers sessions by probing socket files in the socket directory.
func ListSessions(cfg Config) ([]Session, error) {
	entries, err := os.ReadDir(cfg.SocketDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []Session
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := cfg.SocketPath(entry.Name())
		if !IsSocket(path) {
			continue
		}

		info, err := ProbeSession(path)
		if err != nil {
			// Connection refused means the daemon is dead — clean up the stale socket.
			var opErr *net.OpError
			if errors.As(err, &opErr) {
				CleanStaleSocket(path)
			}
			continue
		}

		sessions = append(sessions, Session{
			Name:         entry.Name(),
			PID:          strconv.Itoa(int(info.PID)),
			Clients:      int(info.ClientsLen),
			StartedIn:    info.CwdString(),
			Cmd:          info.CmdString(),
			CreatedAt:    info.CreatedAt,
			TaskEndedAt:  info.TaskEndedAt,
			TaskExitCode: info.TaskExitCode,
		})
	}

	return sessions, nil
}

// RenameSession renames a session by renaming its socket file.
func RenameSession(cfg Config, oldName, newName string) error {
	oldPath := cfg.SocketPath(oldName)
	if !IsSocket(oldPath) {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, oldName)
	}

	conn, err := Connect(oldPath)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := SendMessage(conn, TagRename, []byte(newName)); err != nil {
		return err
	}
	tag, payload, err := ReadMessage(conn, ioTimeout)
	if err != nil {
		return err
	}
	if tag != TagAck {
		return fmt.Errorf("unexpected response tag: %s", tag)
	}
	if len(payload) > 0 {
		return errors.New(string(payload))
	}
	return nil
}

// KillSession sends a kill message to the named session.
func KillSession(cfg Config, name string) error {
	path := cfg.SocketPath(name)
	if !IsSocket(path) {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, name)
	}
	conn, err := Connect(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	return SendMessage(conn, TagKill, nil)
}

// DetachSession disconnects all attached clients from the named session
// without killing the daemon or shell process.
func DetachSession(cfg Config, name string) error {
	path := cfg.SocketPath(name)
	if !IsSocket(path) {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, name)
	}
	conn, err := Connect(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	return SendMessage(conn, TagDetachAll, nil)
}
