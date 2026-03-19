package appconfig

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// Keep this embedded template in sync with config/tsm/config.toml.
//
//go:embed default_config.toml
var defaultTemplate string

func DefaultTemplate() string {
	return defaultTemplate
}

func InstallDefault(getenv func(string) string, force bool) (string, error) {
	path, err := resolvePath(getenv, os.UserHomeDir)
	if err != nil {
		return "", err
	}

	if !force {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("config already exists at %s", path)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat config %q: %w", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return "", fmt.Errorf("mkdir config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(defaultTemplate), 0644); err != nil {
		return "", fmt.Errorf("write config %q: %w", path, err)
	}
	return path, nil
}
