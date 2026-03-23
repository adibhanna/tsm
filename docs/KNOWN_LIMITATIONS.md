# Known Limitations

This document describes the current product boundary for `tsm`.

## Session Manager with Native Multiplexer

`tsm` manages long-lived terminal sessions. For splits and workspaces, `tsm mux` orchestrates your terminal emulator's native split system (cmux, kitty, or Ghostty).

Unlike tmux/zellij, `tsm mux` does not re-emulate VT output. Each pane is a real native terminal surface. This means:

- full GPU rendering, ligatures, native scrollback
- terminal-specific features (image protocols, etc.) work unchanged
- no server wrapping the terminal

Current mux limitations:

- cmux backend: `tsm mux` commands that create/modify layout require running from a non-attached cmux pane (cmux's default `cmuxOnly` socket mode blocks access from tsm session daemons)
- kitty backend: requires `allow_remote_control yes` and `enabled_layouts splits,tall,stack` in kitty.conf
- Ghostty backend: macOS only (uses AppleScript API), no `ReadScreen` support
- `tsm mux last` / `next` don't visually switch panes when run from a shell (pane navigation is best done via terminal keybindings)
- Alacritty, Terminal.app, and iTerm2 have no split APIs and are not supported as mux backends

## Shell Shortcut Scope

The built-in shell shortcut integration is session-local.

That means:

- built-in shortcuts only exist inside fresh TSM-managed shells
- they do not automatically exist in a normal shell outside TSM
- they do not automatically fire inside apps like Neovim, because those apps own the keyboard

If you want a global launcher outside TSM sessions, add your own shell, terminal, or app mapping.

## Agent Activity Is Advisory

Codex and Claude activity shown in the TUI is a status hint, not a hard execution guarantee.

Current behavior:

- Claude uses local transcript/state inference and can optionally use `tsm claude-statusline` for richer detached metadata
- Codex uses local state and rollout inference

So agent activity should be read as:

- useful for deciding whether to switch
- not a substitute for opening the session when precision matters

## Fresh Sessions After Integration Changes

`tsm` session daemons are long-lived.

That means:

- rebuilding `tsm` does not upgrade already-running session daemons
- shell integration changes only apply to fresh sessions
- some attach/restore behavior changes only show up after recreating the session

`tsm attach` and `tsm doctor` warn when a session is running an older daemon build, but the fix is still the same: recreate the session if you need the new behavior in that daemon.

## Source Builds Depend on the Supported Setup Path

The supported source workflow is:

```bash
make setup
make test
make build
```

Plain raw `go test ./...` is not the contributor contract for this repo unless the required Ghostty VT environment is already configured.

## Terminal Backend Contract

`tsm` is a Ghostty-VT-backed session manager.

That means:

- the supported product is the bundled `libghostty-vt` build
- restore quality depends on that backend being available

If you are debugging a local source build, use `tsm doctor` to confirm the runtime environment and backend state.
