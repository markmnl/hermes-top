package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTestDB creates a minimal Hermes-like state.db and returns its path.
func makeTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	w, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	defer w.Close()
	stmts := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, source TEXT NOT NULL, model TEXT,
			parent_session_id TEXT, started_at REAL NOT NULL, ended_at REAL, end_reason TEXT,
			message_count INTEGER DEFAULT 0, tool_call_count INTEGER DEFAULT 0, api_call_count INTEGER DEFAULT 0,
			input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0, cache_read_tokens INTEGER DEFAULT 0,
			cache_write_tokens INTEGER DEFAULT 0, reasoning_tokens INTEGER DEFAULT 0, cwd TEXT, git_branch TEXT,
			billing_provider TEXT, estimated_cost_usd REAL, actual_cost_usd REAL, cost_status TEXT, title TEXT,
			archived INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, role TEXT NOT NULL,
			content TEXT, tool_call_id TEXT, tool_calls TEXT, tool_name TEXT, timestamp REAL NOT NULL,
			token_count INTEGER, finish_reason TEXT, active INTEGER NOT NULL DEFAULT 1, compacted INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO sessions (id, source, model, started_at, message_count) VALUES ('s1', 'cli', 'testmodel', 1000.0, 2)`,
		`INSERT INTO sessions (id, source, started_at, ended_at, end_reason) VALUES ('s2', 'tui', 900.0, 950.0, 'tui_shutdown')`,
		`INSERT INTO messages (session_id, role, content, timestamp) VALUES ('s1', 'user', 'hi', 1000.0)`,
		`INSERT INTO messages (session_id, role, tool_calls, timestamp) VALUES ('s1', 'assistant', '[{"id":"c1","function":{"name":"bash","arguments":"{}"}}]', 1001.0)`,
	}
	for _, s := range stmts {
		if _, err := w.Exec(s); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return path
}

func TestOpenAndQuery(t *testing.T) {
	path := makeTestDB(t)
	ctx := context.Background()
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	sessions, err := d.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	// open (s1) must sort before ended (s2)
	if sessions[0].ID != "s1" {
		t.Errorf("open session should sort first, got %q", sessions[0].ID)
	}
	if _, ok := sessions[1].Ended(); !ok {
		t.Errorf("s2 should be ended")
	}

	msgs, maxID, err := d.BackfillMessages(ctx)
	if err != nil {
		t.Fatalf("BackfillMessages: %v", err)
	}
	if len(msgs) != 2 || maxID != 2 {
		t.Fatalf("want 2 msgs maxID 2, got %d msgs maxID %d", len(msgs), maxID)
	}
	if msgs[0].ID > msgs[1].ID {
		t.Error("backfill should be oldest-first")
	}

	// incremental: nothing new
	more, newMax, err := d.MessagesSince(ctx, maxID)
	if err != nil {
		t.Fatalf("MessagesSince: %v", err)
	}
	if len(more) != 0 || newMax != maxID {
		t.Errorf("expected no new messages, got %d (max %d)", len(more), newMax)
	}

	if _, err := d.DataVersion(ctx); err != nil {
		t.Errorf("DataVersion: %v", err)
	}
}

// TestConnectionRejectsWrites proves the connection we hold cannot mutate the
// database — the core safety guarantee.
func TestConnectionRejectsWrites(t *testing.T) {
	path := makeTestDB(t)
	ctx := context.Background()
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	_, err = d.conn.ExecContext(ctx, "INSERT INTO messages (session_id, role, timestamp) VALUES ('s1','user',1.0)")
	if err == nil {
		t.Fatal("expected write to be rejected on read-only connection, but it succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "readonly") &&
		!strings.Contains(strings.ToLower(err.Error()), "read-only") &&
		!strings.Contains(strings.ToLower(err.Error()), "read only") {
		t.Errorf("expected a read-only error, got: %v", err)
	}
}

func TestMissingDB(t *testing.T) {
	_, err := Open(context.Background(), filepath.Join(t.TempDir(), "nope.db"))
	if err == nil {
		t.Fatal("expected error opening missing db")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("want 'not found' error, got: %v", err)
	}
}

func TestResolvePath(t *testing.T) {
	if p, _ := ResolvePath("/explicit/state.db"); p != "/explicit/state.db" {
		t.Errorf("explicit flag: got %q", p)
	}

	t.Setenv("HERMES_HOME", "/tmp/hh")
	if p, _ := ResolvePath(""); p != filepath.Join("/tmp/hh", "state.db") {
		t.Errorf("HERMES_HOME: got %q", p)
	}

	os.Unsetenv("HERMES_HOME")
	p, err := ResolvePath("")
	if err != nil {
		t.Fatalf("default resolve: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join(".hermes", "state.db")) {
		t.Errorf("default should end with .hermes/state.db, got %q", p)
	}
}
