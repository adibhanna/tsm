package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Expand generates a single mux.Manifest with one surface (tab) per worktree.
// Each tab contains the template layout (splits) applied to that worktree's CWD.
func Expand(cfg *Config, worktrees []Worktree) (*mux.Manifest, error) {
	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no worktrees to expand")
	}
	if len(cfg.Tmpl.Surface) == 0 {
		return nil, fmt.Errorf("project template has no surfaces")
	}

	replacer := strings.NewReplacer(
		"$AGENT", cfg.Agent,
		"${AGENT}", cfg.Agent,
	)

	tabFormat := cfg.WorkspaceFormat
	if tabFormat == "" {
		tabFormat = DefaultWorkspaceFormat
	}

	var surfaces []mux.ManifestSurface
	var startupSession string
	for _, wt := range worktrees {
		if wt.Bare {
			continue
		}
		branch := wt.Branch
		if branch == "" {
			branch = "default"
		}
		safeBranch := SanitizeBranch(branch)
		dirname := filepath.Base(wt.Path)
		tabName := formatWorkspaceName(tabFormat, cfg.Name, safeBranch, dirname)

		// Each template surface becomes a tab for this worktree.
		// For a single-surface template, the tab name is the worktree name.
		// For multi-surface templates, tabs are named "worktree:surface".
		for _, ts := range cfg.Tmpl.Surface {
			surfName := tabName
			sessionName := tabName
			if len(cfg.Tmpl.Surface) > 1 {
				surfName = tabName + ":" + ts.Name
				sessionName = tabName + ":" + ts.Name
			}

			surf := mux.ManifestSurface{
				Name:    surfName,
				Session: sessionName,
				Cwd:     wt.Path,
				Command: replacer.Replace(ts.Command),
			}
			surf.Split = expandSplits(tabName, wt.Path, ts.Split, replacer)
			surfaces = append(surfaces, surf)

			if startupSession == "" {
				startupSession = sessionName
			}
		}
	}
	if len(surfaces) == 0 {
		return nil, fmt.Errorf("no non-bare worktrees to expand")
	}

	return &mux.Manifest{
		Name:    cfg.Name,
		Version: 1,
		Startup: startupSession,
		Surface: surfaces,
	}, nil
}

// formatWorkspaceName applies the tab/workspace name format template.
// Supported placeholders: {project}, {branch}, {dirname}
func formatWorkspaceName(format, project, branch, dirname string) string {
	r := strings.NewReplacer(
		"{project}", project,
		"{branch}", branch,
		"{dirname}", dirname,
	)
	return r.Replace(format)
}

func expandSplits(tabName, cwd string, splits []TemplateSplit, replacer *strings.Replacer) []mux.ManifestSplit {
	var result []mux.ManifestSplit
	for _, tsp := range splits {
		ms := mux.ManifestSplit{
			Name:      tsp.Name,
			Session:   tabName + ":" + tsp.Name,
			Direction: tsp.Direction,
			Cwd:       cwd,
			Command:   replacer.Replace(tsp.Command),
		}
		if len(tsp.Split) > 0 {
			ms.Split = expandSplits(tabName, cwd, tsp.Split, replacer)
		}
		result = append(result, ms)
	}
	return result
}
