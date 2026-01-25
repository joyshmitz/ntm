// Package ensemble provides checkpoint storage for partial synthesis recovery.
// Checkpoints allow resuming ensemble runs after failures or interruptions.
package ensemble

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// checkpointDirName is the directory name for checkpoint storage.
	checkpointDirName = "ensemble-checkpoints"
	// checkpointMetaFile is the metadata filename within a checkpoint.
	checkpointMetaFile = "_meta.json"
)

// CheckpointMetadata holds metadata about a checkpoint.
type CheckpointMetadata struct {
	SessionName  string          `json:"session_name"`
	Question     string          `json:"question"`
	RunID        string          `json:"run_id"`
	Status       EnsembleStatus  `json:"status"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	ContextHash  string          `json:"context_hash,omitempty"`
	CompletedIDs []string        `json:"completed_ids"`
	PendingIDs   []string        `json:"pending_ids"`
	ErrorIDs     []string        `json:"error_ids,omitempty"`
	TotalModes   int             `json:"total_modes"`
}

// ModeCheckpoint stores a single mode's output for recovery.
type ModeCheckpoint struct {
	ModeID       string      `json:"mode_id"`
	Output       *ModeOutput `json:"output,omitempty"`
	Status       string      `json:"status"`
	CapturedAt   time.Time   `json:"captured_at"`
	ContextHash  string      `json:"context_hash,omitempty"`
	TokensUsed   int         `json:"tokens_used,omitempty"`
	Error        string      `json:"error,omitempty"`
}

// CheckpointStore manages checkpoint persistence.
type CheckpointStore struct {
	baseDir string
	logger  *slog.Logger
}

// NewCheckpointStore creates a checkpoint store in the given base directory.
// If baseDir is empty, uses the current working directory's .ntm folder.
func NewCheckpointStore(baseDir string) (*CheckpointStore, error) {
	if baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("get working directory: %w", err)
		}
		baseDir = filepath.Join(cwd, ".ntm")
	}

	checkpointDir := filepath.Join(baseDir, checkpointDirName)
	if err := os.MkdirAll(checkpointDir, 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint directory: %w", err)
	}

	return &CheckpointStore{
		baseDir: checkpointDir,
		logger:  slog.Default(),
	}, nil
}

// WithLogger sets the logger for the checkpoint store.
func (s *CheckpointStore) WithLogger(logger *slog.Logger) *CheckpointStore {
	if logger != nil {
		s.logger = logger
	}
	return s
}

// SaveCheckpoint saves a mode's output as a checkpoint.
func (s *CheckpointStore) SaveCheckpoint(runID string, checkpoint ModeCheckpoint) error {
	if s == nil {
		return errors.New("checkpoint store is nil")
	}
	if runID == "" {
		return errors.New("run ID is required")
	}
	if checkpoint.ModeID == "" {
		return errors.New("mode ID is required")
	}

	runDir := filepath.Join(s.baseDir, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	if checkpoint.CapturedAt.IsZero() {
		checkpoint.CapturedAt = time.Now().UTC()
	}

	filename := filepath.Join(runDir, checkpoint.ModeID+".json")
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}

	s.logger.Info("checkpoint saved",
		"run_id", runID,
		"mode_id", checkpoint.ModeID,
		"status", checkpoint.Status,
		"tokens", checkpoint.TokensUsed,
	)

	return nil
}

// SaveMetadata saves or updates the checkpoint metadata.
func (s *CheckpointStore) SaveMetadata(meta CheckpointMetadata) error {
	if s == nil {
		return errors.New("checkpoint store is nil")
	}
	if meta.RunID == "" {
		return errors.New("run ID is required")
	}

	runDir := filepath.Join(s.baseDir, meta.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}

	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	meta.UpdatedAt = time.Now().UTC()

	filename := filepath.Join(runDir, checkpointMetaFile)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}

	s.logger.Info("checkpoint metadata saved",
		"run_id", meta.RunID,
		"session", meta.SessionName,
		"completed", len(meta.CompletedIDs),
		"pending", len(meta.PendingIDs),
	)

	return nil
}

// LoadCheckpoint loads a specific mode's checkpoint.
func (s *CheckpointStore) LoadCheckpoint(runID, modeID string) (*ModeCheckpoint, error) {
	if s == nil {
		return nil, errors.New("checkpoint store is nil")
	}
	if runID == "" || modeID == "" {
		return nil, errors.New("run ID and mode ID are required")
	}

	filename := filepath.Join(s.baseDir, runID, modeID+".json")
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var checkpoint ModeCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	return &checkpoint, nil
}

// LoadMetadata loads the checkpoint metadata for a run.
func (s *CheckpointStore) LoadMetadata(runID string) (*CheckpointMetadata, error) {
	if s == nil {
		return nil, errors.New("checkpoint store is nil")
	}
	if runID == "" {
		return nil, errors.New("run ID is required")
	}

	filename := filepath.Join(s.baseDir, runID, checkpointMetaFile)
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var meta CheckpointMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// LoadAllCheckpoints loads all mode checkpoints for a run.
func (s *CheckpointStore) LoadAllCheckpoints(runID string) ([]ModeCheckpoint, error) {
	if s == nil {
		return nil, errors.New("checkpoint store is nil")
	}
	if runID == "" {
		return nil, errors.New("run ID is required")
	}

	runDir := filepath.Join(s.baseDir, runID)
	entries, err := os.ReadDir(runDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read checkpoint directory: %w", err)
	}

	var checkpoints []ModeCheckpoint
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Name() == checkpointMetaFile {
			continue
		}

		modeID := strings.TrimSuffix(entry.Name(), ".json")
		checkpoint, err := s.LoadCheckpoint(runID, modeID)
		if err != nil {
			s.logger.Warn("failed to load checkpoint",
				"run_id", runID,
				"mode_id", modeID,
				"error", err,
			)
			continue
		}
		checkpoints = append(checkpoints, *checkpoint)
	}

	return checkpoints, nil
}

// ListRuns returns all available checkpoint run IDs.
func (s *CheckpointStore) ListRuns() ([]CheckpointMetadata, error) {
	if s == nil {
		return nil, errors.New("checkpoint store is nil")
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoint directory: %w", err)
	}

	var runs []CheckpointMetadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runID := entry.Name()
		meta, err := s.LoadMetadata(runID)
		if err != nil {
			// Create minimal metadata from directory
			info, _ := entry.Info()
			meta = &CheckpointMetadata{
				RunID:     runID,
				CreatedAt: info.ModTime(),
			}
		}
		runs = append(runs, *meta)
	}

	// Sort by creation time, newest first
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})

	return runs, nil
}

// DeleteRun removes all checkpoints for a run.
func (s *CheckpointStore) DeleteRun(runID string) error {
	if s == nil {
		return errors.New("checkpoint store is nil")
	}
	if runID == "" {
		return errors.New("run ID is required")
	}

	runDir := filepath.Join(s.baseDir, runID)
	if err := os.RemoveAll(runDir); err != nil {
		return fmt.Errorf("remove checkpoint directory: %w", err)
	}

	s.logger.Info("checkpoint deleted", "run_id", runID)
	return nil
}

// CleanOld removes checkpoints older than the given duration.
func (s *CheckpointStore) CleanOld(maxAge time.Duration) (int, error) {
	if s == nil {
		return 0, errors.New("checkpoint store is nil")
	}

	runs, err := s.ListRuns()
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for _, run := range runs {
		if run.UpdatedAt.Before(cutoff) || (run.UpdatedAt.IsZero() && run.CreatedAt.Before(cutoff)) {
			if err := s.DeleteRun(run.RunID); err != nil {
				s.logger.Warn("failed to delete old checkpoint",
					"run_id", run.RunID,
					"error", err,
				)
				continue
			}
			removed++
		}
	}

	s.logger.Info("old checkpoints cleaned", "removed", removed, "max_age", maxAge)
	return removed, nil
}

// RunExists checks if a checkpoint run exists.
func (s *CheckpointStore) RunExists(runID string) bool {
	if s == nil || runID == "" {
		return false
	}
	runDir := filepath.Join(s.baseDir, runID)
	info, err := os.Stat(runDir)
	return err == nil && info.IsDir()
}

// GetCompletedOutputs returns all successfully completed mode outputs for a run.
func (s *CheckpointStore) GetCompletedOutputs(runID string) ([]*ModeOutput, error) {
	checkpoints, err := s.LoadAllCheckpoints(runID)
	if err != nil {
		return nil, err
	}

	var outputs []*ModeOutput
	for _, cp := range checkpoints {
		if cp.Status == string(AssignmentDone) && cp.Output != nil {
			outputs = append(outputs, cp.Output)
		}
	}

	return outputs, nil
}

// GetPendingModeIDs returns the mode IDs that haven't been completed yet.
func (s *CheckpointStore) GetPendingModeIDs(runID string) ([]string, error) {
	meta, err := s.LoadMetadata(runID)
	if err != nil {
		return nil, err
	}
	return meta.PendingIDs, nil
}

// UpdateModeStatus updates the status of a mode in the metadata.
func (s *CheckpointStore) UpdateModeStatus(runID, modeID, status string) error {
	meta, err := s.LoadMetadata(runID)
	if err != nil {
		return err
	}

	// Remove from pending/error lists
	meta.PendingIDs = removeFromSlice(meta.PendingIDs, modeID)
	meta.ErrorIDs = removeFromSlice(meta.ErrorIDs, modeID)

	// Add to appropriate list
	switch AssignmentStatus(status) {
	case AssignmentDone:
		if !sliceContains(meta.CompletedIDs, modeID) {
			meta.CompletedIDs = append(meta.CompletedIDs, modeID)
		}
	case AssignmentError:
		if !sliceContains(meta.ErrorIDs, modeID) {
			meta.ErrorIDs = append(meta.ErrorIDs, modeID)
		}
	default:
		if !sliceContains(meta.PendingIDs, modeID) {
			meta.PendingIDs = append(meta.PendingIDs, modeID)
		}
	}

	return s.SaveMetadata(*meta)
}

func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}

func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// CheckpointManager coordinates checkpoint operations during ensemble runs.
type CheckpointManager struct {
	store  *CheckpointStore
	runID  string
	logger *slog.Logger
}

// NewCheckpointManager creates a checkpoint manager for a specific run.
func NewCheckpointManager(store *CheckpointStore, runID string) *CheckpointManager {
	return &CheckpointManager{
		store:  store,
		runID:  runID,
		logger: slog.Default(),
	}
}

// WithLogger sets the logger for the checkpoint manager.
func (m *CheckpointManager) WithLogger(logger *slog.Logger) *CheckpointManager {
	if logger != nil {
		m.logger = logger
	}
	return m
}

// Initialize creates the initial checkpoint metadata.
func (m *CheckpointManager) Initialize(session *EnsembleSession, contextHash string) error {
	if m == nil || m.store == nil {
		return errors.New("checkpoint manager is nil")
	}
	if session == nil {
		return errors.New("session is nil")
	}

	modeIDs := make([]string, 0, len(session.Assignments))
	for _, assignment := range session.Assignments {
		modeIDs = append(modeIDs, assignment.ModeID)
	}

	meta := CheckpointMetadata{
		SessionName: session.SessionName,
		Question:    session.Question,
		RunID:       m.runID,
		Status:      session.Status,
		CreatedAt:   session.CreatedAt,
		ContextHash: contextHash,
		PendingIDs:  modeIDs,
		TotalModes:  len(modeIDs),
	}

	return m.store.SaveMetadata(meta)
}

// RecordOutput saves a mode's output as a checkpoint.
func (m *CheckpointManager) RecordOutput(modeID string, output *ModeOutput, tokensUsed int, contextHash string) error {
	if m == nil || m.store == nil {
		return errors.New("checkpoint manager is nil")
	}

	status := string(AssignmentDone)
	var errMsg string
	if output == nil {
		status = string(AssignmentError)
		errMsg = "no output captured"
	}

	checkpoint := ModeCheckpoint{
		ModeID:      modeID,
		Output:      output,
		Status:      status,
		CapturedAt:  time.Now().UTC(),
		ContextHash: contextHash,
		TokensUsed:  tokensUsed,
		Error:       errMsg,
	}

	if err := m.store.SaveCheckpoint(m.runID, checkpoint); err != nil {
		return err
	}

	return m.store.UpdateModeStatus(m.runID, modeID, status)
}

// RecordError records a mode failure.
func (m *CheckpointManager) RecordError(modeID string, err error) error {
	if m == nil || m.store == nil {
		return errors.New("checkpoint manager is nil")
	}

	checkpoint := ModeCheckpoint{
		ModeID:     modeID,
		Status:     string(AssignmentError),
		CapturedAt: time.Now().UTC(),
		Error:      err.Error(),
	}

	if saveErr := m.store.SaveCheckpoint(m.runID, checkpoint); saveErr != nil {
		return saveErr
	}

	return m.store.UpdateModeStatus(m.runID, modeID, string(AssignmentError))
}

// MarkComplete marks the run as complete and optionally removes checkpoints.
func (m *CheckpointManager) MarkComplete(cleanup bool) error {
	if m == nil || m.store == nil {
		return errors.New("checkpoint manager is nil")
	}

	meta, err := m.store.LoadMetadata(m.runID)
	if err != nil {
		return err
	}

	meta.Status = EnsembleComplete
	if err := m.store.SaveMetadata(*meta); err != nil {
		return err
	}

	if cleanup {
		return m.store.DeleteRun(m.runID)
	}

	return nil
}

// IsResumable checks if a run can be resumed.
func (m *CheckpointManager) IsResumable() bool {
	if m == nil || m.store == nil {
		return false
	}

	meta, err := m.store.LoadMetadata(m.runID)
	if err != nil {
		return false
	}

	return len(meta.PendingIDs) > 0 || len(meta.ErrorIDs) > 0
}

// GetResumeState returns the state needed to resume a run.
func (m *CheckpointManager) GetResumeState() (*CheckpointMetadata, []*ModeOutput, error) {
	if m == nil || m.store == nil {
		return nil, nil, errors.New("checkpoint manager is nil")
	}

	meta, err := m.store.LoadMetadata(m.runID)
	if err != nil {
		return nil, nil, err
	}

	outputs, err := m.store.GetCompletedOutputs(m.runID)
	if err != nil {
		return nil, nil, err
	}

	return meta, outputs, nil
}
