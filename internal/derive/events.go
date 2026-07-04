package derive

import (
	"strings"
	"time"

	"github.com/markmnl/hermes-top/internal/db"
)

// EventKind categorizes an event line for styling and filtering.
type EventKind int

const (
	// EvUser is a user message.
	EvUser EventKind = iota
	// EvAssistant is an assistant message (text, no tool call).
	EvAssistant
	// EvToolCall is an assistant message issuing tool call(s).
	EvToolCall
	// EvToolResult is a tool result row.
	EvToolResult
	// EvSystem is a system message.
	EvSystem
	// EvSessionStart is a synthesized session-started event.
	EvSessionStart
	// EvSessionEnd is a synthesized session-ended event.
	EvSessionEnd
)

// Event is one line in the scrolling events pane. It carries plain data;
// styling is applied at render time in the UI.
type Event struct {
	Time      time.Time
	SessionID string // full id
	Short     string // short id for display
	Kind      EventKind
	Role      string
	Summary   string // one-line plain-text summary
	Err       bool   // tool result that looked like an error
}

// ShortID abbreviates a session id for display. Hermes ids look like
// "20260704_090016_5945c6" or "cron_59972e48f818_20260703_083123"; the trailing
// hex suffix is the most distinguishing part.
func ShortID(id string) string {
	if id == "" {
		return "-"
	}
	parts := strings.Split(id, "_")
	last := parts[len(parts)-1]
	if len(last) >= 4 {
		return last
	}
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

// MessageEvent builds an event line from a message row. Returns ok=false for
// rows that should not appear as their own event (currently none, but keeps the
// caller uniform if that changes).
func MessageEvent(m db.Message) (Event, bool) {
	e := Event{
		Time:      m.Time(),
		SessionID: m.SessionID,
		Short:     ShortID(m.SessionID),
		Role:      m.Role,
	}
	switch {
	case m.Role == "tool":
		e.Kind = EvToolResult
		name := nz(m.ToolName)
		if name == "" {
			name = "tool"
		}
		e.Err = SniffStatus(m.Content) == ActionError
		e.Summary = "← " + name + ": " + truncate(collapse(m.Content), 160)
	case m.ToolCalls.Valid && strings.TrimSpace(m.ToolCalls.String) != "":
		e.Kind = EvToolCall
		acts := ActionsFromAssistant(m)
		names := make([]string, 0, len(acts))
		for _, a := range acts {
			if a.ArgsSummary != "" {
				names = append(names, a.Name+"("+truncate(a.ArgsSummary, 60)+")")
			} else {
				names = append(names, a.Name)
			}
		}
		e.Summary = "→ " + strings.Join(names, ", ")
	case m.Role == "assistant":
		e.Kind = EvAssistant
		e.Summary = truncate(collapse(m.Content), 200)
	case m.Role == "system":
		e.Kind = EvSystem
		e.Summary = truncate(collapse(m.Content), 200)
	default: // user and anything else
		e.Kind = EvUser
		e.Summary = truncate(collapse(m.Content), 200)
	}
	if e.Summary == "" {
		e.Summary = "(empty)"
	}
	return e, true
}

// SessionStartEvent synthesizes a lifecycle event for a newly seen session.
func SessionStartEvent(s db.Session) Event {
	src := s.Source
	if src == "" {
		src = "?"
	}
	return Event{
		Time:      s.Started(),
		SessionID: s.ID,
		Short:     ShortID(s.ID),
		Kind:      EvSessionStart,
		Role:      "session",
		Summary:   "▶ session started (" + src + ")",
	}
}

// SessionEndEvent synthesizes a lifecycle event for a session that just ended.
func SessionEndEvent(s db.Session) Event {
	reason := "ended"
	if s.EndReason.Valid && s.EndReason.String != "" {
		reason = s.EndReason.String
	}
	t := s.Started()
	if end, ok := s.Ended(); ok {
		t = end
	}
	return Event{
		Time:      t,
		SessionID: s.ID,
		Short:     ShortID(s.ID),
		Kind:      EvSessionEnd,
		Role:      "session",
		Summary:   "■ session ended: " + reason,
	}
}

// Matches reports whether the event matches a case-insensitive substring
// filter over its display fields (short id, role, summary).
func (e Event) Matches(needle string) bool {
	if needle == "" {
		return true
	}
	needle = strings.ToLower(needle)
	return strings.Contains(strings.ToLower(e.Short), needle) ||
		strings.Contains(strings.ToLower(e.Role), needle) ||
		strings.Contains(strings.ToLower(e.Summary), needle)
}
