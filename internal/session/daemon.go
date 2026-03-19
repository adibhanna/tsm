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
	"unsafe"

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
	shell, argv := resolveShell(shellCmd)

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
		name:       name,
		cfg:        cfg,
		cmd:        cmd,
		ptmx:       ptmx,
		listener:   ln,
		scrollback: NewScrollback(scrollbackSize),
		terminal:   NewTerminalBackend(uint16(rows), uint16(cols)),
		createdAt:  time.Now(),
		clients:    make(map[net.Conn]*clientState),
		done:       make(chan struct{}),
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

		case TagRun:
			// For now, just acknowledge.
			d.sendMessage(conn, TagAck, nil, ioTimeout)
		}
	}
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

// resolveShell determines the shell command to run.
// Returns (executable path, full argv). For a default login shell
// argv[0] is "-zsh"/"-bash"/etc., which the shell interprets as
// "I am a login shell". For explicit commands argv is passed through.
func resolveShell(shellCmd []string) (string, []string) {
	if len(shellCmd) > 0 {
		return shellCmd[0], shellCmd
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// Login shell: argv[0] starts with "-"
	argv0 := "-" + shell[strings.LastIndex(shell, "/")+1:]
	return shell, []string{argv0}
}

func buildDaemonEnv(cfg Config, name, shell string, shellCmd []string) ([]string, error) {
	var env []string
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "TSM_SESSION="):
		case strings.HasPrefix(e, "ZDOTDIR="):
		case strings.HasPrefix(e, "TSM_ORIG_ZDOTDIR="):
		case strings.HasPrefix(e, "TSM_SHELL_INTEGRATION="):
		default:
			env = append(env, e)
		}
	}
	env = append(env, "TSM_SESSION="+name)

	if shouldUseZshIntegration(shell, shellCmd) {
		dir, err := ensureZshIntegration(cfg, name)
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
		env = append(env, "TSM_SHELL_INTEGRATION=zsh")
	}

	return env, nil
}

func shouldUseZshIntegration(shell string, shellCmd []string) bool {
	if len(shellCmd) > 0 {
		return false
	}
	return filepath.Base(shell) == "zsh"
}

func ensureZshIntegration(cfg Config, name string) (string, error) {
	dir := filepath.Join(cfg.SocketDir, ".zsh", name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("mkdir zsh integration dir: %w", err)
	}

	files := map[string]string{
		".zshenv":   zshEnvShim,
		".zprofile": zshProfileShim,
		".zshrc":    zshRcShim,
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

const zshEnvShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zshenv" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zshenv"
fi
`

const zshProfileShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zprofile" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zprofile"
fi
`

const zshRcShim = `if [[ -n "${TSM_ORIG_ZDOTDIR:-}" && -f "${TSM_ORIG_ZDOTDIR}/.zshrc" ]]; then
  source "${TSM_ORIG_ZDOTDIR}/.zshrc"
fi

if [[ -n "${TSM_SESSION:-}" ]]; then
  typeset -g _tsm_prompt_marker="[tsm:${TSM_SESSION}]"
  case "${PROMPT:-}" in
    *"${_tsm_prompt_marker}"*) ;;
    *)
      PROMPT="%F{6}${_tsm_prompt_marker}%f ${PROMPT:-%# }"
      ;;
  esac

  autoload -Uz add-zsh-hook
  _tsm_precmd_title() {
    print -Pn -- "\e]2;tsm:${TSM_SESSION} %~\a"
  }
  if (( ${precmd_functions[(Ie)_tsm_precmd_title]} == 0 )); then
    add-zsh-hook precmd _tsm_precmd_title
  fi
  _tsm_precmd_title
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
