package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Jevs21/cctop/internal/session"
)

// Mode represents the current TUI interaction mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeFilter
	ModeDetail
)

// SortField represents the available sort orderings.
type SortField int

const (
	SortByState    SortField = iota // active > waiting > idle
	SortByDuration                  // longest first
	SortByProject                   // alphabetical
)

// StateFilter represents which session states to display.
type StateFilter int

const (
	FilterAll     StateFilter = iota
	FilterActive
	FilterWaiting
	FilterIdle
)

const (
	// minTerminalWidth is the minimum terminal width before showing a "too narrow" message.
	minTerminalWidth = 60

	// branchColThreshold is the remaining width above which the BRANCH column appears.
	branchColThreshold = 80

	// projectWidthPercent is the percentage of remaining width allocated to the PROJECT column.
	projectWidthPercent = 35

	// minProjectColWidth is the minimum width for the PROJECT column.
	minProjectColWidth = 10

	// minTopicColWidth is the minimum width for the TOPIC column.
	minTopicColWidth = 15

	// fixedColumnSpacing is the total padding/spacing used by the fixed-width columns.
	fixedColumnSpacing = 6

	// uiVerticalOverhead is the number of lines consumed by header, blank, column header,
	// help line, and margins.
	uiVerticalOverhead = 6

	// refreshInterval is the time between session discovery cycles.
	refreshInterval = 2 * time.Second
)

// columnWidths holds the computed widths for each table column.
type columnWidths struct {
	state   int
	source  int
	project int
	topic   int
	branch  int
	dur     int
}

// headerPart pairs the plain text of a header element with its styled rendering.
type headerPart struct {
	plain  string
	styled string
}

// model holds the Bubbletea application state.
type model struct {
	sessions     []session.Session
	cursor       int
	mode         Mode
	filterInput  textinput.Model
	filterText   string
	stateFilter  StateFilter
	sortField    SortField
	windowWidth  int
	windowHeight int
	onceMode     bool
	debugMode    bool
	firstRefresh bool
}

// sessionsRefreshedMsg carries newly discovered sessions from a background refresh.
type sessionsRefreshedMsg struct {
	sessions []session.Session
}

// tickMsg triggers a periodic session refresh.
type tickMsg time.Time

// Run starts the Bubbletea TUI. onceMode prints once and exits; debugMode
// enables timing diagnostics.
func Run(onceMode bool, debugMode bool) error {
	// --once mode: bypass Bubbletea entirely, print to stdout directly
	if onceMode {
		return runOnce(debugMode)
	}

	initialModel := newModel(false, debugMode)
	program := tea.NewProgram(initialModel, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// runOnce discovers sessions and prints the table once to stdout without
// requiring a TTY or alternate screen.
func runOnce(debugMode bool) error {
	var debugStart time.Time
	if debugMode {
		debugStart = time.Now()
	}

	sessions := session.DiscoverAll()

	if debugMode {
		fmt.Fprintf(os.Stderr, "[debug] discovery: %dms, sessions: %d\n",
			time.Since(debugStart).Milliseconds(), len(sessions))
	}

	m := newModel(true, debugMode)
	m.sessions = sessions
	m.firstRefresh = true
	m.windowWidth = 120
	m.windowHeight = 40

	fmt.Println(m.renderNormal())
	return nil
}

func newModel(onceMode bool, debugMode bool) model {
	filterInput := textinput.New()
	filterInput.Placeholder = "filter sessions..."
	filterInput.CharLimit = 100
	filterInput.Width = 40

	return model{
		onceMode:     onceMode,
		debugMode:    debugMode,
		filterInput:  filterInput,
		sortField:    SortByState,
		stateFilter:  FilterAll,
		firstRefresh: false,
	}
}

// Init returns the initial commands: an immediate refresh and a tick timer.
func (m model) Init() tea.Cmd {
	return tea.Batch(refreshSessionsCmd(), tickCmd())
}

// refreshSessionsCmd runs session discovery in a background goroutine.
func refreshSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		sessions := session.DiscoverAll()
		return sessionsRefreshedMsg{sessions: sessions}
	}
}

// tickCmd schedules the next refresh after the interval.
func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update processes messages and returns the updated model and any commands.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		return m, nil

	case sessionsRefreshedMsg:
		m.sessions = msg.sessions
		m.firstRefresh = true

		// In --once mode, quit after the first refresh
		if m.onceMode {
			return m, tea.Quit
		}
		return m, tickCmd()

	case tickMsg:
		return m, refreshSessionsCmd()

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.mode {
		case ModeNormal:
			return m.updateNormal(msg)
		case ModeFilter:
			return m.updateFilter(msg)
		case ModeDetail:
			return m.updateDetail(msg)
		}
	}

	return m, nil
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredSessions()

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(filtered)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		if len(filtered) > 0 {
			m.mode = ModeDetail
		}
	case "/":
		m.mode = ModeFilter
		m.filterInput.SetValue(m.filterText)
		cmd := m.filterInput.Focus()
		return m, cmd
	case "f":
		m.stateFilter = (m.stateFilter + 1) % 4
		m.cursor = 0
	case "s":
		m.sortField = (m.sortField + 1) % 3
	}

	return m, nil
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filterText = m.filterInput.Value()
		m.filterInput.Blur()
		m.mode = ModeNormal
		m.cursor = 0
		return m, nil
	case "esc":
		m.filterInput.Blur()
		m.mode = ModeNormal
		return m, nil
	default:
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		return m, cmd
	}
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = ModeNormal
	}
	return m, nil
}

// filteredSessions returns sessions matching the current filter and state filter,
// sorted by the current sort field.
func (m model) filteredSessions() []session.Session {
	if m.sessions == nil {
		return nil
	}

	var filtered []session.Session
	for _, s := range m.sessions {
		// Apply state filter
		switch m.stateFilter {
		case FilterActive:
			if s.State != session.StateActive {
				continue
			}
		case FilterWaiting:
			if s.State != session.StateWaiting {
				continue
			}
		case FilterIdle:
			if s.State != session.StateIdle {
				continue
			}
		}

		// Apply text filter
		if m.filterText != "" {
			lowerFilter := strings.ToLower(m.filterText)
			matchesProject := strings.Contains(strings.ToLower(s.Project), lowerFilter)
			matchesTopic := strings.Contains(strings.ToLower(s.Topic), lowerFilter)
			matchesBranch := strings.Contains(strings.ToLower(s.Branch), lowerFilter)
			if !matchesProject && !matchesTopic && !matchesBranch {
				continue
			}
		}

		filtered = append(filtered, s)
	}

	// Sort
	sort.SliceStable(filtered, func(i, j int) bool {
		switch m.sortField {
		case SortByDuration:
			return filtered[i].Duration > filtered[j].Duration
		case SortByProject:
			return filtered[i].Project < filtered[j].Project
		default: // SortByState
			return filtered[i].State.Priority() < filtered[j].State.Priority()
		}
	})

	return filtered
}

// View renders the full TUI output.
func (m model) View() string {
	if !m.firstRefresh {
		return "  Loading sessions..."
	}

	switch m.mode {
	case ModeFilter:
		return m.renderFilter()
	case ModeDetail:
		return m.renderDetail()
	default:
		return m.renderNormal()
	}
}

// computeColumnWidths calculates column widths based on the terminal width.
func computeColumnWidths(terminalWidth int) columnWidths {
	cw := columnWidths{
		state:  3,
		source: 7,
		dur:    7,
	}

	remaining := terminalWidth - cw.state - cw.source - cw.dur - fixedColumnSpacing
	if remaining > branchColThreshold {
		cw.branch = 16
		remaining -= cw.branch + 2
	}

	cw.project = remaining * projectWidthPercent / 100
	cw.topic = remaining - cw.project

	if cw.project < minProjectColWidth {
		cw.project = minProjectColWidth
	}
	if cw.topic < minTopicColWidth {
		cw.topic = minTopicColWidth
	}

	return cw
}

// stateIconStyled renders a state icon with the appropriate style and column width.
func stateIconStyled(state session.State, colWidth int) string {
	switch state {
	case session.StateActive:
		return activeStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u25C9"))
	case session.StateWaiting:
		return waitingStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u25CF"))
	default:
		return idleStyle.Render(fmt.Sprintf("%-*s", colWidth, "\u25CB"))
	}
}

// stateDisplayWithIcon returns a styled "icon label" string for the detail view.
func stateDisplayWithIcon(state session.State) string {
	switch state {
	case session.StateActive:
		return activeStyle.Render("\u25C9 active")
	case session.StateWaiting:
		return waitingStyle.Render("\u25CF waiting")
	default:
		return idleStyle.Render("\u25CB idle")
	}
}

// renderNormal renders the main session list view.
func (m model) renderNormal() string {
	var b strings.Builder
	width := m.windowWidth
	if width == 0 {
		width = 80
	}
	height := m.windowHeight
	if height == 0 {
		height = 24
	}

	// Minimum width check
	if width < minTerminalWidth {
		return headerStyle.Width(width).Render(" cctop") + "\n\n  Terminal too narrow (need 60+ cols)"
	}

	filtered := m.filteredSessions()

	// Count states from all sessions (not filtered)
	activeCount, waitingCount, idleCount := m.countStates()
	totalCount := len(m.sessions)

	// ---- Header ----
	b.WriteString(m.renderHeader(width, activeCount, waitingCount, idleCount, totalCount))
	b.WriteString("\n")

	// ---- Empty state ----
	if totalCount == 0 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  No active Claude sessions"))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  Start a session with "))
		b.WriteString(normalTextStyle.Bold(true).Render("claude"))
		b.WriteString(dimStyle.Render(" in a terminal or VSCode"))
		b.WriteString("\n")
		return b.String()
	}

	// ---- Column widths ----
	cw := computeColumnWidths(width)

	b.WriteString("\n")

	// ---- Column headers ----
	b.WriteString(" ")
	b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %-*s", cw.state, "ST")))
	b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %-*s", cw.source, "SRC")))
	b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %-*s", cw.project, "PROJECT")))
	b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %-*s", cw.topic, "TOPIC")))
	if cw.branch > 0 {
		b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %-*s", cw.branch, "BRANCH")))
	}
	b.WriteString(columnHeaderStyle.Render(fmt.Sprintf(" %*s", cw.dur, "DUR")))
	b.WriteString("\n")

	// ---- Rows ----
	maxRows := height - uiVerticalOverhead
	if maxRows < 1 {
		maxRows = 1
	}

	for i, s := range filtered {
		if i >= maxRows {
			remaining := len(filtered) - maxRows
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more sessions", remaining)))
			b.WriteString("\n")
			break
		}

		isSelected := i == m.cursor
		b.WriteString(m.renderRow(s, isSelected, cw))
		b.WriteString("\n")
	}

	// ---- Filter indicator ----
	if m.filterText != "" || m.stateFilter != FilterAll {
		b.WriteString("\n")
		var filterParts []string
		if m.stateFilter != FilterAll {
			filterParts = append(filterParts, "state:"+stateFilterName(m.stateFilter))
		}
		if m.filterText != "" {
			filterParts = append(filterParts, "text:\""+m.filterText+"\"")
		}
		b.WriteString(helpStyle.Render("  filter: "+strings.Join(filterParts, " ")+" | "+fmt.Sprintf("%d/%d shown", len(filtered), totalCount)))
		b.WriteString("\n")
	}

	// ---- Help line ----
	b.WriteString("\n")
	sortName := sortFieldName(m.sortField)
	b.WriteString(helpStyle.Render(fmt.Sprintf("  j/k: navigate  enter: detail  /: filter  f: state(%s)  s: sort(%s)  q: quit", stateFilterName(m.stateFilter), sortName)))

	return b.String()
}

// renderHeader builds the header bar with title and state counts.
func (m model) renderHeader(width int, activeCount int, waitingCount int, idleCount int, totalCount int) string {
	titleText := " cctop -- Claude Session Monitor"

	var parts []headerPart
	if activeCount > 0 {
		text := fmt.Sprintf("%d active", activeCount)
		parts = append(parts, headerPart{text, activeStyle.Render(text)})
	}
	if waitingCount > 0 {
		text := fmt.Sprintf("%d waiting", waitingCount)
		parts = append(parts, headerPart{text, waitingStyle.Render(text)})
	}
	if idleCount > 0 {
		text := fmt.Sprintf("%d idle", idleCount)
		parts = append(parts, headerPart{text, dimStyle.Render(text)})
	}
	quitText := "[q]uit"
	parts = append(parts, headerPart{quitText, helpStyle.Render(quitText)})

	// Build styled right side and calculate plain-text width
	var styledParts []string
	rightPlainLen := 0
	for i, part := range parts {
		styledParts = append(styledParts, part.styled)
		rightPlainLen += len(part.plain)
		if i < len(parts)-1 {
			rightPlainLen += 2 // "  " separator
		}
	}
	rightText := strings.Join(styledParts, "  ")

	// Pad the middle
	middlePad := width - len(titleText) - rightPlainLen
	if middlePad < 1 {
		middlePad = 1
	}

	return headerStyle.Width(width).Render(titleText + strings.Repeat(" ", middlePad) + rightText)
}

// renderRow renders a single session row.
func (m model) renderRow(s session.Session, isSelected bool, cw columnWidths) string {
	var b strings.Builder

	// Cursor indicator
	if isSelected {
		b.WriteString(selectedStyle.Render(" >"))
	} else {
		b.WriteString("  ")
	}

	// State icon
	b.WriteString(" ")
	b.WriteString(stateIconStyled(s.State, cw.state))

	// Source
	var sourceStyled string
	if s.Source.Type == "CLI" {
		sourceStyled = cliSourceStyle.Render(fmt.Sprintf("%-*s", cw.source, s.Source.Type))
	} else {
		sourceStyled = ideSourceStyle.Render(fmt.Sprintf("%-*s", cw.source, s.Source.Type))
	}
	b.WriteString(" ")
	b.WriteString(sourceStyled)

	// Apply dim style to the row if idle
	textStyleFn := func(text string) string { return text }
	if s.State == session.StateIdle && !isSelected {
		textStyleFn = func(text string) string { return dimStyle.Render(text) }
	}

	// Project
	projectDisplay := truncateString(s.Project, cw.project)
	b.WriteString(" ")
	b.WriteString(textStyleFn(fmt.Sprintf("%-*s", cw.project, projectDisplay)))

	// Topic
	topicDisplay := truncateString(s.Topic, cw.topic)
	b.WriteString(" ")
	b.WriteString(textStyleFn(fmt.Sprintf("%-*s", cw.topic, topicDisplay)))

	// Branch (optional)
	if cw.branch > 0 {
		branchDisplay := truncateString(s.Branch, cw.branch)
		b.WriteString(" ")
		b.WriteString(textStyleFn(fmt.Sprintf("%-*s", cw.branch, branchDisplay)))
	}

	// Duration
	durationStr := session.FormatDuration(s.Duration)
	b.WriteString(" ")
	b.WriteString(textStyleFn(fmt.Sprintf("%*s", cw.dur, durationStr)))

	return b.String()
}

// renderFilter renders the filter input view.
func (m model) renderFilter() string {
	var b strings.Builder
	width := m.windowWidth
	if width == 0 {
		width = 80
	}

	b.WriteString(headerStyle.Width(width).Render(" cctop -- Claude Session Monitor"))
	b.WriteString("\n\n")
	b.WriteString(filterPromptStyle.Render("  Filter: "))
	b.WriteString(m.filterInput.View())
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("  enter: apply  esc: cancel"))

	return b.String()
}

// renderDetail renders the expanded detail view for the selected session.
func (m model) renderDetail() string {
	var b strings.Builder
	width := m.windowWidth
	if width == 0 {
		width = 80
	}

	b.WriteString(headerStyle.Width(width).Render(" cctop -- Session Detail"))
	b.WriteString("\n\n")

	filtered := m.filteredSessions()
	if m.cursor >= len(filtered) {
		b.WriteString("  No session selected\n")
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  esc: back"))
		return b.String()
	}

	s := filtered[m.cursor]

	details := []struct {
		label string
		value string
	}{
		{"State", stateDisplayWithIcon(s.State)},
		{"Source", s.Source.String()},
		{"PID", fmt.Sprintf("%d", s.PID)},
		{"Project", s.Project},
		{"CWD", s.CWD},
		{"Branch", s.Branch},
		{"Duration", session.FormatDuration(s.Duration)},
		{"Messages", fmt.Sprintf("~%d", s.Messages)},
		{"Topic", s.Topic},
	}

	for _, detail := range details {
		if detail.value == "" || detail.value == "0" || detail.value == "~0" {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s  %s\n", detailLabelStyle.Render(fmt.Sprintf("%-10s", detail.label)), detail.value))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("  esc: back  q: quit"))

	return b.String()
}

// countStates returns the count of active, waiting, and idle sessions.
func (m model) countStates() (int, int, int) {
	var activeCount, waitingCount, idleCount int
	for _, s := range m.sessions {
		switch s.State {
		case session.StateActive:
			activeCount++
		case session.StateWaiting:
			waitingCount++
		case session.StateIdle:
			idleCount++
		}
	}
	return activeCount, waitingCount, idleCount
}

// truncateString truncates a string to maxLen, appending an ellipsis if needed.
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "\u2026"
	}
	return s[:maxLen-1] + "\u2026"
}

// stateFilterName returns the display name for the current state filter.
func stateFilterName(filter StateFilter) string {
	switch filter {
	case FilterActive:
		return "active"
	case FilterWaiting:
		return "waiting"
	case FilterIdle:
		return "idle"
	default:
		return "all"
	}
}

// sortFieldName returns the display name for the current sort field.
func sortFieldName(field SortField) string {
	switch field {
	case SortByDuration:
		return "duration"
	case SortByProject:
		return "project"
	default:
		return "state"
	}
}
