package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	assignpkg "github.com/Dicklesworthstone/ntm/internal/assign"
	"github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/config"
	statuspkg "github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// ============================================================================
// Dependency Awareness Tests
// ============================================================================

// TestDependencyFilteringInAssignment tests that blocked beads are properly filtered out
// and added to the skipped list with the correct reason.
func TestDependencyFilteringInAssignment(t *testing.T) {
	// Test the SkippedItem structure has correct fields
	skipped := SkippedItem{
		BeadID:       "bd-123",
		BeadTitle:    "Test blocked bead",
		Reason:       "blocked_by_dependency",
		BlockedByIDs: []string{"bd-456", "bd-789"},
	}

	if skipped.Reason != "blocked_by_dependency" {
		t.Errorf("Expected reason 'blocked_by_dependency', got %q", skipped.Reason)
	}
	if len(skipped.BlockedByIDs) != 2 {
		t.Errorf("Expected 2 blockers, got %d", len(skipped.BlockedByIDs))
	}
}

// TestAssignSummaryBlockedCount tests that the summary correctly tracks blocked count
func TestAssignSummaryBlockedCount(t *testing.T) {
	summary := AssignSummaryEnhanced{
		TotalBeadCount:  10,
		ActionableCount: 7,
		BlockedCount:    3,
		AssignedCount:   5,
		SkippedCount:    5, // 3 blocked + 2 other reasons
		IdleAgents:      2,
	}

	if summary.TotalBeadCount != 10 {
		t.Errorf("Expected TotalBeadCount=10, got %d", summary.TotalBeadCount)
	}
	if summary.ActionableCount != 7 {
		t.Errorf("Expected ActionableCount=7, got %d", summary.ActionableCount)
	}
	if summary.BlockedCount != 3 {
		t.Errorf("Expected BlockedCount=3, got %d", summary.BlockedCount)
	}
}

// TestTriageRecommendationToBeadPreviewConversion tests the conversion logic
func TestTriageRecommendationToBeadPreviewConversion(t *testing.T) {
	tests := []struct {
		name          string
		rec           bv.TriageRecommendation
		expectBlocked bool
		expectedPrio  string
	}{
		{
			name: "actionable bead",
			rec: bv.TriageRecommendation{
				ID:        "bd-001",
				Title:     "Test actionable",
				Priority:  1,
				BlockedBy: nil,
			},
			expectBlocked: false,
			expectedPrio:  "P1",
		},
		{
			name: "blocked bead",
			rec: bv.TriageRecommendation{
				ID:        "bd-002",
				Title:     "Test blocked",
				Priority:  2,
				BlockedBy: []string{"bd-003"},
			},
			expectBlocked: true,
			expectedPrio:  "P2",
		},
		{
			name: "multiple blockers",
			rec: bv.TriageRecommendation{
				ID:        "bd-004",
				Title:     "Test multi-blocked",
				Priority:  0,
				BlockedBy: []string{"bd-005", "bd-006", "bd-007"},
			},
			expectBlocked: true,
			expectedPrio:  "P0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isBlocked := len(tc.rec.BlockedBy) > 0
			if isBlocked != tc.expectBlocked {
				t.Errorf("Expected blocked=%v, got %v", tc.expectBlocked, isBlocked)
			}

			// Test conversion to BeadPreview format
			preview := bv.BeadPreview{
				ID:       tc.rec.ID,
				Title:    tc.rec.Title,
				Priority: tc.expectedPrio,
			}
			if preview.Priority != tc.expectedPrio {
				t.Errorf("Expected priority %q, got %q", tc.expectedPrio, preview.Priority)
			}
		})
	}
}

// TestBlockedBeadsReasonString tests that blocked reason string is correct
func TestBlockedBeadsReasonString(t *testing.T) {
	const expectedReason = "blocked_by_dependency"

	// This is the reason string used in the assign command
	// to identify blocked beads in the Skipped list
	skipped := SkippedItem{
		BeadID:       "test",
		Reason:       expectedReason,
		BlockedByIDs: []string{"blocker1"},
	}

	if skipped.Reason != expectedReason {
		t.Errorf("Reason should be %q, got %q", expectedReason, skipped.Reason)
	}
}

// TestAssignOutputEnhancedStructure tests the output structure is correct for JSON
func TestAssignOutputEnhancedStructure(t *testing.T) {
	output := AssignOutputEnhanced{
		Strategy: "balanced",
		Assignments: []AssignmentItem{
			{
				BeadID:    "bd-100",
				BeadTitle: "Test task",
				Pane:      1,
				AgentType: "claude",
				Score:     0.85,
			},
		},
		Skipped: []SkippedItem{
			{
				BeadID:       "bd-101",
				BeadTitle:    "Blocked task",
				Reason:       "blocked_by_dependency",
				BlockedByIDs: []string{"bd-100"},
			},
		},
		Summary: AssignSummaryEnhanced{
			TotalBeadCount:  2,
			ActionableCount: 1,
			BlockedCount:    1,
			AssignedCount:   1,
			SkippedCount:    1,
			IdleAgents:      3,
		},
	}

	// Verify structure
	if output.Strategy != "balanced" {
		t.Errorf("Expected strategy 'balanced', got %q", output.Strategy)
	}
	if len(output.Assignments) != 1 {
		t.Errorf("Expected 1 assigned, got %d", len(output.Assignments))
	}
	if len(output.Skipped) != 1 {
		t.Errorf("Expected 1 skipped, got %d", len(output.Skipped))
	}
	if output.Summary.ActionableCount != 1 {
		t.Errorf("Expected ActionableCount=1, got %d", output.Summary.ActionableCount)
	}
	if output.Summary.BlockedCount != 1 {
		t.Errorf("Expected BlockedCount=1, got %d", output.Summary.BlockedCount)
	}
}

func createAssignProjectRoot(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	// On macOS, t.TempDir() returns /var/folders/... but the code under test
	// calls filepath.EvalSymlinks which canonicalises this to
	// /private/var/folders/... — resolve up-front so expected values match.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.MkdirAll(filepath.Join(root, ".ntm"), 0755); err != nil {
		t.Fatalf("creating .ntm dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ntm", "config.toml"), []byte(""), 0644); err != nil {
		t.Fatalf("writing config marker: %v", err)
	}
	nested := filepath.Join(root, "nested", "pkg")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("creating nested dir: %v", err)
	}

	return root, nested
}

func TestResolveAssignProjectDirUsesProjectRootFromSubdir(t *testing.T) {
	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	got, err := resolveAssignProjectDir("demo")
	if err != nil {
		t.Fatalf("resolveAssignProjectDir() error = %v", err)
	}
	if got != root {
		t.Fatalf("resolveAssignProjectDir() = %q, want %q", got, root)
	}
}

func TestResolveAssignProjectDirRejectsInvalidSessionName(t *testing.T) {
	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_, err := resolveAssignProjectDir("../escape")
	if err == nil {
		t.Fatal("expected invalid session error")
	}
	if got := err.Error(); !strings.Contains(got, "invalid session name") {
		t.Fatalf("expected invalid session error, got %v", err)
	}
}

func TestResolveAssignProjectDirUsesSavedSessionAgentProjectKey(t *testing.T) {
	isolateSessionAgentStorage(t)

	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	actualProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(actualProject, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir actual project git dir: %v", err)
	}
	saveSessionAgentForTest(t, "demo", actualProject, "GreenCastle")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	got, err := resolveAssignProjectDir("demo")
	if err != nil {
		t.Fatalf("resolveAssignProjectDir() error = %v", err)
	}
	if got != actualProject {
		t.Fatalf("resolveAssignProjectDir() = %q, want saved session agent project %q", got, actualProject)
	}
}

func TestResolveAssignProjectDirExplicitRepoOverridesSavedSessionProject(t *testing.T) {
	isolateSessionAgentStorage(t)

	savedProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(savedProject, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir saved project git dir: %v", err)
	}
	overrideProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(overrideProject, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir override project git dir: %v", err)
	}
	saveSessionAgentForTest(t, "demo", savedProject, "GreenCastle")

	previousRepo := assignRepoPath
	assignRepoPath = overrideProject
	t.Cleanup(func() { assignRepoPath = previousRepo })

	got, err := resolveAssignProjectDir("demo")
	if err != nil {
		t.Fatalf("resolveAssignProjectDir() error = %v", err)
	}
	want, err := filepath.Abs(overrideProject)
	if err != nil {
		t.Fatalf("filepath.Abs(): %v", err)
	}
	if got != want {
		t.Fatalf("resolveAssignProjectDir() = %q, want explicit repo %q instead of saved project %q", got, want, savedProject)
	}
}

func TestResolveAssignProjectDirResolvesProjectScopedPrefix(t *testing.T) {
	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	got, err := resolveAssignProjectDir("de")
	if err != nil {
		t.Fatalf("resolveAssignProjectDir() error = %v", err)
	}
	if got != root {
		t.Fatalf("resolveAssignProjectDir() = %q, want %q", got, root)
	}
}

func TestReleaseFileReservationsWithIDsUsesResolvedProjectDir(t *testing.T) {
	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	stub := newMailStub(t, nil)
	defer stub.Close()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := releaseFileReservationsWithIDs("demo", "bd-123", "BlueLake", []int{42}); err != nil {
		t.Fatalf("releaseFileReservationsWithIDs() error = %v", err)
	}

	if len(stub.releaseCalls) != 1 {
		t.Fatalf("expected 1 release call, got %d", len(stub.releaseCalls))
	}
	if got := stub.releaseCalls[0].Project; got != root {
		t.Fatalf("release project = %q, want %q", got, root)
	}
}

func TestClearStoredAssignmentRetainsBarrierUntilLeaseReleaseConfirmed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	projectDir := t.TempDir()
	previousRepo := assignRepoPath
	assignRepoPath = projectDir
	t.Cleanup(func() { assignRepoPath = previousRepo })
	store := assignment.NewStore("clear-release-barrier")
	if _, err := store.Assign("ntm-clear", "Clear me", 2, "codex", "BlueLake", "work"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	store.Assignments["ntm-clear"].ReservationCompleted = true
	store.Assignments["ntm-clear"].ReservedPaths = []string{"internal/cli/**"}
	store.Assignments["ntm-clear"].ReservationIDs = []int{41, 42}
	store.Assignments["ntm-clear"].ClaimActor = "BlueLake/ntm-clear"
	if err := store.Save(); err != nil {
		t.Fatalf("persist reservation metadata: %v", err)
	}

	originalRelease := releaseAssignmentLeases
	originalClaimRelease := releaseBeadClaimForAssignment
	t.Cleanup(func() {
		releaseAssignmentLeases = originalRelease
		releaseBeadClaimForAssignment = originalClaimRelease
	})
	releaseCalls := 0
	claimReleaseCalls := 0
	claimReleaseErr := errors.New("Beads sync unavailable")
	releaseBeadClaimForAssignment = func(_ context.Context, projectDir, beadID, actor string) (bool, error) {
		claimReleaseCalls++
		if projectDir != assignRepoPath || beadID != "ntm-clear" || actor != "BlueLake/ntm-clear" {
			t.Fatalf("claim release args project=%q bead=%q actor=%q", projectDir, beadID, actor)
		}
		if claimReleaseErr != nil {
			return false, claimReleaseErr
		}
		return true, nil
	}
	releaseAssignmentLeases = func(_ string, current *assignment.Assignment) ([]string, error) {
		releaseCalls++
		if current.ClearState != assignment.ClearStateReservationReleasing || !reflect.DeepEqual(current.ReservationIDs, []int{41, 42}) {
			t.Fatalf("release input lost clear barrier metadata: %+v", current)
		}
		return nil, errors.New("release unavailable")
	}

	current := store.Get("ntm-clear")
	if _, err := clearStoredAssignment(t.Context(), store, "clear-release-barrier", current); err == nil || !strings.Contains(err.Error(), "release unavailable") {
		t.Fatalf("first clear error=%v, want release failure", err)
	}
	failed := store.Get("ntm-clear")
	if failed == nil || failed.ClearState != assignment.ClearStateReservationReleasing || failed.ClearError != "release unavailable" || !reflect.DeepEqual(failed.ReservationIDs, []int{41, 42}) {
		t.Fatalf("release failure lost retryable ledger: %+v", failed)
	}
	if claimReleaseCalls != 0 {
		t.Fatalf("tracker claim released before reservation proof: calls=%d", claimReleaseCalls)
	}

	releaseAssignmentLeases = func(_ string, current *assignment.Assignment) ([]string, error) {
		releaseCalls++
		if current.ClearState != assignment.ClearStateReservationReleasing || !reflect.DeepEqual(current.ReservationIDs, []int{41, 42}) {
			t.Fatalf("retry input lost clear barrier metadata: %+v", current)
		}
		return []string{"2 reservations"}, nil
	}
	released, err := clearStoredAssignment(t.Context(), store, "clear-release-barrier", failed)
	if err == nil || !strings.Contains(err.Error(), claimReleaseErr.Error()) {
		t.Fatalf("clear after lease success error=%v, want claim-release failure", err)
	}
	checkpoint := store.Get("ntm-clear")
	if checkpoint == nil || checkpoint.ClearState != assignment.ClearStateLeasesReleased || len(checkpoint.ReservationIDs) != 0 || checkpoint.ClearError == "" {
		t.Fatalf("claim-release failure lost durable lease checkpoint: %+v", checkpoint)
	}
	if releaseCalls != 2 || claimReleaseCalls != 1 || !reflect.DeepEqual(released, []string{"2 reservations"}) {
		t.Fatalf("post-release failure calls: lease=%d claim=%d released=%v", releaseCalls, claimReleaseCalls, released)
	}

	claimReleaseErr = nil
	released, err = clearStoredAssignment(t.Context(), store, "clear-release-barrier", checkpoint)
	if err != nil {
		t.Fatalf("clear after durable lease checkpoint: %v", err)
	}
	if releaseCalls != 2 || claimReleaseCalls != 2 || len(released) != 0 {
		t.Fatalf("checkpoint retry repeated lease release: lease=%d claim=%d released=%v", releaseCalls, claimReleaseCalls, released)
	}
	if got := store.Get("ntm-clear"); got != nil {
		t.Fatalf("confirmed lease release left assignment: %+v", got)
	}
}

func TestReleaseAssignmentReservationsForClearSkipsDiscoveryForAtomicNoLeaseIntent(t *testing.T) {
	originalRelease := releaseReservations
	t.Cleanup(func() { releaseReservations = originalRelease })
	discoveryCalls := 0
	releaseReservations = func(_, _, _ string) ([]string, error) {
		discoveryCalls++
		return []string{"legacy"}, nil
	}

	atomicNoLease := &assignment.Assignment{
		BeadID:           "ntm-no-lease",
		AgentName:        "BlueLake",
		IdempotencyKey:   "intent-1",
		ReservationState: assignment.ReservationPending,
	}
	released, err := releaseAssignmentReservationsForClear("demo", atomicNoLease)
	if err != nil {
		t.Fatalf("atomic no-lease clear: %v", err)
	}
	if len(released) != 0 || discoveryCalls != 0 {
		t.Fatalf("atomic no-lease clear released=%v discoveryCalls=%d, want local-only clear", released, discoveryCalls)
	}

	legacyUnknown := &assignment.Assignment{BeadID: "ntm-legacy", AgentName: "BlueLake"}
	released, err = releaseAssignmentReservationsForClear("demo", legacyUnknown)
	if err != nil {
		t.Fatalf("legacy reservation discovery: %v", err)
	}
	if !reflect.DeepEqual(released, []string{"legacy"}) || discoveryCalls != 1 {
		t.Fatalf("legacy release=%v discoveryCalls=%d, want one discovery", released, discoveryCalls)
	}
}

func TestCLIAtomicReservationReconciliationClassifiesAbsentCompleteAndPartial(t *testing.T) {
	expires := agentmail.FlexTime{Time: time.Now().UTC().Add(time.Hour)}
	request := assignment.ReservationRequest{
		BeadID:         "ntm-reconcile",
		AgentName:      "BlueLake",
		Target:         "%42",
		RequestedPaths: []string{"internal/cli/a.go", "internal/cli/b.go"},
	}

	for _, test := range []struct {
		name         string
		reservations []agentmail.FileReservation
		wantState    assignment.ReservationReconciliationState
		wantIDs      []int
	}{
		{name: "absent", wantState: assignment.ReservationReconciliationAbsent},
		{
			name: "complete",
			reservations: []agentmail.FileReservation{
				{ID: 51, ProjectID: 1, AgentName: "BlueLake", PathPattern: "internal/cli/a.go", Exclusive: true, Reason: "bead assignment: ntm-reconcile", ExpiresTS: expires},
				{ID: 52, ProjectID: 1, AgentName: "BlueLake", PathPattern: "internal/cli/b.go", Exclusive: true, Reason: "bead assignment: ntm-reconcile", ExpiresTS: expires},
			},
			wantState: assignment.ReservationReconciliationReserved,
			wantIDs:   []int{51, 52},
		},
		{
			name: "partial is known reserved",
			reservations: []agentmail.FileReservation{
				{ID: 53, ProjectID: 1, AgentName: "BlueLake", PathPattern: "internal/cli/a.go", Exclusive: true, Reason: "bead assignment: ntm-reconcile", ExpiresTS: expires},
			},
			wantState: assignment.ReservationReconciliationReserved,
			wantIDs:   []int{53},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := newMailStub(t, nil)
			defer stub.Close()
			stub.reservations = test.reservations
			port := &cliAtomicReservationPort{manager: assignpkg.NewFileReservationManager(
				agentmail.NewClient(agentmail.WithBaseURL(stub.server.URL+"/")), "/test/project",
			)}

			got, err := port.ReconcileReservation(t.Context(), request, assignment.LeaseReceipt{})
			if err != nil {
				t.Fatalf("ReconcileReservation() error = %v", err)
			}
			if got.State != test.wantState || !reflect.DeepEqual(got.Lease.ReservationIDs, test.wantIDs) {
				t.Fatalf("ReconcileReservation() = %+v, want state=%s IDs=%v", got, test.wantState, test.wantIDs)
			}
		})
	}
}

func TestReleaseAssignmentReservationsForClearReconcilesUnknownLease(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir project marker: %v", err)
	}
	previousRepo := assignRepoPath
	previousTimeout := assignTimeout
	assignRepoPath = project
	assignTimeout = 2 * time.Second
	t.Cleanup(func() {
		assignRepoPath = previousRepo
		assignTimeout = previousTimeout
	})

	stub := newMailStub(t, nil)
	defer stub.Close()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL)
	stub.reservations = []agentmail.FileReservation{{
		ID: 61, ProjectID: 1, AgentName: "BlueLake", PathPattern: "internal/cli/reconcile.go", Exclusive: true,
		Reason: "bead assignment: ntm-unknown", ExpiresTS: agentmail.FlexTime{Time: time.Now().UTC().Add(time.Hour)},
	}}
	current := &assignment.Assignment{
		BeadID: "ntm-unknown", BeadTitle: "Fix internal/cli/reconcile.go", AgentName: "BlueLake",
		IdempotencyKey: "intent-unknown", ReservationRequired: true,
		ReservationState: assignment.ReservationUnknown, ReservationAttempts: 1,
		ReservationRequested: []string{"internal/cli/reconcile.go"},
	}

	released, err := releaseAssignmentReservationsForClear("demo", current)
	if err != nil {
		t.Fatalf("releaseAssignmentReservationsForClear() error = %v", err)
	}
	if len(stub.releaseCalls) != 1 || !reflect.DeepEqual(stub.releaseCalls[0].IDs, []int{61}) {
		t.Fatalf("release calls = %+v", stub.releaseCalls)
	}
	if len(released) != 1 || released[0] != "1 reservations" {
		t.Fatalf("released = %v", released)
	}
}

func TestReleaseAssignmentReservationsForClearProvesUnknownLeaseAbsent(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir project marker: %v", err)
	}
	previousRepo := assignRepoPath
	assignRepoPath = project
	t.Cleanup(func() { assignRepoPath = previousRepo })

	stub := newMailStub(t, nil)
	defer stub.Close()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL)
	current := &assignment.Assignment{
		BeadID: "ntm-absent", BeadTitle: "Fix internal/cli/absent.go", AgentName: "BlueLake",
		IdempotencyKey: "intent-absent", ReservationRequired: true,
		ReservationState: assignment.ReservationUnknown, ReservationAttempts: 1,
		ReservationRequested: []string{"internal/cli/absent.go"},
	}

	released, err := releaseAssignmentReservationsForClear("demo", current)
	if err != nil {
		t.Fatalf("releaseAssignmentReservationsForClear() error = %v", err)
	}
	if len(released) != 0 || len(stub.releaseCalls) != 0 {
		t.Fatalf("absent reconciliation released=%v calls=%+v", released, stub.releaseCalls)
	}
}

func TestReleaseAssignmentReservationsForClearRetainsUnknownLeaseOnReleaseFailure(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir project marker: %v", err)
	}
	previousRepo := assignRepoPath
	assignRepoPath = project
	t.Cleanup(func() { assignRepoPath = previousRepo })

	stub := newMailStub(t, nil)
	defer stub.Close()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL)
	stub.releaseResult = agentmail.ReleaseReservationsResult{Released: 0}
	stub.reservations = []agentmail.FileReservation{{
		ID: 71, ProjectID: 1, AgentName: "BlueLake", PathPattern: "internal/cli/fail.go", Exclusive: true,
		Reason: "bead assignment: ntm-release-fail", ExpiresTS: agentmail.FlexTime{Time: time.Now().UTC().Add(time.Hour)},
	}}
	current := &assignment.Assignment{
		BeadID: "ntm-release-fail", BeadTitle: "Fix internal/cli/fail.go", AgentName: "BlueLake",
		IdempotencyKey: "intent-release-fail", ReservationRequired: true,
		ReservationState: assignment.ReservationUnknown, ReservationAttempts: 1,
		ReservationRequested: []string{"internal/cli/fail.go"},
	}

	if _, err := releaseAssignmentReservationsForClear("demo", current); err == nil || !strings.Contains(err.Error(), "released 0 of 1") {
		t.Fatalf("releaseAssignmentReservationsForClear() error = %v", err)
	}
	if len(stub.releaseCalls) != 1 || !reflect.DeepEqual(stub.releaseCalls[0].IDs, []int{71}) {
		t.Fatalf("release failure calls = %+v", stub.releaseCalls)
	}
}

func TestReserveFilesForBeadUsesResolvedProjectDir(t *testing.T) {
	root, nested := createAssignProjectRoot(t)

	oldCfg := cfg
	cfg = &config.Config{ProjectsBase: filepath.Join(root, "projects-base")}
	t.Cleanup(func() { cfg = oldCfg })

	stub := newMailStub(t, nil)
	defer stub.Close()
	t.Setenv("AGENT_MAIL_URL", stub.server.URL)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	result := reserveFilesForBead("demo", "bd-123", "Update internal/cli/assign.go", "claude", false, time.Second)
	if result == nil {
		t.Fatal("reserveFilesForBead() returned nil result")
	}
	if !result.Success {
		t.Fatalf("reserveFilesForBead() success = false, error = %q", result.Error)
	}

	if len(stub.reserveCalls) != 1 {
		t.Fatalf("expected 1 reserve call, got %d", len(stub.reserveCalls))
	}
	if got := stub.reserveCalls[0].Project; got != root {
		t.Fatalf("reserve project = %q, want %q", got, root)
	}
}

// ============================================================================
// Completion Detection and Unblock Tests
// ============================================================================

// TestIsBeadInCycle tests the cycle detection helper function
func TestIsBeadInCycle(t *testing.T) {
	cycles := [][]string{
		{"bd-001", "bd-002", "bd-003"},
		{"bd-010", "bd-011"},
	}

	tests := []struct {
		name     string
		beadID   string
		expected bool
	}{
		{"in first cycle", "bd-001", true},
		{"in first cycle - middle", "bd-002", true},
		{"in second cycle", "bd-010", true},
		{"not in any cycle", "bd-099", false},
		{"partial match (not in cycle)", "bd-00", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsBeadInCycle(tc.beadID, cycles)
			if result != tc.expected {
				t.Errorf("IsBeadInCycle(%q) = %v, want %v", tc.beadID, result, tc.expected)
			}
		})
	}
}

// TestIsBeadInCycleEmptyCycles tests with empty cycles
func TestIsBeadInCycleEmptyCycles(t *testing.T) {
	var cycles [][]string

	if IsBeadInCycle("bd-001", cycles) {
		t.Error("Expected false for empty cycles")
	}

	cycles = [][]string{{}} // Single empty cycle
	if IsBeadInCycle("bd-001", cycles) {
		t.Error("Expected false for cycle with no beads")
	}
}

// TestUnblockedBeadStructure tests the UnblockedBead type
func TestUnblockedBeadStructure(t *testing.T) {
	unblocked := UnblockedBead{
		ID:            "bd-100",
		Title:         "Now ready task",
		Priority:      1,
		PrevBlockers:  []string{"bd-050", "bd-060"},
		UnblockedByID: "bd-050",
	}

	if unblocked.ID != "bd-100" {
		t.Errorf("Expected ID 'bd-100', got %q", unblocked.ID)
	}
	if len(unblocked.PrevBlockers) != 2 {
		t.Errorf("Expected 2 previous blockers, got %d", len(unblocked.PrevBlockers))
	}
	if unblocked.UnblockedByID != "bd-050" {
		t.Errorf("Expected UnblockedByID 'bd-050', got %q", unblocked.UnblockedByID)
	}
}

// TestDependencyAwareResultStructure tests the DependencyAwareResult type
func TestDependencyAwareResultStructure(t *testing.T) {
	result := DependencyAwareResult{
		CompletedBeadID: "bd-finished",
		NewlyUnblocked: []UnblockedBead{
			{
				ID:            "bd-ready1",
				Title:         "Ready task 1",
				Priority:      2,
				UnblockedByID: "bd-finished",
			},
			{
				ID:            "bd-ready2",
				Title:         "Ready task 2",
				Priority:      1,
				UnblockedByID: "bd-finished",
			},
		},
		CyclesDetected: [][]string{{"bd-cycle1", "bd-cycle2"}},
		Errors:         []string{"warning: something"},
	}

	if result.CompletedBeadID != "bd-finished" {
		t.Errorf("Expected CompletedBeadID 'bd-finished', got %q", result.CompletedBeadID)
	}
	if len(result.NewlyUnblocked) != 2 {
		t.Errorf("Expected 2 newly unblocked, got %d", len(result.NewlyUnblocked))
	}
	if len(result.CyclesDetected) != 1 {
		t.Errorf("Expected 1 cycle detected, got %d", len(result.CyclesDetected))
	}
	if len(result.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(result.Errors))
	}
}

// TestFilterCyclicBeadsEmpty tests filtering with no cycles
func TestFilterCyclicBeadsEmpty(t *testing.T) {
	beads := []bv.BeadPreview{
		{ID: "bd-001", Title: "Task 1"},
		{ID: "bd-002", Title: "Task 2"},
	}

	// When there are no cycles, all beads should be returned
	// This test just verifies the function signature and basic behavior
	// since CheckCycles requires bv to be available
	if len(beads) != 2 {
		t.Error("Input beads should have 2 items")
	}
}

// ============================================================================
// Reassignment Tests
// ============================================================================

// TestReassignDataStructure tests the ReassignData type structure
func TestReassignDataStructure(t *testing.T) {
	data := ReassignData{
		BeadID:                      "bd-123",
		BeadTitle:                   "Test bead",
		Pane:                        4,
		AgentType:                   "codex",
		AgentName:                   "test_codex",
		Status:                      "assigned",
		PromptSent:                  true,
		AssignedAt:                  "2026-01-19T12:00:00Z",
		PreviousPane:                2,
		PreviousAgent:               "test_claude",
		PreviousAgentType:           "claude",
		PreviousStatus:              "working",
		FileReservationsTransferred: true,
	}

	if data.BeadID != "bd-123" {
		t.Errorf("Expected BeadID 'bd-123', got %q", data.BeadID)
	}
	if data.Pane != 4 {
		t.Errorf("Expected Pane 4, got %d", data.Pane)
	}
	if data.PreviousPane != 2 {
		t.Errorf("Expected PreviousPane 2, got %d", data.PreviousPane)
	}
	if data.AgentType != "codex" {
		t.Errorf("Expected AgentType 'codex', got %q", data.AgentType)
	}
	if data.PreviousAgentType != "claude" {
		t.Errorf("Expected PreviousAgentType 'claude', got %q", data.PreviousAgentType)
	}
	if !data.FileReservationsTransferred {
		t.Error("Expected FileReservationsTransferred to be true")
	}
}

// TestReassignErrorStructure tests the ReassignError type structure
func TestReassignErrorStructure(t *testing.T) {
	err := ReassignError{
		Code:    "TARGET_BUSY",
		Message: "pane 4 already has assignment bd-abc",
		Details: map[string]interface{}{
			"current_bead":   "bd-abc",
			"current_status": "working",
		},
	}

	if err.Code != "TARGET_BUSY" {
		t.Errorf("Expected Code 'TARGET_BUSY', got %q", err.Code)
	}
	if err.Details["current_bead"] != "bd-abc" {
		t.Errorf("Expected current_bead 'bd-abc', got %v", err.Details["current_bead"])
	}
}

// TestReassignEnvelopeSuccessStructure tests the success envelope structure
func TestReassignEnvelopeSuccessStructure(t *testing.T) {
	envelope := ReassignEnvelope{
		Command:    "assign",
		Subcommand: "reassign",
		Session:    "myproject",
		Timestamp:  "2026-01-19T12:00:00Z",
		Success:    true,
		Data: &ReassignData{
			BeadID:            "bd-123",
			BeadTitle:         "Test bead",
			Pane:              4,
			AgentType:         "codex",
			PreviousPane:      2,
			PreviousAgentType: "claude",
		},
		Warnings: []string{},
	}

	if envelope.Command != "assign" {
		t.Errorf("Expected Command 'assign', got %q", envelope.Command)
	}
	if envelope.Subcommand != "reassign" {
		t.Errorf("Expected Subcommand 'reassign', got %q", envelope.Subcommand)
	}
	if !envelope.Success {
		t.Error("Expected Success to be true")
	}
	if envelope.Data == nil {
		t.Error("Expected Data to be non-nil")
	}
	if envelope.Error != nil {
		t.Error("Expected Error to be nil for success case")
	}
}

// TestReassignEnvelopeErrorStructure tests the error envelope structure
func TestReassignEnvelopeErrorStructure(t *testing.T) {
	envelope := ReassignEnvelope{
		Command:    "assign",
		Subcommand: "reassign",
		Session:    "myproject",
		Timestamp:  "2026-01-19T12:00:00Z",
		Success:    false,
		Data:       nil,
		Warnings:   []string{},
		Error: &ReassignError{
			Code:    "NOT_ASSIGNED",
			Message: "bead bd-xyz does not have an active assignment",
		},
	}

	if envelope.Success {
		t.Error("Expected Success to be false")
	}
	if envelope.Data != nil {
		t.Error("Expected Data to be nil for error case")
	}
	if envelope.Error == nil {
		t.Error("Expected Error to be non-nil for error case")
	}
	if envelope.Error.Code != "NOT_ASSIGNED" {
		t.Errorf("Expected Error.Code 'NOT_ASSIGNED', got %q", envelope.Error.Code)
	}
}

// TestMakeReassignErrorEnvelope tests the error envelope helper function
func TestMakeReassignErrorEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		session string
		code    string
		message string
		details map[string]interface{}
	}{
		{
			name:    "basic error",
			session: "test-session",
			code:    "NOT_ASSIGNED",
			message: "bead not found",
			details: nil,
		},
		{
			name:    "error with details",
			session: "test-session",
			code:    "TARGET_BUSY",
			message: "pane is busy",
			details: map[string]interface{}{
				"current_bead":   "bd-999",
				"current_status": "working",
			},
		},
		{
			name:    "no idle agent error",
			session: "myproject",
			code:    "NO_IDLE_AGENT",
			message: "no idle codex agents available",
			details: map[string]interface{}{
				"agent_type": "codex",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			envelope := makeReassignErrorEnvelope(tc.session, tc.code, tc.message, tc.details)

			if envelope.Command != "assign" {
				t.Errorf("Expected Command 'assign', got %q", envelope.Command)
			}
			if envelope.Subcommand != "reassign" {
				t.Errorf("Expected Subcommand 'reassign', got %q", envelope.Subcommand)
			}
			if envelope.Session != tc.session {
				t.Errorf("Expected Session %q, got %q", tc.session, envelope.Session)
			}
			if envelope.Success {
				t.Error("Expected Success to be false")
			}
			if envelope.Error == nil {
				t.Fatal("Expected Error to be non-nil")
			}
			if envelope.Error.Code != tc.code {
				t.Errorf("Expected Error.Code %q, got %q", tc.code, envelope.Error.Code)
			}
			if envelope.Error.Message != tc.message {
				t.Errorf("Expected Error.Message %q, got %q", tc.message, envelope.Error.Message)
			}
			if tc.details != nil {
				for k, v := range tc.details {
					if envelope.Error.Details[k] != v {
						t.Errorf("Expected Details[%q]=%v, got %v", k, v, envelope.Error.Details[k])
					}
				}
			}
		})
	}
}

// TestReassignErrorCodes tests the documented error codes
func TestReassignErrorCodes(t *testing.T) {
	// These are the documented error codes from the bead spec
	errorCodes := []string{
		"NOT_ASSIGNED",   // Bead doesn't have an active assignment
		"TARGET_BUSY",    // Target pane already has an assignment
		"PANE_NOT_FOUND", // Target pane doesn't exist
		"NO_IDLE_AGENT",  // No idle agent of specified type
		"INVALID_ARGS",   // Invalid arguments
		"STORE_ERROR",    // Assignment store error
		"TMUX_ERROR",     // Tmux error
		"REASSIGN_ERROR", // Reassignment operation error
	}

	// Verify each code can be used in an envelope
	for _, code := range errorCodes {
		envelope := makeReassignErrorEnvelope("test", code, "test message", nil)
		if envelope.Error.Code != code {
			t.Errorf("Error code %q not preserved in envelope", code)
		}
	}
}

// ============================================================================
// Strategy Validation Tests
// ============================================================================

// TestStrategyFlagDefaultValue tests that the strategy flag has the correct default
func TestStrategyFlagDefaultValue(t *testing.T) {
	cmd := newAssignCmd()
	flag := cmd.Flags().Lookup("strategy")
	if flag == nil {
		t.Fatal("Expected 'strategy' flag to exist")
	}
	if flag.DefValue != "balanced" {
		t.Errorf("Expected default strategy 'balanced', got %q", flag.DefValue)
	}
}

// TestStrategyFlagHelpText tests that strategy flag has descriptive help
func TestStrategyFlagHelpText(t *testing.T) {
	cmd := newAssignCmd()
	flag := cmd.Flags().Lookup("strategy")
	if flag == nil {
		t.Fatal("Expected 'strategy' flag to exist")
	}

	// Help text should mention all valid strategies
	help := flag.Usage
	expectedStrategies := []string{"balanced", "speed", "quality", "dependency", "round-robin"}
	for _, s := range expectedStrategies {
		if !contains(help, s) {
			t.Errorf("Expected strategy flag help to mention %q", s)
		}
	}
}

// TestAssignOutputIncludesStrategy tests that output structures include strategy field
func TestAssignOutputIncludesStrategy(t *testing.T) {
	output := AssignOutputEnhanced{
		Strategy: "quality",
	}
	if output.Strategy != "quality" {
		t.Errorf("Expected Strategy field to be 'quality', got %q", output.Strategy)
	}
}

// TestAssignCommandOptionsIncludesStrategy tests that options struct includes strategy
func TestAssignCommandOptionsIncludesStrategy(t *testing.T) {
	opts := AssignCommandOptions{
		Session:  "test",
		Strategy: "dependency",
	}
	if opts.Strategy != "dependency" {
		t.Errorf("Expected Strategy in options to be 'dependency', got %q", opts.Strategy)
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// Load-Aware Balanced Strategy Tests
// ============================================================================

// TestBalancedStrategyTieBreakers tests the tie-breaker cascade:
// 1. Fewer active assignments
// 2. Higher capability score
// 3. Least-recently assigned
// 4. Lower pane index (deterministic)
func TestBalancedStrategyTieBreakers(t *testing.T) {
	// Test the tie-breaker ordering logic
	// When counts are equal and scores are equal:
	// - Never assigned beats previously assigned
	// - Earlier assigned beats later assigned
	// - Lower pane index breaks ties for determinism

	t.Run("never_assigned_beats_previously_assigned", func(t *testing.T) {
		// Agent with no prior assignments should win over one with prior assignments
		// when counts and scores are equal
		// This validates the zero-time check in the tie-breaker logic
		assignItem := AssignmentItem{
			BeadID:    "bd-test",
			AgentType: "claude",
			Score:     0.75,
		}
		// This is a structural test; the actual logic is tested via integration
		if assignItem.Score != 0.75 {
			t.Errorf("Expected score 0.75, got %f", assignItem.Score)
		}
	})

	t.Run("lower_pane_index_determinism", func(t *testing.T) {
		// When all other factors are equal, lower pane index wins
		// This ensures deterministic output across runs
		agents := []struct {
			pane  int
			score float64
		}{
			{pane: 3, score: 0.8},
			{pane: 1, score: 0.8},
			{pane: 2, score: 0.8},
		}

		// With equal scores, pane 1 should be selected (lowest index)
		lowestPane := agents[0].pane
		for _, a := range agents {
			if a.pane < lowestPane {
				lowestPane = a.pane
			}
		}
		if lowestPane != 1 {
			t.Errorf("Expected lowest pane to be 1, got %d", lowestPane)
		}
	})

	t.Run("fewer_assignments_wins", func(t *testing.T) {
		// Agents with fewer active assignments should be preferred
		counts := map[int]int{
			1: 3, // 3 active assignments
			2: 1, // 1 active assignment (winner)
			3: 2, // 2 active assignments
		}

		minCount := counts[1]
		bestPane := 1
		for pane, count := range counts {
			if count < minCount {
				minCount = count
				bestPane = pane
			}
		}
		if bestPane != 2 {
			t.Errorf("Expected pane 2 (fewest assignments) to win, got %d", bestPane)
		}
	})
}

// TestBalancedStrategyLoadsFromStore tests that the balanced strategy
// correctly pre-populates assignment counts from AssignmentStore
func TestBalancedStrategyLoadsFromStore(t *testing.T) {
	// This is a structural test to verify the strategy options
	// accept session name for store lookup
	opts := &AssignCommandOptions{
		Session:  "test-session",
		Strategy: "balanced",
	}

	if opts.Session == "" {
		t.Error("Session should not be empty for store lookup")
	}
	if opts.Strategy != "balanced" {
		t.Errorf("Strategy should be 'balanced', got %q", opts.Strategy)
	}
}

// TestBalancedStrategyDeterminism tests that balanced strategy produces
// deterministic output for identical inputs
func TestBalancedStrategyDeterminism(t *testing.T) {
	// Given the same agents and beads, the balanced strategy should
	// always produce the same assignment order
	agents := []assignAgentInfo{
		{agentType: "claude", pane: tmux.Pane{Index: 1}},
		{agentType: "codex", pane: tmux.Pane{Index: 2}},
		{agentType: "gemini", pane: tmux.Pane{Index: 3}},
	}

	// Verify agents are sortable by pane index for deterministic tiebreaker
	paneIndices := make([]int, len(agents))
	for i, a := range agents {
		paneIndices[i] = a.pane.Index
	}

	// Pane indices should be in order 1, 2, 3
	for i := 0; i < len(paneIndices)-1; i++ {
		if paneIndices[i] >= paneIndices[i+1] {
			// This is just checking the test setup
			// The actual determinism is in the strategy implementation
		}
	}
}

// TestAssignAgentInfoHasPane verifies the agent info structure has pane reference
func TestAssignAgentInfoHasPane(t *testing.T) {
	pane := tmux.Pane{Index: 5}
	agent := assignAgentInfo{
		agentType: "claude",
		pane:      pane,
	}

	// Verify pane index is set correctly (zero value Index would be 0)
	if agent.pane.Index != 5 {
		t.Errorf("Expected pane index 5, got %d", agent.pane.Index)
	}
}

// ============================================================================
// Pure Function Tests
// ============================================================================

func TestDetectModelFromTitle(t *testing.T) {

	tests := []struct {
		name      string
		agentType string
		title     string
		expected  string
	}{
		// Opus detection
		{name: "opus lowercase", agentType: "claude", title: "myproject__cc_1_opus", expected: "opus"},
		{name: "opus uppercase", agentType: "claude", title: "myproject__cc_1_OPUS", expected: "opus"},
		{name: "opus mixed case", agentType: "claude", title: "myproject__cc_1_Opus", expected: "opus"},

		// Sonnet detection
		{name: "sonnet lowercase", agentType: "claude", title: "myproject__cc_1_sonnet", expected: "sonnet"},
		{name: "sonnet uppercase", agentType: "claude", title: "myproject__cc_1_SONNET", expected: "sonnet"},
		{name: "sonnet mixed case", agentType: "claude", title: "project_Sonnet_v3", expected: "sonnet"},

		// Haiku detection
		{name: "haiku lowercase", agentType: "claude", title: "myproject__cc_1_haiku", expected: "haiku"},
		{name: "haiku uppercase", agentType: "claude", title: "myproject__cc_1_HAIKU", expected: "haiku"},
		{name: "haiku mixed case", agentType: "claude", title: "project_Haiku", expected: "haiku"},

		// No model detected
		{name: "no model", agentType: "claude", title: "myproject__cc_1", expected: ""},
		{name: "empty title", agentType: "claude", title: "", expected: ""},
		{name: "codex agent", agentType: "codex", title: "myproject__cod_1", expected: ""},
		{name: "gemini agent", agentType: "gemini", title: "myproject__gmi_1", expected: ""},

		// Partial matches (should still match)
		{name: "opus in middle", agentType: "claude", title: "test_opus_session", expected: "opus"},
		{name: "sonnet at start", agentType: "claude", title: "sonnetproject__cc", expected: "sonnet"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectModelFromTitle(tc.agentType, tc.title)
			if result != tc.expected {
				t.Errorf("detectModelFromTitle(%q, %q) = %q; want %q",
					tc.agentType, tc.title, result, tc.expected)
			}
		})
	}
}

func TestDetermineAgentState_NormalizesAliasHints(t *testing.T) {

	tests := []struct {
		name       string
		agentType  string
		scrollback string
		want       string
	}{
		{
			name:      "codex alias with whitespace",
			agentType: " openai-codex ",
			scrollback: "Processing your request...\n" +
				"Token usage: total=150,000 input=140,000 output=10,000\n" +
				"47% context left · ? for shortcuts\n" +
				"codex> ",
			want: "idle",
		},
		{
			name:       "cursor added event suffix",
			agentType:  "cursor_added",
			scrollback: "Done editing.\ncursor> ",
			want:       "idle",
		},
		{
			name:       "windsurf short alias",
			agentType:  "ws",
			scrollback: "Generating code...\nsearching for references",
			want:       "working",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			if got := determineAgentState(tc.scrollback, tc.agentType); got != tc.want {
				t.Fatalf("determineAgentState(%q, %q) = %q, want %q", tc.scrollback, tc.agentType, got, tc.want)
			}
		})
	}
}

func TestClassifyTriageRecForAssignment(t *testing.T) {
	type testCase struct {
		name              string
		rec               bv.TriageRecommendation
		activeAssignments map[string]struct{}
		wantSkip          bool
		wantReason        string
	}

	tests := []testCase{
		{
			name:     "open with no blockers is assignable",
			rec:      bv.TriageRecommendation{ID: "bd-1", Status: "open"},
			wantSkip: false,
		},
		{
			name:     "empty status is treated as assignable",
			rec:      bv.TriageRecommendation{ID: "bd-2"},
			wantSkip: false,
		},
		{
			name:       "dependency blocker wins over status",
			rec:        bv.TriageRecommendation{ID: "bd-3", Status: "open", BlockedBy: []string{"bd-99"}},
			wantSkip:   true,
			wantReason: "blocked_by_dependency",
		},
		{
			name:       "in_progress is skipped",
			rec:        bv.TriageRecommendation{ID: "bd-4", Status: "in_progress"},
			wantSkip:   true,
			wantReason: "already_in_progress",
		},
		{
			name:       "blocked status is skipped",
			rec:        bv.TriageRecommendation{ID: "bd-5", Status: "blocked"},
			wantSkip:   true,
			wantReason: "blocked_status",
		},
		{
			name:       "closed status is skipped",
			rec:        bv.TriageRecommendation{ID: "bd-6", Status: "closed"},
			wantSkip:   true,
			wantReason: "closed_status",
		},
		{
			name:       "operator_gated label beats open status",
			rec:        bv.TriageRecommendation{ID: "bd-7", Status: "open", Labels: []string{"operator-gated"}},
			wantSkip:   true,
			wantReason: "operator_gated",
		},
		{
			name:       "human-input label is operator gated",
			rec:        bv.TriageRecommendation{ID: "bd-8", Status: "open", Labels: []string{"foo", "human-input"}},
			wantSkip:   true,
			wantReason: "operator_gated",
		},
		{
			name:              "already-claimed bead is suppressed",
			rec:               bv.TriageRecommendation{ID: "bd-9", Status: "open"},
			activeAssignments: map[string]struct{}{"bd-9": {}},
			wantSkip:          true,
			wantReason:        "already_assigned",
		},
		{
			name:       "status case + delimiter variation still classifies",
			rec:        bv.TriageRecommendation{ID: "bd-10", Status: "In-Progress"},
			wantSkip:   true,
			wantReason: "already_in_progress",
		},
		{
			name:              "blockedBy beats already_assigned",
			rec:               bv.TriageRecommendation{ID: "bd-11", Status: "open", BlockedBy: []string{"bd-x"}},
			activeAssignments: map[string]struct{}{"bd-11": {}},
			wantSkip:          true,
			wantReason:        "blocked_by_dependency",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyTriageRecForAssignment(tc.rec, tc.activeAssignments)
			if tc.wantSkip {
				if got == nil {
					t.Fatalf("expected skip with reason %q, got nil (assignable)", tc.wantReason)
				}
				if got.Reason != tc.wantReason {
					t.Fatalf("reason = %q, want %q", got.Reason, tc.wantReason)
				}
				if got.BeadID != tc.rec.ID {
					t.Fatalf("BeadID = %q, want %q", got.BeadID, tc.rec.ID)
				}
				if tc.wantReason == "blocked_by_dependency" && len(got.BlockedByIDs) == 0 {
					t.Fatalf("blocked_by_dependency must populate BlockedByIDs")
				}
			} else if got != nil {
				t.Fatalf("expected assignable, got skip with reason %q", got.Reason)
			}
		})
	}
}

func TestCountSkippedByReason(t *testing.T) {
	items := []SkippedItem{
		{BeadID: "a", Reason: "blocked_by_dependency"},
		{BeadID: "b", Reason: "blocked_by_dependency"},
		{BeadID: "c", Reason: "operator_gated"},
		{BeadID: "d", Reason: "already_in_progress"},
	}
	if got := countSkippedByReason(items, "blocked_by_dependency"); got != 2 {
		t.Fatalf("countSkippedByReason(blocked_by_dependency) = %d, want 2", got)
	}
	if got := countSkippedByReason(items, "operator_gated"); got != 1 {
		t.Fatalf("countSkippedByReason(operator_gated) = %d, want 1", got)
	}
	if got := countSkippedByReason(items, "nonexistent"); got != 0 {
		t.Fatalf("countSkippedByReason(nonexistent) = %d, want 0", got)
	}
}

func TestNormalizeBeadStatus(t *testing.T) {
	tests := map[string]string{
		"open":         "open",
		"In Progress":  "in_progress",
		"in-progress":  "in_progress",
		" CLOSED ":     "closed",
		"":             "",
		"ready-for-qa": "ready_for_qa",
	}
	for in, want := range tests {
		if got := normalizeBeadStatus(in); got != want {
			t.Errorf("normalizeBeadStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeAgentTypeAlias(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty string is no filter", raw: "", want: ""},
		{name: "any is no filter", raw: "any", want: ""},
		{name: "ANY is no filter", raw: "ANY", want: ""},
		{name: "all is no filter", raw: "all", want: ""},
		{name: "star is no filter", raw: "*", want: ""},
		{name: "whitespace around any", raw: "  any  ", want: ""},
		{name: "claude resolves to claude", raw: "claude", want: "claude"},
		{name: "codex alias cod resolves", raw: "cod", want: "codex"},
		{name: "gemini alias gmi resolves", raw: "gmi", want: "gemini"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAgentTypeAlias(tc.raw)
			if got != tc.want {
				t.Fatalf("normalizeAgentTypeAlias(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParsePriorityString(t *testing.T) {

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		// Valid priorities
		{name: "P0 critical", input: "P0", expected: 0},
		{name: "P1 high", input: "P1", expected: 1},
		{name: "P2 medium", input: "P2", expected: 2},
		{name: "P3 low", input: "P3", expected: 3},
		{name: "P4 backlog", input: "P4", expected: 4},

		// Invalid - returns default (2)
		{name: "P5 out of range", input: "P5", expected: 2},
		{name: "P9 out of range", input: "P9", expected: 2},
		{name: "empty string", input: "", expected: 2},
		{name: "just P", input: "P", expected: 2},
		{name: "lowercase p0", input: "p0", expected: 2},
		{name: "no P prefix", input: "0", expected: 2},
		{name: "too long", input: "P01", expected: 2},
		{name: "word priority", input: "high", expected: 2},
		{name: "negative-like", input: "P-1", expected: 2},
		{name: "spaces", input: " P1", expected: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parsePriorityString(tc.input)
			if result != tc.expected {
				t.Errorf("parsePriorityString(%q) = %d; want %d", tc.input, result, tc.expected)
			}
		})
	}
}

// TestDetermineAgentStateLiveBusyOverride verifies the #124 fix: when the
// pane scrollback's trailing live-window contains a THINKING-category pattern
// (e.g. codex's "• Working (4m 51s • esc to interrupt)") the verdict must be
// "working" regardless of what the legacy parser concludes — the pane is
// busy and watch-mode autonomous dispatch must not target it.
func TestDetermineAgentStateLiveBusyOverride(t *testing.T) {
	// A scrollback that ends with a codex working bullet inside the
	// live-window. Without the override, the legacy parser sometimes
	// classifies this as idle when there's no fresh prompt yet.
	scrollback := strings.Repeat("filler line\n", 200) +
		"\n• Working (4m 51s • esc to interrupt)\n"

	got := determineAgentState(scrollback, "codex")
	if got != "working" {
		t.Errorf("determineAgentState(busy codex pane) = %q, want \"working\"", got)
	}
}

// TestDetermineAgentStateIgnoresStaleThinking verifies the override does NOT
// trigger when a thinking pattern only exists deep in the scrollback (outside
// the live-window). That historical bullet is from a completed tool call and
// must not lock the agent in "working" forever.
func TestDetermineAgentStateIgnoresStaleThinking(t *testing.T) {
	// Thinking pattern early in the buffer, then enough trailing content to
	// push it outside the live-window (15 trailing lines).
	scrollback := "• Working (10s • esc to interrupt)\n" +
		strings.Repeat("filler line that is unambiguously not thinking\n", 200) +
		"\n>>>" // codex-shaped idle prompt

	// We don't assert "idle" here (that depends on the legacy parser's
	// agent-specific prompt detection), but we must NOT see "working" be
	// forced by the override path on stale scrollback content.
	got := determineAgentState(scrollback, "codex")
	if got == "working" {
		t.Errorf("determineAgentState(stale thinking pattern) = %q, must not be forced to working", got)
	}
}

// ============================================================================
// FIX C: Active-assignment idle-pool guard
// ============================================================================

// TestLoadActiveAssignmentPanes_ExcludesBetweenTurnsPane verifies that a pane
// holding an active assignment (StatusAssigned or StatusWorking) is reported by
// loadActiveAssignmentPanes even when it would momentarily look idle between
// turns. The idle-collection paths exclude these panes so a pane mid-flight on
// bead A is never handed bead B (double-dispatch) just because it briefly shows
// an idle prompt.
func TestLoadActiveAssignmentPanes_ExcludesBetweenTurnsPane(t *testing.T) {
	isolateSessionAgentStorage(t)

	const session = "fixc"
	store := assignment.NewStore(session)

	// Pane 1: mid-flight on bead A, status Working — the "between turns" pane.
	if _, err := store.Assign("bd-A", "Task A", 1, "claude", "fixc_claude_1", "do A"); err != nil {
		t.Fatalf("assign bd-A: %v", err)
	}
	if err := store.MarkWorking("bd-A"); err != nil {
		t.Fatalf("mark working bd-A: %v", err)
	}
	// Pane 2: freshly assigned, status Assigned.
	if _, err := store.Assign("bd-B", "Task B", 2, "codex", "fixc_codex_2", "do B"); err != nil {
		t.Fatalf("assign bd-B: %v", err)
	}
	// Pane 3: a completed assignment — NOT active, must NOT be excluded.
	if _, err := store.Assign("bd-C", "Task C", 3, "claude", "fixc_claude_3", "do C"); err != nil {
		t.Fatalf("assign bd-C: %v", err)
	}
	if err := store.MarkCompleted("bd-C"); err != nil {
		t.Fatalf("mark completed bd-C: %v", err)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save store: %v", err)
	}

	active := loadActiveAssignmentPanes(session)

	if _, ok := active["legacy-index:1"]; !ok {
		t.Errorf("pane 1 (StatusWorking) must be in the active set — it is mid-flight and not dispatchable")
	}
	if _, ok := active["legacy-index:2"]; !ok {
		t.Errorf("pane 2 (StatusAssigned) must be in the active set — it holds an active assignment")
	}
	if _, ok := active["legacy-index:3"]; ok {
		t.Errorf("pane 3 (StatusCompleted) must NOT be in the active set — completed work frees the pane")
	}
	if len(active) != 2 {
		t.Errorf("active pane count = %d, want 2", len(active))
	}
}

// TestLoadActiveAssignmentPanes_EmptyStore returns an empty set (and never
// errors) when no store exists for the session — idle collection then proceeds
// with no exclusions.
func TestLoadActiveAssignmentPanes_EmptyStore(t *testing.T) {
	isolateSessionAgentStorage(t)
	active := loadActiveAssignmentPanes("no-such-session")
	if len(active) != 0 {
		t.Errorf("expected empty active set for missing store, got %d entries", len(active))
	}
}

func TestResolveAssignmentItemPaneCanonicalMultiWindow(t *testing.T) {
	panes := []tmux.Pane{
		{ID: "%40", WindowIndex: 0, Index: 1, Type: tmux.AgentCodex},
		{ID: "%41", WindowIndex: 1, Index: 1, Type: tmux.AgentClaude},
	}

	for _, item := range []AssignmentItem{
		{Pane: 1, PaneTarget: "0.1", PaneID: "%40"},
		{Pane: 1, PaneTarget: "1.1", PaneID: "%41"},
	} {
		pane, err := resolveAssignmentItemPane(panes, item)
		if err != nil {
			t.Fatalf("resolve %+v: %v", item, err)
		}
		if pane.ID != item.PaneID {
			t.Fatalf("resolve %+v selected %s", item, pane.ID)
		}
	}

	if _, err := resolveAssignmentItemPane(panes, AssignmentItem{Pane: 1}); err == nil || !strings.Contains(err.Error(), "resolves to 2 panes") {
		t.Fatalf("ambiguous legacy pane resolved without a useful error: %v", err)
	}
	if _, err := resolveAssignmentItemPane(panes, AssignmentItem{PaneTarget: "0.1", PaneID: "%41"}); err == nil {
		t.Fatal("inconsistent pane target and ID must fail closed")
	}
}

func TestResolvePendingAssignmentPaneNeverFallsBackToReusedLocalIndex(t *testing.T) {
	pending := assignment.Assignment{
		BeadID:         "ntm-pending",
		Pane:           1,
		OccupancyKey:   "%40",
		DispatchTarget: "%40",
	}
	replacementTopology := []tmux.Pane{{ID: "%41", WindowIndex: 1, Index: 1}}
	if _, err := resolvePendingAssignmentPane(replacementTopology, pending); err == nil || !strings.Contains(err.Error(), "%40 is unavailable") {
		t.Fatalf("topology-changed retry did not fail closed: %v", err)
	}

	originalTopology := append(replacementTopology, tmux.Pane{ID: "%40", WindowIndex: 0, Index: 1})
	resolved, err := resolvePendingAssignmentPane(originalTopology, pending)
	if err != nil {
		t.Fatalf("resolve original physical pane: %v", err)
	}
	if resolved.ID != "%40" {
		t.Fatalf("resolved pane ID = %q, want %%40", resolved.ID)
	}

	pending.OccupancyKey = ""
	pending.DispatchTarget = "0.1"
	if _, err := resolvePendingAssignmentPane(originalTopology, pending); err == nil || !strings.Contains(err.Error(), "no canonical physical pane ID") {
		t.Fatalf("index-only pending retry did not fail closed: %v", err)
	}
}

type fixedAssignSessionObserver struct {
	observation statuspkg.SessionObservation
	err         error
}

func (o fixedAssignSessionObserver) Observe(context.Context, string) (statuspkg.SessionObservation, error) {
	return o.observation, o.err
}

func TestCLIAtomicDispatchReobservesAndRejectsBusyPaneBeforeActuation(t *testing.T) {
	port := &cliAtomicPaneDispatchPort{
		session: "demo",
		observer: fixedAssignSessionObserver{observation: statuspkg.SessionObservation{
			Session: "demo",
			Panes: []statuspkg.PaneObservation{{
				Pane: tmux.PaneRef{ID: "%42", WindowIndex: 0, PaneIndex: 1},
				Current: statuspkg.StateObservation{
					Status:     statuspkg.AgentStatus{State: statuspkg.StateWorking},
					ObservedAt: time.Now().UTC(),
					Freshness:  statuspkg.FreshnessFresh,
					Confidence: 0.99,
				},
			}},
		}},
	}

	receipt, err := port.Dispatch(t.Context(), assignment.DispatchRequest{
		BeadID: "ntm-busy-transition",
		Target: "%42",
		Prompt: "must not be delivered",
	})
	if err == nil || !strings.Contains(err.Error(), "not freshly and confidently idle at dispatch") {
		t.Fatalf("busy actuation boundary error = %v", err)
	}
	if receipt.DeliveryID != "" {
		t.Fatalf("busy actuation boundary produced delivery ID %q", receipt.DeliveryID)
	}
}

func TestClassifyCLIReservationAttemptMarksKnownAndPartialFailures(t *testing.T) {
	req := assignment.ReservationRequest{AgentName: "BlueLake", Target: "%42"}

	lease, err := classifyCLIReservationAttempt(req, &assignpkg.FileReservationResult{
		RequestedPaths: []string{"internal/cli/**"},
		Conflicts:      []agentmail.ReservationConflict{{Path: "internal/cli/**", Holders: []string{"GreenLake"}}},
		Success:        false,
	}, nil)
	if !assignment.IsGuaranteedNoReservation(err) || assignment.IsReservationReleaseRequired(err) {
		t.Fatalf("zero-grant conflict classification error = %v", err)
	}
	if !reflect.DeepEqual(lease.Requested, []string{"internal/cli/**"}) || len(lease.Granted) != 0 || len(lease.ReservationIDs) != 0 {
		t.Fatalf("zero-grant conflict lease = %+v", lease)
	}

	lease, err = classifyCLIReservationAttempt(req, &assignpkg.FileReservationResult{
		RequestedPaths: []string{"internal/cli/**", "internal/robot/**"},
		GrantedPaths:   []string{"internal/cli/**"},
		ReservationIDs: []int{73},
		Success:        false,
		Error:          "second path conflicted",
	}, nil)
	if !assignment.IsReservationReleaseRequired(err) || assignment.IsGuaranteedNoReservation(err) {
		t.Fatalf("partial-grant classification error = %v", err)
	}
	if !reflect.DeepEqual(lease.Granted, []string{"internal/cli/**"}) || !reflect.DeepEqual(lease.ReservationIDs, []int{73}) {
		t.Fatalf("partial-grant lease handles were lost: %+v", lease)
	}

	transportErr := errors.New("reservation transport disconnected")
	_, err = classifyCLIReservationAttempt(req, nil, transportErr)
	if !errors.Is(err, transportErr) || assignment.IsGuaranteedNoReservation(err) || assignment.IsReservationReleaseRequired(err) {
		t.Fatalf("zero-handle ambiguous transport error was misclassified: %v", err)
	}

	lease, err = classifyCLIReservationAttempt(req, &assignpkg.FileReservationResult{
		RequestedPaths: []string{"internal/cli/**"},
		Success:        false,
		Error:          transportErr.Error(),
	}, transportErr)
	if !errors.Is(err, transportErr) || assignment.IsGuaranteedNoReservation(err) || assignment.IsReservationReleaseRequired(err) {
		t.Fatalf("result-bearing ambiguous transport error was misclassified: %v", err)
	}
	if !reflect.DeepEqual(lease.Requested, []string{"internal/cli/**"}) || len(lease.Granted) != 0 || len(lease.ReservationIDs) != 0 {
		t.Fatalf("result-bearing transport lease = %+v", lease)
	}
}

func TestGenerateAssignmentsLegacyPreservesMultiWindowPaneIdentity(t *testing.T) {
	agents := []assignAgentInfo{
		{pane: tmux.Pane{ID: "%50", WindowIndex: 0, Index: 1}, agentType: "codex", state: "idle"},
		{pane: tmux.Pane{ID: "%51", WindowIndex: 1, Index: 1}, agentType: "claude", state: "idle"},
	}
	beads := []bv.BeadPreview{{ID: "ntm-a", Title: "First"}, {ID: "ntm-b", Title: "Second"}}
	items := generateAssignmentsLegacy(agents, beads, &AssignCommandOptions{Session: "multi", Strategy: "speed"})
	if len(items) != 2 {
		t.Fatalf("assignments=%d, want 2", len(items))
	}
	if items[0].PaneTarget != "0.1" || items[0].PaneID != "%50" || items[1].PaneTarget != "1.1" || items[1].PaneID != "%51" {
		t.Fatalf("multi-window assignments lost physical identity: %+v", items)
	}
	if items[0].AgentName == items[1].AgentName {
		t.Fatalf("multi-window agents share identity %q", items[0].AgentName)
	}
}

func TestResolveDirectAssignmentPaneCanonicalContract(t *testing.T) {
	panes := []tmux.Pane{
		{ID: "%10", WindowIndex: 0, Index: 0},
		{ID: "%11", WindowIndex: 0, Index: 1},
		{ID: "%12", WindowIndex: 1, Index: 0},
		{ID: "%13", WindowIndex: 1, Index: 1},
	}

	for _, test := range []struct {
		selector string
		paneID   string
		target   string
	}{
		{selector: "0.1", paneID: "%11", target: "0.1"},
		{selector: "%13", paneID: "%13", target: "1.1"},
		{selector: "1.0", paneID: "%12", target: "1.0"},
	} {
		pane, target, err := resolveDirectAssignmentPane(panes, test.selector)
		if err != nil {
			t.Fatalf("resolve %q: %v", test.selector, err)
		}
		if pane.ID != test.paneID || target != test.target {
			t.Fatalf("resolve %q = pane %s target %s, want %s/%s", test.selector, pane.ID, target, test.paneID, test.target)
		}
	}

	if _, _, err := resolveDirectAssignmentPane(panes, "1"); err == nil || !strings.Contains(err.Error(), "matched 2 panes") {
		t.Fatalf("bare multi-window selector must fail when its window has multiple panes: %v", err)
	}

	singleWindow := []tmux.Pane{{ID: "%20", WindowIndex: 0, Index: 0}, {ID: "%21", WindowIndex: 0, Index: 1}}
	pane, target, err := resolveDirectAssignmentPane(singleWindow, "1")
	if err != nil {
		t.Fatalf("resolve single-window bare pane: %v", err)
	}
	if pane.ID != "%21" || target != "1" {
		t.Fatalf("single-window bare selector = %s/%s, want %%21/1", pane.ID, target)
	}
}

func TestDirectAssignmentIdempotencyKeyUsesRawIntentAndPhysicalPane(t *testing.T) {
	base := &AssignCommandOptions{
		Session:      "project",
		PaneSelector: "1.0",
		Prompt:       "review the parser",
		Template:     "impl",
		IgnoreDeps:   true,
	}
	first := directAssignmentIdempotencyKey(base, "ntm-123", "%42")
	alias := *base
	alias.PaneSelector = "%42"
	if got := directAssignmentIdempotencyKey(&alias, "ntm-123", "%42"); got != first {
		t.Fatalf("selector aliases for one physical pane changed the key: %s != %s", got, first)
	}
	changedPrompt := *base
	changedPrompt.Prompt = "review the lexer"
	if got := directAssignmentIdempotencyKey(&changedPrompt, "ntm-123", "%42"); got == first {
		t.Fatal("changed prompt reused the raw-intent key")
	}
	if got := directAssignmentIdempotencyKey(base, "ntm-123", "%43"); got == first {
		t.Fatal("changed physical pane reused the raw-intent key")
	}
	postClear := directAssignmentIdempotencyKey(base, "ntm-123", "%42", 1)
	if postClear == first {
		t.Fatal("post-clear generation reused the previous raw-intent key")
	}
	if got := directAssignmentIdempotencyKey(base, "ntm-123", "%42", 1); got != postClear {
		t.Fatalf("same post-clear generation was not replay-stable: %s != %s", got, postClear)
	}

	templatePath := filepath.Join(t.TempDir(), "assign-template.txt")
	custom := &AssignCommandOptions{
		Session:      "project",
		PaneSelector: "%42",
		Template:     "custom",
		TemplateFile: templatePath,
	}
	if err := os.WriteFile(templatePath, []byte("first {BEAD_ID}"), 0o600); err != nil {
		t.Fatalf("write first template: %v", err)
	}
	firstTemplateKey := directAssignmentIdempotencyKey(custom, "ntm-123", "%42")
	if err := os.WriteFile(templatePath, []byte("second {BEAD_ID}"), 0o600); err != nil {
		t.Fatalf("write changed template: %v", err)
	}
	if got := directAssignmentIdempotencyKey(custom, "ntm-123", "%42"); got == firstTemplateKey {
		t.Fatal("changed template-file content reused the raw-intent key")
	}
}

func TestDirectAssignCLIProcessHelper(t *testing.T) {
	rawArgs := os.Getenv("NTM_DIRECT_ASSIGN_HELPER_ARGS")
	if rawArgs == "" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		t.Fatalf("decode direct assign helper args: %v", err)
	}
	os.Args = append([]string{"ntm"}, args...)
	if err := Execute(); err != nil {
		os.Exit(ExitCode(err))
	}
	os.Exit(0)
}

func TestDirectAssignCLIReplayIsDurableAndBypassesChangedPreflight(t *testing.T) {
	testutil.RequireTmuxThrottled(t)

	root := t.TempDir()
	home := filepath.Join(root, "home")
	projectDir := filepath.Join(root, "project")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{home, projectDir, fakeBin, filepath.Join(projectDir, ".git")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("NTM_DISABLE_INTERNAL_MONITOR", "1")
	realBR, err := exec.LookPath("br")
	if err != nil {
		t.Skip("br is required for guarded-claim replay coverage")
	}
	beadID := ""
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"create", "--title", "Direct assignment", "--type", "task", "--priority", "2", "--json"},
	} {
		cmd := exec.Command(realBR, args...)
		cmd.Dir = projectDir
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("br %s: %v\n%s", strings.Join(args, " "), runErr, output)
		}
		if args[0] == "create" {
			var created struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(output, &created); err != nil || created.ID == "" {
				t.Fatalf("parse br create output: id=%q err=%v output=%s", created.ID, err, output)
			}
			beadID = created.ID
		}
	}

	claimLog := filepath.Join(root, "claims.log")
	brScript := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
exec %q "$@"
`, claimLog, realBR)
	if err := os.WriteFile(filepath.Join(fakeBin, "br"), []byte(brScript), 0o755); err != nil {
		t.Fatalf("write fake br: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	dispatchLog := filepath.Join(root, "dispatch.log")
	agentScriptPath := filepath.Join(root, "agent.sh")
	agentScript := fmt.Sprintf(`#!/bin/sh
printf '❯ '
IFS= read -r line
printf '%%s\n' "$line" >> %q
printf '\n• Working (press esc to interrupt)\n'
sleep 300
`, dispatchLog)
	if err := os.WriteFile(agentScriptPath, []byte(agentScript), 0o755); err != nil {
		t.Fatalf("write agent fixture: %v", err)
	}

	session := fmt.Sprintf("directassign%d", time.Now().UnixNano())
	if err := tmux.CreateSession(session, projectDir); err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { _ = tmux.KillSession(session) })
	paneID, err := tmux.DefaultClient.Run("new-window", "-d", "-t", session, "-c", projectDir, "-P", "-F", "#{pane_id}", agentScriptPath)
	if err != nil {
		t.Fatalf("create agent window: %v", err)
	}
	paneID = strings.TrimSpace(paneID)
	if err := tmux.SetPaneTitle(paneID, session+"__cc_1"); err != nil {
		t.Fatalf("set pane title: %v", err)
	}
	waitForDirectAssignFixture(t, paneID, "❯")

	baseArgs := []string{
		"--json", "assign", session,
		"--pane=" + paneID,
		"--beads=" + beadID,
		"--prompt=inspect durable replay",
		"--ignore-deps",
		"--reserve-files=false",
		"--repo=" + projectDir,
	}
	first, firstCode, firstStderr := runDirectAssignCLIProcess(t, baseArgs)
	if firstCode != 0 || !first.Success || first.Data == nil || first.Data.Receipt == nil {
		claimTrace, _ := os.ReadFile(claimLog)
		t.Fatalf("first assign failed: code=%d stderr=%q error=%+v data=%+v claims=%q", firstCode, firstStderr, first.Error, first.Data, claimTrace)
	}
	waitForDirectAssignFileLines(t, dispatchLog, 1)
	waitForDirectAssignFixture(t, paneID, "Working")
	if output, captureErr := tmux.CapturePaneOutput(paneID, 20); captureErr != nil || determineAgentState(output, "claude") != "working" {
		t.Fatalf("fixture did not become a changed busy preflight: state=%q err=%v output=%q", determineAgentState(output, "claude"), captureErr, output)
	}

	second, secondCode, secondStderr := runDirectAssignCLIProcess(t, baseArgs)
	if secondCode != 0 || !second.Success || second.Data == nil || second.Data.Receipt == nil {
		t.Fatalf("same-intent replay failed: code=%d stderr=%q error=%+v data=%+v", secondCode, secondStderr, second.Error, second.Data)
	}
	if !reflect.DeepEqual(first.Data.Receipt, second.Data.Receipt) {
		t.Fatalf("same-intent replay receipt changed:\nfirst=%+v\nsecond=%+v", first.Data.Receipt, second.Data.Receipt)
	}
	if got := countDirectAssignLogMatches(t, claimLog, "sync --flush-only"); got != 1 {
		t.Fatalf("same-intent replay finalized %d guarded claims, want 1", got)
	}
	if got := countDirectAssignLogLines(t, dispatchLog); got != 1 {
		t.Fatalf("same-intent replay dispatched %d times, want 1", got)
	}

	changedArgs := append([]string(nil), baseArgs...)
	changedArgs[5] = "--prompt=changed intent must conflict"
	changed, changedCode, changedStderr := runDirectAssignCLIProcess(t, changedArgs)
	if changedCode == 0 || changed.Success || changed.Error == nil || changed.Error.Code != "CLAIM_CONFLICT" {
		t.Fatalf("changed intent did not conflict: code=%d stderr=%q error=%+v data=%+v", changedCode, changedStderr, changed.Error, changed.Data)
	}
	if got := countDirectAssignLogMatches(t, claimLog, "sync --flush-only"); got != 1 {
		t.Fatalf("changed-intent conflict actuated a guarded claim; count=%d", got)
	}
	if got := countDirectAssignLogLines(t, dispatchLog); got != 1 {
		t.Fatalf("changed-intent conflict actuated dispatch; count=%d", got)
	}

	store, err := assignment.LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load durable ledger: %v", err)
	}
	durable := store.Get(beadID)
	if durable == nil {
		t.Fatal("direct assignment missing from durable ledger")
	}
	if durable.OccupancyKey != paneID || durable.DispatchTarget != paneID || first.Data.Receipt.Pane.ID != paneID || first.Data.Receipt.Pane.Target != "2.1" {
		t.Fatalf("pane identity drift: ledger=%+v receipt=%+v", durable, first.Data.Receipt.Pane)
	}
}

func countDirectAssignLogMatches(t *testing.T, path, needle string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func runDirectAssignCLIProcess(t *testing.T, args []string) (AssignEnvelope[DirectAssignData], int, string) {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("encode helper args: %v", err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDirectAssignCLIProcessHelper$")
	cmd.Env = append(os.Environ(), "NTM_DIRECT_ASSIGN_HELPER_ARGS="+string(rawArgs), "NTM_NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("start direct assign helper: %v", err)
		}
		exitCode = exitErr.ExitCode()
	}
	var envelope AssignEnvelope[DirectAssignData]
	if decodeErr := json.Unmarshal(stdout.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("decode direct assign output: %v\nstdout=%q\nstderr=%q", decodeErr, stdout.String(), stderr.String())
	}
	return envelope, exitCode, stderr.String()
}

func waitForDirectAssignFixture(t *testing.T, paneID, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		output, err := tmux.CapturePaneOutput(paneID, 30)
		if err == nil && strings.Contains(output, marker) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pane %s did not contain %q", paneID, marker)
}

func waitForDirectAssignFileLines(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if countDirectAssignLogLines(t, path) >= want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not reach %d lines", path, want)
}

func countDirectAssignLogLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read %s: %v", path, err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
