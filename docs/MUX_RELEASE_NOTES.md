# tsm mux — Native Terminal Multiplexer

## What we built

A native terminal multiplexer for tsm that orchestrates splits, tabs, and workspaces through your terminal emulator's own API — no VT re-emulation, no server wrapping your terminal. Every pane is a real native terminal surface with full GPU rendering, ligatures, native scrollback, and image protocol support.

Unlike tmux and zellij (which act as terminal emulators themselves, re-parsing and re-rendering all output), tsm mux delegates layout to the terminal and keeps tsm focused on what it does best: persistent sessions.

## The problem with tmux

tmux runs a server that sits between your terminal and your shell. It parses all VT output, maintains its own cell grid, and re-renders ANSI to your terminal. This means you lose:

- GPU-accelerated rendering
- Native scrollback
- Font ligatures
- Image protocols (Sixel, kitty graphics)
- Terminal-specific extensions
- Native tab/split chrome

## How tsm mux works

```
tsm mux open dev
  ├── reads workspace manifest (TOML)
  ├── creates splits/tabs via terminal's native API
  ├── spawns persistent tsm sessions
  ├── attaches each session into its pane
  └── runs startup commands (nvim, claude, etc.)
```

Each pane is a real terminal surface. Zero re-emulation. 100% native features.

## Four backends, one interface

### cmux

- Full support: splits, tabs, workspaces, sidebar status sync
- Agent state (Claude/Codex) pushed to cmux sidebar
- Detected via `CMUX_SOCKET_PATH`

### kitty

- Splits, tabs, workspaces via `kitten @` remote control
- Requires `allow_remote_control yes` in kitty.conf
- One-command setup: `tsm mux setup kitty`
- Detected via `KITTY_PID`

### Ghostty

- Splits, tabs, workspaces via AppleScript API
- macOS only, requires Ghostty 1.3.0+
- Detected via `GHOSTTY_RESOURCES_DIR`

### WezTerm

- Splits, tabs, workspaces via `wezterm cli` subcommands
- Works out of the box — no config changes needed
- Detected via `WEZTERM_UNIX_SOCKET` or `WEZTERM_PANE`

All backends auto-detected from environment variables. Override with `TSM_MUX_BACKEND`.

## Workspace manifests

Define your dev environment as a TOML file:

```toml
name = "dev"
version = 1

[[surface]]
name = "editor"
session = "editor"
cwd = "~/Developer/myproject"
command = "nvim ."

  [[surface.split]]
  name = "shell"
  session = "shell"
  direction = "right"
  cwd = "~/Developer/myproject"
```

One command opens it all:

```bash
tsm mux open dev
```

Sessions persist across terminal restarts. Close everything, reopen later:

```bash
tsm mux restore dev
```

## All new commands

| Command                         | What it does                                                      |
| ------------------------------- | ----------------------------------------------------------------- |
| `tsm mux open <workspace>`      | Open workspace — creates splits, attaches sessions, runs commands |
| `tsm mux new <workspace>`       | Create a new workspace manifest with sample template              |
| `tsm mux edit`                  | Open workspace directory in $EDITOR                               |
| `tsm mux split <dir> <session>` | Ad-hoc split: `tsm mux split right my-shell`                      |
| `tsm mux tab new <session>`     | New tab with a session attached                                   |
| `tsm mux save <workspace>`      | Save current workspace manifest                                   |
| `tsm mux restore <workspace>`   | Restore workspace from saved manifest                             |
| `tsm mux doctor <workspace>`    | Check which sessions are alive/dead                               |
| `tsm mux sidebar sync <ws>`     | Push Claude/Codex agent state to cmux sidebar                     |
| `tsm mux last`                  | Focus previous pane                                               |
| `tsm mux next`                  | Focus next pane                                                   |
| `tsm mux workspace [name]`      | List or switch workspaces                                         |
| `tsm mux setup kitty`           | Configure kitty for remote control                                |
| `tsm mux status`                | Show detected terminal, backend, workspace info                   |

## TUI integration

- Terminal indicator in title bar: `tsm [ghostty] (3)`
- Press `w` to open workspace picker
- Select a workspace with arrows, Enter to open
- Works in both full and simplified TUI modes

## Startup commands

Workspace manifests support a `command` field that runs after the session attaches:

```toml
[[surface]]
name = "editor"
session = "editor"
command = "nvim ."

  [[surface.split]]
  name = "server"
  session = "server"
  command = "npm run dev"
```

Works with anything: `nvim`, `claude`, `htop`, `tail -f`, `npm run dev`.

## Agent sidebar sync (cmux)

Push Claude Code and Codex activity state to the cmux sidebar:

```bash
tsm mux sidebar sync dev
```

Shows per-session status like:

- `claude · working · implementing auth flow`
- `codex · thinking`
- `2/2 sessions live`

## Terminal detection

tsm auto-detects your terminal and selects the right backend:

| Terminal  | Detected via            | Backend   |
| --------- | ----------------------- | --------- |
| cmux      | `CMUX_SOCKET_PATH`      | cmux      |
| kitty     | `KITTY_PID`             | kitty     |
| Ghostty   | `GHOSTTY_RESOURCES_DIR` | ghostty   |
| WezTerm   | `WEZTERM_UNIX_SOCKET`   | wezterm   |
| iTerm2    | `ITERM_SESSION_ID`      | none      |
| Alacritty | `ALACRITTY_WINDOW_ID`   | none      |

## Architecture

```
                    tsm mux CLI
                         │
               ┌─────────┼──────────┐
               ▼         ▼          ▼
          cmux CLI    kitten @    osascript
          backend     backend     backend
               │         │          │
               ▼         ▼          ▼
            cmux       kitty     Ghostty
         (native)    (native)   (native)
```

The `Backend` interface abstracts all terminal control:

- `SplitPane`, `CreateSurface`, `CreateWorkspace` — layout
- `SendText`, `SendTextToWorkspace` — input
- `ListPaneSurfaces`, `GetFocusedPane` — introspection
- `SetStatus`, `Log` — sidebar (cmux only)

Adding a new backend (WezTerm, etc.) is one file implementing the interface.

## Other improvements

- **Session name "logs" fix**: moved internal log directory from `socketDir/logs` to `socketDir.logs` so "logs" is a valid session name (with auto-migration for existing installs)
- **Security**: Ghostty backend sanitizes all IDs interpolated into AppleScript to prevent injection
- **Reliability**: all backends use `Output()` instead of `CombinedOutput()` so stderr doesn't corrupt JSON parsing

## What's NOT in this release

- No VT re-emulation (by design — that's the whole point)
- WezTerm pane navigation (`last`/`next`) works natively via `activate-pane-direction`
- No Linux Ghostty support (Ghostty's D-Bus API doesn't support splits yet)
- No Alacritty/iTerm2 support (no split APIs)
- Pane navigation (`last`/`next`) works best via terminal keybindings, not CLI

## Demo flow

```bash
# 1. Create a workspace
tsm mux new myproject

# 2. Edit the manifest
tsm mux edit

# 3. Open it — splits appear with sessions attached
tsm mux open myproject

# 4. Ad-hoc split
tsm mux split down my-logs

# 5. New tab
tsm mux tab new my-tests

# 6. Check health
tsm mux doctor myproject

# 7. Close terminal, come back later
tsm mux restore myproject

# 8. From the TUI — press w, select workspace, Enter
tsm
```
