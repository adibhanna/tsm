package session

import (
	"fmt"
	"net"
	"os"
	"testing"
)

func TestSessionDisplayDirUsesHomeTilde(t *testing.T) {
	t.Setenv("HOME", "/Users/demo")
	s := Session{StartedIn: "/Users/demo/work/project"}
	if got := s.DisplayDir(); got != "~/work/project" {
		t.Fatalf("DisplayDir = %q, want %q", got, "~/work/project")
	}
}

func TestDetachSessionSendsDetachAll(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SocketDir: dir}
	path := cfg.SocketPath("demo")

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()
	defer os.Remove(path)

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		tag, _, err := ReadMessage(conn, ioTimeout)
		if err != nil {
			errCh <- err
			return
		}
		if tag != TagDetachAll {
			errCh <- fmt.Errorf("tag = %s, want %s", tag, TagDetachAll)
			return
		}
		errCh <- nil
	}()

	if err := DetachSession(cfg, "demo"); err != nil {
		t.Fatalf("DetachSession: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestRenameSessionRenamesSocket(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{SocketDir: dir}
	oldPath := cfg.SocketPath("old")

	ln, err := net.Listen("unix", oldPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		tag, payload, err := ReadMessage(conn, ioTimeout)
		if err != nil {
			errCh <- err
			return
		}
		if tag != TagRename {
			errCh <- fmt.Errorf("tag = %s, want %s", tag, TagRename)
			return
		}
		if string(payload) != "new" {
			errCh <- fmt.Errorf("payload = %q, want %q", string(payload), "new")
			return
		}
		if err := SendMessage(conn, TagAck, nil); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	if err := RenameSession(cfg, "old", "new"); err != nil {
		t.Fatalf("RenameSession: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestWriteSessionNameFile(t *testing.T) {
	dir := t.TempDir()
	path := sessionNameFilePath(Config{SocketDir: dir}, "zsh", "demo")
	if err := writeSessionNameFile(path, "renamed"); err != nil {
		t.Fatalf("writeSessionNameFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "renamed\n" {
		t.Fatalf("session name file = %q, want %q", string(data), "renamed\n")
	}
}
