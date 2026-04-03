package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func gitMetaDir(cfg Config) string {
	return filepath.Join(cfg.LogDir, "git-meta")
}

// GitMetaPath returns the path to the git metadata sidecar file for a session.
func GitMetaPath(cfg Config, sessionName string) string {
	return filepath.Join(gitMetaDir(cfg), sessionName+".json")
}

// WriteGitMeta atomically writes git context as a JSON sidecar file.
func WriteGitMeta(cfg Config, sessionName string, ctx GitContext) error {
	dir := gitMetaDir(cfg)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(ctx)
	if err != nil {
		return err
	}
	path := GitMetaPath(cfg, sessionName)
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

// ReadGitMeta reads git context from a JSON sidecar file.
func ReadGitMeta(cfg Config, sessionName string) (GitContext, error) {
	data, err := os.ReadFile(GitMetaPath(cfg, sessionName))
	if err != nil {
		return GitContext{}, err
	}
	var ctx GitContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return GitContext{}, err
	}
	return ctx, nil
}
