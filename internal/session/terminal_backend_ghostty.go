//go:build cgo

package session

/*
#cgo pkg-config: libghostty-vt
#include <stdlib.h>
#include <ghostty/vt.h>
*/
import "C"

import (
	"bytes"
	"sync"
	"unsafe"
)

const ghosttyMaxScrollbackLines = 10000

type ghosttyTerminal struct {
	mu      sync.Mutex
	term    C.GhosttyTerminal
	tracker *modeTracker
}

func NewTerminalBackend(rows, cols uint16) TerminalBackend {
	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 80
	}

	opts := C.GhosttyTerminalOptions{
		cols:           C.uint16_t(cols),
		rows:           C.uint16_t(rows),
		max_scrollback: C.size_t(ghosttyMaxScrollbackLines),
	}

	var term C.GhosttyTerminal
	if res := C.ghostty_terminal_new(nil, &term, opts); res != C.GHOSTTY_SUCCESS {
		return newModeTracker()
	}

	return &ghosttyTerminal{term: term, tracker: newModeTracker()}
}

func (g *ghosttyTerminal) Consume(data []byte) {
	if len(data) == 0 || g.term == nil {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	// Only track alt-screen mode (needed for Snapshot prefix). Skip the
	// full modeTracker.Consume to avoid its allocation + parsing overhead
	// on every PTY read — the Ghostty VT backend handles everything else.
	g.tracker.consumeAltScreen(data)
	C.ghostty_terminal_vt_write(g.term, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
}

func (g *ghosttyTerminal) Resize(rows, cols uint16) error {
	if g.term == nil || rows == 0 || cols == 0 {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	C.ghostty_terminal_resize(g.term, C.uint16_t(cols), C.uint16_t(rows))
	return nil
}

func (g *ghosttyTerminal) Snapshot() []byte {
	if g.term == nil {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	screenExtra := C.GhosttyFormatterScreenExtra{
		size:           C.size_t(C.sizeof_GhosttyFormatterScreenExtra),
		cursor:         C.bool(true),
		style:          C.bool(true),
		hyperlink:      C.bool(true),
		protection:     C.bool(true),
		kitty_keyboard: C.bool(true),
		charsets:       C.bool(true),
	}
	termExtra := C.GhosttyFormatterTerminalExtra{
		size:             C.size_t(C.sizeof_GhosttyFormatterTerminalExtra),
		palette:          C.bool(false),
		modes:            C.bool(true),
		scrolling_region: C.bool(true),
		tabstops:         C.bool(false),
		pwd:              C.bool(true),
		keyboard:         C.bool(true),
		screen:           screenExtra,
	}
	opts := C.GhosttyFormatterTerminalOptions{
		size:   C.size_t(C.sizeof_GhosttyFormatterTerminalOptions),
		emit:   C.GHOSTTY_FORMATTER_FORMAT_VT,
		unwrap: C.bool(false),
		trim:   C.bool(false),
		extra:  termExtra,
	}
	snapshot := g.formatLocked(opts)
	if prefix := g.altScreenPrefix(); len(prefix) > 0 && !bytes.HasPrefix(snapshot, prefix) {
		snapshot = append(prefix, snapshot...)
	}
	return snapshot
}

func (g *ghosttyTerminal) Preview() []byte {
	if g.term == nil {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	screenExtra := C.GhosttyFormatterScreenExtra{
		size:      C.size_t(C.sizeof_GhosttyFormatterScreenExtra),
		style:     C.bool(true),
		hyperlink: C.bool(true),
	}
	opts := C.GhosttyFormatterTerminalOptions{
		size:   C.size_t(C.sizeof_GhosttyFormatterTerminalOptions),
		emit:   C.GHOSTTY_FORMATTER_FORMAT_VT,
		unwrap: C.bool(false),
		trim:   C.bool(true),
		extra: C.GhosttyFormatterTerminalExtra{
			size:   C.size_t(C.sizeof_GhosttyFormatterTerminalExtra),
			screen: screenExtra,
		},
	}

	return g.formatLocked(opts)
}

func (g *ghosttyTerminal) formatLocked(opts C.GhosttyFormatterTerminalOptions) []byte {
	var formatter C.GhosttyFormatter
	if res := C.ghostty_formatter_terminal_new(nil, &formatter, g.term, opts); res != C.GHOSTTY_SUCCESS {
		return nil
	}
	defer C.ghostty_formatter_free(formatter)

	var ptr *C.uint8_t
	var n C.size_t
	if res := C.ghostty_formatter_format_alloc(formatter, nil, &ptr, &n); res != C.GHOSTTY_SUCCESS || ptr == nil || n == 0 {
		return nil
	}
	defer C.free(unsafe.Pointer(ptr))

	return C.GoBytes(unsafe.Pointer(ptr), C.int(n))
}

func (g *ghosttyTerminal) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.term != nil {
		C.ghostty_terminal_free(g.term)
		g.term = nil
	}
	return nil
}

func (g *ghosttyTerminal) altScreenPrefix() []byte {
	if g.tracker != nil && g.tracker.altScreen {
		return g.tracker.altScreenPrefix()
	}
	return nil
}
