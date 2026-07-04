// Command hermes-top is a read-only, htop-style live dashboard for the Hermes
// Agent. It reads Hermes's SQLite state.db and never writes to it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
	"github.com/markmnl/hermes-top/internal/poll"
	"github.com/markmnl/hermes-top/internal/ui"
)

func main() {
	var (
		dbFlag   = flag.String("db", "", "path to Hermes state.db (default: $HERMES_HOME/state.db or ~/.hermes/state.db)")
		interval = flag.Duration("interval", 400*time.Millisecond, "refresh interval (clamped to 250ms–500ms)")
		dump     = flag.Bool("dump", false, "print one snapshot of the database as text and exit (no TUI)")
	)
	flag.Parse()

	path, err := db.ResolvePath(*dbFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hermes-top:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	database, err := db.Open(ctx, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hermes-top:", err)
		fmt.Fprintf(os.Stderr, "\nresolved path: %s\n", path)
		fmt.Fprintln(os.Stderr, "Hint: is Hermes installed and has it run at least once? Set --db or $HERMES_HOME to point elsewhere.")
		os.Exit(1)
	}
	defer database.Close()

	if *dump {
		if err := runDump(ctx, database); err != nil {
			fmt.Fprintln(os.Stderr, "hermes-top:", err)
			os.Exit(1)
		}
		return
	}

	if err := ui.Run(ctx, database, clampInterval(*interval)); err != nil {
		fmt.Fprintln(os.Stderr, "hermes-top:", err)
		os.Exit(1)
	}
}

func clampInterval(d time.Duration) time.Duration {
	const lo, hi = 250 * time.Millisecond, 500 * time.Millisecond
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

// runDump prints a single poll snapshot as plain text. It doubles as a
// read-only smoke test of the db layer.
func runDump(ctx context.Context, database *db.DB) error {
	p := poll.New(database)
	d := p.Poll(ctx)
	if d.Err != nil {
		return d.Err
	}
	now := time.Now()

	var active, ended int
	var totIn, totOut int64
	for _, s := range d.Sessions {
		switch s.Status(now) {
		case db.StatusEnded:
			ended++
		default:
			active++
		}
		totIn += s.InputTokens
		totOut += s.OutputTokens
	}

	fmt.Printf("state.db: %s\n", database.Path)
	fmt.Printf("sessions: %d (%d open, %d ended)\n", len(d.Sessions), active, ended)
	fmt.Printf("messages loaded: %d\n", len(d.NewMessages))
	fmt.Printf("tokens: in=%d out=%d\n\n", totIn, totOut)

	fmt.Println("SESSIONS (most recent first):")
	for _, s := range d.Sessions {
		model := "-"
		if s.Model.Valid {
			model = s.Model.String
		}
		title := ""
		if s.Title.Valid {
			title = s.Title.String
		}
		fmt.Printf("  %-8s %-5s %-14s %-7s %5dmsg %4dtool  in=%-8d out=%-6d  %s\n",
			derive.ShortID(s.ID), s.Source, model, s.Status(now),
			s.MessageCount, s.ToolCallCount, s.InputTokens, s.OutputTokens, title)
	}

	// Show the last few derived events and actions as a sanity check.
	n := len(d.NewMessages)
	tail := d.NewMessages
	if n > 12 {
		tail = d.NewMessages[n-12:]
	}
	fmt.Println("\nRECENT EVENTS:")
	for _, m := range tail {
		if ev, ok := derive.MessageEvent(m); ok {
			fmt.Printf("  %s  %-8s [%-9s] %s\n",
				ev.Time.Format("15:04:05"), ev.Short, ev.Role, ev.Summary)
		}
	}
	return nil
}
