package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Expand generates one mux.Manifest per worktree by stamping the project template.
// Each manifest becomes a cmux workspace.
func Expand(cfg *Config, worktrees []Worktree) ([]*mux.Manifest, error) {
	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no worktrees to expand")
	}
	if len(cfg.Tmpl.Surface) == 0 {
		return nil, fmt.Errorf("project template has no surfaces")
	}

	var manifests []*mux.Manifest
	for _, wt := range worktrees {
		if wt.Bare {
			continue
		}
		m, err := expandWorktree(cfg, wt)
		if err != nil {
			return nil, fmt.Errorf("worktree %q: %w", wt.Path, err)
		}
		manifests = append(manifests, m)
	}
	if len(manifests) == 0 {
		return nil, fmt.Errorf("no non-bare worktrees to expand")
	}
	return manifests, nil
}

// formatWorkspaceName applies the workspace_format template.
// Supported placeholders: {project}, {branch}, {dirname}
func formatWorkspaceName(format, project, branch, dirname string) string {
	r := strings.NewReplacer(
		"{project}", project,
		"{branch}", branch,
		"{dirname}", dirname,
	)
	return r.Replace(format)
}

func expandWorktree(cfg *Config, wt Worktree) (*mux.Manifest, error) {
	branch := wt.Branch
	if branch == "" {
		branch = "default"
	}
	safeBranch := SanitizeBranch(branch)
	dirname := filepath.Base(wt.Path)

	// Apply workspace name format.
	format := cfg.WorkspaceFormat
	if format == "" {
		format = DefaultWorkspaceFormat
	}
	wsName := formatWorkspaceName(format, cfg.Name, safeBranch, dirname)

	replacer := strings.NewReplacer(
		"$AGENT", cfg.Agent,
		"${AGENT}", cfg.Agent,
	)

	var surfaces []mux.ManifestSurface
	for _, ts := range cfg.Tmpl.Surface {
		sessionName := wsName
		if len(cfg.Tmpl.Surface) > 1 {
			sessionName = wsName + ":" + ts.Name
		}

		surf := mux.ManifestSurface{
			Name:    ts.Name,
			Session: sessionName,
			Cwd:     wt.Path,
			Command: replacer.Replace(ts.Command),
		}

		for _, tsp := range ts.Split {
			splitSession := wsName + ":" + tsp.Name
			surf.Split = append(surf.Split, mux.ManifestSplit{
				Name:      tsp.Name,
				Session:   splitSession,
				Direction: tsp.Direction,
				Cwd:       wt.Path,
				Command:   replacer.Replace(tsp.Command),
			})
		}

		surfaces = append(surfaces, surf)
	}

	return &mux.Manifest{
		Name:    wsName,
		Version: 1,
		Startup: surfaces[0].Session,
		Surface: surfaces,
	}, nil
}
