package adapters

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/coordinator"
	"github.com/Dicklesworthstone/ntm/internal/handoff"
	"github.com/Dicklesworthstone/ntm/internal/tracker"
)

func TestNormalizeWork(t *testing.T) {
	t.Parallel()

	triage := &bv.TriageResponse{
		Triage: bv.TriageData{
			QuickRef: bv.TriageQuickRef{
				ActionableCount: 3,
			},
			Recommendations: []bv.TriageRecommendation{
				{
					ID:          "bd-1",
					Title:       "Fix adapter",
					Type:        "task",
					Priority:    1,
					Labels:      []string{"robot-redesign", "adapters"},
					Score:       8.5,
					Reasons:     []string{"high impact"},
					UnblocksIDs: []string{"bd-2", "bd-3"},
				},
			},
			QuickWins: []bv.TriageRecommendation{
				{ID: "bd-1"},
			},
			BlockersToClear: []bv.BlockerToClear{
				{ID: "bd-blocker", UnblocksCount: 4},
			},
			ProjectHealth: &bv.ProjectHealth{
				GraphMetrics: &bv.GraphMetrics{
					TotalNodes: 17,
					TotalEdges: 23,
					CycleCount: 1,
					MaxDepth:   5,
					Density:    0.2,
				},
			},
		},
	}

	section := NormalizeWork(WorkInputs{
		Summary: &bv.BeadsSummary{
			Available:  true,
			Total:      10,
			Open:       7,
			InProgress: 2,
			Closed:     3,
			Ready:      3,
			Blocked:    2,
			ReadyPreview: []bv.BeadPreview{
				{ID: "bd-1", Title: "Fix adapter", Priority: "P1"},
			},
			InProgressList: []bv.BeadInProgress{
				{ID: "bd-4", Title: "Existing work", Assignee: "BlueLake"},
			},
		},
		Ready: []bv.BeadPreview{
			{ID: "bd-1", Title: "Fix adapter", Priority: "P1"},
		},
		Blocked: []bv.BeadPreview{
			{ID: "bd-9", Title: "Blocked task", Priority: "P2"},
		},
		InProgress: []bv.BeadInProgress{
			{ID: "bd-4", Title: "Existing work", Assignee: "BlueLake"},
		},
		Triage: triage,
	})

	if !section.Available {
		t.Fatal("expected work section to be available")
	}
	if section.Summary == nil || section.Summary.Ready != 3 {
		t.Fatalf("unexpected summary: %+v", section.Summary)
	}
	if len(section.Ready) != 1 || section.Ready[0].Unblocks != 2 {
		t.Fatalf("unexpected ready items: %+v", section.Ready)
	}
	if len(section.Blocked) != 1 || section.Blocked[0].ID != "bd-9" {
		t.Fatalf("unexpected blocked items: %+v", section.Blocked)
	}
	if len(section.InProgress) != 1 || section.InProgress[0].Assignee != "BlueLake" {
		t.Fatalf("unexpected in-progress items: %+v", section.InProgress)
	}
	if section.Triage == nil || section.Triage.TopRecommendation == nil || section.Triage.TopRecommendation.ID != "bd-1" {
		t.Fatalf("unexpected triage summary: %+v", section.Triage)
	}
	if section.Graph == nil || section.Graph.CycleCount != 1 {
		t.Fatalf("unexpected graph summary: %+v", section.Graph)
	}
}

func TestNormalizeWorkUnavailable(t *testing.T) {
	t.Parallel()

	section := NormalizeWork(WorkInputs{
		Summary: &bv.BeadsSummary{
			Available: false,
			Reason:    "beads unavailable",
		},
	})

	if section.Available {
		t.Fatal("expected unavailable work section")
	}
	if section.Reason != "beads unavailable" {
		t.Fatalf("reason = %q, want beads unavailable", section.Reason)
	}
}

func TestNormalizeCoordination(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 22, 4, 0, 0, 0, time.UTC)
	expiringSoon := agentmail.FlexTime{Time: now.Add(30 * time.Minute)}
	later := agentmail.FlexTime{Time: now.Add(3 * time.Hour)}

	section := NormalizeCoordination(CoordinationInputs{
		InboxByAgent: map[string][]agentmail.InboxMessage{
			"BlueLake": {
				{
					ID:          1,
					Subject:     "Claim update",
					From:        "GreenStone",
					ThreadID:    stringPtr("bd-j9jo3.3.2"),
					CreatedTS:   agentmail.FlexTime{Time: now.Add(-10 * time.Minute)},
					Importance:  "urgent",
					AckRequired: true,
				},
			},
			"RedHill": {
				{
					ID:        2,
					Subject:   "Coordination",
					ThreadID:  stringPtr("bd-j9jo3.3.2"),
					CreatedTS: agentmail.FlexTime{Time: now.Add(-2 * time.Hour)},
				},
			},
		},
		Reservations: []agentmail.FileReservation{
			{
				ID:          1,
				PathPattern: "internal/robot/adapters/*.go",
				AgentName:   "BlueLake",
				Exclusive:   true,
				Reason:      "bd-j9jo3.3.2",
				CreatedTS:   agentmail.FlexTime{Time: now.Add(-20 * time.Minute)},
				ExpiresTS:   expiringSoon,
			},
			{
				ID:          2,
				PathPattern: "internal/robot/adapters/work_coordination.go",
				AgentName:   "GreenStone",
				Exclusive:   true,
				Reason:      "bd-j9jo3.3.5",
				CreatedTS:   agentmail.FlexTime{Time: now.Add(-5 * time.Minute)},
				ExpiresTS:   later,
			},
		},
		ReservationConflicts: []coordinator.Conflict{
			{
				Pattern: "internal/robot/adapters/*.go <-> internal/robot/adapters/work_coordination.go",
				Holders: []coordinator.Holder{
					{AgentName: "BlueLake"},
					{AgentName: "GreenStone"},
				},
			},
		},
		FileConflicts: []tracker.Conflict{
			{
				Path:     "internal/robot/adapters/work_coordination.go",
				Severity: "critical",
				Agents:   []string{"BlueLake", "GreenStone"},
			},
		},
		Handoff: (&handoff.Handoff{
			Session:          "ntm--robot-redesign",
			Status:           "blocked",
			Goal:             "finish adapter",
			Now:              "resolve reservation conflict",
			UpdatedAt:        now,
			Blockers:         []string{"reservation conflict"},
			ActiveBeads:      []string{"bd-j9jo3.3.2"},
			AgentMailThreads: []string{"bd-j9jo3.3.2"},
			Files: handoff.FileChanges{
				Modified: []string{"internal/robot/adapters/work_coordination.go"},
			},
		}),
		Now:                       now,
		ThreadStaleAfter:          time.Hour,
		ReservationExpiringWithin: time.Hour,
		MailBacklogThreshold:      1,
	})

	if !section.Available {
		t.Fatal("expected coordination section to be available")
	}
	if section.Mail == nil || section.Mail.TotalUnread != 2 || section.Mail.UrgentUnread != 1 || section.Mail.PendingAck != 1 {
		t.Fatalf("unexpected mail summary: %+v", section.Mail)
	}
	if section.Threads == nil || section.Threads.Active != 1 || section.Threads.TopThreads[0] != "bd-j9jo3.3.2" {
		t.Fatalf("unexpected threads summary: %+v", section.Threads)
	}
	if section.Reservations == nil || section.Reservations.Active != 2 || section.Reservations.Expiring != 1 || section.Reservations.Conflicts != 1 {
		t.Fatalf("unexpected reservations summary: %+v", section.Reservations)
	}
	if section.Handoff == nil || section.Handoff.Status != "blocked" {
		t.Fatalf("unexpected handoff summary: %+v", section.Handoff)
	}
	if len(section.Problems) < 4 {
		t.Fatalf("expected multiple coordination problems, got %+v", section.Problems)
	}
}

func TestNormalizeCoordinationDerivesReservationConflicts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 22, 4, 0, 0, 0, time.UTC)
	section := NormalizeCoordination(CoordinationInputs{
		Reservations: []agentmail.FileReservation{
			{
				ID:          1,
				PathPattern: "internal/robot/*.go",
				AgentName:   "BlueLake",
				Exclusive:   true,
				CreatedTS:   agentmail.FlexTime{Time: now.Add(-5 * time.Minute)},
				ExpiresTS:   agentmail.FlexTime{Time: now.Add(time.Hour)},
			},
			{
				ID:          2,
				PathPattern: "internal/robot/work_coordination.go",
				AgentName:   "GreenStone",
				Exclusive:   true,
				CreatedTS:   agentmail.FlexTime{Time: now.Add(-3 * time.Minute)},
				ExpiresTS:   agentmail.FlexTime{Time: now.Add(time.Hour)},
			},
		},
		Now: now,
	})

	if section.Reservations == nil {
		t.Fatal("expected reservations summary")
	}
	if section.Reservations.Conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1", section.Reservations.Conflicts)
	}
	found := false
	for _, problem := range section.Problems {
		if problem.Kind == "reservation_conflict" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected derived reservation conflict problem, got %+v", section.Problems)
	}
}

func stringPtr(v string) *string {
	return &v
}
