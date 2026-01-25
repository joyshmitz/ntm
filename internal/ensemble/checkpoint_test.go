package ensemble

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCheckpointStore(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}
	if store == nil {
		t.Fatal("store is nil")
	}

	// Verify directory was created
	checkpointDir := filepath.Join(tmpDir, checkpointDirName)
	if _, err := os.Stat(checkpointDir); os.IsNotExist(err) {
		t.Error("checkpoint directory was not created")
	}

	t.Logf("TEST: %s - assertion: checkpoint store created successfully", t.Name())
}

func TestCheckpointStore_SaveAndLoadCheckpoint(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-run-1"
	checkpoint := ModeCheckpoint{
		ModeID: "deductive",
		Output: &ModeOutput{
			ModeID: "deductive",
			Thesis: "Test thesis",
		},
		Status:      string(AssignmentDone),
		CapturedAt:  time.Now().UTC(),
		ContextHash: "abc123",
		TokensUsed:  1000,
	}

	// Save checkpoint
	if err := store.SaveCheckpoint(runID, checkpoint); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Load checkpoint
	loaded, err := store.LoadCheckpoint(runID, "deductive")
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if loaded.ModeID != checkpoint.ModeID {
		t.Errorf("ModeID = %q, want %q", loaded.ModeID, checkpoint.ModeID)
	}
	if loaded.Status != checkpoint.Status {
		t.Errorf("Status = %q, want %q", loaded.Status, checkpoint.Status)
	}
	if loaded.TokensUsed != checkpoint.TokensUsed {
		t.Errorf("TokensUsed = %d, want %d", loaded.TokensUsed, checkpoint.TokensUsed)
	}
	if loaded.Output == nil || loaded.Output.Thesis != checkpoint.Output.Thesis {
		t.Error("Output not loaded correctly")
	}

	t.Logf("TEST: %s - assertion: checkpoint save/load works", t.Name())
}

func TestCheckpointStore_SaveAndLoadMetadata(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	meta := CheckpointMetadata{
		SessionName: "test-session",
		Question:    "What is the meaning of life?",
		RunID:       "test-run-2",
		Status:      EnsembleActive,
		CreatedAt:   time.Now().UTC(),
		ContextHash: "def456",
		PendingIDs:  []string{"deductive", "inductive"},
		TotalModes:  2,
	}

	// Save metadata
	if err := store.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Load metadata
	loaded, err := store.LoadMetadata("test-run-2")
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	if loaded.SessionName != meta.SessionName {
		t.Errorf("SessionName = %q, want %q", loaded.SessionName, meta.SessionName)
	}
	if loaded.Question != meta.Question {
		t.Errorf("Question = %q, want %q", loaded.Question, meta.Question)
	}
	if len(loaded.PendingIDs) != len(meta.PendingIDs) {
		t.Errorf("PendingIDs count = %d, want %d", len(loaded.PendingIDs), len(meta.PendingIDs))
	}

	t.Logf("TEST: %s - assertion: metadata save/load works", t.Name())
}

func TestCheckpointStore_LoadCheckpoint_NotFound(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	_, err = store.LoadCheckpoint("nonexistent-run", "nonexistent-mode")
	if err == nil {
		t.Error("expected error for nonexistent checkpoint")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}

	t.Logf("TEST: %s - assertion: not found error returned", t.Name())
}

func TestCheckpointStore_LoadAllCheckpoints(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-run-3"
	modes := []string{"deductive", "inductive", "causal"}

	for _, mode := range modes {
		checkpoint := ModeCheckpoint{
			ModeID: mode,
			Status: string(AssignmentDone),
		}
		if err := store.SaveCheckpoint(runID, checkpoint); err != nil {
			t.Fatalf("SaveCheckpoint failed for %s: %v", mode, err)
		}
	}

	// Load all
	checkpoints, err := store.LoadAllCheckpoints(runID)
	if err != nil {
		t.Fatalf("LoadAllCheckpoints failed: %v", err)
	}

	if len(checkpoints) != len(modes) {
		t.Errorf("got %d checkpoints, want %d", len(checkpoints), len(modes))
	}

	t.Logf("TEST: %s - assertion: all checkpoints loaded", t.Name())
}

func TestCheckpointStore_ListRuns(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	// Create multiple runs
	runs := []string{"run-a", "run-b", "run-c"}
	for _, runID := range runs {
		meta := CheckpointMetadata{
			RunID:     runID,
			CreatedAt: time.Now().UTC(),
		}
		if err := store.SaveMetadata(meta); err != nil {
			t.Fatalf("SaveMetadata failed for %s: %v", runID, err)
		}
	}

	// List runs
	listed, err := store.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns failed: %v", err)
	}

	if len(listed) != len(runs) {
		t.Errorf("got %d runs, want %d", len(listed), len(runs))
	}

	t.Logf("TEST: %s - assertion: all runs listed", t.Name())
}

func TestCheckpointStore_DeleteRun(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-run-delete"
	meta := CheckpointMetadata{
		RunID:     runID,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Verify exists
	if !store.RunExists(runID) {
		t.Error("run should exist before delete")
	}

	// Delete
	if err := store.DeleteRun(runID); err != nil {
		t.Fatalf("DeleteRun failed: %v", err)
	}

	// Verify gone
	if store.RunExists(runID) {
		t.Error("run should not exist after delete")
	}

	t.Logf("TEST: %s - assertion: run deleted successfully", t.Name())
}

func TestCheckpointStore_RunExists(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	if store.RunExists("nonexistent") {
		t.Error("nonexistent run should return false")
	}

	runID := "existing-run"
	meta := CheckpointMetadata{RunID: runID}
	if err := store.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	if !store.RunExists(runID) {
		t.Error("existing run should return true")
	}

	t.Logf("TEST: %s - assertion: RunExists works correctly", t.Name())
}

func TestCheckpointStore_UpdateModeStatus(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-run-status"
	meta := CheckpointMetadata{
		RunID:      runID,
		PendingIDs: []string{"mode-a", "mode-b"},
	}
	if err := store.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Update mode-a to done
	if err := store.UpdateModeStatus(runID, "mode-a", string(AssignmentDone)); err != nil {
		t.Fatalf("UpdateModeStatus failed: %v", err)
	}

	// Verify
	loaded, err := store.LoadMetadata(runID)
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	if len(loaded.CompletedIDs) != 1 || loaded.CompletedIDs[0] != "mode-a" {
		t.Errorf("CompletedIDs = %v, want [mode-a]", loaded.CompletedIDs)
	}
	if len(loaded.PendingIDs) != 1 || loaded.PendingIDs[0] != "mode-b" {
		t.Errorf("PendingIDs = %v, want [mode-b]", loaded.PendingIDs)
	}

	t.Logf("TEST: %s - assertion: mode status updated correctly", t.Name())
}

func TestCheckpointStore_GetCompletedOutputs(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-run-outputs"

	// Save completed checkpoint
	completedCP := ModeCheckpoint{
		ModeID: "completed-mode",
		Output: &ModeOutput{ModeID: "completed-mode", Thesis: "Done"},
		Status: string(AssignmentDone),
	}
	if err := store.SaveCheckpoint(runID, completedCP); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Save error checkpoint
	errorCP := ModeCheckpoint{
		ModeID: "error-mode",
		Status: string(AssignmentError),
		Error:  "something failed",
	}
	if err := store.SaveCheckpoint(runID, errorCP); err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Get completed outputs
	outputs, err := store.GetCompletedOutputs(runID)
	if err != nil {
		t.Fatalf("GetCompletedOutputs failed: %v", err)
	}

	if len(outputs) != 1 {
		t.Errorf("got %d completed outputs, want 1", len(outputs))
	}
	if outputs[0].ModeID != "completed-mode" {
		t.Errorf("output ModeID = %q, want completed-mode", outputs[0].ModeID)
	}

	t.Logf("TEST: %s - assertion: only completed outputs returned", t.Name())
}

func TestCheckpointManager_Initialize(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	manager := NewCheckpointManager(store, "test-manager-run")

	session := &EnsembleSession{
		SessionName: "test-session",
		Question:    "Test question?",
		Assignments: []ModeAssignment{
			{ModeID: "mode-1"},
			{ModeID: "mode-2"},
		},
		Status:    EnsembleActive,
		CreatedAt: time.Now().UTC(),
	}

	if err := manager.Initialize(session, "context-hash-123"); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify metadata was created
	meta, err := store.LoadMetadata("test-manager-run")
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}

	if meta.SessionName != session.SessionName {
		t.Errorf("SessionName = %q, want %q", meta.SessionName, session.SessionName)
	}
	if len(meta.PendingIDs) != 2 {
		t.Errorf("PendingIDs count = %d, want 2", len(meta.PendingIDs))
	}

	t.Logf("TEST: %s - assertion: checkpoint manager initialized", t.Name())
}

func TestCheckpointManager_RecordOutput(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	runID := "test-record-run"
	manager := NewCheckpointManager(store, runID)

	// Initialize with metadata
	meta := CheckpointMetadata{
		RunID:      runID,
		PendingIDs: []string{"mode-1"},
	}
	if err := store.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	// Record output
	output := &ModeOutput{
		ModeID: "mode-1",
		Thesis: "Test output",
	}
	if err := manager.RecordOutput("mode-1", output, 500, "ctx-hash"); err != nil {
		t.Fatalf("RecordOutput failed: %v", err)
	}

	// Verify checkpoint was saved
	cp, err := store.LoadCheckpoint(runID, "mode-1")
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}

	if cp.Status != string(AssignmentDone) {
		t.Errorf("Status = %q, want %q", cp.Status, string(AssignmentDone))
	}
	if cp.TokensUsed != 500 {
		t.Errorf("TokensUsed = %d, want 500", cp.TokensUsed)
	}

	t.Logf("TEST: %s - assertion: output recorded successfully", t.Name())
}

func TestCheckpointManager_IsResumable(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tmpDir := t.TempDir()
	store, err := NewCheckpointStore(tmpDir)
	if err != nil {
		t.Fatalf("NewCheckpointStore failed: %v", err)
	}

	// Test with pending modes
	runID1 := "resumable-run"
	meta1 := CheckpointMetadata{
		RunID:      runID1,
		PendingIDs: []string{"mode-1"},
	}
	if err := store.SaveMetadata(meta1); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	manager1 := NewCheckpointManager(store, runID1)
	if !manager1.IsResumable() {
		t.Error("run with pending modes should be resumable")
	}

	// Test with all completed
	runID2 := "complete-run"
	meta2 := CheckpointMetadata{
		RunID:        runID2,
		CompletedIDs: []string{"mode-1"},
	}
	if err := store.SaveMetadata(meta2); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	manager2 := NewCheckpointManager(store, runID2)
	if manager2.IsResumable() {
		t.Error("fully completed run should not be resumable")
	}

	t.Logf("TEST: %s - assertion: IsResumable works correctly", t.Name())
}

func TestCheckpointStore_NilReceiver(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	var store *CheckpointStore

	if _, err := store.LoadCheckpoint("run", "mode"); err == nil {
		t.Error("LoadCheckpoint on nil should return error")
	}
	if _, err := store.LoadMetadata("run"); err == nil {
		t.Error("LoadMetadata on nil should return error")
	}
	if err := store.SaveCheckpoint("run", ModeCheckpoint{}); err == nil {
		t.Error("SaveCheckpoint on nil should return error")
	}
	if err := store.SaveMetadata(CheckpointMetadata{}); err == nil {
		t.Error("SaveMetadata on nil should return error")
	}
	if store.RunExists("run") {
		t.Error("RunExists on nil should return false")
	}

	t.Logf("TEST: %s - assertion: nil receiver handling works", t.Name())
}

func TestSliceContains(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	slice := []string{"a", "b", "c"}

	if !sliceContains(slice, "a") {
		t.Error("sliceContains should return true for existing item")
	}
	if !sliceContains(slice, "c") {
		t.Error("sliceContains should return true for existing item")
	}
	if sliceContains(slice, "d") {
		t.Error("sliceContains should return false for non-existing item")
	}
	if sliceContains(nil, "a") {
		t.Error("sliceContains should return false for nil slice")
	}
	if sliceContains([]string{}, "a") {
		t.Error("sliceContains should return false for empty slice")
	}

	t.Logf("TEST: %s - assertion: sliceContains works correctly", t.Name())
}

func TestRemoveFromSlice(t *testing.T) {
	t.Logf("TEST: %s - starting", t.Name())

	tests := []struct {
		slice  []string
		item   string
		expect []string
	}{
		{[]string{"a", "b", "c"}, "b", []string{"a", "c"}},
		{[]string{"a", "b", "c"}, "a", []string{"b", "c"}},
		{[]string{"a", "b", "c"}, "c", []string{"a", "b"}},
		{[]string{"a", "b", "c"}, "d", []string{"a", "b", "c"}},
		{[]string{}, "a", []string{}},
		{nil, "a", []string{}},
	}

	for _, tt := range tests {
		result := removeFromSlice(tt.slice, tt.item)
		if len(result) != len(tt.expect) {
			t.Errorf("removeFromSlice(%v, %q) = %v, want %v", tt.slice, tt.item, result, tt.expect)
		}
	}

	t.Logf("TEST: %s - assertion: removeFromSlice works correctly", t.Name())
}
