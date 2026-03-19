# Known Issues & Future Work

## Fallback Build Is Not Full-Fidelity

The `noghosttyvt` build works, but it is intentionally limited.

What it can do:

- preserve some terminal modes on reattach
- keep detach/attach functional
- work in environments where `libghostty-vt` is unavailable

What it cannot do reliably:

- restore exact full-screen application contents
- rehydrate Neovim or similar apps to the exact previous screen
- provide the same quality of preview as the Ghostty-backed build

If exact persistence matters, use the default Ghostty-backed build or the bundled release archive.

## Rebuilds Do Not Upgrade Running Sessions

Session daemons are long-lived processes. Rebuilding `tsm` does not replace code in already-running sessions.

Implication:

- old sessions keep old daemon behavior
- old attached clients keep old client behavior

After changing attach/preview/switch logic, test with fresh sessions or kill and recreate the affected session daemons.

## TUI Preview Is Current-Screen Preview, Not Scrollback Browser

The preview pane shows the current terminal screen state, not a full interactive scrollback history browser.

That is deliberate:

- raw PTY scrollback is unsafe to replay directly
- the current tracked screen is what matters for attach fidelity

Future work could add:

- dedicated scrollback browsing
- scrollback snapshots separate from current-screen restore
- richer preview modes for long-running sessions

## Release Automation Still Depends On Native Builds Per Platform

TSM now produces self-contained archives, but those archives are built natively per target platform because of the bundled `libghostty-vt` runtime.

That is the right tradeoff for correctness, but it means release automation is more like platform packaging than pure Go cross-compilation.

## Resolved

These were previously open and are now fixed:

- Ghostty-backed full-screen reattach is the default build path
- colored TUI preview now comes from the tracked terminal state instead of raw scrollback
- selecting another session from inside an attached session now performs a local switch instead of nesting attach
- `attach`, `detach`, and `kill` support current-session behavior through `$TSM_SESSION`
- `kill` accepts multiple session names
- attached-client counting no longer has the probe off-by-one bug
- default `zsh` sessions show the session name in the prompt/title
