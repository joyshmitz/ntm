// Package components provides reusable animated TUI components
package components

import (
	"time"

	bspinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/ntm/internal/tui/styles"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// SpinnerStyle defines different spinner animations
type SpinnerStyle int

const (
	SpinnerDots SpinnerStyle = iota
	SpinnerLine
	SpinnerBounce
	SpinnerPoints
	SpinnerGlobe
	SpinnerMoon
	SpinnerMonkey
	SpinnerMeter
	SpinnerHamburger
)

// mapStyleToBubbles converts our SpinnerStyle to bubbles spinner preset
func mapStyleToBubbles(s SpinnerStyle) bspinner.Spinner {
	switch s {
	case SpinnerDots:
		return bspinner.MiniDot
	case SpinnerLine:
		return bspinner.Line
	case SpinnerBounce:
		return bspinner.Jump
	case SpinnerPoints:
		return bspinner.Points
	case SpinnerGlobe:
		return bspinner.Globe
	case SpinnerMoon:
		return bspinner.Moon
	case SpinnerMonkey:
		return bspinner.Monkey
	case SpinnerMeter:
		return bspinner.Meter
	case SpinnerHamburger:
		return bspinner.Hamburger
	default:
		return bspinner.MiniDot
	}
}

// Spinner is an animated spinner component that delegates to bubbles/spinner
type Spinner struct {
	inner          bspinner.Model // bubbles spinner for frame data
	lastStyle      SpinnerStyle   // tracks style changes
	Style          SpinnerStyle
	Color          lipgloss.Color
	Frame          int
	FPS            time.Duration
	Label          string
	Gradient       bool
	GradientColors []string
}

// SpinnerTickMsg is sent on each animation tick
type SpinnerTickMsg time.Time

// NewSpinner creates a new spinner with defaults
func NewSpinner() Spinner {
	t := theme.Current()
	bs := mapStyleToBubbles(SpinnerDots)
	inner := bspinner.New(
		bspinner.WithSpinner(bs),
		bspinner.WithStyle(lipgloss.NewStyle().Foreground(t.Mauve)),
	)
	return Spinner{
		inner:     inner,
		lastStyle: SpinnerDots,
		Style:     SpinnerDots,
		Color:     t.Mauve,
		Frame:     0,
		FPS:       time.Millisecond * 80,
		Gradient:  false,
		GradientColors: []string{
			string(t.Blue),
			string(t.Mauve),
			string(t.Pink),
		},
	}
}

// syncInner updates the inner bubbles spinner if Style changed
func (s *Spinner) syncInner() {
	if s.Style != s.lastStyle {
		s.inner.Spinner = mapStyleToBubbles(s.Style)
		s.lastStyle = s.Style
	}
	s.inner.Style = lipgloss.NewStyle().Foreground(s.Color)
}

// Init initializes the spinner
func (s Spinner) Init() tea.Cmd {
	if !styles.AnimationsEnabled() {
		return nil
	}
	return s.tick()
}

// Update handles spinner animation
func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	switch msg.(type) {
	case SpinnerTickMsg:
		if !styles.AnimationsEnabled() {
			return s, nil
		}
		s.syncInner()
		frames := s.inner.Spinner.Frames
		if len(frames) == 0 {
			frames = bspinner.MiniDot.Frames
		}
		s.Frame = (s.Frame + 1) % len(frames)
		return s, s.tick()
	case bspinner.TickMsg:
		// Also accept bubbles tick messages for interoperability
		if !styles.AnimationsEnabled() {
			return s, nil
		}
		s.syncInner()
		var cmd tea.Cmd
		s.inner, cmd = s.inner.Update(msg)
		frames := s.inner.Spinner.Frames
		if len(frames) > 0 {
			s.Frame = (s.Frame + 1) % len(frames)
		}
		return s, cmd
	}
	return s, nil
}

// View renders the spinner
func (s Spinner) View() string {
	s.syncInner()
	frames := s.inner.Spinner.Frames
	if len(frames) == 0 {
		frames = bspinner.MiniDot.Frames
	}
	frame := frames[s.Frame%len(frames)]

	var rendered string
	if s.Gradient && len(s.GradientColors) >= 2 {
		rendered = styles.Shimmer(frame, s.Frame*10, s.GradientColors...)
	} else {
		rendered = lipgloss.NewStyle().Foreground(s.Color).Render(frame)
	}

	if s.Label != "" {
		return rendered + " " + s.Label
	}
	return rendered
}

func (s Spinner) tick() tea.Cmd {
	if !styles.AnimationsEnabled() {
		return nil
	}
	return tea.Tick(s.FPS, func(t time.Time) tea.Msg {
		return SpinnerTickMsg(t)
	})
}

// TickCmd returns the tick command for external use
func (s Spinner) TickCmd() tea.Cmd {
	return s.tick()
}
