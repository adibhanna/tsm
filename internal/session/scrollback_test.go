package session

import "testing"

func TestScrollbackBytesNoWrap(t *testing.T) {
	s := NewScrollback(8)
	if _, err := s.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := string(s.Bytes()); got != "hello" {
		t.Fatalf("Bytes = %q, want %q", got, "hello")
	}
}

func TestScrollbackBytesWrap(t *testing.T) {
	s := NewScrollback(5)
	if _, err := s.Write([]byte("abc")); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if _, err := s.Write([]byte("def")); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if got := string(s.Bytes()); got != "bcdef" {
		t.Fatalf("Bytes = %q, want %q", got, "bcdef")
	}
}

func TestScrollbackWriteLargerThanBufferKeepsTail(t *testing.T) {
	s := NewScrollback(4)
	if _, err := s.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := string(s.Bytes()); got != "cdef" {
		t.Fatalf("Bytes = %q, want %q", got, "cdef")
	}
}

func TestScrollbackTailLines(t *testing.T) {
	s := NewScrollback(64)
	if _, err := s.Write([]byte("one\ntwo\nthree\nfour")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := s.TailLines(2); got != "three\nfour" {
		t.Fatalf("TailLines = %q, want %q", got, "three\nfour")
	}
}
