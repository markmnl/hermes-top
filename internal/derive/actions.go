// Package derive turns raw Hermes message/session rows into the higher-level
// things the dashboard shows: tool "actions" (a tool call paired with its
// result) and one-line "events". All functions here are pure and depend only
// on the db row types, so they are unit-testable without a database.
package derive

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/markmnl/hermes-top/internal/db"
)

// ActionStatus is the outcome of a tool call.
type ActionStatus int

const (
	// ActionPending means the tool call has no result yet.
	ActionPending ActionStatus = iota
	// ActionOK means the tool returned without an error signal.
	ActionOK
	// ActionError means the tool result looked like an error.
	ActionError
)

func (s ActionStatus) String() string {
	switch s {
	case ActionOK:
		return "ok"
	case ActionError:
		return "error"
	default:
		return "pending"
	}
}

// Action is a tool invocation and (once it arrives) its result.
type Action struct {
	CallID      string
	SessionID   string
	Name        string
	ArgsSummary string
	StartedAt   time.Time
	EndedAt     time.Time // zero until the result arrives
	Status      ActionStatus
	Result      string // one-line result snippet
}

// Done reports whether the tool result has been paired in.
func (a Action) Done() bool { return !a.EndedAt.IsZero() }

// Duration returns how long the tool took (or has been running, if pending).
func (a Action) Duration(now time.Time) time.Duration {
	if a.Done() {
		return a.EndedAt.Sub(a.StartedAt)
	}
	return now.Sub(a.StartedAt)
}

// toolCall is the shape of one element of an assistant row's tool_calls JSON.
// Hermes stores both "id" and "call_id" (usually identical); the tool result
// row's tool_call_id matches these.
type toolCall struct {
	ID       string `json:"id"`
	CallID   string `json:"call_id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (tc toolCall) id() string {
	if tc.ID != "" {
		return tc.ID
	}
	return tc.CallID
}

// ActionsFromAssistant parses an assistant message's tool_calls JSON into
// pending Actions. Malformed or unexpected JSON never panics: it yields a
// single fallback Action carrying a raw snippet so the timeline still shows
// that something happened.
func ActionsFromAssistant(m db.Message) []Action {
	if !m.ToolCalls.Valid || strings.TrimSpace(m.ToolCalls.String) == "" {
		return nil
	}
	raw := m.ToolCalls.String
	start := m.Time()

	var calls []toolCall
	if err := json.Unmarshal([]byte(raw), &calls); err != nil || len(calls) == 0 {
		return []Action{{
			CallID:      "",
			SessionID:   m.SessionID,
			Name:        "tool_calls?",
			ArgsSummary: truncate(collapse(raw), 120),
			StartedAt:   start,
			Status:      ActionPending,
		}}
	}

	out := make([]Action, 0, len(calls))
	for _, c := range calls {
		name := c.Function.Name
		if name == "" {
			name = "tool"
		}
		out = append(out, Action{
			CallID:      c.id(),
			SessionID:   m.SessionID,
			Name:        name,
			ArgsSummary: truncate(collapse(c.Function.Arguments), 120),
			StartedAt:   start,
			Status:      ActionPending,
		})
	}
	return out
}

// ApplyToolResult folds a role='tool' result row into the matching Action.
func ApplyToolResult(a *Action, m db.Message) {
	a.EndedAt = m.Time()
	a.Status = SniffStatus(m.Content)
	a.Result = truncate(collapse(m.Content), 160)
	if a.Name == "" || a.Name == "tool" {
		if m.ToolName.Valid && m.ToolName.String != "" {
			a.Name = m.ToolName.String
		}
	}
}

// OrphanAction builds an Action for a tool result whose originating call was
// not seen (e.g. it fell outside the backfill window).
func OrphanAction(m db.Message) Action {
	name := "tool"
	if m.ToolName.Valid && m.ToolName.String != "" {
		name = m.ToolName.String
	}
	t := m.Time()
	return Action{
		CallID:    nz(m.ToolCallID),
		SessionID: m.SessionID,
		Name:      name,
		StartedAt: t,
		EndedAt:   t,
		Status:    SniffStatus(m.Content),
		Result:    truncate(collapse(m.Content), 160),
	}
}

// SniffStatus classifies a tool result body as OK or Error. It first tries to
// read a JSON object for common error signals, then falls back to scanning the
// leading text. Isolated so the heuristic is easy to test and tune.
func SniffStatus(content string) ActionStatus {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ActionOK
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
			if v, ok := obj["error"]; ok && !isEmpty(v) {
				return ActionError
			}
			if b, ok := obj["is_error"].(bool); ok && b {
				return ActionError
			}
			if b, ok := obj["ok"].(bool); ok && !b {
				return ActionError
			}
			if s, ok := obj["status"].(string); ok && strings.EqualFold(s, "error") {
				return ActionError
			}
			// A well-formed JSON object with no error signal is a success.
			return ActionOK
		}
	}
	head := strings.ToLower(trimmed)
	if len(head) > 200 {
		head = head[:200]
	}
	for _, marker := range []string{"error:", "error ", "traceback", "exception", "denied", "failed", "failure"} {
		if strings.Contains(head, marker) {
			return ActionError
		}
	}
	return ActionOK
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case bool:
		return !x
	default:
		return false
	}
}

// collapse flattens all runs of whitespace (including newlines) to single
// spaces and trims, for compact one-line display.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// nz returns the string value of a sql.NullString, or "" when NULL.
func nz(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
