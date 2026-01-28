package ensemble

import (
	"testing"
)

func TestConflictTracker_FromAudit(t *testing.T) {
	report := &AuditReport{
		Conflicts: []DetailedConflict{
			{
				Topic:    "Thesis divergence",
				Severity: ConflictHigh,
				Positions: []ConflictPosition{
					{ModeID: "A1", Position: "Do X"},
					{ModeID: "B2", Position: "Avoid X"},
				},
			},
		},
	}

	tracker := NewConflictTracker()
	conflicts := tracker.FromAudit(report)

	if tracker.Source != "auditor" {
		t.Fatalf("expected source=auditor, got %q", tracker.Source)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].ModeA != "A1" || conflicts[0].ModeB != "B2" {
		t.Errorf("unexpected modes: %+v", conflicts[0])
	}
	if conflicts[0].Topic != "Thesis divergence" {
		t.Errorf("unexpected topic: %s", conflicts[0].Topic)
	}
	if conflicts[0].Severity != ConflictHigh {
		t.Errorf("unexpected severity: %s", conflicts[0].Severity)
	}
}

func TestConflictTracker_DetectConflictsFallback(t *testing.T) {
	outputs := []ModeOutput{
		{ModeID: "A1", Thesis: "alpha beta gamma"},
		{ModeID: "B2", Thesis: "delta epsilon zeta"},
	}

	tracker := NewConflictTracker()
	conflicts := tracker.DetectConflicts(outputs)

	if tracker.Source != "fallback" {
		t.Fatalf("expected source=fallback, got %q", tracker.Source)
	}
	if len(conflicts) == 0 {
		t.Fatal("expected conflicts from fallback detection")
	}
}

func TestConflictTracker_GetDensity(t *testing.T) {
	tracker := &ConflictTracker{
		Source: "auditor",
		Conflicts: []Conflict{
			{ModeA: "A1", ModeB: "B2", Resolved: true},
			{ModeA: "A1", ModeB: "B2"},
			{ModeA: "A1", ModeB: "C3"},
		},
	}

	density := tracker.GetDensity(3)

	if density.TotalConflicts != 3 {
		t.Errorf("TotalConflicts = %d, want 3", density.TotalConflicts)
	}
	if density.ResolvedConflicts != 1 {
		t.Errorf("ResolvedConflicts = %d, want 1", density.ResolvedConflicts)
	}
	if density.UnresolvedConflicts != 2 {
		t.Errorf("UnresolvedConflicts = %d, want 2", density.UnresolvedConflicts)
	}
	if density.ConflictsPerPair != 1 {
		t.Errorf("ConflictsPerPair = %f, want 1.0", density.ConflictsPerPair)
	}
	if density.Source != "auditor" {
		t.Errorf("Source = %q, want auditor", density.Source)
	}
	if len(density.HighConflictPairs) != 1 || density.HighConflictPairs[0] != "A1 <-> B2" {
		t.Errorf("unexpected HighConflictPairs: %v", density.HighConflictPairs)
	}
}
