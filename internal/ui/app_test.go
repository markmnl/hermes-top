package ui

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/markmnl/hermes-top/internal/db"
	"github.com/markmnl/hermes-top/internal/derive"
	"github.com/markmnl/hermes-top/internal/poll"
)

func newTestModel() *model {
	ti := textinput.New()
	ti.Prompt = "/"
	return &model{
		ctx:             context.Background(),
		interval:        400 * time.Millisecond,
		st:              newStyles(),
		keys:            newKeyMap(),
		help:            help.New(),
		filter:          ti,
		byID:            make(map[string]*db.Session),
		actions:         make(map[string][]*derive.Action),
		actionByCall:    make(map[string]*derive.Action),
		expandedActions: make(map[int64]bool),
		expandedEvents:  make(map[int64]bool),
		timelineFollow:  true,
		eventsFollow:    true,
		timelineCursor:  -1,
		eventsCursor:    -1,
		now:             time.Unix(1783128300, 0),
	}
}

func sampleDelta(now time.Time) poll.Delta {
	sessions := []db.Session{
		{ID: "20260704_090016_5945c6", Source: "cli", Model: sql.NullString{String: "qwen3.6:35b", Valid: true},
			StartedAt: float64(now.Add(-30 * time.Second).Unix()), MessageCount: 3, ToolCallCount: 1,
			InputTokens: 1200, OutputTokens: 340,
			LastMsgAt: sql.NullFloat64{Float64: float64(now.Add(-2 * time.Second).Unix()), Valid: true}},
		{ID: "20260703_082655_b67460", Source: "tui", Model: sql.NullString{String: "gemma4:26b", Valid: true},
			StartedAt: float64(now.Add(-2 * time.Hour).Unix()),
			EndedAt:   sql.NullFloat64{Float64: float64(now.Add(-1 * time.Hour).Unix()), Valid: true},
			EndReason: sql.NullString{String: "tui_shutdown", Valid: true}, MessageCount: 10, ToolCallCount: 4,
			InputTokens: 5000, OutputTokens: 900},
	}
	msgs := []db.Message{
		{ID: 1, SessionID: "20260704_090016_5945c6", Role: "user", Content: "list files", Timestamp: float64(now.Add(-20 * time.Second).Unix())},
		{ID: 2, SessionID: "20260704_090016_5945c6", Role: "assistant",
			ToolCalls: sql.NullString{String: `[{"id":"call_x","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"ls\"}"}}]`, Valid: true},
			Timestamp: float64(now.Add(-18 * time.Second).Unix())},
		{ID: 3, SessionID: "20260704_090016_5945c6", Role: "tool",
			Content: `{"error":"denied by user"}`, ToolCallID: sql.NullString{String: "call_x", Valid: true},
			ToolName: sql.NullString{String: "bash", Valid: true}, Timestamp: float64(now.Add(-16 * time.Second).Unix())},
	}
	return poll.Delta{SessionsChanged: true, Sessions: sessions, NewMessages: msgs, PolledAt: now}
}

func TestModelRenderSmoke(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.now = time.Unix(1783128300, 0)
	m.applyDelta(sampleDelta(m.now))
	m.rebuildViewports()

	view := m.View()
	if view == "" {
		t.Fatal("empty view")
	}
	for _, want := range []string{"hermes-top", "sessions", "events", "5945c6"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}

	// The error tool result should have been paired into an error action.
	acts := m.actions["20260704_090016_5945c6"]
	if len(acts) != 1 {
		t.Fatalf("want 1 action, got %d", len(acts))
	}
	if !acts[0].Done() {
		t.Error("action should be paired with its result")
	}
}

func TestSmallTerminalFallback(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})
	if got := m.View(); !strings.Contains(got, "too small") {
		t.Errorf("want too-small fallback, got %q", got)
	}
}

func TestFilterModeGatesKeys(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.applyDelta(sampleDelta(m.now))

	// enter filter mode
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if m.mode != modeFilter {
		t.Fatal("should be in filter mode after /")
	}
	// typing 'q' must NOT quit; it should go into the filter text
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.mode != modeFilter {
		t.Error("q in filter mode should not exit filter mode")
	}
	if !strings.Contains(m.filter.Value(), "q") {
		t.Errorf("q should be typed into filter, got %q", m.filter.Value())
	}
	// esc clears and exits
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal || m.filterText != "" {
		t.Errorf("esc should clear filter and return to normal, mode=%v filter=%q", m.mode, m.filterText)
	}
}

func TestTabCyclesFocus(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	start := m.focus
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus == start {
		t.Error("tab should change focus")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != start {
		t.Errorf("three tabs should return to start focus, got %v want %v", m.focus, start)
	}
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

func TestTimelineExpand(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.applyDelta(sampleDelta(m.now))
	m.focus = paneTimeline
	m.rebuildViewports()

	before, _, _ := m.timelineContent()
	m.toggleExpand(false, false) // expand cursor action
	after, _, _ := m.timelineContent()

	if lineCount(after) <= lineCount(before) {
		t.Errorf("expanding an action should add lines: before=%d after=%d",
			lineCount(before), lineCount(after))
	}
	// the expanded block should contain the pretty-printed argument key
	if !strings.Contains(after, "cmd") {
		t.Errorf("expanded action should show argument JSON, got:\n%s", after)
	}

	m.toggleExpand(true, false) // collapse
	if collapsed, _, _ := m.timelineContent(); lineCount(collapsed) != lineCount(before) {
		t.Errorf("collapse should restore line count: got %d want %d",
			lineCount(collapsed), lineCount(before))
	}
}

func TestEventsExpand(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.applyDelta(sampleDelta(m.now))
	m.focus = paneEvents
	m.rebuildViewports()

	before, _, _ := m.eventsContent()
	m.toggleExpand(false, false)
	after, _, _ := m.eventsContent()
	if lineCount(after) <= lineCount(before) {
		t.Errorf("expanding an event should add lines: before=%d after=%d",
			lineCount(before), lineCount(after))
	}
}

func TestCursorFollowsNewData(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.focus = paneEvents
	m.applyDelta(sampleDelta(m.now))
	m.rebuildViewports()
	// following: cursor pinned to last visible event
	if m.eventsCursor != m.visibleEventCount()-1 {
		t.Errorf("following cursor should be at last, got %d of %d", m.eventsCursor, m.visibleEventCount())
	}
	// move up → breaks follow
	m.moveEventsCursor(-1)
	if m.eventsFollow {
		t.Error("moving up should break follow")
	}
	prev := m.eventsCursor
	// new data arrives; non-following cursor should not jump to bottom
	d := sampleDelta(m.now)
	d.NewMessages = d.NewMessages[:0] // sessions-only refresh
	m.applyDelta(d)
	m.rebuildViewports()
	if m.eventsCursor != prev {
		t.Errorf("non-following cursor should stay put: got %d want %d", m.eventsCursor, prev)
	}
}

func TestSelectionStableAcrossReorder(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.applyDelta(sampleDelta(m.now))
	// select the second (ended) session
	m.selected = "20260703_082655_b67460"
	// a reorder that keeps both sessions but flips order
	d := sampleDelta(m.now)
	d.Sessions[0], d.Sessions[1] = d.Sessions[1], d.Sessions[0]
	m.applyDelta(d)
	if m.selected != "20260703_082655_b67460" {
		t.Errorf("selection should survive reorder, got %q", m.selected)
	}
}
