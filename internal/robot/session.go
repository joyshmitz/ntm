// Package robot provides JSON output functions for AI agent integration.
package robot

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Dicklesworthstone/ntm/internal/session"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// SaveOptions configures the robot-save operation.
type SaveOptions struct {
	Session    string // Session name to save
	OutputFile string // Optional custom output file path
}

// RestoreOptions configures the robot-restore operation.
type RestoreOptions struct {
	SavedName string // Name of saved state to restore
	DryRun    bool   // Preview without executing
}

// SaveResult represents the JSON output for robot-save.
type SaveResult struct {
	RobotResponse
	Session  string                `json:"session"`
	SavedAs  string                `json:"saved_as"`
	FilePath string                `json:"file_path"`
	State    *session.SessionState `json:"state,omitempty"`
}

// RestoreResult represents the JSON output for robot-restore.
type RestoreResult struct {
	RobotResponse
	SavedName  string                `json:"saved_name"`
	RestoredAs string                `json:"restored_as,omitempty"`
	DryRun     bool                  `json:"dry_run"`
	State      *session.SessionState `json:"state,omitempty"`
	Preview    *RestorePreview       `json:"preview,omitempty"`
}

// RestorePreview describes what would happen during restore.
type RestorePreview struct {
	SessionName string   `json:"session_name"`
	WorkDir     string   `json:"work_dir"`
	PaneCount   int      `json:"pane_count"`
	AgentCount  int      `json:"agent_count"`
	Layout      string   `json:"layout"`
	Actions     []string `json:"actions"`
}

// GetSave saves a session state and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetSave(opts SaveOptions) (*SaveResult, error) {
	if err := tmux.EnsureInstalled(); err != nil {
		return &SaveResult{
			RobotResponse: NewErrorResponse(err, ErrCodeDependencyMissing, "Install tmux to save sessions"),
			Session:       opts.Session,
		}, nil
	}

	sessionName := opts.Session
	if sessionName == "" {
		return &SaveResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("session name is required"),
				ErrCodeInvalidFlag,
				"Provide a session name with --robot-save",
			),
			Session: "",
		}, nil
	}

	if !tmux.SessionExists(sessionName) {
		return &SaveResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("session '%s' not found", sessionName),
				ErrCodeSessionNotFound,
				"Use --robot-status to list available sessions",
			),
			Session: sessionName,
		}, nil
	}

	// Capture session state
	state, err := session.Capture(sessionName)
	if err != nil {
		return &SaveResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to capture session state: %w", err),
				ErrCodeInternalError,
				"Check the tmux session and retry",
			),
			Session: sessionName,
		}, nil
	}

	// Save state
	saveOpts := session.SaveOptions{
		Overwrite: true, // Robot mode always overwrites
	}
	path, err := session.Save(state, saveOpts)
	if err != nil {
		return &SaveResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to save session state: %w", err),
				ErrCodeInternalError,
				"Check session storage permissions and retry",
			),
			Session: sessionName,
		}, nil
	}

	// If custom output file requested, also write there
	if opts.OutputFile != "" {
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return &SaveResult{
				RobotResponse: NewErrorResponse(
					fmt.Errorf("failed to marshal state: %w", err),
					ErrCodeInternalError,
					"Retry without a custom output file",
				),
				Session: sessionName,
			}, nil
		}
		if err := os.WriteFile(opts.OutputFile, data, 0644); err != nil {
			return &SaveResult{
				RobotResponse: NewErrorResponse(
					fmt.Errorf("failed to write to %s: %w", opts.OutputFile, err),
					ErrCodeInternalError,
					"Check output path permissions and retry",
				),
				Session: sessionName,
			}, nil
		}
		path = opts.OutputFile
	}

	return &SaveResult{
		RobotResponse: NewRobotResponse(true),
		Session:       sessionName,
		SavedAs:       sessionName,
		FilePath:      path,
		State:         state,
	}, nil
}

// PrintSave saves a session state and outputs JSON.
// This is a thin wrapper around GetSave() for CLI output.
func PrintSave(opts SaveOptions) error {
	result, err := GetSave(opts)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(result, result.RobotResponse, "robot save failed")
}

// GetRestore restores a session from saved state and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetRestore(opts RestoreOptions) (*RestoreResult, error) {
	if err := tmux.EnsureInstalled(); err != nil {
		return &RestoreResult{
			RobotResponse: NewErrorResponse(err, ErrCodeDependencyMissing, "Install tmux to restore sessions"),
			SavedName:     opts.SavedName,
		}, nil
	}

	if opts.SavedName == "" {
		return &RestoreResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("saved state name is required"),
				ErrCodeInvalidFlag,
				"Provide a saved state name with --robot-restore",
			),
			SavedName: "",
		}, nil
	}

	// Load saved state
	state, err := session.Load(opts.SavedName)
	if err != nil {
		return &RestoreResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to load saved state: %w", err),
				ErrCodeInternalError,
				"Use the saved-session list to verify the state name",
			),
			SavedName: opts.SavedName,
		}, nil
	}

	// Dry run mode - preview what would happen
	if opts.DryRun {
		preview := buildRestorePreview(state)
		return &RestoreResult{
			RobotResponse: NewRobotResponse(true),
			SavedName:     opts.SavedName,
			DryRun:        true,
			State:         state,
			Preview:       preview,
		}, nil
	}

	// Check if session already exists
	if tmux.SessionExists(state.Name) {
		return &RestoreResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("session '%s' already exists", state.Name),
				ErrCodeInvalidFlag,
				"Choose a different session name or remove the existing session first",
			),
			SavedName: opts.SavedName,
		}, nil
	}

	// Restore session
	restoreOpts := session.RestoreOptions{
		Force: false, // Robot mode is cautious by default
	}
	if err := session.Restore(state, restoreOpts); err != nil {
		return &RestoreResult{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to restore session: %w", err),
				ErrCodeInternalError,
				"Check tmux health and retry the restore",
			),
			SavedName: opts.SavedName,
		}, nil
	}

	return &RestoreResult{
		RobotResponse: NewRobotResponse(true),
		SavedName:     opts.SavedName,
		RestoredAs:    state.Name,
		DryRun:        false,
		State:         state,
	}, nil
}

// PrintRestore restores a session from saved state and outputs JSON.
// This is a thin wrapper around GetRestore() for CLI output.
func PrintRestore(opts RestoreOptions) error {
	result, err := GetRestore(opts)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(result, result.RobotResponse, "robot restore failed")
}

func buildRestorePreview(state *session.SessionState) *RestorePreview {
	actions := []string{
		fmt.Sprintf("Create tmux session '%s'", state.Name),
		fmt.Sprintf("Set working directory to '%s'", state.WorkDir),
	}

	if len(state.Panes) > 1 {
		actions = append(actions, fmt.Sprintf("Create %d panes", len(state.Panes)))
	}

	if state.Layout != "" && state.Layout != "tiled" {
		actions = append(actions, fmt.Sprintf("Apply layout '%s'", state.Layout))
	}

	for _, p := range state.Panes {
		if p.Command != "" {
			actions = append(actions, fmt.Sprintf("Start '%s' in pane %d", p.Title, p.Index))
		}
	}

	return &RestorePreview{
		SessionName: state.Name,
		WorkDir:     state.WorkDir,
		PaneCount:   len(state.Panes),
		AgentCount:  state.Agents.Total(),
		Layout:      state.Layout,
		Actions:     actions,
	}
}
