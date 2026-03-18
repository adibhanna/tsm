# tsm

A terminal session manager. Create, attach, detach, and manage persistent terminal sessions from a TUI.

Sessions run as background daemons with PTY support — they survive disconnects and can be reattached from anywhere. No external dependencies required.

## Install

### From source

```
go install github.com/adibhanna/tsm@latest
```

### Build from repo

```
git clone https://github.com/adibhanna/tsm.git
cd tsm
go build -o tsm .
```

## Usage

Run `tsm` to open the session manager. From there you can create, attach to, filter, and kill sessions.

To attach to a session, select it and press `enter`. Press `ctrl+\` to detach back to your shell.

## Key Bindings

| Key | Action |
|-----|--------|
| `↑` `↓` | Navigate sessions |
| `←` `→` | Scroll preview |
| `space` | Toggle selection |
| `ctrl+a` | Select / deselect all |
| `enter` | Attach to session |
| `n` | New session |
| `k` | Kill selected session(s) |
| `c` | Copy attach command |
| `s` | Cycle sort mode |
| `/` | Filter sessions |
| `[` `]` | Scroll activity log |
| `r` | Refresh |
| `q` | Quit |

## How It Works

Each session is a daemon process that holds a PTY and listens on a Unix domain socket. The TUI communicates with daemons over a simple binary IPC protocol to list, create, attach, get history, and kill sessions.

Sessions are stored as socket files in `$TMPDIR/tsm-{uid}/` (configurable via `$TSM_DIR`).

## Environment Variables

| Variable | Description |
|----------|-------------|
| `TSM_DIR` | Override the socket directory |
| `TSM_SESSION` | Set automatically inside sessions (contains the session name) |

## License

[MIT](LICENSE)
