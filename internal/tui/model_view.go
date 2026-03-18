package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/adibhanna/tsm/internal/engine"
	"github.com/mattn/go-runewidth"
)

func previewMaxWidth(raw string) int {
	maxW := 0
	for _, line := range strings.Split(raw, "\n") {
		if w := runewidth.StringWidth(line); w > maxW {
			maxW = w
		}
	}
	return maxW
}

// Layout

func (m Model) mainContentHeight(helpLines int) int {
	// 4 = 2 (list/preview borders) + 2 (log borders)
	h := m.height - logContentHeight - 4 - helpLines
	if h < 1 {
		h = 1
	}
	return h
}

// listOuterWidth computes the list pane width from session content.
// Row layout: indicator(2) + name + " " + pid + " " + client + " " + mem + borders(2)
func (m *Model) listOuterWidth() int {
	// Minimum: must fit the title elements (display widths, not byte lengths).
	// Left (non-filtering is always wider): " tsm sessions (NNN) " = 17 + digits
	// Right (longest sort label): " ↓ clients " = 11 display cells
	// Border chrome: ╭─ ... ╮ = 4 (2 left + 1 right + 1 fill)
	n := len(m.sessions)
	digits := len(fmt.Sprintf("%d", n))
	titleMin := (17 + digits) + 11 + 4

	metrics := m.allSessionMetrics()
	// 2 (indicator) + name + " " + pid + " " + mem + " " + uptime + " " + client + 2 (borders)
	w := 2 + metrics.nameW + 1 + metrics.pidW + 1 + metrics.memW + 1 + metrics.uptimeW + 1 + metrics.clientW + 2
	if w < titleMin {
		w = titleMin
	}
	if w > listMaxOuterWidth {
		w = listMaxOuterWidth
	}
	// Don't let the list take more than half the terminal
	if half := m.width / 2; w > half && half >= titleMin {
		w = half
	}
	return w
}

func (m *Model) listInnerWidth() int {
	return m.listOuterWidth() - 2
}

func (m *Model) previewOuterWidth() int {
	w := m.width - m.listOuterWidth()
	if w < 10 {
		w = 10
	}
	return w
}

func (m *Model) previewInnerWidth() int {
	return m.previewOuterWidth() - 2
}

// View

func (m Model) View() tea.View {
	if m.err != nil {
		v := tea.NewView(fmt.Sprintf("\n  Error: %v\n\n  Session engine error?\n", m.err))
		v.AltScreen = true
		return v
	}
	if m.width == 0 {
		v := tea.NewView("  Loading...")
		v.AltScreen = true
		return v
	}

	visible := m.visibleSessions()

	// Compute help first so we know its height for layout
	help := m.renderHelp()
	helpLines := strings.Count(help, "\n") + 1
	ch := m.mainContentHeight(helpLines)

	// --- List pane ---
	listContent := m.renderList(ch)
	listContent = clampLines(listContent, ch)

	listTitleLeft := fmt.Sprintf(" tsm sessions (%d) ", len(visible))
	if len(visible) != len(m.sessions) {
		listTitleLeft = fmt.Sprintf(" tsm (%d/%d) ", len(visible), len(m.sessions))
	}
	sortArrow := "↑"
	if !m.sortAsc {
		sortArrow = "↓"
	}
	listTitleRight := fmt.Sprintf(" %s %s ", sortArrow, m.sortMode.label())

	low := m.listOuterWidth()
	listPane := listBorderStyle.
		Width(low).
		Height(ch + 2).
		Render(listContent)
	listPane = replaceTopBorder(listPane, buildTopBorderLRStyled(listTitleLeft, listTitleRight, low, sortStyle))
	if selCount := len(m.selected); selCount > 0 {
		selLabel := fmt.Sprintf(" %d sel ", selCount)
		listPane = replaceBottomBorder(listPane, buildBottomBorderR(selLabel, low))
	}

	// --- Preview pane ---
	pw := m.previewInnerWidth()
	previewContent := clampLines(engine.ScrollPreview(m.preview, m.previewScrollX, pw), ch)
	previewTitleLeft := " Preview "
	previewTitleRight := ""
	if m.cursor < len(visible) {
		s := visible[m.cursor]
		previewTitleLeft = fmt.Sprintf(" %s ", s.Name)
		previewTitleRight = fmt.Sprintf(" 📂 %s ", s.DisplayDir())
	}
	pow := m.previewOuterWidth()

	previewPane := previewBorderStyle.
		Width(pow).
		Height(ch + 2).
		Render(previewContent)
	previewPane = replaceTopBorder(previewPane, buildTopBorderLR(previewTitleLeft, previewTitleRight, pow))

	body := lipgloss.JoinHorizontal(lipgloss.Top, listPane, previewPane)

	// --- Log pane ---
	logContent := m.renderLog()

	logPane := logBorderStyle.
		Width(m.width).
		Height(logContentHeight + 2).
		Render(logContent)

	logPane = replaceTopBorder(logPane, buildTopBorder(" Activity Log ", m.width))

	full := lipgloss.JoinVertical(lipgloss.Left, body, logPane, help)
	v := tea.NewView(clampLines(full, m.height))
	v.AltScreen = true
	return v
}

func (m Model) renderLog() string {
	if len(m.logLines) == 0 {
		return logDimStyle.Render("  No activity yet.")
	}

	end := m.logOffset + logContentHeight
	if end > len(m.logLines) {
		end = len(m.logLines)
	}
	start := m.logOffset
	if start < 0 {
		start = 0
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(m.logLines[i])
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *Model) renderList(maxRows int) string {
	visible := m.visibleSessions()
	if len(visible) == 0 {
		if m.filterText != "" {
			return normalStyle.Render("  No matches. Esc to clear filter.")
		}
		return normalStyle.Render("  No sessions found. Press r to refresh.")
	}

	lw := m.listInnerWidth()
	metrics := m.visibleMetrics
	var b strings.Builder

	end := m.listOffset + maxRows
	if end > len(visible) {
		end = len(visible)
	}

	for i := m.listOffset; i < end; i++ {
		s := visible[i]
		isCursor := i == m.cursor
		isSelected := m.selected[s.Name]

		var indicator string
		switch {
		case isCursor && isSelected:
			indicator = selectedStyle.Render("▸●")
		case isCursor:
			indicator = selectedStyle.Render("▸ ")
		case isSelected:
			indicator = selectedStyle.Render(" ●")
		default:
			indicator = "  "
		}

		var clientInd string
		if s.Clients > 0 {
			clientInd = activeClientStyle.Render(padLeft(fmt.Sprintf("●%d", s.Clients), metrics.clientW))
		} else {
			clientInd = inactiveClientStyle.Render(padLeft("○0", metrics.clientW))
		}

		pidStr := pidStyle.Render(padLeft(s.PID, metrics.pidW))

		memLabel := "-"
		if s.Memory > 0 {
			memLabel = engine.FormatBytes(s.Memory)
		}
		memStr := memStyle.Render(padLeft(memLabel, metrics.memW))

		uptimeLabel := "-"
		if s.Uptime > 0 {
			uptimeLabel = engine.FormatUptime(s.Uptime)
		}
		uptimeStr := uptimeStyle.Render(padLeft(uptimeLabel, metrics.uptimeW))

		// lw = indicator(2) + name + " " + pid + " " + mem + " " + uptime + " " + client
		nameWidth := lw - 6 - metrics.pidW - metrics.memW - metrics.uptimeW - metrics.clientW
		if nameWidth < 10 {
			nameWidth = 10
		}
		name := truncate(s.Name, nameWidth)
		paddedName := padRight(name, nameWidth)

		style := normalStyle
		if isCursor || isSelected {
			style = selectedStyle
		}

		var styledName string
		if m.filterText != "" {
			styledName = highlightMatch(paddedName, m.filterText, style, filterMatchStyle)
		} else {
			styledName = style.Render(paddedName)
		}

		row := fmt.Sprintf("%s%s %s %s %s %s", indicator, styledName, pidStr, memStr, uptimeStr, clientInd)
		b.WriteString(row)
		if i < end-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderHelp() string {
	if m.state == stateNewSession {
		cursor := "█"
		return helpStyle.Render(" New session: ") + helpKeyStyle.Render(m.newSessionName) + helpStyle.Render(cursor+"  Enter create | Esc cancel")
	}

	if m.state == stateFilter {
		cursor := "█"
		return helpStyle.Render(" /") + helpKeyStyle.Render(m.filterText) + helpStyle.Render(cursor+"  Enter accept | Esc clear")
	}

	if m.state == stateConfirmKill {
		targets := m.killTargets()
		if len(targets) == 1 {
			return confirmStyle.Render(fmt.Sprintf(" Kill %s? y/n ", targets[0]))
		}
		return confirmStyle.Render(fmt.Sprintf(" Kill %d sessions? y/n ", len(targets)))
	}

	parts := []string{
		helpKeyStyle.Render("←→") + helpStyle.Render(" scroll"),
		helpKeyStyle.Render("↑↓") + helpStyle.Render(" nav"),
		helpKeyStyle.Render("space") + helpStyle.Render(" sel"),
		helpKeyStyle.Render("^a") + helpStyle.Render(" all"),
		helpKeyStyle.Render("enter") + helpStyle.Render(" attach"),
		helpKeyStyle.Render("n") + helpStyle.Render(" new"),
		helpKeyStyle.Render("k") + helpStyle.Render(" kill"),
		helpKeyStyle.Render("c") + helpStyle.Render(" copy cmd"),
		helpKeyStyle.Render("s") + helpStyle.Render(" sort"),
	}
	if m.filterText != "" {
		parts = append(parts, helpKeyStyle.Render("esc")+helpStyle.Render(" clear"))
	} else {
		parts = append(parts, helpKeyStyle.Render("/")+helpStyle.Render(" filter"))
	}
	parts = append(parts,
		helpKeyStyle.Render("[]")+helpStyle.Render(" log"),
		helpKeyStyle.Render("q")+helpStyle.Render(" quit"),
	)

	if m.status != "" {
		parts = append(parts, statusStyle.Render(m.status))
	}

	return wrapHelpParts(parts, m.width)
}

// wrapHelpParts joins help items with wrapping at maxWidth.
func wrapHelpParts(parts []string, maxWidth int) string {
	if maxWidth <= 0 {
		return " " + strings.Join(parts, "  ")
	}
	var lines []string
	line := " "
	lineW := 1
	for i, p := range parts {
		pw := lipgloss.Width(p)
		sep := "  "
		sepW := 2
		if i == 0 {
			sep = ""
			sepW = 0
		}
		if lineW+sepW+pw > maxWidth && lineW > 1 {
			lines = append(lines, line)
			line = " " + p
			lineW = 1 + pw
		} else {
			line += sep + p
			lineW += sepW + pw
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

// Border helpers

var borderCharStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

func buildTopBorder(title string, outerWidth int) string {
	return buildTopBorderLR(title, "", outerWidth)
}

func buildTopBorderLR(left, right string, outerWidth int) string {
	return buildTopBorderLRStyled(left, right, outerWidth, logDimStyle)
}

func buildTopBorderLRStyled(left, right string, outerWidth int, rs lipgloss.Style) string {
	styledLeft := titleStyle.Render(left)
	leftVW := lipgloss.Width(styledLeft)

	var styledRight string
	var rightVW int
	if right != "" {
		styledRight = rs.Render(right)
		rightVW = lipgloss.Width(styledRight)
	}

	maxVW := outerWidth - 4
	if maxVW < 1 {
		maxVW = 1
	}

	// Truncate right (dir) first to preserve left (session name)
	if leftVW+rightVW > maxVW {
		maxRight := maxVW - leftVW - 1
		if maxRight < 4 {
			// Not enough room for right at all, drop it
			styledRight = ""
			rightVW = 0
		} else {
			right = truncate(right, maxRight)
			styledRight = rs.Render(right)
			rightVW = lipgloss.Width(styledRight)
		}
	}
	// If still too wide, truncate left
	if leftVW+rightVW > maxVW {
		left = truncate(left, maxVW-rightVW-1)
		styledLeft = titleStyle.Render(left)
		leftVW = lipgloss.Width(styledLeft)
	}

	fill := outerWidth - 3 - leftVW - rightVW
	if fill < 0 {
		fill = 0
	}

	result := borderCharStyle.Render("╭─") + styledLeft
	if styledRight != "" {
		result += borderCharStyle.Render(strings.Repeat("─", fill)) + styledRight + borderCharStyle.Render("╮")
	} else {
		result += borderCharStyle.Render(strings.Repeat("─", fill) + "╮")
	}
	return result
}

func buildBottomBorderR(right string, outerWidth int) string {
	styledRight := selectedStyle.Render(right)
	rightVW := lipgloss.Width(styledRight)
	// ╰ (1) + fill + right (rightVW) + ╯ (1) = outerWidth
	fill := outerWidth - 2 - rightVW
	if fill < 0 {
		fill = 0
	}
	return borderCharStyle.Render("╰"+strings.Repeat("─", fill)) + styledRight + borderCharStyle.Render("╯")
}

func replaceBottomBorder(pane, newBottom string) string {
	lastNL := strings.LastIndex(pane, "\n")
	if lastNL < 0 {
		return pane
	}
	return pane[:lastNL+1] + newBottom
}

func replaceTopBorder(pane, newTop string) string {
	_, rest, ok := strings.Cut(pane, "\n")
	if !ok {
		return pane
	}
	return newTop + "\n" + rest
}

func clampLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return runewidth.Truncate(s, maxLen, "")
	}
	return runewidth.Truncate(s, maxLen, "...")
}

// highlightMatch renders s with base style, but highlights the first case-insensitive
// match of query using hlStyle.
func highlightMatch(s, query string, base, hlStyle lipgloss.Style) string {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, strings.ToLower(query))
	if idx < 0 {
		return base.Render(s)
	}
	end := idx + len(query)
	return base.Render(s[:idx]) + hlStyle.Render(s[idx:end]) + base.Render(s[end:])
}

func padLeft(s string, width int) string {
	if w := runewidth.StringWidth(s); w >= width {
		return s
	} else {
		return strings.Repeat(" ", width-w) + s
	}
}

func padRight(s string, width int) string {
	if w := runewidth.StringWidth(s); w >= width {
		return s
	} else {
		return s + strings.Repeat(" ", width-w)
	}
}
