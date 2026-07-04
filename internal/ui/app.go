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
		ctx:            ctx,
		poller:         p,
		interval:       interval,
		st:             newStyles(),
		keys:           newKeyMap(),
		help:           help.New(),
		filter:         ti,
		byID:           make(map[string]*db.Session),
		actions:        make(map[string][]*derive.Action),
		actionByCall:   make(map[string]*derive.Action),
		timelineFollow: true,
		eventsFollow:   true,
		now:            time.Now(),
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
	}
	return m, nil
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
		m.timelineVP.ScrollUp(1)
		m.timelineFollow = m.timelineVP.AtBottom()
	case paneEvents:
		m.eventsVP.ScrollUp(1)
		m.eventsFollow = m.eventsVP.AtBottom()
	}
}

func (m *model) moveDown() {
	switch m.focus {
	case paneSessions:
		m.selectDelta(1)
	case paneTimeline:
		m.timelineVP.ScrollDown(1)
		m.timelineFollow = m.timelineVP.AtBottom()
	case paneEvents:
		m.eventsVP.ScrollDown(1)
		m.eventsFollow = m.eventsVP.AtBottom()
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
		m.timelineVP.GotoTop()
		m.timelineFollow = false
	case paneEvents:
		m.eventsVP.GotoTop()
		m.eventsFollow = false
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
		m.timelineVP.GotoBottom()
		m.timelineFollow = true
	case paneEvents:
		m.eventsVP.GotoBottom()
		m.eventsFollow = true
	}
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
