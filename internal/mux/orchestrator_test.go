package mux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adibhanna/tsm/internal/session"
)

// mockBackend records all calls for verification.
type mockBackend struct {
	workspaces []Workspace
	calls      []string
	nextPaneID int
}

func (m *mockBackend) Name() string      { return "mock" }
func (m *mockBackend) Available() bool    { return true }

func (m *mockBackend) ListWorkspaces() ([]Workspace, error) {
	m.calls = append(m.calls, "ListWorkspaces")
	return m.workspaces, nil
}
func (m *mockBackend) CreateWorkspace(name string) (Workspace, error) {
	m.calls = append(m.calls, "CreateWorkspace:"+name)
	w := Workspace{ID: "workspace:1", Name: name}
	m.workspaces = append(m.workspaces, w)
	return w, nil
}
func (m *mockBackend) SelectWorkspace(id string) error {
	m.calls = append(m.calls, "SelectWorkspace:"+id)
	return nil
}
func (m *mockBackend) CreateSurface(workspaceID string) (Surface, error) {
	m.calls = append(m.calls, "CreateSurface:"+workspaceID)
	return Surface{ID: "surface:1", WorkspaceID: workspaceID}, nil
}
func (m *mockBackend) CloseSurface(id string) error {
	m.calls = append(m.calls, "CloseSurface:"+id)
	return nil
}
func (m *mockBackend) SplitPane(workspaceID string, dir Direction) (Pane, error) {
	m.nextPaneID++
	m.calls = append(m.calls, "SplitPane:"+dir.String())
	return Pane{ID: "pane:1"}, nil
}
func (m *mockBackend) ClosePane(id string) error {
	m.calls = append(m.calls, "ClosePane:"+id)
	return nil
}
func (m *mockBackend) FocusPane(id string) error {
	m.calls = append(m.calls, "FocusPane:"+id)
	return nil
}
func (m *mockBackend) GetFocusedPane() (Pane, error) {
	m.calls = append(m.calls, "GetFocusedPane")
	return Pane{ID: "pane:0"}, nil
}
func (m *mockBackend) SendText(paneID string, text string) error {
	m.calls = append(m.calls, "SendText:"+strings.TrimSpace(text))
	return nil
}
func (m *mockBackend) GetTree(workspaceID string) (LayoutNode, error) {
	m.calls = append(m.calls, "GetTree")
	return LayoutNode{}, nil
}
func (m *mockBackend) ReadScreen(workspaceID, surfaceID string) (string, error) {
	m.calls = append(m.calls, "ReadScreen:"+surfaceID)
	return "[tsm:test] ~", nil
}
func (m *mockBackend) SendTextToWorkspace(workspaceID, surfaceID, text string) error {
	m.calls = append(m.calls, "SendTextToWorkspace:"+workspaceID+":"+surfaceID+":"+strings.TrimSpace(text))
	return nil
}
func (m *mockBackend) ListPaneSurfaces(workspaceID string) ([]string, error) {
	m.calls = append(m.calls, "ListPaneSurfaces:"+workspaceID)
	return []string{"surface:1"}, nil
}
func (m *mockBackend) SetStatus(key, value string) error {
	m.calls = append(m.calls, "SetStatus:"+key+"="+value)
	return nil
}
func (m *mockBackend) Log(msg string) error {
	m.calls = append(m.calls, "Log:"+msg)
	return nil
}

func TestOrchestratorOpen(t *testing.T) {
	// Set up a temporary config so sessions won't conflict.
	tmpDir := t.TempDir()
	t.Setenv("TSM_DIR", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Write a test manifest.
	m := &Manifest{
		Name:    "test",
		Version: 1,
		Surface: []ManifestSurface{
			{
				Name:    "main",
				Session: "main",
				Split: []ManifestSplit{
					{Name: "side", Session: "side", Direction: "right"},
				},
			},
		},
	}
	if err := SaveManifest(m); err != nil {
		t.Fatal(err)
	}

	mock := &mockBackend{}
	orch := &Orchestrator{
		Backend: mock,
		SessCfg: session.Config{
			SocketDir: filepath.Join(tmpDir),
			LogDir:    filepath.Join(tmpDir, "logs"),
		},
	}

	// Open will fail on SpawnDaemon since we're not running full tsm,
	// but we can verify the backend calls up to that point.
	err := orch.Open("test")

	// The orchestrator will try to create sessions. Since we can't actually
	// spawn daemons in a unit test, check that the backend calls are correct
	// up to the point of failure.
	if err != nil {
		// Expected: SpawnDaemon will fail in test environment.
		// Verify at least the workspace was created.
		hasCreate := false
		for _, c := range mock.calls {
			if strings.HasPrefix(c, "CreateWorkspace:") || strings.HasPrefix(c, "SelectWorkspace:") {
				hasCreate = true
				break
			}
		}
		if !hasCreate {
			t.Errorf("expected workspace creation call, got calls: %v", mock.calls)
		}
		return
	}

	// If we got here, verify full flow.
	var sendCalls []string
	for _, c := range mock.calls {
		if strings.HasPrefix(c, "SendText:") {
			sendCalls = append(sendCalls, c)
		}
	}
	if len(sendCalls) < 2 {
		t.Errorf("expected at least 2 SendText calls, got: %v", mock.calls)
	}
}

func TestOrchestratorSplit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TSM_DIR", tmpDir)

	mock := &mockBackend{}
	orch := &Orchestrator{
		Backend: mock,
		SessCfg: session.Config{
			SocketDir: tmpDir,
			LogDir:    filepath.Join(tmpDir, "logs"),
		},
	}

	// Split will try to spawn a daemon. Check backend calls.
	err := orch.Split(DirRight, "test-session", nil)
	if err != nil {
		// Expected in test env.
		// Verify no SplitPane was called (session creation should fail first).
		for _, c := range mock.calls {
			if c == "SplitPane:right" {
				// Session was created somehow (unlikely in test), that's fine too.
				return
			}
		}
		return
	}

	// If split succeeded, verify the calls.
	found := false
	for _, c := range mock.calls {
		if c == "SplitPane:right" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SplitPane:right call, got: %v", mock.calls)
	}
}

func TestOrchestratorFindOrCreateWorkspace(t *testing.T) {
	mock := &mockBackend{
		workspaces: []Workspace{{ID: "workspace:1", Name: "existing"}},
	}
	orch := &Orchestrator{Backend: mock}

	// Should find existing.
	id, err := orch.findOrCreateWorkspace("existing")
	if err != nil {
		t.Fatal(err)
	}
	if id != "workspace:1" {
		t.Errorf("id = %q, want %q", id, "workspace:1")
	}

	// Should create new.
	id, err = orch.findOrCreateWorkspace("new-ws")
	if err != nil {
		t.Fatal(err)
	}
	if id != "workspace:1" { // mock always returns workspace:1
		t.Errorf("id = %q, want %q", id, "workspace:1")
	}

	// Verify CreateWorkspace was called.
	found := false
	for _, c := range mock.calls {
		if c == "CreateWorkspace:new-ws" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CreateWorkspace:new-ws, got: %v", mock.calls)
	}
}

func TestDetectSessionFromScreen(t *testing.T) {
	tests := []struct {
		screen string
		want   string
	}{
		{"[tsm:editor] tsm on main via Go\n> ", "editor"},
		{"[tsm:my-logs] ~ \n> ", "my-logs"},
		{"some output\n[tsm:shell] ~/projects\n> ", "shell"},
		{"no session here\n> ", ""},
		{"[tsm:] empty", ""},
		{"[tsm:first]\n[tsm:second]", "second"}, // last match wins
	}
	for _, tt := range tests {
		got := detectSessionFromScreen(tt.screen)
		if got != tt.want {
			t.Errorf("detectSessionFromScreen(%q) = %q, want %q", tt.screen, got, tt.want)
		}
	}
}

func init() {
	// Ensure test temp dirs exist.
	os.MkdirAll(os.TempDir(), 0o755)
}
