package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Manifest describes a workspace layout: which surfaces exist,
// which tsm sessions they attach to, and how they are split.
type Manifest struct {
	Name    string            `toml:"name"`
	Version int               `toml:"version"`
	Startup string            `toml:"startup_session,omitempty"`
	Surface []ManifestSurface `toml:"surface"`
}

// ManifestSurface is a tab in the workspace.
type ManifestSurface struct {
	Name    string          `toml:"name"`
	Session string          `toml:"session"`
	Cwd     string          `toml:"cwd,omitempty"`
	Command string          `toml:"command,omitempty"`
	Split   []ManifestSplit `toml:"split,omitempty"`
}

// ManifestSplit describes a pane created by splitting a surface or another pane.
type ManifestSplit struct {
	Name      string `toml:"name"`
	Session   string `toml:"session"`
	Direction string `toml:"direction"` // left, right, up, down
	Cwd       string `toml:"cwd,omitempty"`
	Command   string `toml:"command,omitempty"`
}

// ManifestDir returns the directory where workspace manifests are stored.
func ManifestDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "tsm", "workspaces"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "tsm", "workspaces"), nil
}

// LoadManifest reads and parses a workspace manifest by name.
func LoadManifest(name string) (*Manifest, error) {
	dir, err := ManifestDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %q: %w", path, err)
	}
	if err := validateManifest(&m); err != nil {
		return nil, fmt.Errorf("validate manifest %q: %w", name, err)
	}
	return &m, nil
}

// SaveManifest writes a workspace manifest to disk.
func SaveManifest(m *Manifest) error {
	dir, err := ManifestDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	data, err := toml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	path := filepath.Join(dir, m.Name+".toml")
	tmp, err := os.CreateTemp(dir, m.Name+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// ListManifests returns the names of all saved workspace manifests.
func ListManifests() ([]string, error) {
	dir, err := ManifestDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, ok := strings.CutSuffix(e.Name(), ".toml"); ok {
			names = append(names, name)
		}
	}
	return names, nil
}

// ExpandPath expands ~ to the user's home directory.
func ExpandPath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

func validateManifest(m *Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("manifest name is required")
	}
	if strings.ContainsAny(m.Name, "/\\") || strings.Contains(m.Name, "..") || strings.HasPrefix(m.Name, ".") {
		return fmt.Errorf("manifest name %q contains unsafe characters", m.Name)
	}
	if m.Version != 1 {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if len(m.Surface) == 0 {
		return fmt.Errorf("manifest must have at least one surface")
	}
	seen := map[string]bool{}
	for i, s := range m.Surface {
		if s.Name == "" {
			return fmt.Errorf("surface[%d]: name is required", i)
		}
		if s.Session == "" {
			return fmt.Errorf("surface[%d] %q: session is required", i, s.Name)
		}
		if seen[s.Name] {
			return fmt.Errorf("duplicate surface name %q", s.Name)
		}
		seen[s.Name] = true

		for j, sp := range s.Split {
			if sp.Name == "" {
				return fmt.Errorf("surface[%d].split[%d]: name is required", i, j)
			}
			if sp.Session == "" {
				return fmt.Errorf("surface[%d].split[%d] %q: session is required", i, j, sp.Name)
			}
			if _, ok := ParseDirection(sp.Direction); !ok {
				return fmt.Errorf("surface[%d].split[%d] %q: invalid direction %q", i, j, sp.Name, sp.Direction)
			}
			if seen[sp.Name] {
				return fmt.Errorf("duplicate pane name %q", sp.Name)
			}
			seen[sp.Name] = true
		}
	}
	return nil
}
