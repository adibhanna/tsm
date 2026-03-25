package project

import (
	"testing"
)

func TestExpand(t *testing.T) {
	cfg := &Config{
		Name:  "monolith",
		Root:  "/Users/dev/monolith",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{
					Name:    "dev",
					Command: "$AGENT",
					Split: []TemplateSplit{
						{
							Name:      "git",
							Direction: "right",
							Command:   "lazygit",
						},
					},
				},
			},
		},
	}

	worktrees := []Worktree{
		{Path: "/Users/dev/monolith", Branch: "main"},
		{Path: "/Users/dev/monolith-feat-auth", Branch: "feat/auth"},
		{Path: "/Users/dev/monolith-fix-perf", Branch: "fix/perf"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 3 {
		t.Fatalf("expected 3 manifests, got %d", len(manifests))
	}

	// Default format: project:branch
	m := manifests[0]
	if m.Name != "monolith:main" {
		t.Errorf("manifest[0].Name = %q, want %q", m.Name, "monolith:main")
	}
	if len(m.Surface) != 1 {
		t.Fatalf("manifest[0] has %d surfaces, want 1", len(m.Surface))
	}
	surf := m.Surface[0]
	if surf.Session != "monolith:main" {
		t.Errorf("surface.Session = %q, want %q", surf.Session, "monolith:main")
	}
	if surf.Cwd != "/Users/dev/monolith" {
		t.Errorf("surface.Cwd = %q, want %q", surf.Cwd, "/Users/dev/monolith")
	}
	if surf.Command != "claude" {
		t.Errorf("surface.Command = %q, want %q", surf.Command, "claude")
	}
	if len(surf.Split) != 1 {
		t.Fatalf("surface has %d splits, want 1", len(surf.Split))
	}
	split := surf.Split[0]
	if split.Session != "monolith:main:git" {
		t.Errorf("split.Session = %q, want %q", split.Session, "monolith:main:git")
	}
	if split.Direction != "right" {
		t.Errorf("split.Direction = %q, want %q", split.Direction, "right")
	}
	if split.Command != "lazygit" {
		t.Errorf("split.Command = %q, want %q", split.Command, "lazygit")
	}

	m2 := manifests[1]
	if m2.Name != "monolith:feat-auth" {
		t.Errorf("manifest[1].Name = %q, want %q", m2.Name, "monolith:feat-auth")
	}

	m3 := manifests[2]
	if m3.Name != "monolith:fix-perf" {
		t.Errorf("manifest[2].Name = %q, want %q", m3.Name, "monolith:fix-perf")
	}
}

func TestExpandCustomFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{"dirname:project:branch", "{dirname}:{project}:{branch}", "app-main:app:main"},
		{"dirname:branch", "{dirname}:{branch}", "app-main:main"},
		{"dirname only", "{dirname}", "app-main"},
		{"project/branch", "{project}:{branch}", "app:main"},
		{"empty uses default", "", "app:main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Name:            "app",
				Root:            "/dev/app",
				Agent:           "claude",
				WorkspaceFormat: tt.format,
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
			if manifests[0].Name != tt.want {
				t.Errorf("Name = %q, want %q", manifests[0].Name, tt.want)
			}
		})
	}
}

func TestExpandSkipsBare(t *testing.T) {
	cfg := &Config{
		Name:  "repo",
		Root:  "/dev/repo",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{Name: "dev", Command: "$AGENT"},
			},
		},
	}

	worktrees := []Worktree{
		{Path: "/dev/repo.bare", Bare: true},
		{Path: "/dev/repo-main", Branch: "main"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest (bare skipped), got %d", len(manifests))
	}
	if manifests[0].Name != "repo:main" {
		t.Errorf("Name = %q, want %q", manifests[0].Name, "repo:main")
	}
}

func TestExpandNoWorktrees(t *testing.T) {
	cfg := &Config{
		Name:  "repo",
		Root:  "/dev/repo",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{Name: "dev"},
			},
		},
	}

	_, err := Expand(cfg, nil)
	if err == nil {
		t.Fatal("expected error for empty worktrees")
	}
}

func TestExpandMultipleSurfaces(t *testing.T) {
	cfg := &Config{
		Name:  "app",
		Root:  "/dev/app",
		Agent: "codex",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{Name: "code", Command: "${AGENT}"},
				{Name: "server", Command: "npm run dev"},
			},
		},
	}

	worktrees := []Worktree{
		{Path: "/dev/app", Branch: "main"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	m := manifests[0]
	if len(m.Surface) != 2 {
		t.Fatalf("expected 2 surfaces, got %d", len(m.Surface))
	}
	if m.Surface[0].Session != "app:main:code" {
		t.Errorf("surface[0].Session = %q, want %q", m.Surface[0].Session, "app:main:code")
	}
	if m.Surface[0].Command != "codex" {
		t.Errorf("surface[0].Command = %q, want %q", m.Surface[0].Command, "codex")
	}
	if m.Surface[1].Session != "app:main:server" {
		t.Errorf("surface[1].Session = %q, want %q", m.Surface[1].Session, "app:main:server")
	}
	if m.Surface[1].Command != "npm run dev" {
		t.Errorf("surface[1].Command = %q, want %q", m.Surface[1].Command, "npm run dev")
	}
}
