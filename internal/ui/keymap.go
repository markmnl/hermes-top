package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds all key bindings. Two help views are exposed: normal mode and
// filter mode.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Tab      key.Binding
	Filter   key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Expand   key.Binding
	Collapse key.Binding
	Refresh  key.Binding
	Quit     key.Binding

	// filter-mode
	Apply  key.Binding
	Cancel key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Tab:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pane")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Expand:   key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("enter", "expand")),
		Collapse: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "collapse")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),

		Apply:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
	}
}

// ShortHelp is the normal-mode footer hint set.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Tab, k.Expand, k.Filter, k.Top, k.Bottom, k.Refresh, k.Quit}
}

// FullHelp satisfies help.KeyMap (single row).
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
