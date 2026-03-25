package project

import "testing"

func TestFormatWorkspaceName(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		project string
		branch  string
		dirname string
		want    string
	}{
		{
			name:    "default format",
			format:  "{project}:{branch}",
			project: "app", branch: "main", dirname: "app-main",
			want: "app:main",
		},
		{
			name:    "dirname:project:branch",
			format:  "{dirname}:{project}:{branch}",
			project: "app", branch: "feat-auth", dirname: "app-feat",
			want: "app-feat:app:feat-auth",
		},
		{
			name:    "dirname only",
			format:  "{dirname}",
			project: "app", branch: "main", dirname: "app-main",
			want: "app-main",
		},
		{
			name:    "dirname:branch",
			format:  "{dirname}:{branch}",
			project: "app", branch: "main", dirname: "app-main",
			want: "app-main:main",
		},
		{
			name:    "project only",
			format:  "{project}",
			project: "monolith", branch: "main", dirname: "monolith",
			want: "monolith",
		},
		{
			name:    "literal text preserved",
			format:  "ws-{project}-{branch}",
			project: "app", branch: "main", dirname: "app-main",
			want: "ws-app-main",
		},
		{
			name:    "no placeholders",
			format:  "static-name",
			project: "app", branch: "main", dirname: "app-main",
			want: "static-name",
		},
		{
			name:    "repeated placeholders",
			format:  "{project}:{project}:{branch}",
			project: "app", branch: "main", dirname: "app-main",
			want: "app:app:main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatWorkspaceName(tt.format, tt.project, tt.branch, tt.dirname)
			if got != tt.want {
				t.Errorf("formatWorkspaceName(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestExpandUsesDefaultFormat(t *testing.T) {
	cfg := &Config{
		Name:  "app",
		Root:  "/dev/app",
		Agent: "claude",
		// WorkspaceFormat intentionally empty — should use default.
		Tmpl: Template{
			Surface: []TemplateSurface{
				{Name: "dev", Command: "$AGENT"},
			},
		},
	}
	worktrees := []Worktree{
		{Path: "/dev/app-main", Branch: "main"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default is {project}:{branch}
	if manifests[0].Name != "app:main" {
		t.Errorf("Name = %q, want %q", manifests[0].Name, "app:main")
	}
}

func TestExpandSessionNamesMatchWorkspaceName(t *testing.T) {
	cfg := &Config{
		Name:            "mono",
		Root:            "/dev/mono",
		Agent:           "claude",
		WorkspaceFormat: "{dirname}:{branch}",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{
					Name:    "dev",
					Command: "$AGENT",
					Split: []TemplateSplit{
						{Name: "git", Direction: "right", Command: "lazygit"},
					},
				},
			},
		},
	}
	worktrees := []Worktree{
		{Path: "/dev/mono-feat", Branch: "feat/auth"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := manifests[0]

	// Workspace name uses custom format.
	if m.Name != "mono-feat:feat-auth" {
		t.Errorf("Name = %q, want %q", m.Name, "mono-feat:feat-auth")
	}
	// Session names derive from workspace name.
	if m.Surface[0].Session != "mono-feat:feat-auth" {
		t.Errorf("Session = %q, want %q", m.Surface[0].Session, "mono-feat:feat-auth")
	}
	// Split session appends pane name.
	if m.Surface[0].Split[0].Session != "mono-feat:feat-auth:git" {
		t.Errorf("Split.Session = %q, want %q", m.Surface[0].Split[0].Session, "mono-feat:feat-auth:git")
	}
}

func TestExpandAllWorktreesCwd(t *testing.T) {
	cfg := &Config{
		Name:  "app",
		Root:  "/dev/app",
		Agent: "codex",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{
					Name:    "code",
					Command: "${AGENT}",
					Split: []TemplateSplit{
						{Name: "term", Direction: "down"},
					},
				},
			},
		},
	}
	worktrees := []Worktree{
		{Path: "/dev/app-main", Branch: "main"},
		{Path: "/dev/app-staging", Branch: "staging"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(manifests))
	}

	// Each manifest's surfaces and splits should have the worktree CWD.
	for i, m := range manifests {
		wantCwd := worktrees[i].Path
		for _, surf := range m.Surface {
			if surf.Cwd != wantCwd {
				t.Errorf("manifest[%d] surface cwd = %q, want %q", i, surf.Cwd, wantCwd)
			}
			for _, sp := range surf.Split {
				if sp.Cwd != wantCwd {
					t.Errorf("manifest[%d] split cwd = %q, want %q", i, sp.Cwd, wantCwd)
				}
			}
		}
	}

	// Agent variable should be expanded.
	if manifests[0].Surface[0].Command != "codex" {
		t.Errorf("Command = %q, want %q", manifests[0].Surface[0].Command, "codex")
	}
}

func TestExpandAllBare(t *testing.T) {
	cfg := &Config{
		Name:  "repo",
		Root:  "/dev/repo",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{{Name: "dev"}},
		},
	}
	worktrees := []Worktree{
		{Path: "/dev/repo.bare", Bare: true},
	}

	_, err := Expand(cfg, worktrees)
	if err == nil {
		t.Fatal("expected error when all worktrees are bare")
	}
}
