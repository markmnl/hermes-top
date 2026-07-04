# hermes-top

A read-only, `htop`/`btop`-style live terminal dashboard for the
[Hermes Agent](https://github.com/). It watches everything Hermes is doing on
the local machine — sessions, models, tool executions, events, token usage — by
reading Hermes's SQLite `state.db`.

**It never writes to the database and cannot interfere with a running Hermes.**

```
hermes-top  ⠦  sessions 17 (1 running)  ·  tokens 11.0M in / 115.0k out  ·  updated just now
╭─ sessions ─────────╮╭─ session ───────────────────────────────────────────╮
│● 5945c6  cli        ││Fixing DNF Upgrade Certificate Errors                 │
│  qwen3.6:35b 363ktok││status       running                                 │
│■ b67460  tui        ││model        qwen3.6:35b  via custom                  │
│  qwen3.6:35b 745ktok││runtime      22m38s  started 1h ago                   │
│ …                   ││tokens       in 359.5k · out 4.3k                     │
│                     │╰──────────────────────────────────────────────────────╯
│                     │╭─ actions · 12 ──────────────────────────────────────╮
│                     ││09:22:07 ✓ computer_use({"action":"list_apps"})   25ms│
│                     ││09:22:46 ✗ computer_use({"action":"type",…})     42.0s│
╰─────────────────────╯╰──────────────────────────────────────────────────────╯
╭─ events · 646 ───────────────────────────────────────────────────────────────╮
│09:23:27 5945c6 [tool]      ← computer_use: {"error": "denied by user", …}     │
│09:23:34 5945c6 [assistant] I can't type it right now (it was denied)…         │
╰────────────────────────────────────────────────────────────────────────────────╯
↑/k up • ↓/j down • tab pane • / filter • g top • G bottom • r refresh • q quit
```

## Requirements

- Go 1.26+ (only needed to build).
- Hermes Agent **v0.18 or newer**, which must have run at least once so its
  `state.db` exists.

The SQLite driver is [`modernc.org/sqlite`](https://modernc.org/sqlite), a pure
Go implementation — **no CGO, no system libsqlite3**, so the build is a single
static binary and cross-compiles trivially.

## Build & run

```sh
# from the repo root
go build -o hermes-top ./cmd/hermes-top
./hermes-top
```

Or run without installing:

```sh
go run ./cmd/hermes-top
```

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--db PATH` | (auto) | Path to Hermes `state.db`. |
| `--interval DUR` | `400ms` | Refresh interval, clamped to `250ms`–`500ms`. |
| `--dump` | off | Print one text snapshot of the database and exit (no TUI). Handy for scripting or a quick read-only smoke test. |

### Database location

`hermes-top` resolves the database path in this order:

1. `--db PATH` if given (supports `~`).
2. `$HERMES_HOME/state.db` if `HERMES_HOME` is set.
3. `~/.hermes/state.db` (the platform default).

If the file does not exist, it prints the resolved path and a hint, then exits
non-zero.

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑` / `k`, `↓` / `j` | Move the entry cursor in the focused pane |
| `Tab` | Cycle focus: sessions → actions → events |
| `Enter` / `→` | Expand the highlighted action/event to full pretty-printed JSON |
| `←` | Collapse the highlighted entry |
| `/` | Filter events (type to filter live; `Enter` applies, `Esc` clears) |
| `g` | Jump to top of the focused pane |
| `G` | Jump to bottom of the focused pane (re-enables auto-follow) |
| `r` | Force an immediate refresh |
| `q` / `Ctrl-C` | Quit |

In the actions and events panes the highlighted row is marked with `▸`. Panes
auto-follow new rows while the cursor is on the newest entry (like `tail -f`);
move the cursor up and the position freezes as new rows arrive, `G` re-pins.

### JSON display

Tool arguments and results are JSON. By default each entry is a single readable
line — rendered as `key=value` with escapes decoded (so `&` shows as `&`, not
`&`) and subtly syntax-colored. Highlight an entry and press `Enter` to
expand it inline into full, indented, syntax-highlighted JSON; `←` collapses it.
Non-JSON payloads (e.g. an assistant's prose) expand to the full wrapped text.

## What it shows

- **Header** — total sessions, how many are running/stale, aggregate token
  usage, a refresh spinner, and the time since the last refresh (or a red error
  indicator if the DB is temporarily unreadable).
- **Sessions pane (left)** — every non-archived session, running ones first
  (`●` green, pulsing), then stale open ones (`◌` yellow), then ended (`■` dim),
  each ordered by most recent activity.
- **Summary (top-right)** — the selected session's title, status, model &
  provider, id/source, runtime, token breakdown, activity counts, estimated/
  actual cost, and working directory.
- **Actions (middle-right)** — the selected session's tool-call timeline: each
  call with its arguments (inline `key=value`), and once the result arrives,
  `✓`/`✗` plus duration. Press `Enter` on a row to expand its JSON.
- **Events (bottom)** — a scrolling, filterable stream of every message and
  synthesized session start/end event across all sessions; tool calls/results
  render inline and expand to full JSON on `Enter`.

### Session liveness

Hermes marks a session ended by setting `ended_at`; it has no heartbeat, and a
crashed process never sets `ended_at`. So an open session is shown as **running**
only if it has had activity within the last 2 minutes, and **stale** otherwise.
"Stale" means "open but quiet" — never assume it means "crashed".

## Architecture

```
cmd/hermes-top      entry point: flags, DB path resolution, --dump, launch TUI
internal/db         read-only SQLite access: connection, row models, queries
internal/poll       incremental polling: change detection + lifecycle diffing
internal/derive     pure logic: tool-call/result pairing, error sniffing, events
internal/ui         Bubble Tea model, panes, styling, key handling
```

Data flows one way: `db` reads rows → `poll` turns successive reads into a
`Delta` (new sessions, new messages, synthesized lifecycle events) → `derive`
converts rows into `Action`s and `Event`s → `ui` folds each `Delta` into its
display state and renders. The UI polls on a `tea.Tick`; each poll runs off the
UI thread and reports back as a message, so rendering never blocks on the DB.

### Efficiency

- **Change detection.** Every tick reads `PRAGMA data_version` (one integer). If
  it hasn't changed since the last poll, nothing else runs — idle polling does
  almost no work.
- **Incremental reads.** Messages have a monotonic autoincrement `id`; each poll
  fetches only `WHERE id > <last seen id>`. The session list is re-queried only
  when `data_version` changed.
- **Bounded memory.** Raw messages are folded into actions/events and dropped;
  the event stream is capped (5000) and per-session action history is capped.

### Read-only guarantees

Interfering with a live SQLite writer is the one real risk for a tool like this,
so read-only-ness is enforced at several layers:

1. The connection is opened with `mode=ro` (SQLite refuses writes at the VFS
   layer) **and** `PRAGMA query_only(1)`.
2. All reads are short autocommit statements — the tool never holds a read
   transaction open, so it never pins Hermes's write-ahead log or blocks its
   checkpoints.
3. It never issues `wal_checkpoint`, `VACUUM`, or any DDL.

A single dedicated connection is held for the process lifetime (required for
`data_version` change detection to be meaningful) with a short busy timeout, so
if Hermes momentarily holds a lock the tool skips that poll rather than waiting.

The `internal/db` test suite includes a check that a write on the connection is
rejected with a read-only error. You can also confirm externally:

```sh
stat -c '%y %s' ~/.hermes/state.db ~/.hermes/state.db-wal   # before
./hermes-top                                                # run a while, quit
stat -c '%y %s' ~/.hermes/state.db ~/.hermes/state.db-wal   # unchanged
```

(`state.db-shm` may change: SQLite WAL readers legitimately update shared-memory
read marks. `state.db` and `state.db-wal` are never modified by hermes-top.)

## Development

```sh
go test ./...     # unit tests (derive logic, db access & read-only proof, UI)
go vet ./...
go build ./...
```

## License

MIT
