package mux

// Direction specifies which side of a pane to split.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

func (d Direction) String() string {
	switch d {
	case DirLeft:
		return "left"
	case DirRight:
		return "right"
	case DirUp:
		return "up"
	case DirDown:
		return "down"
	default:
		return "unknown"
	}
}

// ParseDirection converts a string to a Direction.
func ParseDirection(s string) (Direction, bool) {
	switch s {
	case "left", "l":
		return DirLeft, true
	case "right", "r":
		return DirRight, true
	case "up", "u":
		return DirUp, true
	case "down", "d":
		return DirDown, true
	default:
		return 0, false
	}
}

// Workspace represents a named workspace (analogous to a tmux session).
type Workspace struct {
	ID   string
	Name string
}

// Surface represents a tab within a workspace.
type Surface struct {
	ID          string
	WorkspaceID string
}

// Pane represents a single pane within a surface.
type Pane struct {
	ID        string
	SurfaceID string
}

// LayoutNode represents a node in the workspace layout tree.
type LayoutNode struct {
	Type     string       // "pane", "hsplit", "vsplit"
	PaneID   string       // set when Type == "pane"
	Children []LayoutNode // set when Type is a split
}

// Backend is the interface that terminal emulator mux backends must implement.
// Each backend wraps a specific terminal emulator's split/tab/workspace API.
type Backend interface {
	// Name returns the backend identifier (e.g. "cmux", "kitty").
	Name() string

	// Available reports whether this backend's terminal emulator and API are reachable.
	Available() bool

	// ListWorkspaces returns all workspaces.
	ListWorkspaces() ([]Workspace, error)

	// CreateWorkspace creates a new named workspace and returns it.
	CreateWorkspace(name string) (Workspace, error)

	// SelectWorkspace switches to the given workspace.
	SelectWorkspace(id string) error

	// CreateSurface creates a new tab/surface in the given workspace.
	CreateSurface(workspaceID string) (Surface, error)

	// CloseSurface closes a surface.
	CloseSurface(id string) error

	// SplitPane creates a new pane by splitting within a workspace.
	// If workspaceID is empty, splits in the caller's workspace.
	SplitPane(workspaceID string, dir Direction) (Pane, error)

	// ClosePane closes a pane.
	ClosePane(id string) error

	// FocusPane focuses the given pane.
	FocusPane(id string) error

	// GetFocusedPane returns the currently focused pane.
	GetFocusedPane() (Pane, error)

	// SendText sends text (keystrokes) into a pane.
	SendText(paneID string, text string) error

	// SendTextToWorkspace sends text to a surface within a specific workspace.
	// This is needed when the calling process is in a different workspace.
	SendTextToWorkspace(workspaceID, surfaceID, text string) error

	// ListPaneSurfaces returns terminal surface refs in a workspace.
	ListPaneSurfaces(workspaceID string) ([]string, error)

	// ReadScreen reads the terminal text content from a surface.
	ReadScreen(workspaceID, surfaceID string) (string, error)

	// GetTree returns the layout tree for a workspace.
	GetTree(workspaceID string) (LayoutNode, error)

	// SetStatus sets a key-value status entry (best-effort, no-op if unsupported).
	SetStatus(key, value string) error

	// Log writes a log message to the sidebar/status area (best-effort).
	Log(msg string) error
}
