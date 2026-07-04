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

// timelineContent renders the selected session's action timeline, one line per
// tool call: time, name(args), and status/duration.
func (m *model) timelineContent() string {
	st := m.st
	s := m.selectedSession()
	if s == nil {
		return st.dim.Render("no session selected")
	}
	acts := m.actions[s.ID]
	if len(acts) == 0 {
		return st.dim.Render("no tool activity")
	}
	width := m.timelineVP.Width

	var b strings.Builder
	for i, a := range acts {
		b.WriteString(m.actionLine(a, width))
		if i < len(acts)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m *model) actionLine(a *derive.Action, width int) string {
	st := m.st
	ts := st.dim.Render(a.StartedAt.Format("15:04:05"))

	var mark, dur string
	var markStyle = st.pending
	switch a.Status {
	case derive.ActionOK:
		mark, markStyle = "✓", st.ok
		dur = humanDuration(a.Duration(m.now))
	case derive.ActionError:
		mark, markStyle = "✗", st.errText
		dur = humanDuration(a.Duration(m.now))
	default: // pending
		mark, markStyle = "…", st.pending
		dur = humanDuration(a.Duration(m.now))
	}

	call := a.Name
	if a.ArgsSummary != "" {
		call += "(" + a.ArgsSummary + ")"
	}

	// left: time + mark + call ; right: duration
	left := ts + " " + markStyle.Render(mark) + " " + st.value.Render(call)
	right := markStyle.Render(dur)

	line := joinLR(left, right, width)

	// error detail on same line if room already consumed; append snippet line-inline
	if a.Status == derive.ActionError && a.Result != "" {
		// nothing extra; result shown in events pane
	}
	return line
}

// joinLR places left text and right text on one line of the given width, with
// the right text flushed to the right edge (truncating left if needed).
func joinLR(left, right string, width int) string {
	lw := ansiWidth(left)
	rw := ansiWidth(right)
	if lw+1+rw > width {
		// truncate left to make room
		left = clipANSI(left, max1(width-rw-1))
		lw = ansiWidth(left)
	}
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
