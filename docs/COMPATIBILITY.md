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

## Mux Backends (Native Splits)

`tsm mux` orchestrates native terminal splits via backend adapters.

| Terminal | Backend | Splits | Tabs | Sidebar | Platform | Detection |
| -------- | ------- | ------ | ---- | ------- | -------- | --------- |
| cmux | cmux | yes | yes | yes | macOS | `CMUX_SOCKET_PATH` |
| kitty | kitty | yes | yes | no | macOS, Linux | `KITTY_PID` |
| Ghostty | ghostty | yes | yes | no | macOS only | `GHOSTTY_RESOURCES_DIR` |
| WezTerm | wezterm | yes | yes | no | macOS, Linux | `WEZTERM_UNIX_SOCKET` |
| Alacritty | none | no | no | no | - | - |
| Terminal.app | none | no | no | no | - | - |
| iTerm2 | none | no | no | no | - | - |

Notes:

- kitty requires `allow_remote_control yes` and `enabled_layouts splits,tall,stack` in kitty.conf (`tsm mux setup kitty`)
- Ghostty requires 1.3.0+ and uses the AppleScript API (macOS only, preview feature)
- WezTerm works out of the box — no config changes needed
- cmux sidebar sync pushes session and agent (claude/codex) state
- terminals without a backend can still use `tsm` for session management — splits are not required

## Known Scope Boundaries

- shell shortcuts only exist inside fresh TSM-managed shells unless you add your own global launcher
- some behavior changes require fresh sessions because daemons are long-lived
