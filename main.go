package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

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

	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "":
		launchTUI()
	case "list", "l", "ls":
		cmdList()
	case "tui":
		launchTUI()
	case "attach", "a":
		cmdAttach()
	case "detach", "d":
		cmdDetach()
	case "new", "n":
		cmdNew()
	case "rename", "mv":
		cmdRename()
	case "kill", "k":
		cmdKill()
	case "version", "v", "-v", "--version":
		fmt.Printf("tsm %s (%s, %s) backend=%s\n", version, commit, date, session.RestoreBackendName())
	case "help", "h", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "tsm: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Run 'tsm help' for usage.")
		os.Exit(1)
	}
}

func cmdAttach() {
	cfg := session.DefaultConfig()
	if len(os.Args) < 3 {
		sessions, err := session.ListSessions(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
			os.Exit(1)
		}
		switch len(sessions) {
		case 0:
			name, err := suggestSessionName(cfg, sessions)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating session name: %v\n", err)
				os.Exit(1)
			}
			if err := attachSession(cfg, name, true); err != nil {
				fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", name)
			return
		case 1:
			name := sessions[0].Name
			if err := attachSession(cfg, name, false); err != nil {
				fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", name)
			return
		default:
			launchTUI()
			return
		}
	}

	name := os.Args[2]
	if switched := emitLocalSwitchRequest(name); switched {
		return
	}
	if err := attachSession(cfg, name, true); err != nil {
		var switchErr *session.SwitchSessionError
		if errors.As(err, &switchErr) {
			if err := execAttachTarget(switchErr.Target); err != nil {
				fmt.Fprintf(os.Stderr, "Switch error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", name)
}

func emitLocalSwitchRequest(target string) bool {
	current := os.Getenv("TSM_SESSION")
	if current == "" {
		return false
	}
	if current == target {
		return true
	}
	fmt.Fprint(os.Stdout, session.AttachSwitchSequence(target))
	return true
}

func execAttachTarget(name string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, []string{exe, "attach", name}, os.Environ())
}

func attachSession(cfg session.Config, name string, createIfMissing bool) error {
	path := cfg.SocketPath(name)
	if createIfMissing {
		if _, err := os.Lstat(path); err == nil && !session.IsSocket(path) {
			return fmt.Errorf("session path %q exists and is not a socket", path)
		}
		if !session.IsSocket(path) {
			if err := session.SpawnDaemon(name, nil); err != nil {
				return fmt.Errorf("create session %q: %w", name, err)
			}
		}
	}
	return session.Attach(cfg, name)
}

func suggestSessionName(cfg session.Config, sessions []session.Session) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	base := filepath.Base(cwd)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "shell"
	}
	base = sanitizeSessionName(base)
	if base == "" {
		base = "shell"
	}

	existing := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		existing[s.Name] = struct{}{}
	}
	maxLen := cfg.MaxSessionNameLen()
	if maxLen <= 0 {
		maxLen = len(base)
	}

	base = truncateSessionName(base, maxLen)
	if _, ok := existing[base]; !ok && socketPathAvailable(cfg, base) {
		return base, nil
	}

	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		candidate := truncateSessionName(base, maxLen-len(suffix)) + suffix
		if candidate == suffix {
			candidate = fmt.Sprintf("s%d", i)
			candidate = truncateSessionName(candidate, maxLen)
		}
		if _, ok := existing[candidate]; !ok && socketPathAvailable(cfg, candidate) {
			return candidate, nil
		}
	}
}

func socketPathAvailable(cfg session.Config, name string) bool {
	_, err := os.Lstat(cfg.SocketPath(name))
	return os.IsNotExist(err)
}

func sanitizeSessionName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "\t", "-")
	return name
}

func truncateSessionName(name string, maxLen int) string {
	if maxLen <= 0 || len(name) <= maxLen {
		return name
	}
	return name[:maxLen]
}

func cmdNew() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: tsm new <name> [cmd...]")
		os.Exit(1)
	}
	name := os.Args[2]
	shellCmd := os.Args[3:]

	if err := session.SpawnDaemon(name, shellCmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Session %q created\n", name)
}

func cmdDetach() {
	cfg := session.DefaultConfig()
	name := resolveDetachTarget(os.Args)
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: tsm detach [name]")
		fmt.Fprintln(os.Stderr, "when no name is given, TSM_SESSION must be set")
		os.Exit(1)
	}

	if err := session.DetachSession(cfg, name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Session %q detached\n", name)
}

func resolveDetachTarget(args []string) string {
	if len(args) >= 3 {
		return args[2]
	}
	return os.Getenv("TSM_SESSION")
}

func cmdList() {
	cfg := session.DefaultConfig()
	sessions, err := session.ListSessions(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		fmt.Printf("no sessions found in %s\n", cfg.SocketDir)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPID\tATTACHED\tUPTIME\tCMD\tDIR")
	for _, s := range sessions {
		uptime := formatUptime(s.CreatedAt)
		attached := "-"
		if s.Clients > 0 {
			attached = fmt.Sprintf("yes (%d)", s.Clients)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Name, s.PID, attached, uptime, s.Cmd, s.DisplayDir())
	}
	w.Flush()
}

func cmdRename() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm rename <old> <new>")
		os.Exit(1)
	}
	oldName := os.Args[2]
	newName := os.Args[3]
	cfg := session.DefaultConfig()

	if err := session.RenameSession(cfg, oldName, newName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Session %q renamed to %q\n", oldName, newName)
}

func cmdKill() {
	targets := resolveKillTargets(os.Args)
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "usage: tsm kill [name...]")
		fmt.Fprintln(os.Stderr, "when no name is given, TSM_SESSION must be set")
		os.Exit(1)
	}
	cfg := session.DefaultConfig()

	var failed bool
	for _, name := range targets {
		if err := session.KillSession(cfg, name); err != nil {
			fmt.Fprintf(os.Stderr, "Error killing %q: %v\n", name, err)
			failed = true
			continue
		}
		fmt.Printf("Session %q killed\n", name)
	}
	if failed {
		os.Exit(1)
	}
}

func resolveKillTargets(args []string) []string {
	if len(args) >= 3 {
		return args[2:]
	}
	if current := os.Getenv("TSM_SESSION"); current != "" {
		return []string{current}
	}
	return nil
}

func launchTUI() {
	p := tea.NewProgram(tui.NewModel())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If the user pressed Enter to attach, connect via native IPC.
	if m, ok := finalModel.(tui.Model); ok && m.AttachTarget() != "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Resolve executable error: %v\n", err)
			os.Exit(1)
		}
		cmd := exec.Command(exe, "attach", m.AttachTarget())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Print(`tsm — terminal session manager

Usage:
  tsm                      Open interactive TUI (default)
  tsm tui                  Open interactive TUI
  tsm attach [name]        Attach to session (smart attach if omitted)
  tsm detach [name]        Detach current or named session
  tsm new <name> [cmd...]  Create a new session
  tsm list                 List active sessions
  tsm rename <old> <new>   Rename a session
  tsm kill [name...]       Kill current or named sessions
  tsm version              Show version
  tsm help                 Show this help

Aliases:
  attach=a  detach=d  new=n  list=l,ls  rename=mv  kill=k  version=v  help=h

Detach from a session with Ctrl+\
`)
}

func formatUptime(createdAt uint64) string {
	if createdAt == 0 {
		return "-"
	}
	d := time.Since(time.Unix(int64(createdAt), 0))
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%dd%dh", days, hours)
	}
}
