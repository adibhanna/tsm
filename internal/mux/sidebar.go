package mux

import (
	"fmt"
	"strings"

	"github.com/adibhanna/tsm/internal/engine"
	"github.com/adibhanna/tsm/internal/session"
)

// SidebarSync reads tsm session and agent state from a workspace manifest
// and pushes it to the mux backend's sidebar/status area.
func SidebarSync(backend Backend, sessCfg session.Config, workspaceName string) error {
	manifest, err := LoadManifest(workspaceName)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Collect all session names from the manifest.
	var sessionNames []string
	for _, surf := range manifest.Surface {
		sessionNames = append(sessionNames, surf.Session)
		sessionNames = append(sessionNames, collectManifestSplitSessions(surf.Split)...)
	}

	// Fetch live sessions and their process/agent info.
	allSessions, err := session.ListSessions(sessCfg)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	// Filter to sessions in this workspace.
	nameSet := make(map[string]bool, len(sessionNames))
	for _, n := range sessionNames {
		nameSet[n] = true
	}
	var wsSessions []session.Session
	for _, s := range allSessions {
		if nameSet[s.Name] {
			wsSessions = append(wsSessions, s)
		}
	}

	// Get agent info for workspace sessions.
	processInfo := engine.FetchProcessInfo(wsSessions)

	// Push per-session status to sidebar.
	var live, total int
	for _, name := range sessionNames {
		total++
		info, ok := processInfo[name]

		if !ok {
			// Session not running.
			_ = backend.SetStatus("tsm:"+name, "dead")
			continue
		}
		live++

		// Build status string from agent state.
		status := formatSessionStatus(name, info)
		_ = backend.SetStatus("tsm:"+name, status)
	}

	// Set overall workspace status.
	_ = backend.SetStatus("tsm", fmt.Sprintf("%d/%d sessions live", live, total))

	return nil
}

// collectManifestSplitSessions recursively collects session names from nested splits.
func collectManifestSplitSessions(splits []ManifestSplit) []string {
	var names []string
	for _, sp := range splits {
		names = append(names, sp.Session)
		if len(sp.Split) > 0 {
			names = append(names, collectManifestSplitSessions(sp.Split)...)
		}
	}
	return names
}

// SidebarSyncManifests syncs agent status for multiple manifests (e.g. all
// worktrees in a project). It fetches process info once for all sessions,
// then pushes per-workspace status to the sidebar.
func SidebarSyncManifests(backend Backend, sessCfg session.Config, manifests []*Manifest) error {
	// Collect all sessions across all manifests, grouped by workspace.
	type wsInfo struct {
		name     string
		sessions []string
	}
	var workspaces []wsInfo
	var allSessionNames []string
	for _, m := range manifests {
		var sessions []string
		for _, surf := range m.Surface {
			sessions = append(sessions, surf.Session)
			sessions = append(sessions, collectManifestSplitSessions(surf.Split)...)
		}
		workspaces = append(workspaces, wsInfo{name: m.Name, sessions: sessions})
		allSessionNames = append(allSessionNames, sessions...)
	}

	// Fetch live sessions once.
	allSessions, err := session.ListSessions(sessCfg)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	// Filter to project sessions.
	nameSet := make(map[string]bool, len(allSessionNames))
	for _, n := range allSessionNames {
		nameSet[n] = true
	}
	var projectSessions []session.Session
	for _, s := range allSessions {
		if nameSet[s.Name] {
			projectSessions = append(projectSessions, s)
		}
	}

	// Get agent info for all project sessions.
	processInfo := engine.FetchProcessInfo(projectSessions)

	// Push per-workspace status.
	var totalLive, totalAll int
	for _, ws := range workspaces {
		var live int
		var agentStates []string
		for _, name := range ws.sessions {
			totalAll++
			info, ok := processInfo[name]
			if !ok {
				continue
			}
			live++
			totalLive++
			status := formatSessionStatus(name, info)
			agentStates = append(agentStates, status)
			_ = backend.SetStatus("tsm:"+name, status)
		}

		// Workspace-level summary.
		wsSummary := fmt.Sprintf("%d/%d live", live, len(ws.sessions))
		if len(agentStates) > 0 {
			// Show the most interesting agent state.
			wsSummary = agentStates[0]
		}
		_ = backend.SetStatus("tsm:ws:"+ws.name, wsSummary)
	}

	// Overall project summary.
	_ = backend.SetStatus("tsm:project", fmt.Sprintf("%d/%d sessions live", totalLive, totalAll))

	return nil
}

// formatSessionStatus builds a concise status string for a session.
func formatSessionStatus(name string, info engine.ProcessInfo) string {
	if info.AgentKind == "" {
		return "active"
	}

	state := engine.DisplayAgentState(info.AgentState, info.AgentUpdated)

	var parts []string
	parts = append(parts, info.AgentKind)
	parts = append(parts, state)

	if info.AgentSummary != "" {
		summary := info.AgentSummary
		if len(summary) > 40 {
			summary = summary[:37] + "..."
		}
		parts = append(parts, summary)
	}

	return strings.Join(parts, " · ")
}
