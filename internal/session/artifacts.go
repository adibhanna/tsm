package session

import (
	"os"
	"path/filepath"
)

func RemoveSessionArtifacts(cfg Config, sessionName string) error {
	paths := []string{
		daemonBuildInfoPath(cfg, sessionName),
		ClaudeStatuslinePath(cfg, sessionName),
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func RenameSessionArtifacts(cfg Config, oldName, newName string) error {
	pairs := [][2]string{
		{daemonBuildInfoPath(cfg, oldName), daemonBuildInfoPath(cfg, newName)},
		{ClaudeStatuslinePath(cfg, oldName), ClaudeStatuslinePath(cfg, newName)},
	}
	for _, pair := range pairs {
		if err := renameOptionalFile(pair[0], pair[1]); err != nil {
			return err
		}
	}
	return nil
}

func renameOptionalFile(oldPath, newPath string) error {
	if _, err := os.Stat(oldPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}
