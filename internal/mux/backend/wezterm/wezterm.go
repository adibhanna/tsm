// Package wezterm wraps the wezterm CLI to implement mux.Backend.
//
// WezTerm concepts mapping:
//   - wezterm workspace → mux.Workspace
//   - wezterm tab       → mux.Surface
//   - wezterm pane      → mux.Pane
//
// WezTerm does not have a sidebar, so SetStatus and Log are no-ops.
package wezterm

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Backend implements mux.Backend using the wezterm CLI.
type Backend struct {
	bin string
}

// New returns a wezterm backend.
func New() *Backend {
	bin, _ := exec.LookPath("wezterm")
	return &Backend{bin: bin}
}

func (b *Backend) Name() string { return "wezterm" }

func (b *Backend) Available() bool {
	if b.bin == "" {
		return false
	}
	_, err := b.run("cli", "list", "--format", "json")
	return err == nil
}

// --- Internal types ---

type paneInfo struct {
	WindowID  int    `json:"window_id"`
	TabID     int    `json:"tab_id"`
	PaneID    int    `json:"pane_id"`
	Workspace string `json:"workspace"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
}

type clientInfo struct {
	Workspace    string `json:"workspace"`
	FocusedPaneID int   `json:"focused_pane_id"`
}

func (b *Backend) listPanes() ([]paneInfo, error) {
	out, err := b.run("cli", "list", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	var panes []paneInfo
	if err := json.Unmarshal([]byte(out), &panes); err != nil {
		return nil, fmt.Errorf("parse list: %w", err)
	}
	return panes, nil
}

// --- Workspaces ---

func (b *Backend) ListWorkspaces() ([]mux.Workspace, error) {
	panes, err := b.listPanes()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ws []mux.Workspace
	for _, p := range panes {
		name := p.Workspace
		if name == "" {
			name = "default"
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		ws = append(ws, mux.Workspace{ID: name, Name: name})
	}
	return ws, nil
}

func (b *Backend) CreateWorkspace(name string) (mux.Workspace, error) {
	out, err := b.run("cli", "spawn", "--new-window", "--workspace", name)
	if err != nil {
		return mux.Workspace{}, fmt.Errorf("spawn workspace: %w", err)
	}
	_ = out // output is the new pane ID
	return mux.Workspace{ID: name, Name: name}, nil
}

func (b *Backend) SelectWorkspace(id string) error {
	// WezTerm doesn't have a direct workspace-switch CLI command.
	// Find a pane in the target workspace and activate it.
	panes, err := b.listPanes()
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p.Workspace == id {
			_, err := b.run("cli", "activate-pane", "--pane-id", strconv.Itoa(p.PaneID))
			return err
		}
	}
	return fmt.Errorf("no panes in workspace %q", id)
}

// --- Surfaces (tabs) ---

func (b *Backend) CreateSurface(workspaceID string) (mux.Surface, error) {
	args := []string{"cli", "spawn"}
	if workspaceID != "" {
		// Find a window in this workspace to spawn the tab into.
		panes, err := b.listPanes()
		if err == nil {
			for _, p := range panes {
				if p.Workspace == workspaceID {
					args = append(args, "--window-id", strconv.Itoa(p.WindowID))
					break
				}
			}
		}
	}
	out, err := b.run(args...)
	if err != nil {
		return mux.Surface{}, fmt.Errorf("spawn tab: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Surface{ID: id, WorkspaceID: workspaceID}, nil
}

func (b *Backend) CloseSurface(id string) error {
	// Close all panes in the tab. Find panes with matching tab ID.
	_, err := b.run("cli", "kill-pane", "--pane-id", id)
	return err
}

// --- Panes ---

func (b *Backend) SplitPane(workspaceID string, dir mux.Direction) (mux.Pane, error) {
	args := []string{"cli", "split-pane"}
	switch dir {
	case mux.DirLeft:
		args = append(args, "--left")
	case mux.DirRight:
		args = append(args, "--right")
	case mux.DirUp:
		args = append(args, "--top")
	case mux.DirDown:
		args = append(args, "--bottom")
	}
	out, err := b.run(args...)
	if err != nil {
		return mux.Pane{}, fmt.Errorf("split-pane: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Pane{ID: id, SurfaceID: id}, nil
}

func (b *Backend) ClosePane(id string) error {
	_, err := b.run("cli", "kill-pane", "--pane-id", id)
	return err
}

func (b *Backend) FocusPane(id string) error {
	_, err := b.run("cli", "activate-pane", "--pane-id", id)
	return err
}

func (b *Backend) GetFocusedPane() (mux.Pane, error) {
	out, err := b.run("cli", "list-clients", "--format", "json")
	if err != nil {
		return mux.Pane{}, fmt.Errorf("list-clients: %w", err)
	}
	var clients []clientInfo
	if err := json.Unmarshal([]byte(out), &clients); err != nil {
		return mux.Pane{}, fmt.Errorf("parse list-clients: %w", err)
	}
	if len(clients) == 0 {
		return mux.Pane{}, fmt.Errorf("no clients found")
	}
	id := strconv.Itoa(clients[0].FocusedPaneID)
	return mux.Pane{ID: id, SurfaceID: id}, nil
}

// --- I/O ---

func (b *Backend) SendText(paneID string, text string) error {
	args := []string{"cli", "send-text", "--no-paste"}
	if paneID != "" {
		args = append(args, "--pane-id", paneID)
	}
	args = append(args, text)
	_, err := b.run(args...)
	return err
}

func (b *Backend) SendTextToWorkspace(workspaceID, surfaceID, text string) error {
	args := []string{"cli", "send-text", "--no-paste"}
	if surfaceID != "" {
		args = append(args, "--pane-id", surfaceID)
	}
	args = append(args, text)
	_, err := b.run(args...)
	return err
}

func (b *Backend) ListPaneSurfaces(workspaceID string) ([]string, error) {
	panes, err := b.listPanes()
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, p := range panes {
		if workspaceID != "" && p.Workspace != workspaceID {
			continue
		}
		refs = append(refs, strconv.Itoa(p.PaneID))
	}
	return refs, nil
}

// --- Screen ---

func (b *Backend) ReadScreen(workspaceID, surfaceID string) (string, error) {
	args := []string{"cli", "get-text"}
	if surfaceID != "" {
		args = append(args, "--pane-id", surfaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return "", fmt.Errorf("get-text: %w", err)
	}
	return out, nil
}

// --- Layout ---

func (b *Backend) GetTree(workspaceID string) (mux.LayoutNode, error) {
	// Intentionally unimplemented for V1.
	return mux.LayoutNode{Type: "workspace"}, nil
}

// --- Navigation ---

func (b *Backend) FocusNextPane() error {
	_, err := b.run("cli", "activate-pane-direction", "Next")
	return err
}

func (b *Backend) FocusPreviousPane() error {
	_, err := b.run("cli", "activate-pane-direction", "Prev")
	return err
}

// --- Sidebar (no-op for WezTerm) ---

func (b *Backend) SetStatus(key, value string) error { return nil }
func (b *Backend) Log(msg string) error              { return nil }

// --- Helpers ---

func (b *Backend) run(args ...string) (string, error) {
	if b.bin == "" {
		return "", fmt.Errorf("wezterm binary not found")
	}
	cmd := exec.Command(b.bin, args...)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		if stderr != "" {
			return "", fmt.Errorf("%s %s: %w: %s", b.bin, strings.Join(args, " "), err, stderr)
		}
		return "", fmt.Errorf("%s %s: %w", b.bin, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
