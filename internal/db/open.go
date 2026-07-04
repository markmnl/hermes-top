// Package db is the read-only access layer for the Hermes Agent SQLite state
// store (state.db). It never writes: connections are opened with mode=ro and
// PRAGMA query_only, and all reads run as short autocommit statements so we
// never pin the WAL or block the live Hermes writer.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go, CGO-free SQLite driver
)

// DB is a read-only handle to a Hermes state.db. It holds a single dedicated
// connection for its whole lifetime: PRAGMA data_version is per-connection, so
// change detection breaks if the pool recycles connections underneath us.
type DB struct {
	Path string

	sqlDB *sql.DB
	conn  *sql.Conn

	// Column presence, probed once at open, so queries tolerate older Hermes
	// schemas that predate some columns.
	sessionCols map[string]bool
	messageCols map[string]bool
}

// ResolvePath determines which state.db to read, in priority order:
//   - the explicit flag value, if non-empty
//   - $HERMES_HOME/state.db, if HERMES_HOME is set
//   - ~/.hermes/state.db (the platform default)
func ResolvePath(flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return expandUser(flag)
	}
	if home := strings.TrimSpace(os.Getenv("HERMES_HOME")); home != "" {
		return filepath.Join(home, "state.db"), nil
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve home directory: %w", err)
	}
	return filepath.Join(uh, ".hermes", "state.db"), nil
}

func expandUser(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		uh, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return uh, nil
		}
		return filepath.Join(uh, p[2:]), nil
	}
	return p, nil
}

// Open opens the given state.db read-only. The file must already exist (Hermes
// creates and initializes it); a missing file is reported clearly so the caller
// can print a friendly hint.
func Open(ctx context.Context, path string) (*DB, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("state database not found at %s", path)
		}
		return nil, fmt.Errorf("cannot access %s: %w", path, err)
	}

	// Read-only at every layer we can reach:
	//   mode=ro         SQLite refuses writes at the VFS level
	//   query_only(1)   second belt-and-braces guard
	//   busy_timeout    small: if the writer briefly locks, skip rather than queue
	//   _txlock=deferred never take a write lock to begin a transaction
	dsn := fmt.Sprintf("file:%s?mode=ro&_txlock=deferred&_pragma=query_only(1)&_pragma=busy_timeout(200)",
		filepath.ToSlash(path))

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One connection, held for the process lifetime (see DB doc comment).
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxIdleTime(0)
	sqlDB.SetConnMaxLifetime(0)

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("connect %s: %w", path, err)
	}

	d := &DB{Path: path, sqlDB: sqlDB, conn: conn}
	if d.sessionCols, err = d.probeColumns(ctx, "sessions"); err != nil {
		_ = d.Close()
		return nil, err
	}
	if d.messageCols, err = d.probeColumns(ctx, "messages"); err != nil {
		_ = d.Close()
		return nil, err
	}
	if !d.sessionCols["id"] || !d.messageCols["id"] {
		_ = d.Close()
		return nil, fmt.Errorf("%s does not look like a Hermes state database (missing sessions/messages)", path)
	}
	return d, nil
}

// Close releases the connection and pool.
func (d *DB) Close() error {
	var first error
	if d.conn != nil {
		if err := d.conn.Close(); err != nil {
			first = err
		}
	}
	if d.sqlDB != nil {
		if err := d.sqlDB.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// probeColumns records which columns a table has, so queries can substitute
// NULL for columns absent in older schemas.
func (d *DB) probeColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := d.conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("probe %s columns: %w", table, err)
	}
	defer rows.Close()

	cols := make(map[string]bool)
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     sql.NullString
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		cols[name] = true
	}
	return cols, rows.Err()
}

// reconnect drops the current connection and acquires a fresh one; used to
// recover from a dead connection (e.g. the DB was replaced under us).
func (d *DB) reconnect(ctx context.Context) error {
	if d.conn != nil {
		_ = d.conn.Close()
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := d.sqlDB.Conn(cctx)
	if err != nil {
		return fmt.Errorf("reconnect: %w", err)
	}
	d.conn = conn
	return nil
}
