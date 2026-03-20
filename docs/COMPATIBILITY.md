# Compatibility

This document is the current support contract for `tsm`.

## Release Targets

Tagged release archives are built for:

| OS | Architecture | Status |
| --- | --- | --- |
| macOS | `amd64` | supported |
| macOS | `arm64` | supported |
| Linux | `amd64` | supported |
| Linux | `arm64` | supported |

All release archives bundle `libghostty-vt`.

## Shells

The built-in interactive shell integration is supported for:

| Shell | Status | Notes |
| --- | --- | --- |
| `zsh` | supported | prompt/title integration and built-in in-session shortcut |
| `bash` | supported | prompt/title integration and built-in in-session shortcut |
| `fish` | supported | prompt/title integration and built-in in-session shortcut |

Notes:

- shell integration is session-local, not a global launcher
- fresh sessions pick up the current shell integration logic
- already-running sessions keep the shell environment they started with

## Terminal / Restore Backend

`tsm` ships as a Ghostty-VT-backed session manager.

| Terminal concern | Status | Notes |
| --- | --- | --- |
| bundled `libghostty-vt` restore backend | required | this is the supported restore path |
| full-screen restore (`nvim`, TUIs) | supported | relies on bundled `libghostty-vt` |
| plain shell sessions | supported | same backend, simpler workload |

Notes:

- the supported product is the bundled `libghostty-vt` build
- source builds are supported through `make setup`, `make build`, and `make test`

## Homebrew

Homebrew support is through the `adibhanna/tsm` tap and uses the bundled release artifacts.

| Install path | Status | Notes |
| --- | --- | --- |
| `brew install adibhanna/tsm/tsm` | supported | uses published release archives |
| `brew search tsm` / `homebrew/core` | not supported | `tsm` is not in `homebrew/core` |

## Agent Activity Preview

Agent activity in the TUI is supported for:

| Agent | Status | Notes |
| --- | --- | --- |
| Claude Code | supported | local transcript inference plus optional official `tsm claude-statusline` integration |
| Codex | supported | local state / rollout inference |

Notes:

- agent activity is advisory status, not a hard execution guarantee
- Claude can surface richer detached metadata when `tsm claude-statusline` is configured

## Known Scope Boundaries

- `tsm` manages persistent sessions, not panes or splits
- shell shortcuts only exist inside fresh TSM-managed shells unless you add your own global launcher
- some behavior changes require fresh sessions because daemons are long-lived
