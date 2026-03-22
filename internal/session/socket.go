package session

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	connectTimeout = 1 * time.Second
	ioTimeout      = 1 * time.Second
)

// Connect dials a Unix domain socket with a timeout.
func Connect(socketPath string) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath, connectTimeout)
}

// SendMessage writes a header + payload to a connection.
func SendMessage(conn net.Conn, tag Tag, payload []byte) error {
	conn.SetWriteDeadline(time.Now().Add(ioTimeout))
	msg := MarshalMessage(tag, payload)
	_, err := conn.Write(msg)
	return err
}

// ReadMessage reads a header + payload from a connection.
func ReadMessage(conn net.Conn, timeout time.Duration) (Tag, []byte, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))

	var hdrBuf [HeaderSize]byte
	if _, err := io.ReadFull(conn, hdrBuf[:]); err != nil {
		return 0, nil, fmt.Errorf("read header: %w", err)
	}
	hdr, err := ParseHeader(hdrBuf[:])
	if err != nil {
		return 0, nil, err
	}

	if hdr.Len > MaxPayloadSize {
		return 0, nil, fmt.Errorf("payload too large: %d bytes (max %d)", hdr.Len, MaxPayloadSize)
	}

	if hdr.Len == 0 {
		return hdr.Tag, nil, nil
	}

	payload := make([]byte, hdr.Len)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, fmt.Errorf("read payload: %w", err)
	}
	return hdr.Tag, payload, nil
}

// ProbeSession connects to a session socket and requests its info.
func ProbeSession(socketPath string) (*InfoPayload, error) {
	conn, err := Connect(socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	if err := SendMessage(conn, TagInfo, nil); err != nil {
		return nil, fmt.Errorf("send info request: %w", err)
	}

	tag, payload, err := ReadMessage(conn, ioTimeout)
	if err != nil {
		return nil, fmt.Errorf("read info response: %w", err)
	}
	if tag != TagInfo {
		return nil, fmt.Errorf("unexpected response tag: %s", tag)
	}

	info, err := ParseInfo(payload)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// IsSocket returns true if the path is a Unix domain socket.
func IsSocket(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().Type()&os.ModeSocket != 0
}

// CleanStaleSocket removes a socket file for a dead session and prunes session sidecars.
func CleanStaleSocket(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	cfg := DefaultConfig()
	if filepath.Dir(path) == cfg.SocketDir {
		_ = RemoveSessionRuntimeFiles(cfg, filepath.Base(path))
	}
	return nil
}
