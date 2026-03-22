package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/adibhanna/tsm/internal/engine"
	"github.com/mattn/go-runewidth"
)

const (
	listMaxOuterWidth = 56
	logContentHeight  = 4
	maxLogLines       = 200
	paletteMaxWidth   = 68
	paletteMinWidth   = 46
	paletteMaxRows    = 8
	autoRefreshEvery  = 2 * time.Second
)

type state int

const (
	stateNormal state = iota
	stateConfirmKill
	stateFilter
	stateNewSession
	stateRenameSession
)

type sortMode int

const (
	sortByName sortMode = iota
	sortByClients
	sortByPID
	sortByMemory
	sortByUptime
	sortModeCount
)

func (s sortMode) label() string {
	switch s {
	case sortByName:
		return "name"
	case sortByClients:
		return "clients"
	case sortByPID:
		return "pid"
	case sortByMemory:
		return "memory"
	case sortByUptime:
		return "uptime"
	}
	return ""
}

// Messages

type sessionsMsg struct {
	sessions []Session
	err      error
}

type Session = engine.Session

type previewMsg struct {
	name    string
	content string
}

type statusClearMsg struct{}
type autoRefreshMsg struct{}
type refreshSpinnerMsg struct{}

type killOneResultMsg struct {
	name string
	err  error
}

type detachOneResultMsg struct {
	name string
	err  error
}

type processInfoMsg struct {
	info map[string]engine.ProcessInfo
}

type createSessionMsg struct {
	name string
	err  error
}

type renameSessionMsg struct {
	oldName string
	newName string
	err     error
}

// Commands

func fetchSessionsCmd() tea.Msg {
	sessions, err := engine.FetchSessions()
	return sessionsMsg{sessions: sessions, err: err}
}

func fetchProcessInfoCmd(sessions []Session) tea.Cmd {
	return func() tea.Msg {
		return processInfoMsg{info: engine.FetchProcessInfo(sessions)}
	}
}

func fetchPreviewCmd(name string, lines int) tea.Cmd {
	return func() tea.Msg {
		return previewMsg{name: name, content: engine.FetchPreview(name, lines)}
	}
}

func createSessionCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := engine.CreateSession(name)
		return createSessionMsg{name: name, err: err}
	}
}

func renameSessionCmd(oldName, newName string) tea.Cmd {
	return func() tea.Msg {
		err := engine.RenameSession(oldName, newName)
		return renameSessionMsg{oldName: oldName, newName: newName, err: err}
	}
}

func killOneCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := engine.KillSession(name)
		return killOneResultMsg{name: name, err: err}
	}
}

func detachOneCmd(name string) tea.Cmd {
	return func() tea.Msg {
		err := engine.DetachSession(name)
		return detachOneResultMsg{name: name, err: err}
	}
}

func clearStatusAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return statusClearMsg{}
	})
}

func autoRefreshCmd() tea.Cmd {
	return tea.Tick(autoRefreshEvery, func(time.Time) tea.Msg {
		return autoRefreshMsg{}
	})
}

func refreshSpinnerCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg {
		return refreshSpinnerMsg{}
	})
}

// Model

type Model struct {
	options Options

	sessions   []Session
	cursor     int
	listOffset int
	selected   map[string]bool

	filterText   string
	sortMode     sortMode
	sortAsc      bool
	attachTarget string // non-empty → exec tsm attach after quit

	preview        string
	previewScrollX int
	state          state
	status         string
	refreshing     bool
	refreshFrame   int
	newSessionName string
	renameOldName  string
	renameNewName  string

	// Activity log
	logLines  []string
	logOffset int

	width  int
	height int
	err    error

	visibleCache      []Session
	visibleCacheDirty bool
	visibleMetrics    listMetrics
	allMetrics        listMetrics
	allMetricsDirty   bool
}

type listMetrics struct {
	nameW   int
	pidW    int
	memW    int
	uptimeW int
	clientW int
}

func initialModel() Model {
	return Model{
		options:           NormalizeOptions(Options{}),
		selected:          make(map[string]bool),
		sortAsc:           true,
		visibleCacheDirty: true,
		allMetricsDirty:   true,
	}
}

func NewModel(opts ...Options) Model {
	m := initialModel()
	if len(opts) > 0 {
		m.options = NormalizeOptions(opts[0])
	}
	return m
}

func (m Model) AttachTarget() string {
	return m.attachTarget
}

func (m Model) Options() Options {
	return NormalizeOptions(m.options)
}

func (m Model) isSimplified() bool {
	return NormalizeOptions(m.options).Mode == ModeSimplified
}

func (m Model) keymap() Keymap {
	return NormalizeOptions(m.options).Keymap
}

func (m Model) inlineFilterEnabled() bool {
	return m.keymap() == KeymapPalette
}

func (m Model) showHelp() bool {
	return NormalizeOptions(m.options).ShowHelp
}

// visibleSessions returns sessions matching the current filter, sorted by sortMode.
func (m *Model) visibleSessions() []Session {
	if !m.visibleCacheDirty {
		return m.visibleCache
	}
	m.visibleCache = m.computeVisibleSessions()
	m.visibleMetrics = computeListMetrics(m.visibleCache)
	m.visibleCacheDirty = false
	return m.visibleCache
}

func (m *Model) markSessionsChanged() {
	m.visibleCacheDirty = true
	m.allMetricsDirty = true
}

func (m *Model) markVisibleChanged() {
	m.visibleCacheDirty = true
}

func (m *Model) allSessionMetrics() listMetrics {
	if m.allMetricsDirty {
		m.allMetrics = computeListMetrics(m.sessions)
		m.allMetricsDirty = false
	}
	return m.allMetrics
}

func (m *Model) computeVisibleSessions() []Session {
	var filtered []Session
	if m.filterText == "" {
		filtered = make([]Session, len(m.sessions))
		copy(filtered, m.sessions)
	} else {
		lower := strings.ToLower(m.filterText)
		for _, s := range m.sessions {
			if strings.Contains(strings.ToLower(s.Name), lower) ||
				strings.Contains(strings.ToLower(s.StartedIn), lower) {
				filtered = append(filtered, s)
			}
		}
	}

	dir := 1
	if !m.sortAsc {
		dir = -1
	}
	switch m.sortMode {
	case sortByName:
		slices.SortFunc(filtered, func(a, b Session) int {
			return dir * cmp.Compare(a.Name, b.Name)
		})
	case sortByClients:
		slices.SortFunc(filtered, func(a, b Session) int {
			if a.Clients != b.Clients {
				return dir * (a.Clients - b.Clients)
			}
			return cmp.Compare(a.Name, b.Name)
		})
	case sortByPID:
		slices.SortFunc(filtered, func(a, b Session) int {
			ai, _ := strconv.Atoi(a.PID)
			bi, _ := strconv.Atoi(b.PID)
			if ai != bi {
				return dir * (ai - bi)
			}
			return cmp.Compare(a.Name, b.Name)
		})
	case sortByMemory:
		slices.SortFunc(filtered, func(a, b Session) int {
			if a.Memory != b.Memory {
				return dir * cmp.Compare(a.Memory, b.Memory)
			}
			return cmp.Compare(a.Name, b.Name)
		})
	case sortByUptime:
		slices.SortFunc(filtered, func(a, b Session) int {
			if a.Uptime != b.Uptime {
				return dir * (a.Uptime - b.Uptime)
			}
			return cmp.Compare(a.Name, b.Name)
		})
	}

	return filtered
}

func computeListMetrics(sessions []Session) listMetrics {
	metrics := listMetrics{
		pidW:    1,
		memW:    1,
		uptimeW: 1,
		clientW: 2,
	}
	for _, s := range sessions {
		if w := runewidth.StringWidth(s.Name); w > metrics.nameW {
			metrics.nameW = w
		}
		if w := runewidth.StringWidth(s.PID); w > metrics.pidW {
			metrics.pidW = w
		}
		memLabel := "-"
		if s.Memory > 0 {
			memLabel = engine.FormatBytes(s.Memory)
		}
		if w := runewidth.StringWidth(memLabel); w > metrics.memW {
			metrics.memW = w
		}
		uptimeLabel := "-"
		if s.Uptime > 0 {
			uptimeLabel = engine.FormatUptime(s.Uptime)
		}
		if w := runewidth.StringWidth(uptimeLabel); w > metrics.uptimeW {
			metrics.uptimeW = w
		}
		clientLabel := fmt.Sprintf("●%d", s.Clients)
		if w := runewidth.StringWidth(clientLabel); w > metrics.clientW {
			metrics.clientW = w
		}
	}
	return metrics
}

func (m *Model) addLog(line string) {
	ts := logDimStyle.Render(time.Now().Format("15:04:05"))
	m.logLines = append(m.logLines, ts+" "+line)
	if len(m.logLines) > maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
	}
	maxOff := len(m.logLines) - logContentHeight
	if maxOff < 0 {
		maxOff = 0
	}
	m.logOffset = maxOff
	if m.logOffset > len(m.logLines) {
		m.logOffset = len(m.logLines)
	}
}

func (m Model) listContentHeight(helpLines int) int {
	if m.isSimplified() {
		h := m.height - helpLines - 4
		if h < 3 {
			h = 3
		}
		return h
	}
	return m.mainContentHeight(helpLines)
}

// clampCursor ensures cursor and listOffset are valid for the visible list.
func (m *Model) clampCursor() {
	visible := m.visibleSessions()
	if m.cursor >= len(visible) {
		m.cursor = max(0, len(visible)-1)
	}
	if m.listOffset > m.cursor {
		m.listOffset = m.cursor
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchSessionsCmd, autoRefreshCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		visible := m.visibleSessions()
		if !m.isSimplified() && m.cursor < len(visible) {
			return m, m.previewCmd()
		}

	case sessionsMsg:
		if msg.err != nil {
			m.err = msg.err
			if m.refreshing {
				m.refreshing = false
				m.status = "Refresh failed"
				return m, clearStatusAfter(2 * time.Second)
			}
			return m, nil
		}
		prevByName := make(map[string]Session, len(m.sessions))
		for _, s := range m.sessions {
			prevByName[s.Name] = s
		}
		for i := range msg.sessions {
			if prev, ok := prevByName[msg.sessions[i].Name]; ok {
				msg.sessions[i].Memory = prev.Memory
				msg.sessions[i].Uptime = prev.Uptime
				msg.sessions[i].AgentKind = prev.AgentKind
				msg.sessions[i].AgentState = prev.AgentState
				msg.sessions[i].AgentSummary = prev.AgentSummary
				msg.sessions[i].AgentUpdated = prev.AgentUpdated
				msg.sessions[i].AgentModel = prev.AgentModel
				msg.sessions[i].AgentVersion = prev.AgentVersion
				msg.sessions[i].AgentPrompt = prev.AgentPrompt
				msg.sessions[i].AgentPlan = prev.AgentPlan
				msg.sessions[i].AgentApproval = prev.AgentApproval
				msg.sessions[i].AgentSandbox = prev.AgentSandbox
				msg.sessions[i].AgentBranch = prev.AgentBranch
				msg.sessions[i].AgentGitSHA = prev.AgentGitSHA
				msg.sessions[i].AgentGitOrigin = prev.AgentGitOrigin
				msg.sessions[i].AgentName = prev.AgentName
				msg.sessions[i].AgentRole = prev.AgentRole
				msg.sessions[i].AgentMemory = prev.AgentMemory
				msg.sessions[i].AgentSessionID = prev.AgentSessionID
				msg.sessions[i].AgentSubagent = prev.AgentSubagent
				msg.sessions[i].AgentInput = prev.AgentInput
				msg.sessions[i].AgentOutput = prev.AgentOutput
				msg.sessions[i].AgentCached = prev.AgentCached
				msg.sessions[i].AgentTotal = prev.AgentTotal
				msg.sessions[i].AgentContext = prev.AgentContext
				msg.sessions[i].AgentCostUSD = prev.AgentCostUSD
				msg.sessions[i].AgentDurationMS = prev.AgentDurationMS
				msg.sessions[i].AgentAPIMS = prev.AgentAPIMS
				msg.sessions[i].AgentLinesAdded = prev.AgentLinesAdded
				msg.sessions[i].AgentLinesRemoved = prev.AgentLinesRemoved
				msg.sessions[i].AgentOutputStyle = prev.AgentOutputStyle
				msg.sessions[i].AgentProjectDir = prev.AgentProjectDir
				msg.sessions[i].AgentWorktreePath = prev.AgentWorktreePath
			}
		}
		m.sessions = msg.sessions
		m.markSessionsChanged()
		live := make(map[string]bool, len(m.sessions))
		for _, s := range m.sessions {
			live[s.Name] = true
		}
		for name := range m.selected {
			if !live[name] {
				delete(m.selected, name)
			}
		}
		m.clampCursor()
		cmds := []tea.Cmd{fetchProcessInfoCmd(m.sessions)}
		visible := m.visibleSessions()
		if !m.isSimplified() && len(visible) > 0 && m.cursor < len(visible) {
			if visible[m.cursor].AgentKind == "" {
				cmds = append(cmds, m.previewCmd())
			}
		} else {
			m.preview = ""
		}
		if m.refreshing {
			m.refreshing = false
			m.refreshFrame = 0
			m.status = "Refreshed"
			cmds = append(cmds, clearStatusAfter(1200*time.Millisecond))
		}
		return m, tea.Batch(cmds...)

	case processInfoMsg:
		updated := false
		for i := range m.sessions {
			if info, ok := msg.info[m.sessions[i].Name]; ok {
				m.sessions[i].Memory = info.Memory
				m.sessions[i].Uptime = info.Uptime
				m.sessions[i].AgentKind = info.AgentKind
				m.sessions[i].AgentState = info.AgentState
				m.sessions[i].AgentSummary = info.AgentSummary
				m.sessions[i].AgentUpdated = info.AgentUpdated
				m.sessions[i].AgentModel = info.AgentModel
				m.sessions[i].AgentVersion = info.AgentVersion
				m.sessions[i].AgentPrompt = info.AgentPrompt
				m.sessions[i].AgentPlan = info.AgentPlan
				m.sessions[i].AgentApproval = info.AgentApproval
				m.sessions[i].AgentSandbox = info.AgentSandbox
				m.sessions[i].AgentBranch = info.AgentBranch
				m.sessions[i].AgentGitSHA = info.AgentGitSHA
				m.sessions[i].AgentGitOrigin = info.AgentGitOrigin
				m.sessions[i].AgentName = info.AgentName
				m.sessions[i].AgentRole = info.AgentRole
				m.sessions[i].AgentMemory = info.AgentMemory
				m.sessions[i].AgentSessionID = info.AgentSessionID
				m.sessions[i].AgentSubagent = info.AgentSubagent
				m.sessions[i].AgentInput = info.AgentInput
				m.sessions[i].AgentOutput = info.AgentOutput
				m.sessions[i].AgentCached = info.AgentCached
				m.sessions[i].AgentTotal = info.AgentTotal
				m.sessions[i].AgentContext = info.AgentContext
				m.sessions[i].AgentCostUSD = info.AgentCostUSD
				m.sessions[i].AgentDurationMS = info.AgentDurationMS
				m.sessions[i].AgentAPIMS = info.AgentAPIMS
				m.sessions[i].AgentLinesAdded = info.AgentLinesAdded
				m.sessions[i].AgentLinesRemoved = info.AgentLinesRemoved
				m.sessions[i].AgentOutputStyle = info.AgentOutputStyle
				m.sessions[i].AgentProjectDir = info.AgentProjectDir
				m.sessions[i].AgentWorktreePath = info.AgentWorktreePath
				updated = true
			}
		}
		if updated {
			m.markSessionsChanged()
		}

	case previewMsg:
		visible := m.visibleSessions()
		if m.cursor < len(visible) && visible[m.cursor].Name == msg.name {
			m.preview = msg.content
		}

	case killOneResultMsg:
		if msg.err != nil {
			m.addLog(confirmStyle.Render("  ✗ " + msg.name))
			// On error, refresh immediately to restore the session
			return m, fetchSessionsCmd
		}
		m.addLog(statusStyle.Render("  ✓ " + msg.name))
		// Delay refresh to let session finish cleanup
		return m, tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
			return fetchSessionsCmd()
		})

	case detachOneResultMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Detach failed: %v", msg.err)
			m.addLog(confirmStyle.Render(fmt.Sprintf("  ✗ Detach %s: %v", msg.name, msg.err)))
			return m, tea.Batch(fetchSessionsCmd, clearStatusAfter(3*time.Second))
		}
		m.status = fmt.Sprintf("Detached %s", msg.name)
		m.addLog(statusStyle.Render(fmt.Sprintf("  ✓ Detached: %s", msg.name)))
		return m, tea.Batch(fetchSessionsCmd, clearStatusAfter(3*time.Second))

	case renameSessionMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Rename failed: %v", msg.err)
			m.addLog(confirmStyle.Render(fmt.Sprintf("  ✗ Rename %s: %v", msg.oldName, msg.err)))
		} else {
			m.status = fmt.Sprintf("Renamed %s → %s", msg.oldName, msg.newName)
			m.addLog(statusStyle.Render(fmt.Sprintf("  ✓ Renamed: %s → %s", msg.oldName, msg.newName)))
		}
		return m, tea.Batch(fetchSessionsCmd, clearStatusAfter(3*time.Second))

	case createSessionMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("Create failed: %v", msg.err)
			m.addLog(confirmStyle.Render(fmt.Sprintf("  ✗ Create %s: %v", msg.name, msg.err)))
		} else {
			m.status = fmt.Sprintf("Created %s", msg.name)
			m.addLog(statusStyle.Render(fmt.Sprintf("  ✓ Created session: %s", msg.name)))
		}
		return m, tea.Batch(fetchSessionsCmd, clearStatusAfter(3*time.Second))

	case statusClearMsg:
		m.status = ""

	case refreshSpinnerMsg:
		if !m.refreshing {
			return m, nil
		}
		m.refreshFrame = (m.refreshFrame + 1) % len(refreshSpinnerFrames)
		return m, refreshSpinnerCmd()

	case autoRefreshMsg:
		return m, tea.Batch(fetchSessionsCmd, autoRefreshCmd())

	case tea.KeyPressMsg:
		if m.state == stateFilter {
			return m.handleFilterKey(msg)
		}
		if m.state == stateNewSession {
			return m.handleNewSessionKey(msg)
		}
		if m.state == stateRenameSession {
			return m.handleRenameSessionKey(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) previewCmd() tea.Cmd {
	if m.isSimplified() {
		return nil
	}
	visible := m.visibleSessions()
	if m.cursor >= len(visible) {
		return nil
	}
	return fetchPreviewCmd(visible[m.cursor].Name, m.mainContentHeight(1))
}

func (m *Model) killTargets() []string {
	return m.actionTargets()
}

func (m *Model) detachTargets() []string {
	return m.actionTargets()
}

func (m *Model) actionTargets() []string {
	if len(m.selected) > 0 {
		names := make([]string, 0, len(m.selected))
		for name := range m.selected {
			names = append(names, name)
		}
		return names
	}
	visible := m.visibleSessions()
	if m.cursor < len(visible) {
		return []string{visible[m.cursor].Name}
	}
	return nil
}

func (m *Model) toggleLayout() tea.Cmd {
	opts := NormalizeOptions(m.options)
	if opts.Mode == ModeSimplified {
		opts.Mode = ModeFull
	} else {
		opts.Mode = ModeSimplified
	}
	m.options = opts
	if m.isSimplified() {
		m.preview = ""
		return nil
	}
	return m.previewCmd()
}
