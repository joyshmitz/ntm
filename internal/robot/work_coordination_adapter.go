// Package robot provides machine-readable output for AI agents.
// work_coordination_adapter.go normalizes work and coordination signals into canonical projection shapes.
//
// This adapter transforms beads/bv state, Agent Mail state, file conflicts,
// reservation conflicts, and handoff context into stable WorkSection and
// CoordinationSection structures. It hides the peculiarities of each source
// tool so robot surfaces can reason about coordination with one vocabulary.
//
// Bead: bd-j9jo3.3.2
package robot

import (
	"sort"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/tracker"
)

// =============================================================================
// Section Types (from projection-section-model.md)
// =============================================================================

// WorkSection represents beads/task state.
type WorkSection struct {
	// Counts
	Total      int `json:"total"`
	Open       int `json:"open"`
	Ready      int `json:"ready"` // No blockers, claimable
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`

	// Ready queue (top N)
	ReadyQueue []BeadRef `json:"ready_queue"` // Limit 5

	// In-flight work
	InFlight []InFlightWork `json:"in_flight,omitempty"`

	// Recent activity
	RecentlyClosed int `json:"recently_closed"` // Last 24h

	// Health
	StaleCount int `json:"stale_count"` // In-progress > threshold
	CycleCount int `json:"cycle_count"` // Dependency cycles
}

// BeadRef is a lightweight reference to a bead.
type BeadRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Priority int    `json:"priority"`
	Type     string `json:"type"` // task, bug, feature, epic
}

// InFlightWork represents work currently being done.
type InFlightWork struct {
	BeadID      string `json:"bead_id"`
	BeadTitle   string `json:"bead_title"`
	Agent       string `json:"agent,omitempty"` // session:pane if known
	StartedAt   string `json:"started_at"`
	DurationSec int    `json:"duration_sec"`
}

// CoordinationSection represents agent coordination state.
type CoordinationSection struct {
	// Mail
	Mail MailSummary `json:"mail"`

	// File reservations
	Reservations []ReservationInfo `json:"reservations,omitempty"`

	// Conflicts (if any)
	Conflicts []ConflictInfo `json:"conflicts,omitempty"`

	// Handoff state
	Handoff *HandoffInfo `json:"handoff,omitempty"`
}

// MailSummary provides Agent Mail metrics.
type MailSummary struct {
	Unread      int `json:"unread"`
	Urgent      int `json:"urgent"`
	AckRequired int `json:"ack_required"`
	ThreadCount int `json:"thread_count"`

	// Recent messages (top N)
	Recent []MailRef `json:"recent,omitempty"` // Limit 3
}

// MailRef is a lightweight reference to a mail message.
type MailRef struct {
	ID         string `json:"id"`
	From       string `json:"from"`
	Subject    string `json:"subject"`
	Urgent     bool   `json:"urgent"`
	ReceivedAt string `json:"received_at"`
}

// ReservationInfo describes a file reservation.
type ReservationInfo struct {
	Agent     string   `json:"agent"`
	Patterns  []string `json:"patterns"`
	Exclusive bool     `json:"exclusive"`
	ExpiresAt string   `json:"expires_at"`
	Reason    string   `json:"reason,omitempty"` // Usually bead ID
}

// ConflictInfo describes a detected conflict.
type ConflictInfo struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"` // file_conflict, reservation_conflict
	Files      []string `json:"files,omitempty"`
	Agents     []string `json:"agents"`
	DetectedAt string   `json:"detected_at"`
	Resolved   bool     `json:"resolved"`
}

// HandoffInfo provides handoff context.
type HandoffInfo struct {
	Session    string `json:"session"`
	Goal       string `json:"goal,omitempty"`
	Now        string `json:"now,omitempty"`
	Path       string `json:"path,omitempty"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Status     string `json:"status,omitempty"`
}

// =============================================================================
// Configuration
// =============================================================================

// WorkCoordinationAdapterConfig controls the adapter behavior.
type WorkCoordinationAdapterConfig struct {
	// ReadyQueueLimit is the max number of ready beads to include.
	// Default: 5
	ReadyQueueLimit int

	// RecentMailLimit is the max number of recent mail items.
	// Default: 3
	RecentMailLimit int

	// StaleThresholdMinutes is when in-progress work is considered stale.
	// Default: 120 (2 hours)
	StaleThresholdMinutes int
}

// DefaultWorkCoordinationAdapterConfig returns sensible defaults.
func DefaultWorkCoordinationAdapterConfig() WorkCoordinationAdapterConfig {
	return WorkCoordinationAdapterConfig{
		ReadyQueueLimit:       5,
		RecentMailLimit:       3,
		StaleThresholdMinutes: 120,
	}
}

// =============================================================================
// Adapter
// =============================================================================

// WorkCoordinationAdapter normalizes work and coordination data.
type WorkCoordinationAdapter struct {
	config WorkCoordinationAdapterConfig
}

// NewWorkCoordinationAdapter creates a new adapter with the given configuration.
func NewWorkCoordinationAdapter(config WorkCoordinationAdapterConfig) *WorkCoordinationAdapter {
	return &WorkCoordinationAdapter{config: config}
}

// =============================================================================
// Work Section Normalization
// =============================================================================

// NormalizeWorkSection creates a WorkSection from beads data.
func (a *WorkCoordinationAdapter) NormalizeWorkSection(summary *bv.BeadsSummary) *WorkSection {
	work := &WorkSection{
		ReadyQueue: make([]BeadRef, 0),
		InFlight:   make([]InFlightWork, 0),
	}

	// Populate counts from summary
	if summary != nil {
		work.Total = summary.Total
		work.Open = summary.Open
		work.InProgress = summary.InProgress
		work.Blocked = summary.Blocked
		work.Ready = summary.Ready

		// Populate ready queue from preview
		for i, preview := range summary.ReadyPreview {
			if i >= a.config.ReadyQueueLimit {
				break
			}
			work.ReadyQueue = append(work.ReadyQueue, BeadRef{
				ID:       preview.ID,
				Title:    truncateForDisplay(preview.Title, 80),
				Priority: parsePriorityString(preview.Priority),
				Type:     "task", // Default, would need bead metadata
			})
		}

		// Populate in-flight from in_progress_list
		for _, inProg := range summary.InProgressList {
			startedAt := ""
			durationSec := 0
			if !inProg.UpdatedAt.IsZero() {
				startedAt = inProg.UpdatedAt.Format(time.RFC3339)
				durationSec = int(time.Since(inProg.UpdatedAt).Seconds())
			}
			work.InFlight = append(work.InFlight, InFlightWork{
				BeadID:      inProg.ID,
				BeadTitle:   truncateForDisplay(inProg.Title, 80),
				Agent:       inProg.Assignee,
				StartedAt:   startedAt,
				DurationSec: durationSec,
			})
		}
	}

	return work
}

// =============================================================================
// Coordination Section Normalization
// =============================================================================

// AgentMailData represents raw Agent Mail data for normalization.
type AgentMailData struct {
	Inbox []InboxMessage
}

// InboxMessage represents a message in the inbox.
type InboxMessage struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	From        string    `json:"from"`
	Importance  string    `json:"importance"`
	AckRequired bool      `json:"ack_required"`
	CreatedTS   time.Time `json:"created_ts"`
	ThreadID    string    `json:"thread_id"`
}

// ReservationData represents raw reservation data.
type ReservationData struct {
	Agent     string
	Patterns  []string
	Exclusive bool
	ExpiresAt time.Time
	Reason    string
}

// NormalizeCoordinationSection creates a CoordinationSection from various sources.
func (a *WorkCoordinationAdapter) NormalizeCoordinationSection(
	mailData *AgentMailData,
	reservations []ReservationData,
	conflicts []tracker.Conflict,
	handoff *HandoffSummary,
) *CoordinationSection {
	coord := &CoordinationSection{
		Mail:         MailSummary{Recent: make([]MailRef, 0)},
		Reservations: make([]ReservationInfo, 0),
		Conflicts:    make([]ConflictInfo, 0),
	}

	// Normalize mail
	if mailData != nil {
		coord.Mail = a.normalizeMailSummary(mailData)
	}

	// Normalize reservations
	for _, res := range reservations {
		coord.Reservations = append(coord.Reservations, ReservationInfo{
			Agent:     res.Agent,
			Patterns:  res.Patterns,
			Exclusive: res.Exclusive,
			ExpiresAt: res.ExpiresAt.Format(time.RFC3339),
			Reason:    res.Reason,
		})
	}

	// Normalize conflicts
	for _, conflict := range conflicts {
		detectedAt := ""
		if !conflict.LastAt.IsZero() {
			detectedAt = conflict.LastAt.Format(time.RFC3339)
		}
		coord.Conflicts = append(coord.Conflicts, ConflictInfo{
			ID:         conflict.Path, // Use path as ID
			Type:       "file_conflict",
			Files:      []string{conflict.Path},
			Agents:     conflict.Agents,
			DetectedAt: detectedAt,
			Resolved:   false, // tracker.Conflict doesn't track resolution
		})
	}

	// Normalize handoff
	if handoff != nil && handoff.Session != "" {
		coord.Handoff = &HandoffInfo{
			Session:    handoff.Session,
			Goal:       handoff.Goal,
			Now:        handoff.Now,
			Path:       handoff.Path,
			AgeSeconds: handoff.AgeSeconds,
			Status:     handoff.Status,
		}
	}

	return coord
}

// normalizeMailSummary converts raw mail data to a MailSummary.
func (a *WorkCoordinationAdapter) normalizeMailSummary(mailData *AgentMailData) MailSummary {
	summary := MailSummary{
		Recent: make([]MailRef, 0),
	}

	if mailData == nil || len(mailData.Inbox) == 0 {
		return summary
	}

	// Count threads
	threads := make(map[string]bool)

	// Process messages
	for _, msg := range mailData.Inbox {
		summary.Unread++
		if msg.Importance == "urgent" {
			summary.Urgent++
		}
		if msg.AckRequired {
			summary.AckRequired++
		}
		if msg.ThreadID != "" {
			threads[msg.ThreadID] = true
		}
	}
	summary.ThreadCount = len(threads)

	// Sort by timestamp descending and take recent
	sorted := make([]InboxMessage, len(mailData.Inbox))
	copy(sorted, mailData.Inbox)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedTS.After(sorted[j].CreatedTS)
	})

	for i, msg := range sorted {
		if i >= a.config.RecentMailLimit {
			break
		}
		summary.Recent = append(summary.Recent, MailRef{
			ID:         msg.ID,
			From:       msg.From,
			Subject:    truncateForDisplay(msg.Subject, 60),
			Urgent:     msg.Importance == "urgent",
			ReceivedAt: msg.CreatedTS.Format(time.RFC3339),
		})
	}

	return summary
}

// =============================================================================
// Combined Normalization
// =============================================================================

// WorkCoordinationSnapshot holds both sections.
type WorkCoordinationSnapshot struct {
	Work         WorkSection         `json:"work"`
	Coordination CoordinationSection `json:"coordination"`
	CollectedAt  time.Time           `json:"collected_at"`
}

// NormalizeSnapshot creates a complete work/coordination snapshot.
func (a *WorkCoordinationAdapter) NormalizeSnapshot(
	beadsSummary *bv.BeadsSummary,
	mailData *AgentMailData,
	reservations []ReservationData,
	conflicts []tracker.Conflict,
	handoff *HandoffSummary,
) *WorkCoordinationSnapshot {
	return &WorkCoordinationSnapshot{
		Work:         *a.NormalizeWorkSection(beadsSummary),
		Coordination: *a.NormalizeCoordinationSection(mailData, reservations, conflicts, handoff),
		CollectedAt:  time.Now(),
	}
}

// =============================================================================
// Helpers
// =============================================================================

// truncateForDisplay truncates a string to maxLen runes, adding "..." if truncated.
// Note: Named differently to avoid conflict with truncateString in tui_parity.go
func truncateForDisplay(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// parsePriorityString converts a priority string like "P0", "P1" to an int.
// Returns 2 (medium priority) if parsing fails.
func parsePriorityString(s string) int {
	if len(s) < 2 {
		return 2
	}
	// Handle "P0", "P1", etc.
	if s[0] == 'P' || s[0] == 'p' {
		if s[1] >= '0' && s[1] <= '4' {
			return int(s[1] - '0')
		}
	}
	return 2
}
