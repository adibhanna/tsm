package mux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	m := &Manifest{
		Name:    "test-workspace",
		Version: 1,
		Startup: "editor",
		Surface: []ManifestSurface{
			{
				Name:    "editor",
				Session: "editor",
				Cwd:     "~/projects/myapp",
				Command: "nvim",
				Split: []ManifestSplit{
					{
						Name:      "server",
						Session:   "server",
						Direction: "right",
						Cwd:       "~/projects/myapp",
						Command:   "npm run dev",
					},
				},
			},
			{
				Name:    "logs",
				Session: "logs",
				Command: "tail -f /var/log/app.log",
			},
		},
	}

	if err := SaveManifest(m); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Verify the file exists.
	path := filepath.Join(dir, "tsm", "workspaces", "test-workspace.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest file not created: %v", err)
	}

	// Load it back.
	loaded, err := LoadManifest("test-workspace")
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	if loaded.Name != m.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, m.Name)
	}
	if loaded.Version != m.Version {
		t.Errorf("Version = %d, want %d", loaded.Version, m.Version)
	}
	if loaded.Startup != m.Startup {
		t.Errorf("Startup = %q, want %q", loaded.Startup, m.Startup)
	}
	if len(loaded.Surface) != 2 {
		t.Fatalf("len(Surface) = %d, want 2", len(loaded.Surface))
	}
	if loaded.Surface[0].Name != "editor" {
		t.Errorf("Surface[0].Name = %q, want %q", loaded.Surface[0].Name, "editor")
	}
	if loaded.Surface[0].Session != "editor" {
		t.Errorf("Surface[0].Session = %q, want %q", loaded.Surface[0].Session, "editor")
	}
	if len(loaded.Surface[0].Split) != 1 {
		t.Fatalf("len(Surface[0].Split) = %d, want 1", len(loaded.Surface[0].Split))
	}
	sp := loaded.Surface[0].Split[0]
	if sp.Name != "server" {
		t.Errorf("Split[0].Name = %q, want %q", sp.Name, "server")
	}
	if sp.Direction != "right" {
		t.Errorf("Split[0].Direction = %q, want %q", sp.Direction, "right")
	}
}

func TestManifestValidation(t *testing.T) {
	tests := []struct {
		name    string
		m       Manifest
		wantErr bool
	}{
		{
			name:    "empty name",
			m:       Manifest{Version: 1, Surface: []ManifestSurface{{Name: "a", Session: "a"}}},
			wantErr: true,
		},
		{
			name:    "bad version",
			m:       Manifest{Name: "x", Version: 2, Surface: []ManifestSurface{{Name: "a", Session: "a"}}},
			wantErr: true,
		},
		{
			name:    "no surfaces",
			m:       Manifest{Name: "x", Version: 1},
			wantErr: true,
		},
		{
			name:    "surface missing session",
			m:       Manifest{Name: "x", Version: 1, Surface: []ManifestSurface{{Name: "a"}}},
			wantErr: true,
		},
		{
			name:    "duplicate surface names",
			m:       Manifest{Name: "x", Version: 1, Surface: []ManifestSurface{{Name: "a", Session: "a"}, {Name: "a", Session: "b"}}},
			wantErr: true,
		},
		{
			name: "split bad direction",
			m: Manifest{Name: "x", Version: 1, Surface: []ManifestSurface{
				{Name: "a", Session: "a", Split: []ManifestSplit{{Name: "b", Session: "b", Direction: "diagonal"}}},
			}},
			wantErr: true,
		},
		{
			name: "valid minimal",
			m:    Manifest{Name: "x", Version: 1, Surface: []ManifestSurface{{Name: "a", Session: "a"}}},
		},
		{
			name: "valid with splits",
			m: Manifest{Name: "x", Version: 1, Surface: []ManifestSurface{
				{Name: "a", Session: "a", Split: []ManifestSplit{{Name: "b", Session: "b", Direction: "right"}}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateManifest(&tt.m)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateManifest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListManifests(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	wsDir := filepath.Join(dir, "tsm", "workspaces")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work.toml", "personal.toml", "nottoml.txt"} {
		if err := os.WriteFile(filepath.Join(wsDir, name), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	names, err := ListManifests()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("len(names) = %d, want 2", len(names))
	}

	// Names should be sorted by directory order (alphabetical on most systems).
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["work"] || !found["personal"] {
		t.Errorf("expected work and personal, got %v", names)
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input string
		want  string
	}{
		{"~/foo", filepath.Join(home, "foo")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		if got := ExpandPath(tt.input); got != tt.want {
			t.Errorf("ExpandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestManifestNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	_, err := LoadManifest("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent manifest")
	}
}
