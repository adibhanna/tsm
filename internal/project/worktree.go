package project

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Worktree represents a git worktree discovered from `git worktree list --porcelain`.
type Worktree struct {
	Path   string // absolute path
	Branch string // short branch name (e.g. "main", "feat/auth")
	Bare   bool
}

// DetectWorktrees runs `git worktree list --porcelain` on the given repo root
// and returns the discovered worktrees.
func DetectWorktrees(repoRoot string) ([]Worktree, error) {
	expanded := expandPath(repoRoot)
	cmd := exec.Command("git", "-C", expanded, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list in %q: %w", expanded, err)
	}
	return parseWorktreeList(string(out))
}

// parseWorktreeList parses the porcelain output of `git worktree list --porcelain`.
// Each worktree block is separated by a blank line:
//
//	worktree /path/to/worktree
//	HEAD abc123
//	branch refs/heads/main
//
//	worktree /path/to/other
//	HEAD def456
//	branch refs/heads/feat/auth
func parseWorktreeList(output string) ([]Worktree, error) {
	var trees []Worktree
	var current *Worktree

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if current != nil {
				trees = append(trees, *current)
				current = nil
			}
			continue
		}

		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			current = &Worktree{Path: path}
			continue
		}
		if current == nil {
			continue
		}

		if ref, ok := strings.CutPrefix(line, "branch "); ok {
			current.Branch = strings.TrimPrefix(ref, "refs/heads/")
			continue
		}
		if line == "bare" {
			current.Bare = true
			continue
		}
		if line == "detached" {
			if current.Branch == "" {
				current.Branch = "detached"
			}
			continue
		}
	}
	// Flush last entry if no trailing blank line.
	if current != nil {
		trees = append(trees, *current)
	}

	return trees, scanner.Err()
}

// IsGitRepo checks whether the given path is inside a git repository.
func IsGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", expandPath(path), "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// RepoName returns the base directory name of the git repo root.
// For worktrees, it uses --git-common-dir to find the actual repo,
// not the worktree directory.
func RepoName(path string) (string, error) {
	expanded := expandPath(path)

	// Try --git-common-dir first (works for worktrees and bare repos).
	cmd := exec.Command("git", "-C", expanded, "rev-parse", "--git-common-dir")
	out, err := cmd.Output()
	if err == nil {
		commonDir := strings.TrimSpace(string(out))
		// --git-common-dir returns the .git dir (or the bare repo dir).
		// For bare repos like "/path/repo.bare", use it directly.
		// For regular repos, it returns "/path/repo/.git" — take the parent.
		name := sanitizeRepoName(strings.TrimSuffix(commonDir, "/.git"))
		if name != "project" {
			return name, nil
		}
	}

	// Fallback to --show-toplevel.
	cmd = exec.Command("git", "-C", expanded, "rev-parse", "--show-toplevel")
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse in %q: %w", expanded, err)
	}
	top := strings.TrimSpace(string(out))
	return sanitizeRepoName(top), nil
}

func sanitizeRepoName(toplevel string) string {
	base := toplevel
	// Take last path component.
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	// Remove common suffixes.
	base = strings.TrimSuffix(base, ".git")
	base = strings.TrimSuffix(base, ".bare")
	if base == "" {
		base = "project"
	}
	return base
}

func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := homeDir(); err == nil {
			if p == "~" {
				return home
			}
			return home + p[1:]
		}
	}
	return p
}

func homeDir() (string, error) {
	return os.UserHomeDir()
}
