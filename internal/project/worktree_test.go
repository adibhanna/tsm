package project

import (
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	input := `worktree /Users/dev/monolith
HEAD abc123def456
branch refs/heads/main

worktree /Users/dev/monolith-feat-auth
HEAD def456abc789
branch refs/heads/feat/auth

worktree /Users/dev/monolith-fix-perf
HEAD 789abcdef012
branch refs/heads/fix/perf

`
	trees, err := parseWorktreeList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trees) != 3 {
		t.Fatalf("expected 3 worktrees, got %d", len(trees))
	}

	tests := []struct {
		path   string
		branch string
	}{
		{"/Users/dev/monolith", "main"},
		{"/Users/dev/monolith-feat-auth", "feat/auth"},
		{"/Users/dev/monolith-fix-perf", "fix/perf"},
	}
	for i, tt := range tests {
		if trees[i].Path != tt.path {
			t.Errorf("tree[%d].Path = %q, want %q", i, trees[i].Path, tt.path)
		}
		if trees[i].Branch != tt.branch {
			t.Errorf("tree[%d].Branch = %q, want %q", i, trees[i].Branch, tt.branch)
		}
		if trees[i].Bare {
			t.Errorf("tree[%d].Bare = true, want false", i)
		}
	}
}

func TestParseWorktreeListBare(t *testing.T) {
	input := `worktree /Users/dev/monolith.bare
HEAD abc123
bare

worktree /Users/dev/monolith-main
HEAD def456
branch refs/heads/main
`
	trees, err := parseWorktreeList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(trees))
	}
	if !trees[0].Bare {
		t.Error("tree[0].Bare = false, want true")
	}
	if trees[1].Bare {
		t.Error("tree[1].Bare = true, want false")
	}
	if trees[1].Branch != "main" {
		t.Errorf("tree[1].Branch = %q, want %q", trees[1].Branch, "main")
	}
}

func TestParseWorktreeListDetached(t *testing.T) {
	input := `worktree /Users/dev/monolith
HEAD abc123
detached
`
	trees, err := parseWorktreeList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(trees))
	}
	if trees[0].Branch != "detached" {
		t.Errorf("Branch = %q, want %q", trees[0].Branch, "detached")
	}
}

func TestParseWorktreeListNoTrailingNewline(t *testing.T) {
	input := `worktree /Users/dev/repo
HEAD abc123
branch refs/heads/main`
	trees, err := parseWorktreeList(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(trees))
	}
	if trees[0].Path != "/Users/dev/repo" {
		t.Errorf("Path = %q, want %q", trees[0].Path, "/Users/dev/repo")
	}
}

func TestSanitizeBranch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"main", "main"},
		{"feat/auth", "feat-auth"},
		{"fix/perf/issue", "fix-perf-issue"},
		{"refs/heads/main", "refs-heads-main"},
		{"..sneaky", "sneaky"},
		{".dotfile", "dotfile"},
		{"", "default"},
		{"normal-branch", "normal-branch"},
	}
	for _, tt := range tests {
		got := SanitizeBranch(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeRepoName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/Users/dev/monolith", "monolith"},
		{"/Users/dev/monolith.git", "monolith"},
		{"/Users/dev/monolith.bare", "monolith"},
		{"/Users/dev/my-repo.git", "my-repo"},
		{".git", "project"},
	}
	for _, tt := range tests {
		got := sanitizeRepoName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeRepoName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
