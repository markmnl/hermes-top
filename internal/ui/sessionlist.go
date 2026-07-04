package ui

import (
	"strings"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
)

// ensureSelectedVisible adjusts listOffset so the row at idx stays within the
// visible window of the sessions pane.
func (m *model) ensureSelectedVisible(idx int) {
	rows := m.sessionListRows()
	if rows < 1 {
		return
	}
	if idx < m.listOffset {
		m.listOffset = idx
	} else if idx >= m.listOffset+rows {
		m.listOffset = idx - rows + 1
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}

// sessionListRows is how many session rows fit in the left pane.
func (m *model) sessionListRows() int {
	// topH outer minus border(2) minus title(1); each session takes 2 lines.
	inner := m.dim.topH - borderPad - 1
	if inner < 1 {
		return 1
	}
	return inner / 2
}

func (m *model) renderSessionList() string {
	st := m.st
	if len(m.sessions) == 0 {
		return st.dim.Render("no sessions found")
	}
	rows := m.sessionListRows()
	width := m.dim.leftW - borderPad
	if width < 1 {
		width = 1
	}

	end := m.listOffset + rows
	if end > len(m.sessions) {
		end = len(m.sessions)
	}

	var b strings.Builder
	for i := m.listOffset; i < end; i++ {
		s := &m.sessions[i]
		selected := s.ID == m.selected
		b.WriteString(m.sessionRow(s, selected, width))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	// scroll hint
	if len(m.sessions) > rows {
		hint := st.dim.Render(padRight("  "+itoa(m.selectedIndex()+1)+"/"+itoa(len(m.sessions)), width))
		b.WriteString("\n" + hint)
	}
	return b.String()
}

// sessionRow renders a two-line entry: status marker + short id + source, then
// model + token summary.
func (m *model) sessionRow(s *db.Session, selected bool, width int) string {
	st := m.st
	status := s.Status(m.now)

	marker, markerStyle := "■", st.ended
	switch status {
	case db.StatusRunning:
		marker, markerStyle = "●", st.running
		if m.frame%2 == 0 {
			markerStyle = st.ok // subtle pulse: alternate bright/normal green
		}
	case db.StatusStale:
		marker, markerStyle = "◌", st.stale
	}

	short := derive.ShortID(s.ID)
	src := s.Source
	line1 := marker + " " + short + "  " + src

	model := "-"
	if s.Model.Valid && s.Model.String != "" {
		model = s.Model.String
	}
	dur := humanDuration(s.Duration(m.now))
	line2 := "  " + model + "  " + humanTokens(s.TotalTokens()) + "tok " + dur

	l1 := padRight(line1, width)
	l2 := padRight(line2, width)

	if selected {
		return st.selRow.Render(l1) + "\n" + st.selRow.Render(l2)
	}
	// status color on line1 marker+id, dim on line2
	styled1 := markerStyle.Render(marker) + " " + st.value.Render(padRight(short+"  "+src, width-2))
	styled2 := st.dim.Render(l2)
	return styled1 + "\n" + styled2
}
