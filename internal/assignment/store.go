// Package assignment provides assignment tracking for bead-to-agent mappings.
package assignment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/events"
	"github.com/Dicklesworthstone/ntm/internal/util"
)

// ErrAssignmentStatusMismatch means a guarded mutation reloaded a different
// lifecycle state than the caller selected before acquiring the bead lock.
var ErrAssignmentStatusMismatch = errors.New("assignment status changed")

const (
	// assignmentsDirName is the directory name for assignment storage
	assignmentsDirName     = "assignments"
	fileExtension          = ".json"
	assignmentStoreVersion = 7
)

// AssignmentStatus represents the current state of an assignment
type AssignmentStatus string

// AssignmentClearState tracks the external lease-release boundary for clear.
type AssignmentClearState string

const (
	StatusClaiming   AssignmentStatus = "claiming"   // Durable intent exists; external claim outcome may need reconciliation
	StatusClaimed    AssignmentStatus = "claimed"    // Bead claimed; reservation/dispatch may still be pending
	StatusAssigned   AssignmentStatus = "assigned"   // Prompt sent, waiting to start
	StatusWorking    AssignmentStatus = "working"    // Agent actively working
	StatusCompleted  AssignmentStatus = "completed"  // Bead closed successfully
	StatusFailed     AssignmentStatus = "failed"     // Agent crashed or gave up
	StatusReassigned AssignmentStatus = "reassigned" // Moved to different agent

	ClearStateNone                 AssignmentClearState = ""
	ClearStateReservationReleasing AssignmentClearState = "reservation_releasing"
	ClearStateLeasesReleased       AssignmentClearState = "leases_released"
)

// Assignment represents a bead assigned to an agent
type Assignment struct {
	BeadID        string           `json:"bead_id"`
	BeadTitle     string           `json:"bead_title"`
	Pane          int              `json:"pane"`
	AgentType     string           `json:"agent_type"`           // claude, codex, gemini
	AgentName     string           `json:"agent_name,omitempty"` // Agent Mail name if registered
	Status        AssignmentStatus `json:"status"`
	AssignedAt    time.Time        `json:"assigned_at"`
	StartedAt     *time.Time       `json:"started_at,omitempty"` // When agent started working
	CompletedAt   *time.Time       `json:"completed_at,omitempty"`
	FailedAt      *time.Time       `json:"failed_at,omitempty"`
	FailReason    string           `json:"fail_reason,omitempty"`
	FailureReason string           `json:"failure_reason,omitempty"` // Detailed failure reason
	RetryCount    int              `json:"retry_count,omitempty"`    // Number of retry attempts
	PromptSent    string           `json:"prompt_sent,omitempty"`    // The actual prompt sent

	// Atomic assignment metadata is persisted before each external boundary so
	// retries can distinguish completed, recoverable, and outcome-unknown work.
	IdempotencyKey           string               `json:"idempotency_key,omitempty"`
	ClaimActor               string               `json:"claim_actor,omitempty"`
	ClaimState               ClaimState           `json:"claim_state,omitempty"`
	ClaimStatus              string               `json:"claim_status,omitempty"`
	ClaimAttempts            int                  `json:"claim_attempts,omitempty"`
	ClaimStartedAt           *time.Time           `json:"claim_started_at,omitempty"`
	ClaimError               string               `json:"claim_error,omitempty"`
	ClaimedAt                *time.Time           `json:"claimed_at,omitempty"`
	ClaimRequiresNonTerminal bool                 `json:"claim_requires_non_terminal,omitempty"`
	ReservationRequired      bool                 `json:"reservation_required,omitempty"`
	ReservationDiscovery     bool                 `json:"reservation_discovery,omitempty"`
	ReservationInputPaths    []string             `json:"reservation_input_paths,omitempty"`
	ReservationState         ReservationState     `json:"reservation_state,omitempty"`
	ReservationAttempts      int                  `json:"reservation_attempts,omitempty"`
	ReservationStartedAt     *time.Time           `json:"reservation_started_at,omitempty"`
	ReservationCompleted     bool                 `json:"reservation_completed,omitempty"`
	ReservationAgent         string               `json:"reservation_agent,omitempty"`
	ReservationTarget        string               `json:"reservation_target,omitempty"`
	ReservationRequested     []string             `json:"reservation_requested,omitempty"`
	ReservedPaths            []string             `json:"reserved_paths,omitempty"`
	ReservationIDs           []int                `json:"reservation_ids,omitempty"`
	ReservationExpiresAt     *time.Time           `json:"reservation_expires_at,omitempty"`
	ReservationError         string               `json:"reservation_error,omitempty"`
	DispatchState            DispatchState        `json:"dispatch_state,omitempty"`
	DispatchTarget           string               `json:"dispatch_target,omitempty"`
	OccupancyKey             string               `json:"occupancy_key,omitempty"`
	PromptSHA256             string               `json:"prompt_sha256,omitempty"`
	IntentSHA256             string               `json:"intent_sha256,omitempty"`
	PendingPrompt            string               `json:"pending_prompt,omitempty"`
	DispatchAttempts         int                  `json:"dispatch_attempts,omitempty"`
	DispatchStartedAt        *time.Time           `json:"dispatch_started_at,omitempty"`
	DispatchedAt             *time.Time           `json:"dispatched_at,omitempty"`
	DispatchReceiptID        string               `json:"dispatch_receipt_id,omitempty"`
	DispatchDuration         time.Duration        `json:"dispatch_duration,omitempty"`
	LastDispatchError        string               `json:"last_dispatch_error,omitempty"`
	ClearState               AssignmentClearState `json:"clear_state,omitempty"`
	ClearStartedAt           *time.Time           `json:"clear_started_at,omitempty"`
	ClearError               string               `json:"clear_error,omitempty"`
}

// AssignmentUpdate describes mutable assignment metadata that can be updated
// after the initial assignment record is created.
type AssignmentUpdate struct {
	PromptSent *string
	RetryCount *int
}

// ReassignmentTarget identifies the physical pane and agent that will own the
// replacement generation of an assignment.
type ReassignmentTarget struct {
	Pane           int
	AgentType      string
	AgentName      string
	DispatchTarget string
	OccupancyKey   string
}

func normalizeFailureReason(a *Assignment) {
	if a == nil {
		return
	}
	if strings.TrimSpace(a.FailReason) == "" && strings.TrimSpace(a.FailureReason) != "" {
		a.FailReason = a.FailureReason
	}
	if strings.TrimSpace(a.FailReason) != "" {
		a.FailureReason = ""
	}
}

func cloneTimePtr(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	cloned := *ts
	return &cloned
}

func cloneAssignment(a *Assignment) *Assignment {
	if a == nil {
		return nil
	}
	cloned := *a
	cloned.StartedAt = cloneTimePtr(a.StartedAt)
	cloned.CompletedAt = cloneTimePtr(a.CompletedAt)
	cloned.FailedAt = cloneTimePtr(a.FailedAt)
	cloned.ClaimedAt = cloneTimePtr(a.ClaimedAt)
	cloned.ClaimStartedAt = cloneTimePtr(a.ClaimStartedAt)
	cloned.ReservationExpiresAt = cloneTimePtr(a.ReservationExpiresAt)
	cloned.ReservationStartedAt = cloneTimePtr(a.ReservationStartedAt)
	cloned.DispatchStartedAt = cloneTimePtr(a.DispatchStartedAt)
	cloned.DispatchedAt = cloneTimePtr(a.DispatchedAt)
	cloned.ClearStartedAt = cloneTimePtr(a.ClearStartedAt)
	cloned.ReservationRequested = append([]string(nil), a.ReservationRequested...)
	cloned.ReservationInputPaths = append([]string(nil), a.ReservationInputPaths...)
	cloned.ReservedPaths = append([]string(nil), a.ReservedPaths...)
	cloned.ReservationIDs = append([]int(nil), a.ReservationIDs...)
	normalizeFailureReason(&cloned)
	return &cloned
}

// AssignmentStore manages bead-to-agent assignments for a session
type AssignmentStore struct {
	SessionName        string                 `json:"session_name"`
	Assignments        map[string]*Assignment `json:"assignments"`                   // bead_id -> active or terminal assignment
	ClearedGenerations map[string]uint64      `json:"cleared_generations,omitempty"` // bead_id -> completed explicit clears
	UpdatedAt          time.Time              `json:"updated_at"`
	Version            int                    `json:"version"` // Schema version for migrations

	mutex    sync.RWMutex
	path     string                 // Path to persistence file
	baseline map[string]*Assignment // Last disk snapshot used to derive local deltas
	replace  map[string]struct{}    // Beads intentionally replaced as whole records
}

// PersistenceError represents an error during persistence operations
type PersistenceError struct {
	Operation string
	Path      string
	Cause     error
}

func (e *PersistenceError) Error() string {
	return fmt.Sprintf("[ASSIGN] %s failed at %s: %v", e.Operation, e.Path, e.Cause)
}

func (e *PersistenceError) Unwrap() error {
	return e.Cause
}

// InvalidTransitionError represents an invalid state transition
type InvalidTransitionError struct {
	BeadID string
	From   AssignmentStatus
	To     AssignmentStatus
}

// ConcurrentMutationError reports a destructive mutation based on a stale
// ledger snapshot. Retrying requires reloading and re-evaluating the latest
// assignment so a newer atomic barrier or receipt is never overwritten.
type ConcurrentMutationError struct {
	BeadID    string
	Operation string
}

func (e *ConcurrentMutationError) Error() string {
	return fmt.Sprintf("[ASSIGN] Concurrent %s conflict for %s; reload assignment state", e.Operation, e.BeadID)
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("[ASSIGN] Invalid transition %s -> %s for %s", e.From, e.To, e.BeadID)
}

// StorageDir returns the path to the assignment storage directory.
// Uses ~/.ntm/sessions/ (assignments are stored within session directories).
func StorageDir() string {
	ntmDir, err := util.NTMDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ntm", "sessions")
	}
	return filepath.Join(ntmDir, "sessions")
}

// NewStore creates a new AssignmentStore for a session
func NewStore(sessionName string) *AssignmentStore {
	// Store assignments inside the session directory: ~/.ntm/sessions/<session>/assignments.json
	baseDir := StorageDir()
	sessionDir := filepath.Join(baseDir, sessionName)

	// Ensure session directory exists (it might not if we are just creating assignments before session save)
	_ = os.MkdirAll(sessionDir, 0700)

	return &AssignmentStore{
		SessionName:        sessionName,
		Assignments:        make(map[string]*Assignment),
		ClearedGenerations: make(map[string]uint64),
		UpdatedAt:          time.Now().UTC(),
		Version:            assignmentStoreVersion,
		path:               filepath.Join(sessionDir, assignmentsDirName+fileExtension),
		baseline:           make(map[string]*Assignment),
		replace:            make(map[string]struct{}),
	}
}

// LoadStore loads an AssignmentStore from disk, creating a new one if it doesn't exist
func LoadStore(sessionName string) (*AssignmentStore, error) {
	store := NewStore(sessionName)
	if err := store.Load(); err != nil {
		// If load fails, start fresh
		return store, nil
	}
	return store, nil
}

// LoadStoreStrict loads the durable assignment ledger without treating
// corruption as an empty store. Mutating orchestration paths must use this so
// a lost `sending` marker cannot turn an ambiguous delivery into a duplicate.
func LoadStoreStrict(sessionName string) (*AssignmentStore, error) {
	store := NewStore(sessionName)
	if err := store.LoadStrict(); err != nil {
		return nil, err
	}
	return store, nil
}

// LoadStrict is the fail-closed counterpart to Load. A missing store is a
// valid empty ledger only when no backup exists. Mutating callers never fall
// back to a backup because it may predate a persisted sending barrier.
func (s *AssignmentStore) LoadStrict() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	unlock, err := acquireStoreFileLock(s.path)
	if err != nil {
		return &PersistenceError{Operation: "lock for load", Path: s.path, Cause: err}
	}
	defer unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			return &PersistenceError{Operation: "load", Path: s.path, Cause: err}
		}
		bakPath := s.path + ".bak"
		if _, backupErr := os.Stat(bakPath); backupErr == nil {
			return &PersistenceError{
				Operation: "load",
				Path:      s.path,
				Cause:     fmt.Errorf("primary ledger is missing while backup %s exists", bakPath),
			}
		} else if !os.IsNotExist(backupErr) {
			return &PersistenceError{Operation: "inspect backup", Path: bakPath, Cause: backupErr}
		}
		return nil
	}

	var loaded AssignmentStore
	if err := json.Unmarshal(data, &loaded); err != nil {
		return &PersistenceError{Operation: "parse primary ledger", Path: s.path, Cause: err}
	}

	s.SessionName = loaded.SessionName
	s.Assignments = loaded.Assignments
	s.ClearedGenerations = loaded.ClearedGenerations
	s.UpdatedAt = loaded.UpdatedAt
	s.Version = loaded.Version
	if s.Version < assignmentStoreVersion {
		s.Version = assignmentStoreVersion
	}
	if s.Assignments == nil {
		s.Assignments = make(map[string]*Assignment)
	}
	if s.ClearedGenerations == nil {
		s.ClearedGenerations = make(map[string]uint64)
	}
	for _, assignment := range s.Assignments {
		normalizeFailureReason(assignment)
	}
	s.baseline = cloneAssignmentMap(s.Assignments)
	s.replace = make(map[string]struct{})
	return nil
}

// Load reads the assignment store from disk
func (s *AssignmentStore) Load() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	unlock, lockErr := acquireStoreFileLock(s.path)
	if lockErr != nil {
		return &PersistenceError{Operation: "lock for load", Path: s.path, Cause: lockErr}
	}
	defer unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try backup
			bakPath := s.path + ".bak"
			data, err = os.ReadFile(bakPath)
			if err != nil {
				// Start fresh - not an error
				return nil
			}
			// Log recovery from backup
			slog.Warn("recovered assignment store from backup", "path", bakPath)
		} else {
			return &PersistenceError{Operation: "load", Path: s.path, Cause: err}
		}
	}

	var loaded AssignmentStore
	if err := json.Unmarshal(data, &loaded); err != nil {
		// Try backup on corrupt JSON
		bakPath := s.path + ".bak"
		data, bakErr := os.ReadFile(bakPath)
		if bakErr != nil {
			// Start fresh
			slog.Warn("assignment store corrupted, starting fresh", "error", err)
			return nil
		}
		if err := json.Unmarshal(data, &loaded); err != nil {
			slog.Warn("assignment store and backup corrupted, starting fresh", "error", err)
			return nil
		}
		slog.Warn("assignment store corrupted, recovered from backup")
	}

	s.SessionName = loaded.SessionName
	s.Assignments = loaded.Assignments
	s.ClearedGenerations = loaded.ClearedGenerations
	s.UpdatedAt = loaded.UpdatedAt
	s.Version = loaded.Version
	if s.Version < assignmentStoreVersion {
		s.Version = assignmentStoreVersion
	}

	if s.Assignments == nil {
		s.Assignments = make(map[string]*Assignment)
	}
	if s.ClearedGenerations == nil {
		s.ClearedGenerations = make(map[string]uint64)
	}
	for _, assignment := range s.Assignments {
		normalizeFailureReason(assignment)
	}
	s.baseline = cloneAssignmentMap(s.Assignments)
	s.replace = make(map[string]struct{})

	return nil
}

// Save writes the assignment store to disk with backup
func (s *AssignmentStore) Save() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.saveLocked()
}

// saveLocked performs the actual save (must hold lock)
func (s *AssignmentStore) saveLocked() error {
	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return &PersistenceError{Operation: "save", Path: s.path, Cause: fmt.Errorf("create directory: %w", err)}
	}
	unlock, err := acquireStoreFileLock(s.path)
	if err != nil {
		return &PersistenceError{Operation: "lock for save", Path: s.path, Cause: err}
	}
	defer unlock()

	latest, latestClearedGenerations, err := readAssignmentStateForMerge(s.path)
	if err != nil {
		return &PersistenceError{Operation: "reload before save", Path: s.path, Cause: err}
	}
	merged, mergeErr := mergeAssignmentDeltas(latest, s.baseline, s.Assignments, s.replace)
	if mergeErr != nil {
		s.Assignments = cloneAssignmentMap(latest)
		s.ClearedGenerations = cloneClearedGenerationMap(latestClearedGenerations)
		s.baseline = cloneAssignmentMap(latest)
		s.replace = make(map[string]struct{})
		return &PersistenceError{Operation: "merge before save", Path: s.path, Cause: mergeErr}
	}
	mergedClearedGenerations := mergeClearedGenerations(latestClearedGenerations, s.ClearedGenerations)

	updatedAt := time.Now().UTC()

	snapshot := &AssignmentStore{
		SessionName:        s.SessionName,
		Assignments:        merged,
		ClearedGenerations: mergedClearedGenerations,
		UpdatedAt:          updatedAt,
		Version:            assignmentStoreVersion,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return &PersistenceError{Operation: "save", Path: s.path, Cause: fmt.Errorf("marshal: %w", err)}
	}

	// A unique temp file avoids collisions between independent processes even
	// before the advisory lock is acquired on unusual filesystems.
	tmpFile, err := os.CreateTemp(dir, ".assignments-*.tmp")
	if err != nil {
		return &PersistenceError{Operation: "save", Path: dir, Cause: err}
	}
	tmpPath := tmpFile.Name()
	if chmodErr := tmpFile.Chmod(0600); chmodErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &PersistenceError{Operation: "save", Path: tmpPath, Cause: chmodErr}
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &PersistenceError{Operation: "save", Path: tmpPath, Cause: err}
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return &PersistenceError{Operation: "sync", Path: tmpPath, Cause: err}
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return &PersistenceError{Operation: "save", Path: tmpPath, Cause: err}
	}
	defer func() { _ = os.Remove(tmpPath) }()

	// Publish the same new snapshot to the backup before the primary. If the
	// process stops between these renames, the still-valid primary is older but
	// cannot have crossed an external side-effect boundary that follows Save.
	bakPath := s.path + ".bak"
	backupTemp, err := os.CreateTemp(dir, ".assignments-backup-*.tmp")
	if err != nil {
		return &PersistenceError{Operation: "save backup", Path: dir, Cause: err}
	}
	backupTempPath := backupTemp.Name()
	if chmodErr := backupTemp.Chmod(0600); chmodErr != nil {
		_ = backupTemp.Close()
		_ = os.Remove(backupTempPath)
		return &PersistenceError{Operation: "save backup", Path: backupTempPath, Cause: chmodErr}
	}
	if _, writeErr := backupTemp.Write(data); writeErr != nil {
		_ = backupTemp.Close()
		_ = os.Remove(backupTempPath)
		return &PersistenceError{Operation: "save backup", Path: backupTempPath, Cause: writeErr}
	}
	if syncErr := backupTemp.Sync(); syncErr != nil {
		_ = backupTemp.Close()
		_ = os.Remove(backupTempPath)
		return &PersistenceError{Operation: "sync backup", Path: backupTempPath, Cause: syncErr}
	}
	if closeErr := backupTemp.Close(); closeErr != nil {
		_ = os.Remove(backupTempPath)
		return &PersistenceError{Operation: "save backup", Path: backupTempPath, Cause: closeErr}
	}
	defer func() { _ = os.Remove(backupTempPath) }()
	if err := os.Rename(backupTempPath, bakPath); err != nil {
		return &PersistenceError{Operation: "save backup", Path: bakPath, Cause: err}
	}
	if err := syncAssignmentDirectory(dir); err != nil {
		return &PersistenceError{Operation: "sync backup directory", Path: dir, Cause: err}
	}

	// Rename temp to current
	if err := os.Rename(tmpPath, s.path); err != nil {
		return &PersistenceError{Operation: "save", Path: s.path, Cause: err}
	}
	if err := syncAssignmentDirectory(dir); err != nil {
		return &PersistenceError{Operation: "sync directory", Path: dir, Cause: err}
	}
	s.Assignments = merged
	s.ClearedGenerations = mergedClearedGenerations
	s.baseline = cloneAssignmentMap(merged)
	s.replace = make(map[string]struct{})
	s.UpdatedAt = updatedAt
	s.Version = assignmentStoreVersion

	return nil
}

func cloneAssignmentMap(input map[string]*Assignment) map[string]*Assignment {
	cloned := make(map[string]*Assignment, len(input))
	for beadID, value := range input {
		cloned[beadID] = cloneAssignment(value)
	}
	return cloned
}

func cloneClearedGenerationMap(input map[string]uint64) map[string]uint64 {
	cloned := make(map[string]uint64, len(input))
	for beadID, generation := range input {
		cloned[beadID] = generation
	}
	return cloned
}

func mergeClearedGenerations(latest, current map[string]uint64) map[string]uint64 {
	merged := cloneClearedGenerationMap(latest)
	for beadID, generation := range current {
		if generation > merged[beadID] {
			merged[beadID] = generation
		}
	}
	return merged
}

func readAssignmentStateForMerge(path string) (map[string]*Assignment, map[string]uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if _, backupErr := os.Stat(path + ".bak"); backupErr == nil {
				return nil, nil, fmt.Errorf("primary ledger is missing while backup %s exists", path+".bak")
			} else if !os.IsNotExist(backupErr) {
				return nil, nil, backupErr
			}
			return make(map[string]*Assignment), make(map[string]uint64), nil
		}
		return nil, nil, err
	}
	var loaded AssignmentStore
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, nil, err
	}
	if loaded.Assignments == nil {
		loaded.Assignments = make(map[string]*Assignment)
	}
	if loaded.ClearedGenerations == nil {
		loaded.ClearedGenerations = make(map[string]uint64)
	}
	return cloneAssignmentMap(loaded.Assignments), cloneClearedGenerationMap(loaded.ClearedGenerations), nil
}

func mergeAssignmentDeltas(latest, baseline, current map[string]*Assignment, replacements map[string]struct{}) (map[string]*Assignment, error) {
	merged := cloneAssignmentMap(latest)
	for beadID, previous := range baseline {
		if _, stillPresent := current[beadID]; !stillPresent {
			if durable, exists := latest[beadID]; exists && !reflect.DeepEqual(durable, previous) {
				return nil, &ConcurrentMutationError{BeadID: beadID, Operation: "remove"}
			}
			delete(merged, beadID)
		}
	}
	for beadID, value := range current {
		if previous, existed := baseline[beadID]; !existed || !reflect.DeepEqual(previous, value) {
			if !existed {
				if durable, createdConcurrently := latest[beadID]; createdConcurrently && !reflect.DeepEqual(durable, value) {
					return nil, &ConcurrentMutationError{BeadID: beadID, Operation: "create"}
				}
				merged[beadID] = cloneAssignment(value)
				continue
			}
			if _, replace := replacements[beadID]; replace {
				if durable, exists := latest[beadID]; !exists || !reflect.DeepEqual(durable, previous) {
					return nil, &ConcurrentMutationError{BeadID: beadID, Operation: "replace"}
				}
				merged[beadID] = cloneAssignment(value)
				continue
			}
			durable, exists := latest[beadID]
			if !exists {
				return nil, &ConcurrentMutationError{BeadID: beadID, Operation: "update removed assignment"}
			}
			if !sameAssignmentGeneration(previous, durable) {
				return nil, &ConcurrentMutationError{BeadID: beadID, Operation: "update superseded generation"}
			}
			merged[beadID] = mergeAssignmentDelta(durable, previous, value)
		}
	}
	return merged, nil
}

func sameAssignmentGeneration(previous, durable *Assignment) bool {
	if previous == nil || durable == nil {
		return false
	}
	previousKey := strings.TrimSpace(previous.IdempotencyKey)
	durableKey := strings.TrimSpace(durable.IdempotencyKey)
	if previousKey != "" || durableKey != "" {
		return previousKey != "" && previousKey == durableKey
	}
	return previous.AssignedAt.Equal(durable.AssignedAt)
}

// mergeAssignmentDelta applies only fields changed from the caller's baseline
// onto the latest durable record. This prevents a stale lifecycle writer from
// erasing a newer dispatch barrier or receipt for the same bead.
func mergeAssignmentDelta(latest, baseline, current *Assignment) *Assignment {
	if current == nil {
		return nil
	}
	if latest == nil {
		return cloneAssignment(current)
	}
	if baseline == nil {
		baseline = &Assignment{}
	}

	merged := cloneAssignment(latest)
	baselineValue := reflect.ValueOf(baseline).Elem()
	currentValue := reflect.ValueOf(current).Elem()
	mergedValue := reflect.ValueOf(merged).Elem()
	assignmentType := currentValue.Type()
	localStatusChanged := current.Status != baseline.Status

	for i := 0; i < currentValue.NumField(); i++ {
		fieldName := assignmentType.Field(i).Name
		if fieldName == "Status" {
			continue
		}
		if localStatusChanged && isAssignmentLifecycleField(fieldName) {
			continue
		}
		if !reflect.DeepEqual(baselineValue.Field(i).Interface(), currentValue.Field(i).Interface()) {
			mergedValue.Field(i).Set(currentValue.Field(i))
		}
	}

	if localStatusChanged && shouldApplyAssignmentStatusDelta(baseline.Status, latest.Status, current.Status) {
		merged.Status = current.Status
		merged.StartedAt = cloneTimePtr(current.StartedAt)
		merged.CompletedAt = cloneTimePtr(current.CompletedAt)
		merged.FailedAt = cloneTimePtr(current.FailedAt)
		merged.FailReason = current.FailReason
		merged.FailureReason = current.FailureReason
	}
	return cloneAssignment(merged)
}

func isAssignmentLifecycleField(name string) bool {
	switch name {
	case "Status", "StartedAt", "CompletedAt", "FailedAt", "FailReason", "FailureReason":
		return true
	default:
		return false
	}
}

func shouldApplyAssignmentStatusDelta(baseline, latest, current AssignmentStatus) bool {
	if latest == baseline {
		return true
	}
	if isTerminalAssignmentStatus(latest) {
		return false
	}
	if isTerminalAssignmentStatus(current) {
		return true
	}
	return assignmentStatusRank(current) > assignmentStatusRank(latest)
}

func isTerminalAssignmentStatus(status AssignmentStatus) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusReassigned:
		return true
	default:
		return false
	}
}

func assignmentStatusRank(status AssignmentStatus) int {
	switch status {
	case StatusClaiming:
		return 1
	case StatusClaimed:
		return 2
	case StatusAssigned:
		return 3
	case StatusWorking:
		return 4
	case StatusCompleted, StatusFailed, StatusReassigned:
		return 5
	default:
		return 0
	}
}

// Assign creates or updates an assignment for a bead
func (s *AssignmentStore) Assign(beadID, beadTitle string, pane int, agentType, agentName, prompt string) (*Assignment, error) {
	s.mutex.Lock()
	if _, exists := s.Assignments[beadID]; exists {
		if s.replace == nil {
			s.replace = make(map[string]struct{})
		}
		s.replace[beadID] = struct{}{}
	}

	now := time.Now().UTC()
	assignment := &Assignment{
		BeadID:     beadID,
		BeadTitle:  beadTitle,
		Pane:       pane,
		AgentType:  agentType,
		AgentName:  agentName,
		Status:     StatusAssigned,
		AssignedAt: now,
		PromptSent: prompt,
	}

	s.Assignments[beadID] = assignment

	// Persist immediately
	if err := s.saveLocked(); err != nil {
		s.mutex.Unlock()
		return nil, err
	}

	cloned := cloneAssignment(assignment)
	s.mutex.Unlock()

	events.DefaultEmitter().Emit(events.NewWebhookEvent(
		events.WebhookBeadAssigned,
		s.SessionName,
		fmt.Sprintf("%d", pane),
		agentType,
		fmt.Sprintf("Bead assigned: %s", beadID),
		map[string]string{
			"bead_id":     beadID,
			"bead_title":  beadTitle,
			"pane_index":  fmt.Sprintf("%d", pane),
			"agent_type":  agentType,
			"agent_name":  agentName,
			"retry_count": fmt.Sprintf("%d", cloned.RetryCount),
		},
	))

	return cloned, nil
}

// Get retrieves an assignment by bead ID
func (s *AssignmentStore) Get(beadID string) *Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return cloneAssignment(s.Assignments[beadID])
}

// ClearedGeneration returns the number of completed explicit clears for a
// bead. It is retained after the assignment row is removed so a deliberate
// post-clear assignment can derive a fresh, replay-stable identity.
func (s *AssignmentStore) ClearedGeneration(beadID string) uint64 {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.ClearedGenerations[beadID]
}

// GetAll returns all assignments as values
func (s *AssignmentStore) GetAll() []Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []Assignment
	for _, a := range s.Assignments {
		result = append(result, *cloneAssignment(a))
	}
	return result
}

// List returns all assignments
func (s *AssignmentStore) List() []*Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make([]*Assignment, 0, len(s.Assignments))
	for _, a := range s.Assignments {
		result = append(result, cloneAssignment(a))
	}
	return result
}

// ListByPane returns all assignments for a specific pane
func (s *AssignmentStore) ListByPane(pane int) []*Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []*Assignment
	for _, a := range s.Assignments {
		if a.Pane == pane {
			result = append(result, cloneAssignment(a))
		}
	}
	return result
}

// ListByStatus returns all assignments with a specific status
func (s *AssignmentStore) ListByStatus(status AssignmentStatus) []*Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []*Assignment
	for _, a := range s.Assignments {
		if a.Status == status {
			result = append(result, cloneAssignment(a))
		}
	}
	return result
}

// ListActive returns all assignments that are assigned or working
func (s *AssignmentStore) ListActive() []*Assignment {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var result []*Assignment
	for _, a := range s.Assignments {
		if a.ClearState != ClearStateNone || a.Status == StatusClaiming || a.Status == StatusClaimed || a.Status == StatusAssigned || a.Status == StatusWorking {
			result = append(result, cloneAssignment(a))
		}
	}
	return result
}

// BeginClear persists a cross-process barrier before external reservation
// release. The barrier retains exact reservation IDs and blocks new assignment
// generations until CompleteClear removes the record.
func (s *AssignmentStore) BeginClear(ctx context.Context, beadID string, startedAt time.Time) (*Assignment, error) {
	return s.beginClear(ctx, beadID, startedAt, nil)
}

// BeginClearIfStatus establishes the clear barrier only if the status still
// matches one of expected after acquiring the bead lock and reloading durable
// state. This closes filter-then-clear races such as --clear-failed clearing a
// concurrently retried assignment.
func (s *AssignmentStore) BeginClearIfStatus(ctx context.Context, beadID string, startedAt time.Time, expected ...AssignmentStatus) (*Assignment, error) {
	if len(expected) == 0 {
		return nil, errors.New("at least one expected assignment status is required")
	}
	return s.beginClear(ctx, beadID, startedAt, expected)
}

func (s *AssignmentStore) beginClear(ctx context.Context, beadID string, startedAt time.Time, expected []AssignmentStatus) (*Assignment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, s.path, beadID)
	if err != nil {
		return nil, fmt.Errorf("lock assignment clear %s: %w", beadID, err)
	}
	defer operationUnlock()
	if err := s.LoadStrict(); err != nil {
		return nil, fmt.Errorf("refresh assignment clear %s: %w", beadID, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	assignment := s.Assignments[beadID]
	if assignment == nil {
		return nil, fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}
	if len(expected) > 0 && !assignmentStatusAllowed(assignment.Status, expected) {
		return nil, fmt.Errorf("%w: %s is %s, expected %s", ErrAssignmentStatusMismatch, beadID, assignment.Status, formatAssignmentStatuses(expected))
	}
	if assignment.DispatchState == DispatchSending {
		return nil, fmt.Errorf("%w: cannot clear %s while dispatch outcome is unknown", ErrDispatchOutcomeUnknown, beadID)
	}
	if assignment.ClearState != ClearStateNone {
		return cloneAssignment(assignment), nil
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	assignment.ClearState = ClearStateReservationReleasing
	assignment.ClearStartedAt = &startedAt
	assignment.ClearError = ""
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneAssignment(assignment), nil
}

func assignmentStatusAllowed(actual AssignmentStatus, expected []AssignmentStatus) bool {
	for _, status := range expected {
		if actual == status {
			return true
		}
	}
	return false
}

func formatAssignmentStatuses(statuses []AssignmentStatus) string {
	values := make([]string, 0, len(statuses))
	for _, status := range statuses {
		values = append(values, string(status))
	}
	return strings.Join(values, ",")
}

// RecordClearReleaseFailed preserves a retryable clear barrier and diagnostic.
func (s *AssignmentStore) RecordClearReleaseFailed(ctx context.Context, beadID string, releaseErr error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, s.path, beadID)
	if err != nil {
		return fmt.Errorf("lock assignment clear failure %s: %w", beadID, err)
	}
	defer operationUnlock()
	if err := s.LoadStrict(); err != nil {
		return fmt.Errorf("refresh assignment clear failure %s: %w", beadID, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	assignment := s.Assignments[beadID]
	if assignment == nil {
		return nil
	}
	if assignment.ClearState == ClearStateNone {
		return fmt.Errorf("assignment %s is not awaiting reservation release", beadID)
	}
	assignment.ClearError = ""
	if releaseErr != nil {
		assignment.ClearError = releaseErr.Error()
	}
	return s.saveLocked()
}

// RecordClearLeasesReleased durably records that every matching external lease
// is absent. Later clear retries can skip the non-idempotent release call while
// still retrying tracker-claim release and local ledger completion.
func (s *AssignmentStore) RecordClearLeasesReleased(ctx context.Context, beadID string) (*Assignment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, s.path, beadID)
	if err != nil {
		return nil, fmt.Errorf("lock assignment lease-release completion %s: %w", beadID, err)
	}
	defer operationUnlock()
	if err := s.LoadStrict(); err != nil {
		return nil, fmt.Errorf("refresh assignment lease-release completion %s: %w", beadID, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	assignment := s.Assignments[beadID]
	if assignment == nil {
		return nil, fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}
	if assignment.ClearState == ClearStateLeasesReleased {
		return cloneAssignment(assignment), nil
	}
	if assignment.ClearState != ClearStateReservationReleasing {
		return nil, fmt.Errorf("assignment %s is not awaiting reservation release", beadID)
	}
	assignment.ClearState = ClearStateLeasesReleased
	assignment.ClearError = ""
	assignment.ReservationState = ReservationReleased
	assignment.ReservationCompleted = false
	assignment.ReservedPaths = nil
	assignment.ReservationIDs = nil
	assignment.ReservationExpiresAt = nil
	assignment.ReservationError = ""
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneAssignment(assignment), nil
}

// CompleteClear removes an assignment only after its external reservations are
// confirmed released. Missing records are an idempotent success for racers.
func (s *AssignmentStore) CompleteClear(ctx context.Context, beadID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, s.path, beadID)
	if err != nil {
		return fmt.Errorf("lock assignment clear completion %s: %w", beadID, err)
	}
	defer operationUnlock()
	if err := s.LoadStrict(); err != nil {
		return fmt.Errorf("refresh assignment clear completion %s: %w", beadID, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	assignment := s.Assignments[beadID]
	if assignment == nil {
		return nil
	}
	if assignment.ClearState != ClearStateLeasesReleased {
		return fmt.Errorf("assignment %s has not durably completed reservation release", beadID)
	}
	if s.ClearedGenerations == nil {
		s.ClearedGenerations = make(map[string]uint64)
	}
	s.ClearedGenerations[beadID]++
	delete(s.Assignments, beadID)
	return s.saveLocked()
}

// CompleteTerminalReconciliation retires tracker-terminal work only after the
// caller has proven every external reservation released. BeginClearIfStatus
// establishes the cross-process barrier consumed here.
func (s *AssignmentStore) CompleteTerminalReconciliation(ctx context.Context, beadID string, terminalStatus AssignmentStatus, reason string) error {
	if terminalStatus != StatusCompleted && terminalStatus != StatusFailed {
		return fmt.Errorf("terminal reconciliation status must be completed or failed, got %s", terminalStatus)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	operationUnlock, err := acquireAtomicBeadOperationLock(ctx, s.path, beadID)
	if err != nil {
		return fmt.Errorf("lock terminal assignment reconciliation %s: %w", beadID, err)
	}
	defer operationUnlock()
	if err := s.LoadStrict(); err != nil {
		return fmt.Errorf("refresh terminal assignment reconciliation %s: %w", beadID, err)
	}

	s.mutex.Lock()
	assignment := s.Assignments[beadID]
	if assignment == nil {
		s.mutex.Unlock()
		return nil
	}
	if assignment.ClearState != ClearStateLeasesReleased {
		s.mutex.Unlock()
		return fmt.Errorf("assignment %s has not durably completed reservation release", beadID)
	}
	if assignment.DispatchState == DispatchSending {
		s.mutex.Unlock()
		return fmt.Errorf("%w: cannot retire %s while dispatch outcome is unknown", ErrDispatchOutcomeUnknown, beadID)
	}

	previousStatus := assignment.Status
	now := time.Now().UTC()
	assignment.Status = terminalStatus
	assignment.ReservationState = ReservationReleased
	assignment.ReservationCompleted = false
	assignment.ReservedPaths = nil
	assignment.ReservationIDs = nil
	assignment.ReservationExpiresAt = nil
	assignment.ReservationError = ""
	assignment.ClearState = ClearStateNone
	assignment.ClearStartedAt = nil
	assignment.ClearError = ""
	assignment.CompletedAt = nil
	assignment.FailedAt = nil
	assignment.FailReason = ""
	assignment.FailureReason = ""
	if terminalStatus == StatusCompleted {
		assignment.CompletedAt = &now
	} else {
		assignment.FailedAt = &now
		assignment.FailReason = reason
	}
	if err := s.saveLocked(); err != nil {
		s.mutex.Unlock()
		return err
	}
	emitIdle := s.shouldEmitAgentIdleLocked(assignment, previousStatus, terminalStatus)
	cloned := cloneAssignment(assignment)
	s.mutex.Unlock()

	emitAssignmentStatusEvent(s.SessionName, cloned, terminalStatus, reason)
	if emitIdle {
		emitAgentIdle(s.SessionName, cloned, previousStatus, terminalStatus)
	}
	return nil
}

// Update updates mutable assignment metadata while preserving snapshot semantics
// for store callers.
func (s *AssignmentStore) Update(beadID string, update AssignmentUpdate) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	assignment, ok := s.Assignments[beadID]
	if !ok {
		return fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}

	changed := false
	if update.PromptSent != nil && assignment.PromptSent != *update.PromptSent {
		assignment.PromptSent = *update.PromptSent
		changed = true
	}
	if update.RetryCount != nil && assignment.RetryCount != *update.RetryCount {
		assignment.RetryCount = *update.RetryCount
		changed = true
	}
	if !changed {
		return nil
	}

	if err := s.saveLocked(); err != nil {
		return err
	}

	return nil
}

// ValidTransitions defines valid state transitions.
//
// `StatusAssigned -> StatusCompleted` is permitted because beads can be closed
// externally (via `br close` from another agent, or by the assigned agent
// before the assignment store observed a `working` transition). The watch
// loop correlates closures back into the store after the fact and would
// otherwise drop those completions on the floor with an "invalid transition"
// warning, leaving the assignment forever stuck in "assigned" (#124).
var ValidTransitions = map[AssignmentStatus][]AssignmentStatus{
	StatusClaimed:    {StatusAssigned, StatusFailed},
	StatusAssigned:   {StatusWorking, StatusCompleted, StatusFailed},
	StatusWorking:    {StatusCompleted, StatusFailed, StatusReassigned},
	StatusFailed:     {StatusAssigned}, // Retry
	StatusCompleted:  {},               // Terminal
	StatusReassigned: {},               // Terminal (new assignment created)
}

// isValidTransition checks if a state transition is valid
func isValidTransition(from, to AssignmentStatus) bool {
	validTargets, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, valid := range validTargets {
		if valid == to {
			return true
		}
	}
	return false
}

// UpdateStatus changes the status of an assignment with validation
func (s *AssignmentStore) UpdateStatus(beadID string, newStatus AssignmentStatus) error {
	s.mutex.Lock()

	assignment, ok := s.Assignments[beadID]
	if !ok {
		s.mutex.Unlock()
		return fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}

	prevStatus := assignment.Status

	if !isValidTransition(prevStatus, newStatus) {
		s.mutex.Unlock()
		return &InvalidTransitionError{
			BeadID: beadID,
			From:   prevStatus,
			To:     newStatus,
		}
	}

	now := time.Now().UTC()

	// Update status and timestamps
	assignment.Status = newStatus
	switch newStatus {
	case StatusClaimed:
		assignment.StartedAt = nil
		assignment.CompletedAt = nil
		assignment.FailedAt = nil
		assignment.FailReason = ""
		assignment.FailureReason = ""
	case StatusAssigned:
		assignment.StartedAt = nil
		assignment.CompletedAt = nil
		assignment.FailedAt = nil
		assignment.FailReason = ""
		assignment.FailureReason = ""
	case StatusWorking:
		assignment.StartedAt = &now
	case StatusCompleted:
		assignment.CompletedAt = &now
	case StatusFailed:
		assignment.FailedAt = &now
	}

	// Persist
	if err := s.saveLocked(); err != nil {
		s.mutex.Unlock()
		return err
	}

	emitIdle := s.shouldEmitAgentIdleLocked(assignment, prevStatus, newStatus)
	cloned := cloneAssignment(assignment)
	s.mutex.Unlock()

	emitAssignmentStatusEvent(s.SessionName, cloned, newStatus, "")
	if emitIdle {
		emitAgentIdle(s.SessionName, cloned, prevStatus, newStatus)
	}

	return nil
}

// MarkWorking marks an assignment as actively working
func (s *AssignmentStore) MarkWorking(beadID string) error {
	return s.UpdateStatus(beadID, StatusWorking)
}

// MarkCompleted marks an assignment as completed
func (s *AssignmentStore) MarkCompleted(beadID string) error {
	return s.UpdateStatus(beadID, StatusCompleted)
}

// MarkFailed marks an assignment as failed with a reason
func (s *AssignmentStore) MarkFailed(beadID, reason string) error {
	s.mutex.Lock()

	assignment, ok := s.Assignments[beadID]
	if !ok {
		s.mutex.Unlock()
		return fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}

	prevStatus := assignment.Status

	if !isValidTransition(prevStatus, StatusFailed) {
		s.mutex.Unlock()
		return &InvalidTransitionError{
			BeadID: beadID,
			From:   prevStatus,
			To:     StatusFailed,
		}
	}

	now := time.Now().UTC()
	assignment.Status = StatusFailed
	assignment.FailedAt = &now
	assignment.FailReason = reason
	assignment.FailureReason = ""

	if err := s.saveLocked(); err != nil {
		s.mutex.Unlock()
		return err
	}

	emitIdle := s.shouldEmitAgentIdleLocked(assignment, prevStatus, StatusFailed)
	cloned := cloneAssignment(assignment)
	s.mutex.Unlock()

	emitAssignmentStatusEvent(s.SessionName, cloned, StatusFailed, reason)
	if emitIdle {
		emitAgentIdle(s.SessionName, cloned, prevStatus, StatusFailed)
	}

	return nil
}

// Reassign moves an assignment to a different agent
func (s *AssignmentStore) Reassign(beadID string, target ReassignmentTarget) (*Assignment, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	oldAssignment, ok := s.Assignments[beadID]
	if !ok {
		return nil, fmt.Errorf("[ASSIGN] Assignment not found: %s", beadID)
	}

	if !isValidTransition(oldAssignment.Status, StatusReassigned) {
		return nil, &InvalidTransitionError{
			BeadID: beadID,
			From:   oldAssignment.Status,
			To:     StatusReassigned,
		}
	}

	// Mark old assignment as reassigned
	oldAssignment.Status = StatusReassigned

	// Create new assignment
	now := time.Now().UTC()
	newAssignment := &Assignment{
		BeadID:         beadID,
		BeadTitle:      oldAssignment.BeadTitle,
		Pane:           target.Pane,
		AgentType:      target.AgentType,
		AgentName:      target.AgentName,
		Status:         StatusAssigned,
		AssignedAt:     now,
		RetryCount:     oldAssignment.RetryCount,
		DispatchTarget: target.DispatchTarget,
		OccupancyKey:   target.OccupancyKey,
	}

	s.Assignments[beadID] = newAssignment
	if s.replace == nil {
		s.replace = make(map[string]struct{})
	}
	s.replace[beadID] = struct{}{}

	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return cloneAssignment(newAssignment), nil
}

// Remove removes an assignment from the store.
func (s *AssignmentStore) Remove(beadID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.Assignments, beadID)

	return s.saveLocked()
}

// Clear removes all assignments from the store.
func (s *AssignmentStore) Clear() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.Assignments = make(map[string]*Assignment)

	return s.saveLocked()
}

// Stats returns summary statistics about assignments
func (s *AssignmentStore) Stats() AssignmentStats {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	stats := AssignmentStats{}
	for _, a := range s.Assignments {
		stats.Total++
		switch a.Status {
		case StatusClaimed:
			stats.Claimed++
		case StatusAssigned:
			stats.Assigned++
		case StatusWorking:
			stats.Working++
		case StatusCompleted:
			stats.Completed++
		case StatusFailed:
			stats.Failed++
		case StatusReassigned:
			stats.Reassigned++
		}
	}
	return stats
}

// AssignmentStats contains summary statistics
type AssignmentStats struct {
	Total      int `json:"total"`
	Claimed    int `json:"claimed"`
	Assigned   int `json:"assigned"`
	Working    int `json:"working"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Reassigned int `json:"reassigned"`
}

func emitAssignmentStatusEvent(session string, a *Assignment, newStatus AssignmentStatus, failReason string) {
	if a == nil {
		return
	}

	baseDetails := map[string]string{
		"bead_id":    a.BeadID,
		"bead_title": a.BeadTitle,
		"pane_index": fmt.Sprintf("%d", a.Pane),
		"agent_type": a.AgentType,
		"agent_name": a.AgentName,
		"status":     string(newStatus),
	}

	switch newStatus {
	case StatusWorking:
		events.DefaultEmitter().Emit(events.NewWebhookEvent(
			events.WebhookAgentBusy,
			session,
			fmt.Sprintf("%d", a.Pane),
			a.AgentType,
			fmt.Sprintf("Agent busy on %s", a.BeadID),
			baseDetails,
		))
	case StatusCompleted:
		events.DefaultEmitter().Emit(events.NewWebhookEvent(
			events.WebhookBeadCompleted,
			session,
			fmt.Sprintf("%d", a.Pane),
			a.AgentType,
			fmt.Sprintf("Bead completed: %s", a.BeadID),
			baseDetails,
		))
		events.DefaultEmitter().Emit(events.NewWebhookEvent(
			events.WebhookAgentCompleted,
			session,
			fmt.Sprintf("%d", a.Pane),
			a.AgentType,
			fmt.Sprintf("Agent completed bead %s", a.BeadID),
			baseDetails,
		))
	case StatusFailed:
		details := baseDetails
		if strings.TrimSpace(failReason) != "" {
			// Clone to avoid mutating base map used by other emissions.
			details = make(map[string]string, len(baseDetails)+1)
			for k, v := range baseDetails {
				details[k] = v
			}
			details["fail_reason"] = failReason
		}
		msg := fmt.Sprintf("Bead failed: %s", a.BeadID)
		if strings.TrimSpace(failReason) != "" {
			msg = fmt.Sprintf("%s (%s)", msg, strings.TrimSpace(failReason))
		}
		events.DefaultEmitter().Emit(events.NewWebhookEvent(
			events.WebhookBeadFailed,
			session,
			fmt.Sprintf("%d", a.Pane),
			a.AgentType,
			msg,
			details,
		))
		events.DefaultEmitter().Emit(events.NewWebhookEvent(
			events.WebhookAgentError,
			session,
			fmt.Sprintf("%d", a.Pane),
			a.AgentType,
			msg,
			details,
		))
	}
}

func (s *AssignmentStore) shouldEmitAgentIdleLocked(a *Assignment, prevStatus, newStatus AssignmentStatus) bool {
	if a == nil {
		return false
	}
	if prevStatus != StatusWorking {
		return false
	}
	if newStatus != StatusCompleted && newStatus != StatusFailed {
		return false
	}

	// Only emit idle when there are no remaining "working" assignments for this pane.
	for _, other := range s.Assignments {
		if other == nil {
			continue
		}
		if other.Pane == a.Pane && other.Status == StatusWorking {
			return false
		}
	}

	return true
}

func emitAgentIdle(session string, a *Assignment, prevStatus, newStatus AssignmentStatus) {
	events.DefaultEmitter().Emit(events.NewWebhookEvent(
		events.WebhookAgentIdle,
		session,
		fmt.Sprintf("%d", a.Pane),
		a.AgentType,
		"Agent idle (no active bead assignments)",
		map[string]string{
			"bead_id":     a.BeadID,
			"bead_title":  a.BeadTitle,
			"pane_index":  fmt.Sprintf("%d", a.Pane),
			"agent_type":  a.AgentType,
			"agent_name":  a.AgentName,
			"prev_status": string(prevStatus),
			"new_status":  string(newStatus),
		},
	))
}
