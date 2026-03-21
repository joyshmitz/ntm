// Package robot provides machine-readable output for AI agents.
// snapshot_attention.go defines types and function for the attention summary
// in --robot-snapshot output. (br-slg9g: Attention Feed Phase 2a2)
package robot

// SnapshotAttentionSummary provides a compact orientation summary from the
// attention feed at snapshot time. Helps operators choose the next targeted
// command without rereading the entire snapshot or waiting for a digest.
type SnapshotAttentionSummary struct {
	// TotalEvents is the number of events currently in the journal.
	TotalEvents int `json:"total_events"`

	// ActionRequiredCount is events needing operator action.
	ActionRequiredCount int `json:"action_required_count"`

	// InterestingCount is events worth noting but not urgent.
	InterestingCount int `json:"interesting_count"`

	// TopItems surfaces the most recent action_required events (up to 3)
	// so operators can immediately see what needs attention.
	TopItems []SnapshotAttentionItem `json:"top_items,omitempty"`

	// ByCategoryCount groups events by category for orientation.
	ByCategoryCount map[string]int `json:"by_category,omitempty"`

	// UnsupportedSignals lists signals that were considered but are
	// deliberately not supported, so operators know what to expect.
	UnsupportedSignals []string `json:"unsupported_signals,omitempty"`

	// NextSteps are mechanical suggestions for what to do next.
	NextSteps []NextAction `json:"next_steps,omitempty"`
}

// SnapshotAttentionItem is a compact representation of a top attention item.
type SnapshotAttentionItem struct {
	Cursor        int64  `json:"cursor"`
	Category      string `json:"category"`
	Actionability string `json:"actionability"`
	Severity      string `json:"severity"`
	Summary       string `json:"summary"`
}
