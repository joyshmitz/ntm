package robot

import (
	"testing"
)

// =============================================================================
// CalculateHealthScore Tests
// =============================================================================

func TestCalculateHealthScore_PerfectHealth(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		IsWorking:  true,
		Confidence: 0.9,
	}

	score := CalculateHealthScore(state, nil)
	if score != 100 {
		t.Errorf("perfect health score = %d, want 100", score)
	}
}

func TestCalculateHealthScore_RateLimited(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:     "claude",
		IsRateLimited: true,
		Confidence:    0.9,
	}

	score := CalculateHealthScore(state, nil)
	// Rate limited deducts 50
	if score != 50 {
		t.Errorf("rate limited score = %d, want 50", score)
	}
}

func TestCalculateHealthScore_ErrorState(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:      "claude",
		Recommendation: "ERROR_STATE",
		Confidence:     0.9,
	}

	score := CalculateHealthScore(state, nil)
	// Error state deducts 40
	if score != 60 {
		t.Errorf("error state score = %d, want 60", score)
	}
}

func TestCalculateHealthScore_ContextLowIdle(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:    "claude",
		IsContextLow: true,
		IsIdle:       true,
		IsWorking:    false,
		Confidence:   0.9,
	}

	score := CalculateHealthScore(state, nil)
	// Context low + idle deducts 25
	if score != 75 {
		t.Errorf("context low idle score = %d, want 75", score)
	}
}

func TestCalculateHealthScore_ContextLowWorking(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:    "claude",
		IsContextLow: true,
		IsWorking:    true,
		Confidence:   0.9,
	}

	score := CalculateHealthScore(state, nil)
	// Context low + working deducts 10
	if score != 90 {
		t.Errorf("context low working score = %d, want 90", score)
	}
}

func TestCalculateHealthScore_UnknownAgent(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "unknown",
		Confidence: 0.9,
	}

	score := CalculateHealthScore(state, nil)
	// Unknown agent deducts 15
	if score != 85 {
		t.Errorf("unknown agent score = %d, want 85", score)
	}
}

func TestCalculateHealthScore_LowConfidence(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		Confidence: 0.3,
	}

	score := CalculateHealthScore(state, nil)
	// Low confidence deducts 10
	if score != 90 {
		t.Errorf("low confidence score = %d, want 90", score)
	}
}

func TestCalculateHealthScore_MultipleIssues(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:      "unknown",
		IsRateLimited:  true,
		IsContextLow:   true,
		IsIdle:         true,
		Recommendation: "ERROR_STATE",
		Confidence:     0.3,
	}

	score := CalculateHealthScore(state, nil)
	// Rate limited (-50) + error (-40) + context low idle (-25) + unknown (-15) + low conf (-10) = -40
	// Floor at 0
	if score != 0 {
		t.Errorf("multiple issues score = %d, want 0", score)
	}
}

func TestCalculateHealthScore_FloorAtZero(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:      "unknown",
		IsRateLimited:  true,
		Recommendation: "ERROR_STATE",
		Confidence:     0.1,
	}

	score := CalculateHealthScore(state, nil)
	if score < 0 {
		t.Errorf("score %d should not be negative", score)
	}
}

// =============================================================================
// HealthGrade Tests
// =============================================================================

func TestHealthGrade_ScoreBoundaries(t *testing.T) {
	tests := []struct {
		score int
		grade string
	}{
		{100, "A"},
		{95, "A"},
		{90, "A"},
		{89, "B"},
		{85, "B"},
		{80, "B"},
		{79, "C"},
		{75, "C"},
		{70, "C"},
		{69, "D"},
		{50, "D"},
		{49, "F"},
		{25, "F"},
		{0, "F"},
	}

	for _, tt := range tests {
		t.Run(tt.grade, func(t *testing.T) {
			got := HealthGrade(tt.score)
			if got != tt.grade {
				t.Errorf("HealthGrade(%d) = %q, want %q", tt.score, got, tt.grade)
			}
		})
	}
}

// =============================================================================
// CollectIssues Tests
// =============================================================================

func TestCollectIssues_RateLimited(t *testing.T) {
	state := &PaneWorkStatus{
		IsRateLimited: true,
		Confidence:    0.9,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "Rate limited") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should contain rate limit warning", issues)
	}
}

func TestCollectIssues_ErrorState(t *testing.T) {
	state := &PaneWorkStatus{
		Recommendation: "ERROR_STATE",
		Confidence:     0.9,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "error state") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should contain error state warning", issues)
	}
}

func TestCollectIssues_ContextLow(t *testing.T) {
	remaining := 15.0
	state := &PaneWorkStatus{
		IsContextLow:     true,
		ContextRemaining: &remaining,
		Confidence:       0.9,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "Context remaining") || contains(issue, "threshold") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should contain context low warning", issues)
	}
}

func TestCollectIssues_IdleAgent(t *testing.T) {
	state := &PaneWorkStatus{
		IsIdle:     true,
		Confidence: 0.9,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "idle") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should mention idle agent", issues)
	}
}

func TestCollectIssues_UnknownAgent(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "unknown",
		Confidence: 0.9,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "determine agent type") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should mention unknown agent type", issues)
	}
}

func TestCollectIssues_LowConfidence(t *testing.T) {
	state := &PaneWorkStatus{
		Confidence: 0.2,
	}

	issues := CollectIssues(state, nil)

	found := false
	for _, issue := range issues {
		if contains(issue, "confidence") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("issues %v should mention low confidence", issues)
	}
}

func TestCollectIssues_NoIssues(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		IsWorking:  true,
		Confidence: 0.9,
	}

	issues := CollectIssues(state, nil)

	if len(issues) != 0 {
		t.Errorf("expected no issues for healthy agent, got %v", issues)
	}
}

// =============================================================================
// DeriveHealthRecommendation Tests
// =============================================================================

func TestDeriveHealthRecommendation_RateLimited(t *testing.T) {
	state := &PaneWorkStatus{
		IsRateLimited: true,
		Confidence:    0.9,
	}

	rec, reason := DeriveHealthRecommendation(state, nil, 50)

	if rec != RecommendWaitForReset {
		t.Errorf("recommendation = %q, want %q", rec, RecommendWaitForReset)
	}
	if reason == "" {
		t.Error("reason should not be empty")
	}
}

func TestDeriveHealthRecommendation_ErrorState(t *testing.T) {
	state := &PaneWorkStatus{
		Recommendation: "ERROR_STATE",
		Confidence:     0.9,
	}

	rec, _ := DeriveHealthRecommendation(state, nil, 60)

	if rec != RecommendRestartUrgent {
		t.Errorf("recommendation = %q, want %q", rec, RecommendRestartUrgent)
	}
}

func TestDeriveHealthRecommendation_ContextLowIdle(t *testing.T) {
	state := &PaneWorkStatus{
		IsContextLow: true,
		IsIdle:       true,
		Confidence:   0.9,
	}

	rec, reason := DeriveHealthRecommendation(state, nil, 75)

	if rec != RecommendRestartRecommended {
		t.Errorf("recommendation = %q, want %q", rec, RecommendRestartRecommended)
	}
	if !contains(reason, "restart") {
		t.Errorf("reason %q should mention restart", reason)
	}
}

func TestDeriveHealthRecommendation_UnknownStuck(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "unknown",
		Confidence: 0.2,
	}

	rec, _ := DeriveHealthRecommendation(state, nil, 60)

	if rec != RecommendRestartUrgent {
		t.Errorf("recommendation = %q, want %q for unknown stuck agent", rec, RecommendRestartUrgent)
	}
}

func TestDeriveHealthRecommendation_HealthyWorking(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		IsWorking:  true,
		Confidence: 0.9,
	}

	rec, _ := DeriveHealthRecommendation(state, nil, 90)

	if rec != RecommendHealthy {
		t.Errorf("recommendation = %q, want %q", rec, RecommendHealthy)
	}
}

func TestDeriveHealthRecommendation_HealthyIdle(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		IsIdle:     true,
		Confidence: 0.9,
	}

	rec, reason := DeriveHealthRecommendation(state, nil, 85)

	if rec != RecommendHealthy {
		t.Errorf("recommendation = %q, want %q", rec, RecommendHealthy)
	}
	if !contains(reason, "ready for work") {
		t.Errorf("reason %q should mention ready for work", reason)
	}
}

func TestDeriveHealthRecommendation_MonitorRange(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		Confidence: 0.9,
	}

	rec, _ := DeriveHealthRecommendation(state, nil, 55)

	if rec != RecommendMonitor {
		t.Errorf("recommendation = %q, want %q for score 55", rec, RecommendMonitor)
	}
}

func TestDeriveHealthRecommendation_LowScore(t *testing.T) {
	state := &PaneWorkStatus{
		AgentType:  "claude",
		Confidence: 0.9,
	}

	rec, _ := DeriveHealthRecommendation(state, nil, 30)

	if rec != RecommendRestartRecommended {
		t.Errorf("recommendation = %q, want %q for score 30", rec, RecommendRestartRecommended)
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestHealthRecommendationConstants_Distinct(t *testing.T) {
	// Ensure all recommendation constants have distinct values
	recs := []HealthRecommendation{
		RecommendHealthy,
		RecommendMonitor,
		RecommendRestartRecommended,
		RecommendRestartUrgent,
		RecommendWaitForReset,
		RecommendSwitchAccount,
	}

	seen := make(map[HealthRecommendation]bool)
	for _, rec := range recs {
		if seen[rec] {
			t.Errorf("duplicate recommendation constant: %q", rec)
		}
		seen[rec] = true
		if string(rec) == "" {
			t.Error("recommendation constant should not be empty")
		}
	}
}
