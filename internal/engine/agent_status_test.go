package engine

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
			id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode
		) VALUES (?, ?, 1, 1710849600, 'cli', 'openai', ?, 'Resolve known issues from docs', 'workspace-write', 'never')
	`, "thread-1", rollout, cwd); err != nil {
		t.Fatal(err)
	}

	status := lookupCodexStatus(cwd)
	if status.Kind != "codex" || status.State != "working" || !strings.Contains(status.Summary, "make test") {
		t.Fatalf("status = %+v", status)
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
