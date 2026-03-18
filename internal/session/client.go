package session

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

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
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Send Init message with terminal size.
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
		for {
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			tag, payload, err := ReadMessage(conn, 1*time.Second)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
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
				os.Stdout.Write(payload)
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
				// Check for Ctrl+\ (0x1C) — detach.
				if buf[0] == 0x1C {
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
	return nil
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
