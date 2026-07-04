package ui

import (
	"strings"

	"github.com/markmnl/hermes-top/internal/db"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *model) renderHeader() string {
	var running, stale, ended int
	var inTok, outTok, cacheTok int64
	for i := range m.sessions {
		s := &m.sessions[i]
		switch s.Status(m.now) {
		case db.StatusRunning:
			running++
		case db.StatusStale:
			stale++
		default:
			ended++
		}
		inTok += s.InputTokens
		outTok += s.OutputTokens
		cacheTok += s.CacheReadTokens + s.CacheWriteTokens
	}

	st := m.st
	glyph := spinnerFrames[m.frame%len(spinnerFrames)]

	var b strings.Builder
	b.WriteString(st.header.Render("hermes-top"))
	b.WriteString("  ")
	b.WriteString(st.accent.Render(glyph))
	b.WriteString("  ")

	b.WriteString(st.headerKey.Render("sessions "))
	b.WriteString(st.headerVal.Render(itoa(len(m.sessions))))
	parts := []string{}
	if running > 0 {
		parts = append(parts, st.running.Render(itoa(running)+" running"))
	}
	if stale > 0 {
		parts = append(parts, st.stale.Render(itoa(stale)+" stale"))
	}
	if len(parts) > 0 {
		b.WriteString(st.headerKey.Render(" ("))
		b.WriteString(strings.Join(parts, st.headerKey.Render(", ")))
		b.WriteString(st.headerKey.Render(")"))
	}

	b.WriteString(st.dim.Render("  ·  "))
	b.WriteString(st.headerKey.Render("tokens "))
	b.WriteString(st.headerVal.Render(humanTokens(inTok)))
	b.WriteString(st.headerKey.Render(" in / "))
	b.WriteString(st.headerVal.Render(humanTokens(outTok)))
	b.WriteString(st.headerKey.Render(" out"))
	if cacheTok > 0 {
		b.WriteString(st.headerKey.Render(" / "))
		b.WriteString(st.headerVal.Render(humanTokens(cacheTok)))
		b.WriteString(st.headerKey.Render(" cache"))
	}

	b.WriteString(st.dim.Render("  ·  "))
	if m.pollErr != nil {
		b.WriteString(st.headerErr.Render("db error: " + clip(m.pollErr.Error(), 40)))
	} else {
		b.WriteString(st.headerKey.Render("updated "))
		b.WriteString(st.value.Render(relativeTime(m.lastPollAt, m.now)))
	}

	return clipANSI(b.String(), m.width)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
