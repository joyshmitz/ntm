package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/robot"
)

func TestWorkAssignmentStruct(t *testing.T) {
	now := time.Now()
	wa := WorkAssignment{
		BeadID:         "ntm-1234",
		BeadTitle:      "Implement feature X",
		AgentPaneID:    "%0",
		AgentMailName:  "BlueFox",
		AgentType:      "cc",
		AssignedAt:     now,
		Priority:       1,
		Score:          0.85,
		FilesToReserve: []string{"internal/feature/*.go"},
	}

	if wa.BeadID != "ntm-1234" {
		t.Errorf("expected BeadID 'ntm-1234', got %q", wa.BeadID)
	}
	if wa.Score != 0.85 {
		t.Errorf("expected Score 0.85, got %f", wa.Score)
	}
	if len(wa.FilesToReserve) != 1 {
		t.Errorf("expected 1 file to reserve, got %d", len(wa.FilesToReserve))
	}
}

func TestAssignmentResultStruct(t *testing.T) {
	ar := AssignmentResult{
		Success:      true,
		MessageSent:  true,
		Reservations: []string{"internal/*.go"},
	}

	if !ar.Success {
		t.Error("expected Success to be true")
	}
	if !ar.MessageSent {
		t.Error("expected MessageSent to be true")
	}
	if ar.Error != "" {
		t.Error("expected empty error on success")
	}
}

func TestRemoveRecommendation(t *testing.T) {
	recs := []bv.TriageRecommendation{
		{ID: "ntm-001", Title: "First"},
		{ID: "ntm-002", Title: "Second"},
		{ID: "ntm-003", Title: "Third"},
	}

	result := removeRecommendation(recs, "ntm-002")

	if len(result) != 2 {
		t.Errorf("expected 2 recommendations after removal, got %d", len(result))
	}
	for _, r := range result {
		if r.ID == "ntm-002" {
			t.Error("expected ntm-002 to be removed")
		}
	}

	// Test removing non-existent ID
	result2 := removeRecommendation(recs, "ntm-999")
	if len(result2) != 3 {
		t.Errorf("expected 3 recommendations when removing non-existent, got %d", len(result2))
	}

	// Test empty slice (should not panic)
	result3 := removeRecommendation(nil, "ntm-001")
	if result3 != nil {
		t.Errorf("expected nil for empty input, got %v", result3)
	}

	result4 := removeRecommendation([]bv.TriageRecommendation{}, "ntm-001")
	if result4 != nil {
		t.Errorf("expected nil for empty slice, got %v", result4)
	}
}

func TestFindBestMatch(t *testing.T) {
	c := New("test-session", "/tmp/test", nil, "TestAgent")

	agent := &AgentState{
		PaneID:        "%0",
		AgentType:     "cc",
		AgentMailName: "BlueFox",
		Status:        robot.StateWaiting,
		Healthy:       true,
	}

	recs := []bv.TriageRecommendation{
		{ID: "ntm-001", Title: "Blocked Task", Status: "blocked", Score: 0.9},
		{ID: "ntm-002", Title: "Ready Task", Status: "open", Priority: 1, Score: 0.8},
		{ID: "ntm-003", Title: "Another Ready", Status: "open", Priority: 2, Score: 0.7},
	}

	assignment, rec := c.findBestMatch(agent, recs)

	if assignment == nil {
		t.Fatal("expected assignment, got nil")
	}
	if rec == nil {
		t.Fatal("expected recommendation, got nil")
	}
	if assignment.BeadID != "ntm-002" {
		t.Errorf("expected BeadID 'ntm-002' (first non-blocked), got %q", assignment.BeadID)
	}
	if assignment.AgentMailName != "BlueFox" {
		t.Errorf("expected AgentMailName 'BlueFox', got %q", assignment.AgentMailName)
	}
}

func TestFindBestMatchAllBlocked(t *testing.T) {
	c := New("test-session", "/tmp/test", nil, "TestAgent")

	agent := &AgentState{
		PaneID:    "%0",
		AgentType: "cc",
	}

	recs := []bv.TriageRecommendation{
		{ID: "ntm-001", Title: "Blocked 1", Status: "blocked"},
		{ID: "ntm-002", Title: "Blocked 2", Status: "blocked"},
	}

	assignment, rec := c.findBestMatch(agent, recs)

	if assignment != nil {
		t.Error("expected nil assignment when all are blocked")
	}
	if rec != nil {
		t.Error("expected nil recommendation when all are blocked")
	}
}

func TestFindBestMatchEmpty(t *testing.T) {
	c := New("test-session", "/tmp/test", nil, "TestAgent")

	agent := &AgentState{
		PaneID:    "%0",
		AgentType: "cc",
	}

	assignment, rec := c.findBestMatch(agent, nil)

	if assignment != nil || rec != nil {
		t.Error("expected nil for empty recommendations")
	}

	assignment, rec = c.findBestMatch(agent, []bv.TriageRecommendation{})

	if assignment != nil || rec != nil {
		t.Error("expected nil for empty slice")
	}
}

func TestFormatAssignmentMessage(t *testing.T) {
	c := New("test-session", "/tmp/test", nil, "TestAgent")

	assignment := &WorkAssignment{
		BeadID:    "ntm-1234",
		BeadTitle: "Implement feature X",
		Priority:  1,
		Score:     0.85,
	}

	rec := &bv.TriageRecommendation{
		ID:          "ntm-1234",
		Title:       "Implement feature X",
		Reasons:     []string{"High impact", "Unblocks others"},
		UnblocksIDs: []string{"ntm-2000", "ntm-2001"},
	}

	body := c.formatAssignmentMessage(assignment, rec)

	if body == "" {
		t.Error("expected non-empty message body")
	}
	if !strings.Contains(body, "# Work Assignment") {
		t.Error("expected markdown header in message")
	}
	if !strings.Contains(body, "ntm-1234") {
		t.Error("expected bead ID in message")
	}
	if !strings.Contains(body, "High impact") {
		t.Error("expected reasons in message")
	}
	if !strings.Contains(body, "bd show") {
		t.Error("expected bd show instruction in message")
	}
}

func TestDefaultScoreConfig(t *testing.T) {
	config := DefaultScoreConfig()

	if !config.PreferCriticalPath {
		t.Error("expected PreferCriticalPath to be true by default")
	}
	if !config.PenalizeFileOverlap {
		t.Error("expected PenalizeFileOverlap to be true by default")
	}
	if !config.UseAgentProfiles {
		t.Error("expected UseAgentProfiles to be true by default")
	}
	if !config.BudgetAware {
		t.Error("expected BudgetAware to be true by default")
	}
	if config.ContextThreshold != 0.8 {
		t.Errorf("expected ContextThreshold 0.8, got %f", config.ContextThreshold)
	}
}

func TestEstimateTaskComplexity(t *testing.T) {
	tests := []struct {
		name     string
		rec      *bv.TriageRecommendation
		expected float64
		minExp   float64
		maxExp   float64
	}{
		{
			name:   "epic is complex",
			rec:    &bv.TriageRecommendation{Type: "epic", Priority: 2},
			minExp: 0.7,
			maxExp: 1.0,
		},
		{
			name:   "chore is simple",
			rec:    &bv.TriageRecommendation{Type: "chore", Priority: 2},
			minExp: 0.0,
			maxExp: 0.4,
		},
		{
			name:   "feature is moderately complex",
			rec:    &bv.TriageRecommendation{Type: "feature", Priority: 2},
			minExp: 0.6,
			maxExp: 0.8,
		},
		{
			name:   "epic with many unblocks is very complex",
			rec:    &bv.TriageRecommendation{Type: "epic", Priority: 2, UnblocksIDs: []string{"a", "b", "c", "d", "e"}},
			minExp: 0.9,
			maxExp: 1.0,
		},
		{
			name:   "critical bug is simpler",
			rec:    &bv.TriageRecommendation{Type: "bug", Priority: 0},
			minExp: 0.3,
			maxExp: 0.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complexity := estimateTaskComplexity(tt.rec)
			if complexity < tt.minExp || complexity > tt.maxExp {
				t.Errorf("expected complexity in [%f, %f], got %f", tt.minExp, tt.maxExp, complexity)
			}
		})
	}
}

func TestComputeAgentTypeBonus(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
		rec       *bv.TriageRecommendation
		wantSign  string // "positive", "negative", "zero"
	}{
		{
			name:      "claude on epic gets bonus",
			agentType: "cc",
			rec:       &bv.TriageRecommendation{Type: "epic", Priority: 2},
			wantSign:  "positive",
		},
		{
			name:      "claude on chore gets penalty",
			agentType: "claude",
			rec:       &bv.TriageRecommendation{Type: "chore", Priority: 2},
			wantSign:  "negative",
		},
		{
			name:      "codex on chore gets bonus",
			agentType: "cod",
			rec:       &bv.TriageRecommendation{Type: "chore", Priority: 2},
			wantSign:  "positive",
		},
		{
			name:      "codex on epic gets penalty",
			agentType: "codex",
			rec:       &bv.TriageRecommendation{Type: "epic", Priority: 2},
			wantSign:  "negative",
		},
		{
			name:      "gemini on medium task neutral or small bonus",
			agentType: "gmi",
			rec:       &bv.TriageRecommendation{Type: "task", Priority: 2},
			wantSign:  "zero", // task is medium complexity
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bonus := computeAgentTypeBonus(tt.agentType, tt.rec)
			switch tt.wantSign {
			case "positive":
				if bonus <= 0 {
					t.Errorf("expected positive bonus, got %f", bonus)
				}
			case "negative":
				if bonus >= 0 {
					t.Errorf("expected negative bonus, got %f", bonus)
				}
			case "zero":
				if bonus < -0.05 || bonus > 0.1 {
					t.Errorf("expected near-zero bonus, got %f", bonus)
				}
			}
		})
	}
}

func TestComputeContextPenalty(t *testing.T) {
	tests := []struct {
		name         string
		contextUsage float64
		threshold    float64
		wantZero     bool
	}{
		{"below threshold", 0.5, 0.8, true},
		{"at threshold", 0.8, 0.8, true},
		{"above threshold", 0.9, 0.8, false},
		{"way above threshold", 0.95, 0.8, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			penalty := computeContextPenalty(tt.contextUsage, tt.threshold)
			if tt.wantZero && penalty != 0 {
				t.Errorf("expected zero penalty, got %f", penalty)
			}
			if !tt.wantZero && penalty <= 0 {
				t.Errorf("expected positive penalty, got %f", penalty)
			}
		})
	}
}

func TestComputeFileOverlapPenalty(t *testing.T) {
	tests := []struct {
		name         string
		agent        *AgentState
		reservations map[string][]string
		wantZero     bool
	}{
		{
			name:         "no reservations",
			agent:        &AgentState{PaneID: "%0"},
			reservations: nil,
			wantZero:     true,
		},
		{
			name:         "agent with reservations",
			agent:        &AgentState{PaneID: "%0", Reservations: []string{"a.go", "b.go", "c.go"}},
			reservations: nil,
			wantZero:     false,
		},
		{
			name:         "reservations in map",
			agent:        &AgentState{PaneID: "%0"},
			reservations: map[string][]string{"%0": {"x.go"}},
			wantZero:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			penalty := computeFileOverlapPenalty(tt.agent, tt.reservations)
			if tt.wantZero && penalty != 0 {
				t.Errorf("expected zero penalty, got %f", penalty)
			}
			if !tt.wantZero && penalty <= 0 {
				t.Errorf("expected positive penalty, got %f", penalty)
			}
		})
	}
}

func TestScoreAndSelectAssignments(t *testing.T) {
	agents := []*AgentState{
		{PaneID: "%1", AgentType: "cc", ContextUsage: 0.3, Status: robot.StateWaiting},
		{PaneID: "%2", AgentType: "cod", ContextUsage: 0.5, Status: robot.StateWaiting},
	}

	triage := &bv.TriageResponse{
		Triage: bv.TriageData{
			Recommendations: []bv.TriageRecommendation{
				{ID: "ntm-001", Title: "Epic task", Type: "epic", Status: "open", Priority: 2, Score: 0.8},
				{ID: "ntm-002", Title: "Quick fix", Type: "chore", Status: "open", Priority: 2, Score: 0.6},
				{ID: "ntm-003", Title: "Blocked", Type: "task", Status: "blocked", Priority: 2, Score: 0.9},
			},
		},
	}

	config := DefaultScoreConfig()
	results := ScoreAndSelectAssignments(agents, triage, config, nil)

	if len(results) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(results))
	}

	// Verify each agent got exactly one task
	agentTasks := make(map[string]string)
	for _, r := range results {
		if existing, ok := agentTasks[r.Agent.PaneID]; ok {
			t.Errorf("agent %s assigned twice: %s and %s", r.Agent.PaneID, existing, r.Assignment.BeadID)
		}
		agentTasks[r.Agent.PaneID] = r.Assignment.BeadID
	}

	// Verify blocked task not assigned
	for _, r := range results {
		if r.Assignment.BeadID == "ntm-003" {
			t.Error("blocked task should not be assigned")
		}
	}
}

func TestScoreAndSelectAssignmentsEmpty(t *testing.T) {
	// Empty agents
	result := ScoreAndSelectAssignments(nil, &bv.TriageResponse{}, DefaultScoreConfig(), nil)
	if result != nil {
		t.Error("expected nil for empty agents")
	}

	// Empty triage
	agents := []*AgentState{{PaneID: "%0", AgentType: "cc"}}
	result = ScoreAndSelectAssignments(agents, nil, DefaultScoreConfig(), nil)
	if result != nil {
		t.Error("expected nil for nil triage")
	}

	// Empty recommendations
	result = ScoreAndSelectAssignments(agents, &bv.TriageResponse{}, DefaultScoreConfig(), nil)
	if result != nil {
		t.Error("expected nil for empty recommendations")
	}
}

func TestComputeCriticalPathBonus(t *testing.T) {
	tests := []struct {
		name      string
		breakdown *bv.ScoreBreakdown
		wantZero  bool
	}{
		{
			name:      "low pagerank",
			breakdown: &bv.ScoreBreakdown{Pagerank: 0.01, BlockerRatio: 0.01},
			wantZero:  true,
		},
		{
			name:      "high pagerank",
			breakdown: &bv.ScoreBreakdown{Pagerank: 0.1, BlockerRatio: 0.01},
			wantZero:  false,
		},
		{
			name:      "high blocker ratio",
			breakdown: &bv.ScoreBreakdown{Pagerank: 0.01, BlockerRatio: 0.1},
			wantZero:  false,
		},
		{
			name:      "high time to impact",
			breakdown: &bv.ScoreBreakdown{Pagerank: 0.01, BlockerRatio: 0.01, TimeToImpact: 0.06},
			wantZero:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bonus := computeCriticalPathBonus(tt.breakdown)
			if tt.wantZero && bonus != 0 {
				t.Errorf("expected zero bonus, got %f", bonus)
			}
			if !tt.wantZero && bonus <= 0 {
				t.Errorf("expected positive bonus, got %f", bonus)
			}
		})
	}
}
