package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/adibhanna/tsm/internal/session"
	"github.com/adibhanna/tsm/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Printf("tsm %s (%s, %s)\n", version, commit, date)
		return
	}

	// Internal daemon mode — not user-facing.
	if len(os.Args) > 2 && os.Args[1] == "--daemon" {
		name := os.Args[2]
		shellCmd := os.Args[3:]
		if err := session.StartDaemon(name, shellCmd); err != nil {
			fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	p := tea.NewProgram(tui.NewModel())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If the user pressed Enter to attach, connect via native IPC.
	if m, ok := finalModel.(tui.Model); ok && m.AttachTarget() != "" {
		cfg := session.DefaultConfig()
		if err := session.Attach(cfg, m.AttachTarget()); err != nil {
			fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
			os.Exit(1)
		}
	}
}
