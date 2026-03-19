# TSM CLI Reference

## Commands

```text
tsm                          Open interactive TUI
tsm tui                      Open interactive TUI
tsm attach [name]            Attach, smart-attach, or switch sessions
tsm detach [name]            Detach current or named session without killing it
tsm new <name> [cmd...]      Create a new session
tsm list                     List active sessions
tsm rename <old> <new>       Rename a session
tsm kill [name...]           Kill current or named sessions
tsm version                  Show version and active backend
tsm help                     Show help
```

## Aliases

```text
attach  = a
detach  = d
new     = n
list    = l, ls
rename  = mv
kill    = k
version = v
help    = h
```

## Attach Behavior

`tsm attach` with no name:

- no sessions: create a new session named after the current directory and attach
- one session: attach directly
- multiple sessions: open the TUI chooser

`tsm attach <name>`:

- attaches to the named session
- creates it if it does not exist
- if run from inside another attached session, performs a local client-side switch instead of nesting one attach inside another PTY

## Detach Behavior

`tsm detach` with no name uses `$TSM_SESSION`, so it detaches the current session when run from inside an attached shell.

`tsm detach <name>` sends a detach request to that session and disconnects all attached clients without killing the daemon.

Detach from an attached session interactively with `Ctrl+\`. Both raw `Ctrl+\` and Kitty keyboard protocol are supported.

## Kill Behavior

`tsm kill` with no name uses `$TSM_SESSION`, so it kills the current session when run from inside an attached shell.

`tsm kill <name>...` kills one or more named sessions in sequence.

## TUI Key Bindings

| Key | Action |
| --- | --- |
| `↑↓` | Navigate sessions |
| `←→` | Scroll preview |
| `space` | Toggle selection |
| `Ctrl+A` | Select all |
| `Enter` | Attach to selected session |
| `n` | New session |
| `k` | Kill selected session(s) |
| `R` | Rename session |
| `c` | Copy attach command |
| `s` | Cycle sort mode |
| `/` | Filter sessions |
| `[]` | Scroll activity log |
| `q` | Quit |

## Build

```bash
make setup-ghostty-vt   # Clone/build libghostty-vt into the repo
make build              # Build binary with libghostty-vt
make test               # Run tests
make release            # Create a self-contained current-platform archive
make build-fallback     # Build without libghostty-vt
make test-fallback      # Run tests without libghostty-vt
make clean              # Remove build artifacts
```
