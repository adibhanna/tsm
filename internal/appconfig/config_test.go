package appconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathUsesOverride(t *testing.T) {
	getenv := func(key string) string {
		if key == DefaultConfigPathEnv {
			return "/tmp/custom-tsm.toml"
		}
		return ""
	}
	got, err := resolvePath(getenv, func() (string, error) {
		return "/Users/test", nil
	})
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if got != "/tmp/custom-tsm.toml" {
		t.Fatalf("resolvePath() = %q, want override path", got)
	}
}

func TestResolvePathUsesXDGConfigHome(t *testing.T) {
	getenv := func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return "/tmp/xdg"
		}
		return ""
	}
	got, err := resolvePath(getenv, func() (string, error) {
		return "/Users/test", nil
	})
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if got != "/tmp/xdg/tsm/config.toml" {
		t.Fatalf("resolvePath() = %q, want xdg config path", got)
	}
}

func TestLoadParsesTUIOverrides(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	content := `
[tui]
mode = "simplified"
keymap = "palette"
show_help = false

[tui.keymaps.default]
detach = ["x"]

[tui.keymaps.palette]
copy_command = ["ctrl+k"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(func(key string) string {
		if key == DefaultConfigPathEnv {
			return configPath
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TUI.Mode != "simplified" {
		t.Fatalf("cfg.TUI.Mode = %q, want simplified", cfg.TUI.Mode)
	}
	if cfg.TUI.Keymap != "palette" {
		t.Fatalf("cfg.TUI.Keymap = %q, want palette", cfg.TUI.Keymap)
	}
	if cfg.TUI.ShowHelp == nil || *cfg.TUI.ShowHelp {
		t.Fatalf("cfg.TUI.ShowHelp = %#v, want false", cfg.TUI.ShowHelp)
	}
	if got := cfg.TUI.Keymaps["default"]["detach"]; len(got) != 1 || got[0] != "x" {
		t.Fatalf("default detach bindings = %#v, want [x]", got)
	}
	if got := cfg.TUI.Keymaps["palette"]["copy_command"]; len(got) != 1 || got[0] != "ctrl+k" {
		t.Fatalf("palette copy bindings = %#v, want [ctrl+k]", got)
	}
}

func TestLoadMissingConfigReturnsDefaults(t *testing.T) {
	cfg, err := Load(func(key string) string {
		if key == DefaultConfigPathEnv {
			return filepath.Join(t.TempDir(), "missing.toml")
		}
		return ""
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TUI.Keymaps["default"] == nil || cfg.TUI.Keymaps["palette"] == nil {
		t.Fatalf("expected empty keymap override maps, got %#v", cfg.TUI.Keymaps)
	}
}

func TestInstallDefaultWritesTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsm.toml")

	got, err := InstallDefault(func(key string) string {
		if key == DefaultConfigPathEnv {
			return path
		}
		return ""
	}, false)
	if err != nil {
		t.Fatalf("InstallDefault() error = %v", err)
	}
	if got != path {
		t.Fatalf("InstallDefault() path = %q, want %q", got, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed config: %v", err)
	}
	if !strings.Contains(string(data), "[tui.keymaps.default]") {
		t.Fatalf("installed config missing expected template contents: %q", string(data))
	}
}

func TestInstallDefaultRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsm.toml")
	if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	_, err := InstallDefault(func(key string) string {
		if key == DefaultConfigPathEnv {
			return path
		}
		return ""
	}, false)
	if err == nil {
		t.Fatal("InstallDefault() error = nil, want existing-file failure")
	}
}

func TestInstallDefaultOverwritesWithForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tsm.toml")
	if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	_, err := InstallDefault(func(key string) string {
		if key == DefaultConfigPathEnv {
			return path
		}
		return ""
	}, true)
	if err != nil {
		t.Fatalf("InstallDefault(..., true) error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read forced config: %v", err)
	}
	if strings.Contains(string(data), "existing") {
		t.Fatalf("expected force install to overwrite existing file, got %q", string(data))
	}
}
