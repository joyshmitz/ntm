package panels

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Panel defines the common interface all dashboard panels must implement.
// It intentionally mirrors the Bubble Tea model lifecycle so panels can be
// composed inside the dashboard without bespoke glue code.
type Panel interface {
	// Init returns the initial command for the panel.
	Init() tea.Cmd

	// Update applies a message to the panel and returns the updated panel and
	// an optional command to run.
	Update(msg tea.Msg) (Panel, tea.Cmd)

	// View renders the panel.
	View() string

	// Title is the human-readable panel name for headers and focus indicators.
	Title() string

	// Priority is used to order panels when space is constrained.
	Priority() int

	// Keybindings returns the key bindings the panel wants to expose to the
	// dashboard (e.g., for focus or command palette help).
	Keybindings() []key.Binding
}

// PanelConfig captures the shared configuration used when constructing a panel.
// It allows caller-provided metadata (title/priority) and optional keybindings
// to be applied consistently across panel implementations.
type PanelConfig struct {
	Title       string
	Priority    int
	Keybindings []key.Binding
}
