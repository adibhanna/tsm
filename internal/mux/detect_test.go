package mux

import "testing"

func TestDetectTerminal(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantName string
		wantBack string
	}{
		{
			name:     "cmux via socket path",
			env:      map[string]string{"CMUX_SOCKET_PATH": "/tmp/cmux.sock"},
			wantName: "cmux",
			wantBack: "cmux",
		},
		{
			name:     "cmux via surface id",
			env:      map[string]string{"CMUX_SURFACE_ID": "surface:1"},
			wantName: "cmux",
			wantBack: "cmux",
		},
		{
			name:     "kitty",
			env:      map[string]string{"KITTY_PID": "12345"},
			wantName: "kitty",
			wantBack: "kitty",
		},
		{
			name:     "wezterm",
			env:      map[string]string{"WEZTERM_EXECUTABLE": "/usr/bin/wezterm"},
			wantName: "wezterm",
			wantBack: "wezterm",
		},
		{
			name:     "ghostty",
			env:      map[string]string{"GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty"},
			wantName: "ghostty",
			wantBack: "ghostty",
		},
		{
			name:     "iterm2 via session id",
			env:      map[string]string{"ITERM_SESSION_ID": "w0t0p0"},
			wantName: "iterm2",
			wantBack: "",
		},
		{
			name:     "unknown",
			env:      map[string]string{},
			wantName: "unknown",
			wantBack: "",
		},
	}

	// Clear all terminal env vars before each subtest.
	termVars := []string{
		"CMUX_SOCKET_PATH", "CMUX_SURFACE_ID", "CMUX_WORKSPACE_ID",
		"KITTY_PID", "WEZTERM_EXECUTABLE", "WEZTERM_UNIX_SOCKET",
		"GHOSTTY_RESOURCES_DIR", "ITERM_SESSION_ID", "TERM_PROGRAM",
		"ALACRITTY_WINDOW_ID",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, v := range termVars {
				t.Setenv(v, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got := DetectTerminal()
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Backend != tt.wantBack {
				t.Errorf("Backend = %q, want %q", got.Backend, tt.wantBack)
			}
		})
	}
}
