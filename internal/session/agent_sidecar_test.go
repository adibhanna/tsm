package session

import (
	"os"
	"testing"
)

func TestWriteClaudeStatuslineWritesSessionSidecar(t *testing.T) {
	cfg := Config{LogDir: t.TempDir()}
	data := []byte(`{"session_id":"sess-1"}`)
	if err := WriteClaudeStatusline(cfg, "demo", data); err != nil {
		t.Fatalf("WriteClaudeStatusline: %v", err)
	}
	path := ClaudeStatuslinePath(cfg, "demo")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	if string(got) != string(data) {
		t.Fatalf("sidecar = %q, want %q", got, data)
	}
}
