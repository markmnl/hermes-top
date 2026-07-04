package ui

import "github.com/charmbracelet/lipgloss"

// Palette — a clean dark theme with subtle accents. Colors are chosen to read
// on a dark terminal; lipgloss degrades gracefully on limited color terminals.
var (
	colText    = lipgloss.Color("#c0c5ce")
	colDim     = lipgloss.Color("#5c6370")
	colAccent  = lipgloss.Color("#56b6c2") // cyan: focus, selection
	colGreen   = lipgloss.Color("#98c379") // running, ok
	colRed     = lipgloss.Color("#e06c75") // error
	colYellow  = lipgloss.Color("#e5c07b") // pending, stale
	colBlue    = lipgloss.Color("#61afef") // user role
	colMagenta = lipgloss.Color("#c678dd") // assistant role
)

// styles holds every lipgloss.Style, built once at startup and reused, never
// reconstructed per frame.
type styles struct {
	base        lipgloss.Style
	dim         lipgloss.Style
	header      lipgloss.Style
	headerKey   lipgloss.Style
	headerVal   lipgloss.Style
	headerErr   lipgloss.Style
	paneBorder  lipgloss.Style // unfocused pane
	paneFocused lipgloss.Style // focused pane
	paneTitle   lipgloss.Style
	paneTitleFo lipgloss.Style

	selRow  lipgloss.Style
	running lipgloss.Style
	stale   lipgloss.Style
	ended   lipgloss.Style
	ok      lipgloss.Style
	errText lipgloss.Style
	pending lipgloss.Style
	accent  lipgloss.Style
	label   lipgloss.Style
	value   lipgloss.Style

	roleUser      lipgloss.Style
	roleAssistant lipgloss.Style
	roleTool      lipgloss.Style
	roleSystem    lipgloss.Style
	roleSession   lipgloss.Style

	footer    lipgloss.Style
	footerKey lipgloss.Style

	tooSmall lipgloss.Style
}

func newStyles() styles {
	var s styles
	s.base = lipgloss.NewStyle().Foreground(colText)
	s.dim = lipgloss.NewStyle().Foreground(colDim)

	s.header = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.headerKey = lipgloss.NewStyle().Foreground(colDim)
	s.headerVal = lipgloss.NewStyle().Foreground(colText).Bold(true)
	s.headerErr = lipgloss.NewStyle().Foreground(colRed).Bold(true)

	s.paneBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colDim)
	s.paneFocused = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colAccent)
	s.paneTitle = lipgloss.NewStyle().Foreground(colDim).Bold(true)
	s.paneTitleFo = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	s.selRow = lipgloss.NewStyle().Foreground(lipgloss.Color("#1a1c22")).Background(colAccent).Bold(true)
	s.running = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	s.stale = lipgloss.NewStyle().Foreground(colYellow)
	s.ended = lipgloss.NewStyle().Foreground(colDim)
	s.ok = lipgloss.NewStyle().Foreground(colGreen)
	s.errText = lipgloss.NewStyle().Foreground(colRed)
	s.pending = lipgloss.NewStyle().Foreground(colYellow)
	s.accent = lipgloss.NewStyle().Foreground(colAccent)
	s.label = lipgloss.NewStyle().Foreground(colDim)
	s.value = lipgloss.NewStyle().Foreground(colText)

	s.roleUser = lipgloss.NewStyle().Foreground(colBlue)
	s.roleAssistant = lipgloss.NewStyle().Foreground(colMagenta)
	s.roleTool = lipgloss.NewStyle().Foreground(colAccent)
	s.roleSystem = lipgloss.NewStyle().Foreground(colDim)
	s.roleSession = lipgloss.NewStyle().Foreground(colYellow)

	s.footer = lipgloss.NewStyle().Foreground(colDim)
	s.footerKey = lipgloss.NewStyle().Foreground(colText).Bold(true)

	s.tooSmall = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	return s
}

// roleStyle returns the style for a message/event role name.
func (s styles) roleStyle(role string) lipgloss.Style {
	switch role {
	case "user":
		return s.roleUser
	case "assistant":
		return s.roleAssistant
	case "tool":
		return s.roleTool
	case "system":
		return s.roleSystem
	case "session":
		return s.roleSession
	default:
		return s.base
	}
}
