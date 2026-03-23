package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/adibhanna/tsm/internal/appconfig"
	"github.com/adibhanna/tsm/internal/engine"
	"github.com/adibhanna/tsm/internal/mux"
	cmuxbackend "github.com/adibhanna/tsm/internal/mux/backend/cmux"
	ghosttybackend "github.com/adibhanna/tsm/internal/mux/backend/ghostty"
	kittybackend "github.com/adibhanna/tsm/internal/mux/backend/kitty"
	"github.com/adibhanna/tsm/internal/session"
	"github.com/adibhanna/tsm/internal/tui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	dirty   = "false"

	doctorExecutable    = os.Executable
	doctorLookPath      = exec.LookPath
	doctorRunCommand    = runCommand
	doctorReadDir       = os.ReadDir
	doctorProbe         = session.ProbeSession
	doctorIsSocket      = session.IsSocket
	doctorGhosttyStatus = detectGhosttyStatus
	doctorCleanSocket   = session.CleanStaleSocket
	debugFetchPreview   = engine.FetchPreview
)

var errSessionPathNotSocket = errors.New("session path exists and is not a socket")

func main() {
	// Internal daemon mode — not user-facing.
	if len(os.Args) > 2 && os.Args[1] == "--daemon" {
		name := os.Args[2]
		if err := session.ValidateSessionName(name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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
	case "doctor":
		cmdDoctor()
	case "debug":
		cmdDebug()
	case "claude-statusline":
		cmdClaudeStatusline()
	case "mux", "m":
		cmdMux()
	case "help", "h", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "tsm: unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "Run 'tsm help' for usage.")
		os.Exit(1)
	}
}

func cmdClaudeStatusline() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Claude statusline error: read stdin: %v\n", err)
		os.Exit(1)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return
	}
	if !json.Valid(data) {
		fmt.Fprintln(os.Stderr, "Claude statusline error: invalid JSON input")
		os.Exit(1)
	}
	if name := strings.TrimSpace(os.Getenv("TSM_SESSION")); name != "" {
		if err := session.WriteClaudeStatusline(session.DefaultConfig(), name, append(data, '\n')); err != nil {
			fmt.Fprintf(os.Stderr, "Claude statusline error: write session status: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Println(formatClaudeStatusline(data))
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
				fmt.Fprintln(os.Stderr, formatSessionActionError("attach", name, err))
				os.Exit(1)
			}
			fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", name)
			return
		case 1:
			name := sessions[0].Name
			if err := attachSession(cfg, name, false); err != nil {
				fmt.Fprintln(os.Stderr, formatSessionActionError("attach", name, err))
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
	if err := session.ValidateSessionName(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
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
		fmt.Fprintln(os.Stderr, formatSessionActionError("attach", name, err))
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
		fmt.Fprintln(os.Stderr, formatSessionActionError("attach", target, err))
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
	if os.Getenv("TSM_TEST_ATTACH_STDIO") == "1" {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

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
			return fmt.Errorf("%w: %q", errSessionPathNotSocket, path)
		}
		if !session.IsSocket(path) {
			if err := session.SpawnDaemon(name, nil); err != nil {
				return fmt.Errorf("create session %q: %w", name, err)
			}
		}
	} else if !session.IsSocket(path) {
		return fmt.Errorf("%w: %q", session.ErrSessionNotFound, name)
	}
	if warning := daemonBuildWarning(cfg, name); warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}
	if err := markSessionFocused(cfg, name, os.Getenv("TSM_SESSION")); err != nil {
		return fmt.Errorf("record focus: %w", err)
	}
	return session.Attach(cfg, name)
}

func daemonBuildWarning(cfg session.Config, name string) string {
	currentInfo, err := session.CurrentBuildInfo()
	if !daemonBuildMismatch(cfg, name, currentInfo, err) {
		return ""
	}
	return fmt.Sprintf("Warning: session %q is running an older tsm daemon build.\nRecreate the session to pick up the latest session logic if behavior looks stale.", name)
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

	for i := 2; i < 10000; i++ {
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
	return "", fmt.Errorf("could not generate unique session name")
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
	if err := session.ValidateSessionName(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
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
		fmt.Fprintln(os.Stderr, formatSessionActionError("detach", name, err))
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

func cmdDoctor() {
	if len(os.Args) >= 3 {
		switch os.Args[2] {
		case "clean-stale":
			cmdDoctorCleanStale()
			return
		default:
			fmt.Fprintf(os.Stderr, "tsm doctor: unknown subcommand %q\n", os.Args[2])
			printDoctorUsage()
			os.Exit(1)
		}
	}

	report, err := doctorReport(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Doctor error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report)
}

func cmdDoctorCleanStale() {
	cfg := session.DefaultConfig()
	removedSockets, err := cleanStaleSockets(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Doctor clean-stale error: %v\n", err)
		os.Exit(1)
	}
	removedArtifacts, err := cleanStaleArtifacts(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Doctor clean-stale error: %v\n", err)
		os.Exit(1)
	}
	if len(removedSockets) == 0 && len(removedArtifacts) == 0 {
		fmt.Println("No stale sockets or orphaned artifacts found")
		return
	}
	for _, name := range removedSockets {
		fmt.Printf("Removed stale socket %q\n", name)
	}
	for _, status := range removedArtifacts {
		fmt.Printf("Removed orphaned artifacts for %q (%s)\n", status.Name, strings.Join(status.Kinds, ", "))
	}
}

func cmdDebug() {
	if len(os.Args) < 4 || os.Args[2] != "session" {
		printDebugUsage()
		os.Exit(1)
	}

	report, healthy, err := debugSessionReport(session.DefaultConfig(), os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Debug error: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(report)
	if !healthy {
		os.Exit(1)
	}
}

func cmdRename() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm rename <old> <new>")
		os.Exit(1)
	}
	oldName := os.Args[2]
	newName := os.Args[3]
	if err := session.ValidateSessionName(oldName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid old name: %v\n", err)
		os.Exit(1)
	}
	if err := session.ValidateSessionName(newName); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid new name: %v\n", err)
		os.Exit(1)
	}
	cfg := session.DefaultConfig()

	if err := session.RenameSession(cfg, oldName, newName); err != nil {
		fmt.Fprintln(os.Stderr, formatSessionActionError("rename", oldName, err))
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
		if err := session.ValidateSessionName(name); err != nil {
			fmt.Fprintln(os.Stderr, formatSessionActionError("kill", name, err))
			failed = true
			continue
		}
		if err := session.KillSession(cfg, name); err != nil {
			fmt.Fprintln(os.Stderr, formatSessionActionError("kill", name, err))
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

	// If the user selected a workspace to open, run tsm mux open.
	type muxOpener interface {
		MuxOpenTarget() string
	}
	if m, ok := finalModel.(muxOpener); ok && m.MuxOpenTarget() != "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		cmd := exec.Command(exe, "mux", "open", m.MuxOpenTarget())
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error opening workspace: %v\n", err)
			os.Exit(1)
		}
	}
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func doctorReport(getenv func(string) string) (string, error) {
	var b strings.Builder

	cfg := session.DefaultConfig()
	configState := "unavailable"
	configPath := ""
	if loadedCfg, err := appconfig.Load(getenv); err == nil {
		configPath = loadedCfg.Path
		if _, err := os.Stat(configPath); err == nil {
			configState = "present"
		} else if os.IsNotExist(err) {
			configState = "missing"
		} else {
			configState = err.Error()
		}
	} else {
		configState = "error: " + err.Error()
	}

	exePath, exeErr := doctorExecutable()
	pkgConfigPath, pkgConfigErr := doctorLookPath("pkg-config")
	ghosttyStatus := doctorGhosttyStatus(session.RestoreBackendName(), pkgConfigErr)

	fmt.Fprintf(&b, "tsm doctor\n")
	fmt.Fprintf(&b, "version: %s\n", versionString(session.RestoreBackendName()))
	if exeErr != nil {
		fmt.Fprintf(&b, "executable: error: %v\n", exeErr)
	} else {
		fmt.Fprintf(&b, "executable: %s\n", exePath)
	}
	if configPath == "" {
		configPath = "<unknown>"
	}
	fmt.Fprintf(&b, "config: %s (%s)\n", configPath, configState)
	fmt.Fprintf(&b, "socket dir: %s\n", cfg.SocketDir)
	if pkgConfigErr != nil {
		fmt.Fprintf(&b, "pkg-config: missing (%v)\n", pkgConfigErr)
	} else {
		fmt.Fprintf(&b, "pkg-config: %s\n", pkgConfigPath)
	}
	fmt.Fprintf(&b, "libghostty-vt: %s\n", ghosttyStatus)

	socketStatuses, err := doctorSocketStatuses(cfg)
	if err != nil {
		return "", err
	}
	artifactStatuses, err := doctorArtifactStatuses(cfg)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(&b, "sessions:\n")
	if len(socketStatuses) == 0 {
		fmt.Fprintf(&b, "  none\n")
	} else {
		for _, status := range socketStatuses {
			if status.Err != "" {
				fmt.Fprintf(&b, "  - %s: stale (%s)\n", status.Name, status.Err)
				continue
			}
			fmt.Fprintf(
				&b,
				"  - %s: live pid=%d clients=%d cmd=%q dir=%q%s\n",
				status.Name,
				status.Info.PID,
				status.Info.ClientsLen,
				status.Info.CmdString(),
				status.Info.CwdString(),
				formatDoctorBuildSuffix(status.BuildMismatch),
			)
		}
	}

	fmt.Fprintf(&b, "artifacts:\n")
	if len(artifactStatuses) == 0 {
		fmt.Fprintf(&b, "  none\n")
	} else {
		for _, status := range artifactStatuses {
			fmt.Fprintf(&b, "  - %s: orphaned %s\n", status.Name, strings.Join(status.Kinds, ", "))
		}
	}

	return b.String(), nil
}

type doctorSocketStatus struct {
	Name          string
	Info          *session.InfoPayload
	Err           string
	BuildMismatch bool
}

type doctorArtifactStatus struct {
	Name  string
	Kinds []string
}

func doctorSocketStatuses(cfg session.Config) ([]doctorSocketStatus, error) {
	entries, err := doctorReadDir(cfg.SocketDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	statuses := make([]doctorSocketStatus, 0, len(entries))
	currentBuild, currentBuildErr := session.CurrentBuildInfo()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := cfg.SocketPath(name)
		if !doctorIsSocket(path) {
			continue
		}
		info, err := doctorProbe(path)
		if err != nil {
			statuses = append(statuses, doctorSocketStatus{
				Name: name,
				Err:  err.Error(),
			})
			continue
		}
		statuses = append(statuses, doctorSocketStatus{
			Name:          name,
			Info:          info,
			BuildMismatch: daemonBuildMismatch(cfg, name, currentBuild, currentBuildErr),
		})
	}

	return statuses, nil
}

func cleanStaleSockets(cfg session.Config) ([]string, error) {
	statuses, err := doctorSocketStatuses(cfg)
	if err != nil {
		return nil, err
	}

	var removed []string
	for _, status := range statuses {
		if status.Err == "" {
			continue
		}
		path := cfg.SocketPath(status.Name)
		if err := doctorCleanSocket(path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed = append(removed, status.Name)
	}

	return removed, nil
}

func doctorArtifactStatuses(cfg session.Config) ([]doctorArtifactStatus, error) {
	artifacts, err := session.ListSessionArtifacts(cfg)
	if err != nil {
		return nil, err
	}

	bySession := make(map[string][]string)
	for _, artifact := range artifacts {
		path := cfg.SocketPath(artifact.Session)
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		bySession[artifact.Session] = append(bySession[artifact.Session], artifact.Kind)
	}

	statuses := make([]doctorArtifactStatus, 0, len(bySession))
	for name, kinds := range bySession {
		statuses = append(statuses, doctorArtifactStatus{Name: name, Kinds: kinds})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	for i := range statuses {
		sort.Strings(statuses[i].Kinds)
	}
	return statuses, nil
}

func cleanStaleArtifacts(cfg session.Config) ([]doctorArtifactStatus, error) {
	statuses, err := doctorArtifactStatuses(cfg)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if err := session.RemoveSessionArtifacts(cfg, status.Name); err != nil {
			return nil, err
		}
	}
	return statuses, nil
}

func detectGhosttyStatus(backend string, pkgConfigErr error) string {
	if pkgConfigErr != nil {
		if backend == "libghostty-vt" {
			return "loaded (pkg-config not found)"
		}
		return "pkg-config not found"
	}
	if doctorRunCommand("pkg-config", "--exists", "libghostty-vt") == nil {
		return "ok"
	}
	if backend == "libghostty-vt" {
		return "loaded (pkg-config not configured)"
	}
	return "missing"
}

func formatSessionActionError(action, name string, err error) string {
	switch {
	case errors.Is(err, session.ErrSessionNotFound), errors.Is(err, os.ErrNotExist):
		return fmt.Sprintf("Cannot %s session %q: session not found.\nRun 'tsm ls' to list sessions.", action, name)
	case errors.Is(err, errSessionPathNotSocket):
		return fmt.Sprintf("Cannot %s session %q: the session path is not a Unix socket.\nRun 'tsm doctor' to inspect the socket directory.", action, name)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Sprintf("Cannot %s session %q: the session socket exists but the daemon is not responding.\nRun 'tsm doctor clean-stale' to remove stale sockets, then recreate or reattach the session.", action, name)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("Cannot %s session %q: the daemon timed out.\nRun 'tsm debug session %s' for details, or 'tsm doctor' for a broader health check.", action, name, name)
	}

	return fmt.Sprintf("Cannot %s session %q: %v\nRun 'tsm debug session %s' for details, or 'tsm doctor' for a broader health check.", action, name, err, name)
}

func daemonBuildMismatch(cfg session.Config, name string, currentInfo session.DaemonBuildInfo, currentErr error) bool {
	daemonInfo, err := session.ReadDaemonBuildInfo(cfg, name)
	if err != nil || daemonInfo.ModTimeUnix == 0 {
		return false
	}
	if currentErr != nil || currentInfo.ModTimeUnix == 0 {
		return false
	}
	return daemonInfo.Executable != currentInfo.Executable || daemonInfo.ModTimeUnix != currentInfo.ModTimeUnix
}

func formatDoctorBuildSuffix(mismatch bool) string {
	if !mismatch {
		return ""
	}
	return " [older daemon build]"
}

func debugSessionReport(cfg session.Config, name string) (string, bool, error) {
	var b strings.Builder
	path := cfg.SocketPath(name)

	fmt.Fprintf(&b, "tsm debug session %s\n", name)
	fmt.Fprintf(&b, "socket: %s\n", path)

	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(&b, "state: missing\n")
			return b.String(), false, nil
		}
		return "", false, err
	}
	if !doctorIsSocket(path) {
		fmt.Fprintf(&b, "state: invalid (path exists but is not a socket)\n")
		return b.String(), false, nil
	}

	info, err := doctorProbe(path)
	if err != nil {
		fmt.Fprintf(&b, "state: stale\n")
		fmt.Fprintf(&b, "error: %v\n", err)
		return b.String(), false, nil
	}

	fmt.Fprintf(&b, "state: live\n")
	fmt.Fprintf(&b, "pid: %d\n", info.PID)
	fmt.Fprintf(&b, "clients: %d\n", info.ClientsLen)
	fmt.Fprintf(&b, "command: %s\n", info.CmdString())
	fmt.Fprintf(&b, "cwd: %s\n", info.CwdString())
	if info.CreatedAt != 0 {
		fmt.Fprintf(&b, "created: %s\n", time.Unix(int64(info.CreatedAt), 0).Format(time.RFC3339))
	}
	if info.TaskEndedAt != 0 {
		fmt.Fprintf(&b, "task ended: %s\n", time.Unix(int64(info.TaskEndedAt), 0).Format(time.RFC3339))
		fmt.Fprintf(&b, "task exit code: %d\n", info.TaskExitCode)
	}

	preview := strings.TrimSpace(debugFetchPreview(name, 12))
	if preview == "" {
		preview = "(empty)"
	}
	fmt.Fprintf(&b, "preview:\n%s\n", preview)

	return b.String(), true, nil
}

func cmdMux() {
	if len(os.Args) < 3 {
		printMuxUsage()
		os.Exit(1)
	}

	sub := os.Args[2]
	switch sub {
	case "open":
		cmdMuxOpen()
	case "split":
		cmdMuxSplit()
	case "tab":
		cmdMuxTab()
	case "edit":
		cmdMuxEdit()
	case "new":
		cmdMuxNew()
	case "save":
		cmdMuxSave()
	case "restore":
		cmdMuxRestore()
	case "doctor":
		cmdMuxDoctor()
	case "last", "prev":
		cmdMuxLast()
	case "next":
		cmdMuxNext()
	case "workspace", "ws":
		cmdMuxWorkspace()
	case "setup":
		cmdMuxSetup()
	case "sidebar":
		cmdMuxSidebar()
	case "status":
		cmdMuxStatus()
	case "help", "-h", "--help":
		printMuxUsage()
	default:
		fmt.Fprintf(os.Stderr, "tsm mux: unknown subcommand %q\n", sub)
		printMuxUsage()
		os.Exit(1)
	}
}

func cmdMuxOpen() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux open <workspace>")
		os.Exit(1)
	}
	name := os.Args[3]

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := orch.Open(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error opening workspace %q: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("Workspace %q opened\n", name)
}

func cmdMuxSplit() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux split <left|right|up|down> <session> [cmd...]")
		os.Exit(1)
	}
	dirStr := os.Args[3]
	dir, ok := mux.ParseDirection(dirStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: invalid direction %q (use left, right, up, down)\n", dirStr)
		os.Exit(1)
	}
	sessionName := os.Args[4]
	cmd := os.Args[5:]

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := orch.Split(dir, sessionName, cmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Split %s with session %q\n", dirStr, sessionName)
}

func cmdMuxTab() {
	if len(os.Args) < 4 {
		printMuxTabUsage()
		os.Exit(1)
	}
	sub := os.Args[3]
	switch sub {
	case "new":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: tsm mux tab new <session> [cmd...]")
			os.Exit(1)
		}
		sessionName := os.Args[4]
		cmd := os.Args[5:]

		orch, err := newOrchestrator()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if err := orch.TabNew(sessionName, cmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("New tab with session %q\n", sessionName)
	default:
		fmt.Fprintf(os.Stderr, "tsm mux tab: unknown subcommand %q\n", sub)
		printMuxTabUsage()
		os.Exit(1)
	}
}

func cmdMuxSave() {
	name := ""
	if len(os.Args) >= 4 {
		name = os.Args[3]
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: tsm mux save <workspace>")
		os.Exit(1)
	}

	// Save doesn't need cmux access — works from anywhere.
	orch := &mux.Orchestrator{SessCfg: session.DefaultConfig()}
	if err := orch.Save(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving workspace: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Workspace %q saved\n", name)
}

func cmdMuxRestore() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux restore <workspace>")
		os.Exit(1)
	}
	name := os.Args[3]

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := orch.Restore(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error restoring workspace %q: %v\n", name, err)
		os.Exit(1)
	}
	fmt.Printf("Workspace %q restored\n", name)
}

func cmdMuxDoctor() {
	name := ""
	if len(os.Args) >= 4 {
		name = os.Args[3]
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: tsm mux doctor <workspace>")
		os.Exit(1)
	}

	// Doctor doesn't need cmux access — works from anywhere.
	orch := &mux.Orchestrator{SessCfg: session.DefaultConfig()}
	status, err := orch.Doctor(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Workspace: %s\n", status.WorkspaceName)
	if status.HasManifest {
		fmt.Printf("Manifest:  ~/.config/tsm/workspaces/%s.toml\n", status.WorkspaceName)
	}
	fmt.Printf("Sessions:  %d\n", len(status.Sessions))
	for _, s := range status.Sessions {
		state := "dead"
		if s.Live {
			state = fmt.Sprintf("live (clients=%d)", s.Clients)
		}
		fmt.Printf("  %s: %s\n", s.Name, state)
	}
}

func cmdMuxEdit() {
	dir, err := mux.ManifestDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, dir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdMuxNew() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux new <workspace>")
		os.Exit(1)
	}
	name := os.Args[3]

	dir, err := mux.ManifestDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	path := filepath.Join(dir, name+".toml")
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(os.Stderr, "Workspace %q already exists at %s\n", name, path)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	sample := fmt.Sprintf(`name = "%s"
version = 1

[[surface]]
name = "main"
session = "%s-main"
cwd = "%s"
# command = "nvim ."

  [[surface.split]]
  name = "shell"
  session = "%s-shell"
  direction = "right"
  cwd = "%s"
`, name, name, cwd, name, cwd)

	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created %s\n", path)

	// Open in editor.
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func cmdMuxLast() {
	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := orch.Backend.FocusPreviousPane(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdMuxNext() {
	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := orch.Backend.FocusNextPane(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func cmdMuxWorkspace() {
	if len(os.Args) < 4 {
		// List workspaces.
		orch, err := newOrchestrator()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		workspaces, err := orch.Backend.ListWorkspaces()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, w := range workspaces {
			fmt.Printf("%s\t%s\n", w.ID, w.Name)
		}
		return
	}
	// Switch to named workspace.
	name := os.Args[3]
	orch, err := newOrchestrator()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	workspaces, err := orch.Backend.ListWorkspaces()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	for _, w := range workspaces {
		if w.Name == name || w.ID == name {
			if err := orch.Backend.SelectWorkspace(w.ID); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}
	fmt.Fprintf(os.Stderr, "Workspace %q not found\n", name)
	os.Exit(1)
}

func cmdMuxSetup() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux setup <kitty>")
		os.Exit(1)
	}
	target := os.Args[3]
	switch target {
	case "kitty":
		setupKitty()
	default:
		fmt.Fprintf(os.Stderr, "tsm mux setup: unknown target %q (supported: kitty)\n", target)
		os.Exit(1)
	}
}

func setupKitty() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	confDir := filepath.Join(home, ".config", "kitty")
	confPath := filepath.Join(confDir, "kitty.conf")

	// Read existing config.
	existing, _ := os.ReadFile(confPath)
	content := string(existing)

	// Check if already configured.
	if strings.Contains(content, "allow_remote_control") {
		if strings.Contains(content, "allow_remote_control yes") || strings.Contains(content, "allow_remote_control socket-only") {
			fmt.Println("kitty remote control is already enabled")
			return
		}
		fmt.Fprintln(os.Stderr, "kitty.conf has allow_remote_control set to a different value")
		fmt.Fprintln(os.Stderr, "Edit ~/.config/kitty/kitty.conf and set: allow_remote_control yes")
		os.Exit(1)
	}

	// Create config dir if needed.
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config dir: %v\n", err)
		os.Exit(1)
	}

	// Append the settings.
	line := "\n# Added by tsm for mux support\nallow_remote_control yes\nenabled_layouts splits,tall,stack\n"
	f, err := os.OpenFile(confPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening kitty.conf: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing kitty.conf: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Added allow_remote_control to %s\n", confPath)
	fmt.Println("Restart kitty for the change to take effect")
}

func cmdMuxSidebar() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: tsm mux sidebar sync [workspace]")
		os.Exit(1)
	}
	sub := os.Args[3]
	switch sub {
	case "sync":
		name := ""
		if len(os.Args) >= 5 {
			name = os.Args[4]
		}
		if name == "" {
			fmt.Fprintln(os.Stderr, "usage: tsm mux sidebar sync <workspace>")
			os.Exit(1)
		}
		orch, err := newOrchestrator()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := mux.SidebarSync(orch.Backend, session.DefaultConfig(), name); err != nil {
			fmt.Fprintf(os.Stderr, "Error syncing sidebar: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Sidebar synced for workspace %q\n", name)
	default:
		fmt.Fprintf(os.Stderr, "tsm mux sidebar: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "usage: tsm mux sidebar sync <workspace>")
		os.Exit(1)
	}
}

func cmdMuxStatus() {
	term := mux.DetectTerminal()
	fmt.Printf("Terminal: %s\n", term.Name)
	if term.Backend != "" {
		fmt.Printf("Backend:  %s\n", term.Backend)
	} else {
		fmt.Printf("Backend:  none (no split API available for %s)\n", term.Name)
	}

	orch, err := newOrchestrator()
	if err != nil {
		fmt.Printf("Status:   unavailable (%v)\n", err)
	} else {
		fmt.Printf("Status:   connected to %s\n", orch.Backend.Name())
	}

	manifests, err := mux.ListManifests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing workspaces: %v\n", err)
		os.Exit(1)
	}
	if len(manifests) == 0 {
		fmt.Println("Workspaces: none")
	} else {
		fmt.Println("Workspaces:")
		for _, m := range manifests {
			fmt.Printf("  %s\n", m)
		}
	}
}

func newOrchestrator() (*mux.Orchestrator, error) {
	// Check for explicit override via env var.
	preferred := os.Getenv("TSM_MUX_BACKEND")

	// Auto-detect from terminal environment if not set.
	if preferred == "" {
		term := mux.DetectTerminal()
		preferred = term.Backend
	}

	var backend mux.Backend
	switch preferred {
	case "cmux":
		backend = cmuxbackend.New()
	case "kitty":
		backend = kittybackend.New()
	case "ghostty":
		backend = ghosttybackend.New()
	default:
		// Try each backend in priority order.
		if cb := cmuxbackend.New(); cb.Available() {
			backend = cb
		} else if kb := kittybackend.New(); kb.Available() {
			backend = kb
		} else if gb := ghosttybackend.New(); gb.Available() {
			backend = gb
		} else {
			backend = cmuxbackend.New() // fallback for error message
		}
	}

	if !backend.Available() {
		term := mux.DetectTerminal()
		if term.Backend == "" {
			return nil, fmt.Errorf("terminal %q has no split API; install cmux or use a supported terminal", term.Name)
		}
		// Try to get a specific reason from the backend.
		if cb, ok := backend.(*cmuxbackend.Backend); ok {
			if reason := cb.UnavailableReason(); reason != "" {
				return nil, fmt.Errorf("%s", reason)
			}
		}
		return nil, fmt.Errorf("mux backend %q not available (is %s running?)", preferred, preferred)
	}
	return &mux.Orchestrator{
		Backend: backend,
		SessCfg: session.DefaultConfig(),
	}, nil
}

func printMuxUsage() {
	fmt.Print(`tsm mux — native terminal multiplexer

Usage:
  tsm mux open <workspace>                        Open a workspace from manifest
  tsm mux new <workspace>                          Create a new workspace manifest
  tsm mux edit                                     Open workspace manifests in $EDITOR
  tsm mux split <left|right|up|down> <session>     Split focused pane with session
  tsm mux tab new <session> [cmd...]               New tab with session
  tsm mux save <workspace>                          Save current workspace layout
  tsm mux restore <workspace>                      Restore workspace from manifest
  tsm mux last                                     Focus previous pane
  tsm mux next                                     Focus next pane
  tsm mux workspace [name]                         List or switch workspaces
  tsm mux doctor <workspace>                       Diagnose workspace health
  tsm mux sidebar sync <workspace>                 Sync session state to cmux sidebar
  tsm mux setup kitty                              Enable kitty remote control
  tsm mux status                                   Show terminal, backend, and workspace info
  tsm mux help                                     Show this help

Workspaces are defined as TOML manifests in ~/.config/tsm/workspaces/

Env:
  TSM_MUX_BACKEND    Override backend (e.g. "cmux")

Aliases:
  mux=m
`)
}

func printMuxTabUsage() {
	fmt.Print(`tsm mux tab

Usage:
  tsm mux tab new <session> [cmd...]    Create a new tab with a tsm session
`)
}

func printUsage() {
	fmt.Print(`tsm — terminal session manager

Usage:
  tsm                      Open interactive TUI (default)
  tsm tui [--simplified] [--keymap default|palette]
                           Open interactive TUI
  tsm palette              Open simplified session palette
  tsm claude-statusline    Capture Claude Code statusline JSON for TSM previews
  tsm config install       Install default config into ~/.config/tsm/config.toml
  tsm attach [name]        Attach to session (smart attach if omitted)
  tsm toggle               Switch to the previous session
  tsm detach [name]        Detach current or named session
  tsm new <name> [cmd...]  Create a new session
  tsm list                 List active sessions
  tsm mux open <workspace>  Open workspace from manifest
  tsm mux save [workspace]  Save current workspace layout
  tsm mux restore <ws>     Restore workspace from manifest
  tsm mux split <dir> <s>  Split focused pane with session
  tsm mux tab new <s>      New tab with session
  tsm mux doctor <ws>      Diagnose workspace health
  tsm mux status           Show terminal and backend info
  tsm doctor               Show runtime diagnostics
  tsm doctor clean-stale   Remove stale session sockets
  tsm debug session <name> Show detailed diagnostics for one session
  tsm rename <old> <new>   Rename a session
  tsm kill [name...]       Kill current or named sessions
  tsm version              Show version
  tsm help                 Show this help

Aliases:
  palette=p  attach=a  detach=d  new=n  list=l,ls  rename=mv  kill=k  mux=m  version=v  help=h

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

func printDoctorUsage() {
	fmt.Print(`tsm doctor

Usage:
  tsm doctor
  tsm doctor clean-stale
`)
}

func printDebugUsage() {
	fmt.Print(`tsm debug

Usage:
  tsm debug session <name>
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

func formatClaudeStatusline(data []byte) string {
	var payload struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Workspace struct {
			CurrentDir string `json:"current_dir"`
		} `json:"workspace"`
		ContextWindow struct {
			UsedPercentage any `json:"used_percentage"`
		} `json:"context_window"`
		Cost struct {
			TotalCostUSD float64 `json:"total_cost_usd"`
		} `json:"cost"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return ""
	}
	parts := []string{}
	if model := strings.TrimSpace(payload.Model.DisplayName); model != "" {
		parts = append(parts, "["+model+"]")
	}
	if dir := strings.TrimSpace(payload.Workspace.CurrentDir); dir != "" {
		parts = append(parts, filepath.Base(dir))
	}
	if pct := formatClaudeStatuslinePercent(payload.ContextWindow.UsedPercentage); pct != "" {
		parts = append(parts, pct+" context")
	}
	if payload.Cost.TotalCostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.2f", payload.Cost.TotalCostUSD))
	}
	return strings.Join(parts, "  ")
}

func formatClaudeStatuslinePercent(v any) string {
	switch n := v.(type) {
	case float64:
		return fmt.Sprintf("%.0f%%", n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%.0f%%", f)
	default:
		return ""
	}
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
