package session

import (
	"os"
	"testing"
)

func TestWriteReadGitMeta(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		SocketDir: dir,
		LogDir:    dir + ".logs",
	}

	ctx := GitContext{
		RepoName:   "myrepo",
		BranchName: "feature/login",
		IsWorktree: true,
		IsGitRepo:  true,
		RepoRoot:   "/home/user/myrepo",
	}

	if err := WriteGitMeta(cfg, "test-session", ctx); err != nil {
		t.Fatalf("WriteGitMeta failed: %v", err)
	}

	got, err := ReadGitMeta(cfg, "test-session")
	if err != nil {
		t.Fatalf("ReadGitMeta failed: %v", err)
	}

	if got.RepoName != ctx.RepoName {
		t.Errorf("RepoName = %q, want %q", got.RepoName, ctx.RepoName)
	}
	if got.BranchName != ctx.BranchName {
		t.Errorf("BranchName = %q, want %q", got.BranchName, ctx.BranchName)
	}
	if got.IsWorktree != ctx.IsWorktree {
		t.Errorf("IsWorktree = %v, want %v", got.IsWorktree, ctx.IsWorktree)
	}
	if got.IsGitRepo != ctx.IsGitRepo {
		t.Errorf("IsGitRepo = %v, want %v", got.IsGitRepo, ctx.IsGitRepo)
	}
	if got.RepoRoot != ctx.RepoRoot {
		t.Errorf("RepoRoot = %q, want %q", got.RepoRoot, ctx.RepoRoot)
	}
}

func TestReadGitMeta_NotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		SocketDir: dir,
		LogDir:    dir + ".logs",
	}

	_, err := ReadGitMeta(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent sidecar")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got: %v", err)
	}
}
