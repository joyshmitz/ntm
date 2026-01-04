package status

import (
	"context"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// UnifiedDetector implements the Detector interface by combining
// activity, prompt, and error detection into a unified status check.
type UnifiedDetector struct {
	config DetectorConfig
}

// NewDetector creates a new UnifiedDetector with default configuration
func NewDetector() *UnifiedDetector {
	return &UnifiedDetector{
		config: DefaultConfig(),
	}
}

// NewDetectorWithConfig creates a new UnifiedDetector with custom configuration
func NewDetectorWithConfig(config DetectorConfig) *UnifiedDetector {
	return &UnifiedDetector{
		config: config,
	}
}

// Config returns the current detector configuration
func (d *UnifiedDetector) Config() DetectorConfig {
	return d.config
}

// Detect returns the current status of a single pane.
// Detection priority: error > idle > working > unknown
func (d *UnifiedDetector) Detect(paneID string) (AgentStatus, error) {
	status := AgentStatus{
		PaneID:    paneID,
		UpdatedAt: time.Now(),
		State:     StateUnknown,
	}

	// Get pane activity time
	lastActivity, err := tmux.GetPaneActivity(paneID)
	if err != nil {
		return status, err
	}
	status.LastActive = lastActivity

	// Capture recent output for analysis
	output, err := tmux.CapturePaneOutput(paneID, d.config.ScanLines)
	if err != nil {
		return status, err
	}
	if strings.TrimSpace(output) == "" {
		// Give tmux a brief moment to flush output, then retry once
		time.Sleep(100 * time.Millisecond)
		if retry, err := tmux.CapturePaneOutput(paneID, d.config.ScanLines); err == nil {
			output = retry
		}
	}
	status.LastOutput = truncateOutput(output, d.config.OutputPreviewLength)

	// Try to get pane details for agent type detection
	// We'll parse the pane title from output if needed
	// Use paneID as target - tmux list-panes -s -t paneID lists all panes in that pane's session
	panes, _ := tmux.GetPanesWithActivity(paneID)
	for _, p := range panes {
		if p.Pane.ID == paneID {
			status.PaneName = p.Pane.Title
			status.AgentType = string(p.Pane.Type)
			break
		}
	}

	// Detection priority:
	// 1. Check for errors first (most important)
	// 2. Check for idle (at prompt)
	// 3. Check activity recency (working vs unknown)

	// Check for errors
	if errType := DetectErrorInOutput(output); errType != ErrorNone {
		status.State = StateError
		status.ErrorType = errType
		return status, nil
	}

	// Check if at prompt (idle)
	if DetectIdleFromOutput(output, status.AgentType) {
		status.State = StateIdle
		return status, nil
	}
	// Heuristic: for user panes with empty output, treat as idle
	// Note: We removed the strings.Contains(output, "$") check because it was too broad -
	// it would match any $ in the output (like $i in shell scripts), not just prompts.
	// The DetectIdleFromOutput function already handles prompt detection properly.
	if status.AgentType == "" || status.AgentType == "user" {
		if strings.TrimSpace(output) == "" {
			status.State = StateIdle
			return status, nil
		}
	}

	// Check recent activity
	threshold := time.Duration(d.config.ActivityThreshold) * time.Second
	if time.Since(status.LastActive) < threshold {
		status.State = StateWorking
		return status, nil
	}

	// Default to unknown if can't determine
	status.State = StateUnknown
	return status, nil
}

// DetectAll returns status for all panes in a session.
// Errors on individual panes don't fail the entire operation.
func (d *UnifiedDetector) DetectAll(session string) ([]AgentStatus, error) {
	return d.DetectAllContext(context.Background(), session)
}

// DetectAllContext returns status for all panes in a session with cancellation support.
// Errors on individual panes don't fail the entire operation.
func (d *UnifiedDetector) DetectAllContext(ctx context.Context, session string) ([]AgentStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	panes, err := tmux.GetPanesWithActivityContext(ctx, session)
	if err != nil {
		return nil, err
	}

	statuses := make([]AgentStatus, 0, len(panes))
	for _, pane := range panes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		status := AgentStatus{
			PaneID:     pane.Pane.ID,
			PaneName:   pane.Pane.Title,
			AgentType:  string(pane.Pane.Type),
			LastActive: pane.LastActivity,
			UpdatedAt:  time.Now(),
			State:      StateUnknown,
		}

		// Capture output for this pane
		output, err := tmux.CapturePaneOutputContext(ctx, pane.Pane.ID, d.config.ScanLines)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			// Log but continue - one bad pane shouldn't fail all
			statuses = append(statuses, status)
			continue
		}
		status.LastOutput = truncateOutput(output, d.config.OutputPreviewLength)

		// Detection priority: error > idle > working > unknown

		// Check for errors
		if errType := DetectErrorInOutput(output); errType != ErrorNone {
			status.State = StateError
			status.ErrorType = errType
			statuses = append(statuses, status)
			continue
		}

		// Check if at prompt (idle)
		if DetectIdleFromOutput(output, status.AgentType) {
			status.State = StateIdle
			statuses = append(statuses, status)
			continue
		}
		// Heuristic: for user panes with empty output, treat as idle
		// Note: We removed the strings.Contains(output, "$") check because it was too broad -
		// it would match any $ in the output (like $i in shell scripts), not just prompts.
		// The DetectIdleFromOutput function already handles prompt detection properly.
		if status.AgentType == "" || status.AgentType == "user" {
			if strings.TrimSpace(output) == "" {
				status.State = StateIdle
				statuses = append(statuses, status)
				continue
			}
		}

		// Check recent activity
		threshold := time.Duration(d.config.ActivityThreshold) * time.Second
		if time.Since(status.LastActive) < threshold {
			status.State = StateWorking
			statuses = append(statuses, status)
			continue
		}

		// Default to unknown
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// truncateOutput returns the last n bytes of output, respecting UTF-8 boundaries.
// If maxLen falls in the middle of a multi-byte rune, it advances to the next
// valid rune boundary to avoid producing invalid UTF-8.
func truncateOutput(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}

	// Start position for the tail
	start := len(s) - maxLen

	// If start is in the middle of a UTF-8 rune, advance to the next rune boundary.
	// UTF-8 continuation bytes have the form 10xxxxxx (0x80-0xBF).
	// We need to find the next byte that is NOT a continuation byte.
	for start < len(s) && s[start]&0xC0 == 0x80 {
		start++
	}

	return s[start:]
}

// GetStateSummary returns a summary of states for a set of statuses
func GetStateSummary(statuses []AgentStatus) map[AgentState]int {
	summary := make(map[AgentState]int)
	for _, s := range statuses {
		summary[s.State]++
	}
	return summary
}

// FilterByState returns only statuses matching the given state
func FilterByState(statuses []AgentStatus, state AgentState) []AgentStatus {
	var filtered []AgentStatus
	for _, s := range statuses {
		if s.State == state {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// FilterByAgentType returns only statuses for the given agent type
func FilterByAgentType(statuses []AgentStatus, agentType string) []AgentStatus {
	var filtered []AgentStatus
	for _, s := range statuses {
		if s.AgentType == agentType {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

// HasErrors returns true if any status is in error state
func HasErrors(statuses []AgentStatus) bool {
	for _, s := range statuses {
		if s.State == StateError {
			return true
		}
	}
	return false
}

// AllHealthy returns true if all statuses are healthy (idle or working)
func AllHealthy(statuses []AgentStatus) bool {
	for _, s := range statuses {
		if !s.IsHealthy() {
			return false
		}
	}
	return len(statuses) > 0
}
