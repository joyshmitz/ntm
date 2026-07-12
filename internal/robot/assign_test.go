package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/bv"
)

// =============================================================================
// Task Type Inference Tests
// =============================================================================

func TestInferTaskType_BugKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Fix authentication bug", "bug"},
		{"Bug: login fails on retry", "bug"},
		{"broken pagination in status", "bug"},
		{"Error handling in tmux client", "bug"},
		{"Crash on empty session name", "bug"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P1"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_TestKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Write unit tests for assign module", "testing"},
		{"Add test coverage for robot pkg", "testing"},
		{"Spec: robot env output format", "testing"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P2"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_FeatureKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Feature: Add robot-env command", "feature"},
		{"Implement bead assignment engine", "feature"},
		{"Add tab completion support", "feature"},
		{"New ensemble mode catalog", "feature"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P2"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_RefactorKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Refactor robot output types", "refactor"},
		{"Cleanup unused helpers in tmux pkg", "refactor"},
		{"Improve code structure patterns", "refactor"},
		{"Consolidate send command logic", "refactor"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P2"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_DocumentationKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Update README with ensemble docs", "documentation"},
		{"Add documentation for robot flags", "documentation"},
		{"Comment complex assignment logic", "documentation"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P3"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_AnalysisKeywords(t *testing.T) {
	tests := []struct {
		title    string
		expected string
	}{
		{"Analyze rate limit patterns", "analysis"},
		{"Investigate CI failure", "analysis"},
		{"Research WebSocket alternatives", "analysis"},
		{"Design plugin architecture", "analysis"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: tt.title, Priority: "P2"}
			got := inferTaskType(bead)
			if got != tt.expected {
				t.Errorf("inferTaskType(%q) = %q, want %q", tt.title, got, tt.expected)
			}
		})
	}
}

func TestInferTaskType_Default(t *testing.T) {
	// Titles with no matching keywords should default to "task"
	tests := []string{
		"EPIC: Something",
		"Phase 2 milestone",
		"Integration with external service",
		"",
	}

	for _, title := range tests {
		t.Run(title, func(t *testing.T) {
			bead := bv.BeadPreview{ID: "bd-test", Title: title, Priority: "P2"}
			got := inferTaskType(bead)
			if got != "task" {
				t.Errorf("inferTaskType(%q) = %q, want %q", title, got, "task")
			}
		})
	}
}

func TestInferTaskType_PriorityOrder(t *testing.T) {
	// "Fix" matches bug before feature ("fix" contains no feature keyword)
	// "Fix and add test coverage" should match "bug" because "fix" comes first in the rules
	bead := bv.BeadPreview{ID: "bd-test", Title: "Fix and add test coverage", Priority: "P1"}
	got := inferTaskType(bead)
	if got != "bug" {
		t.Errorf("inferTaskType('Fix and add test coverage') = %q, want %q (bug should match before testing)", got, "bug")
	}
}

func TestInferTaskType_CaseInsensitive(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "BUG: Session Not Found", Priority: "P1"}
	got := inferTaskType(bead)
	if got != "bug" {
		t.Errorf("inferTaskType should be case-insensitive, got %q", got)
	}
}

// =============================================================================
// Priority Parsing Tests
// =============================================================================

func TestParsePriority_Assign(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"P0", 0},
		{"P1", 1},
		{"P2", 2},
		{"P3", 3},
		{"P4", 4},
		{"P5", 2},  // Out of range, defaults to 2
		{"P9", 2},  // Out of range
		{"", 2},    // Empty
		{"Q1", 2},  // Wrong prefix
		{"p1", 2},  // Wrong case (lowercase p)
		{"PP", 2},  // Malformed
		{"P", 2},   // Too short
		{"P12", 2}, // Too long
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parsePriority(tt.input)
			if got != tt.expected {
				t.Errorf("parsePriority(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Confidence Calculation Tests
// =============================================================================

func TestCalculateConfidence_BalancedStrategy(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Fix login bug", Priority: "P1"}
	conf := calculateConfidence("claude", bead, "balanced")

	if conf < 0 || conf > 1.0 {
		t.Errorf("confidence %f should be between 0.0 and 1.0", conf)
	}
}

func TestCalculateConfidence_SpeedStrategy(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Add feature", Priority: "P2"}

	speedConf := calculateConfidence("claude", bead, "speed")
	balancedConf := calculateConfidence("claude", bead, "balanced")

	// Speed should average with 0.9, so typically higher than balanced
	if speedConf < 0 || speedConf > 1.0 {
		t.Errorf("speed confidence %f out of range", speedConf)
	}
	// Speed uses (base + 0.9) / 2, so should generally be >= 0.45
	if speedConf < 0.45 {
		t.Errorf("speed confidence %f suspiciously low (formula should average with 0.9)", speedConf)
	}
	_ = balancedConf
}

func TestCalculateConfidence_DependencyStrategy(t *testing.T) {
	// High priority bead should get a boost in dependency strategy
	p0Bead := bv.BeadPreview{ID: "bd-test", Title: "Some task", Priority: "P0"}
	p1Bead := bv.BeadPreview{ID: "bd-test", Title: "Some task", Priority: "P1"}
	p3Bead := bv.BeadPreview{ID: "bd-test", Title: "Some task", Priority: "P3"}

	p0Conf := calculateConfidence("claude", p0Bead, "dependency")
	p1Conf := calculateConfidence("claude", p1Bead, "dependency")
	p3Conf := calculateConfidence("claude", p3Bead, "dependency")

	// P0 and P1 should get a boost over P3
	if p0Conf <= p3Conf {
		t.Errorf("P0 confidence (%f) should be higher than P3 (%f) in dependency strategy", p0Conf, p3Conf)
	}
	if p1Conf <= p3Conf {
		t.Errorf("P1 confidence (%f) should be higher than P3 (%f) in dependency strategy", p1Conf, p3Conf)
	}
}

func TestCalculateConfidence_QualityStrategy(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Write unit tests", Priority: "P2"}
	conf := calculateConfidence("claude", bead, "quality")

	// Quality strategy doesn't modify base confidence currently
	if conf < 0 || conf > 1.0 {
		t.Errorf("quality confidence %f out of range", conf)
	}
}

func TestCalculateConfidence_CapAt095(t *testing.T) {
	// Dependency strategy caps at 0.95
	bead := bv.BeadPreview{ID: "bd-test", Title: "Fix critical bug", Priority: "P0"}
	conf := calculateConfidence("claude", bead, "dependency")

	if conf > 0.95 {
		t.Errorf("confidence %f should not exceed 0.95", conf)
	}
}

// =============================================================================
// Reasoning Generation Tests
// =============================================================================

func TestGenerateReasoning_HighStrengthAgent(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Fix login bug", Priority: "P0"}
	reasoning := generateReasoning("claude", bead, "balanced")

	if reasoning == "" {
		t.Error("reasoning should not be empty")
	}

	// Should contain strategy reasoning
	if !strings.Contains(reasoning, "balanced") {
		t.Errorf("reasoning %q should mention balanced strategy", reasoning)
	}

	// P0 should get "critical priority" reasoning
	if !strings.Contains(reasoning, "critical priority") {
		t.Errorf("reasoning %q should mention critical priority for P0 bead", reasoning)
	}
}

func TestGenerateReasoning_P1Priority(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Some task", Priority: "P1"}
	reasoning := generateReasoning("claude", bead, "speed")

	if !strings.Contains(reasoning, "high priority") {
		t.Errorf("reasoning %q should mention high priority for P1", reasoning)
	}
	if !strings.Contains(reasoning, "speed") {
		t.Errorf("reasoning %q should mention speed strategy", reasoning)
	}
}

func TestGenerateReasoning_AllStrategies(t *testing.T) {
	bead := bv.BeadPreview{ID: "bd-test", Title: "Some task", Priority: "P2"}
	strategies := []string{"balanced", "speed", "quality", "dependency"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			reasoning := generateReasoning("claude", bead, strategy)
			if reasoning == "" {
				t.Error("reasoning should not be empty")
			}
			if !strings.Contains(reasoning, strategy) {
				t.Errorf("reasoning %q should mention strategy %q", reasoning, strategy)
			}
		})
	}
}

func TestGenerateReasoning_DefaultFallback(t *testing.T) {
	// P2 bead with unknown strategy should still produce output
	bead := bv.BeadPreview{ID: "bd-test", Title: "Integration work", Priority: "P2"}
	reasoning := generateReasoning("claude", bead, "unknown")

	if reasoning == "" {
		t.Error("reasoning should not be empty even with unknown strategy")
	}
}

// =============================================================================
// Agent Hints Generation Tests
// =============================================================================

func TestGenerateAssignHints_NoWork(t *testing.T) {
	hints := generateAssignHints(nil, nil, nil, nil)

	if hints.Summary != "No work available to assign" {
		t.Errorf("Summary = %q, want %q", hints.Summary, "No work available to assign")
	}
}

func TestGenerateAssignHints_NoIdleAgents(t *testing.T) {
	beads := []bv.BeadPreview{{ID: "bd-1", Title: "Task 1", Priority: "P1"}}
	hints := generateAssignHints(nil, nil, beads, nil)

	if !strings.Contains(hints.Summary, "no idle agents") {
		t.Errorf("Summary %q should mention no idle agents", hints.Summary)
	}
}

func TestGenerateAssignHints_WithRecommendations(t *testing.T) {
	recs := []AssignRecommend{
		{Agent: "2", AssignBead: "bd-abc", AgentType: "claude"},
		{Agent: "3", AssignBead: "bd-def", AgentType: "codex"},
	}
	idleAgents := []string{"2", "3"}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "Task A"},
		{ID: "bd-def", Title: "Task B"},
	}

	hints := generateAssignHints(recs, idleAgents, beads, nil)

	if !strings.Contains(hints.Summary, "2 assignments") {
		t.Errorf("Summary %q should mention 2 assignments", hints.Summary)
	}
	if len(hints.SuggestedCommands) != 2 {
		t.Errorf("SuggestedCommands count = %d, want 2", len(hints.SuggestedCommands))
	}
}

func TestGenerateAssignHints_MoreBeadsThanAgents(t *testing.T) {
	recs := []AssignRecommend{{Agent: "2", AssignBead: "bd-abc"}}
	idleAgents := []string{"2"}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "A"},
		{ID: "bd-def", Title: "B"},
		{ID: "bd-ghi", Title: "C"},
	}

	hints := generateAssignHints(recs, idleAgents, beads, nil)

	if len(hints.Warnings) == 0 {
		t.Error("should warn about unassigned beads")
	}
	found := false
	for _, w := range hints.Warnings {
		if strings.Contains(w, "won't be assigned") {
			found = true
			break
		}
	}
	if !found {
		t.Error("warnings should mention beads that won't be assigned")
	}
}

func TestGenerateAssignHints_StaleInProgress(t *testing.T) {
	inProgress := []bv.BeadInProgress{
		{ID: "bd-old", Title: "Stale task", UpdatedAt: time.Now().Add(-48 * time.Hour)},
		{ID: "bd-new", Title: "Fresh task", UpdatedAt: time.Now().Add(-1 * time.Hour)},
	}

	hints := generateAssignHints(nil, nil, nil, inProgress)

	found := false
	for _, w := range hints.Warnings {
		if strings.Contains(w, "stale") {
			found = true
			break
		}
	}
	if !found {
		t.Error("should warn about stale in-progress beads")
	}
}

// =============================================================================
// Assignment Generation Tests
// =============================================================================

func TestGenerateAssignments_BasicFlow(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 2, agentType: "claude", model: "opus", state: "idle"},
		{paneIdx: 3, agentType: "codex", model: "gpt4", state: "idle"},
	}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "Fix bug", Priority: "P1"},
		{ID: "bd-def", Title: "Add feature", Priority: "P2"},
	}
	idleAgents := []string{"2", "3"}

	recs := generateAssignments(agents, beads, "balanced", idleAgents)

	if len(recs) != 2 {
		t.Fatalf("expected 2 recommendations, got %d", len(recs))
	}

	if recs[0].AssignBead != "bd-abc" {
		t.Errorf("first recommendation bead = %q, want %q", recs[0].AssignBead, "bd-abc")
	}
	if recs[0].Agent != "2" {
		t.Errorf("first recommendation agent = %q, want %q", recs[0].Agent, "2")
	}
	if recs[1].AssignBead != "bd-def" {
		t.Errorf("second recommendation bead = %q, want %q", recs[1].AssignBead, "bd-def")
	}
}

func TestGenerateAssignments_MoreAgentsThanBeads(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 2, agentType: "claude", state: "idle"},
		{paneIdx: 3, agentType: "codex", state: "idle"},
		{paneIdx: 4, agentType: "gemini", state: "idle"},
	}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "Fix bug", Priority: "P1"},
	}
	idleAgents := []string{"2", "3", "4"}

	recs := generateAssignments(agents, beads, "balanced", idleAgents)

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation (only 1 bead), got %d", len(recs))
	}
}

func TestGenerateAssignments_MoreBeadsThanAgents(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 2, agentType: "claude", state: "idle"},
	}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "Task A", Priority: "P1"},
		{ID: "bd-def", Title: "Task B", Priority: "P2"},
		{ID: "bd-ghi", Title: "Task C", Priority: "P3"},
	}
	idleAgents := []string{"2"}

	recs := generateAssignments(agents, beads, "balanced", idleAgents)

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation (only 1 idle agent), got %d", len(recs))
	}
	if recs[0].AssignBead != "bd-abc" {
		t.Errorf("should assign first bead, got %q", recs[0].AssignBead)
	}
}

func TestGenerateAssignments_NoIdleAgents(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 2, agentType: "claude", state: "working"},
	}
	beads := []bv.BeadPreview{
		{ID: "bd-abc", Title: "Task A", Priority: "P1"},
	}
	idleAgents := []string{} // None idle

	recs := generateAssignments(agents, beads, "balanced", idleAgents)

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations with no idle agents, got %d", len(recs))
	}
}

func TestGenerateAssignments_NoBeads(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 2, agentType: "claude", state: "idle"},
	}
	idleAgents := []string{"2"}

	recs := generateAssignments(agents, nil, "balanced", idleAgents)

	if len(recs) != 0 {
		t.Errorf("expected 0 recommendations with no beads, got %d", len(recs))
	}
}

func TestGenerateAssignments_RecommendationFields(t *testing.T) {
	agents := []assignAgentInfo{
		{paneIdx: 5, agentType: "claude", model: "opus-4.5", state: "idle"},
	}
	beads := []bv.BeadPreview{
		{ID: "bd-xyz", Title: "Fix critical bug", Priority: "P0"},
	}
	idleAgents := []string{"5"}

	recs := generateAssignments(agents, beads, "quality", idleAgents)

	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}

	rec := recs[0]
	if rec.Agent != "5" {
		t.Errorf("Agent = %q, want %q", rec.Agent, "5")
	}
	if rec.AgentType != "claude" {
		t.Errorf("AgentType = %q, want %q", rec.AgentType, "claude")
	}
	if rec.Model != "opus-4.5" {
		t.Errorf("Model = %q, want %q", rec.Model, "opus-4.5")
	}
	if rec.AssignBead != "bd-xyz" {
		t.Errorf("AssignBead = %q, want %q", rec.AssignBead, "bd-xyz")
	}
	if rec.BeadTitle != "Fix critical bug" {
		t.Errorf("BeadTitle = %q, want %q", rec.BeadTitle, "Fix critical bug")
	}
	if rec.Priority != "P0" {
		t.Errorf("Priority = %q, want %q", rec.Priority, "P0")
	}
	if rec.Confidence <= 0 || rec.Confidence > 1.0 {
		t.Errorf("Confidence = %f, should be (0, 1.0]", rec.Confidence)
	}
	if rec.Reasoning == "" {
		t.Error("Reasoning should not be empty")
	}
}

// =============================================================================
// AgentStrength Tests
// =============================================================================

func TestAgentStrength_ReturnsValidRange(t *testing.T) {
	agentTypes := []string{"claude", "codex", "gemini", "unknown"}
	taskTypes := []string{"bug", "testing", "feature", "refactor", "documentation", "analysis", "task"}

	for _, agent := range agentTypes {
		for _, task := range taskTypes {
			t.Run(fmt.Sprintf("%s_%s", agent, task), func(t *testing.T) {
				score := AgentStrength(agent, task)
				if score < 0 || score > 1.0 {
					t.Errorf("AgentStrength(%q, %q) = %f, should be [0, 1.0]", agent, task, score)
				}
			})
		}
	}
}

// =============================================================================
// Output Struct JSON Serialization Tests
// =============================================================================

func TestAssignOutput_JSONStructure(t *testing.T) {
	output := AssignOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       "test-session",
		Strategy:      "balanced",
		GeneratedAt:   time.Now().UTC(),
		Recommendations: []AssignRecommend{
			{Agent: "2", AgentType: "claude", AssignBead: "bd-abc", BeadTitle: "Fix bug", Priority: "P1", Confidence: 0.85, Reasoning: "test"},
		},
		BlockedBeads: []BlockedBead{
			{ID: "bd-xyz", Title: "Blocked task", BlockedBy: []string{"bd-dep1"}},
		},
		IdleAgents: []string{"2", "3"},
		Summary: AssignSummary{
			TotalAgents:     4,
			IdleAgents:      2,
			WorkingAgents:   2,
			ReadyBeads:      5,
			Recommendations: 1,
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Check top-level fields
	requiredFields := []string{
		"success", "session", "strategy", "generated_at",
		"recommendations", "blocked_beads", "idle_agents", "summary",
	}
	for _, field := range requiredFields {
		if _, ok := parsed[field]; !ok {
			t.Errorf("Missing field %q in JSON output", field)
		}
	}
}

func TestAssignOutput_OmitEmpty(t *testing.T) {
	output := AssignOutput{
		RobotResponse:   NewRobotResponse(true),
		Session:         "test",
		Recommendations: []AssignRecommend{},
		BlockedBeads:    []BlockedBead{},
		IdleAgents:      []string{},
		// UnassignableBeads is nil - should be omitted from top level
		// AgentHints is nil - should be omitted
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Parse to check top-level fields only (summary.unassignable_beads is always present)
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Top-level unassignable_beads should be omitted when nil (omitempty)
	if _, ok := parsed["unassignable_beads"]; ok {
		t.Error("top-level unassignable_beads should be omitted when nil")
	}
	// _agent_hints should be omitted when nil
	if _, ok := parsed["_agent_hints"]; ok {
		t.Error("_agent_hints should be omitted when nil")
	}
}

func TestAssignRecommend_JSONFields(t *testing.T) {
	rec := AssignRecommend{
		Agent:      "2",
		AgentType:  "claude",
		Model:      "opus-4.5",
		AssignBead: "bd-abc",
		BeadTitle:  "Fix bug",
		Priority:   "P1",
		Confidence: 0.85,
		Reasoning:  "excels at bug tasks",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["agent"] != "2" {
		t.Errorf("agent = %v, want %q", parsed["agent"], "2")
	}
	if parsed["confidence"].(float64) != 0.85 {
		t.Errorf("confidence = %v, want 0.85", parsed["confidence"])
	}
}

func TestAssignOptions_Defaults(t *testing.T) {
	opts := AssignOptions{
		Session: "test",
	}

	if opts.Strategy != "" {
		t.Error("Strategy should default to empty (normalized to 'balanced' at runtime)")
	}
	if len(opts.Beads) != 0 {
		t.Error("Beads should default to empty")
	}
}

func TestPrintAssignFailureForcesJSONAndNonzeroExitUnderTOON(t *testing.T) {
	originalFormat := GetOutputFormat()
	SetOutputFormat(FormatTOON)
	t.Cleanup(func() { SetOutputFormat(originalFormat) })

	stdout, err := captureStdout(t, func() error {
		return PrintAssign(AssignOptions{})
	})
	var exitErr *ProcessExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !exitErr.JSONWritten() {
		t.Fatalf("PrintAssign error = %T %v, want written exit-1 ProcessExitError", err, err)
	}

	var output AssignOutput
	if err := json.Unmarshal([]byte(stdout), &output); err != nil {
		t.Fatalf("PrintAssign failure is not JSON: %v\noutput=%q", err, stdout)
	}
	if output.Success || output.ErrorCode != ErrCodeInvalidFlag || output.OutputFormat != string(FormatJSON) {
		t.Fatalf("PrintAssign response = %+v, want INVALID_FLAG JSON failure", output.RobotResponse)
	}
}

func TestDistributeRecommendation_JSONFields(t *testing.T) {
	rec := DistributeRecommendation{
		BeadID:    "bd-abc",
		Title:     "Fix bug",
		PaneIndex: 2,
		AgentType: "claude",
		Reason:    "high priority",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["bead_id"] != "bd-abc" {
		t.Errorf("bead_id = %v, want %q", parsed["bead_id"], "bd-abc")
	}
	if parsed["pane_index"].(float64) != 2 {
		t.Errorf("pane_index = %v, want 2", parsed["pane_index"])
	}
}
