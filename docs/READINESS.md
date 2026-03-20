# TSM Readiness Tracker

This document tracks the highest-value work needed to make `tsm` feel ready for broader public use.

It is intentionally biased toward:

- reliability
- install and release quality
- supportability
- reducing confusing behavior

## Status Model

- `P0`: must address before recommending `tsm` broadly
- `P1`: strong next priority after P0
- `P2`: important polish, but not blocking early adoption

## P0

### ~~1. Reproducible Build, Test, and Release Flow~~ Done

- [x] make the default contributor path obvious and reliable
- [x] ensure local build, CI, release, and Homebrew use the same supported path
- [x] remove any hidden env assumptions from routine commands
- [x] document the exact supported source-build workflow clearly

What shipped:

- `make setup` as single first-time entry point (`check-bootstrap-deps` → `setup-ghostty-vt`)
- Ghostty pinned to a specific commit (`GHOSTTY_REVISION` in Makefile)
- Go pinned to `1.25` in CI/release workflows (matches `mise.toml`)
- Zig version enforced in `check-bootstrap-deps` (reads from `scripts/install_zig.sh`)
- Zig install extracted to `scripts/install_zig.sh` (single source of truth, used by CI and release)
- `CONTRIBUTING.md` with prerequisites, setup flow, and pinned versions table
- README updated with explicit build prerequisites

### ~~2. End-to-End Integration Tests~~ Done

- [x] add attach/detach smoke tests
- [x] add rename and local-switch tests
- [x] add kill/detach cleanup tests
- [x] add resize and restore-order tests
- [x] add at least one full-screen app smoke test path

What shipped:

- daemon/session integration coverage in [internal/session/integration_test.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/integration_test.go)
- CLI/client integration coverage in [cli_integration_test.go](/Users/adibhanna/Developer/opensource/tsm/cli_integration_test.go)
- real attach → detach → reattach coverage through the client path
- local switch coverage
- terminal cleanup coverage after kill
- TUI and palette attach handoff coverage
- daemon-side rename, resize, multi-client detach, and full-screen restore smoke coverage

### ~~3. Better Diagnostics and Recovery Tooling~~ Done

Problem:

- when something goes wrong, debugging still depends too much on manual inspection
- stale sockets, daemon mismatches, and runtime-linking issues are hard for users to reason about

Why it matters:

- supportability determines whether the tool is usable by anyone except the author

Track:

- [x] add `tsm doctor`
- [x] add `tsm debug session <name>`
- [x] improve runtime and daemon error messages
- [x] add stale-session and stale-socket repair guidance

What shipped:

- `tsm doctor` for runtime/backend/config/socket diagnostics
- `tsm doctor clean-stale` for stale socket cleanup
- `tsm debug session <name>` for per-session inspection, including state, pid, clients, cwd/cmd, exit metadata, and preview output
- attach/kill/detach/rename failures now point users at `tsm ls`, `tsm doctor`, `tsm doctor clean-stale`, or `tsm debug session <name>` instead of exposing raw socket errors alone
- README and CLI docs now include stale-socket cleanup and per-session debug guidance

Relevant areas:

- [main.go](/Users/adibhanna/Developer/opensource/tsm/main.go)
- [internal/session/session.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/session.go)
- [internal/session/socket.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/socket.go)

## P1

### ~~4. Harden Config and Keybinding Validation~~ Done

Problem:

- the config surface is useful, but invalid or conflicting bindings can still create confusing behavior
- shell shortcut behavior and TUI keymaps are easy to misconfigure

Why it matters:

- user customization should not silently degrade the product

Track:

- [x] validate config values more strictly
- [x] detect key conflicts and ambiguous bindings
- [x] produce clearer startup/config parse errors
- [x] reject invalid shell shortcut formats with actionable messages

What shipped:

- config load now rejects unknown TUI mode/keymap values
- shell shortcut config now rejects unsupported formats and duplicate shell shortcuts
- TUI keymap overrides now reject conflicting bindings instead of silently shadowing actions
- config validation errors now include the config path plus field-specific failure details

Relevant files:

- [internal/appconfig/config.go](/Users/adibhanna/Developer/opensource/tsm/internal/appconfig/config.go)
- [internal/tui/bindings.go](/Users/adibhanna/Developer/opensource/tsm/internal/tui/bindings.go)

### 5. Simplify the Shortcut Story

Problem:

- there are currently multiple shortcut layers:
  - TSM-managed shell shortcuts
  - optional user-defined global shell shortcuts
  - app-local shortcuts inside tools like Neovim
- users can easily expect one shortcut model to work everywhere

Why it matters:

- input behavior needs to be easy to explain, not just technically possible

Track:

- [ ] document a single recommended workflow clearly
- [ ] separate “inside TSM shell” from “global launcher” in docs and config guidance
- [ ] evaluate an out-of-band control path for opening the picker from anywhere

Relevant areas:

- [internal/session/daemon.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/daemon.go)
- [README.md](/Users/adibhanna/Developer/opensource/tsm/README.md)

### 6. Improve Agent Status Accuracy

Problem:

- Codex and Claude activity is useful, but currently inferred from local state
- status can be stale, noisy, or misleading if the latest local records are low-value

Why it matters:

- agent-heavy workflows are a major part of `tsm`’s value proposition now
- inaccurate agent status damages trust quickly

Track:

- [ ] keep state labels human and trustworthy
- [ ] filter internal or low-signal events more aggressively
- [ ] surface staleness clearly
- [ ] prefer official Claude integration surfaces where possible
- [ ] keep compact palette output terse and high-signal

Relevant files:

- [internal/engine/agent_status.go](/Users/adibhanna/Developer/opensource/tsm/internal/engine/agent_status.go)
- [internal/tui/model_view.go](/Users/adibhanna/Developer/opensource/tsm/internal/tui/model_view.go)

### 7. Smooth Session Lifecycle Edge Cases

Problem:

- daemon upgrades, shell integration changes, detach/kill cleanup, and prompt restoration still have edge cases

Why it matters:

- these are the issues that make a session manager feel “haunted”

Track:

- [ ] improve behavior when old daemons are still running after a rebuild
- [ ] reduce shell-specific sharp edges
- [ ] audit detach/kill/quit screen cleanup paths
- [ ] improve crash cleanup and stale resource handling

Relevant areas:

- [internal/session/daemon.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/daemon.go)
- [internal/session/client.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/client.go)
- [internal/session/switch.go](/Users/adibhanna/Developer/opensource/tsm/internal/session/switch.go)

## P2

### 8. Release Maturity

Problem:

- the project has release automation, but broad adoption needs consistent release quality and support expectations

Why it matters:

- install confidence is part of product confidence

Track:

- [ ] publish tagged releases regularly
- [ ] verify Homebrew install from published release artifacts
- [ ] publish release notes with notable behavior changes
- [ ] document OS, shell, and terminal compatibility

Relevant files:

- [.github/workflows/release.yaml](/Users/adibhanna/Developer/opensource/tsm/.github/workflows/release.yaml)
- [Formula/tsm.rb](/Users/adibhanna/Developer/opensource/tsm/Formula/tsm.rb)

### 9. Reintroduce a Known Limitations Document

Problem:

- users need a clear contract for what `tsm` does and does not do

Why it matters:

- good limitations documentation prevents incorrect expectations and bug reports that are really product-boundary issues

Track:

- [ ] document no-pane/no-split scope clearly
- [ ] document shell shortcut scope clearly
- [ ] document agent activity as advisory status
- [ ] document when fresh sessions are needed after integration changes

Suggested location:

- `docs/KNOWN_LIMITATIONS.md`

## Suggested Execution Order

If the goal is “ready for people to use,” the most leveraged order is:

1. reproducible build/test/release flow
2. end-to-end integration tests
3. diagnostics and recovery tooling
4. config and keybinding hardening
5. agent status accuracy

## Current Summary

Today, `tsm` looks ready for:

- power users
- early adopters
- AI-agent-heavy terminal workflows

It does not yet fully look ready for:

- casual users
- outside contributors with zero setup context
- people who expect polished failure recovery by default

That is a good place to be. The project already has a strong core. The remaining work is mostly about reliability, clarity, and supportability.
