package session

import (
	"strconv"
	"strings"
	"sync"
)

// TerminalBackend tracks PTY output and can serialize enough terminal state to
// bring a newly attached client back into sync with the daemon.
type TerminalBackend interface {
	Consume(data []byte)
	Resize(rows, cols uint16) error
	Snapshot() []byte
	Preview() []byte
	Close() error
}

// modeTracker tracks the small subset of terminal modes we need to restore on
// reattach when a full VT backend is unavailable.
type modeTracker struct {
	mu      sync.RWMutex
	pending []byte
	scratch []byte // reusable buffer to avoid per-Consume allocations

	altScreenMode  int
	altScreen      bool
	mouseX10       bool
	mouseVT200     bool
	mouseAny       bool
	mouseSGR       bool
	focusEvents    bool
	bracketedPaste bool
	cursorVisible  bool
}

func newModeTracker() *modeTracker {
	return &modeTracker{cursorVisible: true}
}

// Consume updates terminal mode state from raw PTY output.
func (t *modeTracker) Consume(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	needed := len(t.pending) + len(data)
	if cap(t.scratch) < needed {
		t.scratch = make([]byte, 0, needed)
	}
	buf := t.scratch[:0]
	buf = append(buf, t.pending...)
	buf = append(buf, data...)
	t.pending = t.pending[:0]

	for i := 0; i < len(buf); {
		if buf[i] != 0x1b {
			i++
			continue
		}
		if i+1 >= len(buf) {
			t.pending = append(t.pending, buf[i:]...)
			break
		}
		if buf[i+1] != '[' {
			i++
			continue
		}
		if i+2 >= len(buf) {
			t.pending = append(t.pending, buf[i:]...)
			break
		}
		if buf[i+2] != '?' {
			i++
			continue
		}

		j := i + 3
		for j < len(buf) {
			b := buf[j]
			if (b >= '0' && b <= '9') || b == ';' || b == ':' {
				j++
				continue
			}
			break
		}
		if j >= len(buf) {
			t.pending = append(t.pending, buf[i:]...)
			break
		}

		final := buf[j]
		if final == 'h' || final == 'l' {
			t.applyPrivateModes(string(buf[i+3:j]), final == 'h')
		}
		i = j + 1
	}

	if len(t.pending) > 64 {
		t.pending = append([]byte(nil), t.pending[len(t.pending)-64:]...)
	}
}

// Snapshot returns the escape sequences needed to put a fresh client terminal
// back into the currently active mode set.
func (t *modeTracker) Snapshot() []byte {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var b strings.Builder
	if t.altScreen {
		b.Write(t.altScreenPrefix())
	}
	if t.mouseX10 {
		b.WriteString("\x1b[?1000h")
	}
	if t.mouseVT200 {
		b.WriteString("\x1b[?1002h")
	}
	if t.mouseAny {
		b.WriteString("\x1b[?1003h")
	}
	if t.mouseSGR {
		b.WriteString("\x1b[?1006h")
	}
	if t.focusEvents {
		b.WriteString("\x1b[?1004h")
	}
	if t.bracketedPaste {
		b.WriteString("\x1b[?2004h")
	}
	if !t.cursorVisible {
		b.WriteString("\x1b[?25l")
	}
	return []byte(b.String())
}

func (t *modeTracker) Resize(rows, cols uint16) error {
	_ = rows
	_ = cols
	return nil
}

func (t *modeTracker) Preview() []byte {
	return nil
}

func (t *modeTracker) Close() error {
	return nil
}

func (t *modeTracker) applyPrivateModes(params string, enabled bool) {
	for _, part := range strings.Split(params, ";") {
		if part == "" {
			continue
		}
		modePart, _, _ := strings.Cut(part, ":")
		mode, err := strconv.Atoi(modePart)
		if err != nil {
			continue
		}

		switch mode {
		case 25:
			t.cursorVisible = enabled
		case 47, 1047, 1049:
			t.altScreen = enabled
			if enabled {
				t.altScreenMode = mode
			} else if t.altScreenMode == 0 || t.altScreenMode == mode {
				t.altScreenMode = 0
			}
		case 1000:
			t.mouseX10 = enabled
		case 1002:
			t.mouseVT200 = enabled
		case 1003:
			t.mouseAny = enabled
		case 1004:
			t.focusEvents = enabled
		case 1006:
			t.mouseSGR = enabled
		case 2004:
			t.bracketedPaste = enabled
		}
	}
}

// consumeAltScreen is a lightweight scanner that only tracks alt-screen mode
// changes (47, 1047, 1049). Used by the Ghostty backend to avoid the full
// Consume overhead on every PTY read.
func (t *modeTracker) consumeAltScreen(data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := 0; i < len(data); {
		if data[i] != 0x1b {
			i++
			continue
		}
		// Need at least ESC [ ?
		if i+2 >= len(data) || data[i+1] != '[' || data[i+2] != '?' {
			i++
			continue
		}
		// Scan digits and semicolons
		j := i + 3
		for j < len(data) {
			b := data[j]
			if (b >= '0' && b <= '9') || b == ';' || b == ':' {
				j++
				continue
			}
			break
		}
		if j >= len(data) {
			break // incomplete sequence at end of buffer
		}
		final := data[j]
		if final == 'h' || final == 'l' {
			enabled := final == 'h'
			params := string(data[i+3 : j])
			for _, part := range strings.Split(params, ";") {
				modePart, _, _ := strings.Cut(part, ":")
				mode, err := strconv.Atoi(modePart)
				if err != nil {
					continue
				}
				switch mode {
				case 47, 1047, 1049:
					t.altScreen = enabled
					if enabled {
						t.altScreenMode = mode
					} else if t.altScreenMode == 0 || t.altScreenMode == mode {
						t.altScreenMode = 0
					}
				}
			}
		}
		i = j + 1
	}
}

func (t *modeTracker) altScreenPrefix() []byte {
	switch t.altScreenMode {
	case 47:
		return []byte("\x1b[?47h")
	case 1047:
		return []byte("\x1b[?1047h")
	default:
		return []byte("\x1b[?1049h")
	}
}
