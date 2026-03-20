# Known Limitations

This document describes the current product boundary for `tsm`.

## Session Manager, Not a Multiplexer

`tsm` manages long-lived terminal sessions.

It does not manage:

- panes
- splits
- tmux-style window layouts

Use your terminal or application for those:

- Ghostty splits for multiple visible terminals
- Neovim splits for editor panes
- multiple `tsm` sessions plus the palette for fast switching

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
