package robot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	dispatchsvc "github.com/Dicklesworthstone/ntm/internal/dispatch"
	"github.com/Dicklesworthstone/ntm/internal/redaction"
	statuspkg "github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestDecodeBulkAssignTriageValid(t *testing.T) {
	payload := `{"generated_at":"2026-01-19T23:16:00Z","data_hash":"abc","triage":{"meta":{"version":"1","generated_at":"2026-01-19T23:16:00Z","phase2_ready":true,"issue_count":1,"compute_time_ms":12},"quick_ref":{"open_count":1,"actionable_count":1,"blocked_count":0,"in_progress_count":0,"top_picks":[]},"recommendations":[{"id":"bd-1","title":"Test","type":"task","status":"ready","priority":1,"score":0.5,"action":"do","reasons":[]}],"quick_wins":[],"blockers_to_clear":[]}}`

	triage, err := decodeBulkAssignTriage([]byte(payload))
	if err != nil {
		t.Fatalf("decodeBulkAssignTriage failed: %v", err)
	}

	t.Logf("triage parsed: %+v", triage.Triage.Recommendations)
	if len(triage.Triage.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(triage.Triage.Recommendations))
	}
	if triage.Triage.Recommendations[0].ID != "bd-1" {
		t.Errorf("expected bead id bd-1, got %q", triage.Triage.Recommendations[0].ID)
	}
}

func TestDecodeBulkAssignTriageInvalid(t *testing.T) {
	_, err := decodeBulkAssignTriage([]byte(`{"triage":`))
	if err == nil {
		t.Fatal("expected error for invalid triage JSON")
	}
	if !strings.Contains(err.Error(), "unexpected end") {
		t.Logf("invalid JSON error: %v", err)
	}
}

func TestGetBulkAssignRejectsInvalidStrategyBeforeExternalWork(t *testing.T) {
	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{Session: "proj", FromBV: true, Strategy: "fastest"})
	if err != nil {
		t.Fatalf("GetBulkAssign transport error: %v", err)
	}
	if output.Success || output.ErrorCode != ErrCodeInvalidFlag || output.Assignments == nil {
		t.Fatalf("invalid strategy output = %+v", output)
	}
}

func TestGetBulkAssignPreservesDependencyLocalDeadline(t *testing.T) {
	deps := BulkAssignDependencies{
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%1", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex}}, nil
		},
		ResolveProject: func(context.Context, string, []tmux.Pane) (string, error) { return t.TempDir(), nil },
		FetchTriage:    func(context.Context, string) (*bv.TriageResponse, error) { return nil, context.DeadlineExceeded },
	}
	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{Session: "proj", FromBV: true, Strategy: "impact", Deps: &deps})
	if err != nil {
		t.Fatalf("GetBulkAssign transport error: %v", err)
	}
	if output.Success || output.ErrorCode != ErrCodeTimeout {
		t.Fatalf("dependency deadline output = %+v, want TIMEOUT", output)
	}
}

func TestBulkAssignImpactStrategySorting(t *testing.T) {
	triage := mockTriage(nil, []bv.BlockerToClear{
		{ID: "bd-1", Title: "A", UnblocksCount: 2},
		{ID: "bd-2", Title: "B", UnblocksCount: 5},
		{ID: "bd-3", Title: "C", UnblocksCount: 3},
	})
	panes := mockPanes("proj", []int{1, 2, 3})
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "impact"}, BulkAssignDependencies{}, panes, triage, nil)

	got := []string{}
	for _, a := range plan.Assignments {
		got = append(got, a.Bead)
	}
	expected := []string{"bd-2", "bd-3", "bd-1"}

	t.Logf("strategy=impact triage blockers=%v", triage.Triage.BlockersToClear)
	t.Logf("expected order=%v actual=%v", expected, got)

	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("impact strategy order mismatch: got %v, want %v", got, expected)
	}
}

func TestBulkAssignReadyStrategyFilters(t *testing.T) {
	recs := []bv.TriageRecommendation{
		{ID: "bd-1", Title: "Open low", Status: "open", Priority: 2},
		{ID: "bd-2", Title: "Blocked", Status: "blocked", Priority: 0},
		{ID: "bd-3", Title: "Ready high", Status: "ready", Priority: 1},
	}
	triage := mockTriage(recs, nil)
	panes := mockPanes("proj", []int{1, 2, 3})
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "ready"}, BulkAssignDependencies{}, panes, triage, nil)

	got := []string{}
	for _, a := range plan.Assignments {
		got = append(got, a.Bead)
	}
	expected := []string{"bd-3", "bd-1"}

	t.Logf("strategy=ready triage recs=%v", recs)
	t.Logf("expected=%v actual=%v", expected, got)

	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("ready strategy order mismatch: got %v, want %v", got, expected)
	}
}

func TestBulkAssignStaleStrategy(t *testing.T) {
	now := time.Date(2026, 1, 20, 1, 0, 0, 0, time.UTC)
	inProgress := []bv.BeadInProgress{
		{ID: "bd-1", Title: "Recent", UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "bd-2", Title: "Stale", UpdatedAt: now.Add(-48 * time.Hour)},
		{ID: "bd-3", Title: "Oldest", UpdatedAt: now.Add(-72 * time.Hour)},
	}
	panes := mockPanes("proj", []int{1, 2, 3})
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "stale"}, BulkAssignDependencies{}, panes, nil, inProgress)

	got := []string{}
	for _, a := range plan.Assignments {
		got = append(got, a.Bead)
	}
	expected := []string{"bd-3", "bd-2", "bd-1"}

	t.Logf("strategy=stale in_progress=%v", inProgress)
	t.Logf("expected=%v actual=%v", expected, got)

	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("stale strategy order mismatch: got %v, want %v", got, expected)
	}
}

func TestBulkAssignBalancedStrategyMix(t *testing.T) {
	triage := mockTriage(
		[]bv.TriageRecommendation{
			{ID: "bd-r1", Title: "Ready1", Status: "ready", Priority: 1},
			{ID: "bd-r2", Title: "Ready2", Status: "ready", Priority: 2},
		},
		[]bv.BlockerToClear{
			{ID: "bd-i1", Title: "Impact1", UnblocksCount: 5},
			{ID: "bd-i2", Title: "Impact2", UnblocksCount: 3},
		},
	)
	inProgress := []bv.BeadInProgress{
		{ID: "bd-s1", Title: "Stale1", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "bd-s2", Title: "Stale2", UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	panes := mockPanes("proj", []int{1, 2, 3, 4, 5, 6})
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "balanced"}, BulkAssignDependencies{}, panes, triage, inProgress)

	got := []string{}
	for _, a := range plan.Assignments {
		got = append(got, a.Bead)
	}
	// Expected interleaving: impact1, ready1, stale1, impact2, ready2, stale2
	expected := []string{"bd-i1", "bd-r1", "bd-s1", "bd-i2", "bd-r2", "bd-s2"}

	t.Logf("strategy=balanced expected=%v actual=%v", expected, got)
	if !reflect.DeepEqual(expected, got) {
		t.Fatalf("balanced strategy order mismatch: got %v, want %v", got, expected)
	}
}

func TestBulkAssignMoreBeadsThanPanes(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	beads := []bulkBead{{ID: "bd-1"}, {ID: "bd-2"}, {ID: "bd-3"}}
	plan := allocateBulkAssignBeads(panes, beads)

	t.Logf("beads=%v panes=%v", beads, panes)
	if len(plan.UnassignedBeads) != 1 {
		t.Fatalf("expected 1 unassigned bead, got %d", len(plan.UnassignedBeads))
	}
	if plan.UnassignedBeads[0] != "bd-3" {
		t.Errorf("expected unassigned bead bd-3, got %v", plan.UnassignedBeads)
	}
}

func TestBulkAssignMorePanesThanBeads(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2, 3})
	beads := []bulkBead{{ID: "bd-1"}}
	plan := allocateBulkAssignBeads(panes, beads)

	t.Logf("beads=%v panes=%v", beads, panes)
	if len(plan.UnassignedPanes) != 2 {
		t.Fatalf("expected 2 unassigned panes, got %d", len(plan.UnassignedPanes))
	}
}

func TestBulkAssignExactCounts(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	beads := []bulkBead{{ID: "bd-1"}, {ID: "bd-2"}}
	plan := allocateBulkAssignBeads(panes, beads)

	t.Logf("beads=%v panes=%v", beads, panes)
	if len(plan.UnassignedBeads) != 0 || len(plan.UnassignedPanes) != 0 {
		t.Fatalf("expected no unassigned items, got beads=%v panes=%v", plan.UnassignedBeads, plan.UnassignedPanes)
	}
}

func TestBulkAssignTemplateSubstitution(t *testing.T) {
	template := "{bead_id}:{bead_title}:{bead_type}:{bead_deps}:{session}:{pane}"
	result := expandBulkAssignTemplate(template, "bd-1", "Title", "task", []string{"bd-2", "bd-3"}, "proj", "0.2")
	expected := "bd-1:Title:task:bd-2, bd-3:proj:0.2"

	t.Logf("template=%q result=%q", template, result)
	if result != expected {
		t.Fatalf("template substitution mismatch: got %q want %q", result, expected)
	}
}

func TestBulkAssignTemplateSubstitutionDefaults(t *testing.T) {
	template := "{bead_id}:{bead_type}:{bead_deps}"
	result := expandBulkAssignTemplate(template, "bd-1", "Title", "", nil, "proj", "0.2")
	expected := "bd-1:unknown:none"

	t.Logf("template=%q result=%q", template, result)
	if result != expected {
		t.Fatalf("default substitution mismatch: got %q want %q", result, expected)
	}
}

func TestBulkAssignTemplateLoadingFromFile(t *testing.T) {
	opts := BulkAssignOptions{PromptTemplatePath: "testdata/bulk_assign_template.txt"}
	deps := bulkAssignDeps(nil)
	deps.ReadFile = func(path string) ([]byte, error) {
		return os.ReadFile(path)
	}
	template, err := loadBulkAssignTemplate(opts, deps)
	if err != nil {
		t.Fatalf("loadBulkAssignTemplate failed: %v", err)
	}

	t.Logf("loaded template=%q", template)
	if !strings.Contains(template, "{bead_id}") {
		t.Fatalf("expected template to contain {bead_id}, got %q", template)
	}
}

// TestLoadBulkAssignTemplatePrecedence verifies the resolution order for the
// dispatch prompt template (#153): explicit --bulk-assign-template path beats a
// configured default file, which beats a configured inline default, which beats
// the built-in const.
func TestLoadBulkAssignTemplatePrecedence(t *testing.T) {
	readers := func(byPath map[string]string) func(string) ([]byte, error) {
		return func(path string) ([]byte, error) {
			if content, ok := byPath[path]; ok {
				return []byte(content), nil
			}
			return nil, fmt.Errorf("unexpected ReadFile(%q)", path)
		}
	}

	cases := []struct {
		name string
		opts BulkAssignOptions
		read func(string) ([]byte, error)
		want string
	}{
		{
			name: "explicit path wins over configured defaults",
			opts: BulkAssignOptions{
				PromptTemplatePath:  "explicit.txt",
				DefaultTemplatePath: "configured.txt",
				DefaultTemplate:     "inline default",
			},
			read: readers(map[string]string{"explicit.txt": "explicit template", "configured.txt": "configured template"}),
			want: "explicit template",
		},
		{
			name: "configured file wins over inline default",
			opts: BulkAssignOptions{
				DefaultTemplatePath: "configured.txt",
				DefaultTemplate:     "inline default",
			},
			read: readers(map[string]string{"configured.txt": "configured template"}),
			want: "configured template",
		},
		{
			name: "blank configured file falls through to inline default",
			opts: BulkAssignOptions{
				DefaultTemplatePath: "configured.txt",
				DefaultTemplate:     "inline default",
			},
			read: readers(map[string]string{"configured.txt": "   \n"}),
			want: "inline default",
		},
		{
			name: "inline default used when no files configured",
			opts: BulkAssignOptions{DefaultTemplate: "inline default"},
			want: "inline default",
		},
		{
			name: "built-in const when nothing configured",
			opts: BulkAssignOptions{},
			want: defaultBulkAssignTemplate,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := bulkAssignDeps(nil)
			if tc.read != nil {
				deps.ReadFile = tc.read
			}
			got, err := loadBulkAssignTemplate(tc.opts, deps)
			if err != nil {
				t.Fatalf("loadBulkAssignTemplate failed: %v", err)
			}
			if got != tc.want {
				t.Fatalf("template mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestLoadBulkAssignTemplateConfiguredFileError surfaces a read error for a
// configured default file rather than silently falling back, so a misconfigured
// path is visible to the operator.
func TestLoadBulkAssignTemplateConfiguredFileError(t *testing.T) {
	deps := bulkAssignDeps(nil)
	deps.ReadFile = func(path string) ([]byte, error) {
		return nil, fmt.Errorf("boom")
	}
	_, err := loadBulkAssignTemplate(BulkAssignOptions{DefaultTemplatePath: "missing.txt"}, deps)
	if err == nil {
		t.Fatal("expected error for unreadable configured template file, got nil")
	}
	if !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("expected error to mention the path, got %v", err)
	}
}

func bulkTestDeliverer(t *testing.T, deliver func(dispatchsvc.Delivery) error) dispatchsvc.Deliverer {
	t.Helper()
	return dispatchsvc.DelivererFunc(func(ctx context.Context, delivery dispatchsvc.Delivery) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if delivery.Protocol != dispatchsvc.ProtocolDoubleEnter {
			t.Fatalf("bulk delivery protocol = %q, want %q", delivery.Protocol, dispatchsvc.ProtocolDoubleEnter)
		}
		if delivery.Target.Ref.ID == "" {
			t.Fatal("bulk delivery omitted canonical physical pane ID")
		}
		return deliver(delivery)
	})
}

func TestBulkAssignSequentialDeliveryOrdering(t *testing.T) {
	allocation := `{"2":"bd-2","1":"bd-1"}`
	panes := mockPanes("proj", []int{1, 2})
	callOrder := []string{}
	deps := BulkAssignDependencies{
		FetchBeadTitle: func(_ context.Context, _ string, beadID string) (string, error) { return "Title " + beadID, nil },
		Cwd:            func() (string, error) { return "/tmp", nil },
		ReadFile:       func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}

	plan := planBulkAssignFromAllocation(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), panes, mustParseAllocation(t, allocation))
	output := BulkAssignOutput{Session: "proj"}
	deps.DispatchDeliverer = bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
		callOrder = append(callOrder, delivery.Target.Ref.ID)
		return nil
	})
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)

	expectedOrder := []string{"%1", "%2"}
	if !reflect.DeepEqual(callOrder, expectedOrder) {
		t.Fatalf("send order mismatch: got %v want %v", callOrder, expectedOrder)
	}

	t.Logf("expected order=%v actual order=%v", expectedOrder, callOrder)
}

func TestPlanBulkAssignFromAllocationDeduplicatesPhysicalAliases(t *testing.T) {
	panes := []bulkPane{
		{Ref: tmux.PaneRef{ID: "%10", WindowIndex: 0, PaneIndex: 0}, AgentType: "codex", Title: "proj__cod_1"},
		{Ref: tmux.PaneRef{ID: "%11", WindowIndex: 1, PaneIndex: 0}, AgentType: "claude", Title: "proj__cc_2"},
	}
	deps := bulkAssignDeps(&BulkAssignDependencies{
		FetchBeadTitle: func(_ context.Context, _ string, beadID string) (string, error) { return "Title " + beadID, nil },
	})

	plan := planBulkAssignFromAllocation(t.Context(), BulkAssignOptions{}, deps, panes, map[string]string{
		"1.0": "bd-same",
		"%11": "bd-same",
	})

	if len(plan.Assignments) != 1 {
		t.Fatalf("physical aliases produced %d assignments, want 1: %+v", len(plan.Assignments), plan.Assignments)
	}
	assignment := plan.Assignments[0]
	if assignment.Pane != "1.0" || assignment.PaneID != "%11" || assignment.Bead != "bd-same" || assignment.Status != "planned" {
		t.Fatalf("deduplicated assignment = %+v", assignment)
	}
	if !reflect.DeepEqual(plan.UnassignedPanes, []string{"0.0"}) {
		t.Fatalf("unassigned panes = %v, want [0.0]", plan.UnassignedPanes)
	}
}

func TestPlanBulkAssignFromAllocationRejectsConflictingPhysicalAliases(t *testing.T) {
	panes := []bulkPane{
		{Ref: tmux.PaneRef{ID: "%10", WindowIndex: 0, PaneIndex: 0}, AgentType: "codex", Title: "proj__cod_1"},
		{Ref: tmux.PaneRef{ID: "%11", WindowIndex: 1, PaneIndex: 0}, AgentType: "claude", Title: "proj__cc_2"},
	}
	deps := bulkAssignDeps(&BulkAssignDependencies{
		FetchBeadTitle: func(_ context.Context, _ string, beadID string) (string, error) { return "Title " + beadID, nil },
	})

	plan := planBulkAssignFromAllocation(t.Context(), BulkAssignOptions{}, deps, panes, map[string]string{
		"1.0": "bd-one",
		"%11": "bd-two",
	})

	if len(plan.Assignments) != 2 {
		t.Fatalf("conflicting aliases produced %d results, want 2 failures: %+v", len(plan.Assignments), plan.Assignments)
	}
	for _, assignment := range plan.Assignments {
		if assignment.Status != "failed" || !strings.Contains(assignment.Error, "same physical pane") {
			t.Fatalf("conflicting alias was not rejected before execution: %+v", assignment)
		}
	}
}

func TestBulkAssignStaggerOnlyPacesBetweenAtomicDispatchAttempts(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "One"}, {ID: "bd-2", Title: "Two"}})
	var events []string
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
			events = append(events, delivery.Target.Ref.ID)
			return nil
		}),
		Wait: func(_ context.Context, delay time.Duration) error {
			events = append(events, "wait:"+delay.String())
			return nil
		},
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	output := BulkAssignOutput{Session: "proj"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{Stagger: 25 * time.Millisecond}, bulkAssignDeps(&deps), &output, plan)
	want := []string{"%1", "wait:25ms", "%2"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v, want %v", events, want)
	}
}

func TestBulkAssignSequentialCancellationStopsLaterClaimAndDispatch(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "One"}, {ID: "bd-2", Title: "Two"}})
	ctx, cancel := context.WithCancel(t.Context())
	var sends atomic.Int32
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(dispatchsvc.Delivery) error {
			sends.Add(1)
			cancel()
			return nil
		}),
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	originalClaim := deps.ClaimBead
	var claims atomic.Int32
	deps.ClaimBead = func(ctx context.Context, dir, beadID, actor string) (bv.BeadClaimResult, error) {
		claims.Add(1)
		return originalClaim(ctx, dir, beadID, actor)
	}
	output := BulkAssignOutput{Session: "proj"}
	applyBulkAssignPlan(ctx, BulkAssignOptions{Stagger: time.Hour}, bulkAssignDeps(&deps), &output, plan)

	if got := sends.Load(); got != 1 {
		t.Fatalf("dispatches after cancellation = %d, want exactly first dispatch", got)
	}
	if got := claims.Load(); got != 1 {
		t.Fatalf("claims after cancellation = %d, want exactly first claim", got)
	}
	if len(output.Assignments) != 2 || !strings.Contains(output.Assignments[1].Error, "canceled") || output.Assignments[1].PromptSent {
		t.Fatalf("second assignment after cancellation = %+v", output.Assignments)
	}
}

func TestBulkAssignCancellationPropagatesToTriageDependency(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	inProgressCalls := atomic.Int32{}
	projectDir := t.TempDir()
	deps := BulkAssignDependencies{
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return mockTmuxPanesForList([]int{1}), nil
		},
		ResolveProject: func(context.Context, string, []tmux.Pane) (string, error) {
			return projectDir, nil
		},
		FetchTriage: func(callCtx context.Context, _ string) (*bv.TriageResponse, error) {
			close(started)
			<-callCtx.Done()
			return nil, callCtx.Err()
		},
		FetchInProgress: func(context.Context, string, int) ([]bv.BeadInProgress, error) {
			inProgressCalls.Add(1)
			return nil, nil
		},
	}
	type result struct {
		output *BulkAssignOutput
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, err := GetBulkAssign(ctx, BulkAssignOptions{Session: "bulk-cancel-triage", FromBV: true, Deps: &deps})
		done <- result{output: output, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("bulk triage dependency did not start")
	}
	cancel()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("GetBulkAssign returned transport error: %v", got.err)
		}
		if got.output == nil || got.output.Success || got.output.ErrorCode != ErrCodeTimeout {
			t.Fatalf("canceled triage output = %+v, want structured TIMEOUT failure", got.output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bulk triage dependency ignored caller cancellation")
	}
	if got := inProgressCalls.Load(); got != 0 {
		t.Fatalf("in-progress read after triage cancellation = %d", got)
	}
}

func TestBulkAssignCancellationPropagatesToLiveProjectLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	deps := BulkAssignDependencies{
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return mockTmuxPanesForList([]int{1}), nil
		},
		PaneCurrentPath: func(callCtx context.Context, _ string) (string, error) {
			close(started)
			<-callCtx.Done()
			return "", callCtx.Err()
		},
	}
	type result struct {
		output *BulkAssignOutput
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, err := GetBulkAssign(ctx, BulkAssignOptions{Session: "bulk-cancel-project", FromBV: true, Deps: &deps})
		done <- result{output: output, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("bulk live-project lookup did not start")
	}
	cancel()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("GetBulkAssign returned transport error: %v", got.err)
		}
		if got.output == nil || got.output.Success || got.output.ErrorCode != ErrCodeTimeout {
			t.Fatalf("canceled live-project output = %+v, want structured TIMEOUT failure", got.output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bulk live-project lookup ignored caller cancellation")
	}
}

func TestBulkAssignParallelCancellationJoinsAllWorkers(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "One"}, {ID: "bd-2", Title: "Two"}})
	ctx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{}, 2)
	var active atomic.Int32
	var calls atomic.Int32
	deps := BulkAssignDependencies{
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(ctx context.Context, _ dispatchsvc.Delivery) error {
			calls.Add(1)
			active.Add(1)
			defer active.Add(-1)
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}),
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	output := BulkAssignOutput{Session: "proj"}
	done := make(chan struct{})
	go func() {
		applyBulkAssignPlan(ctx, BulkAssignOptions{Parallel: true}, bulkAssignDeps(&deps), &output, plan)
		close(done)
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("parallel bulk worker did not reach dispatch")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parallel bulk assignment did not join canceled workers")
	}
	if got := active.Load(); got != 0 {
		t.Fatalf("active workers after return = %d", got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("dispatch calls after joined return = %d, want 2", got)
	}
}

func TestBulkAssignUsesPaneTargetWhenAvailable(t *testing.T) {
	panes, err := filterBulkAssignPanes([]tmux.Pane{
		{ID: "%11", Index: 1, Title: "proj__cc_1"},
	}, nil)
	if err != nil {
		t.Fatalf("filter panes: %v", err)
	}
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "Title1"}})

	var gotTarget string
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
			gotTarget = delivery.Target.Ref.ID
			return nil
		}),
		ReadFile: func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	output := BulkAssignOutput{Session: "proj"}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)

	if gotTarget != "%11" {
		t.Fatalf("expected send target %%11, got %q", gotTarget)
	}
}

func TestBulkAssignUsesAgentAwareCanonicalDelivery(t *testing.T) {
	panes := []bulkPane{
		{Ref: tmux.PaneRef{ID: "%21", WindowIndex: 0, PaneIndex: 1}, AgentType: "codex"},
	}
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "Title1"}})

	var (
		gotTarget    string
		gotAgentType tmux.AgentType
	)
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
			gotTarget = delivery.Target.Ref.ID
			gotAgentType = delivery.Target.AgentType
			return nil
		}),
		ReadFile: func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	output := BulkAssignOutput{Session: "proj"}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)

	if gotTarget != "%21" {
		t.Fatalf("expected send target %%21, got %q", gotTarget)
	}
	if gotAgentType != tmux.AgentCodex {
		t.Fatalf("expected codex agent type, got %q", gotAgentType)
	}
}

func TestBulkAssignCanonicalDelivererSubmitsPrompt(t *testing.T) {
	panes := []bulkPane{
		{Ref: tmux.PaneRef{ID: "%31", WindowIndex: 0, PaneIndex: 1}, AgentType: "codex"},
	}
	plan := allocateBulkAssignBeads(panes, []bulkBead{{ID: "bd-1", Title: "Title1"}})

	var (
		gotTarget string
		gotPrompt string
		gotCalls  int
	)
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
			gotTarget = delivery.Target.Ref.ID
			gotPrompt = delivery.Message
			gotCalls++
			return nil
		}),
		ReadFile: func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	output := BulkAssignOutput{Session: "proj"}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)

	if gotCalls != 1 {
		t.Fatalf("expected canonical deliverer to be called once, got %d", gotCalls)
	}
	if gotTarget != "%31" {
		t.Fatalf("expected send target %%31, got %q", gotTarget)
	}
	if !strings.Contains(gotPrompt, "bd-1") || !strings.Contains(gotPrompt, "Title1") {
		t.Fatalf("canonical delivery prompt = %q, want bead identity and title", gotPrompt)
	}
	if output.Assignments[0].Status != "assigned" {
		t.Fatalf("expected assigned status, got %q", output.Assignments[0].Status)
	}
}

func TestBulkAssignFailedDelivery(t *testing.T) {
	panes := mockPanes("proj", []int{1, 2})
	beads := []bulkBead{{ID: "bd-1", Title: "Title1"}, {ID: "bd-2", Title: "Title2"}}
	plan := allocateBulkAssignBeads(panes, beads)

	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(delivery dispatchsvc.Delivery) error {
			if delivery.Target.Ref.ID == "%2" {
				return errors.New("send failed")
			}
			return nil
		}),
		ReadFile: func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}
	output := BulkAssignOutput{Session: "proj"}
	deps = bulkAtomicTestDeps(t, "proj", plan, deps)
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)

	if output.Summary.Failed != 1 {
		t.Fatalf("expected 1 failed assignment, got %d", output.Summary.Failed)
	}
	if output.Assignments[1].Status != "failed" {
		t.Fatalf("expected failed status, got %q", output.Assignments[1].Status)
	}

	t.Logf("output=%+v", output)
}

func TestBulkAssignDryRunSkipsPromptSend(t *testing.T) {
	panes := mockPanes("proj", []int{1})
	beads := []bulkBead{{ID: "bd-1", Title: "Title1"}}
	plan := allocateBulkAssignBeads(panes, beads)

	sent := false
	deps := BulkAssignDependencies{
		DispatchDeliverer: bulkTestDeliverer(t, func(dispatchsvc.Delivery) error {
			sent = true
			return nil
		}),
		ReadFile: func(path string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
	}

	output := BulkAssignOutput{Session: "proj"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{DryRun: true}, bulkAssignDeps(&deps), &output, plan)

	if sent {
		t.Fatal("expected no send calls in dry run")
	}
	if output.Assignments[0].PromptSent {
		t.Fatal("expected prompt_sent false in dry run")
	}

	t.Logf("dry-run output=%+v", output)
}

func TestBulkAssignAtomicOrderAndDurableReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := assignment.NewStore("bulk-atomic")
	plan := allocateBulkAssignBeads(
		[]bulkPane{{Ref: tmux.PaneRef{ID: "%21", WindowIndex: 0, PaneIndex: 1}, AgentType: "codex"}},
		[]bulkBead{{ID: "bd-atomic", Title: "Atomic bulk"}},
	)
	var mu sync.Mutex
	var order []string
	claimCalls := 0
	reserveCalls := 0
	deliverCalls := 0
	keyCalls := 0
	observeCalls := 0
	deps := BulkAssignDependencies{
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
		Cwd:      func() (string, error) { return t.TempDir(), nil },
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%21", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex}}, nil
		},
		LoadStore: func(string) (*assignment.AssignmentStore, error) { return store, nil },
		ClaimBead: func(_ context.Context, _ string, beadID, actor string) (bv.BeadClaimResult, error) {
			mu.Lock()
			defer mu.Unlock()
			claimCalls++
			order = append(order, "claim")
			return bv.BeadClaimResult{ID: beadID, Actor: actor, Status: "in_progress", ClaimedAt: time.Now().UTC()}, nil
		},
		NewIdempotencyKey: func() (string, error) {
			keyCalls++
			return "bulk-atomic-key", nil
		},
		ReservationPort: assignment.ReservationFunc(func(_ context.Context, req assignment.ReservationRequest) (assignment.LeaseReceipt, error) {
			mu.Lock()
			defer mu.Unlock()
			reserveCalls++
			order = append(order, "reserve")
			expires := time.Now().UTC().Add(time.Hour)
			return assignment.LeaseReceipt{AgentName: req.AgentName, Target: req.Target, Requested: append([]string(nil), req.RequestedPaths...), Granted: append([]string(nil), req.RequestedPaths...), ReservationIDs: []int{42}, ExpiresAt: &expires}, nil
		}),
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(_ context.Context, delivery dispatchsvc.Delivery) error {
			mu.Lock()
			defer mu.Unlock()
			deliverCalls++
			order = append(order, "dispatch")
			if delivery.Target.Ref.ID != "%21" {
				t.Errorf("delivery target=%q, want %%21", delivery.Target.Ref.ID)
			}
			return nil
		}),
		LoadRedaction: func(string) (redaction.Config, error) {
			return redaction.Config{Mode: redaction.ModeOff}, nil
		},
		ObserveSession: func(ctx context.Context, session string) (statuspkg.SessionObservation, error) {
			observeCalls++
			if observeCalls > 2 {
				t.Fatal("durable replay re-ran the fresh-idle observation gate")
			}
			return bulkSafeObserver([]tmux.Pane{{ID: "%21", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex}})(ctx, session)
		},
		ResolveAgentName: func(context.Context, string, string, string, string) (string, error) {
			return "AtomicAgent", nil
		},
	}

	first := BulkAssignOutput{Session: "bulk-atomic"}
	reservationPaths := []string{"internal/robot/**"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{RequireReservation: true, ReservationPaths: reservationPaths}, bulkAssignDeps(&deps), &first, plan)
	second := BulkAssignOutput{Session: "bulk-atomic"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{RequireReservation: true, ReservationPaths: reservationPaths}, bulkAssignDeps(&deps), &second, plan)
	if len(first.Assignments) != 1 || !first.Assignments[0].Claimed || !first.Assignments[0].PromptSent || first.Assignments[0].DispatchReceiptID == "" {
		t.Fatalf("first assignments=%+v", first.Assignments)
	}
	if len(second.Assignments) != 1 || !second.Assignments[0].PromptSent || second.Assignments[0].IdempotencyKey != first.Assignments[0].IdempotencyKey {
		t.Fatalf("replayed assignments=%+v", second.Assignments)
	}
	if !reflect.DeepEqual(order, []string{"claim", "reserve", "dispatch"}) {
		t.Fatalf("side-effect order=%v", order)
	}
	if claimCalls != 1 || reserveCalls != 1 || deliverCalls != 1 || keyCalls != 1 || observeCalls != 2 {
		t.Fatalf("calls claim=%d reserve=%d dispatch=%d key=%d observe=%d", claimCalls, reserveCalls, deliverCalls, keyCalls, observeCalls)
	}
}

func TestRobotAtomicPaneDispatchRevalidatesFreshIdleBeforeActuation(t *testing.T) {
	pane := tmux.Pane{ID: "%31", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex}
	tests := []struct {
		name    string
		observe func(context.Context, string) (statuspkg.SessionObservation, error)
	}{
		{
			name: "observation error",
			observe: func(context.Context, string) (statuspkg.SessionObservation, error) {
				return statuspkg.SessionObservation{}, errors.New("capture failed")
			},
		},
		{
			name: "stale observation",
			observe: func(context.Context, string) (statuspkg.SessionObservation, error) {
				observation := bulkSafeObservation("dispatch-guard", []tmux.Pane{pane})
				observation.ObservedAt = time.Now().Add(-statuspkg.DispatchObservationMaxAge - time.Second)
				return observation, nil
			},
		},
		{
			name: "pane became busy",
			observe: func(context.Context, string) (statuspkg.SessionObservation, error) {
				observedAt := time.Now().UTC()
				return statuspkg.SessionObservation{
					Session: "dispatch-guard", ObservedAt: observedAt, Complete: true,
					Panes: []statuspkg.PaneObservation{{
						Pane: pane.Ref(), Metadata: pane,
						Current: statuspkg.StateObservation{
							Status:     statuspkg.AgentStatus{PaneID: pane.ID, State: statuspkg.StateWorking, UpdatedAt: observedAt},
							ObservedAt: observedAt, Freshness: statuspkg.FreshnessFresh, Confidence: 1,
						},
					}},
				}, nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deliveries := 0
			port := newRobotAtomicPaneDispatchPort(
				"dispatch-guard",
				func(context.Context, string) ([]tmux.Pane, error) { return []tmux.Pane{pane}, nil },
				test.observe,
				redaction.Config{Mode: redaction.ModeOff},
				dispatchsvc.DelivererFunc(func(context.Context, dispatchsvc.Delivery) error {
					deliveries++
					return nil
				}),
				nil,
			)
			_, err := port.Dispatch(t.Context(), assignment.DispatchRequest{
				Target: pane.ID, Prompt: "work", IdempotencyKey: "dispatch-guard-key",
			})
			if err == nil || !assignment.IsGuaranteedNoActuation(err) {
				t.Fatalf("Dispatch error=%v, want guaranteed no-actuation failure", err)
			}
			if deliveries != 0 {
				t.Fatalf("deliveries=%d, want zero", deliveries)
			}
		})
	}
}

func TestRobotAtomicPreflightBlocksSensitiveReservationPathBeforeTopologyLookup(t *testing.T) {
	port := newRobotAtomicPaneDispatchPort(
		"sensitive-path", nil, nil, redaction.DefaultConfig(), nil, nil,
	)
	_, err := port.Preflight(t.Context(), assignment.DispatchRequest{
		BeadID: "ntm-sensitive-path", BeadTitle: "Safe title", Target: "%31", Prompt: "safe prompt",
		RequestedPaths: []string{"internal/" + "sk-proj-FAKEtestkey1234567890123456789012345678901234" + ".txt"},
	})
	var dispatchErr *dispatchsvc.Error
	if !errors.As(err, &dispatchErr) || dispatchErr.Code != dispatchsvc.ErrRedactionBlocked || !strings.Contains(err.Error(), "reservation path") {
		t.Fatalf("sensitive path preflight error=%v", err)
	}
}

func TestBulkAssignDryRunSanitizesTitlesAndBlocksSensitiveReservationPaths(t *testing.T) {
	const secret = "sk-proj-FAKEtestkey1234567890123456789012345678901234"
	deps := bulkAssignDeps(&BulkAssignDependencies{
		LoadRedaction: func(string) (redaction.Config, error) { return redaction.DefaultConfig(), nil },
	})

	t.Run("title", func(t *testing.T) {
		output := &BulkAssignOutput{RobotResponse: NewRobotResponse(true), Session: "dry-title", DryRun: true}
		plan := bulkAssignPlan{Assignments: []BulkAssignAssignment{{
			Pane: "1", PaneID: "%31", Bead: "bd-title", BeadTitle: "Fix " + secret, AgentType: "cod", Status: "planned",
		}}}
		applyBulkAssignPlan(t.Context(), BulkAssignOptions{DryRun: true, projectDir: "/project"}, deps, output, plan)
		if len(output.Assignments) != 1 || strings.Contains(output.Assignments[0].BeadTitle, secret) || !strings.Contains(output.Assignments[0].BeadTitle, "[REDACTED:") {
			t.Fatalf("dry-run title output=%+v", output.Assignments)
		}
	})

	t.Run("reservation path", func(t *testing.T) {
		output := &BulkAssignOutput{RobotResponse: NewRobotResponse(true), Session: "dry-path", DryRun: true}
		plan := bulkAssignPlan{Assignments: []BulkAssignAssignment{{
			Pane: "1", PaneID: "%31", Bead: "bd-path", BeadTitle: "Safe title", AgentType: "cod", Status: "planned",
		}}}
		applyBulkAssignPlan(t.Context(), BulkAssignOptions{
			DryRun: true, RequireReservation: true, ReservationPaths: []string{"internal/" + secret + ".txt"}, projectDir: "/project",
		}, deps, output, plan)
		if output.Success || len(output.Assignments) != 1 || output.Assignments[0].Status != "failed" ||
			!strings.Contains(output.Assignments[0].Error, "reservation path") || strings.Contains(fmt.Sprint(output), secret) {
			t.Fatalf("sensitive path dry-run output=%+v", output)
		}
	})
}

func TestRobotAtomicReplayMatchesRawIntentAgainstRedactedDurablePrompt(t *testing.T) {
	store := assignment.NewStore("redacted-replay")
	rawPrompt := "token=raw-secret"
	store.Assignments["bd-redacted"] = &assignment.Assignment{
		BeadID: "bd-redacted", Status: assignment.StatusAssigned,
		Pane: 1, AgentType: "codex", AgentName: "ExactAgent",
		IdempotencyKey: "existing-key", DispatchTarget: "%44", OccupancyKey: "%44",
		DispatchState: assignment.DispatchSent, IntentSHA256: assignment.PromptSHA256(rawPrompt),
		PendingPrompt: "token=[REDACTED]", PromptSent: "token=[REDACTED]",
	}
	keyCalls := 0
	key, err := robotAtomicIdempotencyKey(
		store, "bd-redacted", "%44", 9, "codex", "ExactAgent", rawPrompt, false, nil,
		func() (string, error) { keyCalls++; return "new-key", nil },
	)
	if err != nil || key != "existing-key" || keyCalls != 0 {
		t.Fatalf("key=%q calls=%d err=%v", key, keyCalls, err)
	}
	if replay := robotAtomicReplayIntent(store, "bd-redacted", "%44", 9, "codex", rawPrompt, false, nil); replay == nil || replay.IdempotencyKey != key || replay.Pane != 1 {
		t.Fatalf("raw intent did not match redacted durable replay: %+v", replay)
	}
}

func TestBulkAtomicRuntimeSharesFreshObservationAcrossConcurrentAssignments(t *testing.T) {
	var calls atomic.Int32
	panes := []tmux.Pane{
		{ID: "%1", WindowIndex: 0, Index: 0, Type: tmux.AgentCodex},
		{ID: "%2", WindowIndex: 0, Index: 1, Type: tmux.AgentClaude},
	}
	runtime := &bulkAtomicRuntime{deps: BulkAssignDependencies{
		ObserveSession: func(context.Context, string) (statuspkg.SessionObservation, error) {
			calls.Add(1)
			time.Sleep(25 * time.Millisecond)
			return bulkSafeObservation("bulk-observation", panes), nil
		},
	}}

	start := make(chan struct{})
	results := make(chan statuspkg.SessionObservation, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			observation, err := runtime.observeSession(t.Context(), "bulk-observation")
			if err != nil {
				t.Errorf("observeSession: %v", err)
				return
			}
			results <- observation
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	if calls.Load() != 1 {
		t.Fatalf("whole-session observation calls = %d, want 1", calls.Load())
	}
	for observation := range results {
		if !observation.SafeToDispatch("%1") || !observation.SafeToDispatch("%2") {
			t.Fatalf("shared observation lost safe panes: %+v", observation)
		}
	}

	runtime.observeMu.Lock()
	runtime.observation.ObservedAt = time.Now().Add(-statuspkg.DispatchObservationMaxAge - time.Second)
	runtime.observeMu.Unlock()
	if _, err := runtime.observeSession(t.Context(), "bulk-observation"); err != nil {
		t.Fatalf("refresh expired observation: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expired observation calls = %d, want refresh to 2", calls.Load())
	}
}

func TestApplyBulkAtomicExecutionResultKeepsDurableReceiptAuthoritativeAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	output := &BulkAssignAssignment{
		Pane: "0", PaneID: "%41", Bead: "bd-post-success-cancel", BeadTitle: "Original", Status: "planned",
	}
	record := &assignment.Assignment{
		BeadID: "bd-post-success-cancel", BeadTitle: "Durable", IdempotencyKey: "post-success-key",
		Status: assignment.StatusAssigned, ClaimState: assignment.ClaimClaimed, ClaimActor: "ExactAgent",
		DispatchState: assignment.DispatchSent, DispatchReceiptID: "receipt-post-success",
	}
	applyBulkAtomicExecutionResult(ctx, output, "post-success-key", assignment.AtomicResult{
		Assignment: record,
		Sent:       true,
		Dispatch:   assignment.DispatchReceipt{DeliveryID: "receipt-post-success"},
	}, nil)

	if output.Status != "assigned" || !output.PromptSent || !output.Claimed || output.ClaimActor != "ExactAgent" ||
		output.DispatchReceiptID != "receipt-post-success" || output.Error != "" || output.failureCause != nil {
		t.Fatalf("post-success cancellation overwrote authoritative receipt: %+v", output)
	}

	unstarted := &BulkAssignAssignment{Pane: "1", PaneID: "%42", Bead: "bd-unstarted-cancel", Status: "planned"}
	applyBulkAtomicExecutionResult(ctx, unstarted, "unstarted-key", assignment.AtomicResult{}, nil)
	if unstarted.Status != "failed" || unstarted.PromptSent || unstarted.failureCode != ErrCodeTimeout || !errors.Is(unstarted.failureCause, context.Canceled) {
		t.Fatalf("unstarted canceled assignment = %+v", unstarted)
	}
}

func TestBulkAssignFailureClassReportsAmbiguousDispatchTruthfully(t *testing.T) {
	code, hint := bulkAssignFailureClass([]BulkAssignAssignment{{
		Status: "failed", failureCause: assignment.ErrDispatchOutcomeUnknown,
	}})
	if code != ErrCodeDispatchUnknown || !strings.Contains(hint, "outcome is unknown") || strings.Contains(hint, "no failed target was dispatched") {
		t.Fatalf("ambiguous dispatch classification code=%q hint=%q", code, hint)
	}
}

func TestBulkAssignClaimConflictNeverReservesOrDispatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	plan := allocateBulkAssignBeads(
		[]bulkPane{{Ref: tmux.PaneRef{ID: "%7", WindowIndex: 0, PaneIndex: 1}, AgentType: "claude"}},
		[]bulkBead{{ID: "bd-owned", Title: "Owned"}},
	)
	reserveCalls := 0
	deliverCalls := 0
	deps := BulkAssignDependencies{
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
		Cwd:      func() (string, error) { return t.TempDir(), nil },
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return []tmux.Pane{{ID: "%7", WindowIndex: 0, Index: 1, Type: tmux.AgentClaude}}, nil
		},
		LoadStore: func(string) (*assignment.AssignmentStore, error) { return assignment.NewStore("bulk-conflict"), nil },
		ClaimBead: func(context.Context, string, string, string) (bv.BeadClaimResult, error) {
			return bv.BeadClaimResult{}, bv.ErrBeadAlreadyClaimed
		},
		NewIdempotencyKey: func() (string, error) { return "conflict-key", nil },
		ReservationPort: assignment.ReservationFunc(func(context.Context, assignment.ReservationRequest) (assignment.LeaseReceipt, error) {
			reserveCalls++
			return assignment.LeaseReceipt{}, nil
		}),
		ResolveAgentName: func(context.Context, string, string, string, string) (string, error) {
			return "ClaimConflictAgent", nil
		},
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(context.Context, dispatchsvc.Delivery) error {
			deliverCalls++
			return nil
		}),
		LoadRedaction: func(string) (redaction.Config, error) { return redaction.Config{Mode: redaction.ModeOff}, nil },
	}
	output := BulkAssignOutput{Session: "bulk-conflict"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{RequireReservation: true, ReservationPaths: []string{"internal/robot/**"}}, bulkAssignDeps(&deps), &output, plan)
	if len(output.Assignments) != 1 || output.Assignments[0].Status != "failed" || output.Assignments[0].PromptSent || output.Assignments[0].Claimed {
		t.Fatalf("output=%+v", output.Assignments)
	}
	if reserveCalls != 0 || deliverCalls != 0 {
		t.Fatalf("reserve=%d dispatch=%d, want zero", reserveCalls, deliverCalls)
	}
}

func TestRobotAtomicClaimPortMapsAssignmentEligibilityRejection(t *testing.T) {
	port := newRobotAtomicClaimPort(t.TempDir(), func(context.Context, string, string, string) (bv.BeadClaimResult, error) {
		return bv.BeadClaimResult{}, &bv.AssignmentEligibilityError{
			BeadID: "bd-gated", Status: "open", OperatorLabels: []string{"operator-gated"},
		}
	})
	_, err := port.Claim(t.Context(), "bd-gated", "StableActor")
	if !errors.Is(err, assignment.ErrClaimIneligible) || errors.Is(err, assignment.ErrClaimConflict) {
		t.Fatalf("robot atomic claim error=%v, want ineligible only", err)
	}
}

func TestBulkAssignDryRunHasNoAtomicOrPacingSideEffects(t *testing.T) {
	plan := allocateBulkAssignBeads(
		[]bulkPane{{Ref: tmux.PaneRef{ID: "%8", WindowIndex: 0, PaneIndex: 1}, AgentType: "codex"}},
		[]bulkBead{{ID: "bd-dry", Title: "Dry"}},
	)
	calls := 0
	deps := BulkAssignDependencies{
		ReadFile: func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
		LoadStore: func(string) (*assignment.AssignmentStore, error) {
			calls++
			return nil, errors.New("unexpected load")
		},
		ClaimBead: func(context.Context, string, string, string) (bv.BeadClaimResult, error) {
			calls++
			return bv.BeadClaimResult{}, nil
		},
		NewIdempotencyKey: func() (string, error) { calls++; return "", nil },
		ReservationPort: assignment.ReservationFunc(func(context.Context, assignment.ReservationRequest) (assignment.LeaseReceipt, error) {
			calls++
			return assignment.LeaseReceipt{}, nil
		}),
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(context.Context, dispatchsvc.Delivery) error { calls++; return nil }),
		Wait:              func(context.Context, time.Duration) error { calls++; return nil },
	}
	output := BulkAssignOutput{Session: "bulk-dry"}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{DryRun: true, RequireReservation: true, ReservationPaths: []string{"internal/robot/**"}, Stagger: time.Hour}, bulkAssignDeps(&deps), &output, plan)
	if calls != 0 || len(output.Assignments) != 1 || output.Assignments[0].Status != "planned" {
		t.Fatalf("calls=%d output=%+v", calls, output.Assignments)
	}
}

func TestRobotAtomicIdempotencyKeyDoesNotReplayTerminalAssignment(t *testing.T) {
	for _, status := range []assignment.AssignmentStatus{
		assignment.StatusCompleted,
		assignment.StatusFailed,
		assignment.StatusReassigned,
	} {
		t.Run(string(status), func(t *testing.T) {
			store := assignment.NewStore("terminal-key-" + string(status))
			store.Assignments["bd-terminal"] = &assignment.Assignment{
				BeadID: "bd-terminal", Status: status, Pane: 1, AgentType: "codex", AgentName: "AgentOne",
				IdempotencyKey: "old-key", DispatchTarget: "%8", OccupancyKey: "%8",
				DispatchState: assignment.DispatchSent, IntentSHA256: assignment.PromptSHA256("prompt"), PromptSent: "prompt",
			}
			calls := 0
			key, err := robotAtomicIdempotencyKey(
				store, "bd-terminal", "%8", 1, "codex", "AgentOne", "prompt", false, nil,
				func() (string, error) { calls++; return "new-key", nil },
			)
			if err != nil {
				t.Fatalf("robotAtomicIdempotencyKey: %v", err)
			}
			if key != "new-key" || calls != 1 {
				t.Fatalf("terminal status %s reused key %q calls=%d", status, key, calls)
			}
			if replay := robotAtomicReplayIntent(store, "bd-terminal", "%8", 1, "codex", "prompt", false, nil); replay != nil {
				t.Fatalf("terminal status %s replayed stale receipt: %+v", status, replay)
			}
		})
	}
}

func TestBulkAssignTerminalGenerationRequiresReopenedBead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const session = "bulk-terminal-closed"
	const beadID = "bd-terminal-closed"
	const oldKey = "terminal-closed-key"
	store := assignment.NewStore(session)
	store.Assignments[beadID] = &assignment.Assignment{
		BeadID: beadID, BeadTitle: "Closed terminal bead", Pane: 1,
		AgentType: "codex", AgentName: "TerminalAgent", Status: assignment.StatusCompleted,
		AssignedAt: time.Now().UTC(), IdempotencyKey: oldKey, ClaimActor: "retained-actor",
		DispatchTarget: "%8", OccupancyKey: "%8", DispatchState: assignment.DispatchSent,
		DispatchReceiptID: "terminal-receipt", PromptSent: "already delivered",
	}
	if err := store.Save(); err != nil {
		t.Fatalf("seed terminal store: %v", err)
	}
	plan := allocateBulkAssignBeads(
		[]bulkPane{{Ref: tmux.PaneRef{ID: "%8", WindowIndex: 0, PaneIndex: 1}, AgentType: "codex"}},
		[]bulkBead{{ID: beadID, Title: "Closed terminal bead"}},
	)
	claimCalls := 0
	deliverCalls := 0
	panes := []tmux.Pane{{ID: "%8", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex}}
	deps := BulkAssignDependencies{
		ReadFile:       func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
		Cwd:            func() (string, error) { return t.TempDir(), nil },
		ListPanes:      func(context.Context, string) ([]tmux.Pane, error) { return append([]tmux.Pane(nil), panes...), nil },
		LoadStore:      func(string) (*assignment.AssignmentStore, error) { return store, nil },
		GetBeadStatus:  func(context.Context, string, string) (string, error) { return "closed", nil },
		ObserveSession: bulkSafeObserver(panes),
		ClaimBead: func(context.Context, string, string, string) (bv.BeadClaimResult, error) {
			claimCalls++
			return bv.BeadClaimResult{}, errors.New("closed bead must not be claimed")
		},
		NewIdempotencyKey: func() (string, error) { return "new-terminal-key", nil },
		LoadRedaction:     func(string) (redaction.Config, error) { return redaction.Config{Mode: redaction.ModeOff}, nil },
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(context.Context, dispatchsvc.Delivery) error {
			deliverCalls++
			return nil
		}),
	}
	output := BulkAssignOutput{Session: session}
	applyBulkAssignPlan(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), &output, plan)
	if len(output.Assignments) != 1 || output.Assignments[0].Status != "failed" ||
		!strings.Contains(output.Assignments[0].Error, "tracker status is \"closed\"") || output.Assignments[0].PromptSent {
		t.Fatalf("closed terminal assignment output=%+v", output.Assignments)
	}
	if claimCalls != 0 || deliverCalls != 0 {
		t.Fatalf("closed terminal assignment actuated claim=%d deliver=%d", claimCalls, deliverCalls)
	}
	stored := store.Get(beadID)
	if stored == nil || stored.IdempotencyKey != oldKey || stored.DispatchReceiptID != "terminal-receipt" || stored.Status != assignment.StatusCompleted {
		t.Fatalf("closed terminal assignment replaced durable generation: %+v", stored)
	}
}

func TestBulkAssignAllocationParsing(t *testing.T) {
	allocation := `{"1":"bd-1","2":"bd-2"}`
	parsed, err := parseBulkAssignAllocation(allocation)
	if err != nil {
		t.Fatalf("parseBulkAssignAllocation failed: %v", err)
	}

	t.Logf("parsed allocation=%v", parsed)
	if parsed["1"] != "bd-1" || parsed["2"] != "bd-2" {
		t.Fatalf("unexpected allocation map: %v", parsed)
	}
}

func TestBulkAssignAllocationInvalidJSON(t *testing.T) {
	_, err := parseBulkAssignAllocation("not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if _, err := parseBulkAssignAllocation(`{}`); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty allocation error = %v, want non-empty allocation validation", err)
	}
}

func TestBulkAssignSkipPanesApplied(t *testing.T) {
	panes := []tmux.Pane{
		{Index: 1, Title: "proj__cc_1"},
		{Index: 2, Title: "proj__cc_2"},
		{Index: 3, Title: "proj__cc_3"},
	}
	filtered, err := filterBulkAssignPanes(panes, []string{"2"})
	if err != nil {
		t.Fatalf("filter panes: %v", err)
	}

	got := []int{}
	for _, pane := range filtered {
		got = append(got, pane.Ref.PaneIndex)
	}
	sort.Ints(got)
	expected := []int{1, 3}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("filtered panes mismatch: got %v want %v", got, expected)
	}

	t.Logf("filtered panes=%v", got)
}

func TestBulkAssignEmptySession(t *testing.T) {
	triage := mockTriage([]bv.TriageRecommendation{{ID: "bd-1", Title: "Test", Status: "ready", Priority: 1}}, nil)
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "ready"}, BulkAssignDependencies{}, nil, triage, nil)

	if len(plan.Assignments) != 0 {
		t.Fatalf("expected no assignments, got %d", len(plan.Assignments))
	}
	if len(plan.UnassignedBeads) != 1 {
		t.Fatalf("expected 1 unassigned bead, got %v", plan.UnassignedBeads)
	}

	t.Logf("plan=%+v", plan)
}

func TestBulkAssignControlPaneOnly(t *testing.T) {
	panes := []tmux.Pane{{Index: 0, Title: "proj__user_0"}}
	filtered, err := filterBulkAssignPanes(panes, nil)
	if err != nil {
		t.Fatalf("filter panes: %v", err)
	}

	if len(filtered) != 0 {
		t.Fatalf("expected 0 agent panes, got %d", len(filtered))
	}
}

func TestBulkAssignFilterPrefersParsedPaneType(t *testing.T) {
	panes := []tmux.Pane{
		{Index: 1, ID: "%1", Title: "scratch", Type: tmux.AgentClaude},
		{Index: 2, ID: "%2", Title: "claude_notes", Type: tmux.AgentUser},
	}

	filtered, err := filterBulkAssignPanes(panes, nil)
	if err != nil {
		t.Fatalf("filter panes: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 agent pane, got %d", len(filtered))
	}
	if filtered[0].Ref.ID != "%1" || filtered[0].AgentType != "claude" {
		t.Fatalf("filtered pane = %+v, want target %%1 type claude", filtered[0])
	}
}

func TestBulkAssignInvalidBeadIDInAllocation(t *testing.T) {
	allocation := map[string]string{"1": "bd-missing"}
	panes := mockPanes("proj", []int{1})
	deps := BulkAssignDependencies{
		FetchBeadTitle: func(_ context.Context, _ string, beadID string) (string, error) {
			return "", fmt.Errorf("bead %s not found", beadID)
		},
		Cwd: func() (string, error) { return "/tmp", nil },
	}

	plan := planBulkAssignFromAllocation(t.Context(), BulkAssignOptions{}, bulkAssignDeps(&deps), panes, allocation)
	if plan.Assignments[0].Status != "failed" {
		t.Fatalf("expected failed status, got %q", plan.Assignments[0].Status)
	}

	t.Logf("assignment=%+v", plan.Assignments[0])
}

func TestBulkAssignBVFailure(t *testing.T) {
	deps := BulkAssignDependencies{
		FetchTriage: func(context.Context, string) (*bv.TriageResponse, error) {
			return nil, errors.New("bv failed")
		},
		FetchInProgress: func(context.Context, string, int) ([]bv.BeadInProgress, error) {
			return nil, nil
		},
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return mockTmuxPanesForList([]int{1}), nil
		},
		Cwd: func() (string, error) { return "/tmp", nil },
	}

	output, err := captureStdout(t, func() error {
		return PrintBulkAssign(t.Context(), BulkAssignOptions{Session: "proj", FromBV: true, Deps: &deps})
	})
	var exitErr *ProcessExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !exitErr.JSONWritten() {
		t.Fatalf("PrintBulkAssign error = %T %v, want written exit-1 error", err, err)
	}

	var result BulkAssignOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse output as JSON: %v", err)
	}
	if result.Success {
		t.Fatal("expected success=false when bv triage fails")
	}
	if result.ErrorCode != ErrCodeInternalError {
		t.Fatalf("expected error_code %s, got %s", ErrCodeInternalError, result.ErrorCode)
	}
	if !strings.Contains(result.Error, "bv triage failed") {
		t.Fatalf("expected error to mention triage failure, got: %s", result.Error)
	}
}

func TestBulkAssignUsesAuthoritativeSessionProjectForBVPlanning(t *testing.T) {
	authoritative := t.TempDir()
	wrongCWD := t.TempDir()
	var triageDir, inProgressDir string
	cwdCalls := 0
	deps := BulkAssignDependencies{
		ResolveProject: func(_ context.Context, session string, _ []tmux.Pane) (string, error) {
			if session != "authoritative-session" {
				t.Fatalf("ResolveProject session = %q", session)
			}
			return authoritative, nil
		},
		Cwd: func() (string, error) {
			cwdCalls++
			return wrongCWD, nil
		},
		FetchTriage: func(_ context.Context, dir string) (*bv.TriageResponse, error) {
			triageDir = dir
			return mockTriage([]bv.TriageRecommendation{{ID: "bd-auth", Title: "Scoped work", Status: "open", Priority: 1}}, nil), nil
		},
		FetchInProgress: func(_ context.Context, dir string, _ int) ([]bv.BeadInProgress, error) {
			inProgressDir = dir
			return nil, nil
		},
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) {
			return mockTmuxPanesForList([]int{1}), nil
		},
	}

	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{
		Session: "authoritative-session", FromBV: true, DryRun: true, Deps: &deps,
	})
	if err != nil {
		t.Fatalf("GetBulkAssign: %v", err)
	}
	if !output.Success || len(output.Assignments) != 1 {
		t.Fatalf("bulk assignment output = %+v", output)
	}
	if triageDir != authoritative || inProgressDir != authoritative {
		t.Fatalf("planning dirs = triage %q in-progress %q, want %q", triageDir, inProgressDir, authoritative)
	}
	if cwdCalls != 0 {
		t.Fatalf("caller CWD resolved %d time(s), want zero", cwdCalls)
	}
}

func TestBulkAssignLiveSessionProjectOverridesCallerCWDWithoutSavedMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sessionProject := t.TempDir()
	callerProject := t.TempDir()
	for _, projectDir := range []string{sessionProject, callerProject} {
		if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
			t.Fatalf("create project marker: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(projectDir, ".beads"), 0o755); err != nil {
			t.Fatalf("create beads project marker: %v", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(sessionProject, "internal", "robot"), 0o755); err != nil {
		t.Fatalf("create live pane subdirectory: %v", err)
	}
	var triageDir string
	const session = "exact-live-session"
	panes := mockTmuxPanesForList([]int{1})
	deps := BulkAssignDependencies{
		Cwd: func() (string, error) { return callerProject, nil },
		PaneCurrentPath: func(_ context.Context, paneID string) (string, error) {
			if paneID != "%1" {
				t.Fatalf("pane path lookup = %q, want %%1", paneID)
			}
			return filepath.Join(sessionProject, "internal", "robot"), nil
		},
		ListPanes: func(_ context.Context, gotSession string) ([]tmux.Pane, error) {
			if gotSession != session {
				t.Fatalf("ListPanes session = %q, want exact %q", gotSession, session)
			}
			return panes, nil
		},
		FetchTriage: func(_ context.Context, dir string) (*bv.TriageResponse, error) {
			triageDir = dir
			return mockTriage([]bv.TriageRecommendation{{ID: "bd-live", Title: "Live scoped work", Status: "open", Priority: 1}}, nil), nil
		},
		FetchInProgress: func(context.Context, string, int) ([]bv.BeadInProgress, error) { return nil, nil },
	}

	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{Session: session, FromBV: true, DryRun: true, Deps: &deps})
	if err != nil {
		t.Fatalf("GetBulkAssign: %v", err)
	}
	if !output.Success || len(output.Assignments) != 1 {
		t.Fatalf("bulk assignment output = %+v", output)
	}
	if triageDir != sessionProject {
		t.Fatalf("triage dir = %q, want live session project %q instead of caller CWD %q", triageDir, sessionProject, callerProject)
	}
}

func TestBulkAssignRejectsLiveSessionWithMultipleProjectRoots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	firstProject := t.TempDir()
	secondProject := t.TempDir()
	for _, projectDir := range []string{firstProject, secondProject} {
		if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0o755); err != nil {
			t.Fatalf("create project marker: %v", err)
		}
	}
	panes := mockTmuxPanesForList([]int{1, 2})
	deps := BulkAssignDependencies{
		Cwd: func() (string, error) { return secondProject, nil },
		PaneCurrentPath: func(_ context.Context, paneID string) (string, error) {
			if paneID == "%1" {
				return firstProject, nil
			}
			return secondProject, nil
		},
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) { return panes, nil },
		FetchTriage: func(context.Context, string) (*bv.TriageResponse, error) {
			t.Fatal("triage must not run for ambiguous live project roots")
			return nil, nil
		},
	}

	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{Session: "mixed-live-session", FromBV: true, DryRun: true, Deps: &deps})
	if err != nil {
		t.Fatalf("GetBulkAssign: %v", err)
	}
	if output.Success || !strings.Contains(output.Error, "multiple project roots") {
		t.Fatalf("bulk assignment output = %+v, want mixed-root failure", output)
	}
}

func TestBulkAssignUsesAuthoritativeSessionProjectForActuation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	authoritative := t.TempDir()
	wrongCWD := t.TempDir()
	store := assignment.NewStore("authoritative-actuation")
	var titleDir, claimDir, redactionDir string
	cwdCalls := 0
	panes := mockTmuxPanesForList([]int{1})
	deps := BulkAssignDependencies{
		ResolveProject: func(context.Context, string, []tmux.Pane) (string, error) { return authoritative, nil },
		Cwd: func() (string, error) {
			cwdCalls++
			return wrongCWD, nil
		},
		FetchBeadTitle: func(_ context.Context, dir, beadID string) (string, error) {
			titleDir = dir
			return "Scoped " + beadID, nil
		},
		ListPanes: func(context.Context, string) ([]tmux.Pane, error) { return panes, nil },
		ReadFile:  func(string) ([]byte, error) { return []byte(defaultBulkAssignTemplate), nil },
		LoadStore: func(string) (*assignment.AssignmentStore, error) { return store, nil },
		ClaimBead: func(_ context.Context, dir, beadID, actor string) (bv.BeadClaimResult, error) {
			claimDir = dir
			return bv.BeadClaimResult{ID: beadID, Actor: actor, Status: "in_progress", ClaimedAt: time.Now().UTC()}, nil
		},
		NewIdempotencyKey: func() (string, error) { return "authoritative-key", nil },
		ResolveAgentName: func(context.Context, string, string, string, string) (string, error) {
			return "ScopedAgent", nil
		},
		ObserveSession:    bulkSafeObserver(panes),
		DispatchDeliverer: dispatchsvc.DelivererFunc(func(context.Context, dispatchsvc.Delivery) error { return nil }),
		LoadRedaction: func(dir string) (redaction.Config, error) {
			redactionDir = dir
			return redaction.Config{Mode: redaction.ModeOff}, nil
		},
	}

	output, err := GetBulkAssign(t.Context(), BulkAssignOptions{
		Session: "authoritative-actuation", AllocationJSON: `{"1":"bd-auth"}`, Deps: &deps,
	})
	if err != nil {
		t.Fatalf("GetBulkAssign: %v", err)
	}
	if !output.Success || len(output.Assignments) != 1 || output.Assignments[0].Status != "assigned" {
		t.Fatalf("bulk assignment output = %+v", output)
	}
	if titleDir != authoritative || claimDir != authoritative || redactionDir != authoritative {
		t.Fatalf("actuation dirs = title %q claim %q redaction %q, want %q", titleDir, claimDir, redactionDir, authoritative)
	}
	if cwdCalls != 0 {
		t.Fatalf("caller CWD resolved %d time(s), want zero", cwdCalls)
	}
}

func TestRobotReservationPreflightFailuresGuaranteeNoLease(t *testing.T) {
	runtime := &robotAgentMailReservationRuntime{
		client: &fakeRobotReservationClient{}, projectKey: "/project", projectID: 7,
		registered: map[string]agentmail.Agent{"KnownAgent": {Name: "KnownAgent", ProjectID: 7}},
	}
	for _, test := range []struct {
		name string
		req  assignment.ReservationRequest
	}{
		{name: "unregistered recipient", req: assignment.ReservationRequest{AgentName: "MissingAgent", RequestedPaths: []string{"internal/**"}}},
		{name: "invalid paths", req: assignment.ReservationRequest{AgentName: "KnownAgent", RequestedPaths: []string{"internal/**", "internal/**"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := runtime.Reserve(t.Context(), test.req)
			if err == nil || !assignment.IsGuaranteedNoReservation(err) {
				t.Fatalf("Reserve() error = %v, want guaranteed no-reservation failure", err)
			}
		})
	}
}

func TestRobotReservationPartialConflictPreservesLeaseHandles(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour)
	client := &fakeRobotReservationClient{reserveResult: &agentmail.ReservationResult{
		Granted: []agentmail.FileReservation{{
			ID: 41, PathPattern: "internal/robot/**", AgentName: "KnownAgent", ProjectID: 7, Exclusive: true,
			Reason: "bead assignment: bd-partial", ExpiresTS: agentmail.FlexTime{Time: expiresAt},
		}},
		Conflicts: []agentmail.ReservationConflict{{Path: "internal/cli/**", Holders: []string{"OtherAgent"}}},
	}}
	runtime := &robotAgentMailReservationRuntime{
		client: client, projectKey: "/project", projectID: 7,
		registered: map[string]agentmail.Agent{"KnownAgent": {Name: "KnownAgent", ProjectID: 7}},
	}
	lease, err := runtime.Reserve(t.Context(), assignment.ReservationRequest{
		BeadID: "bd-partial", AgentName: "KnownAgent", Target: "%1",
		RequestedPaths: []string{"internal/robot/**", "internal/cli/**"},
	})
	if err == nil || assignment.IsGuaranteedNoReservation(err) {
		t.Fatalf("Reserve() error = %v, want ambiguous partial-lease failure", err)
	}
	if !reflect.DeepEqual(lease.Granted, []string{"internal/robot/**"}) || !reflect.DeepEqual(lease.ReservationIDs, []int{41}) {
		t.Fatalf("partial lease = %+v, want retained release handles", lease)
	}
}

func TestRobotReservationRejectsMalformedGrantReceipts(t *testing.T) {
	valid := agentmail.FileReservation{
		ID: 42, PathPattern: "internal/robot/**", AgentName: "KnownAgent", ProjectID: 7, Exclusive: true,
		Reason: "bead assignment: bd-malformed", ExpiresTS: agentmail.FlexTime{Time: time.Now().UTC().Add(time.Hour)},
	}
	for _, test := range []struct {
		name   string
		mutate func(*agentmail.FileReservation)
	}{
		{name: "wrong reason", mutate: func(row *agentmail.FileReservation) { row.Reason = "bead assignment: other" }},
		{name: "already released", mutate: func(row *agentmail.FileReservation) {
			released := agentmail.FlexTime{Time: time.Now().UTC()}
			row.ReleasedTS = &released
		}},
		{name: "missing expiry", mutate: func(row *agentmail.FileReservation) { row.ExpiresTS = agentmail.FlexTime{} }},
		{name: "expired", mutate: func(row *agentmail.FileReservation) {
			row.ExpiresTS = agentmail.FlexTime{Time: time.Now().UTC().Add(-time.Second)}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := valid
			test.mutate(&row)
			runtime := &robotAgentMailReservationRuntime{
				client:     &fakeRobotReservationClient{reserveResult: &agentmail.ReservationResult{Granted: []agentmail.FileReservation{row}}},
				projectKey: "/project", projectID: 7,
				registered: map[string]agentmail.Agent{"KnownAgent": {Name: "KnownAgent", ProjectID: 7}},
			}
			lease, err := runtime.Reserve(t.Context(), assignment.ReservationRequest{
				BeadID: "bd-malformed", AgentName: "KnownAgent", Target: "%1", RequestedPaths: []string{"internal/robot/**"},
			})
			if err == nil {
				t.Fatal("malformed grant was accepted")
			}
			if !reflect.DeepEqual(lease.ReservationIDs, []int{42}) || !reflect.DeepEqual(lease.Granted, []string{"internal/robot/**"}) {
				t.Fatalf("malformed grant lost cleanup handles: %+v", lease)
			}
		})
	}
}

func TestRobotReservationReconciliationDistinguishesAbsentPartialAndReserved(t *testing.T) {
	base := agentmail.FileReservation{
		ID: 51, PathPattern: "internal/robot/**", AgentName: "KnownAgent", ProjectID: 7,
		Exclusive: true, Reason: "bead assignment: bd-reconcile",
		ExpiresTS: agentmail.FlexTime{Time: time.Now().UTC().Add(time.Hour)},
	}
	second := base
	second.ID = 52
	second.PathPattern = "internal/cli/**"
	missingExpiry := base
	missingExpiry.ExpiresTS = agentmail.FlexTime{}
	request := assignment.ReservationRequest{
		BeadID: "bd-reconcile", AgentName: "KnownAgent", Target: "%1",
		RequestedPaths: []string{"internal/robot/**", "internal/cli/**"},
	}
	for _, test := range []struct {
		name         string
		reservations []agentmail.FileReservation
		want         assignment.ReservationReconciliationState
	}{
		{name: "absent", want: assignment.ReservationReconciliationAbsent},
		{name: "partial", reservations: []agentmail.FileReservation{base}, want: assignment.ReservationReconciliationUnknown},
		{name: "one missing expiry", reservations: []agentmail.FileReservation{missingExpiry, second}, want: assignment.ReservationReconciliationUnknown},
		{name: "reserved", reservations: []agentmail.FileReservation{base, second}, want: assignment.ReservationReconciliationReserved},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := &robotAgentMailReservationRuntime{
				client:     &fakeRobotReservationClient{reservations: test.reservations},
				projectKey: "/project", projectID: 7,
			}
			got, err := runtime.ReconcileReservation(t.Context(), request, assignment.LeaseReceipt{})
			if err != nil {
				t.Fatalf("ReconcileReservation: %v", err)
			}
			if got.State != test.want {
				t.Fatalf("state = %q, want %q; lease=%+v", got.State, test.want, got.Lease)
			}
			if test.name == "one missing expiry" && !reflect.DeepEqual(got.Lease.ReservationIDs, []int{51}) {
				t.Fatalf("malformed reconciliation lost cleanup handle: %+v", got.Lease)
			}
			if test.want == assignment.ReservationReconciliationReserved && len(got.Lease.ReservationIDs) != 2 {
				t.Fatalf("reserved lease = %+v", got.Lease)
			}
		})
	}
}

func TestBulkAssignPaneListMapsMissingSessionAndTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{name: "tmux missing window wording", err: errors.New("can't find window: absent"), code: ErrCodeSessionNotFound},
		{name: "context timeout", err: context.DeadlineExceeded, code: ErrCodeTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			startedAt := time.Now().UTC()
			deps := BulkAssignDependencies{
				ListPanes: func(context.Context, string) ([]tmux.Pane, error) { return nil, test.err },
			}
			output, err := GetBulkAssign(t.Context(), BulkAssignOptions{Session: "absent", AllocationJSON: `{}`, Deps: &deps})
			if err != nil {
				t.Fatalf("GetBulkAssign returned transport error: %v", err)
			}
			if output.Success || output.ErrorCode != test.code || output.Assignments == nil {
				t.Fatalf("response=%+v, want success=false code=%s and non-nil assignments", output, test.code)
			}
			timestamp, parseErr := time.Parse(time.RFC3339Nano, output.Timestamp)
			if parseErr != nil || timestamp.Before(startedAt.Add(-time.Second)) || timestamp.After(time.Now().UTC().Add(time.Second)) {
				t.Fatalf("early failure timestamp = %q parsed=%v err=%v", output.Timestamp, timestamp, parseErr)
			}
		})
	}
}

func TestBulkAssignLargeTriage(t *testing.T) {
	var recs []bv.TriageRecommendation
	for i := 0; i < 120; i++ {
		recs = append(recs, bv.TriageRecommendation{
			ID:       fmt.Sprintf("bd-%03d", i),
			Title:    fmt.Sprintf("Task %d", i),
			Status:   "ready",
			Priority: i % 5,
		})
	}
	triage := mockTriage(recs, nil)
	panes := mockPanes("proj", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	plan := planBulkAssignFromBV(BulkAssignOptions{Strategy: "ready"}, BulkAssignDependencies{}, panes, triage, nil)

	if len(plan.Assignments) != 10 {
		t.Fatalf("expected 10 assignments, got %d", len(plan.Assignments))
	}
	if len(plan.UnassignedBeads) != 110 {
		t.Fatalf("expected 110 unassigned beads, got %d", len(plan.UnassignedBeads))
	}

	t.Logf("assignments=%d unassigned=%d", len(plan.Assignments), len(plan.UnassignedBeads))
}

func TestBulkAssignConcurrentSafety(t *testing.T) {
	triage := mockTriage([]bv.TriageRecommendation{{ID: "bd-1", Title: "Test", Status: "ready", Priority: 1}}, nil)
	inProgress := []bv.BeadInProgress{{ID: "bd-2", Title: "Stale", UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	candidates := buildBulkAssignCandidates(triage, inProgress)
	before := candidates

	_ = selectBalancedBeads(candidates)
	if !reflect.DeepEqual(before, candidates) {
		t.Fatalf("expected candidates to remain unchanged")
	}
}

// helpers

type fakeRobotReservationClient struct {
	reserveResult *agentmail.ReservationResult
	reserveErr    error
	reservations  []agentmail.FileReservation
	listErr       error
}

func (f *fakeRobotReservationClient) EnsureProject(context.Context, string) (*agentmail.Project, error) {
	return &agentmail.Project{ID: 7, HumanKey: "/project"}, nil
}

func (f *fakeRobotReservationClient) ListAgents(context.Context, string) ([]agentmail.Agent, error) {
	return []agentmail.Agent{{Name: "KnownAgent", ProjectID: 7}}, nil
}

func (f *fakeRobotReservationClient) ListReservations(context.Context, string, string, bool) ([]agentmail.FileReservation, error) {
	return append([]agentmail.FileReservation(nil), f.reservations...), f.listErr
}

func (f *fakeRobotReservationClient) ReservePaths(context.Context, agentmail.FileReservationOptions) (*agentmail.ReservationResult, error) {
	return f.reserveResult, f.reserveErr
}

func mockTriage(recs []bv.TriageRecommendation, blockers []bv.BlockerToClear) *bv.TriageResponse {
	return &bv.TriageResponse{
		Triage: bv.TriageData{
			Recommendations: recs,
			BlockersToClear: blockers,
		},
	}
}

func mockPanes(session string, indices []int) []bulkPane {
	panes := make([]bulkPane, 0, len(indices))
	for _, idx := range indices {
		panes = append(panes, bulkPane{Ref: tmux.PaneRef{ID: fmt.Sprintf("%%%d", idx), WindowIndex: 0, PaneIndex: idx}, AgentType: "claude"})
	}
	sort.Slice(panes, func(i, j int) bool { return panes[i].Ref.PaneIndex < panes[j].Ref.PaneIndex })
	return panes
}

func mockTmuxPanesForList(indices []int) []tmux.Pane {
	panes := make([]tmux.Pane, 0, len(indices))
	for _, idx := range indices {
		panes = append(panes, tmux.Pane{ID: fmt.Sprintf("%%%d", idx), WindowIndex: 0, Index: idx, Title: fmt.Sprintf("proj__cc_%d", idx)})
	}
	return panes
}

func mustParseAllocation(t *testing.T, allocation string) map[string]string {
	parsed, err := parseBulkAssignAllocation(allocation)
	if err != nil {
		t.Fatalf("allocation parse failed: %v", err)
	}
	return parsed
}

func bulkAtomicTestDeps(t *testing.T, session string, plan bulkAssignPlan, deps BulkAssignDependencies) BulkAssignDependencies {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	store := assignment.NewStore(session)
	var mu sync.Mutex
	owners := make(map[string]string)
	key := 0

	deps.LoadStore = func(string) (*assignment.AssignmentStore, error) { return store, nil }
	deps.ClaimBead = func(_ context.Context, _ string, beadID, actor string) (bv.BeadClaimResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if owner := owners[beadID]; owner != "" && owner != actor {
			return bv.BeadClaimResult{}, bv.ErrBeadAlreadyClaimed
		}
		owners[beadID] = actor
		return bv.BeadClaimResult{ID: beadID, Actor: actor, Status: "in_progress", ClaimedAt: time.Now().UTC()}, nil
	}
	deps.NewIdempotencyKey = func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		key++
		return fmt.Sprintf("bulk-test-key-%d", key), nil
	}
	deps.LoadRedaction = func(string) (redaction.Config, error) {
		return redaction.Config{Mode: redaction.ModeOff}, nil
	}
	if deps.ResolveAgentName == nil {
		deps.ResolveAgentName = func(_ context.Context, _, _, paneID, _ string) (string, error) {
			return "TestAgent-" + strings.TrimPrefix(paneID, "%"), nil
		}
	}
	if deps.Cwd == nil {
		workDir := t.TempDir()
		deps.Cwd = func() (string, error) { return workDir, nil }
	}
	if deps.ListPanes == nil {
		panes := make([]tmux.Pane, 0, len(plan.Assignments))
		for _, planned := range plan.Assignments {
			panes = append(panes, tmux.Pane{
				ID: planned.PaneID, WindowIndex: 0, Index: planned.paneIndex,
				Type: bulkAssignTMUXAgentType(planned.AgentType),
			})
		}
		deps.ListPanes = func(context.Context, string) ([]tmux.Pane, error) {
			return append([]tmux.Pane(nil), panes...), nil
		}
	}
	deps.ObserveSession = func(_ context.Context, observedSession string) (statuspkg.SessionObservation, error) {
		panes, err := deps.ListPanes(context.Background(), observedSession)
		if err != nil {
			return statuspkg.SessionObservation{}, err
		}
		return bulkSafeObservation(observedSession, panes), nil
	}
	return deps
}

func bulkSafeObserver(panes []tmux.Pane) func(context.Context, string) (statuspkg.SessionObservation, error) {
	return func(_ context.Context, session string) (statuspkg.SessionObservation, error) {
		return bulkSafeObservation(session, panes), nil
	}
}

func bulkSafeObservation(session string, panes []tmux.Pane) statuspkg.SessionObservation {
	observedAt := time.Now().UTC()
	observation := statuspkg.SessionObservation{Session: session, ObservedAt: observedAt, Complete: true}
	for _, pane := range panes {
		observation.Panes = append(observation.Panes, statuspkg.PaneObservation{
			Pane: pane.Ref(), Metadata: pane,
			Current: statuspkg.StateObservation{
				Status:     statuspkg.AgentStatus{PaneID: pane.ID, State: statuspkg.StateIdle, UpdatedAt: observedAt},
				ObservedAt: observedAt, Freshness: statuspkg.FreshnessFresh, Confidence: 1,
			},
		})
	}
	return observation
}

func osReadFile(path string) ([]byte, error) {
	return osReadFileImpl(path)
}

var osReadFileImpl = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}
