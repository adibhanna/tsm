package session

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// GitContext holds git repository information detected from a working directory.
type GitContext struct {
	RepoName   string `json:"repo_name"`   // basename of the main repo
	BranchName string `json:"branch_name"` // current branch or short SHA for detached HEAD
	IsWorktree bool   `json:"is_worktree"` // true if cwd is a linked worktree
	IsGitRepo  bool   `json:"is_git_repo"` // true if inside any git repo
	RepoRoot   string `json:"repo_root"`   // absolute path to main worktree root
	IsSplit    bool   `json:"is_split"`    // true if this is a workspace split pane session
}

// DetectGitContext detects git repository context from the given directory.
// It runs a single git rev-parse invocation and parses the output.
func DetectGitContext(cwd string) GitContext {
	out, err := exec.Command("git", "-C", cwd, "rev-parse",
		"--show-toplevel", "--git-common-dir", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return GitContext{}
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return GitContext{}
	}

	toplevel := strings.TrimSpace(lines[0])
	gitCommonDir := strings.TrimSpace(lines[1])
	branch := strings.TrimSpace(lines[2])

	// Resolve gitCommonDir relative to toplevel if not absolute.
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(toplevel, gitCommonDir)
	}
	gitCommonDir = filepath.Clean(gitCommonDir)

	// Determine if this is a linked worktree.
	// For linked worktrees, gitCommonDir points to <main-repo>/.git (the common dir),
	// while for the main worktree, gitCommonDir equals <toplevel>/.git.
	mainGitDir := filepath.Join(toplevel, ".git")
	isWorktree := gitCommonDir != mainGitDir

	// Derive repo name from the common git dir.
	repoRoot := deriveRepoRoot(gitCommonDir)
	repoName := filepath.Base(repoRoot)

	// For bare repos, strip .git suffix from the name.
	repoName = strings.TrimSuffix(repoName, ".git")
	if repoName == "" || repoName == "." {
		repoName = filepath.Base(toplevel)
	}

	// Handle detached HEAD — branch will be "HEAD".
	if branch == "HEAD" {
		short, err := exec.Command("git", "-C", cwd, "rev-parse", "--short", "HEAD").Output()
		if err == nil {
			branch = strings.TrimSpace(string(short))
		}
	}

	return GitContext{
		RepoName:   repoName,
		BranchName: branch,
		IsWorktree: isWorktree,
		IsGitRepo:  true,
		RepoRoot:   repoRoot,
	}
}

// deriveRepoRoot determines the repository root directory from the git common dir.
// For a normal repo, gitCommonDir is <repo>/.git, so we return <repo>.
// For a bare repo, gitCommonDir is the bare repo dir itself.
func deriveRepoRoot(gitCommonDir string) string {
	if filepath.Base(gitCommonDir) == ".git" {
		return filepath.Dir(gitCommonDir)
	}
	// Bare repo — the common dir is the repo root.
	return gitCommonDir
}

// FormatSessionName returns a session name based on git context.
// For worktrees: "repo@branch". Otherwise: just the repo name.
func FormatSessionName(ctx GitContext) string {
	if ctx.IsWorktree && ctx.BranchName != "" {
		return SanitizeBranch(ctx.RepoName) + "@" + SanitizeBranch(ctx.BranchName)
	}
	return SanitizeBranch(ctx.RepoName)
}

// SanitizeBranch replaces characters that are invalid in session names.
func SanitizeBranch(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "\t", "-")
	return name
}
