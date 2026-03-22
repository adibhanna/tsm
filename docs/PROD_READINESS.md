# Production Readiness Review

Comprehensive code review of `tsm` — ~13k lines of Go across ~50 files.
Reviewed: 2026-03-21

---

## CRITICAL / P0

### 1. Race condition on `switchErr` in `Attach()`

- **File:** `internal/session/client.go:61,117,156`
- **Status:** [x] DONE
- **Issue:** `switchErr` is written in a goroutine (output relay) and read in the main goroutine with no synchronization. Data race.
- **Fix:** Use `atomic.Pointer[SwitchSessionError]` or communicate via channel.

### 2. Path traversal via unsanitized session names

- **File:** `internal/session/config.go:45`, `main.go` (multiple entry points)
- **Status:** [x] DONE
- **Issue:** Session names from user input are never validated for `/`, `..`, null bytes. A name like `../../etc/foo` traverses outside the socket directory. Flows into `SocketPath()`, `CreateTemp()`, `filepath.Join`, and more.
- **Fix:** Add validation function rejecting `/`, `..`, null bytes, empty, and `.`-prefixed names. Apply at all CLI entry points (`cmdAttach`, `cmdNew`, `cmdKill`, `cmdRename`).

### 3. No max payload size in IPC protocol

- **File:** `internal/session/socket.go:47`
- **Status:** [x] DONE
- **Issue:** `ReadMessage` allocates `make([]byte, hdr.Len)` where `hdr.Len` is `uint32` (up to 4GB). A corrupted or malicious message can OOM the daemon.
- **Fix:** Add a `maxPayloadSize` constant (e.g. 16MB) and reject messages exceeding it.

### 4. Test suite mostly unbuildable without Ghostty

- **File:** All packages except `internal/appconfig`
- **Status:** [ ] TODO (tracking only — not a code fix)
- **Issue:** 80%+ of tests can't run without `libghostty-vt`. No `//go:build !cgo` stub exists. Race detector has never exercised core code paths.
- **Fix:** Provide a `//go:build !cgo` terminal backend stub.

---

## HIGH / P1

### 5. Goroutine leak — stdin relay in `Attach()`

- **File:** `internal/session/client.go:125-141`
- **Status:** [x] DONE
- **Issue:** Stdin relay goroutine blocks on `os.Stdin.Read()` forever after `done` is closed. Leaks until next keystroke.
- **Fix:** Close the connection or stdin to unblock, or use poll-based approach.

### 6. Unbounded `logLines` memory growth

- **File:** `internal/tui/model_core.go:383`
- **Status:** [x] DONE
- **Issue:** `addLog()` appends indefinitely. Long TUI sessions leak memory.
- **Fix:** Cap at ~200 entries, discard oldest.

### 7. PTY write errors silently ignored

- **File:** `internal/session/daemon.go:270,302,342`
- **Status:** [x] DONE
- **Issue:** `d.ptmx.Write()` errors discarded. Dead PTY goes unnoticed.
- **Fix:** Check error, trigger shutdown on write failure.

### 8. SendMessage errors ignored in stdin relay

- **File:** `internal/session/client.go:132-135`
- **Status:** [x] DONE
- **Issue:** Broken connection undetected, data silently discarded.
- **Fix:** Check error, return from goroutine on failure.

### 9. Broadcast to non-attached clients

- **File:** `internal/session/daemon.go:383-388`
- **Status:** [x] DONE
- **Issue:** `broadcast()` sends to probe/info connections that haven't sent `TagInit`.
- **Fix:** Filter to `attached == true` clients only.

### 10. Stack overflow in recursive tree walk

- **File:** `internal/engine/process.go:171`
- **Status:** [x] DONE
- **Issue:** `sumTreeRSS` / `detectAgentKind` recurse with no cycle detection. Corrupted `ps` output → crash.
- **Fix:** Add `visited` map or convert to iterative.

### 11. SpawnDaemon returns success on failure

- **File:** `internal/session/daemon.go:200-206`
- **Status:** [x] DONE
- **Issue:** Returns `nil` even if daemon crashed and socket never appeared.
- **Fix:** Return error if socket doesn't appear within polling period.

### 12. Unix socket created without restrictive permissions

- **File:** `internal/session/daemon.go:78`
- **Status:** [x] DONE
- **Issue:** Socket inherits umask. Any local process that can reach it can inject keystrokes or kill sessions.
- **Fix:** `os.Chmod` socket to `0700` after creation, or verify directory permissions are sufficient.

---

## MEDIUM / P2

### 13. Double-close on `TagDetachAll`

- **File:** `internal/session/daemon.go:326,435`
- **Status:** [x] DONE
- **Issue:** `closeAllClients` closes all conns, then deferred cleanup closes calling client's conn again.
- **Fix:** Check if conn is still in map before closing in defer.

### 14. Log scroll keys conflict with palette filter

- **File:** `internal/tui/model_input.go:22`
- **Status:** [x] DONE
- **Issue:** `[`/`]` both scroll logs AND append to filter text when palette is active.
- **Fix:** Return early after `handleLogScroll` if key was consumed.

### 15. Scrollback `TailLines` excessive allocations

- **File:** `internal/session/scrollback.go:70-78`
- **Status:** [x] DONE
- **Issue:** Copies 10MB buffer → string → split. ~30-40MB transient allocs.
- **Fix:** Work on byte buffer directly to find last N newlines.

### 16. SQLite opened without timeout/WAL

- **File:** `internal/engine/agent_status.go:82`
- **Status:** [x] DONE
- **Issue:** Can block indefinitely if another process holds the lock.
- **Fix:** Add `?_journal_mode=WAL&_busy_timeout=1000&mode=ro` to connection string.

### 17. `NormalizeOptions` called on every keypress/render

- **File:** `internal/tui/model_core.go`, `internal/tui/keys.go`
- **Status:** [x] DONE
- **Issue:** Wasteful repeated allocations on hot path.
- **Fix:** Normalize once in constructor and after mutations, cache result.

### 18. `highlightMatch` byte offset bug with Unicode

- **File:** `internal/tui/model_view.go:1060`
- **Status:** [x] DONE
- **Issue:** Uses byte offset from lowercased string to slice original. Multi-byte runes garble display.
- **Fix:** Use `strings.EqualFold` or rune-based indices.

### 19. JSONL partial line after seek

- **File:** `internal/engine/agent_status.go:858`
- **Status:** [x] DONE
- **Issue:** After seeking mid-file, first partial line is parsed and silently fails.
- **Fix:** Skip bytes until first newline after seeking.

### 20. Fragile duplicated field copy (30+ fields)

- **File:** `internal/tui/model_core.go:444-479,513-547`
- **Status:** [x] DONE
- **Issue:** Two near-identical 30+ field copies. New `Session` field requires updating both.
- **Fix:** Extract a merge helper method.

### 21. `suggestSessionName` unbounded loop

- **File:** `main.go:345`
- **Status:** [x] DONE
- **Issue:** `for i := 2; ; i++` with no upper bound. Tiny `maxLen` → infinite loop.
- **Fix:** Add iteration cap (e.g. 10000) and return error.

### 22. Multiple `TagKill` handlers race redundantly

- **File:** `internal/session/daemon.go:314`
- **Status:** [x] DONE
- **Issue:** Multiple clients send `TagKill` simultaneously, each sleeping 500ms and signaling.
- **Fix:** Use `sync.Once` for the kill sequence.

### 23. Flaky integration tests with `time.Sleep`

- **File:** `cli_integration_test.go:205,229,271,349,465`
- **Status:** [ ] TODO
- **Issue:** Bare `time.Sleep(500ms)` waits instead of polling/readiness checks.
- **Fix:** Replace with polling or condition-based waits.

### 24. Dead code — unreachable type switch cases

- **File:** `main.go:1096`
- **Status:** [x] DONE
- **Issue:** `json.Unmarshal` into `any` never produces `int`/`int64`. Those cases are dead code.
- **Fix:** Remove `int` and `int64` cases.

---

## LOW / P3

### 25. Detach key `0x1C` false positive on binary data

- **File:** `internal/session/client.go:25`

### 26. Stale socket cleanup only handles `*net.OpError`

- **File:** `internal/session/session.go:91`

### 27. `MaxSessionNameLen` can return negative

- **File:** `internal/session/config.go:50`

### 28. `displayAgentKind` misuses `DisplayAgentModel`

- **File:** `internal/tui/model_view.go:784`

### 29. `removeFocusSession` stale `Current` when `Current == Previous`

- **File:** `focus.go:71`

### 30. `os.Chdir` in tests not parallel-safe

- **File:** `main_test.go:20`

### 31. `TestCLITUIAttach` permanently skipped

- **File:** `cli_integration_test.go:433`
