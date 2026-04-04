package cmux

import (
	"os"
	"path/filepath"
	"testing"
)

// newTestBackend creates a Backend that uses the given script as its cmux binary.
func newTestBackend(t *testing.T, script string) *Backend {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "cmux")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Backend{bin: bin}
}

func TestAvailable_LowercasePong(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\necho pong\n")
	if !b.Available() {
		t.Error("Expected Available() = true for lowercase 'pong'")
	}
}

func TestAvailable_UppercasePONG(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\necho PONG\n")
	if !b.Available() {
		t.Error("Expected Available() = true for uppercase 'PONG'")
	}
}

func TestAvailable_MixedCasePong(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\necho Pong\n")
	if !b.Available() {
		t.Error("Expected Available() = true for mixed case 'Pong'")
	}
}

func TestAvailable_NoBinary(t *testing.T) {
	b := &Backend{bin: ""}
	if b.Available() {
		t.Error("Expected Available() = false when binary is empty")
	}
}

func TestAvailable_PingFails(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\nexit 1\n")
	if b.Available() {
		t.Error("Expected Available() = false when ping exits non-zero")
	}
}

func TestAvailable_UnexpectedOutput(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\necho 'something else'\n")
	if b.Available() {
		t.Error("Expected Available() = false for output without 'pong'")
	}
}

func TestName(t *testing.T) {
	b := &Backend{}
	if got := b.Name(); got != "cmux" {
		t.Errorf("Name() = %q, want %q", got, "cmux")
	}
}

func TestUnavailableReason_NoBinary(t *testing.T) {
	b := &Backend{bin: ""}
	got := b.UnavailableReason()
	want := "cmux binary not found in PATH"
	if got != want {
		t.Errorf("UnavailableReason() = %q, want %q", got, want)
	}
}

func TestUnavailableReason_PingSucceeds(t *testing.T) {
	b := newTestBackend(t, "#!/bin/sh\necho PONG\n")
	if reason := b.UnavailableReason(); reason != "" {
		t.Errorf("UnavailableReason() = %q, want empty string", reason)
	}
}
