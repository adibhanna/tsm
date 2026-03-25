package mux

import (
	"testing"
)

// mockActiveBackend extends mockBackend with GetActiveWorkspace support.
type mockActiveBackend struct {
	mockBackend
	activeWS Workspace
}

func (m *mockActiveBackend) GetActiveWorkspace() (Workspace, error) {
	return m.activeWS, nil
}

func TestProjectSwitchNext(t *testing.T) {
	mock := &mockActiveBackend{
		mockBackend: mockBackend{
			workspaces: []Workspace{
				{ID: "ws:1", Name: "app:feat-auth"},
				{ID: "ws:2", Name: "app:main"},
				{ID: "ws:3", Name: "app:fix-perf"},
				{ID: "ws:4", Name: "other:main"}, // different project
			},
		},
		activeWS: Workspace{ID: "ws:1", Name: "app:feat-auth"},
	}

	orch := &Orchestrator{Backend: mock}
	if err := orch.ProjectSwitch(+1); err != nil {
		t.Fatalf("ProjectSwitch(+1): %v", err)
	}

	// Should have selected the next app: workspace (app:main).
	var selected string
	for _, c := range mock.calls {
		if len(c) > len("SelectWorkspace:") && c[:len("SelectWorkspace:")] == "SelectWorkspace:" {
			selected = c[len("SelectWorkspace:"):]
		}
	}
	if selected != "ws:2" {
		t.Errorf("selected = %q, want ws:2 (app:main)", selected)
	}
}

func TestProjectSwitchPrev(t *testing.T) {
	mock := &mockActiveBackend{
		mockBackend: mockBackend{
			workspaces: []Workspace{
				{ID: "ws:1", Name: "app:feat-auth"},
				{ID: "ws:2", Name: "app:main"},
				{ID: "ws:3", Name: "app:fix-perf"},
			},
		},
		activeWS: Workspace{ID: "ws:1", Name: "app:feat-auth"},
	}

	orch := &Orchestrator{Backend: mock}
	if err := orch.ProjectSwitch(-1); err != nil {
		t.Fatalf("ProjectSwitch(-1): %v", err)
	}

	// Should wrap around to last: app:fix-perf.
	var selected string
	for _, c := range mock.calls {
		if len(c) > len("SelectWorkspace:") && c[:len("SelectWorkspace:")] == "SelectWorkspace:" {
			selected = c[len("SelectWorkspace:"):]
		}
	}
	if selected != "ws:3" {
		t.Errorf("selected = %q, want ws:3 (app:fix-perf)", selected)
	}
}

func TestProjectSwitchSingleWorkspace(t *testing.T) {
	mock := &mockActiveBackend{
		mockBackend: mockBackend{
			workspaces: []Workspace{
				{ID: "ws:1", Name: "app:main"},
			},
		},
		activeWS: Workspace{ID: "ws:1", Name: "app:main"},
	}

	orch := &Orchestrator{Backend: mock}
	if err := orch.ProjectSwitch(+1); err != nil {
		t.Fatalf("ProjectSwitch(+1): %v", err)
	}

	// Should be a no-op — no SelectWorkspace call.
	for _, c := range mock.calls {
		if len(c) > len("SelectWorkspace:") && c[:len("SelectWorkspace:")] == "SelectWorkspace:" {
			t.Error("should not switch when only one workspace")
		}
	}
}

func TestProjectSwitchNonProjectWorkspace(t *testing.T) {
	mock := &mockActiveBackend{
		mockBackend: mockBackend{
			workspaces: []Workspace{
				{ID: "ws:1", Name: "Terminal 1"},
			},
		},
		activeWS: Workspace{ID: "ws:1", Name: "Terminal 1"},
	}

	orch := &Orchestrator{Backend: mock}
	err := orch.ProjectSwitch(+1)
	if err == nil {
		t.Fatal("expected error for non-project workspace")
	}
}

func TestProjectSwitchFiltersToProject(t *testing.T) {
	mock := &mockActiveBackend{
		mockBackend: mockBackend{
			workspaces: []Workspace{
				{ID: "ws:1", Name: "app:main"},
				{ID: "ws:2", Name: "mono:main"},
				{ID: "ws:3", Name: "app:feat"},
				{ID: "ws:4", Name: "mono:feat"},
			},
		},
		activeWS: Workspace{ID: "ws:1", Name: "app:main"},
	}

	orch := &Orchestrator{Backend: mock}
	if err := orch.ProjectSwitch(+1); err != nil {
		t.Fatalf("ProjectSwitch(+1): %v", err)
	}

	// Should switch to app:feat (ws:3), not mono:main (ws:2).
	var selected string
	for _, c := range mock.calls {
		if len(c) > len("SelectWorkspace:") && c[:len("SelectWorkspace:")] == "SelectWorkspace:" {
			selected = c[len("SelectWorkspace:"):]
		}
	}
	if selected != "ws:3" {
		t.Errorf("selected = %q, want ws:3 (app:feat)", selected)
	}
}
