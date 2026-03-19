package engine

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo holds per-session process data fetched asynchronously.
type ProcessInfo struct {
	Memory         uint64
	Uptime         int // seconds
	AgentKind      string
	AgentState     string
	AgentSummary   string
	AgentUpdated   int64
	AgentModel     string
	AgentVersion   string
	AgentPrompt    string
	AgentPlan      string
	AgentApproval  string
	AgentSandbox   string
	AgentBranch    string
	AgentGitSHA    string
	AgentGitOrigin string
	AgentName      string
	AgentRole      string
	AgentMemory    string
	AgentSessionID string
	AgentSubagent  bool
	AgentInput     int64
	AgentOutput    int64
	AgentCached    int64
	AgentTotal     int64
	AgentContext   int64
}

// FetchProcessInfo returns a map of session name → ProcessInfo.
// Uses a single `ps` call to read all processes, then walks the tree in memory.
func FetchProcessInfo(sessions []Session) map[string]ProcessInfo {
	rssMap, childMap, etimeMap, commMap := readProcessTable()

	result := make(map[string]ProcessInfo, len(sessions))
	agentCache := make(map[string]agentStatus)
	for _, s := range sessions {
		pid, err := strconv.Atoi(s.PID)
		if err != nil {
			continue
		}
		info := ProcessInfo{
			Memory: sumTreeRSS(pid, rssMap, childMap),
			Uptime: etimeMap[pid],
		}
		if kind := detectAgentKind(pid, childMap, commMap); kind != "" {
			cacheKey := kind + "\x00" + s.StartedIn
			status, ok := agentCache[cacheKey]
			if !ok {
				status = lookupAgentStatus(kind, s.StartedIn)
				agentCache[cacheKey] = status
			}
			info.AgentKind = status.Kind
			info.AgentState = status.State
			info.AgentSummary = agentStatusSummary(status, s.StartedIn)
			info.AgentUpdated = status.UpdatedAt
			info.AgentModel = status.Model
			info.AgentVersion = status.Version
			info.AgentPrompt = status.LastPrompt
			info.AgentPlan = status.Plan
			info.AgentApproval = status.ApprovalMode
			info.AgentSandbox = status.SandboxPolicy
			info.AgentBranch = status.GitBranch
			info.AgentGitSHA = status.GitSHA
			info.AgentGitOrigin = status.GitOrigin
			info.AgentName = status.AgentName
			info.AgentRole = status.AgentRole
			info.AgentMemory = status.MemoryMode
			info.AgentSessionID = status.SessionID
			info.AgentSubagent = status.IsSubagent
			info.AgentInput = status.InputTokens
			info.AgentOutput = status.OutputTokens
			info.AgentCached = status.CachedTokens
			info.AgentTotal = status.TotalTokens
			info.AgentContext = status.ContextWindow
		}
		result[s.Name] = info
	}
	return result
}

// readProcessTable parses `ps -eo pid,ppid,rss,etime` into RSS, children, and etime maps.
// RSS values from ps are in KiB. Etime is parsed into seconds.
func readProcessTable() (rss map[int]uint64, children map[int][]int, etime map[int]int, comm map[int]string) {
	rss = make(map[int]uint64)
	children = make(map[int][]int)
	etime = make(map[int]int)
	comm = make(map[int]string)

	out, err := runCombinedOutput("ps", "-eo", "pid,ppid,rss,etime,comm")
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		kib, err3 := strconv.ParseUint(fields[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		rss[pid] = kib * 1024 // KiB → bytes
		children[ppid] = append(children[ppid], pid)
		etime[pid] = parseEtime(fields[3])
		comm[pid] = strings.Join(fields[4:], " ")
	}
	return
}

// parseEtime parses ps etime format into seconds.
// Formats: "ss", "mm:ss", "hh:mm:ss", "d-hh:mm:ss"
func parseEtime(s string) int {
	days := 0
	if i := strings.Index(s, "-"); i >= 0 {
		days, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	total := 0
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		total = total*60 + n
	}
	return total + days*86400
}

// FormatUptime formats seconds as a compact human-readable duration.
func FormatUptime(secs int) string {
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh", secs/3600)
	default:
		return fmt.Sprintf("%dd", secs/86400)
	}
}

// sumTreeRSS sums RSS for a process and all its descendants.
func sumTreeRSS(pid int, rss map[int]uint64, children map[int][]int) uint64 {
	total := rss[pid]
	for _, child := range children[pid] {
		total += sumTreeRSS(child, rss, children)
	}
	return total
}

func detectAgentKind(pid int, children map[int][]int, comm map[int]string) string {
	var walk func(int) string
	walk = func(id int) string {
		name := strings.ToLower(filepath.Base(comm[id]))
		switch {
		case strings.Contains(name, "codex"):
			return "codex"
		case strings.Contains(name, "claude"):
			return "claude"
		}
		for _, child := range children[id] {
			if kind := walk(child); kind != "" {
				return kind
			}
		}
		return ""
	}
	return walk(pid)
}

// FormatBytes formats bytes as a human-readable string (e.g., "12M", "1.2G").
func FormatBytes(b uint64) string {
	switch {
	case b >= 1<<30:
		v := float64(b) / float64(1<<30)
		if v >= 10 {
			return fmt.Sprintf("%.0fG", v)
		}
		return fmt.Sprintf("%.1fG", v)
	case b >= 1<<20:
		return fmt.Sprintf("%dM", b>>20)
	case b >= 1<<10:
		return fmt.Sprintf("%dK", b>>10)
	default:
		return fmt.Sprintf("%dB", b)
	}
}
