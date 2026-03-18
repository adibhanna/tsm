package session

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
)

const scrollbackSize = 10 * 1024 * 1024 // 10 MB, default

// Daemon manages a single session: a PTY, a Unix socket, and connected clients.
type Daemon struct {
	name       string
	cfg        Config
	cmd        *exec.Cmd
	ptmx       *os.File
	listener   net.Listener
	scrollback *Scrollback
	createdAt  time.Time

	mu      sync.RWMutex
	clients map[net.Conn]bool

	done     chan struct{}
	doneOnce sync.Once
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
	shell, args := resolveShell(shellCmd)

	cmd := exec.Command(shell, args...)
	cmd.Env = buildDaemonEnv(name)
	cmd.Dir, _ = os.Getwd()

	// Start the command with a PTY.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	defer ptmx.Close()

	d := &Daemon{
		name:       name,
		cfg:        cfg,
		cmd:        cmd,
		ptmx:       ptmx,
		listener:   ln,
		scrollback: NewScrollback(scrollbackSize),
		createdAt:  time.Now(),
		clients:    make(map[net.Conn]bool),
		done:       make(chan struct{}),
	}

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
		d.clients[conn] = true
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
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
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
			}

		case TagInit:
			// Client just connected — send current terminal state.
			if len(payload) >= 4 {
				rows := uint16(payload[0]) | uint16(payload[1])<<8
				cols := uint16(payload[2]) | uint16(payload[3])<<8
				pty.Setsize(d.ptmx, &pty.Winsize{
					Rows: rows,
					Cols: cols,
				})
			}

		case TagInfo:
			d.sendInfo(conn)

		case TagHistory:
			data := d.scrollback.Bytes()
			SendMessage(conn, TagHistory, data)

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
			SendMessage(conn, TagAck, nil)
		}
	}
}

func (d *Daemon) broadcast(tag Tag, data []byte) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	msg := MarshalMessage(tag, data)
	for conn := range d.clients {
		conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
		conn.Write(msg)
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

	SendMessage(conn, TagInfo, data)
}

func (d *Daemon) buildInfo() InfoPayload {
	d.mu.RLock()
	clientCount := len(d.clients)
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
	d.clients = make(map[net.Conn]bool)
}

// resolveShell determines the shell command to run.
func resolveShell(shellCmd []string) (string, []string) {
	if len(shellCmd) > 0 {
		return shellCmd[0], shellCmd[1:]
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// Login shell: argv[0] starts with "-"
	name := "-" + shell[strings.LastIndex(shell, "/")+1:]
	return shell, []string{name}
}

func buildDaemonEnv(name string) []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "TSM_SESSION=") {
			env = append(env, e)
		}
	}
	env = append(env, "TSM_SESSION="+name)
	return env
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

