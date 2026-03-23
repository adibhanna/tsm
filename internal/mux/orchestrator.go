package mux

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adibhanna/tsm/internal/session"
)

// Orchestrator coordinates a mux backend with tsm sessions.
type Orchestrator struct {
	Backend Backend
	SessCfg session.Config
}

// Open loads a workspace manifest and creates all surfaces and panes,
// auto-creating tsm sessions and attaching them.
func (o *Orchestrator) Open(workspaceName string) error {
	manifest, err := LoadManifest(workspaceName)
	if err != nil {
		return fmt.Errorf("load workspace: %w", err)
	}

	// Find or create the cmux workspace.
	wsID, err := o.findOrCreateWorkspace(manifest.Name)
	if err != nil {
		return err
	}

	if err := o.Backend.SelectWorkspace(wsID); err != nil {
		return fmt.Errorf("select workspace: %w", err)
	}
	time.Sleep(150 * time.Millisecond)

	// Get the default terminal surface that was created with the workspace.
	surfaces, err := o.Backend.ListPaneSurfaces(wsID)
	if err != nil {
		return fmt.Errorf("list workspace surfaces: %w", err)
	}
	if len(surfaces) == 0 {
		return fmt.Errorf("workspace %q has no terminal surfaces", manifest.Name)
	}
	defaultSurface := surfaces[0]

	// Phase 1: Create all sessions, surfaces, and splits.
	// Collect surface→session mappings for attaching later.
	type attachJob struct {
		surfaceRef  string
		sessionName string
		command     string // optional command to run after attach
	}
	var jobs []attachJob

	for i, surf := range manifest.Surface {
		// Don't pass command to ensureSession — it would be used as the shell binary.
		// Instead we send it as text after attaching.
		if err := o.ensureSession(surf.Session, surf.Cwd, ""); err != nil {
			return fmt.Errorf("surface %q: %w", surf.Name, err)
		}

		var surfaceRef string
		if i == 0 {
			surfaceRef = defaultSurface
		} else {
			if _, err := o.Backend.CreateSurface(wsID); err != nil {
				return fmt.Errorf("surface %q: create: %w", surf.Name, err)
			}
			time.Sleep(150 * time.Millisecond)

			newSurfaces, err := o.Backend.ListPaneSurfaces(wsID)
			if err != nil {
				return fmt.Errorf("surface %q: list surfaces: %w", surf.Name, err)
			}
			surfaceRef = findNewSurface(surfaces, newSurfaces)
			surfaces = newSurfaces
		}
		jobs = append(jobs, attachJob{surfaceRef: surfaceRef, sessionName: surf.Session, command: surf.Command})

		for _, sp := range surf.Split {
			dir, ok := ParseDirection(sp.Direction)
			if !ok {
				return fmt.Errorf("surface %q split %q: invalid direction %q", surf.Name, sp.Name, sp.Direction)
			}
			if err := o.ensureSession(sp.Session, sp.Cwd, ""); err != nil {
				return fmt.Errorf("split %q: %w", sp.Name, err)
			}

			beforeSurfaces, _ := o.Backend.ListPaneSurfaces(wsID)
			if _, err := o.Backend.SplitPane(wsID, dir); err != nil {
				return fmt.Errorf("split %q: create: %w", sp.Name, err)
			}
			time.Sleep(150 * time.Millisecond)

			afterSurfaces, err := o.Backend.ListPaneSurfaces(wsID)
			if err != nil {
				return fmt.Errorf("split %q: list surfaces: %w", sp.Name, err)
			}
			newSurf := findNewSurface(beforeSurfaces, afterSurfaces)
			if newSurf == "" {
				return fmt.Errorf("split %q: could not find new terminal surface", sp.Name)
			}
			surfaces = afterSurfaces
			jobs = append(jobs, attachJob{surfaceRef: newSurf, sessionName: sp.Session, command: sp.Command})
		}
	}

	// Phase 2: Attach all sessions into their target surfaces.
	// Done after all layout is created so splits don't interfere with sends.
	for _, job := range jobs {
		if err := o.sendAttach(wsID, job.surfaceRef, job.sessionName); err != nil {
			return fmt.Errorf("attach %q into %s: %w", job.sessionName, job.surfaceRef, err)
		}
	}

	// Phase 3: Send startup commands into sessions that have them.
	// Brief delay to let sessions finish attaching.
	hasCommands := false
	for _, job := range jobs {
		if job.command != "" {
			hasCommands = true
			break
		}
	}
	if hasCommands {
		time.Sleep(500 * time.Millisecond)
		for _, job := range jobs {
			if job.command != "" {
				if err := o.Backend.SendTextToWorkspace(wsID, job.surfaceRef, job.command+"\n"); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to send command to surface %s: %v\n", job.surfaceRef, err)
				}
			}
		}
	}

	_ = o.Backend.Log(fmt.Sprintf("workspace %q opened", manifest.Name))
	return nil
}

// Split creates a new split in the focused pane and attaches a tsm session.
func (o *Orchestrator) Split(dir Direction, sessionName string, cmd []string) error {
	cmdStr := ""
	if len(cmd) > 0 {
		cmdStr = cmd[0]
	}
	if err := o.ensureSession(sessionName, "", cmdStr); err != nil {
		return err
	}

	pane, err := o.Backend.SplitPane("", dir)
	if err != nil {
		return fmt.Errorf("split pane: %w", err)
	}
	time.Sleep(150 * time.Millisecond)

	// Split/TabNew are run from within the target workspace, so SendText works.
	text := fmt.Sprintf("tsm attach %s\n", sessionName)
	return o.Backend.SendText(pane.SurfaceID, text)
}

// TabNew creates a new surface (tab) and attaches a tsm session.
func (o *Orchestrator) TabNew(sessionName string, cmd []string) error {
	cmdStr := ""
	if len(cmd) > 0 {
		cmdStr = cmd[0]
	}
	if err := o.ensureSession(sessionName, "", cmdStr); err != nil {
		return err
	}

	s, err := o.Backend.CreateSurface("")
	if err != nil {
		return fmt.Errorf("create surface: %w", err)
	}
	time.Sleep(150 * time.Millisecond)

	text := fmt.Sprintf("tsm attach %s\n", sessionName)
	return o.Backend.SendText(s.ID, text)
}

// Save writes a workspace manifest based on the current tsm session list.
// If a manifest already exists, it updates session liveness info.
// If the backend is available, it also reads pane screens for session detection.
// This command works without cmux access — it uses tsm's own data.
func (o *Orchestrator) Save(workspaceName string) error {
	if workspaceName == "" {
		return fmt.Errorf("workspace name is required")
	}

	// Try to load existing manifest and update it.
	existing, _ := LoadManifest(workspaceName)
	if existing != nil {
		// Manifest exists — just re-save it (preserves layout info).
		return SaveManifest(existing)
	}

	// No existing manifest — build one from live tsm sessions.
	sessions, err := session.ListSessions(o.SessCfg)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		return fmt.Errorf("no tsm sessions running")
	}

	var surfaces []ManifestSurface
	for _, s := range sessions {
		surfaces = append(surfaces, ManifestSurface{
			Name:    s.Name,
			Session: s.Name,
			Cwd:     s.StartedIn,
		})
	}

	manifest := &Manifest{
		Name:    workspaceName,
		Version: 1,
		Surface: surfaces,
	}

	if err := SaveManifest(manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// Restore is an alias for Open — it loads a manifest and recreates the layout.
// The difference is semantic: restore implies the workspace previously existed.
func (o *Orchestrator) Restore(workspaceName string) error {
	return o.Open(workspaceName)
}

// WorkspaceStatus holds diagnostic info about a workspace.
type WorkspaceStatus struct {
	WorkspaceName string
	Sessions      []SessionStatus
	HasManifest   bool
}

// SessionStatus holds diagnostic info about a session in the workspace.
type SessionStatus struct {
	Name    string
	Live    bool
	Clients int
}

// Doctor inspects a workspace manifest and checks session health.
// This command works without cmux access — it uses tsm's own data.
func (o *Orchestrator) Doctor(workspaceName string) (*WorkspaceStatus, error) {
	manifest, err := LoadManifest(workspaceName)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}

	status := &WorkspaceStatus{
		WorkspaceName: manifest.Name,
		HasManifest:   true,
	}

	// Collect all session names from the manifest.
	var sessionNames []string
	for _, surf := range manifest.Surface {
		sessionNames = append(sessionNames, surf.Session)
		for _, sp := range surf.Split {
			sessionNames = append(sessionNames, sp.Session)
		}
	}

	// Check each session's health.
	for _, name := range sessionNames {
		ss := SessionStatus{Name: name}
		path := o.SessCfg.SocketPath(name)
		if session.IsSocket(path) {
			if info, err := session.ProbeSession(path); err == nil {
				ss.Live = true
				ss.Clients = int(info.ClientsLen)
			}
		}
		status.Sessions = append(status.Sessions, ss)
	}

	return status, nil
}

// detectSessionFromScreen parses terminal screen text to find a tsm session name.
// It looks for the "[tsm:sessionname]" prompt prefix that tsm sets.
func detectSessionFromScreen(screen string) string {
	// Look for [tsm:NAME] pattern anywhere in the screen text.
	const prefix = "[tsm:"
	idx := strings.LastIndex(screen, prefix)
	if idx < 0 {
		return ""
	}
	rest := screen[idx+len(prefix):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	name := rest[:end]
	if name == "" {
		return ""
	}
	return name
}

// findOrCreateWorkspace locates an existing workspace by name or creates one.
func (o *Orchestrator) findOrCreateWorkspace(name string) (string, error) {
	workspaces, err := o.Backend.ListWorkspaces()
	if err != nil {
		return "", fmt.Errorf("list workspaces: %w", err)
	}
	for _, w := range workspaces {
		if w.Name == name {
			return w.ID, nil
		}
	}
	w, err := o.Backend.CreateWorkspace(name)
	if err != nil {
		return "", fmt.Errorf("create workspace %q: %w", name, err)
	}
	return w.ID, nil
}

// ensureSession creates a tsm session if it doesn't already exist.
func (o *Orchestrator) ensureSession(name, cwd, command string) error {
	path := o.SessCfg.SocketPath(name)
	if session.IsSocket(path) {
		if _, err := session.ProbeSession(path); err == nil {
			return nil
		}
		session.CleanStaleSocket(path)
	}

	var shellCmd []string
	if command != "" {
		shellCmd = []string{command}
	}

	// Temporarily change CWD for the daemon spawn, then restore it.
	if cwd != "" {
		expanded := ExpandPath(cwd)
		origDir, _ := os.Getwd()
		if err := os.Chdir(expanded); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not chdir to %q: %v\n", expanded, err)
		} else if origDir != "" {
			defer os.Chdir(origDir)
		}
	}

	if err := session.SpawnDaemon(name, shellCmd); err != nil {
		return fmt.Errorf("create session %q: %w", name, err)
	}
	return nil
}

// sendAttach sends "tsm attach <session>\n" into a surface within a workspace.
// Uses workspace-scoped send so it works even when called from a different workspace.
func (o *Orchestrator) sendAttach(wsID, surfaceRef, sessionName string) error {
	text := fmt.Sprintf("tsm attach %s\n", sessionName)
	return o.Backend.SendTextToWorkspace(wsID, surfaceRef, text)
}

// findNewSurface returns the first surface in newList that wasn't in oldList.
func findNewSurface(oldList, newList []string) string {
	old := make(map[string]bool, len(oldList))
	for _, s := range oldList {
		old[s] = true
	}
	for _, s := range newList {
		if !old[s] {
			return s
		}
	}
	return ""
}
