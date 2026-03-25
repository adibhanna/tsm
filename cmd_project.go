package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/adibhanna/tsm/internal/mux"
	"github.com/adibhanna/tsm/internal/project"
	"github.com/adibhanna/tsm/internal/session"
)

// collectSessions returns all session names from manifest surfaces and
// their splits (recursively).
func collectSessions(manifests []*mux.Manifest) []string {
	var names []string
	for _, m := range manifests {
		for _, surf := range m.Surface {
			names = append(names, surf.Session)
			names = append(names, collectSplitSessions(surf.Split)...)
		}
	}
	return names
}

func collectSplitSessions(splits []mux.ManifestSplit) []string {
	var names []string
	for _, sp := range splits {
		names = append(names, sp.Session)
		if len(sp.Split) > 0 {
			names = append(names, collectSplitSessions(sp.Split)...)
		}
	}
	return names
}

func cmdProject() {
	if len(os.Args) < 3 {
		printProjectUsage()
		os.Exit(1)
	}
	sub := os.Args[2]

	// Project commands require cmux.
	if sub != "help" && sub != "-h" && sub != "--help" {
		term := mux.DetectTerminal()
		if term.Backend != "cmux" {
			fmt.Fprintf(os.Stderr, "Error: tsm project requires cmux (detected: %s)\n", term.Name)
			os.Exit(1)
		}
	}

	switch sub {
	case "init":
		cmdProjectInit()
	case "open":
		cmdProjectOpen()
	case "list", "ls":
		cmdProjectList()
	case "close":
		cmdProjectClose()
	case "add":
		cmdProjectAdd()
	case "remove", "rm":
		cmdProjectRemove()
	case "next":
		cmdProjectSwitch(+1)
	case "prev":
		cmdProjectSwitch(-1)
	case "sidebar":
		cmdProjectSidebar()
	case "sync":
		cmdProjectSync()
	case "edit":
		cmdProjectEdit()
	case "help", "-h", "--help":
		printProjectUsage()
	default:
		fmt.Fprintf(os.Stderr, "tsm project: unknown subcommand %q\n", sub)
		printProjectUsage()
		os.Exit(1)
	}
}

func cmdProjectInit() {
	// tsm project init [path]
	dir, _ := os.Getwd()
	if len(os.Args) >= 4 {
		dir = os.Args[3]
	}

	if !project.IsGitRepo(dir) {
		fmt.Fprintf(os.Stderr, "Error: %q is not a git repository\n", dir)
		os.Exit(1)
	}

	// Determine project name from repo.
	name, err := project.RepoName(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Check if project already exists.
	if existing, _ := project.Load(name); existing != nil {
		fmt.Fprintf(os.Stderr, "Project %q already exists. Use 'tsm project sync %s' to update worktrees.\n", name, name)
		os.Exit(1)
	}

	// Detect worktrees.
	trees, err := project.DetectWorktrees(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error detecting worktrees: %v\n", err)
		os.Exit(1)
	}

	// Convert detected worktrees to config entries.
	var entries []project.WorktreeEntry
	for _, wt := range trees {
		if wt.Bare {
			continue
		}
		entries = append(entries, project.WorktreeEntry{
			Path:   wt.Path,
			Branch: wt.Branch,
		})
	}

	// Resolve absolute root path.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}

	cfg := &project.Config{
		Name:  name,
		Root:  absDir,
		Agent: "claude",
		Tmpl: project.Template{
			Surface: []project.TemplateSurface{
				{
					Name:    "dev",
					Command: "$AGENT",
					Split: []project.TemplateSplit{
						{
							Name:      "git",
							Direction: "right",
							Command:   "lazygit",
						},
					},
				},
			},
		},
		Trees: entries,
	}

	if err := project.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving project: %v\n", err)
		os.Exit(1)
	}

	projDir, _ := project.ProjectDir()
	fmt.Printf("Project %q created at %s/%s.toml\n", name, projDir, name)
	fmt.Printf("Worktrees: %d\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  %s (%s)\n", e.Branch, e.Path)
	}
	fmt.Printf("\nEdit the config to customize, then run: tsm project open %s\n", name)
}

// resolveWorktrees loads worktrees from config entries or auto-detects them,
// then optionally filters to a single branch.
func resolveWorktrees(cfg *project.Config, branchFilter string) ([]project.Worktree, error) {
	var worktrees []project.Worktree
	if len(cfg.Trees) > 0 {
		for _, e := range cfg.Trees {
			worktrees = append(worktrees, project.Worktree{
				Path:   e.Path,
				Branch: e.Branch,
			})
		}
	} else {
		var err error
		worktrees, err = project.DetectWorktrees(cfg.Root)
		if err != nil {
			return nil, fmt.Errorf("detecting worktrees: %w", err)
		}
	}

	if branchFilter != "" {
		sanitized := project.SanitizeBranch(branchFilter)
		var filtered []project.Worktree
		for _, wt := range worktrees {
			if wt.Branch == branchFilter || project.SanitizeBranch(wt.Branch) == sanitized {
				filtered = append(filtered, wt)
			}
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("no worktree matching branch %q", branchFilter)
		}
		worktrees = filtered
	}
	return worktrees, nil
}

func cmdProjectOpen() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm project open <name> [branch]")
		os.Exit(1)
	}
	name := os.Args[3]
	branchFilter := ""
	if len(os.Args) >= 5 {
		branchFilter = os.Args[4]
	}

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	worktrees, err := resolveWorktrees(cfg, branchFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Expand template into manifests.
	manifests, err := project.Expand(cfg, worktrees)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error expanding project: %v\n", err)
		os.Exit(1)
	}

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Open each worktree as its own workspace.
	for _, m := range manifests {
		fmt.Printf("Opening workspace %q ...\n", m.Name)
		if err := orch.OpenManifest(m); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening workspace %q: %v\n", m.Name, err)
			os.Exit(1)
		}
	}
	fmt.Printf("Project %q opened (%d workspaces)\n", name, len(manifests))
}

func cmdProjectClose() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm project close <name> [branch]")
		os.Exit(1)
	}
	name := os.Args[3]
	branchFilter := ""
	if len(os.Args) >= 5 {
		branchFilter = os.Args[4]
	}

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	worktrees, err := resolveWorktrees(cfg, branchFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Expand to get session names.
	manifests, err := project.Expand(cfg, worktrees)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error expanding project: %v\n", err)
		os.Exit(1)
	}

	// Kill all sessions from manifests (including nested splits).
	sessCfg := session.DefaultConfig()
	var killed int
	for _, name := range collectSessions(manifests) {
		if err := session.KillSession(sessCfg, name); err == nil {
			fmt.Printf("  killed session %q\n", name)
			killed++
		}
	}

	if branchFilter != "" {
		fmt.Printf("Closed %q branch %q (%d sessions killed)\n", name, branchFilter, killed)
	} else {
		fmt.Printf("Closed project %q (%d sessions killed)\n", name, killed)
	}
}

func cmdProjectAdd() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: tsm project add <name> <branch> [path]")
		os.Exit(1)
	}
	name := os.Args[3]
	branch := os.Args[4]

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Determine worktree path.
	var wtPath string
	if len(os.Args) >= 6 {
		// Explicit path provided.
		wtPath = os.Args[5]
	} else {
		// Auto: create worktree as sibling of root dir.
		rootDir := filepath.Dir(cfg.Root)
		safeBranch := project.SanitizeBranch(branch)
		wtPath = filepath.Join(rootDir, cfg.Name+"-"+safeBranch)
	}

	absPath, err := filepath.Abs(wtPath)
	if err != nil {
		absPath = wtPath
	}

	// Create the git worktree.
	cmd := exec.Command("git", "-C", cfg.Root, "worktree", "add", absPath, "-b", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Branch might already exist, try without -b.
		cmd = exec.Command("git", "-C", cfg.Root, "worktree", "add", absPath, branch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating worktree: %v\n", err)
			os.Exit(1)
		}
	}

	// Add to project config.
	cfg.Trees = append(cfg.Trees, project.WorktreeEntry{
		Path:   absPath,
		Branch: branch,
	})
	if err := project.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving project: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added worktree %q at %s\n", branch, absPath)
	fmt.Printf("Run 'tsm project open %s %s' to open it\n", name, project.SanitizeBranch(branch))
}

func cmdProjectRemove() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: tsm project remove <name> <branch>")
		os.Exit(1)
	}
	name := os.Args[3]
	branch := os.Args[4]
	sanitized := project.SanitizeBranch(branch)

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Find and remove the worktree entry.
	var removed *project.WorktreeEntry
	var remaining []project.WorktreeEntry
	for _, e := range cfg.Trees {
		if e.Branch == branch || project.SanitizeBranch(e.Branch) == sanitized {
			removed = &e
		} else {
			remaining = append(remaining, e)
		}
	}
	if removed == nil {
		fmt.Fprintf(os.Stderr, "No worktree matching branch %q in project %q\n", branch, name)
		os.Exit(1)
	}

	// Kill sessions for this worktree first.
	worktrees := []project.Worktree{{Path: removed.Path, Branch: removed.Branch}}
	if manifests, err := project.Expand(cfg, worktrees); err == nil {
		sessCfg := session.DefaultConfig()
		for _, name := range collectSessions(manifests) {
			_ = session.KillSession(sessCfg, name)
		}
	}

	// Remove the git worktree.
	cmd := exec.Command("git", "-C", cfg.Root, "worktree", "remove", removed.Path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: git worktree remove failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "The worktree entry will still be removed from the project config.")
	}

	// Update project config.
	cfg.Trees = remaining
	if err := project.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving project: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed worktree %q from project %q\n", branch, name)
}

func cmdProjectSidebar() {
	if len(os.Args) < 5 || os.Args[3] != "sync" {
		fmt.Fprintln(os.Stderr, "usage: tsm project sidebar sync <name>")
		os.Exit(1)
	}
	name := os.Args[4]

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	worktrees, err := resolveWorktrees(cfg, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	manifests, err := project.Expand(cfg, worktrees)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error expanding project: %v\n", err)
		os.Exit(1)
	}

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := mux.SidebarSyncManifests(orch.Backend, session.DefaultConfig(), manifests); err != nil {
		fmt.Fprintf(os.Stderr, "Error syncing sidebar: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Sidebar synced for project %q\n", name)
}

func cmdProjectSwitch(direction int) {
	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := orch.ProjectSwitch(direction); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdProjectList() {
	names, err := project.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(names) == 0 {
		fmt.Println("No projects configured.")
		fmt.Println("Run 'tsm project init [path]' to create one.")
		return
	}
	for _, n := range names {
		cfg, err := project.Load(n)
		if err != nil {
			fmt.Printf("  %s (error: %v)\n", n, err)
			continue
		}
		if len(cfg.Trees) == 0 {
			fmt.Printf("  %s  root=%s  agent=%s  worktrees=auto\n", n, cfg.Root, cfg.Agent)
		} else {
			fmt.Printf("  %s  root=%s  agent=%s  worktrees=%d\n", n, cfg.Root, cfg.Agent, len(cfg.Trees))
		}
	}
}

func cmdProjectSync() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm project sync <name>")
		os.Exit(1)
	}
	name := os.Args[3]

	cfg, err := project.Load(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	trees, err := project.DetectWorktrees(cfg.Root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error detecting worktrees: %v\n", err)
		os.Exit(1)
	}

	var entries []project.WorktreeEntry
	for _, wt := range trees {
		if wt.Bare {
			continue
		}
		entries = append(entries, project.WorktreeEntry{
			Path:   wt.Path,
			Branch: wt.Branch,
		})
	}
	cfg.Trees = entries

	if err := project.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving project: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Project %q synced (%d worktrees)\n", name, len(entries))
	for _, e := range entries {
		fmt.Printf("  %s (%s)\n", e.Branch, e.Path)
	}
}

func cmdProjectEdit() {
	dir, err := project.ProjectDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	target := dir
	if len(os.Args) >= 4 {
		name := os.Args[3]
		if err := mux.ValidateWorkspaceName(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid project name: %v\n", err)
			os.Exit(1)
		}
		target = filepath.Join(dir, name+".toml")
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, target)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printProjectUsage() {
	fmt.Print(`tsm project — worktree-aware project workspaces (cmux only)

Manage git worktree workflows as cmux workspaces. Each worktree gets its
own workspace with a configurable layout (e.g. agent + git split).

Usage:
  tsm project init [path]                 Detect repo + worktrees, create config
  tsm project open <name> [branch]        Open all (or one) worktree workspaces
  tsm project close <name> [branch]       Close all (or one) worktree sessions
  tsm project add <name> <branch> [path]  Add a new worktree
  tsm project remove <name> <branch>      Remove a worktree and its sessions
  tsm project next                        Switch to next worktree workspace
  tsm project prev                        Switch to previous worktree workspace
  tsm project sidebar sync <name>         Sync agent status to cmux sidebar
  tsm project list                        List configured projects
  tsm project sync <name>                 Re-scan worktrees from git
  tsm project edit [name]                 Open project config in $EDITOR
  tsm project help                        Show this help

Projects are defined as TOML configs in ~/.config/tsm/projects/
Session names follow the pattern: project:branch

Aliases:
  project=proj  remove=rm
`)
}
