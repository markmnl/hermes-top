package ui

import (
	"strings"

	"github.com/markmnl/hermes-top/internal/derive"
)

func (m *model) timelineTitle() string {
	s := m.selectedSession()
	if s == nil {
		return "actions"
	}
	n := len(m.actions[s.ID])
	return "actions · " + itoa(n)
}

// timelineContent renders the selected session's action timeline. It returns
// the content plus the line span (top, height) of the cursor entry so the
// viewport can be scrolled to keep it visible.
func (m *model) timelineContent() (string, int, int) {
	st := m.st
	s := m.selectedSession()
	if s == nil {
		return st.dim.Render("no session selected"), -1, 0
	}
	acts := m.actions[s.ID]
	if len(acts) == 0 {
		return st.dim.Render("no tool activity"), -1, 0
	}
	width := m.timelineVP.Width
	focused := m.focus == paneTimeline

	blocks := make([]string, len(acts))
	counts := make([]int, len(acts))
	for i, a := range acts {
		isCursor := i == m.timelineCursor
		blocks[i] = m.actionBlock(a, width, focused && isCursor, isCursor)
		counts[i] = strings.Count(blocks[i], "\n") + 1
	}

	top, height := -1, 0
	if m.timelineCursor >= 0 && m.timelineCursor < len(acts) {
		t := 0
		for j := 0; j < m.timelineCursor; j++ {
			t += counts[j]
		}
		top, height = t, counts[m.timelineCursor]
	}
	return strings.Join(blocks, "\n"), top, height
}

// actionBlock renders one action: a summary line (with a cursor gutter) and,
// when expanded, its pretty-printed arguments (and result JSON on errors).
func (m *model) actionBlock(a *derive.Action, width int, active, isCursor bool) string {
	inner := width - 2 // 2-col gutter
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
	line := gs.Render(marker+" ") + m.actionLine(a, inner)

	if !m.expandedActions[a.Seq] {
		return line
	}
	var b strings.Builder
	b.WriteString(line)
	if strings.TrimSpace(a.ArgsRaw) != "" {
		b.WriteString("\n" + indentBlock(prettyJSON(a.ArgsRaw, m.st, width-4), 4))
	}
	if a.Status == derive.ActionError && strings.TrimSpace(a.ResultRaw) != "" {
		b.WriteString("\n" + indentBlock(m.st.dim.Render("result:"), 4))
		b.WriteString("\n" + indentBlock(prettyJSON(a.ResultRaw, m.st, width-4), 4))
	}
	return b.String()
}

// actionLine renders the single-line summary of an action within innerWidth.
func (m *model) actionLine(a *derive.Action, innerWidth int) string {
	st := m.st
	ts := st.dim.Render(a.StartedAt.Format("15:04:05"))

	var mark, dur string
	markStyle := st.pending
	switch a.Status {
	case derive.ActionOK:
		mark, markStyle = "✓", st.ok
	case derive.ActionError:
		mark, markStyle = "✗", st.errText
	default:
		mark, markStyle = "…", st.pending
	}
	dur = humanDuration(a.Duration(m.now))

	call := st.value.Render(a.Name)
	if kv, ok := inlineJSON(a.ArgsRaw, st); ok && kv != "" {
		call += "  " + kv
	} else if a.ArgsSummary != "" {
		call += st.jsonPn.Render("(") + st.value.Render(a.ArgsSummary) + st.jsonPn.Render(")")
	}

	left := ts + " " + markStyle.Render(mark) + " " + call
	right := markStyle.Render(dur)
	return joinLR(left, right, innerWidth)
}

// joinLR places left text and right text on one line of the given width, with
// the right text flushed to the right edge (truncating left if needed).
func joinLR(left, right string, width int) string {
	lw := ansiWidth(left)
	rw := ansiWidth(right)
	if lw+1+rw > width {
		left = clipANSI(left, max1(width-rw-1))
		lw = ansiWidth(left)
	}
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
