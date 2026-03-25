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
		{"default format", "{project}:{branch}", "app", "main", "app-main", "app:main"},
		{"dirname:project:branch", "{dirname}:{project}:{branch}", "app", "feat-auth", "app-feat", "app-feat:app:feat-auth"},
		{"dirname only", "{dirname}", "app", "main", "app-main", "app-main"},
		{"dirname:branch", "{dirname}:{branch}", "app", "main", "app-main", "app-main:main"},
		{"project only", "{project}", "monolith", "main", "monolith", "monolith"},
		{"literal text preserved", "ws-{project}-{branch}", "app", "main", "app-main", "ws-app-main"},
		{"no placeholders", "static-name", "app", "main", "app-main", "static-name"},
		{"repeated placeholders", "{project}:{project}:{branch}", "app", "main", "app-main", "app:app:main"},
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
		Tmpl: Template{
			Surface: []TemplateSurface{
				{Name: "dev", Command: "$AGENT"},
			},
		},
	}
	worktrees := []Worktree{
		{Path: "/dev/app-main", Branch: "main"},
	}

	m, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Surface[0].Name != "app:main" {
		t.Errorf("tab name = %q, want %q", m.Surface[0].Name, "app:main")
	}
}

func TestExpandSessionNamesMatchTabName(t *testing.T) {
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

	m, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	surf := m.Surface[0]
	if surf.Name != "mono-feat:feat-auth" {
		t.Errorf("tab name = %q, want %q", surf.Name, "mono-feat:feat-auth")
	}
	if surf.Session != "mono-feat:feat-auth" {
		t.Errorf("session = %q, want %q", surf.Session, "mono-feat:feat-auth")
	}
	if surf.Split[0].Session != "mono-feat:feat-auth:git" {
		t.Errorf("split session = %q, want %q", surf.Split[0].Session, "mono-feat:feat-auth:git")
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

	m, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Surface) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(m.Surface))
	}

	expected := []string{"/dev/app-main", "/dev/app-staging"}
	for i, surf := range m.Surface {
		if surf.Cwd != expected[i] {
			t.Errorf("surface[%d] cwd = %q, want %q", i, surf.Cwd, expected[i])
		}
		for _, sp := range surf.Split {
			if sp.Cwd != expected[i] {
				t.Errorf("surface[%d] split cwd = %q, want %q", i, sp.Cwd, expected[i])
			}
		}
	}

	if m.Surface[0].Command != "codex" {
		t.Errorf("Command = %q, want %q", m.Surface[0].Command, "codex")
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
