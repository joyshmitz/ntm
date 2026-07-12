package assignment

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	store := NewStore("test-session")
	if store.SessionName != "test-session" {
		t.Errorf("expected session name 'test-session', got '%s'", store.SessionName)
	}
	if store.Assignments == nil {
		t.Error("expected assignments map to be initialized")
	}
	if len(store.Assignments) != 0 {
		t.Errorf("expected empty assignments, got %d", len(store.Assignments))
	}
	if store.Version != assignmentStoreVersion {
		t.Errorf("expected version %d, got %d", assignmentStoreVersion, store.Version)
	}
}

func TestAssign(t *testing.T) {
	// Use temp directory
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")

	assignment, err := store.Assign("bd-123", "Test bead title", 1, "claude", "TestAgent", "Test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if assignment.BeadID != "bd-123" {
		t.Errorf("expected bead ID 'bd-123', got '%s'", assignment.BeadID)
	}
	if assignment.BeadTitle != "Test bead title" {
		t.Errorf("expected bead title 'Test bead title', got '%s'", assignment.BeadTitle)
	}
	if assignment.Pane != 1 {
		t.Errorf("expected pane 1, got %d", assignment.Pane)
	}
	if assignment.AgentType != "claude" {
		t.Errorf("expected agent type 'claude', got '%s'", assignment.AgentType)
	}
	if assignment.AgentName != "TestAgent" {
		t.Errorf("expected agent name 'TestAgent', got '%s'", assignment.AgentName)
	}
	if assignment.Status != StatusAssigned {
		t.Errorf("expected status 'assigned', got '%s'", assignment.Status)
	}
	if assignment.PromptSent != "Test prompt" {
		t.Errorf("expected prompt 'Test prompt', got '%s'", assignment.PromptSent)
	}
}

func TestGet(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")

	// Test getting existing assignment
	a := store.Get("bd-123")
	if a == nil {
		t.Fatal("expected assignment, got nil")
	}
	if a.BeadID != "bd-123" {
		t.Errorf("expected bead ID 'bd-123', got '%s'", a.BeadID)
	}

	// Test getting non-existent assignment
	a = store.Get("bd-nonexistent")
	if a != nil {
		t.Errorf("expected nil for non-existent assignment, got %v", a)
	}
}

func TestGetReturnsSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	assigned, _ := store.Assign("bd-123", "Test bead", 1, "claude", "", "prompt")
	assigned.Pane = 99

	if err := store.MarkWorking("bd-123"); err != nil {
		t.Fatalf("MarkWorking: %v", err)
	}

	got := store.Get("bd-123")
	if got == nil {
		t.Fatal("expected assignment, got nil")
	}
	if got.Pane != 1 {
		t.Fatalf("expected pane 1, got %d", got.Pane)
	}
	if got.StartedAt == nil {
		t.Fatal("expected StartedAt to be set")
	}

	mutated := got.StartedAt.Add(5 * time.Minute)
	*got.StartedAt = mutated
	got.PromptSent = "mutated"

	fresh := store.Get("bd-123")
	if fresh == nil {
		t.Fatal("expected assignment on second read, got nil")
	}
	if fresh.PromptSent != "prompt" {
		t.Fatalf("expected stored prompt to remain %q, got %q", "prompt", fresh.PromptSent)
	}
	if fresh.StartedAt == nil {
		t.Fatal("expected StartedAt on second read")
	}
	if fresh.StartedAt.Equal(mutated) {
		t.Fatalf("expected StartedAt snapshot to be isolated from caller mutation")
	}
}

func TestList(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 2, "codex", "", "")
	_, _ = store.Assign("bd-3", "Bead 3", 3, "gemini", "", "")

	assignments := store.List()
	if len(assignments) != 3 {
		t.Errorf("expected 3 assignments, got %d", len(assignments))
	}
}

func TestListByPane(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 1, "claude", "", "")
	_, _ = store.Assign("bd-3", "Bead 3", 2, "codex", "", "")

	pane1 := store.ListByPane(1)
	if len(pane1) != 2 {
		t.Errorf("expected 2 assignments for pane 1, got %d", len(pane1))
	}

	pane2 := store.ListByPane(2)
	if len(pane2) != 1 {
		t.Errorf("expected 1 assignment for pane 2, got %d", len(pane2))
	}
}

func TestListByStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 2, "codex", "", "")

	// Mark one as working
	_ = store.MarkWorking("bd-1")

	assigned := store.ListByStatus(StatusAssigned)
	if len(assigned) != 1 {
		t.Errorf("expected 1 assigned, got %d", len(assigned))
	}

	working := store.ListByStatus(StatusWorking)
	if len(working) != 1 {
		t.Errorf("expected 1 working, got %d", len(working))
	}
}

func TestListActive(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 2, "codex", "", "")
	_, _ = store.Assign("bd-3", "Bead 3", 3, "gemini", "", "")

	// Mark one as working and one as completed
	_ = store.MarkWorking("bd-1")
	_ = store.MarkWorking("bd-3")
	_ = store.MarkCompleted("bd-3")

	active := store.ListActive()
	if len(active) != 2 {
		t.Errorf("expected 2 active (assigned + working), got %d", len(active))
	}
}

func TestStateTransitions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	tests := []struct {
		name  string
		from  AssignmentStatus
		to    AssignmentStatus
		valid bool
	}{
		{"assigned to working", StatusAssigned, StatusWorking, true},
		{"assigned to failed", StatusAssigned, StatusFailed, true},
		// `assigned -> completed` is permitted because beads can close externally
		// (br close from another agent) before the assignment store ever observed
		// a `working` transition. See ValidTransitions docs and #124.
		{"assigned to completed (external close)", StatusAssigned, StatusCompleted, true},
		{"working to completed", StatusWorking, StatusCompleted, true},
		{"working to failed", StatusWorking, StatusFailed, true},
		{"working to reassigned", StatusWorking, StatusReassigned, true},
		{"completed to anything", StatusCompleted, StatusAssigned, false},
		{"failed to assigned (retry)", StatusFailed, StatusAssigned, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidTransition(tt.from, tt.to)
			if result != tt.valid {
				t.Errorf("expected isValidTransition(%s, %s) = %v, got %v",
					tt.from, tt.to, tt.valid, result)
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")

	// Valid transition: assigned -> working
	err := store.MarkWorking("bd-123")
	if err != nil {
		t.Errorf("unexpected error marking as working: %v", err)
	}

	a := store.Get("bd-123")
	if a.Status != StatusWorking {
		t.Errorf("expected status working, got %s", a.Status)
	}
	if a.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}

	// Valid transition: working -> completed
	err = store.MarkCompleted("bd-123")
	if err != nil {
		t.Errorf("unexpected error marking as completed: %v", err)
	}

	a = store.Get("bd-123")
	if a.Status != StatusCompleted {
		t.Errorf("expected status completed, got %s", a.Status)
	}
	if a.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestInvalidTransition(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	if _, err := store.Assign("bd-123", "Test bead", 1, "claude", "", ""); err != nil {
		t.Fatalf("Assign() error = %v", err)
	}

	// Move to a terminal state first. `assigned -> completed` is now valid
	// (#124) because beads can close externally before the store ever
	// observed a `working` transition; we use that valid transition here as
	// setup so we can then assert the *terminal -> non-terminal* transition
	// is still rejected.
	if err := store.UpdateStatus("bd-123", StatusCompleted); err != nil {
		t.Fatalf("setup: assigned -> completed should be valid, got: %v", err)
	}

	// Invalid transition: completed -> assigned (terminal -> any).
	err := store.UpdateStatus("bd-123", StatusAssigned)
	if err == nil {
		t.Fatal("expected error for invalid transition completed->assigned, got nil")
	}
	if _, ok := err.(*InvalidTransitionError); !ok {
		t.Errorf("expected InvalidTransitionError, got %T (%v)", err, err)
	}

	// Status should remain completed
	a := store.Get("bd-123")
	if a == nil {
		t.Fatal("Get(bd-123) returned nil")
	}
	if a.Status != StatusCompleted {
		t.Errorf("expected status to remain completed, got %s", a.Status)
	}
}

// TestExternalCloseTransitionsAssignedToCompleted verifies the #124 fix:
// when br close happens externally before the agent ever reported "working",
// the watch loop's correlation step must be able to mark the assignment as
// completed without an "Invalid transition assigned -> completed" error
// leaving the row stuck.
func TestExternalCloseTransitionsAssignedToCompleted(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session-extclose")
	_, _ = store.Assign("bd-456", "External close bead", 2, "claude", "", "")

	// Skip Working — simulate br close fired externally before the agent
	// reported any progress that would have moved us to Working.
	if err := store.UpdateStatus("bd-456", StatusCompleted); err != nil {
		t.Fatalf("assigned -> completed (external close) should be valid: %v", err)
	}

	a := store.Get("bd-456")
	if a.Status != StatusCompleted {
		t.Fatalf("status = %s, want completed", a.Status)
	}
	if a.CompletedAt == nil {
		t.Error("CompletedAt should be set after assigned -> completed transition")
	}
}

func TestMarkFailed(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")

	err := store.MarkFailed("bd-123", "Agent crashed")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	a := store.Get("bd-123")
	if a.Status != StatusFailed {
		t.Errorf("expected status failed, got %s", a.Status)
	}
	if a.FailReason != "Agent crashed" {
		t.Errorf("expected fail reason 'Agent crashed', got '%s'", a.FailReason)
	}
	if a.FailedAt == nil {
		t.Error("expected FailedAt to be set")
	}
}

func TestRetryTransitionClearsFailureMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")
	if err := store.MarkWorking("bd-123"); err != nil {
		t.Fatalf("MarkWorking: %v", err)
	}
	if err := store.MarkFailed("bd-123", "Agent crashed"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := store.UpdateStatus("bd-123", StatusAssigned); err != nil {
		t.Fatalf("UpdateStatus retry: %v", err)
	}

	got := store.Get("bd-123")
	if got == nil {
		t.Fatal("expected assignment, got nil")
	}
	if got.Status != StatusAssigned {
		t.Fatalf("expected status assigned, got %s", got.Status)
	}
	if got.FailedAt != nil {
		t.Fatalf("expected FailedAt to be cleared on retry")
	}
	if got.StartedAt != nil {
		t.Fatalf("expected StartedAt to be cleared on retry")
	}
	if got.FailReason != "" {
		t.Fatalf("expected FailReason to be cleared on retry, got %q", got.FailReason)
	}
	if got.FailureReason != "" {
		t.Fatalf("expected FailureReason to be cleared on retry, got %q", got.FailureReason)
	}
}

func TestReassign(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "Agent1", "Do the thing")
	retryCount := 2
	if err := store.Update("bd-123", AssignmentUpdate{RetryCount: &retryCount}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Must be working to reassign
	_ = store.MarkWorking("bd-123")

	newAssignment, err := store.Reassign("bd-123", 2, "codex", "Agent2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if newAssignment.Pane != 2 {
		t.Errorf("expected pane 2, got %d", newAssignment.Pane)
	}
	if newAssignment.AgentType != "codex" {
		t.Errorf("expected agent type 'codex', got '%s'", newAssignment.AgentType)
	}
	if newAssignment.AgentName != "Agent2" {
		t.Errorf("expected agent name 'Agent2', got '%s'", newAssignment.AgentName)
	}
	if newAssignment.Status != StatusAssigned {
		t.Errorf("expected status assigned, got %s", newAssignment.Status)
	}
	if newAssignment.PromptSent != "" {
		t.Errorf("expected prompt to be empty until resent, got '%s'", newAssignment.PromptSent)
	}
	if newAssignment.RetryCount != retryCount {
		t.Errorf("expected retry count %d to be preserved, got %d", retryCount, newAssignment.RetryCount)
	}
}

func TestUpdateAssignmentMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")

	prompt := "Actual delivered prompt"
	retryCount := 3
	if err := store.Update("bd-123", AssignmentUpdate{
		PromptSent: &prompt,
		RetryCount: &retryCount,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := store.Get("bd-123")
	if got == nil {
		t.Fatal("expected assignment, got nil")
	}
	if got.PromptSent != prompt {
		t.Fatalf("expected prompt %q, got %q", prompt, got.PromptSent)
	}
	if got.RetryCount != retryCount {
		t.Fatalf("expected retry count %d, got %d", retryCount, got.RetryCount)
	}
}

func TestRemove(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-123", "Test bead", 1, "claude", "", "")

	store.Remove("bd-123")

	a := store.Get("bd-123")
	if a != nil {
		t.Errorf("expected nil after remove, got %v", a)
	}
}

func TestClear(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 2, "codex", "", "")

	store.Clear()

	if len(store.List()) != 0 {
		t.Errorf("expected empty after clear, got %d", len(store.List()))
	}
}

func TestStats(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")
	_, _ = store.Assign("bd-1", "Bead 1", 1, "claude", "", "")
	_, _ = store.Assign("bd-2", "Bead 2", 2, "codex", "", "")
	_, _ = store.Assign("bd-3", "Bead 3", 3, "gemini", "", "")
	_, _ = store.Assign("bd-4", "Bead 4", 4, "claude", "", "")

	_ = store.MarkWorking("bd-2")
	_ = store.MarkWorking("bd-3")
	_ = store.MarkCompleted("bd-3")
	_ = store.MarkFailed("bd-4", "crashed")

	stats := store.Stats()
	if stats.Total != 4 {
		t.Errorf("expected total 4, got %d", stats.Total)
	}
	if stats.Assigned != 1 {
		t.Errorf("expected assigned 1, got %d", stats.Assigned)
	}
	if stats.Working != 1 {
		t.Errorf("expected working 1, got %d", stats.Working)
	}
	if stats.Completed != 1 {
		t.Errorf("expected completed 1, got %d", stats.Completed)
	}
	if stats.Failed != 1 {
		t.Errorf("expected failed 1, got %d", stats.Failed)
	}
}

func TestPersistenceSaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create and save
	store1 := NewStore("persist-test")
	_, _ = store1.Assign("bd-123", "Test bead", 1, "claude", "TestAgent", "Test prompt")
	_ = store1.MarkWorking("bd-123")

	// Load in new store
	store2, err := LoadStore("persist-test")
	if err != nil {
		t.Fatalf("unexpected error loading: %v", err)
	}

	a := store2.Get("bd-123")
	if a == nil {
		t.Fatal("expected assignment after load, got nil")
	}
	if a.BeadID != "bd-123" {
		t.Errorf("expected bead ID 'bd-123', got '%s'", a.BeadID)
	}
	if a.Status != StatusWorking {
		t.Errorf("expected status working, got %s", a.Status)
	}
	if a.AgentName != "TestAgent" {
		t.Errorf("expected agent name 'TestAgent', got '%s'", a.AgentName)
	}
}

func TestPersistenceBackupRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create valid backup file
	// assignments are now in ~/.ntm/sessions/<session>/assignments.json
	// So we need to create <tmpDir>/.ntm/sessions/backup-test/assignments.json.bak
	dir := filepath.Join(tmpDir, ".ntm", "sessions", "backup-test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	backupStore := &AssignmentStore{
		SessionName: "backup-test",
		Assignments: map[string]*Assignment{
			"bd-backup": {
				BeadID:     "bd-backup",
				BeadTitle:  "Backup bead",
				Pane:       1,
				AgentType:  "claude",
				Status:     StatusAssigned,
				AssignedAt: time.Now().UTC(),
			},
		},
		UpdatedAt: time.Now().UTC(),
		Version:   1,
	}

	data, _ := json.MarshalIndent(backupStore, "", "  ")
	// Name is assignments.json (assignmentsDirName + fileExtension)
	bakPath := filepath.Join(dir, "assignments.json.bak")
	if err := os.WriteFile(bakPath, data, 0644); err != nil {
		t.Fatalf("failed to write backup: %v", err)
	}

	// Write corrupted main file
	mainPath := filepath.Join(dir, "assignments.json")
	if err := os.WriteFile(mainPath, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write corrupted file: %v", err)
	}

	// Load should recover from backup
	store, err := LoadStore("backup-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := store.Get("bd-backup")
	if a == nil {
		t.Fatal("expected assignment from backup, got nil")
	}
	if a.BeadID != "bd-backup" {
		t.Errorf("expected bead ID 'bd-backup', got '%s'", a.BeadID)
	}
	if store.Version != assignmentStoreVersion {
		t.Fatalf("recovered store version=%d, want migrated version %d", store.Version, assignmentStoreVersion)
	}
}

func TestLoadStoreStrictRejectsCorruptLedgerWithoutBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	store := NewStore("strict-corrupt")
	if err := os.WriteFile(store.path, []byte("{not-json"), 0644); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}

	if _, err := LoadStoreStrict("strict-corrupt"); err == nil {
		t.Fatal("expected strict load to reject corrupt primary without backup")
	}
}

func TestLoadStoreStrictNeverRollsBackToBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	store := NewStore("strict-stale-backup")

	stale := &AssignmentStore{
		SessionName: "strict-stale-backup",
		Assignments: map[string]*Assignment{
			"bd-ambiguous": {
				BeadID:         "bd-ambiguous",
				Status:         StatusClaimed,
				DispatchState:  DispatchPending,
				IdempotencyKey: "attempt-1",
			},
		},
		Version: assignmentStoreVersion,
	}
	backup, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal stale backup: %v", err)
	}
	if err := os.WriteFile(store.path+".bak", backup, 0644); err != nil {
		t.Fatalf("write stale backup: %v", err)
	}
	if err := os.WriteFile(store.path, []byte("{not-json"), 0644); err != nil {
		t.Fatalf("write corrupt primary: %v", err)
	}

	if _, err := LoadStoreStrict("strict-stale-backup"); err == nil {
		t.Fatal("expected strict load to reject corrupt primary instead of restoring retryable backup")
	}
}

func TestLoadStoreStrictRejectsOrphanedBackup(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	store := NewStore("strict-orphaned-backup")
	if err := os.WriteFile(store.path+".bak", []byte(`{"session_name":"strict-orphaned-backup","assignments":{}}`), 0644); err != nil {
		t.Fatalf("write orphaned backup: %v", err)
	}

	if _, err := LoadStoreStrict("strict-orphaned-backup"); err == nil {
		t.Fatal("expected strict load to reject a backup with no primary ledger")
	}
}

func TestStaleLifecycleWriterPreservesAtomicDispatchBarrierAndReceipt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		session = "same-bead-field-merge"
		beadID  = "ntm-same-bead"
		key     = "same-bead-attempt"
	)

	seed := NewStore(session)
	req := AtomicRequest{
		BeadID:         beadID,
		BeadTitle:      "Same bead merge",
		Target:         "%17",
		Pane:           2,
		AgentType:      "codex",
		AgentName:      "CodexOne",
		Actor:          "CodexOne",
		Prompt:         "continue",
		IdempotencyKey: key,
	}
	claimedAt := time.Now().UTC().Add(-time.Minute)
	if _, err := seed.RecordAtomicIntent(req, StableClaimActor(req.Actor, key), claimedAt); err != nil {
		t.Fatalf("RecordAtomicIntent: %v", err)
	}
	if _, err := seed.RecordAtomicClaim(req, ClaimReceipt{
		BeadID: beadID, Actor: StableClaimActor(req.Actor, key), Status: "in_progress", ClaimedAt: claimedAt,
	}); err != nil {
		t.Fatalf("RecordAtomicClaim: %v", err)
	}
	seed.Assignments[beadID].Status = StatusAssigned
	if err := seed.Save(); err != nil {
		t.Fatalf("persist assigned seed: %v", err)
	}

	staleLifecycle, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load stale lifecycle writer: %v", err)
	}
	dispatchWriter, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load dispatch writer: %v", err)
	}
	startedAt := time.Now().UTC()
	if err := dispatchWriter.RecordAtomicDispatchStarted(beadID, key, startedAt); err != nil {
		t.Fatalf("RecordAtomicDispatchStarted: %v", err)
	}
	if err := staleLifecycle.MarkCompleted(beadID); err != nil {
		t.Fatalf("stale MarkCompleted: %v", err)
	}

	afterCompletion, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("reload after stale completion: %v", err)
	}
	barrier := afterCompletion.Get(beadID)
	if barrier == nil || barrier.Status != StatusCompleted || barrier.DispatchState != DispatchSending ||
		barrier.DispatchAttempts != 1 || barrier.DispatchStartedAt == nil || !barrier.DispatchStartedAt.Equal(startedAt) {
		t.Fatalf("stale lifecycle merge erased sending barrier: %+v", barrier)
	}

	dispatchedAt := startedAt.Add(time.Second)
	if err := dispatchWriter.RecordAtomicDispatchSent(beadID, key, req.Prompt, DispatchReceipt{
		DeliveryID: "delivery-17", Duration: 25 * time.Millisecond,
	}, dispatchedAt); err != nil {
		t.Fatalf("RecordAtomicDispatchSent: %v", err)
	}
	finalStore, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("reload final store: %v", err)
	}
	final := finalStore.Get(beadID)
	if final == nil || final.Status != StatusCompleted || final.DispatchState != DispatchSent ||
		final.DispatchAttempts != 1 || final.DispatchReceiptID != "delivery-17" ||
		final.DispatchedAt == nil || !final.DispatchedAt.Equal(dispatchedAt) {
		t.Fatalf("dispatch receipt/lifecycle merge = %+v", final)
	}
}

func TestSaveMergeRejectsCorruptPrimaryInsteadOfUsingBackup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewStore("strict-save-merge")
	if _, err := store.Assign("ntm-corrupt", "Corrupt", 1, "codex", "CodexOne", "prompt"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := os.WriteFile(store.path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}
	if err := store.Save(); err == nil {
		t.Fatal("Save succeeded by rolling back to a stale backup")
	}
}

func TestReassignExplicitlyReplacesAtomicRecord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewStore("reassign-atomic-record")
	req := AtomicRequest{
		BeadID: "ntm-reassign", BeadTitle: "Reassign", Target: "%31", Pane: 1,
		AgentType: "codex", AgentName: "CodexOne", Actor: "CodexOne",
		Prompt: "work", IdempotencyKey: "old-atomic-key",
	}
	now := time.Now().UTC()
	if _, err := store.RecordAtomicIntent(req, StableClaimActor(req.Actor, req.IdempotencyKey), now); err != nil {
		t.Fatalf("RecordAtomicIntent: %v", err)
	}
	if _, err := store.RecordAtomicClaim(req, ClaimReceipt{
		BeadID: req.BeadID, Actor: StableClaimActor(req.Actor, req.IdempotencyKey), Status: "in_progress", ClaimedAt: now,
	}); err != nil {
		t.Fatalf("RecordAtomicClaim: %v", err)
	}
	if err := store.RecordAtomicDispatchStarted(req.BeadID, req.IdempotencyKey, now); err != nil {
		t.Fatalf("RecordAtomicDispatchStarted: %v", err)
	}
	if err := store.RecordAtomicDispatchSent(req.BeadID, req.IdempotencyKey, req.Prompt, DispatchReceipt{DeliveryID: "old-receipt"}, now); err != nil {
		t.Fatalf("RecordAtomicDispatchSent: %v", err)
	}
	if err := store.MarkWorking(req.BeadID); err != nil {
		t.Fatalf("MarkWorking: %v", err)
	}
	if _, err := store.Reassign(req.BeadID, 4, "claude", "ClaudeFour"); err != nil {
		t.Fatalf("Reassign: %v", err)
	}

	reloaded, err := LoadStoreStrict("reassign-atomic-record")
	if err != nil {
		t.Fatalf("LoadStoreStrict: %v", err)
	}
	got := reloaded.Get(req.BeadID)
	if got == nil || got.Pane != 4 || got.AgentType != "claude" || got.AgentName != "ClaudeFour" || got.Status != StatusAssigned {
		t.Fatalf("reassigned record = %+v", got)
	}
	if got.IdempotencyKey != "" || got.DispatchState != "" || got.DispatchTarget != "" ||
		got.DispatchReceiptID != "" || got.DispatchAttempts != 0 || got.PromptSent != "" {
		t.Fatalf("reassign retained stale atomic metadata: %+v", got)
	}
}

func TestDestructiveStoreMutationsRejectStaleAtomicBarrier(t *testing.T) {
	operations := []struct {
		name string
		run  func(*AssignmentStore, string) error
	}{
		{name: "remove", run: func(store *AssignmentStore, beadID string) error {
			return store.Remove(beadID)
		}},
		{name: "clear", run: func(store *AssignmentStore, _ string) error {
			return store.Clear()
		}},
		{name: "assign", run: func(store *AssignmentStore, beadID string) error {
			_, err := store.Assign(beadID, "replacement", 9, "claude", "ClaudeNine", "replacement prompt")
			return err
		}},
		{name: "reassign", run: func(store *AssignmentStore, beadID string) error {
			_, err := store.Reassign(beadID, 9, "claude", "ClaudeNine")
			return err
		}},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			session := "stale-destructive-" + operation.name
			beadID := "ntm-stale-" + operation.name
			key := "stale-key-" + operation.name
			seed := NewStore(session)
			req := AtomicRequest{
				BeadID: beadID, BeadTitle: "Stale mutation", Target: "%17", OccupancyKey: "%17", Pane: 2,
				AgentType: "codex", AgentName: "CodexTwo", Actor: "CodexTwo", Prompt: "continue", IdempotencyKey: key,
			}
			now := time.Now().UTC()
			actor := StableClaimActor(req.Actor, key)
			if _, err := seed.RecordAtomicIntent(req, actor, now); err != nil {
				t.Fatalf("RecordAtomicIntent: %v", err)
			}
			if _, err := seed.RecordAtomicClaim(req, ClaimReceipt{BeadID: beadID, Actor: actor, Status: "in_progress", ClaimedAt: now}); err != nil {
				t.Fatalf("RecordAtomicClaim: %v", err)
			}
			seed.Assignments[beadID].Status = StatusWorking
			if err := seed.Save(); err != nil {
				t.Fatalf("persist working seed: %v", err)
			}

			stale, err := LoadStoreStrict(session)
			if err != nil {
				t.Fatalf("load stale writer: %v", err)
			}
			barrierWriter, err := LoadStoreStrict(session)
			if err != nil {
				t.Fatalf("load barrier writer: %v", err)
			}
			startedAt := now.Add(time.Second)
			if err := barrierWriter.RecordAtomicDispatchStarted(beadID, key, startedAt); err != nil {
				t.Fatalf("RecordAtomicDispatchStarted: %v", err)
			}

			mutationErr := operation.run(stale, beadID)
			var conflict *ConcurrentMutationError
			if !errors.As(mutationErr, &conflict) || conflict.BeadID != beadID {
				t.Fatalf("stale %s error=%v, want ConcurrentMutationError for %s", operation.name, mutationErr, beadID)
			}
			for label, stored := range map[string]*Assignment{
				"stale writer": stale.Get(beadID),
				"durable":      mustLoadAssignment(t, session, beadID),
			} {
				if stored == nil || stored.DispatchState != DispatchSending || stored.DispatchAttempts != 1 ||
					stored.DispatchStartedAt == nil || !stored.DispatchStartedAt.Equal(startedAt) || stored.IdempotencyKey != key {
					t.Fatalf("%s after stale %s = %+v", label, operation.name, stored)
				}
			}
		})
	}
}

func TestStaleLifecycleUpdateCannotResurrectRemovedAssignment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const session = "stale-update-after-remove"
	const beadID = "ntm-removed-generation"

	seed := NewStore(session)
	if _, err := seed.Assign(beadID, "Removed assignment", 2, "codex", "CodexTwo", "work"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := seed.MarkWorking(beadID); err != nil {
		t.Fatalf("MarkWorking: %v", err)
	}
	stale, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load stale writer: %v", err)
	}
	remover, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load remover: %v", err)
	}
	if err := remover.Remove(beadID); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	updateErr := stale.MarkCompleted(beadID)
	var conflict *ConcurrentMutationError
	if !errors.As(updateErr, &conflict) || conflict.BeadID != beadID {
		t.Fatalf("stale MarkCompleted error=%v, want ConcurrentMutationError", updateErr)
	}
	if got := stale.Get(beadID); got != nil {
		t.Fatalf("losing store resurrected removed assignment in memory: %+v", got)
	}
	if got := mustLoadAssignment(t, session, beadID); got != nil {
		t.Fatalf("stale lifecycle update resurrected durable assignment: %+v", got)
	}
}

func TestStaleLifecycleUpdateCannotCrossAssignmentGeneration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const session = "stale-update-cross-generation"
	const beadID = "ntm-cross-generation"
	now := time.Now().UTC()
	oldRequest := AtomicRequest{
		BeadID: beadID, BeadTitle: "Old generation", Target: "%41", OccupancyKey: "%41", Pane: 1,
		AgentType: "codex", AgentName: "CodexOne", Actor: "CodexOne", Prompt: "old work", IdempotencyKey: "old-generation-key",
	}
	seed := NewStore(session)
	oldActor := StableClaimActor(oldRequest.Actor, oldRequest.IdempotencyKey)
	if _, err := seed.RecordAtomicIntent(oldRequest, oldActor, now); err != nil {
		t.Fatalf("RecordAtomicIntent(old): %v", err)
	}
	if _, err := seed.RecordAtomicClaim(oldRequest, ClaimReceipt{BeadID: beadID, Actor: oldActor, Status: "in_progress", ClaimedAt: now}); err != nil {
		t.Fatalf("RecordAtomicClaim(old): %v", err)
	}
	if err := seed.UpdateStatus(beadID, StatusAssigned); err != nil {
		t.Fatalf("mark old assigned: %v", err)
	}
	if err := seed.MarkWorking(beadID); err != nil {
		t.Fatalf("mark old working: %v", err)
	}
	stale, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load old-generation writer: %v", err)
	}
	winner, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load winner: %v", err)
	}
	if err := winner.MarkCompleted(beadID); err != nil {
		t.Fatalf("complete old generation: %v", err)
	}
	newRequest := oldRequest
	newRequest.BeadTitle = "New generation"
	newRequest.Prompt = "new work"
	newRequest.IdempotencyKey = "new-generation-key"
	newActor := StableClaimActor(newRequest.Actor, newRequest.IdempotencyKey)
	if _, err := winner.RecordAtomicIntent(newRequest, newActor, now.Add(time.Minute)); err != nil {
		t.Fatalf("RecordAtomicIntent(new): %v", err)
	}

	updateErr := stale.MarkCompleted(beadID)
	var conflict *ConcurrentMutationError
	if !errors.As(updateErr, &conflict) || conflict.BeadID != beadID {
		t.Fatalf("old-generation MarkCompleted error=%v, want ConcurrentMutationError", updateErr)
	}
	for label, stored := range map[string]*Assignment{
		"losing store": stale.Get(beadID),
		"durable":      mustLoadAssignment(t, session, beadID),
	} {
		if stored == nil || stored.IdempotencyKey != newRequest.IdempotencyKey || stored.Status != StatusClaiming || stored.PendingPrompt != newRequest.Prompt {
			t.Fatalf("%s after generation conflict = %+v", label, stored)
		}
	}
}

func TestRecordAtomicIntentConflictKeepsDurableWinnerInMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const session = "atomic-intent-concurrent-generation"
	const beadID = "ntm-intent-winner"
	now := time.Now().UTC()
	seed := NewStore(session)
	old := AtomicRequest{
		BeadID: beadID, BeadTitle: "Terminal", Target: "%51", OccupancyKey: "%51", Pane: 1,
		AgentType: "codex", AgentName: "CodexOne", Actor: "CodexOne", Prompt: "old", IdempotencyKey: "terminal-key",
	}
	if _, err := seed.RecordAtomicIntent(old, StableClaimActor(old.Actor, old.IdempotencyKey), now); err != nil {
		t.Fatalf("seed intent: %v", err)
	}
	seed.Assignments[beadID].Status = StatusCompleted
	if err := seed.Save(); err != nil {
		t.Fatalf("persist terminal seed: %v", err)
	}
	winner, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load winner: %v", err)
	}
	loser, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("load loser: %v", err)
	}
	winnerRequest := old
	winnerRequest.Prompt = "winner"
	winnerRequest.IdempotencyKey = "winner-key"
	if _, err := winner.RecordAtomicIntent(winnerRequest, StableClaimActor(winnerRequest.Actor, winnerRequest.IdempotencyKey), now.Add(time.Minute)); err != nil {
		t.Fatalf("winner RecordAtomicIntent: %v", err)
	}
	loserRequest := old
	loserRequest.Prompt = "loser"
	loserRequest.IdempotencyKey = "loser-key"
	_, loserErr := loser.RecordAtomicIntent(loserRequest, StableClaimActor(loserRequest.Actor, loserRequest.IdempotencyKey), now.Add(2*time.Minute))
	var conflict *ConcurrentMutationError
	if !errors.As(loserErr, &conflict) || conflict.BeadID != beadID {
		t.Fatalf("loser RecordAtomicIntent error=%v, want ConcurrentMutationError", loserErr)
	}
	for label, stored := range map[string]*Assignment{
		"loser memory": loser.Get(beadID),
		"durable":      mustLoadAssignment(t, session, beadID),
	} {
		if stored == nil || stored.IdempotencyKey != winnerRequest.IdempotencyKey || stored.PendingPrompt != winnerRequest.Prompt {
			t.Fatalf("%s did not retain durable winner: %+v", label, stored)
		}
	}
}

func TestAssignmentClearBarrierRetainsLeaseAndBlocksNewWork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const session = "assignment-clear-barrier"
	const beadID = "ntm-clear-barrier"
	now := time.Now().UTC()
	store := NewStore(session)
	store.Assignments[beadID] = &Assignment{
		BeadID: beadID, BeadTitle: "Clear barrier", Pane: 3,
		AgentType: "codex", AgentName: "CodexThree", Status: StatusAssigned, AssignedAt: now,
		IdempotencyKey: "clear-generation", ClaimActor: "clear-actor",
		DispatchTarget: "%63", OccupancyKey: "%63", DispatchState: DispatchSent,
		DispatchReceiptID: "clear-receipt", ReservationCompleted: true,
		ReservationAgent: "CodexThree", ReservationTarget: "%63",
		ReservedPaths: []string{"internal/assignment/**"}, ReservationIDs: []int{631, 632},
	}
	if err := store.Save(); err != nil {
		t.Fatalf("seed clear assignment: %v", err)
	}
	clearing, err := store.BeginClear(t.Context(), beadID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("BeginClear: %v", err)
	}
	if clearing.ClearState != ClearStateReservationReleasing || len(clearing.ReservationIDs) != 2 || clearing.DispatchReceiptID != "clear-receipt" {
		t.Fatalf("clear barrier lost durable lease or receipt: %+v", clearing)
	}

	claimer := &atomicClaimLedger{}
	dispatcher := &atomicDispatchRecorder{}
	request := atomicTestRequest("replacement-generation", "%63")
	request.BeadID = beadID
	result, assignErr := NewAtomicCoordinator(store, claimer, nil, dispatcher).Execute(t.Context(), request)
	if !errors.Is(assignErr, ErrClaimConflict) || result.Sent || claimer.calls != 0 || dispatcher.calls.Load() != 0 {
		t.Fatalf("assignment crossed clear barrier: result=%+v err=%v claims=%d dispatch=%d", result, assignErr, claimer.calls, dispatcher.calls.Load())
	}

	releaseErr := errors.New("Agent Mail unavailable")
	if err := store.RecordClearReleaseFailed(t.Context(), beadID, releaseErr); err != nil {
		t.Fatalf("RecordClearReleaseFailed: %v", err)
	}
	reloaded, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("reload failed clear: %v", err)
	}
	failed := reloaded.Get(beadID)
	if failed == nil || failed.ClearState != ClearStateReservationReleasing || failed.ClearError != releaseErr.Error() || len(failed.ReservationIDs) != 2 {
		t.Fatalf("failed clear did not retain retry metadata: %+v", failed)
	}
	if err := reloaded.CompleteClear(t.Context(), beadID); err != nil {
		t.Fatalf("CompleteClear: %v", err)
	}
	if got := mustLoadAssignment(t, session, beadID); got != nil {
		t.Fatalf("confirmed clear left assignment: %+v", got)
	}
}

func TestAssignmentClearRejectsUnknownDispatchOutcome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const beadID = "ntm-clear-sending"
	store := NewStore("assignment-clear-sending")
	request := AtomicRequest{
		BeadID: beadID, BeadTitle: "Sending", Target: "%71", OccupancyKey: "%71", Pane: 1,
		AgentType: "codex", AgentName: "CodexOne", Actor: "CodexOne", Prompt: "work", IdempotencyKey: "sending-key",
	}
	now := time.Now().UTC()
	actor := StableClaimActor(request.Actor, request.IdempotencyKey)
	if _, err := store.RecordAtomicIntent(request, actor, now); err != nil {
		t.Fatalf("RecordAtomicIntent: %v", err)
	}
	if err := store.RecordAtomicDispatchStarted(beadID, request.IdempotencyKey, now); err != nil {
		t.Fatalf("RecordAtomicDispatchStarted: %v", err)
	}
	_, err := store.BeginClear(t.Context(), beadID, now.Add(time.Second))
	if !errors.Is(err, ErrDispatchOutcomeUnknown) {
		t.Fatalf("BeginClear error=%v, want ErrDispatchOutcomeUnknown", err)
	}
	stored := store.Get(beadID)
	if stored == nil || stored.ClearState != ClearStateNone || stored.DispatchState != DispatchSending || stored.DispatchAttempts != 1 {
		t.Fatalf("rejected clear mutated dispatch barrier: %+v", stored)
	}
}

func mustLoadAssignment(t *testing.T, session, beadID string) *Assignment {
	t.Helper()
	store, err := LoadStoreStrict(session)
	if err != nil {
		t.Fatalf("LoadStoreStrict(%s): %v", session, err)
	}
	return store.Get(beadID)
}

func TestLoadNormalizesLegacyFailureReason(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := filepath.Join(tmpDir, ".ntm", "sessions", "legacy-failure-test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	raw := []byte(`{
  "session_name": "legacy-failure-test",
  "assignments": {
    "bd-legacy": {
      "bead_id": "bd-legacy",
      "bead_title": "Legacy failure bead",
      "pane": 1,
      "agent_type": "claude",
      "status": "failed",
      "assigned_at": "2026-01-01T00:00:00Z",
      "failed_at": "2026-01-01T00:05:00Z",
      "failure_reason": "legacy reason"
    }
  },
  "updated_at": "2026-01-01T00:05:00Z",
  "version": 1
}`)
	mainPath := filepath.Join(dir, "assignments.json")
	if err := os.WriteFile(mainPath, raw, 0644); err != nil {
		t.Fatalf("failed to write legacy file: %v", err)
	}

	store, err := LoadStore("legacy-failure-test")
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	got := store.Get("bd-legacy")
	if got == nil {
		t.Fatal("expected legacy assignment, got nil")
	}
	if got.FailReason != "legacy reason" {
		t.Fatalf("expected FailReason to be normalized, got %q", got.FailReason)
	}
	if got.FailureReason != "" {
		t.Fatalf("expected FailureReason to be cleared after normalization, got %q", got.FailureReason)
	}
}

func TestPersistenceMissingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Don't create directory - Load should handle it gracefully
	store, err := LoadStore("missing-dir-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have empty store
	if len(store.List()) != 0 {
		t.Errorf("expected empty store, got %d assignments", len(store.List()))
	}

	// Save should create directory
	_, err = store.Assign("bd-123", "Test", 1, "claude", "", "")
	if err != nil {
		t.Errorf("unexpected error assigning: %v", err)
	}

	// Verify directory was created
	dir := filepath.Join(tmpDir, ".ntm", "sessions", "missing-dir-test")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("concurrent-test")

	var wg sync.WaitGroup
	numGoroutines := 10
	assignmentsPerGoroutine := 5

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < assignmentsPerGoroutine; j++ {
				beadID := "bd-" + string(rune('A'+goroutineID)) + string(rune('0'+j))
				_, _ = store.Assign(beadID, "Test", goroutineID, "claude", "", "")
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < assignmentsPerGoroutine; j++ {
				_ = store.List()
				_ = store.Stats()
			}
		}()
	}

	wg.Wait()

	// Verify all assignments were created
	assignments := store.List()
	expectedCount := numGoroutines * assignmentsPerGoroutine
	if len(assignments) != expectedCount {
		t.Errorf("expected %d assignments, got %d", expectedCount, len(assignments))
	}
}

func TestNonExistentAssignment(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	store := NewStore("test-session")

	// Try to update non-existent assignment
	err := store.MarkWorking("bd-nonexistent")
	if err == nil {
		t.Error("expected error for non-existent assignment")
	}

	// Try to reassign non-existent assignment
	_, err = store.Reassign("bd-nonexistent", 2, "codex", "")
	if err == nil {
		t.Error("expected error for non-existent assignment")
	}
}

func TestStorageDir(t *testing.T) {
	// Test with HOME set
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	dir := StorageDir()
	// New behavior: ~/.ntm/sessions
	expected := filepath.Join(tmpDir, ".ntm", "sessions")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestPersistenceErrorTypes(t *testing.T) {
	// Test PersistenceError
	cause := os.ErrPermission
	perr := &PersistenceError{
		Operation: "save",
		Path:      "/test/path",
		Cause:     cause,
	}

	if perr.Unwrap() != cause {
		t.Error("expected Unwrap to return cause")
	}

	errStr := perr.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}

	// Test InvalidTransitionError
	iterr := &InvalidTransitionError{
		BeadID: "bd-123",
		From:   StatusAssigned,
		To:     StatusCompleted,
	}

	errStr = iterr.Error()
	if errStr == "" {
		t.Error("expected non-empty error string")
	}
}
