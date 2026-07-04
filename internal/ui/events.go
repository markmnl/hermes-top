package ui

import (
	"strings"

	"github.com/markmnl/hermes-top/internal/derive"
)

func (m *model) eventsTitle() string {
	if m.filterText != "" {
		shown, total := m.eventCounts()
		return "events · filter \"" + m.filterText + "\" (" + itoa(shown) + "/" + itoa(total) + ")"
	}
	return "events · " + itoa(len(m.events))
}

func (m *model) eventCounts() (shown, total int) {
	total = len(m.events)
	for i := range m.events {
		if m.events[i].Matches(m.filterText) {
			shown++
		}
	}
	return
}

// eventsContent renders the (optionally filtered) event stream, one line each.
func (m *model) eventsContent() string {
	st := m.st
	if len(m.events) == 0 {
		return st.dim.Render("no events yet")
	}
	width := m.eventsVP.Width
	var b strings.Builder
	first := true
	for i := range m.events {
		e := &m.events[i]
		if !e.Matches(m.filterText) {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString(m.eventLine(e, width))
	}
	if first { // nothing matched
		return st.dim.Render("no events match filter")
	}
	return b.String()
}

func (m *model) eventLine(e *derive.Event, width int) string {
	st := m.st
	ts := st.dim.Render(e.Time.Format("15:04:05"))
	sid := st.dim.Render(padRight(e.Short, 8))
	role := m.roleBadge(e)

	summaryStyle := st.value
	switch e.Kind {
	case derive.EvToolResult:
		if e.Err {
			summaryStyle = st.errText
		} else {
			summaryStyle = st.ok
		}
	case derive.EvToolCall:
		summaryStyle = st.accent
	case derive.EvSessionStart, derive.EvSessionEnd:
		summaryStyle = st.roleSession
	case derive.EvAssistant:
		summaryStyle = st.value
	case derive.EvUser:
		summaryStyle = st.value
	case derive.EvSystem:
		summaryStyle = st.dim
	}

	prefix := ts + " " + sid + " " + role + " "
	avail := width - ansiWidth(prefix)
	if avail < 1 {
		avail = 1
	}
	summary := summaryStyle.Render(clip(e.Summary, avail))
	return padANSILine(prefix+summary, width)
}

func (m *model) roleBadge(e *derive.Event) string {
	label := e.Role
	switch len(label) {
	case 0:
		label = "?"
	}
	return m.st.roleStyle(e.Role).Render(padRight("["+label+"]", 11))
}
