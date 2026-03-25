package project

import "testing"

func TestExpandNestedSplits(t *testing.T) {
	cfg := &Config{
		Name:  "app",
		Root:  "/dev/app",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{
					Name:    "dev",
					Command: "$AGENT",
					Split: []TemplateSplit{
						{
							Name:      "editor",
							Direction: "right",
							Command:   "nvim",
							Split: []TemplateSplit{
								{
									Name:      "git",
									Direction: "down",
									Command:   "lazygit",
								},
							},
						},
					},
				},
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
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}

	m := manifests[0]
	surf := m.Surface[0]

	// Surface: agent pane
	if surf.Command != "claude" {
		t.Errorf("surface command = %q, want %q", surf.Command, "claude")
	}

	// First split: editor (right)
	if len(surf.Split) != 1 {
		t.Fatalf("expected 1 top-level split, got %d", len(surf.Split))
	}
	editor := surf.Split[0]
	if editor.Name != "editor" {
		t.Errorf("split name = %q, want %q", editor.Name, "editor")
	}
	if editor.Session != "app:main:editor" {
		t.Errorf("split session = %q, want %q", editor.Session, "app:main:editor")
	}
	if editor.Direction != "right" {
		t.Errorf("split direction = %q, want %q", editor.Direction, "right")
	}
	if editor.Command != "nvim" {
		t.Errorf("split command = %q, want %q", editor.Command, "nvim")
	}

	// Nested split: git (down, inside editor)
	if len(editor.Split) != 1 {
		t.Fatalf("expected 1 nested split, got %d", len(editor.Split))
	}
	git := editor.Split[0]
	if git.Name != "git" {
		t.Errorf("nested split name = %q, want %q", git.Name, "git")
	}
	if git.Session != "app:main:git" {
		t.Errorf("nested split session = %q, want %q", git.Session, "app:main:git")
	}
	if git.Direction != "down" {
		t.Errorf("nested split direction = %q, want %q", git.Direction, "down")
	}
	if git.Command != "lazygit" {
		t.Errorf("nested split command = %q, want %q", git.Command, "lazygit")
	}
	if git.Cwd != "/dev/app-main" {
		t.Errorf("nested split cwd = %q, want %q", git.Cwd, "/dev/app-main")
	}
}

func TestExpandNestedSplitSessionNames(t *testing.T) {
	cfg := &Config{
		Name:  "mono",
		Root:  "/dev/mono",
		Agent: "codex",
		Tmpl: Template{
			Surface: []TemplateSurface{
				{
					Name:    "dev",
					Command: "$AGENT",
					Split: []TemplateSplit{
						{
							Name:      "right-panel",
							Direction: "right",
							Split: []TemplateSplit{
								{Name: "tests", Direction: "down", Command: "npm test"},
								// Note: "tests" split has no further nesting
							},
						},
					},
				},
			},
		},
	}

	worktrees := []Worktree{
		{Path: "/dev/mono-main", Branch: "main"},
	}

	manifests, err := Expand(cfg, worktrees)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := manifests[0]
	// Session names should all use the workspace name as prefix.
	if m.Surface[0].Session != "mono:main" {
		t.Errorf("surface session = %q", m.Surface[0].Session)
	}
	rightPanel := m.Surface[0].Split[0]
	if rightPanel.Session != "mono:main:right-panel" {
		t.Errorf("right-panel session = %q", rightPanel.Session)
	}
	tests := rightPanel.Split[0]
	if tests.Session != "mono:main:tests" {
		t.Errorf("tests session = %q", tests.Session)
	}
}
