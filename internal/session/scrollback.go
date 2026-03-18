package session

import (
	"strings"
	"sync"
)

// Scrollback is a thread-safe ring buffer that stores raw PTY output.
type Scrollback struct {
	mu   sync.RWMutex
	buf  []byte
	size int  // max capacity
	pos  int  // next write position
	full bool // true once the buffer has wrapped
}

// NewScrollback creates a scrollback buffer with the given max byte capacity.
func NewScrollback(size int) *Scrollback {
	return &Scrollback{
		buf:  make([]byte, size),
		size: size,
	}
}

// Write appends data to the ring buffer.
func (s *Scrollback) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(p)
	if n >= s.size {
		// Data larger than buffer — keep only the tail
		copy(s.buf, p[n-s.size:])
		s.pos = 0
		s.full = true
		return n, nil
	}

	space := s.size - s.pos
	if n <= space {
		copy(s.buf[s.pos:], p)
	} else {
		copy(s.buf[s.pos:], p[:space])
		copy(s.buf, p[space:])
	}
	s.pos = (s.pos + n) % s.size
	if !s.full && s.pos < n {
		s.full = true
	}
	return n, nil
}

// Bytes returns the full contents of the ring buffer in order.
func (s *Scrollback) Bytes() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.full {
		out := make([]byte, s.pos)
		copy(out, s.buf[:s.pos])
		return out
	}
	out := make([]byte, s.size)
	copy(out, s.buf[s.pos:])
	copy(out[s.size-s.pos:], s.buf[:s.pos])
	return out
}

// TailLines returns the last n lines from the buffer.
func (s *Scrollback) TailLines(n int) string {
	data := s.Bytes()
	str := string(data)
	lines := strings.Split(str, "\n")
	if len(lines) <= n {
		return str
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
