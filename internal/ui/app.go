// Package ui implements the read-only Bubble Tea dashboard. The root model owns
// all data; panes are rendered by helper methods that read that data. Focus
// routing and an input-mode gate (normal vs filter) live here.
package ui

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
	"github.com/markmnl/hermes-top/internal/poll"
)

const (
	eventCap    = 5000 // max retained events (ring)
	actionCap   = 500  // max retained actions per session
	minWidth    = 60
	minHeight   = 16
	listWidth   = 34 // left pane target width
	summaryRows = 9  // top-right summary height (content lines)
)

type paneID int

const (
	paneSessions paneID = iota
	paneTimeline
	paneEvents
	paneCount
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeFilter
)

type model struct {
	ctx      context.Context
	poller   *poll.Poller
	interval time.Duration
	st       styles
	keys     keyMap
	help     help.Model
	filter   textinput.Model

	// data
	sessions     []db.Session // display order (active first)
	byID         map[string]*db.Session
	actions      map[string][]*derive.Action
	actionByCall map[string]*derive.Action
	events       []derive.Event

	// ui state
	focus      paneID
	mode       inputMode
	selected   string // selected session id (stable across reorder)
	filterText string // applied filter

	timelineVP     viewport.Model
	eventsVP       viewport.Model
	timelineFollow bool
	eventsFollow   bool
	listOffset     int

	// entry cursors for the actions/events panes (index into the current
	// action list / filtered event list; -1 when empty)
	timelineCursor int
	eventsCursor   int
	// expansion state keyed by stable Seq; assigned as rows are recorded
	expandedActions map[int64]bool
	expandedEvents  map[int64]bool
	actionSeq       int64
	eventSeq        int64

	width, height int
	ready         bool
	frame         int
	lastPollAt    time.Time
	pollErr       error
	now           time.Time
	dim           layoutDims
}

// layoutDims holds computed outer box dimensions for the current window size.
type layoutDims struct {
	leftW     int // sessions pane outer width
	rightW    int // right column outer width
	topH      int // top region outer height (sessions | summary+timeline)
	summaryH  int // summary box outer height
	timelineH int // timeline box outer height
	eventsH   int // events box outer height
}

// Run starts the dashboard against the given read-only DB.
func Run(ctx context.Context, database *db.DB, interval time.Duration) error {
	p := poll.New(database)

	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter events…"
	ti.CharLimit = 128

	m := &model{
		ctx:             ctx,
		poller:          p,
		interval:        interval,
		st:              newStyles(),
		keys:            newKeyMap(),
		help:            help.New(),
		filter:          ti,
		byID:            make(map[string]*db.Session),
		actions:         make(map[string][]*derive.Action),
		actionByCall:    make(map[string]*derive.Action),
		expandedActions: make(map[int64]bool),
		expandedEvents:  make(map[int64]bool),
		timelineFollow:  true,
		eventsFollow:    true,
		timelineCursor:  -1,
		eventsCursor:    -1,
		now:             time.Now(),
	}

	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := prog.Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return pollCmd(m.ctx, m.poller)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.rebuildViewports()
		return m, nil

	case tickMsg:
		return m, pollCmd(m.ctx, m.poller)

	case pollDoneMsg:
		m.now = time.Now()
		m.frame++
		if msg.delta.Err != nil {
			m.pollErr = msg.delta.Err
		} else {
			m.pollErr = nil
			if msg.delta.HasChanges() {
				m.applyDelta(msg.delta)
			}
			m.lastPollAt = msg.delta.PolledAt
		}
		// Re-render viewport content each cycle so durations/pulse update.
		m.rebuildViewports()
		return m, scheduleTick(m.interval)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeFilter {
		return m.handleFilterKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Tab):
		m.focus = (m.focus + 1) % paneCount
		return m, nil
	case key.Matches(msg, m.keys.Filter):
		m.mode = modeFilter
		m.filter.SetValue(m.filterText)
		return m, m.filter.Focus()
	case key.Matches(msg, m.keys.Refresh):
		m.poller.ForceNext()
		return m, pollCmd(m.ctx, m.poller)
	case key.Matches(msg, m.keys.Up):
		m.moveUp()
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveDown()
		return m, nil
	case key.Matches(msg, m.keys.Top):
		m.gotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.gotoBottom()
		return m, nil
	case key.Matches(msg, m.keys.Expand):
		m.toggleExpand(false, false)
		return m, nil
	case key.Matches(msg, m.keys.Collapse):
		m.toggleExpand(true, false)
		return m, nil
	}
	return m, nil
}

// currentActions returns the selected session's action list.
func (m *model) currentActions() []*derive.Action {
	if m.selected == "" {
		return nil
	}
	return m.actions[m.selected]
}

// visibleEvents returns the events matching the current filter, in order.
func (m *model) visibleEvents() []*derive.Event {
	out := make([]*derive.Event, 0, len(m.events))
	for i := range m.events {
		if m.events[i].Matches(m.filterText) {
			out = append(out, &m.events[i])
		}
	}
	return out
}

// visibleEventCount counts filter-matching events without allocating.
func (m *model) visibleEventCount() int {
	if m.filterText == "" {
		return len(m.events)
	}
	n := 0
	for i := range m.events {
		if m.events[i].Matches(m.filterText) {
			n++
		}
	}
	return n
}

// syncCursors pins following cursors to the last entry and clamps both cursors
// to their current list lengths. Called from rebuildViewports.
func (m *model) syncCursors() {
	na := len(m.currentActions())
	if m.timelineFollow {
		m.timelineCursor = na - 1
	}
	m.timelineCursor = clampIdx(m.timelineCursor, na)

	ne := m.visibleEventCount()
	if m.eventsFollow {
		m.eventsCursor = ne - 1
	}
	m.eventsCursor = clampIdx(m.eventsCursor, ne)
}

func (m *model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Apply):
		m.filterText = m.filter.Value()
		m.filter.Blur()
		m.mode = modeNormal
		m.rebuildViewports()
		return m, nil
	case key.Matches(msg, m.keys.Cancel):
		m.filterText = ""
		m.filter.SetValue("")
		m.filter.Blur()
		m.mode = modeNormal
		m.rebuildViewports()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	// Live-apply the filter as the user types.
	m.filterText = m.filter.Value()
	m.rebuildViewports()
	return m, cmd
}

// --- selection & scrolling routed by focus ---

func (m *model) moveUp() {
	switch m.focus {
	case paneSessions:
		m.selectDelta(-1)
	case paneTimeline:
		m.moveTimelineCursor(-1)
	case paneEvents:
		m.moveEventsCursor(-1)
	}
}

func (m *model) moveDown() {
	switch m.focus {
	case paneSessions:
		m.selectDelta(1)
	case paneTimeline:
		m.moveTimelineCursor(1)
	case paneEvents:
		m.moveEventsCursor(1)
	}
}

func (m *model) gotoTop() {
	switch m.focus {
	case paneSessions:
		if len(m.sessions) > 0 {
			m.selected = m.sessions[0].ID
			m.listOffset = 0
			m.onSelectionChanged()
		}
	case paneTimeline:
		m.timelineCursor = 0
		m.timelineFollow = false
		m.rebuildViewports()
	case paneEvents:
		m.eventsCursor = 0
		m.eventsFollow = false
		m.rebuildViewports()
	}
}

func (m *model) gotoBottom() {
	switch m.focus {
	case paneSessions:
		if n := len(m.sessions); n > 0 {
			m.selected = m.sessions[n-1].ID
			m.onSelectionChanged()
		}
	case paneTimeline:
		m.timelineFollow = true // syncCursors will pin to last
		m.rebuildViewports()
	case paneEvents:
		m.eventsFollow = true
		m.rebuildViewports()
	}
}

// moveTimelineCursor moves the actions cursor by delta entries.
func (m *model) moveTimelineCursor(delta int) {
	n := len(m.currentActions())
	if n == 0 {
		return
	}
	cur := m.timelineCursor
	if cur < 0 {
		cur = n - 1
	}
	cur += delta
	cur = clampIdx(cur, n)
	m.timelineCursor = cur
	m.timelineFollow = cur == n-1
	m.rebuildViewports()
}

// moveEventsCursor moves the events cursor by delta entries (over the filtered
// list).
func (m *model) moveEventsCursor(delta int) {
	n := m.visibleEventCount()
	if n == 0 {
		return
	}
	cur := m.eventsCursor
	if cur < 0 {
		cur = n - 1
	}
	cur += delta
	cur = clampIdx(cur, n)
	m.eventsCursor = cur
	m.eventsFollow = cur == n-1
	m.rebuildViewports()
}

// toggleExpand expands/collapses the focused pane's cursor entry.
func (m *model) toggleExpand(force, expand bool) {
	switch m.focus {
	case paneTimeline:
		acts := m.currentActions()
		if m.timelineCursor < 0 || m.timelineCursor >= len(acts) {
			return
		}
		seq := acts[m.timelineCursor].Seq
		m.expandedActions[seq] = choose(force, expand, !m.expandedActions[seq])
	case paneEvents:
		vis := m.visibleEvents()
		if m.eventsCursor < 0 || m.eventsCursor >= len(vis) {
			return
		}
		seq := vis[m.eventsCursor].Seq
		m.expandedEvents[seq] = choose(force, expand, !m.expandedEvents[seq])
	default:
		return
	}
	m.rebuildViewports()
}

func choose(force, forced, toggled bool) bool {
	if force {
		return forced
	}
	return toggled
}

// clampIdx clamps i into [0, n-1], or -1 when n == 0.
func clampIdx(i, n int) int {
	if n <= 0 {
		return -1
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// selectDelta moves the session selection by delta positions in display order.
func (m *model) selectDelta(delta int) {
	if len(m.sessions) == 0 {
		return
	}
	idx := m.selectedIndex()
	if idx < 0 {
		idx = 0
	} else {
		idx += delta
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.sessions) {
		idx = len(m.sessions) - 1
	}
	m.selected = m.sessions[idx].ID
	m.ensureSelectedVisible(idx)
	m.onSelectionChanged()
}

func (m *model) selectedIndex() int {
	for i, s := range m.sessions {
		if s.ID == m.selected {
			return i
		}
	}
	return -1
}

// onSelectionChanged resets the timeline to follow and rebuilds its content.
func (m *model) onSelectionChanged() {
	m.timelineFollow = true
	m.rebuildViewports()
}

func (m *model) selectedSession() *db.Session {
	if s, ok := m.byID[m.selected]; ok {
		return s
	}
	return nil
}
