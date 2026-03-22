//go:build !cgo

package session

// stubTerminal is a no-op TerminalBackend used when the Ghostty C library
// is not available (e.g. in CI or lightweight dev environments).
type stubTerminal struct{}

func NewTerminalBackend(rows, cols uint16) TerminalBackend {
	return &stubTerminal{}
}

func (s *stubTerminal) Consume(data []byte) {}

func (s *stubTerminal) Resize(rows, cols uint16) error { return nil }

func (s *stubTerminal) Snapshot() []byte { return nil }

func (s *stubTerminal) Preview() []byte { return nil }

func (s *stubTerminal) Close() error { return nil }
