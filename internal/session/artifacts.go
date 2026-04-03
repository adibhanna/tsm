package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SessionArtifact struct {
	Session string
	Kind    string
	Path    string
}

func RemoveSessionArtifacts(cfg Config, sessionName string) error {
	paths := []string{
		daemonBuildInfoPath(cfg, sessionName),
		ClaudeStatuslinePath(cfg, sessionName),
		GitMetaPath(cfg, sessionName),
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func RemoveSessionRuntimeFiles(cfg Config, names ...string) error {
	seen := make(map[string]struct{})
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if err := RemoveSessionArtifacts(cfg, name); err != nil {
			return err
		}
		for _, shell := range []string{"zsh", "bash", "fish"} {
			if err := os.RemoveAll(shellIntegrationDir(cfg, shell, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func RenameSessionArtifacts(cfg Config, oldName, newName string) error {
	pairs := [][2]string{
		{daemonBuildInfoPath(cfg, oldName), daemonBuildInfoPath(cfg, newName)},
		{ClaudeStatuslinePath(cfg, oldName), ClaudeStatuslinePath(cfg, newName)},
		{GitMetaPath(cfg, oldName), GitMetaPath(cfg, newName)},
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

func ListSessionArtifacts(cfg Config) ([]SessionArtifact, error) {
	type source struct {
		kind string
		dir  func(Config) string
	}
	sources := []source{
		{kind: "daemon-build", dir: daemonBuildDir},
		{kind: "claude-statusline", dir: claudeStatuslineDir},
		{kind: "git-meta", dir: gitMetaDir},
	}

	var artifacts []SessionArtifact
	for _, src := range sources {
		entries, err := os.ReadDir(src.dir(cfg))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if filepath.Ext(name) != ".json" {
				continue
			}
			sessionName := strings.TrimSuffix(name, ".json")
			artifacts = append(artifacts, SessionArtifact{
				Session: sessionName,
				Kind:    src.kind,
				Path:    filepath.Join(src.dir(cfg), name),
			})
		}
	}

	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].Session == artifacts[j].Session {
			return artifacts[i].Kind < artifacts[j].Kind
		}
		return artifacts[i].Session < artifacts[j].Session
	})
	return artifacts, nil
}
