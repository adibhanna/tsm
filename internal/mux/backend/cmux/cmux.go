// Package cmux wraps the cmux CLI to implement mux.Backend.
package cmux

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Backend implements mux.Backend using the cmux CLI.
type Backend struct {
	// bin is the path to the cmux binary (resolved once).
	bin string
	// socket is the cmux socket path, captured at construction time so it
	// works even from shells that don't have CMUX_SOCKET_PATH set (e.g.
	// inside tsm sessions spawned before cmux started).
	socket string
}

// New returns a cmux backend. It resolves the binary path and socket path
// eagerly so they work even if the env changes later.
func New() *Backend {
	bin, _ := exec.LookPath("cmux")
	socket := resolveSocket()
	return &Backend{bin: bin, socket: socket}
}

// resolveSocket finds the cmux socket path from env vars, a cached file,
// or the default location. It also caches the result so tsm sessions
// spawned later (which lack CMUX_SOCKET_PATH) can still find it.
func resolveSocket() string {
	// 1. Explicit env var.
	if s := os.Getenv("CMUX_SOCKET_PATH"); s != "" {
		cacheSocketPath(s)
		return s
	}
	if s := os.Getenv("CMUX_SOCKET"); s != "" {
		cacheSocketPath(s)
		return s
	}

	// 2. Read from cached file (written by a previous tsm invocation that had the env).
	if s := readCachedSocketPath(); s != "" {
		return s
	}

	// 3. Default location.
	return "/tmp/cmux.sock"
}

func socketCachePath() string {
	dir := os.Getenv("TMPDIR")
	if dir == "" {
		dir = "/tmp"
	}
	return fmt.Sprintf("%s/tsm-cmux-socket-%d", strings.TrimRight(dir, "/"), os.Getuid())
}

func cacheSocketPath(socket string) {
	_ = os.WriteFile(socketCachePath(), []byte(socket), 0o600)
}

func readCachedSocketPath() string {
	data, err := os.ReadFile(socketCachePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (b *Backend) Name() string { return "cmux" }

func (b *Backend) Available() bool {
	if b.bin == "" {
		return false
	}
	out, err := b.run("ping")
	if err != nil {
		return false
	}
	return strings.Contains(out, "pong")
}

// UnavailableReason returns a human-readable reason why the backend is unavailable.
func (b *Backend) UnavailableReason() string {
	if b.bin == "" {
		return "cmux binary not found in PATH"
	}
	_, err := b.run("ping")
	if err == nil {
		return ""
	}
	errStr := err.Error()
	if strings.Contains(errStr, "Access denied") {
		return "cmux access denied — run tsm mux commands from a non-attached cmux pane, or set CMUX_SOCKET_MODE=allowAll"
	}
	if strings.Contains(errStr, "Failed to write") {
		return "cannot reach cmux socket — run tsm mux commands from a cmux pane"
	}
	return errStr
}

// --- Workspaces ---

// cmux --json list-workspaces returns:
//
//	{"workspaces": [{"ref": "workspace:1", "title": "name", ...}], "window_ref": "..."}
func (b *Backend) ListWorkspaces() ([]mux.Workspace, error) {
	out, err := b.run("--json", "list-workspaces")
	if err != nil {
		return nil, fmt.Errorf("list-workspaces: %w", err)
	}
	var resp struct {
		Workspaces []struct {
			Ref   string `json:"ref"`
			Title string `json:"title"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parse list-workspaces: %w", err)
	}
	ws := make([]mux.Workspace, len(resp.Workspaces))
	for i, r := range resp.Workspaces {
		ws[i] = mux.Workspace{ID: r.Ref, Name: r.Title}
	}
	return ws, nil
}

// cmux new-workspace returns plain text: "OK workspace:10"
func (b *Backend) CreateWorkspace(name string) (mux.Workspace, error) {
	out, err := b.run("new-workspace")
	if err != nil {
		return mux.Workspace{}, fmt.Errorf("new-workspace: %w", err)
	}

	// Parse "OK workspace:10" format.
	ref := strings.TrimPrefix(out, "OK ")
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return mux.Workspace{}, fmt.Errorf("new-workspace: unexpected output %q", out)
	}

	// Rename the workspace to our desired name.
	if _, err := b.run("rename-workspace", "--workspace", ref, name); err != nil {
		// Non-fatal: workspace was created, just naming failed.
		return mux.Workspace{ID: ref, Name: name}, nil
	}
	return mux.Workspace{ID: ref, Name: name}, nil
}

func (b *Backend) SelectWorkspace(id string) error {
	_, err := b.run("select-workspace", "--workspace", id)
	if err != nil {
		return fmt.Errorf("select-workspace: %w", err)
	}
	return nil
}

// ListPaneSurfaces returns the terminal surface refs across all panes in a workspace.
// It uses list-panes (not list-pane-surfaces which only returns one pane's surfaces).
func (b *Backend) ListPaneSurfaces(workspaceID string) ([]string, error) {
	args := []string{"--json", "list-panes"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return nil, fmt.Errorf("list-panes: %w", err)
	}
	var resp struct {
		Panes []struct {
			SurfaceRefs        []string `json:"surface_refs"`
			SelectedSurfaceRef string   `json:"selected_surface_ref"`
		} `json:"panes"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("parse list-panes: %w", err)
	}
	var refs []string
	for _, p := range resp.Panes {
		// Use the selected (active) terminal surface in each pane.
		if p.SelectedSurfaceRef != "" {
			refs = append(refs, p.SelectedSurfaceRef)
		} else if len(p.SurfaceRefs) > 0 {
			refs = append(refs, p.SurfaceRefs[0])
		}
	}
	return refs, nil
}

// --- Surfaces ---

// cmux new-surface creates a new tab. The output format may vary;
// we use identify afterward to find the focused surface.
func (b *Backend) CreateSurface(workspaceID string) (mux.Surface, error) {
	args := []string{"new-surface", "--type", "terminal"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	if _, err := b.run(args...); err != nil {
		return mux.Surface{}, fmt.Errorf("new-surface: %w", err)
	}

	// The new surface is auto-focused. Use identify to get its ref.
	focused, err := b.GetFocusedPane()
	if err != nil {
		return mux.Surface{ID: "", WorkspaceID: workspaceID}, nil
	}
	return mux.Surface{ID: focused.SurfaceID, WorkspaceID: workspaceID}, nil
}

func (b *Backend) CloseSurface(id string) error {
	_, err := b.run("close-surface", "--surface", id)
	if err != nil {
		return fmt.Errorf("close-surface: %w", err)
	}
	return nil
}

// --- Panes ---

// cmux --json new-split right returns:
//
//	{"surface_ref": "surface:18", "pane_ref": "pane:12", "workspace_ref": "...", "type": "terminal", "window_ref": "..."}
func (b *Backend) SplitPane(workspaceID string, dir mux.Direction) (mux.Pane, error) {
	dirStr := dir.String()
	args := []string{"--json", "new-split", dirStr}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return mux.Pane{}, fmt.Errorf("new-split %s: %w", dirStr, err)
	}
	var resp struct {
		SurfaceRef string `json:"surface_ref"`
		PaneRef    string `json:"pane_ref"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return mux.Pane{}, fmt.Errorf("parse new-split: %w", err)
	}
	return mux.Pane{ID: resp.PaneRef, SurfaceID: resp.SurfaceRef}, nil
}

func (b *Backend) ClosePane(id string) error {
	// cmux has no direct close-pane; close the surface in the pane.
	_, err := b.run("close-surface", "--panel", id)
	if err != nil {
		return fmt.Errorf("close-pane (via close-surface): %w", err)
	}
	return nil
}

func (b *Backend) FocusPane(id string) error {
	_, err := b.run("focus-pane", "--pane", id)
	if err != nil {
		return fmt.Errorf("focus-pane: %w", err)
	}
	return nil
}

// cmux --json identify returns:
//
//	{"focused": {"pane_ref": "pane:10", "surface_ref": "surface:17", ...}, "caller": {...}}
func (b *Backend) GetFocusedPane() (mux.Pane, error) {
	out, err := b.run("--json", "identify")
	if err != nil {
		return mux.Pane{}, fmt.Errorf("identify: %w", err)
	}
	var resp struct {
		Focused struct {
			PaneRef    string `json:"pane_ref"`
			SurfaceRef string `json:"surface_ref"`
		} `json:"focused"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return mux.Pane{}, fmt.Errorf("parse identify: %w", err)
	}
	return mux.Pane{
		ID:        resp.Focused.PaneRef,
		SurfaceID: resp.Focused.SurfaceRef,
	}, nil
}

// --- I/O ---

func (b *Backend) SendText(paneID string, text string) error {
	args := []string{"send"}
	if paneID != "" {
		if strings.HasPrefix(paneID, "surface:") {
			args = append(args, "--surface", paneID)
		} else if strings.HasPrefix(paneID, "pane:") {
			args = []string{"send-panel", "--panel", paneID}
		} else {
			args = append(args, "--surface", paneID)
		}
	}
	args = append(args, "--", text)
	_, err := b.run(args...)
	if err != nil {
		return fmt.Errorf("send-text: %w", err)
	}
	return nil
}

// SendTextToWorkspace sends text to a specific surface within a workspace.
func (b *Backend) SendTextToWorkspace(workspaceID, surfaceID, text string) error {
	args := []string{"send"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	if surfaceID != "" {
		args = append(args, "--surface", surfaceID)
	}
	args = append(args, "--", text)
	_, err := b.run(args...)
	if err != nil {
		return fmt.Errorf("send-text: %w", err)
	}
	return nil
}

// --- Screen ---

// cmux read-screen reads the terminal text from a surface.
func (b *Backend) ReadScreen(workspaceID, surfaceID string) (string, error) {
	args := []string{"read-screen"}
	if workspaceID != "" {
		args = append(args, "--workspace", workspaceID)
	}
	if surfaceID != "" {
		args = append(args, "--surface", surfaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return "", fmt.Errorf("read-screen: %w", err)
	}
	return out, nil
}

// --- Layout ---

func (b *Backend) GetTree(workspaceID string) (mux.LayoutNode, error) {
	// Intentionally unimplemented for V1: the tree JSON structure varies
	// across cmux versions and full layout save/restore is not yet needed.
	// Return a minimal stub so callers don't break.
	return mux.LayoutNode{Type: "workspace"}, nil
}

// --- Navigation ---

func (b *Backend) FocusNextPane() error {
	_, err := b.run("next-pane")
	return err
}

func (b *Backend) FocusPreviousPane() error {
	_, err := b.run("last-pane")
	return err
}

// --- Sidebar ---

func (b *Backend) SetStatus(key, value string) error {
	_, err := b.run("set-status", key, value)
	if err != nil {
		return fmt.Errorf("set-status: %w", err)
	}
	return nil
}

func (b *Backend) Log(msg string) error {
	_, err := b.run("log", "--", msg)
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}
	return nil
}

// --- Helpers ---

func (b *Backend) run(args ...string) (string, error) {
	if b.bin == "" {
		return "", fmt.Errorf("cmux binary not found")
	}
	// Prepend --socket so cmux CLI can reach the server even when
	// CMUX_SOCKET_PATH isn't in the current env (e.g. inside tsm sessions).
	if b.socket != "" {
		args = append([]string{"--socket", b.socket}, args...)
	}
	cmd := exec.Command(b.bin, args...)
	// Use Output (not CombinedOutput) so stderr warnings don't corrupt
	// stdout, which we parse as JSON in many callers.
	out, err := cmd.Output()
	if err != nil {
		// If there's an ExitError, include stderr in the error message.
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
