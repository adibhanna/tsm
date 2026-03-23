// Package ghostty wraps Ghostty's AppleScript API to implement mux.Backend.
//
// Requires Ghostty 1.3.0+ on macOS. Uses osascript to call AppleScript commands.
//
// Ghostty concepts mapping:
//   - Ghostty window  → mux.Workspace
//   - Ghostty tab     → mux.Surface
//   - Ghostty terminal (split) → mux.Pane
//
// Ghostty does not have a sidebar, so SetStatus and Log are no-ops.
// ReadScreen is not available via AppleScript.
package ghostty

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/adibhanna/tsm/internal/mux"
)

// Backend implements mux.Backend using Ghostty's AppleScript API.
type Backend struct{}

// New returns a Ghostty backend.
func New() *Backend {
	return &Backend{}
}

func (b *Backend) Name() string { return "ghostty" }

func (b *Backend) Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	out, err := osascript(`tell application "System Events" to get (name of processes) contains "Ghostty"`)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "true"
}

// --- Workspaces (Ghostty windows) ---

func (b *Backend) ListWorkspaces() ([]mux.Workspace, error) {
	out, err := osascript(`tell application "Ghostty" to get id of every window`)
	if err != nil {
		return nil, fmt.Errorf("list windows: %w", err)
	}
	ids := parseStringList(out)
	var ws []mux.Workspace
	for _, id := range ids {
		name, _ := osascript(fmt.Sprintf(`tell application "Ghostty" to get name of window id "%s"`, id))
		name = strings.TrimSpace(name)
		if name == "" {
			name = "window-" + id
		}
		ws = append(ws, mux.Workspace{ID: id, Name: name})
	}
	return ws, nil
}

func (b *Backend) CreateWorkspace(name string) (mux.Workspace, error) {
	out, err := osascript(`tell application "Ghostty"
		set newWin to new window
		return id of newWin
	end tell`)
	if err != nil {
		return mux.Workspace{}, fmt.Errorf("new window: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Workspace{ID: id, Name: name}, nil
}

func (b *Backend) SelectWorkspace(id string) error {
	_, err := osascript(fmt.Sprintf(`tell application "Ghostty" to activate window id "%s"`, id))
	if err != nil {
		// Fallback: just bring Ghostty to front.
		_, _ = osascript(`tell application "Ghostty" to activate`)
	}
	return nil
}

// --- Surfaces (Ghostty tabs) ---

func (b *Backend) CreateSurface(workspaceID string) (mux.Surface, error) {
	var script string
	if workspaceID != "" {
		script = fmt.Sprintf(`tell application "Ghostty"
			set newTab to new tab in window id "%s"
			return id of focused terminal of newTab
		end tell`, workspaceID)
	} else {
		script = `tell application "Ghostty"
			set newTab to new tab in front window
			return id of focused terminal of newTab
		end tell`
	}
	out, err := osascript(script)
	if err != nil {
		return mux.Surface{}, fmt.Errorf("new tab: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Surface{ID: id, WorkspaceID: workspaceID}, nil
}

func (b *Backend) CloseSurface(id string) error {
	// Close the tab containing this terminal.
	_, err := osascript(fmt.Sprintf(`tell application "Ghostty" to close terminal id "%s"`, id))
	if err != nil {
		return fmt.Errorf("close surface: %w", err)
	}
	return nil
}

// --- Panes (Ghostty terminals within splits) ---

func (b *Backend) SplitPane(workspaceID string, dir mux.Direction) (mux.Pane, error) {
	ghosttyDir := ghosttyDirection(dir)
	script := fmt.Sprintf(`tell application "Ghostty"
		set focusedTerm to focused terminal of selected tab of front window
		set newTerm to split focusedTerm direction %s
		return id of newTerm
	end tell`, ghosttyDir)
	out, err := osascript(script)
	if err != nil {
		return mux.Pane{}, fmt.Errorf("split: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Pane{ID: id, SurfaceID: id}, nil
}

func (b *Backend) ClosePane(id string) error {
	_, err := osascript(fmt.Sprintf(`tell application "Ghostty" to close terminal id "%s"`, id))
	if err != nil {
		return fmt.Errorf("close terminal: %w", err)
	}
	return nil
}

func (b *Backend) FocusPane(id string) error {
	_, err := osascript(fmt.Sprintf(`tell application "Ghostty" to focus terminal id "%s"`, id))
	if err != nil {
		return fmt.Errorf("focus terminal: %w", err)
	}
	return nil
}

func (b *Backend) GetFocusedPane() (mux.Pane, error) {
	out, err := osascript(`tell application "Ghostty" to get id of focused terminal of selected tab of front window`)
	if err != nil {
		return mux.Pane{}, fmt.Errorf("get focused: %w", err)
	}
	id := strings.TrimSpace(out)
	return mux.Pane{ID: id, SurfaceID: id}, nil
}

// --- I/O ---

func (b *Backend) SendText(paneID string, text string) error {
	// Strip trailing \n — we'll send Enter as a separate key event.
	sendEnter := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")

	escaped := escapeAppleScript(text)
	var target string
	if paneID != "" {
		target = fmt.Sprintf(`terminal id "%s"`, paneID)
	} else {
		target = `focused terminal of selected tab of front window`
	}

	script := fmt.Sprintf(`tell application "Ghostty" to input text %s to %s`, escaped, target)
	if _, err := osascript(script); err != nil {
		return fmt.Errorf("input text: %w", err)
	}

	if sendEnter {
		script = fmt.Sprintf(`tell application "Ghostty" to send key "enter" to %s`, target)
		if _, err := osascript(script); err != nil {
			return fmt.Errorf("send enter: %w", err)
		}
	}
	return nil
}

func (b *Backend) SendTextToWorkspace(workspaceID, surfaceID, text string) error {
	sendEnter := strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")

	escaped := escapeAppleScript(text)
	var target string
	if surfaceID != "" {
		target = fmt.Sprintf(`terminal id "%s"`, surfaceID)
	} else if workspaceID != "" {
		target = fmt.Sprintf(`focused terminal of selected tab of window id "%s"`, workspaceID)
	} else {
		target = `focused terminal of selected tab of front window`
	}

	script := fmt.Sprintf(`tell application "Ghostty" to input text %s to %s`, escaped, target)
	if _, err := osascript(script); err != nil {
		return fmt.Errorf("input text: %w", err)
	}

	if sendEnter {
		script = fmt.Sprintf(`tell application "Ghostty" to send key "enter" to %s`, target)
		if _, err := osascript(script); err != nil {
			return fmt.Errorf("send enter: %w", err)
		}
	}
	return nil
}

func (b *Backend) ListPaneSurfaces(workspaceID string) ([]string, error) {
	var script string
	if workspaceID != "" {
		script = fmt.Sprintf(`tell application "Ghostty" to get id of every terminal of selected tab of window id "%s"`, workspaceID)
	} else {
		script = `tell application "Ghostty" to get id of every terminal of selected tab of front window`
	}
	out, err := osascript(script)
	if err != nil {
		return nil, fmt.Errorf("list terminals: %w", err)
	}
	return parseStringList(out), nil
}

// --- Screen ---

func (b *Backend) ReadScreen(workspaceID, surfaceID string) (string, error) {
	return "", fmt.Errorf("read-screen not supported in Ghostty")
}

// --- Layout ---

func (b *Backend) GetTree(workspaceID string) (mux.LayoutNode, error) {
	return mux.LayoutNode{Type: "workspace"}, nil
}

// --- Sidebar (no-op for Ghostty) ---

func (b *Backend) SetStatus(key, value string) error { return nil }
func (b *Backend) Log(msg string) error              { return nil }

// --- Helpers ---

func ghosttyDirection(dir mux.Direction) string {
	switch dir {
	case mux.DirLeft:
		return "left"
	case mux.DirRight:
		return "right"
	case mux.DirUp:
		return "up"
	case mux.DirDown:
		return "down"
	default:
		return "right"
	}
}

func osascript(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// escapeAppleScript wraps a string in AppleScript quotes, escaping backslashes and quotes.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

var idRe = regexp.MustCompile(`[A-Za-z0-9_-]+`)

// parseStringList parses AppleScript list output like "abc, def, ghi" into string IDs.
// Ghostty IDs are UUIDs or opaque strings, not just numbers.
func parseStringList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ", ")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
