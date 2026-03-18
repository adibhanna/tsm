package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// ProcessInfo holds per-session process data fetched asynchronously.
type ProcessInfo struct {
	Memory uint64
	Uptime int // seconds
}

// FetchProcessInfo returns a map of session name → ProcessInfo.
// Uses a single `ps` call to read all processes, then walks the tree in memory.
func FetchProcessInfo(sessions []Session) map[string]ProcessInfo {
	rssMap, childMap, etimeMap := readProcessTable()

	result := make(map[string]ProcessInfo, len(sessions))
	for _, s := range sessions {
		pid, err := strconv.Atoi(s.PID)
		if err != nil {
			continue
		}
		result[s.Name] = ProcessInfo{
			Memory: sumTreeRSS(pid, rssMap, childMap),
			Uptime: etimeMap[pid],
		}
	}
	return result
}

// readProcessTable parses `ps -eo pid,ppid,rss,etime` into RSS, children, and etime maps.
// RSS values from ps are in KiB. Etime is parsed into seconds.
func readProcessTable() (rss map[int]uint64, children map[int][]int, etime map[int]int) {
	rss = make(map[int]uint64)
	children = make(map[int][]int)
	etime = make(map[int]int)

	out, err := runCombinedOutput("ps", "-eo", "pid,ppid,rss,etime")
	if err != nil {
		return
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
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
