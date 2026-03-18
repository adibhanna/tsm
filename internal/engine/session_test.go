package engine

import (
	"strings"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1K"},
		{50 * 1024, "50K"},
		{1 << 20, "1M"},
		{5 * (1 << 20), "5M"},
		{142 * (1 << 20), "142M"},
		{1 << 30, "1.0G"},
		{2 * (1 << 30), "2.0G"},
		{10 * (1 << 30), "10G"},
		{15 * (1 << 30), "15G"},
	}
	for _, tt := range tests {
		got := FormatBytes(tt.input)
		if got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSumTreeRSS(t *testing.T) {
	// Tree:  1 → 2 → 4
	//          → 3
	rss := map[int]uint64{
		1: 100,
		2: 200,
		3: 300,
		4: 400,
	}
	children := map[int][]int{
		1: {2, 3},
		2: {4},
	}

	tests := []struct {
		pid  int
		want uint64
	}{
		{1, 1000}, // 100 + 200 + 300 + 400
		{2, 600},  // 200 + 400
		{3, 300},  // leaf
		{4, 400},  // leaf
		{99, 0},   // missing pid
	}
	for _, tt := range tests {
		got := sumTreeRSS(tt.pid, rss, children)
		if got != tt.want {
			t.Errorf("sumTreeRSS(%d) = %d, want %d", tt.pid, got, tt.want)
		}
	}
}

func TestSumTreeRSS_DisjointTrees(t *testing.T) {
	// Two separate trees: 10→11, 20→21→22
	rss := map[int]uint64{
		10: 50,
		11: 60,
		20: 70,
		21: 80,
		22: 90,
	}
	children := map[int][]int{
		10: {11},
		20: {21},
		21: {22},
	}

	if got := sumTreeRSS(10, rss, children); got != 110 {
		t.Errorf("tree rooted at 10 = %d, want 110", got)
	}
	if got := sumTreeRSS(20, rss, children); got != 240 {
		t.Errorf("tree rooted at 20 = %d, want 240", got)
	}
}

func TestParseEtime(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"00:05", 5},           // 5 seconds
		{"01:30", 90},          // 1 min 30 sec
		{"02:15:30", 8130},     // 2h 15m 30s
		{"1-00:00:00", 86400},  // 1 day
		{"3-12:30:45", 304245}, // 3d 12h 30m 45s
		{"00:00", 0},           // zero
	}
	for _, tt := range tests {
		got := parseEtime(tt.input)
		if got != tt.want {
			t.Errorf("parseEtime(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0s"},
		{30, "30s"},
		{59, "59s"},
		{60, "1m"},
		{3599, "59m"},
		{3600, "1h"},
		{7200, "2h"},
		{86399, "23h"},
		{86400, "1d"},
		{259200, "3d"},
	}
	for _, tt := range tests {
		got := FormatUptime(tt.input)
		if got != tt.want {
			t.Errorf("FormatUptime(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTailLinesFromReader(t *testing.T) {
	input := "one\n\x1b[31mtwo\x1b[0m\nthree\nfour\n"
	got, err := tailLinesFromReader(strings.NewReader(input), 2)
	if err != nil {
		t.Fatalf("tailLinesFromReader error: %v", err)
	}
	if got != "three\nfour" {
		t.Fatalf("tailLinesFromReader = %q, want %q", got, "three\nfour")
	}
}

func TestTailLinesFromReader_AtLeastOneLine(t *testing.T) {
	got, err := tailLinesFromReader(strings.NewReader("solo\n"), 1)
	if err != nil {
		t.Fatalf("tailLinesFromReader error: %v", err)
	}
	if got != "solo" {
		t.Fatalf("tailLinesFromReader = %q, want %q", got, "solo")
	}
}

func TestFetchSessionsNoError(t *testing.T) {
	// FetchSessions uses native IPC now. Should not error even with no sessions.
	sessions, err := FetchSessions()
	if err != nil {
		t.Fatalf("FetchSessions error: %v", err)
	}
	_ = sessions
}

func TestCopyToClipboardUsesInjectedDeps(t *testing.T) {
	orig := deps
	defer func() { deps = orig }()

	var copied string
	deps.clipboardWrite = func(text string) error {
		copied = text
		return nil
	}

	if err := CopyToClipboard("tsm attach demo"); err != nil {
		t.Fatalf("CopyToClipboard error: %v", err)
	}
	if copied != "tsm attach demo" {
		t.Fatalf("clipboard text = %q, want %q", copied, "tsm attach demo")
	}
}
