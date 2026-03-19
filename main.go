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
	"github.com/adibhanna/tsm/internal/appconfig"
	"github.com/adibhanna/tsm/internal/session"
	"github.com/adibhanna/tsm/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	dirty   = "false"
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
		launchTUI(mustResolveTUIOptions(nil))
	case "list", "l", "ls":
		cmdList()
	case "tui":
		launchTUI(mustResolveTUIOptions(os.Args[2:]))
	case "palette", "p":
		launchTUI(mustResolveTUIOptions([]string{"--simplified"}))
	case "config":
		cmdConfig()
	case "attach", "a":
		cmdAttach()
	case "toggle", "last", "prev":
		cmdToggle()
	case "detach", "d":
		cmdDetach()
	case "new", "n":
		cmdNew()
	case "rename", "mv":
		cmdRename()
	case "kill", "k":
		cmdKill()
	case "version", "v", "-v", "--version":
		fmt.Println(versionString(session.RestoreBackendName()))
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
			launchTUI(mustResolveTUIOptions(nil))
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

func cmdToggle() {
	cfg := session.DefaultConfig()
	current := os.Getenv("TSM_SESSION")
	target, err := resolveToggleTarget(cfg, current)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Toggle error: %v\n", err)
		os.Exit(1)
	}
	if switched := emitLocalSwitchRequest(target); switched {
		return
	}
	if err := attachSession(cfg, target, false); err != nil {
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
	fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", target)
}

func cmdConfig() {
	if len(os.Args) < 3 {
		printConfigUsage()
		os.Exit(1)
	}

	switch os.Args[2] {
	case "install":
		force := false
		for _, arg := range os.Args[3:] {
			switch arg {
			case "--force", "-f":
				force = true
			default:
				fmt.Fprintf(os.Stderr, "tsm config install: unknown option %q\n", arg)
				os.Exit(1)
			}
		}
		path, err := appconfig.InstallDefault(os.Getenv, force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Config install error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Installed config at %s\n", path)
	default:
		printConfigUsage()
		os.Exit(1)
	}
}

func emitLocalSwitchRequest(target string) bool {
	current := os.Getenv("TSM_SESSION")
	if current == "" {
		return false
	}
	if current == target {
		return true
	}
	_ = markSessionFocused(session.DefaultConfig(), target, current)
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

func runAttachTarget(name string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "attach", name)
	cmd.Env = os.Environ()

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		defer tty.Close()
		cmd.Stdin = tty
		cmd.Stdout = tty
		cmd.Stderr = tty
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
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
	if err := markSessionFocused(cfg, name, os.Getenv("TSM_SESSION")); err != nil {
		return fmt.Errorf("record focus: %w", err)
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
		_ = removeFocusSession(cfg, name)
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

func mustResolveTUIOptions(args []string) tui.Options {
	opts, err := resolveTUIOptions(args, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUI option error: %v\n", err)
		os.Exit(1)
	}
	return opts
}

func resolveTUIOptions(args []string, getenv func(string) string) (tui.Options, error) {
	cfg, err := appconfig.Load(getenv)
	if err != nil {
		return tui.Options{}, err
	}
	return resolveTUIOptionsWithConfig(args, getenv, cfg)
}

func resolveTUIOptionsWithConfig(args []string, getenv func(string) string, cfg appconfig.Config) (tui.Options, error) {
	opts := tui.Options{}

	if raw := cfg.TUI.Mode; raw != "" {
		mode, err := tui.ParseMode(raw)
		if err != nil {
			return tui.Options{}, err
		}
		opts.Mode = mode
	}
	if raw := cfg.TUI.Keymap; raw != "" {
		keymap, err := tui.ParseKeymap(raw)
		if err != nil {
			return tui.Options{}, err
		}
		opts.Keymap = keymap
	}
	if cfg.TUI.ShowHelp != nil {
		opts.ShowHelp = *cfg.TUI.ShowHelp
		opts.ShowHelpSet = true
	}

	if raw := getenv("TSM_TUI_MODE"); raw != "" {
		mode, err := tui.ParseMode(raw)
		if err != nil {
			return tui.Options{}, err
		}
		opts.Mode = mode
	}
	if raw := getenv("TSM_TUI_KEYMAP"); raw != "" {
		keymap, err := tui.ParseKeymap(raw)
		if err != nil {
			return tui.Options{}, err
		}
		opts.Keymap = keymap
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--simplified":
			opts.Mode = tui.ModeSimplified
		case arg == "--full":
			opts.Mode = tui.ModeFull
		case arg == "--keymap":
			if i+1 >= len(args) {
				return tui.Options{}, fmt.Errorf("--keymap requires a value")
			}
			i++
			keymap, err := tui.ParseKeymap(args[i])
			if err != nil {
				return tui.Options{}, err
			}
			opts.Keymap = keymap
		case strings.HasPrefix(arg, "--keymap="):
			keymap, err := tui.ParseKeymap(strings.TrimPrefix(arg, "--keymap="))
			if err != nil {
				return tui.Options{}, err
			}
			opts.Keymap = keymap
		default:
			return tui.Options{}, fmt.Errorf("unknown tui option %q", arg)
		}
	}

	opts = tui.NormalizeOptions(opts)
	bindings, err := tui.BuildBindings(opts.Keymap, cfg.TUI.Keymaps[opts.Keymap.String()])
	if err != nil {
		return tui.Options{}, err
	}
	opts.Bindings = bindings

	return tui.NormalizeOptions(opts), nil
}

func launchTUI(opts tui.Options) {
	p := tea.NewProgram(tui.NewModel(opts))
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If the user pressed Enter to attach, connect via native IPC.
	type attachTargeter interface {
		AttachTarget() string
	}
	if m, ok := finalModel.(attachTargeter); ok && m.AttachTarget() != "" {
		_ = markSessionFocused(session.DefaultConfig(), m.AttachTarget(), os.Getenv("TSM_SESSION"))
		if err := runAttachTarget(m.AttachTarget()); err != nil {
			fmt.Fprintf(os.Stderr, "Attach error: %v\n", err)
			os.Exit(1)
		}
	}
}

func printUsage() {
	fmt.Print(`tsm — terminal session manager

Usage:
  tsm                      Open interactive TUI (default)
  tsm tui [--simplified] [--keymap default|palette]
                           Open interactive TUI
  tsm palette              Open simplified session palette
  tsm config install       Install default config into ~/.config/tsm/config.toml
  tsm attach [name]        Attach to session (smart attach if omitted)
  tsm toggle               Switch to the previous session
  tsm detach [name]        Detach current or named session
  tsm new <name> [cmd...]  Create a new session
  tsm list                 List active sessions
  tsm rename <old> <new>   Rename a session
  tsm kill [name...]       Kill current or named sessions
  tsm version              Show version
  tsm help                 Show this help

Aliases:
  palette=p  attach=a  detach=d  new=n  list=l,ls  rename=mv  kill=k  version=v  help=h

Detach from a session with Ctrl+\

TUI env:
  TSM_TUI_MODE=full|simplified
  TSM_TUI_KEYMAP=default|palette
  TSM_CONFIG_FILE=~/.config/tsm/config.toml
`)
}

func printConfigUsage() {
	fmt.Print(`tsm config

Usage:
  tsm config install [--force]
`)
}

func versionString(backend string) string {
	parts := []string{"tsm", normalizedVersion()}
	if meta := versionMetadata(); meta != "" {
		parts = append(parts, meta)
	}
	parts = append(parts, "backend="+backend)
	return strings.Join(parts, " ")
}

func normalizedVersion() string {
	v := strings.TrimSpace(version)
	if v == "" || v == "unknown" || v == "none" {
		return "dev"
	}
	return v
}

func versionMetadata() string {
	var items []string
	if c := shortCommit(commit); c != "" {
		items = append(items, "commit "+c)
	}
	if isDirtyBuild() {
		items = append(items, "dirty")
	}
	if builtAt := strings.TrimSpace(date); builtAt != "" && builtAt != "unknown" {
		items = append(items, "built "+builtAt)
	}
	if len(items) == 0 {
		return ""
	}
	return "(" + strings.Join(items, ", ") + ")"
}

func shortCommit(c string) string {
	c = strings.TrimSpace(c)
	switch c {
	case "", "none", "unknown":
		return ""
	}
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func isDirtyBuild() bool {
	switch strings.ToLower(strings.TrimSpace(dirty)) {
	case "1", "true", "yes", "dirty":
		return true
	default:
		return false
	}
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
