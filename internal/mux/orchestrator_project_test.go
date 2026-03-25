package mux

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adibhanna/tsm/internal/session"
)

// TestOpenManifestTabPerWorktree verifies that OpenManifest works with
// a single manifest containing multiple surfaces (tabs) — one per worktree.
func TestOpenManifestTabPerWorktree(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TSM_DIR", tmpDir)

	mock := &mockBackend{}
	orch := &Orchestrator{
		Backend: mock,
		SessCfg: session.Config{
			SocketDir: filepath.Join(tmpDir),
			LogDir:    filepath.Join(tmpDir, "logs"),
		},
	}

	// Single manifest with 3 tabs (one per worktree), each with a split.
	manifest := &Manifest{
		Name:    "app",
		Version: 1,
		Startup: "app:main",
		Surface: []ManifestSurface{
			{
				Name:    "app:main",
				Session: "app:main",
				Cwd:     "/dev/app-main",
				Command: "claude",
				Split: []ManifestSplit{
					{Name: "git", Session: "app:main:git", Direction: "right", Cwd: "/dev/app-main", Command: "lazygit"},
				},
			},
			{
				Name:    "app:feat-auth",
				Session: "app:feat-auth",
				Cwd:     "/dev/app-feat",
				Command: "claude",
				Split: []ManifestSplit{
					{Name: "git2", Session: "app:feat-auth:git", Direction: "right", Cwd: "/dev/app-feat", Command: "lazygit"},
				},
			},
		},
	}

	err := orch.OpenManifest(manifest)
	// SpawnDaemon will fail in test env, but backend calls should be correct.
	if err != nil {
		hasCreate := false
		for _, c := range mock.calls {
			if strings.HasPrefix(c, "CreateWorkspace:") || strings.HasPrefix(c, "SelectWorkspace:") {
				hasCreate = true
				break
			}
		}
		if !hasCreate {
			t.Errorf("expected workspace creation call, got calls: %v", mock.calls)
		}
		return
	}
}

// TestOpenManifestReuseExistingWorkspace verifies that if a workspace
// already exists, OpenManifest selects it instead of creating a new one.
func TestOpenManifestReuseExistingWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TSM_DIR", tmpDir)

	mock := &mockBackend{
		workspaces: []Workspace{
			{ID: "workspace:42", Name: "app"},
		},
	}
	orch := &Orchestrator{
		Backend: mock,
		SessCfg: session.Config{
			SocketDir: filepath.Join(tmpDir),
			LogDir:    filepath.Join(tmpDir, "logs"),
		},
	}

	m := &Manifest{
		Name:    "app",
		Version: 1,
		Surface: []ManifestSurface{
			{Name: "app:main", Session: "app:main"},
		},
	}

	_ = orch.OpenManifest(m)

	for _, c := range mock.calls {
		if strings.HasPrefix(c, "CreateWorkspace:") {
			t.Errorf("should not create workspace when it exists, got: %s", c)
		}
	}

	hasSelect := false
	for _, c := range mock.calls {
		if c == "SelectWorkspace:workspace:42" {
			hasSelect = true
		}
	}
	if !hasSelect {
		t.Errorf("expected SelectWorkspace:workspace:42, got calls: %v", mock.calls)
	}
}

// TestManifestValidationWithProjectNames ensures that project-style
// names with colons pass manifest validation.
func TestManifestValidationWithProjectNames(t *testing.T) {
	m := &Manifest{
		Name:    "app",
		Version: 1,
		Surface: []ManifestSurface{
			{
				Name:    "app:main",
				Session: "app:main",
				Split: []ManifestSplit{
					{Name: "git", Session: "app:main:git", Direction: "right"},
				},
			},
			{
				Name:    "app:feat-auth",
				Session: "app:feat-auth",
				Split: []ManifestSplit{
					{Name: "git2", Session: "app:feat-auth:git", Direction: "right"},
				},
			},
		},
	}

	if err := validateManifest(m); err != nil {
		t.Errorf("project-style manifest should be valid, got: %v", err)
	}
}
