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

// eventsContent renders the (optionally filtered) event stream. Returns the
// content plus the cursor entry's line span for scroll-to-cursor.
func (m *model) eventsContent() (string, int, int) {
	st := m.st
	if len(m.events) == 0 {
		return st.dim.Render("no events yet"), -1, 0
	}
	width := m.eventsVP.Width
	focused := m.focus == paneEvents

	vis := m.visibleEvents()
	if len(vis) == 0 {
		return st.dim.Render("no events match filter"), -1, 0
	}

	blocks := make([]string, len(vis))
	counts := make([]int, len(vis))
	for i, e := range vis {
		isCursor := i == m.eventsCursor
		blocks[i] = m.eventBlock(e, width, focused && isCursor, isCursor)
		counts[i] = strings.Count(blocks[i], "\n") + 1
	}

	top, height := -1, 0
	if m.eventsCursor >= 0 && m.eventsCursor < len(vis) {
		t := 0
		for j := 0; j < m.eventsCursor; j++ {
			t += counts[j]
		}
		top, height = t, counts[m.eventsCursor]
	}
	return strings.Join(blocks, "\n"), top, height
}

// eventBlock renders one event line (with cursor gutter) and, when expanded,
// its pretty-printed payload.
func (m *model) eventBlock(e *derive.Event, width int, active, isCursor bool) string {
	inner := width - 2
	if inner < 1 {
		inner = 1
	}
	marker, gs := " ", m.st.dim
	if isCursor {
		marker = "▸"
		if active {
			gs = m.st.cursor
		}
	}
	line := gs.Render(marker+" ") + m.eventLine(e, inner)

	if !m.expandedEvents[e.Seq] || strings.TrimSpace(e.Raw) == "" {
		return line
	}
	return line + "\n" + indentBlock(prettyJSON(e.Raw, m.st, width-4), 4)
}

func (m *model) eventLine(e *derive.Event, width int) string {
	st := m.st
	ts := st.dim.Render(e.Time.Format("15:04:05"))
	sid := st.dim.Render(padRight(e.Short, 8))
	role := m.roleBadge(e)
	prefix := ts + " " + sid + " " + role + " "

	var body string
	switch e.Kind {
	case derive.EvToolResult:
		mark, ms := "✓", st.ok
		if e.Err {
			mark, ms = "✗", st.errText
		}
		body = ms.Render(mark) + " " + st.value.Render(e.Name)
		if kv, ok := inlineJSON(e.Raw, st); ok && kv != "" {
			body += "  " + kv
		}
	case derive.EvToolCall:
		body = st.accent.Render("→ ") + st.value.Render(e.Name)
		if kv, ok := inlineJSON(e.Raw, st); ok && kv != "" {
			body += "  " + kv
		}
	case derive.EvSessionStart, derive.EvSessionEnd:
		body = st.roleSession.Render(e.Summary)
	case derive.EvSystem:
		body = st.dim.Render(e.Summary)
	default: // assistant, user
		body = st.value.Render(e.Summary)
	}

	avail := width - ansiWidth(prefix)
	if avail < 1 {
		avail = 1
	}
	return padANSILine(prefix+clipANSI(body, avail), width)
}

func (m *model) roleBadge(e *derive.Event) string {
	label := e.Role
	if label == "" {
		label = "?"
	}
	return m.st.roleStyle(e.Role).Render(padRight("["+label+"]", 11))
}
