package panels

import tea "github.com/charmbracelet/bubbletea"

// Panel defines a dashboard panel component.
type Panel interface {
	tea.Model
	SetSize(width, height int)
	Focus()
	Blur()
}
