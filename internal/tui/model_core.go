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
	"github.com/adibhanna/tsm/internal/mux"
	"github.com/adibhanna/tsm/internal/project"
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
	stateMuxOpen
	stateProjectPick
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

type muxInfoMsg struct {
	terminal   string
	backend    string
	workspaces []string
}

// projectWorktreeItem represents a worktree in the project picker.
type projectWorktreeItem struct {
	Project string // project config name
	Branch  string // branch name (display)
	TabName string // formatted tab name for opening
}

type projectPickMsg struct {
	items []projectWorktreeItem
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

func fetchProjectWorktreesCmd() tea.Msg {
	names, _ := project.List()
	var items []projectWorktreeItem
	for _, name := range names {
		cfg, err := project.Load(name)
		if err != nil {
			continue
		}
		worktrees, _ := cfg.ResolveWorktrees()
		for _, wt := range worktrees {
			if wt.Bare {
				continue
			}
			items = append(items, projectWorktreeItem{
				Project: name,
				Branch:  wt.Branch,
				TabName: name + ":" + project.SanitizeBranch(wt.Branch),
			})
		}
	}
	return projectPickMsg{items: items}
}

func fetchMuxInfoCmd() tea.Msg {
	term := mux.DetectTerminal()
	workspaces, _ := mux.ListManifests()
	return muxInfoMsg{
		terminal:   term.Name,
		backend:    term.Backend,
		workspaces: workspaces,
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
	options        Options
	normalizedOpts Options // cached normalized options

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

	// Mux workspace
	muxTerminal     string   // detected terminal name
	muxBackend      string   // detected backend name
	workspaceNames  []string // available workspace manifests
	workspaceCursor int
	muxOpenTarget   string // workspace to open after quit

	// Project worktree picker
	projectWorktrees      []projectWorktreeItem
	projectCursor         int
	projectPickProject    string // project name to open after quit
	projectPickBranch     string // branch to open (empty = all)

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
	normed := NormalizeOptions(Options{})
	return Model{
		options:           normed,
		normalizedOpts:    normed,
		selected:          make(map[string]bool),
		sortAsc:           true,
		visibleCacheDirty: true,
		allMetricsDirty:   true,
	}
}

func NewModel(opts ...Options) Model {
	m := initialModel()
	if len(opts) > 0 {
		normed := NormalizeOptions(opts[0])
		m.options = normed
		m.normalizedOpts = normed
	}
	return m
}

func (m Model) AttachTarget() string {
	return m.attachTarget
}

func (m Model) MuxOpenTarget() string {
	return m.muxOpenTarget
}

func (m Model) ProjectPickProject() string {
	return m.projectPickProject
}

func (m Model) ProjectPickBranch() string {
	return m.projectPickBranch
}

func (m Model) Options() Options {
	return m.normalizedOpts
}

func (m Model) isSimplified() bool {
	return m.normalizedOpts.Mode == ModeSimplified
}

func (m Model) keymap() Keymap {
	return m.normalizedOpts.Keymap
}

func (m Model) inlineFilterEnabled() bool {
	return m.keymap() == KeymapPalette
}

func (m Model) showHelp() bool {
	return m.normalizedOpts.ShowHelp
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

// mergeProcessInfo copies process-info fields from info into dst,
// preserving session metadata (Name, PID, Clients, etc.).
func mergeProcessInfo(dst *Session, info engine.ProcessInfo) {
	dst.Memory = info.Memory
	dst.Uptime = info.Uptime
	dst.AgentKind = info.AgentKind
	dst.AgentState = info.AgentState
	dst.AgentSummary = info.AgentSummary
	dst.AgentUpdated = info.AgentUpdated
	dst.AgentModel = info.AgentModel
	dst.AgentVersion = info.AgentVersion
	dst.AgentPrompt = info.AgentPrompt
	dst.AgentPlan = info.AgentPlan
	dst.AgentApproval = info.AgentApproval
	dst.AgentSandbox = info.AgentSandbox
	dst.AgentBranch = info.AgentBranch
	dst.AgentGitSHA = info.AgentGitSHA
	dst.AgentGitOrigin = info.AgentGitOrigin
	dst.AgentName = info.AgentName
	dst.AgentRole = info.AgentRole
	dst.AgentMemory = info.AgentMemory
	dst.AgentSessionID = info.AgentSessionID
	dst.AgentSubagent = info.AgentSubagent
	dst.AgentInput = info.AgentInput
	dst.AgentOutput = info.AgentOutput
	dst.AgentCached = info.AgentCached
	dst.AgentTotal = info.AgentTotal
	dst.AgentContext = info.AgentContext
	dst.AgentCostUSD = info.AgentCostUSD
	dst.AgentDurationMS = info.AgentDurationMS
	dst.AgentAPIMS = info.AgentAPIMS
	dst.AgentLinesAdded = info.AgentLinesAdded
	dst.AgentLinesRemoved = info.AgentLinesRemoved
	dst.AgentOutputStyle = info.AgentOutputStyle
	dst.AgentProjectDir = info.AgentProjectDir
	dst.AgentWorktreePath = info.AgentWorktreePath
}

// copyProcessInfoFromSession copies process-info fields from src into dst,
// preserving session metadata (Name, PID, Clients, etc.).
func copyProcessInfoFromSession(dst *Session, src Session) {
	dst.Memory = src.Memory
	dst.Uptime = src.Uptime
	dst.AgentKind = src.AgentKind
	dst.AgentState = src.AgentState
	dst.AgentSummary = src.AgentSummary
	dst.AgentUpdated = src.AgentUpdated
	dst.AgentModel = src.AgentModel
	dst.AgentVersion = src.AgentVersion
	dst.AgentPrompt = src.AgentPrompt
	dst.AgentPlan = src.AgentPlan
	dst.AgentApproval = src.AgentApproval
	dst.AgentSandbox = src.AgentSandbox
	dst.AgentBranch = src.AgentBranch
	dst.AgentGitSHA = src.AgentGitSHA
	dst.AgentGitOrigin = src.AgentGitOrigin
	dst.AgentName = src.AgentName
	dst.AgentRole = src.AgentRole
	dst.AgentMemory = src.AgentMemory
	dst.AgentSessionID = src.AgentSessionID
	dst.AgentSubagent = src.AgentSubagent
	dst.AgentInput = src.AgentInput
	dst.AgentOutput = src.AgentOutput
	dst.AgentCached = src.AgentCached
	dst.AgentTotal = src.AgentTotal
	dst.AgentContext = src.AgentContext
	dst.AgentCostUSD = src.AgentCostUSD
	dst.AgentDurationMS = src.AgentDurationMS
	dst.AgentAPIMS = src.AgentAPIMS
	dst.AgentLinesAdded = src.AgentLinesAdded
	dst.AgentLinesRemoved = src.AgentLinesRemoved
	dst.AgentOutputStyle = src.AgentOutputStyle
	dst.AgentProjectDir = src.AgentProjectDir
	dst.AgentWorktreePath = src.AgentWorktreePath
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
	return tea.Batch(fetchSessionsCmd, fetchMuxInfoCmd, autoRefreshCmd())
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
				copyProcessInfoFromSession(&msg.sessions[i], prev)
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
				mergeProcessInfo(&m.sessions[i], info)
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

	case muxInfoMsg:
		m.muxTerminal = msg.terminal
		m.muxBackend = msg.backend
		m.workspaceNames = msg.workspaces

	case projectPickMsg:
		m.projectWorktrees = msg.items
		if len(msg.items) > 0 {
			m.state = stateProjectPick
			m.projectCursor = 0
			m.status = ""
		} else {
			m.status = "No projects configured"
			return m, clearStatusAfter(2 * time.Second)
		}

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
		if m.state == stateMuxOpen {
			return m.handleMuxOpenKey(msg)
		}
		if m.state == stateProjectPick {
			return m.handleProjectPickKey(msg)
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
	opts := m.normalizedOpts
	if opts.Mode == ModeSimplified {
		opts.Mode = ModeFull
	} else {
		opts.Mode = ModeSimplified
	}
	m.options = opts
	m.normalizedOpts = NormalizeOptions(opts)
	if m.isSimplified() {
		m.preview = ""
		return nil
	}
	return m.previewCmd()
}
