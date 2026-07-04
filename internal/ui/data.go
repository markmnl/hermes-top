package ui

import (
	"sort"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
	"github.com/markmnl/hermes-top/internal/poll"
)

// applyDelta folds a poll result into model state.
func (m *model) applyDelta(d poll.Delta) {
	if d.SessionsChanged {
		m.setSessions(d.Sessions)
	}

	// Collect new events from messages and lifecycle, then merge by time so the
	// events pane stays chronologically ordered.
	newEvents := make([]derive.Event, 0, len(d.NewMessages)+len(d.Lifecycle))
	for _, msg := range d.NewMessages {
		m.foldMessageAction(msg)
		if ev, ok := derive.MessageEvent(msg); ok {
			newEvents = append(newEvents, ev)
		}
	}
	newEvents = append(newEvents, d.Lifecycle...)
	sort.SliceStable(newEvents, func(i, j int) bool {
		return newEvents[i].Time.Before(newEvents[j].Time)
	})

	m.events = append(m.events, newEvents...)
	if len(m.events) > eventCap {
		m.events = m.events[len(m.events)-eventCap:]
	}
}

// setSessions replaces the session slice and index, preserving selection when
// possible and defaulting it to the top session otherwise.
func (m *model) setSessions(sessions []db.Session) {
	m.sessions = sessions
	m.byID = make(map[string]*db.Session, len(sessions))
	for i := range m.sessions {
		m.byID[m.sessions[i].ID] = &m.sessions[i]
	}
	if _, ok := m.byID[m.selected]; !ok {
		if len(m.sessions) > 0 {
			m.selected = m.sessions[0].ID
		} else {
			m.selected = ""
		}
	}
	// Keep the selected row visible within the list window.
	if idx := m.selectedIndex(); idx >= 0 {
		m.ensureSelectedVisible(idx)
	}
}

// foldMessageAction updates the per-session action timeline from one message.
func (m *model) foldMessageAction(msg db.Message) {
	switch {
	case msg.Role == "tool":
		callID := ""
		if msg.ToolCallID.Valid {
			callID = msg.ToolCallID.String
		}
		if a, ok := m.actionByCall[callID]; ok && callID != "" {
			derive.ApplyToolResult(a, msg)
			return
		}
		// Result with no matching call (outside backfill window): orphan.
		orphan := derive.OrphanAction(msg)
		m.appendAction(msg.SessionID, &orphan)

	case msg.ToolCalls.Valid && msg.ToolCalls.String != "":
		for _, a := range derive.ActionsFromAssistant(msg) {
			act := a // copy; take stable pointer
			m.appendAction(msg.SessionID, &act)
			if act.CallID != "" {
				m.actionByCall[act.CallID] = &act
			}
		}
	}
}

// appendAction adds an action to a session's timeline, trimming the oldest when
// the per-session cap is exceeded (and forgetting its call-id index entry).
func (m *model) appendAction(sessionID string, a *derive.Action) {
	list := m.actions[sessionID]
	list = append(list, a)
	if len(list) > actionCap {
		drop := list[0]
		list = list[1:]
		if drop.CallID != "" {
			if cur, ok := m.actionByCall[drop.CallID]; ok && cur == drop {
				delete(m.actionByCall, drop.CallID)
			}
		}
	}
	m.actions[sessionID] = list
}
