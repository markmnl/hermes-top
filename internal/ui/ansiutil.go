package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ansiWidth returns the display width of a possibly-styled string.
func ansiWidth(s string) int { return ansi.StringWidth(s) }

// clipANSI truncates a possibly-styled string to w display columns, preserving
// ANSI escape sequences.
func clipANSI(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "")
}

// padANSILine truncates a styled line to w columns and right-pads it with
// spaces to exactly w columns, so pane rows fill their width uniformly.
func padANSILine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "")
	if gap := w - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}
