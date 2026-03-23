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
		for _, sp := range surf.Split {
			sessionNames = append(sessionNames, sp.Session)
		}
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
