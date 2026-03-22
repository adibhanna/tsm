package session

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/term"
)

// Kitty keyboard protocol encodes Ctrl+\ as CSI 92;5u (92 = '\', 5 = ctrl modifier).
// Some terminals also send extended format like CSI 92;5:1u.
var kittyCtrlBackslash = []byte("\x1b[92;5u")
var kittyCtrlBackslashExt = []byte("\x1b[92;5:")

// isDetachKey checks if the input contains the detach sequence:
// either traditional Ctrl+\ (0x1C) or Kitty keyboard protocol encoding.
func isDetachKey(data []byte) bool {
	for _, b := range data {
		if b == 0x1C {
			return true
		}
	}
	return bytes.Contains(data, kittyCtrlBackslash) ||
		bytes.Contains(data, kittyCtrlBackslashExt)
}

// Terminal reset sequences sent before attach and on detach.
// Disables mouse tracking, bracketed paste, focus events, keyboard enhancement
// modes, alt screen; resets cursor style to the terminal default; shows cursor.
const termResetSeq = "\033[?1000l\033[?1002l\033[?1003l\033[?1006l" +
	"\033[?2004l\033[?1004l\033[>4m\033[=0;1u\033[0 q\033[?1049l" +
	"\033[?25h" +
	"\033[0m"

// After leaving a session, start the local terminal on a clean visible frame so
// the shell prompt doesn't land at the cursor position left by the detached app.
const termExitSeq = termResetSeq + "\033[2J\033[H"

// Attach connects to a session and relays I/O between the local terminal and the PTY.
func Attach(cfg Config, name string) error {
	path := cfg.SocketPath(name)
	conn, err := Connect(path)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", name, err)
	}
	defer conn.Close()

	// Put terminal in raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	var switchErr atomic.Pointer[SwitchSessionError]
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		// Reset terminal modes that the session may have enabled. When we're
		// about to switch directly into another attach, avoid the full clear so
		// we don't flash the previous screen right before the new session takes over.
		os.Stdout.WriteString(exitSeqForSwitch(switchErr.Load() != nil))
	}()

	// Ignore SIGQUIT so Ctrl+\ doesn't kill us — we use it to detach.
	signal.Ignore(syscall.SIGQUIT)
	defer signal.Reset(syscall.SIGQUIT)

	// Start from a known local terminal state before replaying the session.
	os.Stdout.WriteString(termResetSeq)
	os.Stdout.WriteString("\033[2J\033[H")

	// Send Init — daemon bounces PTY size → SIGWINCH → app redraws.
	if err := sendInit(conn); err != nil {
		return fmt.Errorf("send init: %w", err)
	}

	// Handle SIGWINCH for terminal resize.
	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	defer signal.Stop(winchCh)

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }

	// Relay server output → stdout.
	go func() {
		defer closeDone()
		var filter outputFilter
		for {
			tag, payload, err := ReadMessage(conn, 1*time.Second)
			if err != nil {
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					select {
					case <-done:
						return
					default:
						continue
					}
				}
				return
			}
			if tag == TagOutput {
				filtered, target := filter.Filter(payload)
				if len(filtered) > 0 {
					os.Stdout.Write(filtered)
				}
				if target != "" {
					switchErr.Store(&SwitchSessionError{Target: target})
					return
				}
			}
		}
	}()

	// Relay stdin → server input.
	go func() {
		defer closeDone()
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if isDetachKey(buf[:n]) {
					SendMessage(conn, TagDetach, nil)
					return
				}
				SendMessage(conn, TagInput, buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Handle SIGWINCH.
	go func() {
		for {
			select {
			case <-done:
				return
			case <-winchCh:
				sendResize(conn)
			}
		}
	}()

	<-done
	if se := switchErr.Load(); se != nil {
		return se
	}
	return nil
}

func exitSeqForSwitch(switching bool) string {
	if switching {
		return termResetSeq
	}
	return termExitSeq
}

func sendInit(conn net.Conn) error {
	rows, cols, err := getTerminalSize()
	if err != nil {
		rows, cols = 24, 80
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], rows)
	binary.LittleEndian.PutUint16(payload[2:4], cols)
	return SendMessage(conn, TagInit, payload)
}

func sendResize(conn net.Conn) {
	rows, cols, err := getTerminalSize()
	if err != nil {
		return
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint16(payload[0:2], rows)
	binary.LittleEndian.PutUint16(payload[2:4], cols)
	SendMessage(conn, TagResize, payload)
}

func getTerminalSize() (rows, cols uint16, err error) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0, err
	}
	return uint16(h), uint16(w), nil
}
