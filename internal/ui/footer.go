package ui

func (m *model) renderFooter() string {
	if m.mode == modeFilter {
		m.filter.Width = max1(m.width - 30)
		hint := m.st.footer.Render("  enter apply · esc clear")
		return clipANSI(m.filter.View()+hint, m.width)
	}
	m.help.Width = m.width
	return clipANSI(m.help.ShortHelpView(m.keys.ShortHelp()), m.width)
}
