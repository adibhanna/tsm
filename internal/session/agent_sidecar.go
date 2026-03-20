package session

import (
	"os"
	"path/filepath"
)

func claudeStatuslineDir(cfg Config) string {
	return filepath.Join(cfg.LogDir, "claude-statusline")
}

func ClaudeStatuslinePath(cfg Config, sessionName string) string {
	return filepath.Join(claudeStatuslineDir(cfg), sessionName+".json")
}

func WriteClaudeStatusline(cfg Config, sessionName string, data []byte) error {
	dir := claudeStatuslineDir(cfg)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := ClaudeStatuslinePath(cfg, sessionName)
	tmp, err := os.CreateTemp(dir, sessionName+".*.tmp")
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
