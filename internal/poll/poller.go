// Package poll drives incremental, low-cost reads of the Hermes state store.
// It uses PRAGMA data_version to skip work entirely when nothing has changed,
// reads only messages newer than the last seen id, and synthesizes session
// lifecycle events by diffing successive session snapshots.
package poll

import (
	"context"
	"sync"
	"time"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
)

// Delta is the result of one poll. When SessionsChanged is false and there are
// no NewMessages, nothing changed since the previous poll (the common idle
// case) and the UI can no-op.
type Delta struct {
	SessionsChanged bool
	Sessions        []db.Session
	NewMessages     []db.Message
	Lifecycle       []derive.Event
	Err             error
	PolledAt        time.Time
}

// HasChanges reports whether the delta carries anything worth applying.
func (d Delta) HasChanges() bool {
	return d.SessionsChanged || len(d.NewMessages) > 0 || len(d.Lifecycle) > 0
}

type sessionSnap struct {
	ended bool
}

// Poller holds incremental read state. Poll is safe for sequential use from the
// Bubble Tea command goroutine; a mutex guards against an accidental overlap.
type Poller struct {
	d *db.DB

	mu              sync.Mutex
	lastDataVersion int64
	lastMaxID       int64
	prev            map[string]sessionSnap
	initialized     bool
	forceNext       bool
}

// New creates a Poller over the given read-only DB.
func New(d *db.DB) *Poller {
	return &Poller{d: d, prev: make(map[string]sessionSnap)}
}

// ForceNext makes the next Poll bypass the data_version short-circuit, so a
// manual refresh always re-reads even if nothing committed.
func (p *Poller) ForceNext() {
	p.mu.Lock()
	p.forceNext = true
	p.mu.Unlock()
}

// Poll performs one incremental read and returns a Delta. It never blocks for
// long: queries run with a short busy timeout and autocommit only.
func (p *Poller) Poll(ctx context.Context) Delta {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	force := p.forceNext
	p.forceNext = false

	dv, err := p.d.DataVersion(ctx)
	if err != nil {
		return Delta{Err: err, PolledAt: now}
	}

	if p.initialized && !force && dv == p.lastDataVersion {
		return Delta{PolledAt: now} // nothing changed
	}

	sessions, err := p.d.ListSessions(ctx)
	if err != nil {
		return Delta{Err: err, PolledAt: now}
	}

	var (
		msgs  []db.Message
		maxID int64
	)
	if !p.initialized {
		msgs, maxID, err = p.d.BackfillMessages(ctx)
	} else {
		msgs, maxID, err = p.d.MessagesSince(ctx, p.lastMaxID)
	}
	if err != nil {
		return Delta{Err: err, PolledAt: now}
	}

	lifecycle := p.diffLifecycle(sessions)

	// Commit new state only after all reads succeeded.
	p.lastDataVersion = dv
	p.lastMaxID = maxID
	p.initialized = true

	return Delta{
		SessionsChanged: true,
		Sessions:        sessions,
		NewMessages:     msgs,
		Lifecycle:       lifecycle,
		PolledAt:        now,
	}
}

// diffLifecycle compares the new session set against the previous snapshot to
// synthesize start/end events, and updates the snapshot. On the very first poll
// (prev empty, not yet initialized) it seeds the snapshot without emitting
// events, so we don't flood with "started" lines for historical sessions.
func (p *Poller) diffLifecycle(sessions []db.Session) []derive.Event {
	seeding := !p.initialized
	next := make(map[string]sessionSnap, len(sessions))
	var events []derive.Event

	for _, s := range sessions {
		_, ended := s.Ended()
		next[s.ID] = sessionSnap{ended: ended}
		if seeding {
			continue
		}
		old, existed := p.prev[s.ID]
		if !existed {
			events = append(events, derive.SessionStartEvent(s))
			if ended {
				events = append(events, derive.SessionEndEvent(s))
			}
			continue
		}
		if !old.ended && ended {
			events = append(events, derive.SessionEndEvent(s))
		}
	}
	p.prev = next
	return events
}
