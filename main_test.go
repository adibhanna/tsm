package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adibhanna/tsm/internal/session"
)

func TestSuggestSessionNameUsesCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.DefaultConfig()
	name, err := suggestSessionName(cfg, nil)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name != "demo" {
		t.Fatalf("name = %q, want demo", name)
	}
}

func TestSuggestSessionNameAddsSuffixForCollisions(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.DefaultConfig()
	sessions := []session.Session{{Name: "demo"}, {Name: "demo-2"}}
	name, err := suggestSessionName(cfg, sessions)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name != "demo-3" {
		t.Fatalf("name = %q, want demo-3", name)
	}
}

func TestSuggestSessionNameSkipsExistingSocketPathConflicts(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(workdir, 0750); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(prev)
	if err := os.Chdir(workdir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cfg := session.Config{SocketDir: dir}
	name, err := suggestSessionName(cfg, nil)
	if err != nil {
		t.Fatalf("suggestSessionName: %v", err)
	}
	if name == "demo" {
		t.Fatalf("name = %q, want conflict-avoiding variant", name)
	}
	if !strings.HasSuffix(name, "-2") {
		t.Fatalf("name = %q, want suffix -2", name)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	if got := sanitizeSessionName("  my project\tname "); got != "my-project-name" {
		t.Fatalf("sanitizeSessionName = %q", got)
	}
}

func TestTruncateSessionName(t *testing.T) {
	if got := truncateSessionName("abcdefgh", 5); got != "abcde" {
		t.Fatalf("truncateSessionName = %q, want abcde", got)
	}
}

func TestResolveDetachTargetUsesCurrentSessionEnv(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	if got := resolveDetachTarget([]string{"tsm", "detach"}); got != "demo" {
		t.Fatalf("resolveDetachTarget = %q, want demo", got)
	}
}

func TestResolveDetachTargetPrefersExplicitName(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	if got := resolveDetachTarget([]string{"tsm", "detach", "other"}); got != "other" {
		t.Fatalf("resolveDetachTarget = %q, want other", got)
	}
}

func TestResolveKillTargetsUsesCurrentSessionEnv(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	got := resolveKillTargets([]string{"tsm", "kill"})
	if len(got) != 1 || got[0] != "demo" {
		t.Fatalf("resolveKillTargets = %#v, want [demo]", got)
	}
}

func TestResolveKillTargetsPrefersExplicitNames(t *testing.T) {
	t.Setenv("TSM_SESSION", "demo")
	got := resolveKillTargets([]string{"tsm", "kill", "one", "two"})
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("resolveKillTargets = %#v, want [one two]", got)
	}
}
