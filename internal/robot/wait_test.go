package robot

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestIsValidWaitCondition(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		// Pane-based conditions
		{"idle valid", "idle", true},
		{"complete valid", "complete", true},
		{"generating valid", "generating", true},
		{"healthy valid", "healthy", true},
		{"stalled valid", "stalled", true},
		{"rate_limited valid", "rate_limited", true},

		// Attention-based conditions
		{"attention valid", "attention", true},
		{"action_required valid", "action_required", true},
		{"mail_pending valid", "mail_pending", true},
		{"mail_ack_required valid", "mail_ack_required", true},
		{"context_hot valid", "context_hot", true},
		{"reservation_conflict valid", "reservation_conflict", true},
		{"file_conflict valid", "file_conflict", true},
		{"session_changed valid", "session_changed", true},
		{"pane_changed valid", "pane_changed", true},

		// Composed conditions
		{"composed valid", "idle,healthy", true},
		{"composed with spaces", "idle, healthy", true},
		{"three conditions", "idle,healthy,complete", true},
		{"mixed pane and attention", "idle,action_required", true},

		// Invalid conditions
		{"invalid condition", "invalid", false},
		{"empty string", "", false},
		{"partial invalid", "idle,invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidWaitCondition(tt.condition)
			if got != tt.want {
				t.Errorf("isValidWaitCondition(%q) = %v, want %v", tt.condition, got, tt.want)
			}
		})
	}
}

func TestHasAttentionBasedConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []string
		want       bool
	}{
		{"pane only", []string{"idle", "healthy"}, false},
		{"attention only", []string{"action_required", "mail_pending"}, true},
		{"mixed", []string{"idle", "action_required"}, true},
		{"single pane", []string{"idle"}, false},
		{"single attention", []string{"attention"}, true},
		{"empty", []string{}, false},
		{"all attention types", []string{"attention", "action_required", "mail_pending", "context_hot"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAttentionBasedConditions(tt.conditions)
			if got != tt.want {
				t.Errorf("hasAttentionBasedConditions(%v) = %v, want %v", tt.conditions, got, tt.want)
			}
		})
	}
}

func TestMeetsSingleWaitCondition(t *testing.T) {
	tests := []struct {
		name      string
		state     AgentState
		condition string
		want      bool
	}{
		{"waiting meets idle", StateWaiting, WaitConditionIdle, true},
		{"generating meets generating", StateGenerating, WaitConditionGenerating, true},
		{"waiting meets healthy", StateWaiting, WaitConditionHealthy, true},
		{"thinking meets healthy", StateThinking, WaitConditionHealthy, true},
		{"generating meets healthy", StateGenerating, WaitConditionHealthy, true},
		{"unknown meets healthy", StateUnknown, WaitConditionHealthy, true},
		{"error does not meet healthy", StateError, WaitConditionHealthy, false},
		{"stalled does not meet healthy", StateStalled, WaitConditionHealthy, false},
		{"generating does not meet idle", StateGenerating, WaitConditionIdle, false},
		{"thinking does not meet idle", StateThinking, WaitConditionIdle, false},
		{"unknown does not meet idle", StateUnknown, WaitConditionIdle, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := &AgentActivity{
				State: tt.state,
			}
			got := meetsSingleWaitCondition(activity, tt.condition)
			if got != tt.want {
				t.Errorf("meetsSingleWaitCondition(state=%s, condition=%s) = %v, want %v",
					tt.state, tt.condition, got, tt.want)
			}
		})
	}
}

func TestMeetsAllWaitConditions(t *testing.T) {
	tests := []struct {
		name       string
		state      AgentState
		conditions []string
		want       bool
	}{
		{"single condition met", StateWaiting, []string{"idle"}, true},
		{"single condition not met", StateGenerating, []string{"idle"}, false},
		{"both conditions met", StateWaiting, []string{"idle", "healthy"}, true},
		{"first met second not", StateError, []string{"healthy"}, false},
		{"empty conditions", StateWaiting, []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := &AgentActivity{
				State: tt.state,
			}
			got := meetsAllWaitConditions(activity, tt.conditions)
			if got != tt.want {
				t.Errorf("meetsAllWaitConditions(state=%s, conditions=%v) = %v, want %v",
					tt.state, tt.conditions, got, tt.want)
			}
		})
	}
}

func TestCheckWaitConditionMet_AllMode(t *testing.T) {
	// Test with default (ALL) mode
	opts := WaitOptions{
		Condition:  "idle",
		WaitForAny: false,
	}

	t.Run("all agents idle", func(t *testing.T) {
		activities := []*AgentActivity{
			{PaneID: "test__cc_1", State: StateWaiting},
			{PaneID: "test__cc_2", State: StateWaiting},
		}
		met, matching, pending := checkWaitConditionMet(activities, opts)
		if !met {
			t.Error("Expected condition to be met when all agents are idle")
		}
		if len(matching) != 2 {
			t.Errorf("Expected 2 matching agents, got %d", len(matching))
		}
		if len(pending) != 0 {
			t.Errorf("Expected 0 pending agents, got %d", len(pending))
		}
	})

	t.Run("some agents not idle", func(t *testing.T) {
		activities := []*AgentActivity{
			{PaneID: "test__cc_1", State: StateWaiting},
			{PaneID: "test__cc_2", State: StateGenerating},
		}
		met, matching, pending := checkWaitConditionMet(activities, opts)
		if met {
			t.Error("Expected condition not to be met when some agents are generating")
		}
		if len(matching) != 1 {
			t.Errorf("Expected 1 matching agent, got %d", len(matching))
		}
		if len(pending) != 1 {
			t.Errorf("Expected 1 pending agent, got %d", len(pending))
		}
	})

	t.Run("no agents", func(t *testing.T) {
		activities := []*AgentActivity{}
		met, _, _ := checkWaitConditionMet(activities, opts)
		if met {
			t.Error("Expected condition not to be met with no agents")
		}
	})
}

func TestCheckWaitConditionMet_AnyMode(t *testing.T) {
	// Test with ANY mode
	opts := WaitOptions{
		Condition:  "idle",
		WaitForAny: true,
		CountN:     1,
	}

	t.Run("one agent idle", func(t *testing.T) {
		activities := []*AgentActivity{
			{PaneID: "test__cc_1", State: StateWaiting},
			{PaneID: "test__cc_2", State: StateGenerating},
		}
		met, matching, _ := checkWaitConditionMet(activities, opts)
		if !met {
			t.Error("Expected condition to be met when at least one agent is idle")
		}
		if len(matching) != 1 {
			t.Errorf("Expected 1 matching agent, got %d", len(matching))
		}
	})

	t.Run("no agents idle", func(t *testing.T) {
		activities := []*AgentActivity{
			{PaneID: "test__cc_1", State: StateGenerating},
			{PaneID: "test__cc_2", State: StateGenerating},
		}
		met, _, _ := checkWaitConditionMet(activities, opts)
		if met {
			t.Error("Expected condition not to be met when no agents are idle")
		}
	})

	t.Run("count N requirement", func(t *testing.T) {
		opts := WaitOptions{
			Condition:  "idle",
			WaitForAny: true,
			CountN:     2,
		}
		activities := []*AgentActivity{
			{PaneID: "test__cc_1", State: StateWaiting},
			{PaneID: "test__cc_2", State: StateGenerating},
			{PaneID: "test__cc_3", State: StateWaiting},
		}
		met, matching, _ := checkWaitConditionMet(activities, opts)
		if !met {
			t.Error("Expected condition to be met when 2 agents are idle and CountN=2")
		}
		if len(matching) != 2 {
			t.Errorf("Expected 2 matching agents, got %d", len(matching))
		}
	})
}

func TestCompleteCondition(t *testing.T) {
	t.Run("waiting with no recent output", func(t *testing.T) {
		activity := &AgentActivity{
			State:      StateWaiting,
			LastOutput: time.Time{}, // Zero time - no output recorded
		}
		got := meetsSingleWaitCondition(activity, WaitConditionComplete)
		if !got {
			t.Error("Expected 'complete' condition to be met for waiting agent with no output")
		}
	})

	t.Run("waiting with recent output", func(t *testing.T) {
		activity := &AgentActivity{
			State:      StateWaiting,
			LastOutput: time.Now(), // Just now
		}
		got := meetsSingleWaitCondition(activity, WaitConditionComplete)
		if got {
			t.Error("Expected 'complete' condition not to be met for waiting agent with recent output")
		}
	})

	t.Run("waiting with old output", func(t *testing.T) {
		activity := &AgentActivity{
			State:      StateWaiting,
			LastOutput: time.Now().Add(-10 * time.Second), // 10 seconds ago
		}
		got := meetsSingleWaitCondition(activity, WaitConditionComplete)
		if !got {
			t.Error("Expected 'complete' condition to be met for waiting agent with old output")
		}
	})

	t.Run("generating does not meet complete", func(t *testing.T) {
		activity := &AgentActivity{
			State: StateGenerating,
		}
		got := meetsSingleWaitCondition(activity, WaitConditionComplete)
		if got {
			t.Error("Expected 'complete' condition not to be met for generating agent")
		}
	})
}

func TestWaitConditionConstants(t *testing.T) {
	// Ensure condition constants have expected string values
	if WaitConditionIdle != "idle" {
		t.Errorf("WaitConditionIdle = %q, want %q", WaitConditionIdle, "idle")
	}
	if WaitConditionComplete != "complete" {
		t.Errorf("WaitConditionComplete = %q, want %q", WaitConditionComplete, "complete")
	}
	if WaitConditionGenerating != "generating" {
		t.Errorf("WaitConditionGenerating = %q, want %q", WaitConditionGenerating, "generating")
	}
	if WaitConditionHealthy != "healthy" {
		t.Errorf("WaitConditionHealthy = %q, want %q", WaitConditionHealthy, "healthy")
	}
}

func TestWaitOptionsDefaults(t *testing.T) {
	opts := WaitOptions{
		Session:   "test",
		Condition: "idle",
	}

	// Check that zero values are handled correctly
	if opts.CountN != 0 {
		t.Errorf("Default CountN should be 0, got %d", opts.CountN)
	}
	if opts.WaitForAny {
		t.Error("Default WaitForAny should be false")
	}
	if opts.ExitOnError {
		t.Error("Default ExitOnError should be false")
	}
}

// =============================================================================
// filterWaitPanes Tests
// =============================================================================

func TestFilterWaitPanes(t *testing.T) {
	t.Parallel()

	// Create test panes with various agent types and indices
	// Note: detectAgentType looks for patterns like "claude", "codex", "gemini" in title
	// or short forms like "__cc_", "__cod_", "__gmi_" with word boundaries
	testPanes := []tmux.Pane{
		{Index: 0, Title: "user_0"},                                 // User pane, should be filtered out
		{Index: 1, Title: "myproject__cc_1"},                        // Claude agent (short form with prefix)
		{Index: 2, Title: "myproject__cod_2"},                       // Codex agent (short form with prefix)
		{Index: 3, Title: "myproject__gmi_3"},                       // Gemini agent (short form with prefix)
		{Index: 4, Title: "myproject__cc_4"},                        // Another Claude agent
		{Index: 5, Title: "unknown_agent"},                          // Unknown, should be filtered out
		{Index: 6, Title: "bash"},                                   // Non-agent pane
		{Index: 7, Title: "claude_session", Type: tmux.AgentClaude}, // Using full type name
	}

	t.Run("no_filters_returns_only_agents", func(t *testing.T) {
		opts := WaitOptions{}
		result := filterWaitPanes(testPanes, opts)

		// Should exclude user_0, unknown_agent, bash
		// Should include cc_1, cod_2, gmi_3, cc_4, claude_7
		if len(result) != 5 {
			t.Errorf("filterWaitPanes() returned %d panes, want 5", len(result))
			for _, p := range result {
				t.Logf("  included: Index=%d Title=%q", p.Index, p.Title)
			}
		}
	})

	t.Run("filter_by_pane_indices", func(t *testing.T) {
		opts := WaitOptions{
			PaneIndices: []int{1, 3},
		}
		result := filterWaitPanes(testPanes, opts)

		if len(result) != 2 {
			t.Errorf("filterWaitPanes() returned %d panes, want 2", len(result))
		}

		// Verify correct panes selected
		indices := make(map[int]bool)
		for _, p := range result {
			indices[p.Index] = true
		}
		if !indices[1] || !indices[3] {
			t.Errorf("Expected panes 1 and 3, got indices: %v", indices)
		}
	})

	t.Run("filter_by_agent_type_claude", func(t *testing.T) {
		opts := WaitOptions{
			AgentType: "claude", // detectAgentType returns canonical names like "claude" not "cc"
		}
		result := filterWaitPanes(testPanes, opts)

		// myproject__cc_1, myproject__cc_4, and claude_session should match "claude" type
		if len(result) != 3 {
			t.Errorf("filterWaitPanes(AgentType=claude) returned %d panes, want 3", len(result))
		}
	})

	t.Run("filter_by_agent_type_codex", func(t *testing.T) {
		opts := WaitOptions{
			AgentType: "codex",
		}
		result := filterWaitPanes(testPanes, opts)

		// cod_2 should match "codex" type (detectAgentType maps cod->codex)
		if len(result) != 1 {
			t.Errorf("filterWaitPanes(AgentType=codex) returned %d panes, want 1", len(result))
		}
		if len(result) > 0 && result[0].Index != 2 {
			t.Errorf("Expected pane index 2, got %d", result[0].Index)
		}
	})

	t.Run("filter_by_agent_type_gemini", func(t *testing.T) {
		opts := WaitOptions{
			AgentType: "gemini",
		}
		result := filterWaitPanes(testPanes, opts)

		// gmi_3 should match "gemini" type (detectAgentType maps gmi->gemini)
		if len(result) != 1 {
			t.Errorf("filterWaitPanes(AgentType=gemini) returned %d panes, want 1", len(result))
		}
		if len(result) > 0 && result[0].Index != 3 {
			t.Errorf("Expected pane index 3, got %d", result[0].Index)
		}
	})

	t.Run("filter_by_both_indices_and_type", func(t *testing.T) {
		opts := WaitOptions{
			PaneIndices: []int{1, 2, 3, 4},
			AgentType:   "claude", // Use canonical name
		}
		result := filterWaitPanes(testPanes, opts)

		// Only myproject__cc_1 and myproject__cc_4 should match (both in indices AND type=claude)
		if len(result) != 2 {
			t.Errorf("filterWaitPanes() returned %d panes, want 2", len(result))
		}
	})

	t.Run("empty_panes_input", func(t *testing.T) {
		opts := WaitOptions{}
		result := filterWaitPanes([]tmux.Pane{}, opts)

		if len(result) != 0 {
			t.Errorf("filterWaitPanes() with empty input returned %d panes, want 0", len(result))
		}
	})

	t.Run("no_matching_indices", func(t *testing.T) {
		opts := WaitOptions{
			PaneIndices: []int{99, 100},
		}
		result := filterWaitPanes(testPanes, opts)

		if len(result) != 0 {
			t.Errorf("filterWaitPanes() returned %d panes, want 0", len(result))
		}
	})

	t.Run("case_insensitive_agent_type", func(t *testing.T) {
		opts := WaitOptions{
			AgentType: "CLAUDE", // uppercase canonical name
		}
		result := filterWaitPanes(testPanes, opts)

		// Should still match claude agents (case insensitive via strings.EqualFold)
		if len(result) != 3 {
			t.Errorf("filterWaitPanes(AgentType=CLAUDE) returned %d panes, want 3 (case insensitive)", len(result))
		}
	})

	t.Run("user_pane_always_filtered", func(t *testing.T) {
		// Even if explicitly requested by index, user pane should be excluded
		opts := WaitOptions{
			PaneIndices: []int{0}, // user_0 pane
		}
		result := filterWaitPanes(testPanes, opts)

		if len(result) != 0 {
			t.Errorf("User pane should be filtered out, got %d panes", len(result))
		}
	})
}
