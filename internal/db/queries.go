package db

import (
	"context"
	"fmt"
	"strings"
)

// MaxSessions caps how many sessions we list; far more than a machine ever has
// concurrently, but bounds work if the DB is huge.
const MaxSessions = 200

// BackfillLimit is how many recent messages we load at startup.
const BackfillLimit = 2000

// incrementalLimit is how many new messages we pull per incremental query;
// looped until a short read drains a burst.
const incrementalLimit = 1000

// sessionColumns is the fixed select order. Each entry maps a column name to a
// typed NULL fallback used when an older schema lacks the column. Session
// scanning below must match this order exactly.
var sessionColumns = []struct{ name, null string }{
	{"id", "NULL"},
	{"source", "''"},
	{"model", "NULL"},
	{"parent_session_id", "NULL"},
	{"started_at", "0"},
	{"ended_at", "NULL"},
	{"end_reason", "NULL"},
	{"message_count", "0"},
	{"tool_call_count", "0"},
	{"api_call_count", "0"},
	{"input_tokens", "0"},
	{"output_tokens", "0"},
	{"cache_read_tokens", "0"},
	{"cache_write_tokens", "0"},
	{"reasoning_tokens", "0"},
	{"cwd", "NULL"},
	{"git_branch", "NULL"},
	{"billing_provider", "NULL"},
	{"estimated_cost_usd", "NULL"},
	{"actual_cost_usd", "NULL"},
	{"cost_status", "NULL"},
	{"title", "NULL"},
	{"archived", "0"},
}

// selectList builds the session column expressions honoring missing columns.
func (d *DB) sessionSelectList() string {
	parts := make([]string, 0, len(sessionColumns)+1)
	for _, c := range sessionColumns {
		if d.sessionCols[c.name] {
			parts = append(parts, "s."+c.name)
		} else {
			parts = append(parts, c.null+" AS "+c.name)
		}
	}
	// last_msg_at is always computed
	parts = append(parts, "(SELECT MAX(m.timestamp) FROM messages m WHERE m.session_id = s.id) AS last_msg_at")
	return strings.Join(parts, ", ")
}

// ListSessions returns non-archived sessions, active (open) first, then most
// recently active. The correlated MAX(timestamp) uses idx_messages_session.
func (d *DB) ListSessions(ctx context.Context) ([]Session, error) {
	where := ""
	if d.sessionCols["archived"] {
		where = "WHERE s.archived = 0"
	}
	q := fmt.Sprintf(`
		SELECT * FROM (
			SELECT %s
			FROM sessions s
			%s
		)
		ORDER BY (ended_at IS NULL) DESC, COALESCE(last_msg_at, started_at) DESC
		LIMIT %d`, d.sessionSelectList(), where, MaxSessions)

	rows, err := d.conn.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		// Scan order MUST match sessionColumns, then last_msg_at.
		if err := rows.Scan(
			&s.ID, &s.Source, &s.Model, &s.ParentID,
			&s.StartedAt, &s.EndedAt, &s.EndReason,
			&s.MessageCount, &s.ToolCallCount, &s.APICallCount,
			&s.InputTokens, &s.OutputTokens, &s.CacheReadTokens, &s.CacheWriteTokens, &s.ReasoningTokens,
			&s.CWD, &s.GitBranch, &s.BillingProvider,
			&s.EstimatedCostUSD, &s.ActualCostUSD, &s.CostStatus, &s.Title,
			&s.Archived,
			&s.LastMsgAt,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// messageColumns is the fixed select order for messages, each with a typed
// NULL/default fallback for older schemas. Scanning below matches this order.
var messageColumns = []struct{ name, expr, null string }{
	{"id", "id", "0"},
	{"session_id", "session_id", "''"},
	{"role", "role", "''"},
	{"content", "COALESCE(content, '')", "''"},
	{"tool_call_id", "tool_call_id", "NULL"},
	{"tool_calls", "tool_calls", "NULL"},
	{"tool_name", "tool_name", "NULL"},
	{"timestamp", "timestamp", "0"},
	{"token_count", "token_count", "NULL"},
	{"finish_reason", "finish_reason", "NULL"},
	{"active", "active", "1"},
	{"compacted", "compacted", "0"},
}

func (d *DB) messageSelect() string {
	parts := make([]string, 0, len(messageColumns))
	for _, c := range messageColumns {
		if d.messageCols[c.name] {
			parts = append(parts, c.expr)
		} else {
			parts = append(parts, c.null)
		}
	}
	return "SELECT " + strings.Join(parts, ", ") + " FROM messages"
}

func scanMessages(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Message, error) {
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID, &m.SessionID, &m.Role, &m.Content,
			&m.ToolCallID, &m.ToolCalls, &m.ToolName,
			&m.Timestamp, &m.TokenCount, &m.FinishReason, &m.Active, &m.Compacted,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// BackfillMessages loads the most recent messages (oldest-first in the result),
// and returns the maximum id seen (0 if none).
func (d *DB) BackfillMessages(ctx context.Context) ([]Message, int64, error) {
	q := fmt.Sprintf("%s ORDER BY id DESC LIMIT %d", d.messageSelect(), BackfillLimit)
	rows, err := d.conn.QueryContext(ctx, q)
	if err != nil {
		return nil, 0, fmt.Errorf("backfill messages: %w", err)
	}
	msgs, err := scanMessages(rows)
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	// reverse to oldest-first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	var maxID int64
	for _, m := range msgs {
		if m.ID > maxID {
			maxID = m.ID
		}
	}
	if maxID == 0 {
		// empty table: probe the true max so future WHERE id > ? is correct
		if err := d.conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM messages").Scan(&maxID); err != nil {
			return nil, 0, fmt.Errorf("max message id: %w", err)
		}
	}
	return msgs, maxID, nil
}

// MessagesSince returns messages with id greater than afterID, oldest-first,
// draining bursts by looping until a short read. Returns the new max id.
func (d *DB) MessagesSince(ctx context.Context, afterID int64) ([]Message, int64, error) {
	maxID := afterID
	var all []Message
	for {
		q := fmt.Sprintf("%s WHERE id > ? ORDER BY id ASC LIMIT %d", d.messageSelect(), incrementalLimit)
		rows, err := d.conn.QueryContext(ctx, q, maxID)
		if err != nil {
			return nil, afterID, fmt.Errorf("messages since %d: %w", afterID, err)
		}
		batch, err := scanMessages(rows)
		rows.Close()
		if err != nil {
			return nil, afterID, err
		}
		for _, m := range batch {
			if m.ID > maxID {
				maxID = m.ID
			}
		}
		all = append(all, batch...)
		if len(batch) < incrementalLimit {
			break
		}
	}
	return all, maxID, nil
}

// DataVersion returns PRAGMA data_version, which changes whenever another
// connection commits a write. Compared across polls for near-free change
// detection. It is per-connection, which is why DB holds one dedicated conn.
func (d *DB) DataVersion(ctx context.Context) (int64, error) {
	var v int64
	if err := d.conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v); err != nil {
		// A dead connection is the likely cause; try once to recover so the
		// next poll can proceed.
		if rerr := d.reconnect(ctx); rerr != nil {
			return 0, fmt.Errorf("data_version: %w", err)
		}
		if err2 := d.conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&v); err2 != nil {
			return 0, fmt.Errorf("data_version after reconnect: %w", err2)
		}
	}
	return v, nil
}
