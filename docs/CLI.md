# TSM CLI Reference

## Command Summary

```text
tsm
tsm tui [--simplified] [--keymap default|palette]
tsm palette
tsm claude-statusline
tsm config install [--force]
tsm attach [name]
tsm detach [name]
tsm new <name> [cmd...]
tsm list
tsm doctor
tsm doctor clean-stale
tsm debug session <name>
tsm rename <old> <new>
tsm mux open <workspace>
tsm mux split <left|right|up|down> <session>
tsm mux tab new <session> [cmd...]
tsm mux save <workspace>
tsm mux restore <workspace>
tsm mux doctor <workspace>
tsm mux sidebar sync <workspace>
tsm mux last
tsm mux next
tsm mux workspace [name]
tsm mux setup kitty
tsm mux status
tsm kill [name...]
tsm version
tsm help
```

## Aliases

```text
palette = p
attach  = a
detach  = d
new     = n
list    = l, ls
rename  = mv
kill    = k
mux     = m
version = v
help    = h
```

## Smart Attach

`tsm attach` with no name:

- no sessions: create a new session named after the current directory and attach
- one session: attach directly
- multiple sessions: open the TUI chooser

`tsm attach <name>`:

- attach to the named session
- create it if it does not exist
- if run inside another attached session, perform a local client-side switch instead of nesting the new attach inside the current PTY
- local switches avoid the full terminal clear path, so switching is less visually disruptive
- warn if the session daemon is still running an older `tsm` build after a rebuild

Examples:

```bash
tsm attach
tsm attach work
tsm attach api
```

## New Session

`tsm new <name>` creates the session daemon without attaching.

`tsm new <name> [cmd...]` starts a specific command inside the session instead of your default login shell.

Examples:

```bash
tsm new work
tsm new editor nvim
tsm new logs tail -f /var/log/system.log
tsm new api bash -lc 'npm run dev'
```

## Detach

`tsm detach` with no name uses `$TSM_SESSION`, so it detaches the current session when run from inside an attached shell.

`tsm detach <name>` detaches all attached clients from the named session without killing the daemon.

Interactive detach from an attached session:

```text
Ctrl+\
```

Supported detach key encodings:

- raw `Ctrl+\`
- Kitty keyboard protocol form

Examples:

```bash
tsm detach
tsm detach work
```

## Kill

`tsm kill` with no name uses `$TSM_SESSION`, so it kills the current session when run from inside an attached shell.

`tsm kill <name>...` kills one or more sessions.

Examples:

```bash
tsm kill
tsm kill work api repl
```

## List and Rename

List sessions:

```bash
tsm ls
```

Run diagnostics:

```bash
tsm doctor
```

Rename a session:

```bash
tsm rename old-name new-name
```

Renaming updates the daemon-side session name state, so prompt integration and picker metadata stay in sync for fresh sessions.

## Native Multiplexer

`tsm mux` orchestrates your terminal emulator's native split/tab system. Each pane is a real native terminal surface — no VT re-emulation.

### Open a workspace

```bash
tsm mux open dev
```

Creates sessions, native splits, attaches sessions, and runs startup commands from `~/.config/tsm/workspaces/dev.toml`.

### Ad-hoc splits and tabs

```bash
tsm mux split right my-shell
tsm mux split down my-logs
tsm mux tab new my-extra
```

### Save and restore

```bash
tsm mux save dev
tsm mux restore dev
```

### Workspace health

```bash
tsm mux doctor dev
```

### Sidebar sync (cmux only)

```bash
tsm mux sidebar sync dev
```

Pushes session and agent (claude/codex) state into the cmux sidebar.

### Navigation

```bash
tsm mux last          # Focus previous pane
tsm mux next          # Focus next pane
tsm mux workspace     # List workspaces
tsm mux workspace dev # Switch workspace
```

### Backend setup

```bash
tsm mux setup kitty   # Add allow_remote_control to kitty.conf
tsm mux status        # Show detected terminal and backend
```

### Supported backends

| Terminal | Detection env var | Splits | Tabs | Sidebar |
| -------- | ----------------- | ------ | ---- | ------- |
| cmux | `CMUX_SOCKET_PATH` | yes | yes | yes |
| kitty | `KITTY_PID` | yes | yes | no |
| Ghostty | `GHOSTTY_RESOURCES_DIR` | yes | yes | no |

Override with `TSM_MUX_BACKEND=cmux` (or `kitty`, `ghostty`).

## Diagnostics

`tsm doctor` prints a quick environment and runtime report.

It includes:

- binary version and active backend
- config path status
- socket directory
- `pkg-config` / `libghostty-vt` availability
- live and stale session sockets
- live sessions still running an older daemon build
- orphaned per-session sidecars with no matching socket

If stale sockets or orphaned sidecars are reported, remove them with:

```bash
tsm doctor clean-stale
```

`tsm debug session <name>` prints a deeper report for one session.

It includes:

- socket path
- live / stale / missing state
- daemon PID and attached-client count
- command and cwd
- task end state when available
- a short current preview snapshot

For the broader support matrix, see [COMPATIBILITY.md](/Users/adibhanna/Developer/opensource/tsm/docs/COMPATIBILITY.md).
For product boundaries and caveats, see [KNOWN_LIMITATIONS.md](/Users/adibhanna/Developer/opensource/tsm/docs/KNOWN_LIMITATIONS.md).

## TUI Entry Points

Full TUI:

```bash
tsm
tsm tui
```

The full TUI shows a compact Codex / Claude activity line for the selected session when TSM detects one of those agents running inside that session.

Simplified palette:

```bash
tsm tui --simplified
tsm palette
tsm p
```

The simplified palette shows the same selected-session agent activity line, so you can check what Codex or Claude was doing before attaching.

## Claude Code Statusline Integration

If you want richer detached Claude previews, configure Claude Code to run:

```text
tsm claude-statusline
```

as its `statusLine.command`.

That command:

- captures Claude's official structured statusline JSON into a per-session sidecar
- prints a compact status line back to Claude Code

With this enabled, the full TUI can show official Claude fields like:

- model and version
- cost and duration
- API wait time
- lines added / removed
- output style
- project directory and worktree path

Without it, TSM falls back to local Claude transcript inference.

Layout toggle inside the TUI:

```text
Ctrl+O
```

## Full TUI Default Bindings

| Key | Action |
| --- | --- |
| `↑` `↓` | Navigate sessions |
| `←` `→` | Scroll preview |
| `space` | Toggle selection |
| `ctrl+a` | Select or deselect all |
| `enter` | Attach |
| `d` | Detach selected session(s) |
| `n` | New session |
| `k` | Kill selected session(s) |
| `r` | Rename session |
| `c` | Copy attach command |
| `s` | Cycle sort mode |
| `ctrl+o` | Toggle full / simplified layout |
| `/` | Filter |
| `[` `]` | Scroll activity log |
| `ctrl+r` | Refresh |
| `q` | Quit |

## Simplified Palette Bindings

The simplified palette uses the same selected keymap as the full TUI.

Default keymap:

| Key | Action |
| --- | --- |
| `↑` `↓` | Navigate |
| `space` | Toggle selection |
| `ctrl+a` | Select all |
| `enter` | Attach |
| `d` | Detach |
| `n` | New session |
| `k` | Kill |
| `r` | Rename |
| `c` | Copy attach command |
| `s` | Sort |
| `ctrl+o` | Toggle layout |
| `/` | Filter |
| `ctrl+r` | Refresh |
| `q` | Quit |

Palette keymap:

| Key | Action |
| --- | --- |
| `type` | Filter immediately |
| `↑` `↓` | Navigate |
| `tab` | Toggle selection |
| `ctrl+a` | Select all |
| `enter` | Attach |
| `ctrl+d` | Detach |
| `ctrl+t` | New session |
| `ctrl+x` | Kill |
| `r` | Rename |
| `ctrl+y` | Copy attach command |
| `ctrl+s` | Sort |
| `ctrl+o` | Toggle layout |
| `ctrl+r` | Refresh |
| `ctrl+c` | Quit |

While the palette keymap is active:

- typing filters immediately
- `esc` clears the filter
- `esc` quits if the filter is already empty

## Config

Install the default config template:

```bash
tsm config install
```

Overwrite an existing file:

```bash
tsm config install --force
```

Default config path:

```text
~/.config/tsm/config.toml
```

Override config path:

```bash
export TSM_CONFIG_FILE=/path/to/config.toml
```

Example:

```toml
[tui]
mode = "simplified"
keymap = "default"
show_help = false

[tui.keymaps.default]
move_up = ["k"]
move_down = ["j"]
detach = ["x"]
toggle_layout = ["ctrl+o"]

[shell.shortcuts]
full = ""
palette = "ctrl+]"
toggle = ""
```

Config precedence:

1. built-in defaults
2. config file
3. environment variables
4. CLI flags

Supported action names:

- `move_up`
- `move_down`
- `move_left`
- `move_right`
- `toggle_select_all`
- `toggle_select`
- `attach`
- `detach`
- `new_session`
- `kill`
- `rename`
- `copy_command`
- `sort`
- `toggle_layout`
- `filter`
- `refresh`
- `quit`
- `force_quit`
- `log_up`
- `log_down`

## Shell Shortcuts

Think about shortcuts in three layers:

- built-in in-session shortcut
- optional global launcher you add yourself
- app-level mapping inside tools like Neovim

Recommended workflow:

- inside a fresh TSM-managed shell, use the built-in `Ctrl+]` shortcut
- outside TSM, run `tsm p` directly or add your own global launcher
- inside apps like Neovim, use your own app mapping or global launcher if you want picker access without going back to the shell prompt

Built-in in-session shortcut:

- `Ctrl+]` opens the simplified palette

Supported integrated shells:

- `zsh`
- `bash`
- `fish`

Child sessions created from inside an attached session preserve the original shell config path instead of recursively inheriting the generated TSM shim.

If you want a global shell shortcut from anywhere, add one of these snippets.

For `zsh`:

```zsh
tsm_palette() {
  zle -I
  tsm p
  zle reset-prompt
}
zle -N tsm_palette
bindkey '^g' tsm_palette
```

For `bash`:

```bash
tsm_palette() {
  tsm p
}
bind -x '"\C-g":tsm_palette'
```

`Ctrl+G` is the recommended global launcher because it stays out of the built-in `Ctrl+]` TSM shell shortcut.

## Useful Checks

Check the installed backend:

```bash
tsm version
```

See running sessions:

```bash
tsm ls
```
