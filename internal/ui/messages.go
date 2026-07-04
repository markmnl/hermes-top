package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/markmnl/hermes-top/internal/poll"
)

// tickMsg fires on the refresh interval to trigger the next poll.
type tickMsg struct{}

// pollDoneMsg carries the result of a background poll.
type pollDoneMsg struct {
	delta poll.Delta
}

// scheduleTick schedules the next poll tick after the interval elapses.
func scheduleTick(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg { return tickMsg{} })
}

// pollCmd runs one poll off the UI thread and reports the result.
func pollCmd(ctx context.Context, p *poll.Poller) tea.Cmd {
	return func() tea.Msg {
		return pollDoneMsg{delta: p.Poll(ctx)}
	}
}
