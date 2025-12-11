package panels

import (
	"fmt"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/tui/styles"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AgentMetric represents token usage for a single agent
type AgentMetric struct {
	Name       string
	Type       string // "cc", "cod", "gmi"
	Tokens     int
	Cost       float64
	ContextPct float64
}

// MetricsData holds the data for the metrics panel
type MetricsData struct {
	TotalTokens int
	TotalCost   float64
	Agents      []AgentMetric
}

// MetricsPanel displays session token usage and costs
type MetricsPanel struct {
	width   int
	height  int
	focused bool
	data    MetricsData
	theme   theme.Theme
}

// NewMetricsPanel creates a new metrics panel
func NewMetricsPanel() *MetricsPanel {
	return &MetricsPanel{
		theme: theme.Current(),
	}
}

// Init implements tea.Model
func (m *MetricsPanel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (m *MetricsPanel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

// SetSize sets the panel dimensions
func (m *MetricsPanel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Focus marks the panel as focused
func (m *MetricsPanel) Focus() {
	m.focused = true
}

// Blur marks the panel as unfocused
func (m *MetricsPanel) Blur() {
	m.focused = false
}

// SetData updates the panel data
func (m *MetricsPanel) SetData(data MetricsData) {
	m.data = data
}

// View renders the panel
func (m *MetricsPanel) View() string {
	t := m.theme

	// Create border style based on focus
	borderColor := t.Surface1
	if m.focused {
		borderColor = t.Primary
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width-2).
		Height(m.height-2).
		Padding(0, 1)

	var content strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Lavender).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(t.Surface1).
		Width(m.width - 4).
		Align(lipgloss.Center)

	content.WriteString(headerStyle.Render("Metrics & Usage") + "\n\n")

	// Total Usage Bar
	// Calculate total context limit (heuristic: sum of agents?)
	// Or just show total tokens.
	// For bar, we need a max. Let's assume 1M tokens as a reference scale for "heavy session".
	const refScale = 1000000.0
	totalPct := float64(m.data.TotalTokens) / refScale
	if totalPct > 1.0 {
		totalPct = 1.0
	}

	bar := styles.ProgressBar(totalPct, m.width-6, "█", "░", string(t.Blue), string(t.Pink))

	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render("Session Total") + "\n")
	content.WriteString(bar + "\n")

	stats := fmt.Sprintf("%d tokens  •  $%.2f est.", m.data.TotalTokens, m.data.TotalCost)
	content.WriteString(lipgloss.NewStyle().Foreground(t.Text).Align(lipgloss.Right).Width(m.width-6).Render(stats) + "\n\n")

	// Per-Agent Bars
	// Only show top N agents if space is limited
	availHeight := m.height - 10 // approx header/footer usage
	if availHeight < 0 {
		availHeight = 0
	}

	for i, agent := range m.data.Agents {
		if i >= availHeight/2 { // 2 lines per agent
			content.WriteString(lipgloss.NewStyle().Foreground(t.Overlay).Render(fmt.Sprintf("...and %d more", len(m.data.Agents)-i)) + "\n")
			break
		}

		// Agent label
		valStyle := lipgloss.NewStyle().Foreground(t.Overlay)

		var typeColor lipgloss.Color
		switch agent.Type {
		case "cc":
			typeColor = t.Claude
		case "cod":
			typeColor = t.Codex
		case "gmi":
			typeColor = t.Gemini
		default:
			typeColor = t.Green
		}

		name := lipgloss.NewStyle().Foreground(typeColor).Bold(true).Render(agent.Name)
		info := fmt.Sprintf("%d tok ($%.2f)", agent.Tokens, agent.Cost)

		// Space between name and info
		gap := m.width - 6 - lipgloss.Width(name) - lipgloss.Width(info)
		if gap < 1 {
			gap = 1
		}

		line := name + strings.Repeat(" ", gap) + valStyle.Render(info)
		content.WriteString(line + "\n")

		// Mini bar
		miniBar := styles.ProgressBar(agent.ContextPct/100.0, m.width-6, "━", "┄", string(typeColor))
		content.WriteString(miniBar + "\n")
	}

	return boxStyle.Render(content.String())
}
