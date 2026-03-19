package engine

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/adibhanna/tsm/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// FetchPreview returns the last `lines` lines of the daemon-rendered preview.
func FetchPreview(name string, lines int) string {
	if lines < 1 {
		lines = 1
	}

	cfg := session.DefaultConfig()
	path := cfg.SocketPath(name)
	conn, err := session.Connect(path)
	if err != nil {
		return fmt.Sprintf("(preview unavailable: %v)", err)
	}
	defer conn.Close()

	if err := session.SendMessage(conn, session.TagHistory, nil); err != nil {
		return fmt.Sprintf("(preview unavailable: %v)", err)
	}

	tag, payload, err := session.ReadMessage(conn, 2*time.Second)
	if err != nil {
		return fmt.Sprintf("(preview unavailable: %v)", err)
	}
	if tag != session.TagHistory {
		return fmt.Sprintf("(preview unavailable: unexpected tag %s)", tag)
	}

	preview, err := tailLinesFromReader(strings.NewReader(string(payload)), lines)
	if err != nil {
		return fmt.Sprintf("(preview unavailable: %v)", err)
	}
	return preview
}

func tailLinesFromReader(r io.Reader, lines int) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	tail := make([]string, 0, lines)
	for scanner.Scan() {
		tail = append(tail, sanitizePreviewLine(scanner.Text()))
		if len(tail) > lines {
			tail = tail[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(tail, "\n"), nil
}

// ScrollPreview applies a horizontal offset and width to raw preview text.
func ScrollPreview(raw string, offsetX, maxWidth int) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		rest := ansi.TruncateLeft(line, offsetX, "")
		visible := ansi.Truncate(rest, maxWidth, "")
		if pad := maxWidth - ansi.StringWidth(visible); pad > 0 {
			visible += strings.Repeat(" ", pad)
		}
		lines[i] = visible
	}
	return strings.Join(lines, "\n")
}

func PreviewWidth(raw string) int {
	maxW := 0
	for _, line := range strings.Split(raw, "\n") {
		if w := ansi.StringWidth(line); w > maxW {
			maxW = w
		}
	}
	return maxW
}

// sanitizePreviewLine removes non-printable control characters while keeping
// ANSI styling sequences intact so the TUI can render colored previews.
func sanitizePreviewLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			start := i
			i++
			if i >= len(s) {
				break
			}
			switch s[i] {
			case '[':
				i++
				for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
					i++
				}
				if i < len(s) {
					i++
				}
				b.WriteString(s[start:i])
			case ']':
				i++
				for i < len(s) {
					if s[i] == '\x07' {
						i++
						break
					}
					if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				b.WriteString(s[start:i])
			case '(', ')':
				i++
				if i < len(s) {
					i++
				}
				b.WriteString(s[start:i])
			default:
				i++
			}
		} else if s[i] == '\r' {
			i++
		} else if s[i] < 0x20 && s[i] != '\n' && s[i] != '\t' {
			i++
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
