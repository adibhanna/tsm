package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// DefaultWorkspaceFormat is the default workspace naming pattern.
const DefaultWorkspaceFormat = "{project}:{branch}"

// Config describes a project: a git repo with worktrees and a layout template.
type Config struct {
	Name            string          `toml:"name"`
	Root            string          `toml:"root"`
	Agent           string          `toml:"agent,omitempty"`            // "claude", "codex", etc.
	WorkspaceFormat string          `toml:"workspace_format,omitempty"` // e.g. "{project}:{branch}", "{dirname}:{branch}"
	Tmpl            Template        `toml:"template"`
	Trees           []WorktreeEntry `toml:"worktree,omitempty"` // explicit overrides; empty = auto-detect
}

// Template describes the layout stamped per worktree.
type Template struct {
	Surface []TemplateSurface `toml:"surface"`
}

// TemplateSurface is a tab template.
type TemplateSurface struct {
	Name    string          `toml:"name"`
	Command string          `toml:"command,omitempty"`
	Split   []TemplateSplit `toml:"split,omitempty"`
}

// TemplateSplit is a pane template within a tab. Splits can be nested
// to create complex layouts (e.g. agent left | editor top-right + git bottom-right).
type TemplateSplit struct {
	Name      string          `toml:"name"`
	Direction string          `toml:"direction"`
	Command   string          `toml:"command,omitempty"`
	Split     []TemplateSplit `toml:"split,omitempty"`
}

// WorktreeEntry is an explicit worktree override in the config.
type WorktreeEntry struct {
	Path   string `toml:"path"`
	Branch string `toml:"branch,omitempty"`
}

// ProjectDir returns the directory where project configs are stored.
func ProjectDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "tsm", "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "tsm", "projects"), nil
}

// Load reads a project config by name.
func Load(name string) (*Config, error) {
	dir, err := ProjectDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project %q: %w", path, err)
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse project %q: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("validate project %q: %w", name, err)
	}
	return &c, nil
}

// Save writes a project config to disk.
func Save(c *Config) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("validate project: %w", err)
	}
	dir, err := ProjectDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal project: %w", err)
	}
	path := filepath.Join(dir, c.Name+".toml")
	tmp, err := os.CreateTemp(dir, c.Name+".*.tmp")
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

// List returns the names of all saved project configs.
func List() ([]string, error) {
	dir, err := ProjectDir()
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

// Validate checks a project config for errors.
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("project name is required")
	}
	if strings.ContainsAny(c.Name, "/\\") || strings.Contains(c.Name, "..") || strings.HasPrefix(c.Name, ".") {
		return fmt.Errorf("project name %q contains unsafe characters", c.Name)
	}
	if c.Root == "" {
		return fmt.Errorf("project root is required")
	}
	if len(c.Tmpl.Surface) == 0 {
		return fmt.Errorf("template must have at least one surface")
	}
	for i, s := range c.Tmpl.Surface {
		if s.Name == "" {
			return fmt.Errorf("template.surface[%d]: name is required", i)
		}
	}
	return nil
}

// ProjectNameFromWorkspace extracts the project name from a workspace name.
// For the default format "{project}:{branch}", this is the prefix before the first ":".
// Returns the project name, or empty string if not detected.
func ProjectNameFromWorkspace(wsName string) string {
	if i := strings.Index(wsName, ":"); i > 0 {
		return wsName[:i]
	}
	return ""
}

// SanitizeBranch converts a branch name to a safe session/workspace name component.
// "feat/auth" → "feat-auth", "refs/heads/main" → "refs-heads-main"
func SanitizeBranch(branch string) string {
	s := strings.ReplaceAll(branch, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "..", "-")
	s = strings.TrimLeft(s, ".-")
	if s == "" {
		s = "default"
	}
	return s
}
