//go:build !cgo || noghosttyvt

package session

func NewTerminalBackend(rows, cols uint16) TerminalBackend {
	_ = rows
	_ = cols
	return newModeTracker()
}
