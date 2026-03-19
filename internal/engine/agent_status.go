package engine

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
)

type agentStatus struct {
	Kind          string
	State         string
	Summary       string
	UpdatedAt     int64
	Model         string
	Version       string
	LastPrompt    string
	Plan          string
	InputTokens   int64
	OutputTokens  int64
	CachedTokens  int64
	TotalTokens   int64
	ContextWindow int64
}

var (
	agentUserHomeDir  = os.UserHomeDir
	lookupAgentStatus = resolveAgentStatus
)

func resolveAgentStatus(kind, cwd string) agentStatus {
	switch kind {
	case "codex":
		return lookupCodexStatus(cwd)
	case "claude":
		return lookupClaudeStatus(cwd)
	default:
		return agentStatus{}
	}
}

func lookupCodexStatus(cwd string) agentStatus {
	home, err := agentUserHomeDir()
	if err != nil {
		return agentStatus{}
	}

	dbPath, err := latestMatchingPath(filepath.Join(home, ".codex"), "state_*.sqlite")
	if err != nil {
		return agentStatus{}
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return agentStatus{}
	}
	defer db.Close()

	var title, rolloutPath, modelProvider, cliVersion, source string
	var updatedAt, tokensUsed int64
	err = db.QueryRow(
		`SELECT title, updated_at, rollout_path, tokens_used, model_provider, cli_version, source
		   FROM threads
		  WHERE archived = 0 AND cwd = ?
		  ORDER BY updated_at DESC
		  LIMIT 1`,
		cwd,
	).Scan(&title, &updatedAt, &rolloutPath, &tokensUsed, &modelProvider, &cliVersion, &source)
	if err != nil {
		return agentStatus{}
	}

	status := codexStatusFromRollout(rolloutPath, title, updatedAt)
	status.Model = modelProvider
	status.Version = cliVersion
	status.Plan = source
	if status.LastPrompt == "" {
		status.LastPrompt = title
	}
	if status.TotalTokens == 0 {
		status.TotalTokens = tokensUsed
	}
	if status.Kind == "" {
		status = agentStatus{
			Kind:        "codex",
			State:       "recent",
			Summary:     title,
			UpdatedAt:   updatedAt,
			Model:       modelProvider,
			Version:     cliVersion,
			LastPrompt:  title,
			Plan:        source,
			TotalTokens: tokensUsed,
		}
	}
	return status
}

func lookupClaudeStatus(cwd string) agentStatus {
	home, err := agentUserHomeDir()
	if err != nil {
		return agentStatus{}
	}

	projectDir := filepath.Join(home, ".claude", "projects", sanitizeClaudeProjectPath(cwd))
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return agentStatus{}
	}

	var newest string
	var newestMod time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestMod) {
			newest = filepath.Join(projectDir, entry.Name())
			newestMod = info.ModTime()
		}
	}
	if newest == "" {
		return agentStatus{}
	}

	return claudeStatusFromSession(newest, cwd, newestMod.Unix())
}

func latestMatchingPath(dir, pattern string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}
	slices.Sort(matches)
	return matches[len(matches)-1], nil
}

func sanitizeClaudeProjectPath(cwd string) string {
	var b strings.Builder
	for _, r := range cwd {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func codexStatusFromRollout(path, fallback string, fallbackUpdated int64) agentStatus {
	lines := tailJSONLLines(path, 128<<10)
	if len(lines) == 0 {
		return agentStatus{
			Kind:      "codex",
			State:     "recent",
			Summary:   fallback,
			UpdatedAt: fallbackUpdated,
		}
	}

	status := agentStatus{
		Kind:      "codex",
		State:     "recent",
		Summary:   fallback,
		UpdatedAt: fallbackUpdated,
	}

	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil {
			continue
		}

		ts := parseUnixTimestamp(entry.Timestamp, fallbackUpdated)
		switch entry.Type {
		case "response_item":
			var payload struct {
				Type      string `json:"type"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}
			if json.Unmarshal(entry.Payload, &payload) != nil {
				continue
			}
			switch payload.Type {
			case "function_call":
				status.State = "working"
				status.Summary = summarizeToolInvocation(payload.Name, payload.Arguments)
				status.UpdatedAt = ts
				return status
			case "reasoning":
				status.State = "thinking"
				status.UpdatedAt = ts
				return status
			}
		case "event_msg":
			var payload struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						InputTokens       int64 `json:"input_tokens"`
						CachedInputTokens int64 `json:"cached_input_tokens"`
						OutputTokens      int64 `json:"output_tokens"`
						TotalTokens       int64 `json:"total_tokens"`
					} `json:"total_token_usage"`
					ModelContextWindow int64 `json:"model_context_window"`
				} `json:"info"`
				RateLimits struct {
					PlanType string `json:"plan_type"`
				} `json:"rate_limits"`
			}
			if json.Unmarshal(entry.Payload, &payload) != nil {
				continue
			}
			if payload.Type == "token_count" {
				status.State = "active"
				status.UpdatedAt = ts
				status.InputTokens = payload.Info.TotalTokenUsage.InputTokens
				status.CachedTokens = payload.Info.TotalTokenUsage.CachedInputTokens
				status.OutputTokens = payload.Info.TotalTokenUsage.OutputTokens
				status.TotalTokens = payload.Info.TotalTokenUsage.TotalTokens
				status.ContextWindow = payload.Info.ModelContextWindow
				if payload.RateLimits.PlanType != "" {
					status.Plan = payload.RateLimits.PlanType
				}
			}
		case "session_meta":
			var payload struct {
				Cwd        string `json:"cwd"`
				CLIversion string `json:"cli_version"`
			}
			if json.Unmarshal(entry.Payload, &payload) == nil && status.Version == "" {
				status.Version = payload.CLIversion
			}
		}
	}

	return status
}

func claudeStatusFromSession(path, fallback string, fallbackUpdated int64) agentStatus {
	lines := tailJSONLLines(path, 128<<10)
	if len(lines) == 0 {
		return agentStatus{
			Kind:      "claude",
			State:     "recent",
			Summary:   filepath.Base(fallback),
			UpdatedAt: fallbackUpdated,
		}
	}

	status := agentStatus{
		Kind:      "claude",
		State:     "recent",
		Summary:   filepath.Base(fallback),
		UpdatedAt: fallbackUpdated,
	}
	lastPrompt := ""

	for i := len(lines) - 1; i >= 0; i-- {
		var entry struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Message   json.RawMessage `json:"message"`
			Data      json.RawMessage `json:"data"`
			IsMeta    bool            `json:"isMeta"`
			Version   string          `json:"version"`
		}
		if json.Unmarshal([]byte(lines[i]), &entry) != nil {
			continue
		}

		ts := parseUnixTimestamp(entry.Timestamp, fallbackUpdated)
		switch entry.Type {
		case "assistant":
			if status.Model == "" && entry.Version != "" {
				status.Version = entry.Version
			}
			state, summary, model, input, output, cached := summarizeClaudeAssistant(entry.Message)
			if status.Kind == "" || status.State == "recent" {
				status.Kind = "claude"
				status.State = state
				status.Summary = summary
				status.UpdatedAt = ts
				status.Model = model
				if status.Version == "" {
					status.Version = entry.Version
				}
				status.InputTokens = input
				status.OutputTokens = output
				status.CachedTokens = cached
				status.TotalTokens = input + output + cached
			}
		case "progress":
			var data struct {
				Type      string `json:"type"`
				HookName  string `json:"hookName"`
				HookEvent string `json:"hookEvent"`
			}
			if json.Unmarshal(entry.Data, &data) != nil {
				continue
			}
			if data.Type == "hook_progress" && (status.Kind == "" || status.State == "recent") {
				status.Kind = "claude"
				status.State = "working"
				status.Summary = summarizeClaudeHook(data.HookName, data.HookEvent)
				status.UpdatedAt = ts
			}
		case "user":
			if entry.IsMeta {
				continue
			}
			summary := summarizeClaudeUser(entry.Message)
			if summary != "" && lastPrompt == "" {
				lastPrompt = summary
			}
			if summary != "" && (status.Kind == "" || status.State == "recent") {
				status.Kind = "claude"
				status.State = "waiting"
				status.Summary = summary
				status.UpdatedAt = ts
				status.LastPrompt = summary
				if status.Version == "" {
					status.Version = entry.Version
				}
			}
		}
		if status.Kind != "" && lastPrompt != "" {
			break
		}
	}
	if status.Kind == "" {
		return status
	}
	if status.LastPrompt == "" {
		status.LastPrompt = lastPrompt
	}

	return status
}

func summarizeToolInvocation(name, rawArgs string) string {
	args := map[string]any{}
	if rawArgs != "" {
		_ = json.Unmarshal([]byte(rawArgs), &args)
	}

	switch {
	case stringArg(args, "cmd") != "":
		return "exec: " + oneLine(stringArg(args, "cmd"))
	case stringArg(args, "file_path") != "":
		return strings.ToLower(name) + ": " + filepath.Base(stringArg(args, "file_path"))
	case stringArg(args, "path") != "":
		return strings.ToLower(name) + ": " + filepath.Base(stringArg(args, "path"))
	case stringArg(args, "q") != "":
		return strings.ToLower(name) + ": " + oneLine(stringArg(args, "q"))
	case name != "":
		return strings.ToLower(name)
	default:
		return "active"
	}
}

func summarizeClaudeAssistant(raw json.RawMessage) (string, string, string, int64, int64, int64) {
	var msg struct {
		Content    []json.RawMessage `json:"content"`
		StopReason string            `json:"stop_reason"`
		Model      string            `json:"model"`
		Usage      struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return "", "", "", 0, 0, 0
	}
	cached := msg.Usage.CacheReadInputTokens + msg.Usage.CacheCreationInputTokens
	for _, item := range msg.Content {
		var block struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Text  string          `json:"text"`
			Input json.RawMessage `json:"input"`
		}
		if json.Unmarshal(item, &block) != nil {
			continue
		}
		switch block.Type {
		case "tool_use":
			return "working", summarizeToolInvocation(block.Name, string(block.Input)), msg.Model, msg.Usage.InputTokens, msg.Usage.OutputTokens, cached
		case "text":
			if text := oneLine(block.Text); text != "" {
				switch msg.StopReason {
				case "end_turn":
					return "done", text, msg.Model, msg.Usage.InputTokens, msg.Usage.OutputTokens, cached
				case "tool_use":
					return "working", text, msg.Model, msg.Usage.InputTokens, msg.Usage.OutputTokens, cached
				default:
					return "active", text, msg.Model, msg.Usage.InputTokens, msg.Usage.OutputTokens, cached
				}
			}
		}
	}
	return "", "", msg.Model, msg.Usage.InputTokens, msg.Usage.OutputTokens, cached
}

func summarizeClaudeUser(raw json.RawMessage) string {
	var msg struct {
		Content any `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return ""
	}

	switch content := msg.Content.(type) {
	case string:
		if isInternalClaudeMessage(content) {
			return ""
		}
		return oneLine(content)
	case []any:
		for _, item := range content {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := obj["text"].(string); ok && text != "" {
				if isInternalClaudeMessage(text) {
					continue
				}
				return oneLine(text)
			}
		}
	}
	return ""
}

func isInternalClaudeMessage(s string) bool {
	return strings.Contains(s, "<local-command-") || strings.Contains(s, "<persisted-output>") || strings.Contains(s, "<command-name>")
}

func summarizeClaudeHook(name, event string) string {
	if name != "" {
		return strings.ToLower(name)
	}
	if event != "" {
		return strings.ToLower(event)
	}
	return "active"
}

func stringArg(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func oneLine(s string) string {
	s = cleanSummaryText(s)
	if s == "" {
		return ""
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	out := strings.Join(fields, " ")
	if len(out) > 56 {
		out = out[:53] + "..."
	}
	return out
}

func cleanSummaryText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	replacements := []string{
		"```", " ",
		"`", "",
		"**", "",
		"__", "",
		"*", "",
		"#", "",
		"•", "-",
		"–", "-",
		"—", "-",
	}
	r := strings.NewReplacer(replacements...)
	s = r.Replace(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.TrimSpace(strings.ToValidUTF8(s, ""))
}

func DisplayAgentModel(kind, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	switch kind {
	case "claude":
		model = strings.TrimPrefix(model, "claude-")
		model = strings.ReplaceAll(model, "-", " ")
		return titleWords(model)
	case "codex":
		model = strings.ReplaceAll(model, "-", " ")
		return titleWords(model)
	default:
		return model
	}
}

func titleWords(s string) string {
	fields := strings.Fields(s)
	for i, field := range fields {
		fields[i] = titleWord(field)
	}
	return strings.Join(fields, " ")
}

func titleWord(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + strings.ToLower(s[size:])
}

func parseUnixTimestamp(raw string, fallback int64) int64 {
	if raw == "" {
		return fallback
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return fallback
	}
	return ts.Unix()
}

func tailJSONLLines(path string, maxBytes int64) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	if size <= 0 {
		return nil
	}
	if size > maxBytes {
		_, _ = f.Seek(size-maxBytes, 0)
	}

	data, err := ioReadAll(f)
	if err != nil {
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	const maxLine = 1024 * 1024
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLine)

	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if scanner.Err() != nil && !errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return nil
	}
	return lines
}

func ioReadAll(f *os.File) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(f)
	return buf.Bytes(), err
}

func FormatRelativeTime(unixTS int64) string {
	if unixTS <= 0 {
		return ""
	}
	secs := int(time.Since(time.Unix(unixTS, 0)).Seconds())
	if secs < 0 {
		secs = 0
	}
	return FormatUptime(secs)
}

func agentFallbackSummary(cwd string) string {
	if base := filepath.Base(cwd); base != "" && base != "." && base != string(filepath.Separator) {
		return base
	}
	return cwd
}

func agentStatusSummary(status agentStatus, cwd string) string {
	if status.Summary != "" {
		return status.Summary
	}
	return agentFallbackSummary(cwd)
}
