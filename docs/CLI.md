# TSM CLI Reference

## Command Summary

```text
tsm
tsm tui [--simplified] [--keymap default|palette]
tsm palette
tsm config install [--force]
tsm attach [name]
tsm detach [name]
tsm new <name> [cmd...]
tsm list
tsm rename <old> <new>
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

Rename a session:

```bash
tsm rename old-name new-name
```

Renaming updates the daemon-side session name state, so prompt integration and picker metadata stay in sync for fresh sessions.

## TUI Entry Points

Full TUI:

```bash
tsm
tsm tui
```

Simplified palette:

```bash
tsm tui --simplified
tsm palette
tsm p
```

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
| `R` | Rename session |
| `c` | Copy attach command |
| `s` | Cycle sort mode |
| `ctrl+o` | Toggle full / simplified layout |
| `/` | Filter |
| `[` `]` | Scroll activity log |
| `r` | Refresh |
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
| `R` | Rename |
| `c` | Copy attach command |
| `s` | Sort |
| `ctrl+o` | Toggle layout |
| `/` | Filter |
| `r` | Refresh |
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
| `ctrl+r` | Rename |
| `ctrl+y` | Copy attach command |
| `ctrl+s` | Sort |
| `ctrl+o` | Toggle layout |
| `ctrl+l` | Refresh |
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

Inside fresh TSM-managed interactive shells, `Ctrl+P` opens the simplified palette by default.

Supported integrated shells:

- `zsh`
- `bash`
- `fish`

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

`Ctrl+G` is recommended for the global shell shortcut because it avoids clobbering the common shell-history meaning of `Ctrl+P`.

## Useful Checks

Check the installed backend:

```bash
tsm version
```

See running sessions:

```bash
tsm ls
```
