// Package kitty wraps kitty's remote control protocol to implement mux.Backend.
//
// Requires `allow_remote_control yes` (or `allow_remote_control socket-only`)
// in kitty.conf. Uses `kitten @` CLI for all operations.
//
// Kitty concepts mapping:
//   - kitty OS window  → mux.Workspace
//   - kitty tab        → mux.Surface (tab within a workspace)
//   - kitty window     → mux.Pane (split within a tab)
//
// Kitty does not have a sidebar, so SetStatus and Log are no-ops.
package kitty

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Backend implements mux.Backend using kitty's remote control protocol.
type Backend struct {
	bin string // path to kitten binary
}

// New returns a kitty backend.
func New() *Backend {
	bin, _ := exec.LookPath("kitten")
	return &Backend{bin: bin}
}

func (b *Backend) Name() string { return "kitty" }

func (b *Backend) Available() bool {
	if b.bin == "" {
		return false
	}
	// kitten @ ls succeeds if remote control is enabled.
	_, err := b.run("@", "ls")
	return err == nil
}

// --- Workspaces (kitty OS windows) ---

type kittyOSWindow struct {
	ID         int        `json:"id"`
	IsActive   bool       `json:"is_active"`
	IsFocused  bool       `json:"is_focused"`
	Tabs       []kittyTab `json:"tabs"`
	WMClass    string     `json:"wm_class"`
	WMName     string     `json:"wm_name"`
	Platform   string     `json:"platform_window_id"`
}

type kittyTab struct {
	ID       int           `json:"id"`
	IsActive bool          `json:"is_active"`
	Title    string        `json:"title"`
	Layout   string        `json:"layout_name"`
	Windows  []kittyWindow `json:"windows"`
}

type kittyWindow struct {
	ID        int    `json:"id"`
	IsActive  bool   `json:"is_active"`
	IsFocused bool   `json:"is_focused"`
	Title     string `json:"title"`
	PID       int    `json:"pid"`
	CWD       string `json:"cwd"`
}

func (b *Backend) ListWorkspaces() ([]mux.Workspace, error) {
	out, err := b.run("@", "ls")
	if err != nil {
		return nil, fmt.Errorf("ls: %w", err)
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return nil, fmt.Errorf("parse ls: %w", err)
	}
	var ws []mux.Workspace
	for _, w := range osWindows {
		name := w.WMName
		if name == "" {
			name = fmt.Sprintf("window-%d", w.ID)
		}
		ws = append(ws, mux.Workspace{
			ID:   strconv.Itoa(w.ID),
			Name: name,
		})
	}
	return ws, nil
}

func (b *Backend) CreateWorkspace(name string) (mux.Workspace, error) {
	// kitty: launch a new OS window.
	out, err := b.run("@", "launch", "--type=os-window", "--title="+name)
	if err != nil {
		return mux.Workspace{}, fmt.Errorf("launch os-window: %w", err)
	}
	// Output is the new window ID.
	id := strings.TrimSpace(out)
	return mux.Workspace{ID: id, Name: name}, nil
}

func (b *Backend) SelectWorkspace(id string) error {
	_, err := b.run("@", "focus-window", "--match=id:"+id)
	if err != nil {
		return fmt.Errorf("focus-window: %w", err)
	}
	return nil
}

// --- Surfaces (kitty tabs) ---

func (b *Backend) CreateSurface(workspaceID string) (mux.Surface, error) {
	args := []string{"@", "launch", "--type=tab"}
	if workspaceID != "" {
		args = append(args, "--match=id:"+workspaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return mux.Surface{}, fmt.Errorf("launch tab: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Surface{ID: id, WorkspaceID: workspaceID}, nil
}

func (b *Backend) CloseSurface(id string) error {
	_, err := b.run("@", "close-tab", "--match=id:"+id)
	if err != nil {
		return fmt.Errorf("close-tab: %w", err)
	}
	return nil
}

// --- Panes (kitty windows within a tab) ---

func (b *Backend) SplitPane(workspaceID string, dir mux.Direction) (mux.Pane, error) {
	location := kittyLocation(dir)
	args := []string{"@", "launch", "--location=" + location, "--cwd=current"}
	out, err := b.run(args...)
	if err != nil {
		return mux.Pane{}, fmt.Errorf("launch split: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Pane{ID: id}, nil
}

func (b *Backend) ClosePane(id string) error {
	_, err := b.run("@", "close-window", "--match=id:"+id)
	if err != nil {
		return fmt.Errorf("close-window: %w", err)
	}
	return nil
}

func (b *Backend) FocusPane(id string) error {
	_, err := b.run("@", "focus-window", "--match=id:"+id)
	if err != nil {
		return fmt.Errorf("focus-window: %w", err)
	}
	return nil
}

func (b *Backend) GetFocusedPane() (mux.Pane, error) {
	out, err := b.run("@", "ls")
	if err != nil {
		return mux.Pane{}, fmt.Errorf("ls: %w", err)
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return mux.Pane{}, fmt.Errorf("parse ls: %w", err)
	}
	for _, osw := range osWindows {
		if !osw.IsFocused {
			continue
		}
		for _, tab := range osw.Tabs {
			if !tab.IsActive {
				continue
			}
			for _, win := range tab.Windows {
				if win.IsFocused || win.IsActive {
					return mux.Pane{
						ID:        strconv.Itoa(win.ID),
						SurfaceID: strconv.Itoa(tab.ID),
					}, nil
				}
			}
		}
	}
	return mux.Pane{}, fmt.Errorf("no focused window found")
}

// --- I/O ---

func (b *Backend) SendText(paneID string, text string) error {
	args := []string{"@", "send-text"}
	if paneID != "" {
		args = append(args, "--match=id:"+paneID)
	}
	args = append(args, text)
	_, err := b.run(args...)
	if err != nil {
		return fmt.Errorf("send-text: %w", err)
	}
	return nil
}

func (b *Backend) SendTextToWorkspace(workspaceID, surfaceID, text string) error {
	// kitty: send-text with --match targeting the specific window.
	args := []string{"@", "send-text"}
	if surfaceID != "" {
		args = append(args, "--match=id:"+surfaceID)
	}
	args = append(args, text)
	_, err := b.run(args...)
	if err != nil {
		return fmt.Errorf("send-text: %w", err)
	}
	return nil
}

func (b *Backend) ListPaneSurfaces(workspaceID string) ([]string, error) {
	out, err := b.run("@", "ls")
	if err != nil {
		return nil, fmt.Errorf("ls: %w", err)
	}
	var osWindows []kittyOSWindow
	if err := json.Unmarshal([]byte(out), &osWindows); err != nil {
		return nil, fmt.Errorf("parse ls: %w", err)
	}
	var refs []string
	for _, osw := range osWindows {
		if workspaceID != "" && strconv.Itoa(osw.ID) != workspaceID {
			continue
		}
		for _, tab := range osw.Tabs {
			for _, win := range tab.Windows {
				refs = append(refs, strconv.Itoa(win.ID))
			}
		}
	}
	return refs, nil
}

// --- Screen ---

func (b *Backend) ReadScreen(workspaceID, surfaceID string) (string, error) {
	args := []string{"@", "get-text"}
	if surfaceID != "" {
		args = append(args, "--match=id:"+surfaceID)
	}
	out, err := b.run(args...)
	if err != nil {
		return "", fmt.Errorf("get-text: %w", err)
	}
	return out, nil
}

// --- Layout ---

func (b *Backend) GetTree(workspaceID string) (mux.LayoutNode, error) {
	// kitty's ls already returns the full tree; minimal parse for now.
	return mux.LayoutNode{Type: "workspace"}, nil
}

// --- Sidebar (no-op for kitty) ---

func (b *Backend) SetStatus(key, value string) error { return nil }
func (b *Backend) Log(msg string) error              { return nil }

// --- Helpers ---

func kittyLocation(dir mux.Direction) string {
	switch dir {
	case mux.DirLeft:
		return "vsplit" // kitty vsplit creates left/right; new pane goes right
	case mux.DirRight:
		return "vsplit"
	case mux.DirUp:
		return "hsplit"
	case mux.DirDown:
		return "hsplit"
	default:
		return "vsplit"
	}
}

func (b *Backend) run(args ...string) (string, error) {
	if b.bin == "" {
		return "", fmt.Errorf("kitten binary not found")
	}
	cmd := exec.Command(b.bin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", b.bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
