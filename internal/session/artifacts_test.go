package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveSessionArtifactsRemovesKnownSidecars(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir(), LogDir: filepath.Join(t.TempDir(), "logs")}
	for _, path := range []string{
		daemonBuildInfoPath(cfg, "demo"),
		ClaudeStatuslinePath(cfg, "demo"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveSessionArtifacts(cfg, "demo"); err != nil {
		t.Fatalf("RemoveSessionArtifacts: %v", err)
	}
	for _, path := range []string{
		daemonBuildInfoPath(cfg, "demo"),
		ClaudeStatuslinePath(cfg, "demo"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %q removed, err=%v", path, err)
		}
	}
}

func TestRenameSessionArtifactsMovesKnownSidecars(t *testing.T) {
	cfg := Config{SocketDir: t.TempDir(), LogDir: filepath.Join(t.TempDir(), "logs")}
	oldBuild := daemonBuildInfoPath(cfg, "old")
	oldClaude := ClaudeStatuslinePath(cfg, "old")
	for _, path := range []string{oldBuild, oldClaude} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RenameSessionArtifacts(cfg, "old", "new"); err != nil {
		t.Fatalf("RenameSessionArtifacts: %v", err)
	}
	for _, path := range []string{
		daemonBuildInfoPath(cfg, "new"),
		ClaudeStatuslinePath(cfg, "new"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %q present: %v", path, err)
		}
	}
}
