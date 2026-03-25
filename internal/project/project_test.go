package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid",
			cfg: Config{
				Name:  "myproject",
				Root:  "/dev/repo",
				Agent: "claude",
				Tmpl: Template{
					Surface: []TemplateSurface{{Name: "dev"}},
				},
			},
		},
		{
			name: "empty name",
			cfg: Config{
				Root: "/dev/repo",
				Tmpl: Template{
					Surface: []TemplateSurface{{Name: "dev"}},
				},
			},
			wantErr: true,
		},
		{
			name: "unsafe name with slash",
			cfg: Config{
				Name: "my/project",
				Root: "/dev/repo",
				Tmpl: Template{
					Surface: []TemplateSurface{{Name: "dev"}},
				},
			},
			wantErr: true,
		},
		{
			name: "empty root",
			cfg: Config{
				Name: "myproject",
				Tmpl: Template{
					Surface: []TemplateSurface{{Name: "dev"}},
				},
			},
			wantErr: true,
		},
		{
			name: "no surfaces",
			cfg: Config{
				Name: "myproject",
				Root: "/dev/repo",
			},
			wantErr: true,
		},
		{
			name: "surface missing name",
			cfg: Config{
				Name: "myproject",
				Root: "/dev/repo",
				Tmpl: Template{
					Surface: []TemplateSurface{{Command: "vim"}},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &Config{
		Name:  "testproj",
		Root:  "/dev/repo",
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
		Trees: []WorktreeEntry{
			{Path: "/dev/repo", Branch: "main"},
			{Path: "/dev/repo-feat", Branch: "feat/auth"},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists.
	path := filepath.Join(dir, "tsm", "projects", "testproj.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not found: %v", err)
	}

	// Load it back.
	loaded, err := Load("testproj")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Name != "testproj" {
		t.Errorf("Name = %q, want %q", loaded.Name, "testproj")
	}
	if loaded.Root != "/dev/repo" {
		t.Errorf("Root = %q, want %q", loaded.Root, "/dev/repo")
	}
	if loaded.Agent != "claude" {
		t.Errorf("Agent = %q, want %q", loaded.Agent, "claude")
	}
	if len(loaded.Tmpl.Surface) != 1 {
		t.Fatalf("Template has %d surfaces, want 1", len(loaded.Tmpl.Surface))
	}
	if len(loaded.Trees) != 2 {
		t.Fatalf("Trees has %d entries, want 2", len(loaded.Trees))
	}
}

func TestSaveAndLoadWithWorkspaceFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &Config{
		Name:            "myapp",
		Root:            "/dev/myapp",
		Agent:           "codex",
		WorkspaceFormat: "{dirname}:{project}:{branch}",
		Tmpl: Template{
			Surface: []TemplateSurface{{Name: "dev"}},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load("myapp")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.WorkspaceFormat != "{dirname}:{project}:{branch}" {
		t.Errorf("WorkspaceFormat = %q, want %q", loaded.WorkspaceFormat, "{dirname}:{project}:{branch}")
	}
	if loaded.Agent != "codex" {
		t.Errorf("Agent = %q, want %q", loaded.Agent, "codex")
	}
}

func TestSaveAndLoadEmptyWorkspaceFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := &Config{
		Name:  "minimal",
		Root:  "/dev/minimal",
		Agent: "claude",
		Tmpl: Template{
			Surface: []TemplateSurface{{Name: "dev"}},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load("minimal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Empty format should be omitted and default to "".
	if loaded.WorkspaceFormat != "" {
		t.Errorf("WorkspaceFormat = %q, want empty", loaded.WorkspaceFormat)
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Initially empty.
	names, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty list, got %v", names)
	}

	// Save two projects.
	for _, name := range []string{"alpha", "beta"} {
		cfg := &Config{
			Name:  name,
			Root:  "/dev/" + name,
			Agent: "claude",
			Tmpl: Template{
				Surface: []TemplateSurface{{Name: "dev"}},
			},
		}
		if err := Save(cfg); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}

	names, err = List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(names))
	}
}
