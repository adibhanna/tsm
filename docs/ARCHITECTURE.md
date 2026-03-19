# TSM Architecture

## Overview

TSM manages persistent terminal sessions as background daemons. Each session owns a PTY, exposes a Unix socket, and tracks terminal state so clients can detach and later restore the same screen.

## Process Model

```text
tsm attach <name>          tsm --daemon <name>
┌─────────────┐            ┌─────────────────────────┐
│   Client    │◄──unix───►│         Daemon          │
│  (Attach)   │  socket   │  ┌───────────────────┐  │
│ stdin→Input │           │  │    PTY master     │  │
│ Output→tty  │           │  │         ↕         │  │
└─────────────┘           │  │  shell / nvim /   │  │
                          │  │   full-screen app │  │
                          │  └───────────────────┘  │
                          │  terminal backend       │
                          │  socket listener        │
                          └─────────────────────────┘
```

- one daemon per session
- daemon is spawned by re-exec with `--daemon`
- daemon owns the PTY and accepts multiple attached clients
- PTY output is fed into the terminal backend and broadcast to attached clients

## Package Structure

### `internal/session/`

| File | Purpose |
| --- | --- |
| `config.go` | Socket directory resolution |
| `socket.go` | Unix socket connect/read/write helpers |
| `ipc.go` | Wire protocol tags and message framing |
| `session.go` | List, rename, kill, detach control helpers |
| `daemon.go` | Daemon lifecycle, PTY, socket listener, attach/init handling |
| `client.go` | Interactive attach client, raw mode, resize handling, detach detection |
| `termstate.go` | Terminal backend interface and mode-tracker fallback |
| `terminal_backend_ghostty.go` | `libghostty-vt` backend |
| `terminal_backend_default.go` | fallback backend behind `noghosttyvt` |
| `switch.go` | local session-switch control sequence handling |
| `scrollback.go` | raw PTY byte ring buffer used as fallback history |

### `internal/engine/`

| File | Purpose |
| --- | --- |
| `sessions.go` | Higher-level session operations for the TUI |
| `process.go` | Process-tree memory and uptime collection |
| `preview.go` | ANSI-aware preview cropping and width handling |

### `internal/tui/`

| File | Purpose |
| --- | --- |
| `model_core.go` | TUI state and command wiring |
| `model_input.go` | keyboard handling |
| `model_view.go` | list / preview / log rendering |
| `styles.go` | visual styles |

### `main.go`

CLI entrypoint and smart command behavior:

- `attach` without a name: smart attach
- `attach` from inside another session: local session switch
- `detach` without a name: use `$TSM_SESSION`
- `kill` without a name: use `$TSM_SESSION`

## IPC Protocol

Messages use a 5-byte header:

```text
tag:u8 + length:u32(le) + payload
```

Relevant tags:

- `Input` (0): client keystrokes to daemon PTY
- `Output` (1): daemon PTY/restore output to client
- `Resize` (2): terminal resize
- `Detach` (3): detach current client
- `DetachAll` (4): detach all attached clients
- `Kill` (5): kill session daemon/process
- `Info` (6): session metadata
- `Init` (7): initial attach handshake
- `History` (8): TUI preview request

## Terminal Backend

### Default backend: `libghostty-vt`

The default build uses Ghostty's VT library to:

- consume PTY output continuously
- serialize a VT snapshot on attach
- produce the current screen preview for the TUI

This is what makes Neovim/full-screen reattach work.

### Fallback backend: `noghosttyvt`

The fallback backend only tracks a small set of terminal modes:

- alt screen
- mouse modes
- bracketed paste
- focus events
- cursor visibility

It can restore modes, but not exact screen contents.

## Key Flows

### Session creation

1. `SpawnDaemon()` re-execs the binary with `--daemon <name>`
2. daemon creates/listens on the session socket
3. daemon starts the shell under a PTY
4. daemon injects `TSM_SESSION`
5. for default `zsh`, daemon also generates a `ZDOTDIR` shim for prompt/title integration

### Attach

1. client enters raw mode
2. client sends `Init` with current terminal size
3. daemon sends a VT snapshot of current terminal state
4. daemon resizes the PTY/backend and signals the foreground process group
5. client relays PTY output until detach or disconnect

### Preview

1. TUI asks daemon for `History`
2. daemon returns preview content from the terminal backend
3. TUI keeps ANSI styling intact and crops horizontally with ANSI-aware width logic

### Session switch from inside a session

1. inner `tsm attach <other>` detects `$TSM_SESSION`
2. it emits a private control sequence instead of nesting attach locally
3. outer attach client intercepts that sequence
4. outer client `exec`s into `tsm attach <other>`

## Environment Variables

| Variable | Purpose |
| --- | --- |
| `TSM_DIR` | Override socket directory |
| `TSM_SESSION` | Current session name inside attached shells |
| `TSM_SHELL_INTEGRATION` | Shell integration mode, currently `zsh` |
| `SHELL` | Default shell used for new sessions |

## Release Model

- source builds require `libghostty-vt` available via `pkg-config`
- `make setup-ghostty-vt` vendors Ghostty locally into the repo
- `make release` produces a self-contained archive bundling `tsm` and `libghostty-vt`
