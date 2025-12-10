package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultCheckpointDir is the default directory for checkpoints
	DefaultCheckpointDir = ".local/share/ntm/checkpoints"
	// MetadataFile is the name of the checkpoint metadata file
	MetadataFile = "metadata.json"
	// SessionFile is the name of the session state file
	SessionFile = "session.json"
	// GitPatchFile is the name of the git diff patch file
	GitPatchFile = "git.patch"
	// GitStatusFile is the name of the git status file
	GitStatusFile = "git-status.txt"
	// PanesDir is the subdirectory for pane scrollback captures
	PanesDir = "panes"
)

// Storage manages checkpoint storage on disk.
type Storage struct {
	// BaseDir is the base directory for all checkpoints
	BaseDir string
}

// NewStorage creates a new Storage with the default directory.
func NewStorage() *Storage {
	home, _ := os.UserHomeDir()
	return &Storage{
		BaseDir: filepath.Join(home, DefaultCheckpointDir),
	}
}

// NewStorageWithDir creates a Storage with a custom directory.
func NewStorageWithDir(dir string) *Storage {
	return &Storage{
		BaseDir: dir,
	}
}

// CheckpointDir returns the directory path for a specific checkpoint.
func (s *Storage) CheckpointDir(sessionName, checkpointID string) string {
	return filepath.Join(s.BaseDir, sessionName, checkpointID)
}

// PanesDir returns the panes subdirectory for a checkpoint.
func (s *Storage) PanesDirPath(sessionName, checkpointID string) string {
	return filepath.Join(s.CheckpointDir(sessionName, checkpointID), PanesDir)
}

// GenerateID creates a unique checkpoint ID from timestamp and name.
func GenerateID(name string) string {
	timestamp := time.Now().Format("20060102-150405")
	// Sanitize name for filesystem safety
	safeName := sanitizeName(name)
	if safeName == "" {
		return timestamp
	}
	return fmt.Sprintf("%s-%s", timestamp, safeName)
}

// sanitizeName makes a name safe for use in file paths.
func sanitizeName(name string) string {
	// Replace unsafe characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "-",
		"?", "-",
		"\"", "-",
		"<", "-",
		">", "-",
		"|", "-",
		" ", "_",
	)
	safe := replacer.Replace(strings.TrimSpace(name))
	// Limit length
	if len(safe) > 50 {
		safe = safe[:50]
	}
	return safe
}

// Save writes a checkpoint to disk.
func (s *Storage) Save(cp *Checkpoint) error {
	dir := s.CheckpointDir(cp.SessionName, cp.ID)

	// Create checkpoint directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating checkpoint directory: %w", err)
	}

	// Create panes directory
	panesDir := filepath.Join(dir, PanesDir)
	if err := os.MkdirAll(panesDir, 0755); err != nil {
		return fmt.Errorf("creating panes directory: %w", err)
	}

	// Save metadata
	metaPath := filepath.Join(dir, MetadataFile)
	if err := writeJSON(metaPath, cp); err != nil {
		return fmt.Errorf("saving metadata: %w", err)
	}

	// Save session state separately for easy reading
	sessionPath := filepath.Join(dir, SessionFile)
	if err := writeJSON(sessionPath, cp.Session); err != nil {
		return fmt.Errorf("saving session state: %w", err)
	}

	return nil
}

// Load reads a checkpoint from disk.
func (s *Storage) Load(sessionName, checkpointID string) (*Checkpoint, error) {
	dir := s.CheckpointDir(sessionName, checkpointID)
	metaPath := filepath.Join(dir, MetadataFile)

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("reading checkpoint metadata: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parsing checkpoint metadata: %w", err)
	}

	return &cp, nil
}

// List returns all checkpoints for a session, sorted by creation time (newest first).
func (s *Storage) List(sessionName string) ([]*Checkpoint, error) {
	sessionDir := filepath.Join(s.BaseDir, sessionName)

	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoints yet
		}
		return nil, fmt.Errorf("reading session directory: %w", err)
	}

	var checkpoints []*Checkpoint
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		cp, err := s.Load(sessionName, entry.Name())
		if err != nil {
			// Skip invalid checkpoints
			continue
		}
		checkpoints = append(checkpoints, cp)
	}

	// Sort by creation time, newest first
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].CreatedAt.After(checkpoints[j].CreatedAt)
	})

	return checkpoints, nil
}

// ListAll returns all checkpoints across all sessions.
func (s *Storage) ListAll() ([]*Checkpoint, error) {
	entries, err := os.ReadDir(s.BaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading checkpoints directory: %w", err)
	}

	var all []*Checkpoint
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionCheckpoints, err := s.List(entry.Name())
		if err != nil {
			continue
		}
		all = append(all, sessionCheckpoints...)
	}

	// Sort by creation time, newest first
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	return all, nil
}

// Delete removes a checkpoint from disk.
func (s *Storage) Delete(sessionName, checkpointID string) error {
	dir := s.CheckpointDir(sessionName, checkpointID)
	return os.RemoveAll(dir)
}

// GetLatest returns the most recent checkpoint for a session.
func (s *Storage) GetLatest(sessionName string) (*Checkpoint, error) {
	checkpoints, err := s.List(sessionName)
	if err != nil {
		return nil, err
	}
	if len(checkpoints) == 0 {
		return nil, fmt.Errorf("no checkpoints found for session: %s", sessionName)
	}
	return checkpoints[0], nil
}

// SaveScrollback writes pane scrollback to a file.
func (s *Storage) SaveScrollback(sessionName, checkpointID string, paneIndex int, content string) (string, error) {
	panesDir := s.PanesDirPath(sessionName, checkpointID)
	if err := os.MkdirAll(panesDir, 0755); err != nil {
		return "", fmt.Errorf("creating panes directory: %w", err)
	}

	filename := fmt.Sprintf("pane_%d.txt", paneIndex)
	fullPath := filepath.Join(panesDir, filename)

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("saving scrollback: %w", err)
	}

	return filepath.Join(PanesDir, filename), nil
}

// LoadScrollback reads pane scrollback from a file.
func (s *Storage) LoadScrollback(sessionName, checkpointID string, paneIndex int) (string, error) {
	filename := fmt.Sprintf("pane_%d.txt", paneIndex)
	fullPath := filepath.Join(s.PanesDirPath(sessionName, checkpointID), filename)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("reading scrollback: %w", err)
	}

	return string(data), nil
}

// SaveGitPatch writes the git diff patch to the checkpoint.
func (s *Storage) SaveGitPatch(sessionName, checkpointID, patch string) error {
	if patch == "" {
		return nil
	}
	dir := s.CheckpointDir(sessionName, checkpointID)
	filepath := filepath.Join(dir, GitPatchFile)
	return os.WriteFile(filepath, []byte(patch), 0644)
}

// LoadGitPatch reads the git diff patch from the checkpoint.
func (s *Storage) LoadGitPatch(sessionName, checkpointID string) (string, error) {
	dir := s.CheckpointDir(sessionName, checkpointID)
	filepath := filepath.Join(dir, GitPatchFile)

	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading git patch: %w", err)
	}

	return string(data), nil
}

// SaveGitStatus writes the git status output to the checkpoint.
func (s *Storage) SaveGitStatus(sessionName, checkpointID, status string) error {
	dir := s.CheckpointDir(sessionName, checkpointID)
	filepath := filepath.Join(dir, GitStatusFile)
	return os.WriteFile(filepath, []byte(status), 0644)
}

// writeJSON writes data as formatted JSON to a file.
func writeJSON(path string, data interface{}) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bytes, 0644)
}

// Exists returns true if a checkpoint exists.
func (s *Storage) Exists(sessionName, checkpointID string) bool {
	dir := s.CheckpointDir(sessionName, checkpointID)
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}
