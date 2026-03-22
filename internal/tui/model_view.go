package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/adibhanna/tsm/internal/engine"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

var refreshSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func previewMaxWidth(raw string) int {
	return engine.PreviewWidth(raw)
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

	if m.isSimplified() {
		return m.simplifiedView()
	}
	return m.fullView()
}

func (m Model) fullView() tea.View {
	visible := m.visibleSessions()

	// Compute help first so we know its height for layout
	help := ""
	helpLines := 0
	if m.showHelp() {
		help = m.renderHelp()
		helpLines = strings.Count(help, "\n") + 1
	}
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
	selected := Session{}
	if m.cursor < len(visible) {
		selected = visible[m.cursor]
	}
	previewContent := ""
	if selected.AgentKind != "" {
		previewContent = m.renderAgentPreview(selected, pw, ch)
	} else {
		previewContent = clampLines(engine.ScrollPreview(m.preview, m.previewScrollX, pw), ch)
	}
	previewTitleLeft := " Preview "
	previewTitleRight := ""
	if selected.Name != "" {
		previewTitleLeft = fmt.Sprintf(" %s ", selected.Name)
		previewTitleRight = fmt.Sprintf(" 📂 %s ", selected.DisplayDir())
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

	full := lipgloss.JoinVertical(lipgloss.Left, body, logPane)
	if help != "" {
		full = lipgloss.JoinVertical(lipgloss.Left, full, help)
	}
	v := tea.NewView(clampLines(full, m.height))
	v.AltScreen = true
	return v
}

func (m Model) simplifiedView() tea.View {
	outerWidth := m.simplifiedOuterWidth()
	innerWidth := outerWidth - 2
	if innerWidth < 20 {
		innerWidth = 20
	}
	help := ""
	if m.showHelp() {
		helpModel := m
		helpModel.width = outerWidth
		help = helpModel.renderHelp()
	}

	queryLabel := "> "
	queryText := m.filterText
	if queryText == "" {
		if m.inlineFilterEnabled() {
			queryText = "type to filter sessions"
		} else {
			queryText = "press / to filter"
		}
	}
	queryLine := helpKeyStyle.Render(queryLabel) + helpStyle.Render(queryText)
	if m.filterText != "" {
		queryLine = helpKeyStyle.Render(queryLabel) + helpKeyStyle.Render(m.filterText)
	}

	rowsHeight := m.simplifiedRowsHeight()
	selected := Session{}
	visible := m.visibleSessions()
	if m.cursor < len(visible) {
		selected = visible[m.cursor]
	}
	agentLine := m.renderAgentStatusLine(selected, innerWidth)
	contentHeight := rowsHeight + 1
	if agentLine != "" {
		contentHeight++
	}
	listContent := clampLines(m.renderPaletteList(rowsHeight, innerWidth), rowsHeight)
	bodyLines := []string{queryLine}
	if agentLine != "" {
		bodyLines = append(bodyLines, agentLine)
	}
	bodyLines = append(bodyLines, listContent)
	bodyContent := clampLines(strings.Join(bodyLines, "\n"), contentHeight)

	titleLeft := " sessions "
	titleRight := ""

	pane := listBorderStyle.
		Width(outerWidth).
		Height(contentHeight + 2).
		Render(bodyContent)
	pane = replaceTopBorder(pane, buildTopBorderLRStyled(titleLeft, titleRight, outerWidth, sortStyle))
	if selCount := len(m.selected); selCount > 0 {
		pane = replaceBottomBorder(pane, buildBottomBorderR(fmt.Sprintf(" %d sel ", selCount), outerWidth))
	}

	full := centerBlock(pane, m.width)
	if help != "" {
		helpBlock := lipgloss.NewStyle().Width(outerWidth).Render(help)
		full = lipgloss.JoinVertical(lipgloss.Left, full, centerBlock(helpBlock, m.width))
	}
	full = centerVertically(full, m.height)
	v := tea.NewView(clampLines(full, m.height))
	v.AltScreen = true
	return v
}

func (m Model) renderAgentStatusLine(s Session, width int) string {
	if s.AgentKind == "" || width < 12 {
		return ""
	}

	kindStyle := agentClaudeStyle
	switch s.AgentKind {
	case "codex":
		kindStyle = agentCodexStyle
	case "claude":
		kindStyle = agentClaudeStyle
	}

	parts := []string{
		kindStyle.Render(displayAgentKind(s.AgentKind)),
		agentStateStyle.Render(defaultAgentState(s.AgentState, s.AgentUpdated)),
	}
	if age := engine.FormatRelativeTime(s.AgentUpdated); age != "" {
		parts = append(parts, agentMetaStyle.Render(age))
	}

	prefix := strings.Join(parts, "  ")
	if s.AgentSummary == "" {
		return prefix
	}

	plainPrefix := runewidth.StringWidth(ansi.Strip(prefix))
	avail := width - plainPrefix - 2
	if avail < 8 {
		return prefix
	}
	summary := truncate(s.AgentSummary, avail)
	return prefix + "  " + agentMetaStyle.Render("· "+summary)
}

func defaultAgentState(state string, updatedAt int64) string {
	return engine.DisplayAgentState(state, updatedAt)
}

func (m Model) renderAgentPreview(s Session, width, height int) string {
	if s.AgentKind == "" {
		return ""
	}

	lines := []string{
		m.renderAgentPreviewHeader(s),
		"",
	}

	lines = appendSection(lines, "overview", renderOverviewRows(s))
	lines = appendSection(lines, "runtime", renderRuntimeRows(s))
	lines = appendSection(lines, "usage", renderUsageRows(s))
	lines = appendSection(lines, "recent", renderRecentRows(s, width))

	content := strings.Join(lines, "\n")
	return clampLines(content, height)
}

func (m Model) renderAgentPreviewHeader(s Session) string {
	kindStyle := agentClaudeStyle
	if s.AgentKind == "codex" {
		kindStyle = agentCodexStyle
	}
	parts := []string{
		kindStyle.Render(displayAgentKind(s.AgentKind)),
		agentStateStyle.Render(defaultAgentState(s.AgentState, s.AgentUpdated)),
	}
	if age := engine.FormatRelativeTime(s.AgentUpdated); age != "" {
		parts = append(parts, agentMetaStyle.Render(age+" ago"))
	}
	return strings.Join(parts, "  ")
}

func renderUsageRows(s Session) []string {
	rows := []string{}
	if s.AgentTotal > 0 {
		rows = append(rows, "tokens: "+formatTokenCount(s.AgentTotal)+" total")
	}
	if s.AgentOutput > 0 {
		rows = append(rows, "output: "+formatTokenCount(s.AgentOutput))
	}
	if s.AgentCached > 0 {
		rows = append(rows, "cached: "+formatTokenCount(s.AgentCached))
	}
	if s.AgentInput > 9 {
		rows = append(rows, "input: "+formatTokenCount(s.AgentInput))
	}
	if s.AgentContext > 0 {
		rows = append(rows, "context: "+formatTokenCount(s.AgentContext))
	}
	return rows
}

func formatTokenCount(v int64) string {
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(v)/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(v)/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", float64(v)/1_000)
	default:
		return fmt.Sprintf("%d", v)
	}
}

func wrapPlain(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	lines := []string{}
	line := words[0]
	for _, word := range words[1:] {
		next := line + " " + word
		if lipgloss.Width(next) > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line = next
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
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
		return normalStyle.Render("  No sessions found. Press " + m.refreshKeyLabel() + " to refresh.")
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

func (m *Model) renderPaletteList(maxRows, innerWidth int) string {
	visible := m.visibleSessions()
	if len(visible) == 0 {
		if m.filterText != "" {
			return normalStyle.Render("  No matches.")
		}
		return normalStyle.Render("  No sessions found.")
	}

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

		indicator := "  "
		switch {
		case isCursor && isSelected:
			indicator = selectedStyle.Render("▸●")
		case isCursor:
			indicator = selectedStyle.Render("▸ ")
		case isSelected:
			indicator = selectedStyle.Render(" ●")
		}

		clientLabel := inactiveClientStyle.Render(padLeft("○0", metrics.clientW))
		if s.Clients > 0 {
			clientLabel = activeClientStyle.Render(padLeft(fmt.Sprintf("●%d", s.Clients), metrics.clientW))
		}

		uptimeLabel := "-"
		if s.Uptime > 0 {
			uptimeLabel = engine.FormatUptime(s.Uptime)
		}
		uptimeStr := uptimeStyle.Render(padLeft(uptimeLabel, metrics.uptimeW))

		nameWidth := innerWidth - 5 - metrics.uptimeW - metrics.clientW
		if nameWidth < 12 {
			nameWidth = 12
		}
		style := normalStyle
		if isCursor || isSelected {
			style = selectedStyle
		}
		name := truncate(s.Name, nameWidth)
		paddedName := padRight(name, nameWidth)
		styledName := style.Render(paddedName)
		if m.filterText != "" {
			styledName = highlightMatch(paddedName, m.filterText, style, filterMatchStyle)
		}

		row := fmt.Sprintf("%s%s %s %s", indicator, styledName, uptimeStr, clientLabel)
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

	if m.state == stateRenameSession {
		cursor := "█"
		return helpStyle.Render(" Rename: ") + helpKeyStyle.Render(m.renameNewName) + helpStyle.Render(cursor+"  Enter rename | Esc cancel")
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

	if m.isSimplified() {
		return m.renderSimplifiedHelp()
	}

	parts := []string{
		helpKeyStyle.Render(m.previewScrollKeyLabel()) + helpStyle.Render(" scroll"),
		helpKeyStyle.Render(m.navKeyLabel()) + helpStyle.Render(" nav"),
		helpKeyStyle.Render(m.selectKeyLabel()) + helpStyle.Render(" sel"),
		helpKeyStyle.Render(m.selectAllKeyLabel()) + helpStyle.Render(" all"),
		helpKeyStyle.Render(m.attachKeyLabel()) + helpStyle.Render(" attach"),
		helpKeyStyle.Render(m.detachKeyLabel()) + helpStyle.Render(" detach"),
		helpKeyStyle.Render(m.newKeyLabel()) + helpStyle.Render(" new"),
		helpKeyStyle.Render(m.killKeyLabel()) + helpStyle.Render(" kill"),
		helpKeyStyle.Render(m.renameKeyLabel()) + helpStyle.Render(" rename"),
		helpKeyStyle.Render(m.copyKeyLabel()) + helpStyle.Render(" copy cmd"),
		helpKeyStyle.Render(m.sortKeyLabel()) + helpStyle.Render(" sort"),
		helpKeyStyle.Render(m.refreshKeyLabel()) + helpStyle.Render(" refresh"),
		helpKeyStyle.Render(m.toggleLayoutKeyLabel()) + helpStyle.Render(" layout"),
	}
	if m.filterText != "" {
		parts = append(parts, helpKeyStyle.Render("esc")+helpStyle.Render(" clear"))
	} else {
		parts = append(parts, helpKeyStyle.Render(m.filterKeyLabel())+helpStyle.Render(" filter"))
	}
	parts = append(parts,
		helpKeyStyle.Render(m.logScrollKeyLabel())+helpStyle.Render(" log"),
		helpKeyStyle.Render(m.quitKeyLabel())+helpStyle.Render(" quit"),
	)

	if m.status != "" {
		parts = append(parts, statusStyle.Render(m.renderStatus()))
	}

	return wrapHelpParts(parts, m.width)
}

func (m Model) renderSimplifiedHelp() string {
	items := []string{
		helpKeyStyle.Render(m.navKeyLabel()) + helpStyle.Render(" nav"),
		helpKeyStyle.Render(m.selectKeyLabel()) + helpStyle.Render(" sel"),
		helpKeyStyle.Render(m.selectAllKeyLabel()) + helpStyle.Render(" all"),
		helpKeyStyle.Render(m.attachKeyLabel()) + helpStyle.Render(" attach"),
		helpKeyStyle.Render(m.detachKeyLabel()) + helpStyle.Render(" detach"),
		helpKeyStyle.Render(m.newKeyLabel()) + helpStyle.Render(" new"),
		helpKeyStyle.Render(m.renameKeyLabel()) + helpStyle.Render(" rename"),
		helpKeyStyle.Render(m.copyKeyLabel()) + helpStyle.Render(" copy cmd"),
		helpKeyStyle.Render(m.toggleLayoutKeyLabel()) + helpStyle.Render(" layout"),
		helpKeyStyle.Render(m.killKeyLabel()) + helpStyle.Render(" kill"),
		helpKeyStyle.Render(m.sortKeyLabel()) + helpStyle.Render(" sort"),
		helpKeyStyle.Render(m.refreshKeyLabel()) + helpStyle.Render(" refresh"),
	}

	if m.inlineFilterEnabled() {
		items = append(items,
			helpKeyStyle.Render("type")+helpStyle.Render(" to filter"),
			helpKeyStyle.Render("esc")+helpStyle.Render(" clear/quit"),
		)
	} else if m.filterText != "" {
		items = append(items,
			helpKeyStyle.Render("esc")+helpStyle.Render(" clear"),
			helpKeyStyle.Render(m.quitKeyLabel())+helpStyle.Render(" quit"),
		)
	} else {
		items = append(items,
			helpKeyStyle.Render(m.filterKeyLabel())+helpStyle.Render(" filter"),
			helpKeyStyle.Render(m.quitKeyLabel())+helpStyle.Render(" quit"),
		)
	}
	lines := wrapHelpColumns(items, 2)
	if m.status != "" {
		lines = append(lines, " "+statusStyle.Render(m.renderStatus()))
	}
	return strings.Join(lines, "\n")
}

func renderOverviewRows(s Session) []string {
	rows := []string{}
	if model := engine.DisplayAgentModel(s.AgentKind, s.AgentModel); model != "" {
		rows = append(rows, "model: "+model)
	}
	if s.AgentVersion != "" {
		rows = append(rows, "version: "+s.AgentVersion)
	}
	if s.AgentBranch != "" {
		rows = append(rows, "branch: "+s.AgentBranch)
	}
	if s.AgentName != "" {
		label := "agent: " + s.AgentName
		if s.AgentSubagent {
			label += " (subagent)"
		}
		rows = append(rows, label)
	}
	if s.AgentRole != "" && s.AgentRole != "subagent" {
		rows = append(rows, "role: "+s.AgentRole)
	}
	if s.AgentPlan != "" {
		rows = append(rows, "source: "+s.AgentPlan)
	}
	if s.AgentOutputStyle != "" {
		rows = append(rows, "style: "+s.AgentOutputStyle)
	}
	if s.AgentProjectDir != "" {
		rows = append(rows, "project: "+s.AgentProjectDir)
	}
	if s.AgentWorktreePath != "" {
		rows = append(rows, "worktree: "+s.AgentWorktreePath)
	}
	return rows
}

func renderRuntimeRows(s Session) []string {
	rows := []string{}
	if s.AgentApproval != "" {
		rows = append(rows, "approval: "+s.AgentApproval)
	}
	if s.AgentSandbox != "" {
		rows = append(rows, "sandbox: "+s.AgentSandbox)
	}
	if s.AgentMemory != "" {
		rows = append(rows, "memory: "+s.AgentMemory)
	}
	if sha := shortSHA(s.AgentGitSHA); sha != "" {
		rows = append(rows, "commit: "+sha)
	}
	if s.AgentCostUSD > 0 {
		rows = append(rows, fmt.Sprintf("cost: $%.4f", s.AgentCostUSD))
	}
	if s.AgentDurationMS > 0 {
		rows = append(rows, "duration: "+formatMillis(s.AgentDurationMS))
	}
	if s.AgentAPIMS > 0 {
		rows = append(rows, "api wait: "+formatMillis(s.AgentAPIMS))
	}
	if s.AgentLinesAdded > 0 || s.AgentLinesRemoved > 0 {
		rows = append(rows, fmt.Sprintf("changes: +%d/-%d", s.AgentLinesAdded, s.AgentLinesRemoved))
	}
	return rows
}

func renderRecentRows(s Session, width int) []string {
	rows := []string{}
	if s.AgentPrompt != "" {
		rows = append(rows, agentStateStyle.Render("last prompt"))
		rows = append(rows, wrapPlain("  "+s.AgentPrompt, width))
	}
	if s.AgentSummary != "" {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, agentStateStyle.Render("latest update"))
		rows = append(rows, wrapPlain("  "+s.AgentSummary, width))
	}
	return rows
}

func appendSection(lines []string, title string, rows []string) []string {
	if len(rows) == 0 {
		return lines
	}
	lines = append(lines, agentStateStyle.Render(title))
	for _, row := range rows {
		if strings.TrimSpace(row) == "" {
			lines = append(lines, "")
			continue
		}
		if strings.Contains(row, "\n") {
			lines = append(lines, row)
			continue
		}
		if ansi.Strip(row) != row {
			lines = append(lines, row)
			continue
		}
		lines = append(lines, agentMetaStyle.Render(row))
	}
	lines = append(lines, "")
	return lines
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func formatMillis(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	secs = secs % 60
	if mins < 60 {
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	hours := mins / 60
	mins = mins % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

func displayAgentKind(kind string) string {
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

func (m Model) renderStatus() string {
	if !m.refreshing {
		return m.status
	}
	return refreshSpinnerFrames[m.refreshFrame%len(refreshSpinnerFrames)] + " " + m.status
}

func (m Model) simplifiedOuterWidth() int {
	metrics := m.visibleMetrics
	nameW := metrics.nameW
	if nameW < 16 {
		nameW = 16
	}
	if nameW > 24 {
		nameW = 24
	}

	titleMin := 48
	rowWidth := 2 + nameW + 1 + metrics.uptimeW + 1 + metrics.clientW + 2
	w := max(titleMin, rowWidth)
	if w < paletteMinWidth {
		w = paletteMinWidth
	}
	if w > paletteMaxWidth {
		w = paletteMaxWidth
	}
	if maxW := m.width - 8; maxW > 0 && w > maxW {
		w = maxW
	}
	if w < 20 {
		w = min(m.width, 20)
	}
	return w
}

func centerBlock(s string, width int) string {
	lines := strings.Split(s, "\n")
	maxW := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > maxW {
			maxW = w
		}
	}
	if width <= maxW {
		return s
	}
	pad := strings.Repeat(" ", (width-maxW)/2)
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func centerVertically(s string, height int) string {
	if height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) >= height {
		return s
	}
	padTop := (height - len(lines)) / 2
	if padTop <= 0 {
		return s
	}
	return strings.Repeat("\n", padTop) + s
}

func (m Model) simplifiedRowsHeight() int {
	rows := len(m.visibleSessions())
	if rows < 1 {
		rows = 1
	}
	if rows > paletteMaxRows {
		rows = paletteMaxRows
	}
	return rows
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

func wrapHelpColumns(items []string, columns int) []string {
	if len(items) == 0 {
		return nil
	}
	if columns < 1 {
		columns = 1
	}
	rows := (len(items) + columns - 1) / columns
	colWidths := make([]int, columns)
	grid := make([][]string, columns)
	for col := 0; col < columns; col++ {
		start := col * rows
		if start >= len(items) {
			break
		}
		end := min(start+rows, len(items))
		grid[col] = items[start:end]
		for _, item := range grid[col] {
			if w := lipgloss.Width(item); w > colWidths[col] {
				colWidths[col] = w
			}
		}
	}

	lines := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		cols := make([]string, 0, columns)
		for col := 0; col < columns; col++ {
			if row >= len(grid[col]) {
				continue
			}
			item := grid[col][row]
			if col < columns-1 && colWidths[col] > 0 {
				item = padRightANSI(item, colWidths[col])
			}
			cols = append(cols, item)
		}
		lines = append(lines, " "+strings.Join(cols, "   "))
	}
	return lines
}

func padRightANSI(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
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
	lowerQuery := strings.ToLower(query)
	idx := strings.Index(lower, lowerQuery)
	if idx < 0 {
		return base.Render(s)
	}
	// Convert byte offsets in lowered string to rune offsets,
	// then back to byte offsets in the original string.
	runeStart := utf8.RuneCountInString(lower[:idx])
	runeLen := utf8.RuneCountInString(lowerQuery)

	byteStart := 0
	for i := 0; i < runeStart; i++ {
		_, size := utf8.DecodeRuneInString(s[byteStart:])
		byteStart += size
	}
	byteEnd := byteStart
	for i := 0; i < runeLen; i++ {
		_, size := utf8.DecodeRuneInString(s[byteEnd:])
		byteEnd += size
	}

	return base.Render(s[:byteStart]) + hlStyle.Render(s[byteStart:byteEnd]) + base.Render(s[byteEnd:])
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
