package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// border overhead for a rounded box: 1 col/row each side.
const borderPad = 2

// layout computes outer box dimensions from the current window size and sizes
// the two viewports accordingly.
func (m *model) layout() {
	if m.width < minWidth || m.height < minHeight {
		return
	}
	bodyH := m.height - 2 // header + footer lines

	eventsH := bodyH * 3 / 10
	if eventsH < 6 {
		eventsH = 6
	}
	topH := bodyH - eventsH
	if topH < summaryRows+borderPad+3 {
		// Give the top region a floor; shrink events if needed.
		topH = summaryRows + borderPad + 3
		eventsH = bodyH - topH
		if eventsH < 4 {
			eventsH = 4
		}
	}

	leftW := listWidth
	if leftW > m.width/2 {
		leftW = m.width / 2
	}
	if leftW < 24 {
		leftW = 24
	}
	rightW := m.width - leftW

	summaryH := summaryRows + borderPad
	timelineH := topH - summaryH
	if timelineH < 4 {
		timelineH = 4
	}

	m.dim = layoutDims{
		leftW:     leftW,
		rightW:    rightW,
		topH:      topH,
		summaryH:  summaryH,
		timelineH: timelineH,
		eventsH:   eventsH,
	}

	// Viewport content areas: outer minus border (2) minus 1 title line.
	tlW := rightW - borderPad
	tlH := timelineH - borderPad - 1
	evW := m.width - borderPad
	evH := eventsH - borderPad - 1
	m.timelineVP = ensureVP(m.timelineVP, tlW, tlH)
	m.eventsVP = ensureVP(m.eventsVP, evW, evH)
}

func ensureVP(vp viewport.Model, w, h int) viewport.Model {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	vp.Width = w
	vp.Height = h
	return vp
}

// rebuildViewports regenerates viewport content and scrolls so the cursor entry
// stays visible (or pins to the bottom when following).
func (m *model) rebuildViewports() {
	if !m.ready {
		return
	}
	m.syncCursors()

	tc, ttop, th := m.timelineContent()
	m.timelineVP.SetContent(tc)
	scrollToCursor(&m.timelineVP, m.timelineFollow, ttop, th)

	ec, etop, eh := m.eventsContent()
	m.eventsVP.SetContent(ec)
	scrollToCursor(&m.eventsVP, m.eventsFollow, etop, eh)
}

// scrollToCursor scrolls vp minimally so the [top, top+height) line range is
// visible; when following (or no cursor) it pins to the bottom.
func scrollToCursor(vp *viewport.Model, follow bool, top, height int) {
	if follow || top < 0 {
		vp.GotoBottom()
		return
	}
	off := vp.YOffset
	if top < off {
		off = top
	}
	if top+height > off+vp.Height {
		off = top + height - vp.Height
	}
	if off < 0 {
		off = 0
	}
	vp.SetYOffset(off)
}

// indentBlock prefixes every line of s with n spaces.
func indentBlock(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *model) View() string {
	if !m.ready {
		return "starting hermes-top…"
	}
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("terminal too small\nneed at least %d×%d, have %d×%d",
			minWidth, minHeight, m.width, m.height)
		return lipgloss.Place(max1(m.width), max1(m.height),
			lipgloss.Center, lipgloss.Center, m.st.tooSmall.Render(msg))
	}

	header := m.renderHeader()

	sessionsBox := m.box(paneSessions, "sessions", m.renderSessionList(), m.dim.leftW, m.dim.topH)
	summaryBox := m.box(-1, "session", m.renderSummary(), m.dim.rightW, m.dim.summaryH)
	timelineBox := m.box(paneTimeline, m.timelineTitle(), m.timelineVP.View(), m.dim.rightW, m.dim.timelineH)
	rightCol := lipgloss.JoinVertical(lipgloss.Left, summaryBox, timelineBox)
	top := lipgloss.JoinHorizontal(lipgloss.Top, sessionsBox, rightCol)

	eventsBox := m.box(paneEvents, m.eventsTitle(), m.eventsVP.View(), m.width, m.dim.eventsH)

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, top, eventsBox, footer)
}

// box renders a titled, bordered pane. pane is the pane id for focus styling
// (-1 = never focusable, e.g. the summary).
func (m *model) box(pane paneID, title, body string, outerW, outerH int) string {
	focused := pane >= 0 && m.focus == pane
	border := m.st.paneBorder
	titleStyle := m.st.paneTitle
	if focused {
		border = m.st.paneFocused
		titleStyle = m.st.paneTitleFo
	}
	contentW := outerW - borderPad
	contentH := outerH - borderPad
	if contentW < 1 {
		contentW = 1
	}
	if contentH < 1 {
		contentH = 1
	}
	titleLine := titleStyle.Render(clip(title, contentW))
	inner := lipgloss.JoinVertical(lipgloss.Left, titleLine, body)
	return border.Width(contentW).Height(contentH).MaxHeight(outerH).MaxWidth(outerW).Render(inner)
}

// clip truncates a string to n display columns (rune-based approximation).
func clip(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// padRight pads/truncates s to exactly n columns (rune approximation).
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) == n {
		return s
	}
	if len(r) > n {
		return clip(s, n)
	}
	return s + strings.Repeat(" ", n-len(r))
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
