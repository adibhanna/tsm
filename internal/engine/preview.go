package engine

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/adibhanna/tsm/internal/session"
)

// FetchPreview returns the last `lines` lines of a session's scrollback,
// with all ANSI escape sequences stripped.
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
		tail = append(tail, stripANSI(scanner.Text()))
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
		skipped := 0
		runeIdx := 0
		runes := []rune(line)
		for runeIdx < len(runes) && skipped < offsetX {
			w := runewidth.RuneWidth(runes[runeIdx])
			skipped += w
			runeIdx++
		}
		rest := string(runes[runeIdx:])
		lines[i] = runewidth.FillRight(runewidth.Truncate(rest, maxWidth, ""), maxWidth)
	}
	return strings.Join(lines, "\n")
}

// stripANSI removes all ANSI escape sequences and non-printable control
// characters (except newline and tab) from s.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
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
			case '(', ')':
				i++
				if i < len(s) {
					i++
				}
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
