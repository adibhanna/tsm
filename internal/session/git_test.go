package session

import "testing"

func TestSanitizeBranch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"main", "main"},
		{"feature/login", "feature-login"},
		{"feature/deep/nested", "feature-deep-nested"},
		{"with spaces", "with-spaces"},
		{"with\ttabs", "with-tabs"},
		{"  trimmed  ", "trimmed"},
		{"", ""},
	}
	for _, tt := range tests {
		got := SanitizeBranch(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeBranch(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatSessionName(t *testing.T) {
	tests := []struct {
		name string
		ctx  GitContext
		want string
	}{
		{
			name: "worktree with branch",
			ctx:  GitContext{RepoName: "myrepo", BranchName: "feature/login", IsWorktree: true, IsGitRepo: true},
			want: "myrepo@feature-login",
		},
		{
			name: "worktree with simple branch",
			ctx:  GitContext{RepoName: "myrepo", BranchName: "main", IsWorktree: true, IsGitRepo: true},
			want: "myrepo@main",
		},
		{
			name: "non-worktree returns repo name",
			ctx:  GitContext{RepoName: "myrepo", BranchName: "main", IsWorktree: false, IsGitRepo: true},
			want: "myrepo",
		},
		{
			name: "worktree without branch",
			ctx:  GitContext{RepoName: "myrepo", BranchName: "", IsWorktree: true, IsGitRepo: true},
			want: "myrepo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSessionName(tt.ctx)
			if got != tt.want {
				t.Errorf("FormatSessionName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveRepoRoot(t *testing.T) {
	tests := []struct {
		gitCommonDir string
		want         string
	}{
		{"/home/user/myrepo/.git", "/home/user/myrepo"},
		{"/home/user/bare-repo.git", "/home/user/bare-repo.git"},
		{"/home/user/myrepo", "/home/user/myrepo"},
	}
	for _, tt := range tests {
		got := deriveRepoRoot(tt.gitCommonDir)
		if got != tt.want {
			t.Errorf("deriveRepoRoot(%q) = %q, want %q", tt.gitCommonDir, got, tt.want)
		}
	}
}
