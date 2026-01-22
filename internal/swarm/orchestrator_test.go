package swarm

import (
	"testing"
	"time"
)

func TestNewSessionOrchestrator(t *testing.T) {
	orch := NewSessionOrchestrator()

	if orch == nil {
		t.Fatal("expected non-nil orchestrator")
	}

	if orch.TmuxClient != nil {
		t.Error("expected nil TmuxClient (use default)")
	}

	if orch.StaggerDelay != 300*time.Millisecond {
		t.Errorf("expected StaggerDelay 300ms, got %v", orch.StaggerDelay)
	}
}

func TestSessionOrchestrator_CreateSessions_NilPlan(t *testing.T) {
	orch := NewSessionOrchestrator()

	result, err := orch.CreateSessions(nil)

	if err == nil {
		t.Error("expected error for nil plan")
	}

	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

func TestSessionOrchestrator_CreateSessions_EmptyPlan(t *testing.T) {
	orch := NewSessionOrchestrator()

	plan := &SwarmPlan{
		Sessions: []SessionSpec{},
	}

	result, err := orch.CreateSessions(plan)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(result.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(result.Sessions))
	}
}

func TestSessionOrchestrator_FormatPaneTitle(t *testing.T) {
	orch := NewSessionOrchestrator()

	tests := []struct {
		sessionName string
		pane        PaneSpec
		expected    string
	}{
		{
			sessionName: "cc_agents_1",
			pane:        PaneSpec{Index: 1, AgentType: "cc"},
			expected:    "cc_agents_1__cc_1",
		},
		{
			sessionName: "cod_agents_2",
			pane:        PaneSpec{Index: 5, AgentType: "cod"},
			expected:    "cod_agents_2__cod_5",
		},
		{
			sessionName: "gmi_agents_3",
			pane:        PaneSpec{Index: 3, AgentType: "gmi"},
			expected:    "gmi_agents_3__gmi_3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := orch.formatPaneTitle(tt.sessionName, tt.pane)
			if got != tt.expected {
				t.Errorf("formatPaneTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCreateSessionResult(t *testing.T) {
	result := CreateSessionResult{
		SessionSpec: SessionSpec{
			Name:      "test_session",
			AgentType: "cc",
			PaneCount: 4,
		},
		SessionName: "test_session",
		PaneIDs:     []string{"%1", "%2", "%3", "%4"},
		Error:       nil,
	}

	if result.SessionName != "test_session" {
		t.Errorf("expected session name 'test_session', got %q", result.SessionName)
	}

	if len(result.PaneIDs) != 4 {
		t.Errorf("expected 4 pane IDs, got %d", len(result.PaneIDs))
	}

	if result.Error != nil {
		t.Errorf("expected nil error, got %v", result.Error)
	}
}

func TestOrchestrationResult(t *testing.T) {
	result := OrchestrationResult{
		Sessions: []CreateSessionResult{
			{
				SessionName: "cc_agents_1",
				PaneIDs:     []string{"%1", "%2", "%3"},
			},
			{
				SessionName: "cod_agents_1",
				PaneIDs:     []string{"%4", "%5"},
			},
		},
		TotalPanes:      5,
		SuccessfulPanes: 5,
		FailedPanes:     0,
		Errors:          nil,
	}

	if len(result.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(result.Sessions))
	}

	if result.TotalPanes != 5 {
		t.Errorf("expected 5 total panes, got %d", result.TotalPanes)
	}

	if result.SuccessfulPanes != 5 {
		t.Errorf("expected 5 successful panes, got %d", result.SuccessfulPanes)
	}

	if result.FailedPanes != 0 {
		t.Errorf("expected 0 failed panes, got %d", result.FailedPanes)
	}

	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestOrchestrationResultWithErrors(t *testing.T) {
	result := OrchestrationResult{
		Sessions: []CreateSessionResult{
			{
				SessionName: "cc_agents_1",
				PaneIDs:     []string{"%1", "%2"},
				Error:       nil,
			},
		},
		TotalPanes:      4,
		SuccessfulPanes: 2,
		FailedPanes:     2,
		Errors:          []error{nil}, // Placeholder for test
	}

	if result.SuccessfulPanes != 2 {
		t.Errorf("expected 2 successful panes, got %d", result.SuccessfulPanes)
	}

	if result.FailedPanes != 2 {
		t.Errorf("expected 2 failed panes, got %d", result.FailedPanes)
	}
}

func TestPaneGeometry(t *testing.T) {
	geom := PaneGeometry{
		Width:  120,
		Height: 40,
	}

	if geom.Width != 120 {
		t.Errorf("expected width 120, got %d", geom.Width)
	}

	if geom.Height != 40 {
		t.Errorf("expected height 40, got %d", geom.Height)
	}
}

func TestGeometryResult_Uniform(t *testing.T) {
	result := GeometryResult{
		SessionName: "test_session",
		PaneCount:   4,
		Geometries: []PaneGeometry{
			{Width: 80, Height: 24},
			{Width: 80, Height: 24},
			{Width: 80, Height: 24},
			{Width: 80, Height: 24},
		},
		IsUniform:      true,
		MaxWidthDelta:  0,
		MaxHeightDelta: 0,
	}

	if result.SessionName != "test_session" {
		t.Errorf("expected session name 'test_session', got %q", result.SessionName)
	}

	if result.PaneCount != 4 {
		t.Errorf("expected 4 panes, got %d", result.PaneCount)
	}

	if !result.IsUniform {
		t.Error("expected uniform geometry")
	}

	if result.MaxWidthDelta != 0 {
		t.Errorf("expected 0 width delta, got %d", result.MaxWidthDelta)
	}

	if result.MaxHeightDelta != 0 {
		t.Errorf("expected 0 height delta, got %d", result.MaxHeightDelta)
	}
}

func TestGeometryResult_NonUniform(t *testing.T) {
	result := GeometryResult{
		SessionName: "test_session",
		PaneCount:   3,
		Geometries: []PaneGeometry{
			{Width: 100, Height: 30},
			{Width: 80, Height: 24},
			{Width: 90, Height: 27},
		},
		IsUniform:      false,
		MaxWidthDelta:  20,
		MaxHeightDelta: 6,
	}

	if result.IsUniform {
		t.Error("expected non-uniform geometry")
	}

	if result.MaxWidthDelta != 20 {
		t.Errorf("expected 20 width delta, got %d", result.MaxWidthDelta)
	}

	if result.MaxHeightDelta != 6 {
		t.Errorf("expected 6 height delta, got %d", result.MaxHeightDelta)
	}
}

func TestGeometryResult_EmptySession(t *testing.T) {
	result := GeometryResult{
		SessionName: "empty_session",
		PaneCount:   0,
		Geometries:  []PaneGeometry{},
		IsUniform:   true,
	}

	if !result.IsUniform {
		t.Error("empty session should be considered uniform")
	}

	if result.PaneCount != 0 {
		t.Errorf("expected 0 panes, got %d", result.PaneCount)
	}
}

func TestGeometryResult_WithTolerance(t *testing.T) {
	// Simulating geometry check with tolerance of 2
	// Panes with 1 cell difference should be considered uniform
	geometries := []PaneGeometry{
		{Width: 80, Height: 24},
		{Width: 81, Height: 24}, // 1 cell wider
		{Width: 80, Height: 25}, // 1 cell taller
	}

	maxWidthDelta := 1
	maxHeightDelta := 1
	tolerance := 2

	isUniform := maxWidthDelta <= tolerance && maxHeightDelta <= tolerance

	if !isUniform {
		t.Error("expected uniform with tolerance=2")
	}

	if len(geometries) != 3 {
		t.Errorf("expected 3 geometries, got %d", len(geometries))
	}
}
