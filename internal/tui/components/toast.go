// Package components provides shared TUI building blocks.
package components

import (
	"strings"
	"time"

	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dicklesworthstone/ntm/internal/tui/styles"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// ToastLevel defines the severity/color of a toast notification.
type ToastLevel int

const (
	ToastInfo    ToastLevel = iota // Blue info toast
	ToastSuccess                   // Green success toast
	ToastWarning                   // Yellow warning toast
	ToastError                     // Red error toast
)

// Toast represents a single ephemeral notification.
type Toast struct {
	ID        string        // Unique identifier for dedup
	Message   string        // Display text
	Level     ToastLevel    // Severity level
	CreatedAt time.Time     // When the toast was created
	Duration  time.Duration // How long to display (0 = default 4s)

	// Enhanced toast options
	Persistent bool    // If true, don't auto-dismiss (requires explicit Dismiss)
	Progress   float64 // Progress value 0.0-1.0 (only used if > 0)

	// Spring animation fields (internal)
	offsetX    float64          // Current horizontal offset (pixels from final position)
	offsetXVel float64          // Current velocity for spring physics
	offsetY    float64          // Current vertical offset for stack repositioning
	offsetYVel float64          // Velocity for Y spring
	targetY    float64          // Target Y position in stack
	spring     harmonica.Spring // Spring physics engine for X
	springY    harmonica.Spring // Spring physics engine for Y
	dismissed  bool             // Whether toast is animating out
}

// DefaultToastDuration is the default display time for toasts.
const DefaultToastDuration = 4 * time.Second

// MaxToasts is the maximum number of toasts displayed simultaneously.
const MaxToasts = 4

// MaxToastHistory is the maximum number of dismissed toasts to remember.
const MaxToastHistory = 20

// ToastManager tracks active toasts with automatic expiry.
type ToastManager struct {
	toasts  []Toast
	history []Toast              // Ring buffer of dismissed toasts
	seen    map[string]time.Time // Dedup: ID -> last seen time
}

// NewToastManager creates a new toast manager.
func NewToastManager() *ToastManager {
	return &ToastManager{
		seen: make(map[string]time.Time),
	}
}

// Push adds a toast notification. Deduplicates by ID within the toast duration.
func (tm *ToastManager) Push(toast Toast) {
	if toast.Duration == 0 {
		toast.Duration = DefaultToastDuration
	}
	if toast.CreatedAt.IsZero() {
		toast.CreatedAt = time.Now()
	}

	// Initialize spring animation (slide in from right)
	if styles.ReducedMotionEnabled() {
		toast.offsetX = 0.0
		toast.offsetXVel = 0.0
		toast.offsetY = 0.0
		toast.offsetYVel = 0.0
	} else {
		// Create spring: 60 FPS, frequency 6.0 Hz, damping 0.4 (slightly underdamped for bounce)
		toast.spring = harmonica.NewSpring(harmonica.FPS(60), 6.0, 0.4)
		toast.springY = harmonica.NewSpring(harmonica.FPS(60), 8.0, 0.5) // Faster Y spring for repositioning
		toast.offsetX = 40.0                                             // Start 40 chars to the right (offscreen)
		toast.offsetXVel = 0.0
		toast.offsetY = 0.0
		toast.offsetYVel = 0.0
	}

	// Dedup check
	if toast.ID != "" {
		if lastSeen, ok := tm.seen[toast.ID]; ok {
			if time.Since(lastSeen) < toast.Duration {
				return // Skip duplicate
			}
		}
		tm.seen[toast.ID] = toast.CreatedAt
	}

	tm.toasts = append(tm.toasts, toast)

	// Trim to max
	if len(tm.toasts) > MaxToasts {
		tm.toasts = tm.toasts[len(tm.toasts)-MaxToasts:]
	}
}

// Tick prunes expired toasts and updates spring animations. Call on each dashboard tick.
func (tm *ToastManager) Tick() {
	now := time.Now()
	reducedMotion := styles.ReducedMotionEnabled()

	// Update spring animations for all toasts
	if !reducedMotion {
		for i := range tm.toasts {
			t := &tm.toasts[i]
			// Calculate target X position: 0 for active, 60 for dismissed (slide out right)
			targetX := 0.0
			if t.dismissed {
				targetX = 60.0
			}
			t.offsetX, t.offsetXVel = t.spring.Update(t.offsetX, t.offsetXVel, targetX)
			// Update Y position for stack repositioning
			t.offsetY, t.offsetYVel = t.springY.Update(t.offsetY, t.offsetYVel, t.targetY)
		}
	}

	// Prune expired toasts (keep if not expired or still animating out)
	active := tm.toasts[:0]
	for _, t := range tm.toasts {
		dur := t.Duration
		if dur == 0 {
			dur = DefaultToastDuration
		}
		// Persistent toasts never expire
		expired := !t.Persistent && now.Sub(t.CreatedAt) >= dur

		if expired && !t.dismissed {
			// Start dismiss animation and add to history
			t.dismissed = true
			tm.addToHistory(t)
		}

		// Reduced motion suppresses slide animations, so dismissed toasts should
		// disappear on the next tick instead of waiting for offsetX to advance.
		keep := !t.dismissed && !expired
		if t.dismissed && !reducedMotion && t.offsetX < 55.0 {
			keep = true
		}
		if keep {
			active = append(active, t)
		}
	}
	tm.toasts = active

	// Clean old dedup entries
	for id, seen := range tm.seen {
		if now.Sub(seen) > 30*time.Second {
			delete(tm.seen, id)
		}
	}
}

// Count returns the number of active toasts.
func (tm *ToastManager) Count() int {
	return len(tm.toasts)
}

// IsAnimating returns true if any toast is currently animating (slide-in or slide-out).
// Use this to request faster tick rates during animation.
func (tm *ToastManager) IsAnimating() bool {
	if styles.ReducedMotionEnabled() {
		return false
	}
	for _, t := range tm.toasts {
		// Animating in: offset > 0.5 and not dismissed
		if !t.dismissed && t.offsetX > 0.5 {
			return true
		}
		// Animating out: dismissed and offset < 55
		if t.dismissed && t.offsetX < 55.0 {
			return true
		}
	}
	return false
}

// Dismiss marks a toast for removal, triggering the slide-out animation.
func (tm *ToastManager) Dismiss(id string) bool {
	for i := range tm.toasts {
		if tm.toasts[i].ID == id && !tm.toasts[i].dismissed {
			tm.toasts[i].dismissed = true
			tm.addToHistory(tm.toasts[i])
			tm.updateStackTargets()
			return true
		}
	}
	return false
}

// addToHistory adds a dismissed toast to the history ring buffer.
func (tm *ToastManager) addToHistory(t Toast) {
	tm.history = append(tm.history, t)
	if len(tm.history) > MaxToastHistory {
		tm.history = tm.history[len(tm.history)-MaxToastHistory:]
	}
}

// updateStackTargets recalculates Y target positions for remaining toasts.
func (tm *ToastManager) updateStackTargets() {
	// Each toast is ~3 lines tall; dismissed toasts leave gaps that animate closed
	for i := range tm.toasts {
		if !tm.toasts[i].dismissed {
			tm.toasts[i].targetY = 0 // All active toasts target Y=0 in their slot
		}
	}
}

// History returns the dismissed toast history (most recent last).
func (tm *ToastManager) History() []Toast {
	return tm.history
}

// HistoryCount returns the number of toasts in history.
func (tm *ToastManager) HistoryCount() int {
	return len(tm.history)
}

// ClearHistory removes all toasts from history.
func (tm *ToastManager) ClearHistory() {
	tm.history = nil
}

// PushPersistent adds a toast that won't auto-dismiss.
func (tm *ToastManager) PushPersistent(id, message string, level ToastLevel) {
	tm.Push(Toast{
		ID:         id,
		Message:    message,
		Level:      level,
		Persistent: true,
	})
}

// PushProgress adds a progress toast (0.0-1.0 progress bar).
func (tm *ToastManager) PushProgress(id, message string, progress float64) {
	tm.Push(Toast{
		ID:         id,
		Message:    message,
		Level:      ToastInfo,
		Progress:   progress,
		Persistent: true, // Progress toasts don't auto-dismiss
	})
}

// UpdateProgress updates the progress of an existing progress toast.
// Returns false if the toast doesn't exist.
func (tm *ToastManager) UpdateProgress(id string, progress float64) bool {
	for i := range tm.toasts {
		if tm.toasts[i].ID == id {
			tm.toasts[i].Progress = progress
			// Auto-dismiss when progress reaches 1.0
			if progress >= 1.0 {
				tm.toasts[i].Persistent = false
				tm.toasts[i].Duration = 1 * time.Second
				tm.toasts[i].CreatedAt = time.Now()
			}
			return true
		}
	}
	return false
}

// ToastAtPosition returns the toast ID at the given Y offset within the toast stack.
// Returns empty string if no toast at that position. This is used for click-to-dismiss.
// The yOffset is relative to the top of the toast stack (not absolute screen position).
func (tm *ToastManager) ToastAtPosition(yOffset int) string {
	if len(tm.toasts) == 0 || yOffset < 0 {
		return ""
	}

	// Each toast occupies ~3 lines (1 content + 2 border/padding), or 4 with progress bar
	currentY := 0
	for _, t := range tm.toasts {
		toastHeight := 3
		if t.Progress > 0 {
			toastHeight = 4 // Extra line for progress bar
		}
		if yOffset >= currentY && yOffset < currentY+toastHeight {
			return t.ID
		}
		currentY += toastHeight
	}
	return ""
}

// DismissAll dismisses all active toasts (used for clearing the stack).
func (tm *ToastManager) DismissAll() {
	for i := range tm.toasts {
		if !tm.toasts[i].dismissed {
			tm.toasts[i].dismissed = true
			tm.addToHistory(tm.toasts[i])
		}
	}
	tm.updateStackTargets()
}

// ToastStackHeight returns the total rendered height of the toast stack.
func (tm *ToastManager) ToastStackHeight() int {
	height := 0
	for _, t := range tm.toasts {
		if t.Progress > 0 {
			height += 4 // Content + progress bar + borders
		} else {
			height += 3 // Content + borders
		}
	}
	return height
}

// RenderToasts renders all active toasts as a vertical stack for overlay.
// Designed to be positioned in the bottom-right corner of the dashboard.
func (tm *ToastManager) RenderToasts(maxWidth int) string {
	if len(tm.toasts) == 0 || maxWidth <= 0 {
		return ""
	}

	t := theme.Current()
	toastWidth := maxWidth
	if toastWidth > 60 {
		toastWidth = 60
	}
	if toastWidth < 20 {
		toastWidth = 20
	}

	var rendered []string
	for _, toast := range tm.toasts {
		var bgColor, fgColor, iconColor lipgloss.Color
		var icon string

		switch toast.Level {
		case ToastSuccess:
			bgColor = t.Surface0
			fgColor = t.Green
			iconColor = t.Green
			icon = "✓"
		case ToastWarning:
			bgColor = t.Surface0
			fgColor = t.Yellow
			iconColor = t.Yellow
			icon = "⚠"
		case ToastError:
			bgColor = t.Surface0
			fgColor = t.Red
			iconColor = t.Red
			icon = "✗"
		default: // ToastInfo
			bgColor = t.Surface0
			fgColor = t.Blue
			iconColor = t.Blue
			icon = "ℹ"
		}

		// Calculate remaining time for fade effect
		elapsed := time.Since(toast.CreatedAt)
		dur := toast.Duration
		if dur == 0 {
			dur = DefaultToastDuration
		}
		remaining := dur - elapsed

		// Dim the toast as it approaches expiry (last 25%)
		if remaining < dur/4 {
			fgColor = t.Overlay
			iconColor = t.Overlay
		}

		iconStyled := lipgloss.NewStyle().
			Foreground(iconColor).
			Bold(true).
			Render(icon)

		// Truncate message to fit by visual width (not rune count),
		// so wide characters (CJK, emojis) are handled correctly.
		msg := toast.Message
		msgMaxWidth := toastWidth - 6 // icon + padding + border
		if msgMaxWidth < 10 {
			msgMaxWidth = 10
		}
		if lipgloss.Width(msg) > msgMaxWidth {
			runes := []rune(msg)
			truncated := make([]rune, 0, len(runes))
			visWidth := 0
			for _, r := range runes {
				rw := lipgloss.Width(string(r))
				if visWidth+rw+1 > msgMaxWidth { // +1 for "…"
					break
				}
				truncated = append(truncated, r)
				visWidth += rw
			}
			msg = string(truncated) + "…"
		}

		msgStyled := lipgloss.NewStyle().
			Foreground(fgColor).
			Render(msg)

		content := iconStyled + " " + msgStyled

		// Add progress bar for progress toasts
		if toast.Progress > 0 {
			barWidth := toastWidth - 8 // Account for padding and borders
			if barWidth < 10 {
				barWidth = 10
			}
			filled := int(float64(barWidth) * toast.Progress)
			if filled > barWidth {
				filled = barWidth
			}
			empty := barWidth - filled
			progressBar := lipgloss.NewStyle().Foreground(fgColor).Render(strings.Repeat("█", filled)) +
				lipgloss.NewStyle().Foreground(t.Surface1).Render(strings.Repeat("░", empty))
			content = content + "\n" + progressBar
		}

		toastBox := lipgloss.NewStyle().
			Background(bgColor).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(fgColor).
			Padding(0, 1).
			Width(toastWidth).
			Render(content)

		// Apply horizontal offset from spring animation
		if toast.offsetX > 0.5 {
			// Pad with spaces to create the offset effect
			offset := int(toast.offsetX)
			if offset > 0 {
				toastBox = strings.Repeat(" ", offset) + toastBox
			}
		}

		rendered = append(rendered, toastBox)
	}

	return strings.Join(rendered, "\n")
}
