package engine

import (
	"github.com/adibhanna/tsm/internal/session"
)

// Session is the session type used throughout the application.
type Session = session.Session

// FetchSessions discovers sessions by probing Unix sockets directly.
func FetchSessions() ([]Session, error) {
	return session.ListSessions(session.DefaultConfig())
}

// CreateSession spawns a new daemon process for the named session.
func CreateSession(name string) error {
	return session.SpawnDaemon(name, nil)
}

// KillSession sends a kill message to the named session via Unix socket.
func KillSession(name string) error {
	return session.KillSession(session.DefaultConfig(), name)
}

// RenameSession renames a session.
func RenameSession(oldName, newName string) error {
	return session.RenameSession(session.DefaultConfig(), oldName, newName)
}

// CopyToClipboard copies text to the system clipboard.
func CopyToClipboard(text string) error {
	return deps.clipboardWrite(text)
}
