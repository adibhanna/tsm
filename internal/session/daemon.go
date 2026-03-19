package session

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"github.com/adibhanna/tsm/internal/appconfig"
	"github.com/creack/pty/v2"
)

// getForegroundPgrp returns the foreground process group of the PTY.
func getForegroundPgrp(ptmx *os.File) (int, error) {
	var pgrp int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(),
		syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgrp)))
	if errno != 0 {
		return 0, errno
	}
	return int(pgrp), nil
}

const scrollbackSize = 10 * 1024 * 1024 // 10 MB, default

// Daemon manages a single session: a PTY, a Unix socket, and connected clients.
type Daemon struct {
	name       string
	cfg        Config
	cmd        *exec.Cmd
	ptmx       *os.File
	listener   net.Listener
	scrollback *Scrollback
	terminal   TerminalBackend
	createdAt  time.Time

	mu      sync.RWMutex
	clients map[net.Conn]*clientState

	done     chan struct{}
	doneOnce sync.Once

	sessionNameFile string
}

type clientState struct {
	attached bool
	writeMu  sync.Mutex
}

// StartDaemon creates a new session with a PTY and listens on a Unix socket.
// This is called in the re-exec'd daemon subprocess.
func StartDaemon(name string, shellCmd []string) error {
	cfg := DefaultConfig()

	// Ensure socket directory exists.
	if err := os.MkdirAll(cfg.SocketDir, 0750); err != nil {
		return fmt.Errorf("mkdir %s: %w", cfg.SocketDir, err)
	}

	sockPath := cfg.SocketPath(name)

	// Clean up stale socket if it exists.
	if IsSocket(sockPath) {
		os.Remove(sockPath)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", sockPath, err)
	}
	defer ln.Close()
	defer os.Remove(sockPath)

	// Determine shell command.
	shell := resolveShellPath(shellCmd)
	argv, err := resolveShellArgs(cfg, name, shell, shellCmd)
	if err != nil {
		return fmt.Errorf("resolve shell args: %w", err)
	}

	cmd := exec.Command(shell)
	cmd.Args = argv // Override so argv[0] can be "-zsh" for login shells.
	env, err := buildDaemonEnv(cfg, name, shell, shellCmd)
	if err != nil {
		return fmt.Errorf("build daemon env: %w", err)
	}
	cmd.Env = env
	cmd.Dir, _ = os.Getwd()

	// Start the command with a PTY.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	rows, cols, err := pty.Getsize(ptmx)
	if err != nil || rows <= 0 || cols <= 0 {
		rows, cols = 24, 80
	}

	d := &Daemon{
		name:            name,
		cfg:             cfg,
		cmd:             cmd,
		ptmx:            ptmx,
		listener:        ln,
		scrollback:      NewScrollback(scrollbackSize),
		terminal:        NewTerminalBackend(uint16(rows), uint16(cols)),
		createdAt:       time.Now(),
		clients:         make(map[net.Conn]*clientState),
		done:            make(chan struct{}),
		sessionNameFile: sessionNameFilePath(cfg, shellIntegrationMode(shell, shellCmd), name),
	}
	defer d.terminal.Close()

	// Handle signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		select {
		case <-sigCh:
			d.doneOnce.Do(func() { close(d.done) })
		case <-d.done:
		}
	}()

	// Read PTY output → scrollback + broadcast to clients.
	go d.readPTY()

	// Accept client connections.
	go d.acceptLoop()

	// Wait for the shell to exit.
	cmd.Wait()
	d.doneOnce.Do(func() { close(d.done) })

	// Give clients a moment to read remaining output.
	time.Sleep(100 * time.Millisecond)
	d.closeAllClients()
	return nil
}

// SpawnDaemon starts a daemon in a new process (re-exec with --daemon flag).
func SpawnDaemon(name string, shellCmd []string) error {
	cfg := DefaultConfig()

	// Check if session already exists.
	sockPath := cfg.SocketPath(name)
	if IsSocket(sockPath) {
		if _, err := ProbeSession(sockPath); err == nil {
			return fmt.Errorf("session %q already exists", name)
		}
		// Stale socket — clean up.
		os.Remove(sockPath)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	args := []string{exe, "--daemon", name}
	args = append(args, shellCmd...)

	cmd := exec.Command(exe, args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}

	// Detach — don't wait for the daemon process.
	cmd.Process.Release()

	// Wait briefly for the socket to appear.
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		if IsSocket(sockPath) {
			return nil
		}
	}
	return nil
}

func (d *Daemon) readPTY() {
	buf := make([]byte, 4096)
	for {
		n, err := d.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]
			d.terminal.Consume(data)
			d.scrollback.Write(data)
			d.broadcast(TagOutput, data)
		}
		if err != nil {
			return
		}
	}
}

func (d *Daemon) acceptLoop() {
	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.done:
				return
			default:
				continue
			}
		}
		d.mu.Lock()
		d.clients[conn] = &clientState{}
		d.mu.Unlock()
		go d.handleClient(conn)
	}
}

func (d *Daemon) handleClient(conn net.Conn) {
	defer func() {
		d.mu.Lock()
		delete(d.clients, conn)
		d.mu.Unlock()
		conn.Close()
	}()

	for {
		select {
		case <-d.done:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		tag, payload, err := ReadMessage(conn, 1*time.Second)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			return
		}

		switch tag {
		case TagInput:
			d.ptmx.Write(payload)

		case TagResize:
			if len(payload) >= 4 {
				rows := uint16(payload[0]) | uint16(payload[1])<<8
				cols := uint16(payload[2]) | uint16(payload[3])<<8
				pty.Setsize(d.ptmx, &pty.Winsize{
					Rows: rows,
					Cols: cols,
				})
				d.terminal.Resize(rows, cols)
			}

		case TagInit:
			// Client just connected — resize PTY, signal the foreground
			// process group, and send Ctrl+L to force a redraw.
			d.markClientAttached(conn)
			if seq := d.terminal.Snapshot(); len(seq) > 0 {
				d.sendMessage(conn, TagOutput, seq, ioTimeout)
			}
			if len(payload) >= 4 {
				rows := uint16(payload[0]) | uint16(payload[1])<<8
				cols := uint16(payload[2]) | uint16(payload[3])<<8
				pty.Setsize(d.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
				d.terminal.Resize(rows, cols)
			}
			if pgrp, err := getForegroundPgrp(d.ptmx); err == nil && pgrp > 0 {
				syscall.Kill(-pgrp, syscall.SIGWINCH)
			}
			// Poke the PTY with Ctrl+L to wake up the app's event loop
			// and force an immediate screen redraw (works in vim + shells).
			time.Sleep(10 * time.Millisecond)
			d.ptmx.Write([]byte{0x0c})

		case TagInfo:
			d.sendInfo(conn)

		case TagHistory:
			data := d.terminal.Preview()
			if len(data) == 0 {
				data = []byte(d.scrollback.TailLines(256))
			}
			d.sendMessage(conn, TagHistory, data, ioTimeout)

		case TagKill:
			d.doneOnce.Do(func() { close(d.done) })
			d.cmd.Process.Signal(syscall.SIGHUP)
			time.Sleep(500 * time.Millisecond)
			d.cmd.Process.Signal(syscall.SIGKILL)
			return

		case TagDetach:
			return

		case TagDetachAll:
			d.closeAllClients()
			return

		case TagRename:
			newName := strings.TrimSpace(string(payload))
			var renameErr string
			if newName == "" {
				renameErr = "empty session name"
			} else if err := d.renameSession(newName); err != nil {
				renameErr = err.Error()
			}
			d.sendMessage(conn, TagAck, []byte(renameErr), ioTimeout)
			if renameErr == "" {
				if pgrp, err := getForegroundPgrp(d.ptmx); err == nil && pgrp > 0 {
					syscall.Kill(-pgrp, syscall.SIGWINCH)
				}
				time.Sleep(10 * time.Millisecond)
				d.ptmx.Write([]byte{0x0c})
			}

		case TagRun:
			// For now, just acknowledge.
			d.sendMessage(conn, TagAck, nil, ioTimeout)
		}
	}
}

func (d *Daemon) renameSession(newName string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if newName == d.name {
		return nil
	}

	oldPath := d.cfg.SocketPath(d.name)
	newPath := d.cfg.SocketPath(newName)
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("session %q already exists", newName)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check target session path: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename socket: %w", err)
	}
	if err := writeSessionNameFile(d.sessionNameFile, newName); err != nil {
		_ = os.Rename(newPath, oldPath)
		return fmt.Errorf("update session name file: %w", err)
	}
	d.name = newName
	return nil
}

func (d *Daemon) broadcast(tag Tag, data []byte) {
	msg := MarshalMessage(tag, data)
	for _, client := range d.snapshotClients() {
		client.state.writeMessage(client.conn, msg, 100*time.Millisecond)
	}
}

func (d *Daemon) sendInfo(conn net.Conn) {
	info := d.buildInfo()
	data := make([]byte, InfoSize)

	putUint64LE(data[0:8], uint64(info.ClientsLen))
	putUint32LE(data[8:12], uint32(info.PID))
	putUint16LE(data[12:14], info.CmdLen)
	putUint16LE(data[14:16], info.CwdLen)
	copy(data[16:16+MaxCmdLen], info.Cmd[:])
	copy(data[272:272+MaxCwdLen], info.Cwd[:])
	putUint64LE(data[528:536], info.CreatedAt)
	putUint64LE(data[536:544], info.TaskEndedAt)
	data[544] = info.TaskExitCode

	d.sendMessage(conn, TagInfo, data, ioTimeout)
}

func (d *Daemon) buildInfo() InfoPayload {
	d.mu.RLock()
	clientCount := 0
	for _, state := range d.clients {
		if state.attached {
			clientCount++
		}
	}
	d.mu.RUnlock()

	var info InfoPayload
	info.ClientsLen = uint64(clientCount)
	info.PID = int32(d.cmd.Process.Pid)
	info.CreatedAt = uint64(d.createdAt.Unix())

	// Command
	cmdStr := strings.Join(d.cmd.Args, " ")
	info.CmdLen = uint16(min(len(cmdStr), MaxCmdLen))
	copy(info.Cmd[:], cmdStr)

	// Working directory
	cwd, _ := os.Getwd()
	info.CwdLen = uint16(min(len(cwd), MaxCwdLen))
	copy(info.Cwd[:], cwd)

	return info
}

func (d *Daemon) closeAllClients() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for conn := range d.clients {
		conn.Close()
	}
	d.clients = make(map[net.Conn]*clientState)
}

type clientSnapshot struct {
	conn  net.Conn
	state *clientState
}

func (d *Daemon) snapshotClients() []clientSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()

	clients := make([]clientSnapshot, 0, len(d.clients))
	for conn, state := range d.clients {
		clients = append(clients, clientSnapshot{conn: conn, state: state})
	}
	return clients
}

func (d *Daemon) getClientState(conn net.Conn) *clientState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.clients[conn]
}

func (d *Daemon) markClientAttached(conn net.Conn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state := d.clients[conn]; state != nil {
		state.attached = true
	}
}

func (d *Daemon) sendMessage(conn net.Conn, tag Tag, payload []byte, timeout time.Duration) error {
	msg := MarshalMessage(tag, payload)
	if state := d.getClientState(conn); state != nil {
		return state.writeMessage(conn, msg, timeout)
	}
	conn.SetWriteDeadline(time.Now().Add(timeout))
	_, err := conn.Write(msg)
	return err
}

func (c *clientState) writeMessage(conn net.Conn, msg []byte, timeout time.Duration) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	conn.SetWriteDeadline(time.Now().Add(timeout))
	_, err := conn.Write(msg)
	return err
}

// resolveShellPath determines the executable to run.
func resolveShellPath(shellCmd []string) string {
	if len(shellCmd) > 0 {
		return shellCmd[0]
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell
}

// resolveShellArgs determines the argv used to start the shell.
func resolveShellArgs(cfg Config, name, shell string, shellCmd []string) ([]string, error) {
	if len(shellCmd) > 0 {
		return shellCmd, nil
	}

	base := filepath.Base(shell)
	switch shellIntegrationMode(shell, shellCmd) {
	case "bash":
		return []string{base, "--rcfile", bashRcFilePath(cfg, name), "-i"}, nil
	case "fish":
		return []string{base, "-i"}, nil
	default:
		return []string{"-" + base}, nil
	}
}

func buildDaemonEnv(cfg Config, name, shell string, shellCmd []string) ([]string, error) {
	shortcuts, err := loadShellShortcuts(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("load shell shortcuts: %w", err)
	}
	var env []string
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "TSM_SESSION="):
		case strings.HasPrefix(e, "TSM_SESSION_FILE="):
		case strings.HasPrefix(e, "ZDOTDIR="):
		case strings.HasPrefix(e, "TSM_ORIG_ZDOTDIR="):
		case strings.HasPrefix(e, "XDG_CONFIG_HOME="):
		case strings.HasPrefix(e, "TSM_ORIG_XDG_CONFIG_HOME="):
		case strings.HasPrefix(e, "TSM_SHELL_INTEGRATION="):
		default:
			env = append(env, e)
		}
	}
	env = append(env, "TSM_SESSION="+name)

	switch mode := shellIntegrationMode(shell, shellCmd); mode {
	case "zsh":
		dir, err := ensureZshIntegration(cfg, name, shortcuts)
		if err != nil {
			return nil, err
		}
		origZdotdir := os.Getenv("ZDOTDIR")
		if origZdotdir == "" {
			origZdotdir, _ = os.UserHomeDir()
		}
		if origZdotdir != "" {
			env = append(env, "TSM_ORIG_ZDOTDIR="+origZdotdir)
		}
		env = append(env, "ZDOTDIR="+dir)
		env = append(env, "TSM_SESSION_FILE="+sessionNameFilePath(cfg, mode, name))
		env = append(env, "TSM_SHELL_INTEGRATION=zsh")
	case "bash":
		if err := ensureBashIntegration(cfg, name, shortcuts); err != nil {
			return nil, err
		}
		env = append(env, "TSM_SESSION_FILE="+sessionNameFilePath(cfg, mode, name))
		env = append(env, "TSM_SHELL_INTEGRATION=bash")
	case "fish":
		dir, err := ensureFishIntegration(cfg, name, shortcuts)
		if err != nil {
			return nil, err
		}
		origXDG := os.Getenv("XDG_CONFIG_HOME")
		if origXDG == "" {
			home, _ := os.UserHomeDir()
			if home != "" {
				origXDG = filepath.Join(home, ".config")
			}
		}
		if origXDG != "" {
			env = append(env, "TSM_ORIG_XDG_CONFIG_HOME="+origXDG)
		}
		env = append(env, "XDG_CONFIG_HOME="+dir)
		env = append(env, "TSM_SESSION_FILE="+sessionNameFilePath(cfg, mode, name))
		env = append(env, "TSM_SHELL_INTEGRATION=fish")
	}

	return env, nil
}

func shellIntegrationMode(shell string, shellCmd []string) string {
	if len(shellCmd) > 0 {
		return ""
	}
	switch filepath.Base(shell) {
	case "zsh", "bash", "fish":
		return filepath.Base(shell)
	default:
		return ""
	}
}

func ensureZshIntegration(cfg Config, name string, shortcuts shellShortcuts) (string, error) {
	dir := shellIntegrationDir(cfg, "zsh", name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("mkdir zsh integration dir: %w", err)
	}
	if err := writeSessionNameFile(sessionNameFilePath(cfg, "zsh", name), name); err != nil {
		return "", err
	}

	files := map[string]string{
		".zshenv":   zshEnvShim,
		".zprofile": zshProfileShim,
		".zshrc":    renderZshRcShim(shortcuts),
		".zlogin":   zshLoginShim,
		".zlogout":  zshLogoutShim,
	}
	for name, contents := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0640); err != nil {
			return "", fmt.Errorf("write %s: %w", path, err)
		}
	}

	return dir, nil
}

func ensureBashIntegration(cfg Config, name string, shortcuts shellShortcuts) error {
	dir := shellIntegrationDir(cfg, "bash", name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("mkdir bash integration dir: %w", err)
	}
	if err := writeSessionNameFile(sessionNameFilePath(cfg, "bash", name), name); err != nil {
		return err
	}
	if err := os.WriteFile(bashRcFilePath(cfg, name), []byte(renderBashRcShim(shortcuts)), 0640); err != nil {
		return fmt.Errorf("write %s: %w", bashRcFilePath(cfg, name), err)
	}
	return nil
}

func ensureFishIntegration(cfg Config, name string, shortcuts shellShortcuts) (string, error) {
	dir := shellIntegrationDir(cfg, "fish", name)
	configDir := filepath.Join(dir, "fish")
	if err := os.MkdirAll(configDir, 0750); err != nil {
		return "", fmt.Errorf("mkdir fish integration dir: %w", err)
	}
	if err := writeSessionNameFile(sessionNameFilePath(cfg, "fish", name), name); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.fish"), []byte(renderFishConfigShim(shortcuts)), 0640); err != nil {
		return "", fmt.Errorf("write fish config: %w", err)
	}
	return dir, nil
}

func shellIntegrationDir(cfg Config, shell, name string) string {
	return filepath.Join(cfg.SocketDir, "."+shell, name)
}

func bashRcFilePath(cfg Config, name string) string {
	return filepath.Join(shellIntegrationDir(cfg, "bash", name), ".bashrc")
}

func sessionNameFilePath(cfg Config, shell, name string) string {
	return filepath.Join(shellIntegrationDir(cfg, shell, name), ".tsm-session-name")
}

func writeSessionNameFile(path, name string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("mkdir session name dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(name+"\n"), 0640); err != nil {
		return fmt.Errorf("write session name file %s: %w", path, err)
	}
	return nil
}

const zshEnvShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zshenv" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zshenv"
fi
`

const zshProfileShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zprofile" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zprofile"
fi
`

const zshRcShimTemplate = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zshrc" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zshrc"
fi

if [[ -n "${TSM_SESSION:-}" ]]; then
  autoload -Uz add-zsh-hook
  typeset -g _tsm_prompt_marker=""

  _tsm_refresh_session_name() {
    if [[ -n "${TSM_SESSION_FILE:-}" && -f "${TSM_SESSION_FILE}" ]]; then
      local _tsm_name
      _tsm_name=$(<"${TSM_SESSION_FILE}")
      _tsm_name=${_tsm_name//$'\n'/}
      if [[ -n "${_tsm_name}" ]]; then
        export TSM_SESSION="${_tsm_name}"
      fi
    fi
  }

  _tsm_apply_prompt_marker() {
    local _tsm_new_marker="[tsm:${TSM_SESSION}]"
    if [[ -n "${_tsm_prompt_marker}" && "${PROMPT:-}" == *"${_tsm_prompt_marker}"* ]]; then
      PROMPT="${PROMPT/${_tsm_prompt_marker}/${_tsm_new_marker}}"
    elif [[ "${PROMPT:-}" != *"${_tsm_new_marker}"* ]]; then
      PROMPT="%F{6}${_tsm_new_marker}%f ${PROMPT:-%# }"
    fi
    _tsm_prompt_marker="${_tsm_new_marker}"
  }

  _tsm_precmd_title() {
    _tsm_refresh_session_name
    _tsm_apply_prompt_marker
    print -Pn -- "\e]2;tsm:${TSM_SESSION} %~\a"
  }
  if (( ${precmd_functions[(Ie)_tsm_precmd_title]} == 0 )); then
    add-zsh-hook precmd _tsm_precmd_title
  fi
  _tsm_precmd_title

  if [[ -o interactive ]]; then
    _tsm_session_full() {
      zle -I
      tsm tui
      zle reset-prompt
    }
    zle -N _tsm_session_full
__TSM_ZSH_FULL_BIND__

    _tsm_session_palette() {
      zle -I
      tsm tui --simplified
      zle reset-prompt
    }
    zle -N _tsm_session_palette
__TSM_ZSH_PALETTE_BIND__

    _tsm_session_toggle() {
      zle -I
      tsm toggle
      zle reset-prompt
    }
    zle -N _tsm_session_toggle
__TSM_ZSH_TOGGLE_BIND__
  fi
fi
`

const zshLoginShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zlogin" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zlogin"
fi
`

const zshLogoutShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zlogout" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zlogout"
fi
`

const bashRcShimTemplate = `if [[ -f /etc/profile ]]; then
  source /etc/profile
fi

if [[ -f "${HOME}/.bash_profile" ]]; then
  source "${HOME}/.bash_profile"
elif [[ -f "${HOME}/.bash_login" ]]; then
  source "${HOME}/.bash_login"
elif [[ -f "${HOME}/.profile" ]]; then
  source "${HOME}/.profile"
elif [[ -f "${HOME}/.bashrc" ]]; then
  source "${HOME}/.bashrc"
fi

if [[ -n "${TSM_SESSION:-}" ]]; then
  _tsm_prompt_marker=""

  _tsm_refresh_session_name() {
    if [[ -n "${TSM_SESSION_FILE:-}" && -f "${TSM_SESSION_FILE}" ]]; then
      local _tsm_name
      IFS= read -r _tsm_name < "${TSM_SESSION_FILE}"
      if [[ -n "${_tsm_name}" ]]; then
        export TSM_SESSION="${_tsm_name}"
      fi
    fi
  }

  _tsm_apply_prompt_marker() {
    local _tsm_new_marker="[tsm:${TSM_SESSION}]"
    if [[ -n "${_tsm_prompt_marker}" && "${PS1:-}" == *"${_tsm_prompt_marker}"* ]]; then
      PS1="${PS1/${_tsm_prompt_marker}/${_tsm_new_marker}}"
    elif [[ "${PS1:-}" != *"${_tsm_new_marker}"* ]]; then
      PS1="\[\e[36m\]${_tsm_new_marker}\[\e[0m\] ${PS1:-\\$ }"
    fi
    _tsm_prompt_marker="${_tsm_new_marker}"
  }

  _tsm_precmd() {
    _tsm_refresh_session_name
    _tsm_apply_prompt_marker
    printf '\033]2;tsm:%s %s\007' "${TSM_SESSION}" "${PWD/#${HOME}/~}"
  }

  case ";${PROMPT_COMMAND:-};" in
    *";_tsm_precmd;"*) ;;
    *)
      if [[ -n "${PROMPT_COMMAND:-}" ]]; then
        PROMPT_COMMAND="_tsm_precmd;${PROMPT_COMMAND}"
      else
        PROMPT_COMMAND="_tsm_precmd"
      fi
      ;;
  esac
  _tsm_precmd

__TSM_BASH_FULL_BIND__
__TSM_BASH_PALETTE_BIND__
__TSM_BASH_TOGGLE_BIND__
fi
`

const fishConfigShimTemplate = `if set -q TSM_ORIG_XDG_CONFIG_HOME
  set -l _tsm_fish_root "$TSM_ORIG_XDG_CONFIG_HOME"
else
  set -l _tsm_fish_root "$HOME/.config"
end

set -l _tsm_user_config "$_tsm_fish_root/fish/config.fish"
if test -f "$_tsm_user_config"
  source "$_tsm_user_config"
end

if set -q TSM_SESSION
  function __tsm_refresh_session_name
    if test -n "$TSM_SESSION_FILE"; and test -f "$TSM_SESSION_FILE"
      set -l _tsm_name (string trim (cat "$TSM_SESSION_FILE"))
      if test -n "$_tsm_name"
        set -gx TSM_SESSION "$_tsm_name"
      end
    end
  end

  function __tsm_prompt_marker
    set_color cyan
    echo -n "[tsm:$TSM_SESSION] "
    set_color normal
  end

  if functions -q fish_prompt
    functions -c fish_prompt __tsm_orig_fish_prompt
    function fish_prompt
      __tsm_refresh_session_name
      __tsm_prompt_marker
      __tsm_orig_fish_prompt
    end
  else
    function fish_prompt
      __tsm_refresh_session_name
      __tsm_prompt_marker
      echo -n "> "
    end
  end

  function __tsm_update_title --on-event fish_prompt
    __tsm_refresh_session_name
    printf '\e]2;tsm:%s %s\a' "$TSM_SESSION" (prompt_pwd)
  end

  function __tsm_session_palette
    commandline -f repaint
    tsm p
    commandline -f repaint
  end
__TSM_FISH_PALETTE_BIND__

  function __tsm_session_full
    commandline -f repaint
    tsm tui
    commandline -f repaint
  end
__TSM_FISH_FULL_BIND__

  function __tsm_session_toggle
    commandline -f repaint
    tsm toggle
    commandline -f repaint
  end
__TSM_FISH_TOGGLE_BIND__
end
`

type shellShortcuts struct {
	Full    string
	Palette string
	Toggle  string
}

func defaultShellShortcuts() shellShortcuts {
	return shellShortcuts{
		Full:    "ctrl+[",
		Palette: "ctrl+]",
	}
}

func loadShellShortcuts(getenv func(string) string) (shellShortcuts, error) {
	shortcuts := defaultShellShortcuts()
	cfg, err := appconfig.Load(getenv)
	if err != nil {
		return shellShortcuts{}, err
	}
	if cfg.Shell.Shortcuts.Palette != nil {
		shortcuts.Palette = *cfg.Shell.Shortcuts.Palette
	}
	if cfg.Shell.Shortcuts.Full != nil {
		shortcuts.Full = *cfg.Shell.Shortcuts.Full
	}
	if cfg.Shell.Shortcuts.Toggle != nil {
		shortcuts.Toggle = *cfg.Shell.Shortcuts.Toggle
	}
	shortcuts.Full, err = normalizeDirectShellShortcut(shortcuts.Full, defaultShellShortcuts().Full)
	if err != nil {
		return shellShortcuts{}, err
	}
	shortcuts.Palette, err = normalizeDirectShellShortcut(shortcuts.Palette, defaultShellShortcuts().Palette)
	if err != nil {
		return shellShortcuts{}, err
	}
	shortcuts.Toggle, err = normalizeDirectShellShortcut(shortcuts.Toggle, "")
	if err != nil {
		return shellShortcuts{}, err
	}
	return shortcuts, nil
}

func normalizeDirectShellShortcut(raw, fallback string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.EqualFold(raw, "ctrl+[") {
		return raw, nil
	}
	if _, err := parseShellShortcut(raw, false); err != nil {
		if strings.HasPrefix(strings.ToLower(raw), "ctrl+") {
			return "", err
		}
		// Prefix mode was removed. Plain keys from old configs fall back.
		return fallback, nil
	}
	return raw, nil
}

func renderZshRcShim(shortcuts shellShortcuts) string {
	fullBind := renderZshBind(shortcuts.Full, "_tsm_session_full")
	paletteBind := renderZshBind(shortcuts.Palette, "_tsm_session_palette")
	toggleBind := renderZshBind(shortcuts.Toggle, "_tsm_session_toggle")
	return strings.NewReplacer(
		"__TSM_ZSH_FULL_BIND__", fullBind,
		"__TSM_ZSH_PALETTE_BIND__", paletteBind,
		"__TSM_ZSH_TOGGLE_BIND__", toggleBind,
	).Replace(zshRcShimTemplate)
}

func renderBashRcShim(shortcuts shellShortcuts) string {
	fullBind := renderBashBind(shortcuts.Full, "tsm tui")
	paletteBind := renderBashBind(shortcuts.Palette, "tsm p")
	toggleBind := renderBashBind(shortcuts.Toggle, "tsm toggle")
	return strings.NewReplacer(
		"__TSM_BASH_FULL_BIND__", fullBind,
		"__TSM_BASH_PALETTE_BIND__", paletteBind,
		"__TSM_BASH_TOGGLE_BIND__", toggleBind,
	).Replace(bashRcShimTemplate)
}

func renderFishConfigShim(shortcuts shellShortcuts) string {
	fullBind := renderFishBind(shortcuts.Full, "__tsm_session_full")
	paletteBind := renderFishBind(shortcuts.Palette, "__tsm_session_palette")
	toggleBind := renderFishBind(shortcuts.Toggle, "__tsm_session_toggle")
	return strings.NewReplacer(
		"__TSM_FISH_FULL_BIND__", fullBind,
		"__TSM_FISH_PALETTE_BIND__", paletteBind,
		"__TSM_FISH_TOGGLE_BIND__", toggleBind,
	).Replace(fishConfigShimTemplate)
}

func renderZshBind(raw, widget string) string {
	if raw == "" {
		return ""
	}
	key, _ := parseShellShortcut(raw, false)
	return fmt.Sprintf("    bindkey '%s' %s", zshShellNotation(key), widget)
}

func renderBashBind(raw, command string) string {
	if raw == "" {
		return ""
	}
	key, _ := parseShellShortcut(raw, false)
	return fmt.Sprintf("  bind -x '\"%s\":\"%s\"'", bashShellNotation(key), command)
}

func renderFishBind(raw, command string) string {
	if raw == "" {
		return ""
	}
	key, _ := parseShellShortcut(raw, false)
	return fmt.Sprintf("  bind %s %s", fishShellNotation(key), command)
}

type shellShortcutKey struct {
	Key  rune
	Ctrl bool
}

func parseShellShortcut(raw string, allowPlain bool) (shellShortcutKey, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return shellShortcutKey{}, nil
	}
	if !strings.HasPrefix(raw, "ctrl+") {
		if !allowPlain {
			return shellShortcutKey{}, fmt.Errorf("unsupported shortcut %q", raw)
		}
		key := []rune(raw)
		if len(key) != 1 || unicode.IsSpace(key[0]) {
			return shellShortcutKey{}, fmt.Errorf("unsupported shortcut %q", raw)
		}
		return shellShortcutKey{Key: key[0]}, nil
	}
	key := []rune(strings.TrimPrefix(raw, "ctrl+"))
	if len(key) != 1 {
		return shellShortcutKey{}, fmt.Errorf("unsupported shortcut %q", raw)
	}
	return shellShortcutKey{Key: key[0], Ctrl: true}, nil
}

func zshCtrlNotation(r rune) string {
	return "^" + strings.ToUpper(string([]rune{r}))
}

func bashCtrlNotation(r rune) string {
	return "\\C-" + string([]rune{r})
}

func fishCtrlNotation(r rune) string {
	return "\\c" + string([]rune{r})
}

func zshShellNotation(key shellShortcutKey) string {
	if isGhosttyCtrlLeftBracket(key) {
		return "\\e[91;5u"
	}
	return zshCtrlNotation(key.Key)
}

func bashShellNotation(key shellShortcutKey) string {
	if isGhosttyCtrlLeftBracket(key) {
		return "\\e[91;5u"
	}
	return bashCtrlNotation(key.Key)
}

func fishShellNotation(key shellShortcutKey) string {
	if isGhosttyCtrlLeftBracket(key) {
		return "\\e\\[91\\;5u"
	}
	return fishCtrlNotation(key.Key)
}

func isGhosttyCtrlLeftBracket(key shellShortcutKey) bool {
	return key.Ctrl && key.Key == '['
}

// putUint16LE, putUint32LE, putUint64LE write little-endian integers.
func putUint16LE(b []byte, v uint16) { b[0] = byte(v); b[1] = byte(v >> 8) }
func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}
func putUint64LE(b []byte, v uint64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (i * 8))
	}
}
