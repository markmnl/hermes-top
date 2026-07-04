package db

import (
	"database/sql"
	"time"
)

// StaleThreshold is how long a session may go without a new message before an
// open (ended_at IS NULL) session is treated as "stale" rather than "running".
// Hermes has no heartbeat column and crashed sessions never get ended_at set,
// so this is a heuristic, not a fact. Displayed as "stale", never "crashed".
const StaleThreshold = 120 * time.Second

// SessionStatus is a derived liveness classification for a session.
type SessionStatus int

const (
	// StatusEnded means ended_at is set.
	StatusEnded SessionStatus = iota
	// StatusRunning means ended_at is NULL and there was recent activity.
	StatusRunning
	// StatusStale means ended_at is NULL but no activity for StaleThreshold.
	StatusStale
)

func (s SessionStatus) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusStale:
		return "stale"
	default:
		return "ended"
	}
}

// Session mirrors a row of the Hermes `sessions` table (read-only). Nullable
// columns use sql.Null* so that missing/absent data is distinguishable from
// zero. LastMsgAt is computed by the query, not a stored column.
type Session struct {
	ID       string
	Source   string
	Model    sql.NullString
	ParentID sql.NullString

	StartedAt float64 // unix epoch seconds (REAL in SQLite)
	EndedAt   sql.NullFloat64
	EndReason sql.NullString

	MessageCount  int64
	ToolCallCount int64
	APICallCount  int64

	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64

	CWD             sql.NullString
	GitBranch       sql.NullString
	BillingProvider sql.NullString

	EstimatedCostUSD sql.NullFloat64
	ActualCostUSD    sql.NullFloat64
	CostStatus       sql.NullString
	Title            sql.NullString

	Archived bool

	LastMsgAt sql.NullFloat64 // MAX(messages.timestamp) for this session
}

// Started returns the session start time.
func (s Session) Started() time.Time { return epoch(s.StartedAt) }

// Ended returns the session end time and whether it is set.
func (s Session) Ended() (time.Time, bool) {
	if s.EndedAt.Valid {
		return epoch(s.EndedAt.Float64), true
	}
	return time.Time{}, false
}

// LastActivity returns the most recent known activity time: last message if
// known, otherwise the start time.
func (s Session) LastActivity() time.Time {
	if s.LastMsgAt.Valid {
		return epoch(s.LastMsgAt.Float64)
	}
	return epoch(s.StartedAt)
}

// Status classifies the session relative to now.
func (s Session) Status(now time.Time) SessionStatus {
	if s.EndedAt.Valid {
		return StatusEnded
	}
	if now.Sub(s.LastActivity()) < StaleThreshold {
		return StatusRunning
	}
	return StatusStale
}

// Duration returns wall-clock duration: for ended sessions the closed span,
// otherwise from start to now.
func (s Session) Duration(now time.Time) time.Duration {
	if end, ok := s.Ended(); ok {
		return end.Sub(s.Started())
	}
	return now.Sub(s.Started())
}

// TotalTokens sums the token counters for a compact display.
func (s Session) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheReadTokens + s.CacheWriteTokens + s.ReasoningTokens
}

// Message mirrors a row of the Hermes `messages` table (read-only).
type Message struct {
	ID           int64
	SessionID    string
	Role         string
	Content      string // COALESCE'd to "" in the query
	ToolCallID   sql.NullString
	ToolCalls    sql.NullString // JSON array on assistant rows
	ToolName     sql.NullString // set on role='tool' rows
	Timestamp    float64
	TokenCount   sql.NullInt64
	FinishReason sql.NullString
	Active       bool
	Compacted    bool
}

// Time returns the message timestamp.
func (m Message) Time() time.Time { return epoch(m.Timestamp) }

// epoch converts a SQLite REAL unix timestamp (seconds, fractional) to time.Time.
func epoch(sec float64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	whole := int64(sec)
	nsec := int64((sec - float64(whole)) * 1e9)
	return time.Unix(whole, nsec)
}
