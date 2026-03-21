// Package components provides shared TUI building blocks.
package components

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ScrollablePanel wraps bubbles/viewport for scrollable content areas.
// It provides a reusable scrolling component for dashboard panels.
type ScrollablePanel struct {
	vp          viewport.Model
	content     string
	lastContent string // Track content for change detection
}

// NewScrollablePanel creates a new scrollable panel with the given dimensions.
func NewScrollablePanel(width, height int) *ScrollablePanel {
	vp := viewport.New(width, height)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3
	return &ScrollablePanel{
		vp: vp,
	}
}

// SetContent updates the viewport content.
// Only resets scroll position if content actually changed.
func (sp *ScrollablePanel) SetContent(s string) {
	if s == sp.lastContent {
		return
	}
	sp.content = s
	sp.lastContent = s
	sp.vp.SetContent(s)
}

// SetSize updates the viewport dimensions.
func (sp *ScrollablePanel) SetSize(width, height int) {
	if width == sp.vp.Width && height == sp.vp.Height {
		return
	}

	yOffset := sp.vp.YOffset
	sp.vp.Width = width
	sp.vp.Height = height
	if sp.content != "" {
		sp.vp.SetContent(sp.content)
		sp.vp.SetYOffset(yOffset)
	}
}

// Width returns the current viewport width.
func (sp *ScrollablePanel) Width() int {
	return sp.vp.Width
}

// Height returns the current viewport height.
func (sp *ScrollablePanel) Height() int {
	return sp.vp.Height
}

// Update handles input messages and returns any commands.
func (sp *ScrollablePanel) Update(msg tea.Msg) (*ScrollablePanel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyHome:
			sp.GotoTop()
			return sp, nil
		case tea.KeyEnd:
			sp.GotoBottom()
			return sp, nil
		}
	}

	var cmd tea.Cmd
	sp.vp, cmd = sp.vp.Update(msg)
	return sp, cmd
}

// View returns the rendered viewport content.
func (sp *ScrollablePanel) View() string {
	return sp.vp.View()
}

// ScrollPercent returns the current scroll position as a percentage (0.0-1.0).
func (sp *ScrollablePanel) ScrollPercent() float64 {
	return sp.vp.ScrollPercent()
}

// AtTop returns true if the viewport is scrolled to the top.
func (sp *ScrollablePanel) AtTop() bool {
	return sp.vp.AtTop()
}

// AtBottom returns true if the viewport is scrolled to the bottom.
func (sp *ScrollablePanel) AtBottom() bool {
	return sp.vp.AtBottom()
}

// GotoTop scrolls to the top of the content.
func (sp *ScrollablePanel) GotoTop() {
	sp.vp.GotoTop()
}

// GotoBottom scrolls to the bottom of the content.
func (sp *ScrollablePanel) GotoBottom() {
	sp.vp.GotoBottom()
}

// TotalLines returns the total number of lines in the content.
func (sp *ScrollablePanel) TotalLines() int {
	if sp.content == "" {
		return 0
	}
	return sp.vp.TotalLineCount()
}

// VisibleLines returns the number of lines visible in the viewport.
func (sp *ScrollablePanel) VisibleLines() int {
	if sp.vp.Height <= 0 {
		return 0
	}
	return sp.vp.VisibleLineCount()
}

// NeedsScrolling returns true if content exceeds viewport height.
func (sp *ScrollablePanel) NeedsScrolling() bool {
	return sp.TotalLines() > sp.VisibleLines()
}

// NeedsScroll is an alias for NeedsScrolling for API consistency.
func (sp *ScrollablePanel) NeedsScroll() bool {
	return sp.NeedsScrolling()
}

// YOffset returns the current vertical scroll position in lines.
func (sp *ScrollablePanel) YOffset() int {
	return sp.vp.YOffset
}

// LineDown scrolls down by n lines.
func (sp *ScrollablePanel) LineDown(n int) {
	sp.vp.LineDown(n)
}

// LineUp scrolls up by n lines.
func (sp *ScrollablePanel) LineUp(n int) {
	sp.vp.LineUp(n)
}

// HalfPageDown scrolls down by half a viewport.
func (sp *ScrollablePanel) HalfPageDown() {
	sp.vp.HalfPageDown()
}

// HalfPageUp scrolls up by half a viewport.
func (sp *ScrollablePanel) HalfPageUp() {
	sp.vp.HalfPageUp()
}

// ScrollState returns the current scroll state for use with ScrollFooter.
func (sp *ScrollablePanel) ScrollState() ScrollState {
	totalLines := sp.TotalLines()
	visibleLines := sp.VisibleLines()

	if totalLines == 0 || visibleLines == 0 {
		return ScrollState{}
	}

	firstVisible := sp.YOffset()
	if firstVisible < 0 {
		firstVisible = 0
	}
	lastVisible := firstVisible + visibleLines - 1
	if lastVisible >= totalLines {
		lastVisible = totalLines - 1
	}

	return ScrollState{
		FirstVisible: firstVisible,
		LastVisible:  lastVisible,
		TotalItems:   totalLines,
	}
}

// ScrollIndicator returns the scroll indicator string based on scroll state.
// Uses the existing ScrollState helper for consistency.
func (sp *ScrollablePanel) ScrollIndicator() string {
	return sp.ScrollState().Indicator()
}
