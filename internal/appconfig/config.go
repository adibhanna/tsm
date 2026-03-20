package appconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const DefaultConfigPathEnv = "TSM_CONFIG_FILE"

type Config struct {
	Path  string
	TUI   TUIConfig
	Shell ShellConfig
}

type TUIConfig struct {
	Mode     string
	Keymap   string
	ShowHelp *bool
	Keymaps  map[string]map[string][]string
}

type ShellConfig struct {
	Shortcuts ShellShortcutConfig
}

type ShellShortcutConfig struct {
	Full    *string
	Palette *string
	Toggle  *string
}

type fileConfig struct {
	TUI   fileTUIConfig   `toml:"tui"`
	Shell fileShellConfig `toml:"shell"`
}

type fileTUIConfig struct {
	Mode     string           `toml:"mode"`
	Keymap   string           `toml:"keymap"`
	ShowHelp *bool            `toml:"show_help"`
	Keymaps  fileKeymapConfig `toml:"keymaps"`
}

type fileKeymapConfig struct {
	Default map[string][]string `toml:"default"`
	Palette map[string][]string `toml:"palette"`
}

type fileShellConfig struct {
	Shortcuts fileShellShortcutConfig `toml:"shortcuts"`
}

type fileShellShortcutConfig struct {
	Full    *string `toml:"full"`
	Palette *string `toml:"palette"`
	Toggle  *string `toml:"toggle"`
}

func Load(getenv func(string) string) (Config, error) {
	path, err := resolvePath(getenv, os.UserHomeDir)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Path: path,
		TUI: TUIConfig{
			Keymaps: map[string]map[string][]string{
				"default": {},
				"palette": {},
			},
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}

	var raw fileConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	cfg.TUI.Mode = strings.TrimSpace(raw.TUI.Mode)
	cfg.TUI.Keymap = strings.TrimSpace(raw.TUI.Keymap)
	cfg.TUI.ShowHelp = raw.TUI.ShowHelp
	cfg.TUI.Keymaps["default"] = cloneKeymap(raw.TUI.Keymaps.Default)
	cfg.TUI.Keymaps["palette"] = cloneKeymap(raw.TUI.Keymaps.Palette)
	if raw.Shell.Shortcuts.Full != nil {
		value := strings.TrimSpace(*raw.Shell.Shortcuts.Full)
		cfg.Shell.Shortcuts.Full = &value
	}
	if raw.Shell.Shortcuts.Palette != nil {
		value := strings.TrimSpace(*raw.Shell.Shortcuts.Palette)
		cfg.Shell.Shortcuts.Palette = &value
	}
	if raw.Shell.Shortcuts.Toggle != nil {
		value := strings.TrimSpace(*raw.Shell.Shortcuts.Toggle)
		cfg.Shell.Shortcuts.Toggle = &value
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", path, err)
	}
	return cfg, nil
}

func resolvePath(getenv func(string) string, homeDir func() (string, error)) (string, error) {
	if override := strings.TrimSpace(getenv(DefaultConfigPathEnv)); override != "" {
		return override, nil
	}
	if xdg := strings.TrimSpace(getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "tsm", "config.toml"), nil
	}
	home, err := homeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "tsm", "config.toml"), nil
}

func cloneKeymap(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return map[string][]string{}
	}
	dst := make(map[string][]string, len(src))
	for action, bindings := range src {
		dst[action] = append([]string(nil), bindings...)
	}
	return dst
}

func validateConfig(cfg Config) error {
	if cfg.TUI.Mode != "" {
		switch strings.ToLower(strings.TrimSpace(cfg.TUI.Mode)) {
		case "full", "simplified", "simple", "palette":
		default:
			return fmt.Errorf("unknown TUI mode %q", cfg.TUI.Mode)
		}
	}
	if cfg.TUI.Keymap != "" {
		switch strings.ToLower(strings.TrimSpace(cfg.TUI.Keymap)) {
		case "default", "palette":
		default:
			return fmt.Errorf("unknown keymap %q", cfg.TUI.Keymap)
		}
	}
	if err := validateShellShortcut("shell.shortcuts.full", cfg.Shell.Shortcuts.Full); err != nil {
		return err
	}
	if err := validateShellShortcut("shell.shortcuts.palette", cfg.Shell.Shortcuts.Palette); err != nil {
		return err
	}
	if err := validateShellShortcut("shell.shortcuts.toggle", cfg.Shell.Shortcuts.Toggle); err != nil {
		return err
	}
	if err := validateShellShortcutConflicts(cfg.Shell.Shortcuts); err != nil {
		return err
	}
	return nil
}

func validateShellShortcut(field string, value *string) error {
	if value == nil {
		return nil
	}
	raw := strings.TrimSpace(strings.ToLower(*value))
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "ctrl+") {
		return fmt.Errorf("%s: unsupported shortcut %q (expected ctrl+<key> or empty)", field, *value)
	}
	key := []rune(strings.TrimPrefix(raw, "ctrl+"))
	if len(key) != 1 || strings.TrimSpace(string(key[0])) == "" {
		return fmt.Errorf("%s: unsupported shortcut %q (expected ctrl+<key> or empty)", field, *value)
	}
	return nil
}

func validateShellShortcutConflicts(shortcuts ShellShortcutConfig) error {
	owners := map[string]string{}
	fields := []struct {
		name  string
		value *string
	}{
		{name: "shell.shortcuts.full", value: shortcuts.Full},
		{name: "shell.shortcuts.palette", value: shortcuts.Palette},
		{name: "shell.shortcuts.toggle", value: shortcuts.Toggle},
	}
	for _, field := range fields {
		if field.value == nil {
			continue
		}
		raw := strings.TrimSpace(strings.ToLower(*field.value))
		if raw == "" {
			continue
		}
		if prev, ok := owners[raw]; ok {
			return fmt.Errorf("%s conflicts with %s (both use %q)", field.name, prev, *field.value)
		}
		owners[raw] = field.name
	}
	return nil
}
