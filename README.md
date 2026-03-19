# tsm

A terminal session manager with persistent PTY-backed sessions, a TUI, and Ghostty-powered screen restoration.

## What It Does

- Creates long-lived shell sessions as background daemons
- Reattaches full-screen apps like Neovim using Ghostty's VT engine
- Shows a live colored preview of the current screen in the TUI
- Lets you switch sessions from inside another attached session
- Supports detach and kill control commands from inside the current session

## Install

### Prebuilt releases

Release archives are self-contained. They bundle `tsm` together with `libghostty-vt`, so users do not need to install Ghostty separately.

### Build from source

If you are building from source, TSM needs `libghostty-vt` available via `pkg-config`.

The repo can build it locally for you:

```bash
git clone https://github.com/adibhanna/tsm.git
cd tsm
make setup-ghostty-vt
make build
```

That clones Ghostty into `./ghostty`, builds `libghostty-vt` into `./.ghostty-prefix`, and links `tsm` against it.

Optional fallback build:

```bash
make build-fallback
```

The fallback mode works, but it does not fully restore full-screen terminal state.

## Usage

Run `tsm` to open the TUI.

Key CLI flows:

- `tsm attach`
  - no sessions: create a new session named after the current directory and attach
  - one session: attach directly
  - multiple sessions: open the chooser
- `tsm attach <name>`
  - attach to a named session
  - create it if it does not exist
  - if run from inside another attached session, switch locally instead of nesting
- `tsm detach`
  - inside a session, detach the current session via `$TSM_SESSION`
- `tsm detach <name>`
  - detach all attached clients from that session without killing it
- `tsm kill`
  - inside a session, kill the current session via `$TSM_SESSION`
- `tsm kill <name>...`
  - kill one or more named sessions

Detach from an attached session with `Ctrl+\`.

## TUI

The TUI supports:

- session list and sorting
- colored live preview of the current terminal screen
- create / rename / kill flows
- copy attach command
- attach and session switching

Key bindings:

| Key | Action |
| --- | --- |
| `↑` `↓` | Navigate sessions |
| `←` `→` | Scroll preview |
| `space` | Toggle selection |
| `ctrl+a` | Select / deselect all |
| `enter` | Attach to selected session |
| `n` | New session |
| `k` | Kill selected session(s) |
| `R` | Rename session |
| `c` | Copy attach command |
| `s` | Cycle sort mode |
| `/` | Filter sessions |
| `[` `]` | Scroll activity log |
| `r` | Refresh |
| `q` | Quit |

## Build & Test

```bash
make setup-ghostty-vt
make build
make test
make release
```

`make release` creates a self-contained archive for the current platform.

## How It Works

Each session is a daemon process that owns a PTY and listens on a Unix domain socket. The daemon feeds PTY output into a terminal backend:

- default: `libghostty-vt`
- fallback: lightweight mode tracker behind `-tags noghosttyvt`

On attach, the daemon serializes the current terminal state and sends it to the client before resize, which allows full-screen apps to restore correctly. The TUI preview uses the same tracked terminal state, not a raw byte replay.

For `zsh`, TSM also injects a small shell integration shim:

- prompt prefix: `[tsm:<name>]`
- terminal title: `tsm:<name> ...`
- env var: `$TSM_SESSION`

Sessions live under `$TSM_DIR` or the resolved runtime temp directory.

## Environment Variables

| Variable | Description |
| --- | --- |
| `TSM_DIR` | Override the socket directory |
| `TSM_SESSION` | Set automatically inside sessions |
| `TSM_SHELL_INTEGRATION` | Set to `zsh` when the zsh prompt/title shim is active |
| `SHELL` | Default shell used for new sessions |

## License

[MIT](LICENSE)
