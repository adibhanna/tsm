package mux

import "os"

// Terminal represents a detected terminal emulator.
type Terminal struct {
	Name    string // e.g. "cmux", "kitty", "wezterm", "ghostty", "iterm2"
	Backend string // which mux backend to use (empty if no split API)
}

// DetectTerminal identifies the current terminal emulator from environment
// variables. Returns the best match, or an unknown terminal if none detected.
func DetectTerminal() Terminal {
	// Check in priority order — more specific signals first.

	// cmux sets these in every shell it spawns.
	if os.Getenv("CMUX_SOCKET_PATH") != "" || os.Getenv("CMUX_SURFACE_ID") != "" {
		return Terminal{Name: "cmux", Backend: "cmux"}
	}

	// kitty sets KITTY_PID in every child process.
	if os.Getenv("KITTY_PID") != "" {
		return Terminal{Name: "kitty", Backend: "kitty"}
	}

	// WezTerm sets WEZTERM_EXECUTABLE and WEZTERM_UNIX_SOCKET.
	if os.Getenv("WEZTERM_EXECUTABLE") != "" || os.Getenv("WEZTERM_UNIX_SOCKET") != "" {
		return Terminal{Name: "wezterm", Backend: "wezterm"}
	}

	// Ghostty sets GHOSTTY_RESOURCES_DIR.
	if os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return Terminal{Name: "ghostty", Backend: "ghostty"}
	}

	// iTerm2 sets ITERM_SESSION_ID.
	if os.Getenv("ITERM_SESSION_ID") != "" {
		return Terminal{Name: "iterm2", Backend: ""}
	}

	// TERM_PROGRAM is set by many terminals.
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return Terminal{Name: "iterm2", Backend: ""}
	case "Apple_Terminal":
		return Terminal{Name: "apple-terminal", Backend: ""}
	case "Hyper":
		return Terminal{Name: "hyper", Backend: ""}
	case "vscode":
		return Terminal{Name: "vscode", Backend: ""}
	}

	// Alacritty sets ALACRITTY_WINDOW_ID (no split API).
	if os.Getenv("ALACRITTY_WINDOW_ID") != "" {
		return Terminal{Name: "alacritty", Backend: ""}
	}

	return Terminal{Name: "unknown", Backend: ""}
}

// SupportedBackends returns the list of backend names that have implementations.
func SupportedBackends() []string {
	return []string{"cmux"}
}
