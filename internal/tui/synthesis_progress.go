package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/tui/components"
	"github.com/Dicklesworthstone/ntm/internal/tui/layout"
	"github.com/Dicklesworthstone/ntm/internal/tui/styles"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// SynthesisPhase represents the current synthesis lifecycle phase.
type SynthesisPhase string

const (
	SynthesisWaiting      SynthesisPhase = "waiting"
	SynthesisCollecting   SynthesisPhase = "collecting"
	SynthesisSynthesizing SynthesisPhase = "synthesizing"
	SynthesisComplete     SynthesisPhase = "complete"
)

// String returns the phase name.
func (s SynthesisPhase) String() string {
	return string(s)
}

// SynthesisProgressLine represents a per-window progress line.
type SynthesisProgressLine struct {
	Pane     string
	ModeCode string
	Tier     ensemble.ModeTier
	Tokens   int
	Status   string
}

// SynthesisProgressData captures the display data for synthesis progress.
type SynthesisProgressData struct {
	Phase           SynthesisPhase
	Ready           int
	Pending         int
	Total           int
	Progress        float64
	Lines           []SynthesisProgressLine
	Strategy        string
	SynthesizerMode string
	InputTokens     int
	ResultPath      string
}

// SynthesisProgressMsg updates the synthesis progress model.
type SynthesisProgressMsg struct {
	Data SynthesisProgressData
}

// SynthesisProgress renders the synthesis progress UI component.
type SynthesisProgress struct {
	Width int

	data         SynthesisProgressData
	bar          components.ProgressBar
	lastPhase    SynthesisPhase
	lastTokens   int
	lastProgress float64
}

// NewSynthesisProgress creates a new synthesis progress component.
func NewSynthesisProgress(width int) *SynthesisProgress {
	barWidth := clampInt(width-10, 12, 60)
	bar := components.NewProgressBar(barWidth)
	bar.ShowPercent = true
	bar.ShowLabel = false
	bar.Animated = true

	return &SynthesisProgress{
		Width:     width,
		bar:       bar,
		lastPhase: SynthesisWaiting,
	}
}

// SetData updates the progress component state.
func (s *SynthesisProgress) SetData(data SynthesisProgressData) {
	s.applyData(data)
}

// Init implements tea.Model.
func (s *SynthesisProgress) Init() tea.Cmd {
	return s.bar.Init()
}

// Update implements tea.Model.
func (s *SynthesisProgress) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if update, ok := msg.(SynthesisProgressMsg); ok {
		s.applyData(update.Data)
	}
	bar, cmd := s.bar.Update(msg)
	s.bar = bar
	return s, cmd
}

// View renders the synthesis progress component.
func (s *SynthesisProgress) View() string {
	width := s.Width
	if width <= 0 {
		width = 60
	}

	switch s.data.Phase {
	case SynthesisCollecting:
		return s.viewCollecting(width)
	case SynthesisSynthesizing:
		return s.viewSynthesizing(width)
	case SynthesisComplete:
		return s.viewComplete(width)
	default:
		return s.viewWaiting(width)
	}
}

func (s *SynthesisProgress) applyData(data SynthesisProgressData) {
	if data.Phase == "" {
		data.Phase = SynthesisWaiting
	}

	previous := s.data.Phase
	s.data = data
	s.updateProgressBar()

	if previous != data.Phase {
		slog.Debug("synthesis progress phase transition",
			"from", previous.String(),
			"to", data.Phase.String(),
		)
	}

	progress := s.currentProgress()
	totalTokens := s.totalTokens()
	if progress != s.lastProgress || totalTokens != s.lastTokens {
		slog.Debug("synthesis progress metrics",
			"phase", data.Phase.String(),
			"progress", fmt.Sprintf("%.2f", progress),
			"total_tokens", totalTokens,
		)
		s.lastProgress = progress
		s.lastTokens = totalTokens
	}
}

func (s *SynthesisProgress) updateProgressBar() {
	s.bar.SetPercent(s.currentProgress())
}

func (s *SynthesisProgress) currentProgress() float64 {
	if s.data.Progress > 0 {
		return clampFloat(s.data.Progress, 0, 1)
	}
	if s.data.Total > 0 {
		return clampFloat(float64(s.data.Ready)/float64(s.data.Total), 0, 1)
	}
	return 0
}

func (s *SynthesisProgress) totalTokens() int {
	if s.data.InputTokens > 0 {
		return s.data.InputTokens
	}
	total := 0
	for _, line := range s.data.Lines {
		total += line.Tokens
	}
	return total
}

func (s *SynthesisProgress) viewWaiting(width int) string {
	t := theme.Current()
	var content strings.Builder

	ready := s.data.Ready
	pending := s.data.Pending
	total := s.data.Total

	counts := fmt.Sprintf("Ready: %d  Pending: %d", ready, pending)
	if total > 0 {
		counts = fmt.Sprintf("Ready: %d/%d  Pending: %d", ready, total, pending)
	}
	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render(counts) + "\n\n")

	button := lipgloss.NewStyle().
		Foreground(t.Surface2).
		Background(t.Surface1).
		Bold(true).
		Padding(0, 2).
		Render("Start synthesis")
	content.WriteString(button)

	return lipgloss.NewStyle().Width(width).Render(content.String())
}

func (s *SynthesisProgress) viewCollecting(width int) string {
	t := theme.Current()
	var content strings.Builder

	s.bar.Width = clampInt(width-8, 12, 60)
	s.updateProgressBar()

	header := fmt.Sprintf("Collecting outputs — %d/%d ready", s.data.Ready, s.data.Total)
	content.WriteString(lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(header) + "\n")
	content.WriteString(s.bar.View() + "\n")

	if len(s.data.Lines) == 0 {
		content.WriteString(lipgloss.NewStyle().Foreground(t.Overlay).Italic(true).Render("Awaiting window outputs"))
		return lipgloss.NewStyle().Width(width).Render(content.String())
	}

	content.WriteString("\n")
	for _, line := range s.data.Lines {
		content.WriteString(s.renderLine(line, width) + "\n")
	}

	return lipgloss.NewStyle().Width(width).Render(strings.TrimRight(content.String(), "\n"))
}

func (s *SynthesisProgress) viewSynthesizing(width int) string {
	t := theme.Current()
	var content strings.Builder

	s.bar.Width = clampInt(width-8, 12, 60)
	s.updateProgressBar()

	strategy := s.data.Strategy
	if strings.TrimSpace(strategy) == "" {
		strategy = "—"
	}
	mode := s.data.SynthesizerMode
	if strings.TrimSpace(mode) == "" {
		mode = "—"
	}

	content.WriteString(lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render("Synthesizing") + "\n")
	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render(fmt.Sprintf("Strategy: %s", strategy)) + "\n")
	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render(fmt.Sprintf("Synthesizer: %s", mode)) + "\n")
	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render(fmt.Sprintf("Input tokens: %d", s.data.InputTokens)) + "\n")
	content.WriteString(s.bar.View())

	return lipgloss.NewStyle().Width(width).Render(content.String())
}

func (s *SynthesisProgress) viewComplete(width int) string {
	t := theme.Current()
	var content strings.Builder

	badge := styles.TextBadge("DONE", t.Green, t.Base, styles.BadgeOptions{
		Style:      styles.BadgeStyleCompact,
		Bold:       true,
		ShowIcon:   false,
		FixedWidth: 4,
	})
	content.WriteString(lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render("Synthesis complete ") + badge + "\n")

	result := strings.TrimSpace(s.data.ResultPath)
	if result == "" {
		result = "See ensemble output for details"
	}
	resultLine := layout.TruncateWidthDefault(fmt.Sprintf("Results: %s", result), width-2)
	content.WriteString(lipgloss.NewStyle().Foreground(t.Subtext).Render(resultLine))

	return lipgloss.NewStyle().Width(width).Render(content.String())
}

func (s *SynthesisProgress) renderLine(line SynthesisProgressLine, width int) string {
	t := theme.Current()

	pane := shortenPane(line.Pane)
	modeCode := line.ModeCode
	if modeCode == "" {
		modeCode = "—"
	}

	tierChip := renderTierChip(line.Tier)
	tokenText := fmt.Sprintf("%dtok", line.Tokens)

	statusText := strings.TrimSpace(line.Status)
	statusStyle := lipgloss.NewStyle().Foreground(t.Subtext)
	switch strings.ToLower(statusText) {
	case "done", "complete", "ready":
		statusStyle = statusStyle.Foreground(t.Green)
	case "active", "working":
		statusStyle = statusStyle.Foreground(t.Blue)
	case "error", "failed":
		statusStyle = statusStyle.Foreground(t.Red)
	case "pending", "":
		statusStyle = statusStyle.Foreground(t.Overlay)
		statusText = "pending"
	}

	lineText := fmt.Sprintf("%-8s %-4s %s %8s %s",
		pane,
		modeCode,
		tierChip,
		tokenText,
		statusStyle.Render(statusText),
	)

	return lipgloss.NewStyle().Foreground(t.Text).Render(layout.TruncateWidthDefault(lineText, width-2))
}

func renderTierChip(tier ensemble.ModeTier) string {
	if tier == "" {
		return ""
	}
	t := theme.Current()
	opts := styles.BadgeOptions{
		Style:      styles.BadgeStyleCompact,
		Bold:       true,
		ShowIcon:   false,
		FixedWidth: 4,
	}
	switch tier {
	case ensemble.TierCore:
		return styles.TextBadge("CORE", t.Green, t.Base, opts)
	case ensemble.TierAdvanced:
		return styles.TextBadge("ADV", t.Yellow, t.Base, opts)
	case ensemble.TierExperimental:
		return styles.TextBadge("EXP", t.Red, t.Base, opts)
	default:
		return styles.TextBadge(strings.ToUpper(tier.String()), t.Surface1, t.Text, opts)
	}
}

func shortenPane(title string) string {
	if idx := strings.LastIndex(title, "__"); idx != -1 && idx+2 < len(title) {
		return title[idx+2:]
	}
	return title
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
