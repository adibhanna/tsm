package session

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeSessionIntegration(t *testing.T) {
	// Create a temporary Unix socket that acts as a mock daemon.
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test-session")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Mock daemon: accept one connection, read Info request, respond with Info.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request header
		var hdr [HeaderSize]byte
		conn.Read(hdr[:])

		// Build an Info response
		payload := make([]byte, InfoSize)
		binary.LittleEndian.PutUint64(payload[0:8], 2)       // clients
		binary.LittleEndian.PutUint32(payload[8:12], 99999)   // pid
		binary.LittleEndian.PutUint16(payload[12:14], 3)      // cmd_len
		binary.LittleEndian.PutUint16(payload[14:16], 4)      // cwd_len
		copy(payload[16:], "zsh")                              // cmd
		copy(payload[272:], "/tmp")                            // cwd
		binary.LittleEndian.PutUint64(payload[528:536], 5000) // created_at

		resp := MarshalMessage(TagInfo, payload)
		conn.Write(resp)
	}()

	info, err := ProbeSession(sockPath)
	if err != nil {
		t.Fatalf("ProbeSession: %v", err)
	}
	if info.PID != 99999 {
		t.Errorf("PID = %d, want 99999", info.PID)
	}
	if info.ClientsLen != 2 {
		t.Errorf("ClientsLen = %d, want 2", info.ClientsLen)
	}
	if info.CmdString() != "zsh" {
		t.Errorf("Cmd = %q, want %q", info.CmdString(), "zsh")
	}
	if info.CwdString() != "/tmp" {
		t.Errorf("Cwd = %q, want %q", info.CwdString(), "/tmp")
	}
	if info.CreatedAt != 5000 {
		t.Errorf("CreatedAt = %d, want 5000", info.CreatedAt)
	}
}

func TestProbeSessionRefused(t *testing.T) {
	_, err := ProbeSession("/tmp/nonexistent-tsm-socket-test")
	if err == nil {
		t.Error("expected error for nonexistent socket")
	}
}

func TestIsSocket(t *testing.T) {
	dir := t.TempDir()

	// Regular file
	f := filepath.Join(dir, "regular")
	os.WriteFile(f, []byte("hi"), 0644)
	if IsSocket(f) {
		t.Error("regular file should not be a socket")
	}

	// Unix socket
	sockPath := filepath.Join(dir, "sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	if !IsSocket(sockPath) {
		t.Error("unix socket should be detected as socket")
	}

	// Nonexistent
	if IsSocket(filepath.Join(dir, "nope")) {
		t.Error("nonexistent path should not be a socket")
	}
}
