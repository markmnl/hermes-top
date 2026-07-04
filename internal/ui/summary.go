package ui

import (
	"database/sql"
	"strings"

	"github.com/markmnl/hermes-top/internal/db"
)

func (m *model) renderSummary() string {
	st := m.st
	s := m.selectedSession()
	if s == nil {
		return st.dim.Render("no session selected")
	}
	width := m.dim.rightW - borderPad

	status := s.Status(m.now)
	statusStyle := st.ended
	switch status {
	case db.StatusRunning:
		statusStyle = st.running
	case db.StatusStale:
		statusStyle = st.stale
	}

	title := "(untitled)"
	if s.Title.Valid && s.Title.String != "" {
		title = s.Title.String
	}

	var rows []string
	rows = append(rows, st.accent.Render(clip(title, width)))

	rows = append(rows, m.kv("status", statusStyle.Render(status.String())+"  "+st.dim.Render(endReason(s)), width))
	rows = append(rows, m.kv("model", valOr(s.Model, "-")+m.providerSuffix(s), width))
	rows = append(rows, m.kv("id / source", s.ID+"  ("+s.Source+")", width))

	dur := humanDuration(s.Duration(m.now))
	rows = append(rows, m.kv("runtime", dur+"  "+st.dim.Render("started "+relativeTime(s.Started(), m.now)), width))

	tok := "in " + humanTokens(s.InputTokens) + " · out " + humanTokens(s.OutputTokens)
	if s.CacheReadTokens+s.CacheWriteTokens > 0 {
		tok += " · cache " + humanTokens(s.CacheReadTokens+s.CacheWriteTokens)
	}
	if s.ReasoningTokens > 0 {
		tok += " · reasoning " + humanTokens(s.ReasoningTokens)
	}
	rows = append(rows, m.kv("tokens", tok, width))

	activity := itoa(int(s.MessageCount)) + " msgs · " + itoa(int(s.ToolCallCount)) + " tools · " + itoa(int(s.APICallCount)) + " api calls"
	if cost := sessionCost(s); cost != "" {
		activity += " · " + cost
	}
	rows = append(rows, m.kv("activity", activity, width))

	if s.CWD.Valid && s.CWD.String != "" {
		cwd := s.CWD.String
		if s.GitBranch.Valid && s.GitBranch.String != "" {
			cwd += "  (" + s.GitBranch.String + ")"
		}
		rows = append(rows, m.kv("cwd", cwd, width))
	}

	return strings.Join(rows, "\n")
}

func (m *model) kv(label, val string, width int) string {
	const labelW = 12
	l := m.st.label.Render(padRight(label, labelW))
	v := m.st.value.Render(clip(val, max1(width-labelW-1)))
	return clipANSI(l+" "+v, width)
}

func (m *model) providerSuffix(s *db.Session) string {
	if s.BillingProvider.Valid && s.BillingProvider.String != "" {
		return m.st.dim.Render("  via " + s.BillingProvider.String)
	}
	return ""
}

func valOr(ns sql.NullString, def string) string {
	if ns.Valid && ns.String != "" {
		return ns.String
	}
	return def
}

func endReason(s *db.Session) string {
	if _, ok := s.Ended(); !ok {
		return ""
	}
	if s.EndReason.Valid && s.EndReason.String != "" {
		return "(" + s.EndReason.String + ")"
	}
	return ""
}

func sessionCost(s *db.Session) string {
	if c := humanCost(s.ActualCostUSD.Float64, s.ActualCostUSD.Valid); c != "" {
		return c
	}
	if c := humanCost(s.EstimatedCostUSD.Float64, s.EstimatedCostUSD.Valid); c != "" {
		return "~" + c
	}
	return ""
}
