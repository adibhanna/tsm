package engine

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestFetchProcessInfoIncludesAgentStatus(t *testing.T) {
	origDeps := deps
	defer func() { deps = origDeps }()
	origLookup := lookupAgentStatus
	defer func() { lookupAgentStatus = origLookup }()

	deps.command = fakeCommand(
		"  PID  PPID   RSS ELAPSED COMM\n" +
			"100 1 100 00:10 /bin/zsh\n" +
			"101 100 200 00:09 /Applications/Codex.app/Contents/Resources/codex\n",
	)

	lookupAgentStatus = func(kind, cwd string) agentStatus {
		if kind != "codex" || cwd != "/tmp/work" {
			t.Fatalf("lookupAgentStatus(%q, %q)", kind, cwd)
		}
		return agentStatus{
			Kind:      "codex",
			State:     "working",
			Summary:   "exec: make test",
			UpdatedAt: 123,
		}
	}

	got := FetchProcessInfo([]Session{{Name: "work", PID: "100", StartedIn: "/tmp/work"}})
	info := got["work"]
	if info.AgentKind != "codex" || info.AgentState != "working" || info.AgentSummary != "exec: make test" || info.AgentUpdated != 123 {
		t.Fatalf("agent info = %+v", info)
	}
}

func TestLookupCodexStatusFromLocalState(t *testing.T) {
	home := t.TempDir()
	origHome := agentUserHomeDir
	defer func() { agentUserHomeDir = origHome }()
	agentUserHomeDir = func() (string, error) { return home, nil }

	cwd := "/tmp/demo"
	rollout := filepath.Join(home, ".codex", "sessions", "2026", "03", "19", "rollout-demo.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte(
		"{\"timestamp\":\"2026-03-19T12:00:00Z\",\"type\":\"response_item\",\"payload\":{\"type\":\"function_call\",\"name\":\"exec_command\",\"arguments\":\"{\\\"cmd\\\":\\\"make test\\\"}\"}}\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	dbDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(dbDir, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			rollout_path TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			source TEXT NOT NULL,
			model_provider TEXT NOT NULL,
			cwd TEXT NOT NULL,
			title TEXT NOT NULL,
			sandbox_policy TEXT NOT NULL,
			approval_mode TEXT NOT NULL,
			tokens_used INTEGER NOT NULL DEFAULT 0,
			has_user_event INTEGER NOT NULL DEFAULT 0,
			archived INTEGER NOT NULL DEFAULT 0,
			archived_at INTEGER,
			git_sha TEXT,
			git_branch TEXT,
			git_origin_url TEXT,
			cli_version TEXT NOT NULL DEFAULT '',
			first_user_message TEXT NOT NULL DEFAULT '',
			agent_nickname TEXT,
			agent_role TEXT,
			memory_mode TEXT NOT NULL DEFAULT 'enabled'
		);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO threads (
			id, rollout_path, created_at, updated_at, source, model_provider, cwd, title,
			sandbox_policy, approval_mode, git_sha, git_branch, first_user_message,
			agent_nickname, agent_role, memory_mode
		) VALUES (?, ?, 1, 1710849600, 'cli', 'openai', ?, 'Resolve known issues from docs',
		          'workspace-write', 'never', 'abcdef1234567890', 'main',
		          'Please fix the docs', 'worker-1', 'implementer', 'enabled')
	`, "thread-1", rollout, cwd); err != nil {
		t.Fatal(err)
	}

	status := lookupCodexStatus(cwd)
	if status.Kind != "codex" || status.State != "working" || !strings.Contains(status.Summary, "make test") {
		t.Fatalf("status = %+v", status)
	}
	if status.ApprovalMode != "never" || status.SandboxPolicy != "workspace-write" {
		t.Fatalf("expected runtime metadata, got %+v", status)
	}
	if status.GitBranch != "main" || status.GitSHA != "abcdef1234567890" {
		t.Fatalf("expected git metadata, got %+v", status)
	}
	if status.LastPrompt != "Please fix the docs" || status.AgentName != "worker-1" || status.AgentRole != "implementer" || status.MemoryMode != "enabled" {
		t.Fatalf("expected prompt/agent metadata, got %+v", status)
	}
}

func TestLookupClaudeStatusFromSessionJSONL(t *testing.T) {
	home := t.TempDir()
	origHome := agentUserHomeDir
	defer func() { agentUserHomeDir = origHome }()
	agentUserHomeDir = func() (string, error) { return home, nil }

	cwd := "/Users/test/work"
	projectDir := filepath.Join(home, ".claude", "projects", sanitizeClaudeProjectPath(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(projectDir, "session.jsonl")
	content := strings.Join([]string{
		`{"type":"user","timestamp":"2026-03-19T12:00:00Z","message":{"content":"please update the docs"}}`,
		`{"type":"assistant","timestamp":"2026-03-19T12:00:03Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/Users/test/work/README.md"}}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(sessionPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	status := lookupClaudeStatus(cwd)
	if status.Kind != "claude" || status.State != "working" || !strings.Contains(status.Summary, "README.md") {
		t.Fatalf("status = %+v", status)
	}
	if status.GitBranch != "" {
		t.Fatalf("expected empty branch when not present, got %+v", status)
	}
}

func TestLookupClaudeStatusCapturesBranchAndSubagent(t *testing.T) {
	home := t.TempDir()
	origHome := agentUserHomeDir
	defer func() { agentUserHomeDir = origHome }()
	agentUserHomeDir = func() (string, error) { return home, nil }

	cwd := "/Users/test/work"
	projectDir := filepath.Join(home, ".claude", "projects", sanitizeClaudeProjectPath(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(projectDir, "session.jsonl")
	content := strings.Join([]string{
		`{"type":"user","timestamp":"2026-03-19T12:00:00Z","sessionId":"sess-1","gitBranch":"main","slug":"keen-dawn","isSidechain":true,"message":{"content":"please update the docs"}}`,
		`{"type":"assistant","timestamp":"2026-03-19T12:00:03Z","sessionId":"sess-1","gitBranch":"main","slug":"keen-dawn","isSidechain":true,"message":{"content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(sessionPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	status := lookupClaudeStatus(cwd)
	if status.GitBranch != "main" || status.SessionID != "sess-1" || status.AgentName != "keen-dawn" || !status.IsSubagent {
		t.Fatalf("status = %+v", status)
	}
	if status.AgentRole != "subagent" {
		t.Fatalf("expected subagent role, got %+v", status)
	}
}

func TestLookupClaudeStatusSkipsLowSignalNewestSession(t *testing.T) {
	home := t.TempDir()
	origHome := agentUserHomeDir
	defer func() { agentUserHomeDir = origHome }()
	agentUserHomeDir = func() (string, error) { return home, nil }

	cwd := "/Users/test/work"
	projectDir := filepath.Join(home, ".claude", "projects", sanitizeClaudeProjectPath(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	older := filepath.Join(projectDir, "older.jsonl")
	olderContent := strings.Join([]string{
		`{"type":"assistant","timestamp":"2026-03-19T12:00:03Z","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/Users/test/work/README.md"}}]}}`,
		`{"type":"last-prompt","lastPrompt":"list all the files","sessionId":"sess-1"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(older, []byte(olderContent), 0o644); err != nil {
		t.Fatal(err)
	}

	newer := filepath.Join(projectDir, "newer.jsonl")
	newerContent := strings.Join([]string{
		`{"type":"user","timestamp":"2026-03-19T12:10:00Z","isMeta":true,"message":{"content":"<local-command-caveat>ignore</local-command-caveat>"}}`,
		`{"type":"user","timestamp":"2026-03-19T12:10:00Z","message":{"content":"<command-name>/clear</command-name>"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(newer, []byte(newerContent), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(older, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, now, now); err != nil {
		t.Fatal(err)
	}

	status := lookupClaudeStatus(cwd)
	if status.Kind != "claude" || status.LastPrompt != "list all the files" {
		t.Fatalf("status = %+v", status)
	}
}

func TestClaudeCompletedReplyIsDoneNotWorking(t *testing.T) {
	status := claudeStatusFromSession(writeTempJSONL(t, []string{
		`{"type":"assistant","timestamp":"2026-03-19T12:00:03Z","message":{"content":[{"type":"text","text":"Here are the files"}],"stop_reason":"end_turn"}}`,
	}), "/tmp/demo", 0)

	if status.State != "done" || !strings.Contains(status.Summary, "Here are the files") {
		t.Fatalf("status = %+v", status)
	}
}

func TestClaudeInternalUserMessagesAreIgnored(t *testing.T) {
	status := claudeStatusFromSession(writeTempJSONL(t, []string{
		`{"type":"user","timestamp":"2026-03-19T12:00:00Z","message":{"content":"<local-command-stdout>Bye!</local-command-stdout>"}}`,
		`{"type":"assistant","timestamp":"2026-03-19T12:00:03Z","message":{"content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn"}}`,
	}), "/tmp/demo", 0)

	if status.State != "done" || status.Summary != "Done." {
		t.Fatalf("status = %+v", status)
	}
}

func TestAgentStatusSummaryDoesNotFallBackToCwdName(t *testing.T) {
	status := agentStatus{Kind: "claude", State: "recent"}
	if got := agentStatusSummary(status, "/Users/test/work/tsm"); got != "" {
		t.Fatalf("agentStatusSummary() = %q, want empty summary", got)
	}
}

func TestDisplayAgentStateMarksOldRecentAsStale(t *testing.T) {
	old := time.Now().Add(-20 * time.Minute).Unix()
	if got := DisplayAgentState("recent", old); got != "stale" {
		t.Fatalf("DisplayAgentState() = %q, want stale", got)
	}
}

func TestDisplayAgentStateKeepsFreshRecentAsIdle(t *testing.T) {
	fresh := time.Now().Add(-30 * time.Second).Unix()
	if got := DisplayAgentState("recent", fresh); got != "idle" {
		t.Fatalf("DisplayAgentState() = %q, want idle", got)
	}
}

func fakeCommand(output string) func(string, ...string) *exec.Cmd {
	return func(string, ...string) *exec.Cmd {
		script := fmt.Sprintf("cat <<'EOF'\n%sEOF\n", output)
		return exec.Command("/bin/sh", "-c", script)
	}
}

func writeTempJSONL(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
