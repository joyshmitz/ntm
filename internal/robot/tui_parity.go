// Package robot provides machine-readable output for AI agents.
// tui_parity.go implements robot commands that mirror TUI functionality,
// ensuring AI agents have access to the same information as human users.
package robot

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/alerts"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/history"
	"github.com/Dicklesworthstone/ntm/internal/kernel"
	"github.com/Dicklesworthstone/ntm/internal/state"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tracker"
)

// =============================================================================
// File Changes (--robot-files)
// =============================================================================
// Mirrors the Files panel in the dashboard TUI, providing file change tracking
// with agent attribution and time window filtering.

// FilesOutput represents the output for --robot-files
type FilesOutput struct {
	RobotResponse
	Session    string             `json:"session,omitempty"`
	TimeWindow string             `json:"time_window"` // "5m", "15m", "1h", "all"
	Count      int                `json:"count"`
	Changes    []FileChangeRecord `json:"changes"`
	Summary    FileChangesSummary `json:"summary"`
	AgentHints *AgentHints        `json:"_agent_hints,omitempty"`
}

// FileChangeRecord represents a single file change with agent attribution
type FileChangeRecord struct {
	Timestamp    string   `json:"timestamp"` // RFC3339
	Path         string   `json:"path"`      // Relative file path
	Operation    string   `json:"operation"` // "create", "modify", "delete", "rename"
	Agents       []string `json:"agents"`    // Agents that touched this file
	Session      string   `json:"session"`   // Session where change was detected
	SizeBytes    int64    `json:"size_bytes,omitempty"`
	LinesAdded   int      `json:"lines_added,omitempty"`
	LinesRemoved int      `json:"lines_removed,omitempty"`
}

// FileChangesSummary provides aggregate statistics
type FileChangesSummary struct {
	TotalChanges    int            `json:"total_changes"`
	UniqueFiles     int            `json:"unique_files"`
	ByAgent         map[string]int `json:"by_agent"`     // Agent -> change count
	ByOperation     map[string]int `json:"by_operation"` // Operation -> count
	MostActiveAgent string         `json:"most_active_agent,omitempty"`
	Conflicts       []FileConflict `json:"conflicts,omitempty"` // Files touched by multiple agents
}

// FileConflict represents a file modified by multiple agents
type FileConflict struct {
	Path      string   `json:"path"`
	Agents    []string `json:"agents"`
	Severity  string   `json:"severity"`   // "warning", "critical"
	FirstEdit string   `json:"first_edit"` // RFC3339
	LastEdit  string   `json:"last_edit"`  // RFC3339
}

// FilesOptions configures the --robot-files command
type FilesOptions struct {
	Session    string // Filter to specific session
	TimeWindow string // "5m", "15m", "1h", "all" (default: "15m")
	Limit      int    // Max changes to return (default: 100)
}

// GetFiles returns file changes data.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetFiles(opts FilesOptions) (*FilesOutput, error) {
	// Set defaults
	if opts.TimeWindow == "" {
		opts.TimeWindow = "15m"
	}
	if opts.Limit <= 0 {
		opts.Limit = 100
	}

	output := &FilesOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		TimeWindow:    opts.TimeWindow,
		Changes:       []FileChangeRecord{},
		Summary: FileChangesSummary{
			ByAgent:     make(map[string]int),
			ByOperation: make(map[string]int),
		},
	}

	// Get file changes from the global store
	store := tracker.GlobalFileChanges
	if store == nil {
		output.AgentHints = &AgentHints{
			Summary: "File change tracking not initialized",
			Notes:   []string{"File changes are tracked when agents modify files within ntm sessions"},
		}
		return output, nil
	}

	// Calculate time cutoff
	var cutoff time.Time
	switch opts.TimeWindow {
	case "5m":
		cutoff = time.Now().Add(-5 * time.Minute)
	case "15m":
		cutoff = time.Now().Add(-15 * time.Minute)
	case "1h":
		cutoff = time.Now().Add(-1 * time.Hour)
	case "all":
		cutoff = time.Time{} // No cutoff
	default:
		// Try to parse as duration
		if d, err := time.ParseDuration(opts.TimeWindow); err == nil {
			cutoff = time.Now().Add(-d)
		} else {
			cutoff = time.Now().Add(-15 * time.Minute) // Default
			opts.TimeWindow = "15m"
		}
	}

	// Get changes from store
	allChanges := store.All()

	// Track unique files and conflicts
	fileAgents := make(map[string]map[string]time.Time) // path -> agent -> last touch time
	uniqueFiles := make(map[string]struct{})

	for _, change := range allChanges {
		// Apply time filter
		if !cutoff.IsZero() && change.Timestamp.Before(cutoff) {
			continue
		}

		// Apply session filter
		if opts.Session != "" && change.Session != opts.Session {
			continue
		}

		// Limit results
		if len(output.Changes) >= opts.Limit {
			break
		}

		record := FileChangeRecord{
			Timestamp: FormatTimestamp(change.Timestamp),
			Path:      change.Change.Path,
			Operation: string(change.Change.Type),
			Agents:    change.Agents,
			Session:   change.Session,
		}

		output.Changes = append(output.Changes, record)
		uniqueFiles[change.Change.Path] = struct{}{}

		// Track agent activity
		for _, agent := range change.Agents {
			output.Summary.ByAgent[agent]++

			// Track for conflict detection
			if fileAgents[change.Change.Path] == nil {
				fileAgents[change.Change.Path] = make(map[string]time.Time)
			}
			existing := fileAgents[change.Change.Path][agent]
			if change.Timestamp.After(existing) {
				fileAgents[change.Change.Path][agent] = change.Timestamp
			}
		}

		// Track operation counts
		output.Summary.ByOperation[string(change.Change.Type)]++
	}

	output.Count = len(output.Changes)
	output.Summary.TotalChanges = len(output.Changes)
	output.Summary.UniqueFiles = len(uniqueFiles)

	// Find most active agent
	maxCount := 0
	for agent, count := range output.Summary.ByAgent {
		if count > maxCount {
			maxCount = count
			output.Summary.MostActiveAgent = agent
		}
	}

	// Detect conflicts (files touched by multiple agents)
	for path, agents := range fileAgents {
		if len(agents) > 1 {
			var agentList []string
			var firstEdit, lastEdit time.Time
			for agent, ts := range agents {
				agentList = append(agentList, agent)
				if firstEdit.IsZero() || ts.Before(firstEdit) {
					firstEdit = ts
				}
				if ts.After(lastEdit) {
					lastEdit = ts
				}
			}

			severity := "warning"
			if len(agentList) >= 3 || lastEdit.Sub(firstEdit) < 10*time.Minute {
				severity = "critical"
			}

			output.Summary.Conflicts = append(output.Summary.Conflicts, FileConflict{
				Path:      path,
				Agents:    agentList,
				Severity:  severity,
				FirstEdit: FormatTimestamp(firstEdit),
				LastEdit:  FormatTimestamp(lastEdit),
			})
		}
	}

	// Generate agent hints
	var warnings []string
	var suggestions []RobotAction

	if len(output.Summary.Conflicts) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d file(s) modified by multiple agents - potential conflicts", len(output.Summary.Conflicts)))
		suggestions = append(suggestions, RobotAction{
			Action:   "review_conflicts",
			Target:   "conflicting files",
			Reason:   "Multiple agents touched the same files",
			Priority: 2,
		})
	}

	if output.Count == 0 {
		output.AgentHints = &AgentHints{
			Summary: fmt.Sprintf("No file changes in the last %s", opts.TimeWindow),
			Notes:   []string{"Use --files-window=all to see all tracked changes"},
		}
	} else {
		output.AgentHints = &AgentHints{
			Summary:          fmt.Sprintf("%d changes to %d files in the last %s", output.Count, output.Summary.UniqueFiles, opts.TimeWindow),
			Warnings:         warnings,
			SuggestedActions: suggestions,
		}
	}

	return output, nil
}

// PrintFiles outputs file changes as JSON.
// This is a thin wrapper around GetFiles() for CLI output.
func PrintFiles(opts FilesOptions) error {
	output, err := GetFiles(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Inspect Pane (--robot-inspect-pane)
// =============================================================================
// Provides detailed inspection of a single pane, equivalent to zooming in
// the TUI dashboard. Includes full output capture, state detection, and context.

const (
	inspectSessionSchemaVersion = "ntm.robot.inspect_session.v1"
	inspectAgentSchemaVersion   = "ntm.robot.inspect_agent.v1"
)

// InspectProjectionInfo describes freshness metadata for projection-backed
// inspect surfaces.
type InspectProjectionInfo struct {
	Fresh       bool   `json:"fresh"`
	CollectedAt string `json:"collected_at,omitempty"`
	StaleAfter  string `json:"stale_after,omitempty"`
}

// InspectHealth summarizes the health of an inspected entity.
type InspectHealth struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// InspectAgentDetail is the projection-backed drill-down for one agent.
type InspectAgentDetail struct {
	ID               string                `json:"id"`
	Session          string                `json:"session"`
	Pane             string                `json:"pane"`
	Type             string                `json:"type"`
	Variant          string                `json:"variant,omitempty"`
	TypeConfidence   float64               `json:"type_confidence"`
	TypeMethod       string                `json:"type_method"`
	State            string                `json:"state"`
	StateReason      string                `json:"state_reason,omitempty"`
	PreviousState    string                `json:"previous_state,omitempty"`
	StateChangedAt   string                `json:"state_changed_at,omitempty"`
	LastOutputAt     string                `json:"last_output_at,omitempty"`
	LastOutputAgeSec int                   `json:"last_output_age_sec,omitempty"`
	OutputTailLines  int                   `json:"output_tail_lines,omitempty"`
	CurrentBead      string                `json:"current_bead,omitempty"`
	PendingMail      int                   `json:"pending_mail"`
	AgentMailName    string                `json:"agent_mail_name,omitempty"`
	Health           InspectHealth         `json:"health"`
	Projection       InspectProjectionInfo `json:"projection"`
}

// InspectSessionDetail is the projection-backed drill-down for one session.
type InspectSessionDetail struct {
	Name           string                `json:"name"`
	Label          string                `json:"label,omitempty"`
	ProjectPath    string                `json:"project_path,omitempty"`
	Attached       bool                  `json:"attached"`
	WindowCount    int                   `json:"window_count"`
	PaneCount      int                   `json:"pane_count"`
	AgentCount     int                   `json:"agent_count"`
	ActiveAgents   int                   `json:"active_agents"`
	IdleAgents     int                   `json:"idle_agents"`
	ErrorAgents    int                   `json:"error_agents"`
	CreatedAt      string                `json:"created_at,omitempty"`
	LastAttachedAt string                `json:"last_attached_at,omitempty"`
	LastActivityAt string                `json:"last_activity_at,omitempty"`
	Health         InspectHealth         `json:"health"`
	Projection     InspectProjectionInfo `json:"projection"`
	Agents         []InspectAgentDetail  `json:"agents"`
}

// InspectSessionOutput represents a projection-backed session drill-down.
type InspectSessionOutput struct {
	RobotResponse
	SchemaID      string               `json:"schema_id"`
	SchemaVersion string               `json:"schema_version"`
	Session       string               `json:"session"`
	Detail        InspectSessionDetail `json:"detail"`
	AgentHints    *AgentHints          `json:"_agent_hints,omitempty"`
}

// InspectAgentOutput represents a projection-backed agent drill-down.
type InspectAgentOutput struct {
	RobotResponse
	SchemaID      string             `json:"schema_id"`
	SchemaVersion string             `json:"schema_version"`
	AgentID       string             `json:"agent_id"`
	Detail        InspectAgentDetail `json:"detail"`
	AgentHints    *AgentHints        `json:"_agent_hints,omitempty"`
}

// InspectSessionOptions configures session inspection.
type InspectSessionOptions struct {
	Session string
}

// InspectAgentOptions configures agent inspection.
type InspectAgentOptions struct {
	AgentID string
}

// InspectPaneOutput represents detailed pane inspection
type InspectPaneOutput struct {
	RobotResponse
	Session    string             `json:"session"`
	PaneIndex  int                `json:"pane_index"`
	PaneID     string             `json:"pane_id"`
	Agent      InspectPaneAgent   `json:"agent"`
	Output     InspectPaneOutput_ `json:"output"`
	Context    InspectPaneContext `json:"context"`
	AgentHints *AgentHints        `json:"_agent_hints,omitempty"`
}

// InspectPaneAgent contains agent-specific information
type InspectPaneAgent struct {
	Type            string  `json:"type"` // claude, codex, gemini, user
	Variant         string  `json:"variant,omitempty"`
	Title           string  `json:"title"`
	State           string  `json:"state"` // generating, waiting, thinking, error
	StateConfidence float64 `json:"state_confidence"`
	Command         string  `json:"command,omitempty"`
	ProcessRunning  bool    `json:"process_running"`
}

// InspectPaneOutput_ contains the pane output analysis
type InspectPaneOutput_ struct {
	Lines       int             `json:"lines"`                  // Total lines captured
	Characters  int             `json:"characters"`             // Total characters
	LastLines   []string        `json:"last_lines"`             // Last N lines (configurable)
	CodeBlocks  []CodeBlockInfo `json:"code_blocks,omitempty"`  // Detected code blocks
	ErrorsFound []string        `json:"errors_found,omitempty"` // Detected error messages
}

// CodeBlockInfo represents a detected code block in output
type CodeBlockInfo struct {
	Language  string `json:"language,omitempty"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	FilePath  string `json:"file_path,omitempty"` // Detected target file
}

// InspectPaneContext contains context information
type InspectPaneContext struct {
	WorkingDir     string   `json:"working_dir,omitempty"`
	RecentFiles    []string `json:"recent_files,omitempty"` // Files mentioned in output
	PendingMail    int      `json:"pending_mail"`
	CurrentBead    string   `json:"current_bead,omitempty"`
	ContextPercent float64  `json:"context_percent,omitempty"` // Estimated context usage
}

// InspectPaneOptions configures the inspection
type InspectPaneOptions struct {
	Session     string
	PaneIndex   int
	PaneID      string // Alternative to index
	Lines       int    // Lines to capture (default: 100)
	IncludeCode bool   // Parse code blocks
}

// GetInspectPane returns detailed pane inspection data.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetInspectPane(opts InspectPaneOptions) (*InspectPaneOutput, error) {
	if opts.Lines <= 0 {
		opts.Lines = 100
	}

	// Validate session
	if opts.Session == "" {
		return &InspectPaneOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("session name required"),
				ErrCodeInvalidFlag,
				"Specify session with --robot-inspect-pane=SESSION",
			),
		}, nil
	}

	if !tmux.SessionExists(opts.Session) {
		return &InspectPaneOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("session '%s' not found", opts.Session),
				ErrCodeSessionNotFound,
				"Use 'ntm list' to see available sessions",
			),
			Session: opts.Session,
		}, nil
	}

	// Get panes
	panes, err := tmux.GetPanes(opts.Session)
	if err != nil {
		return &InspectPaneOutput{
			RobotResponse: NewErrorResponse(err, ErrCodeInternalError, "Failed to get panes"),
			Session:       opts.Session,
		}, nil
	}

	// Find the target pane
	var targetPane *tmux.Pane
	for i := range panes {
		if opts.PaneID != "" && panes[i].ID == opts.PaneID {
			targetPane = &panes[i]
			break
		} else if panes[i].Index == opts.PaneIndex {
			targetPane = &panes[i]
			break
		}
	}

	// If pane index 0 was requested but not found, and there are panes,
	// the user likely wants the first pane but tmux has pane-base-index > 0.
	// Fall back to the first pane in the list.
	if targetPane == nil && opts.PaneIndex == 0 && len(panes) > 0 {
		targetPane = &panes[0]
	}

	if targetPane == nil {
		// Build list of actual valid pane indices for a helpful hint
		var validIndices []string
		for _, p := range panes {
			validIndices = append(validIndices, fmt.Sprintf("%d", p.Index))
		}
		hint := "No panes found"
		if len(validIndices) > 0 {
			hint = fmt.Sprintf("Valid pane indices: %s", strings.Join(validIndices, ", "))
		}
		return &InspectPaneOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("pane %d not found in session '%s'", opts.PaneIndex, opts.Session),
				ErrCodePaneNotFound,
				hint,
			),
			Session:   opts.Session,
			PaneIndex: opts.PaneIndex,
		}, nil
	}

	// Capture output
	captured, captureErr := tmux.CapturePaneOutput(targetPane.ID, opts.Lines)

	output := &InspectPaneOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		PaneIndex:     targetPane.Index,
		PaneID:        targetPane.ID,
	}

	// Populate agent info
	detection := DetectAgentTypeEnhanced(*targetPane, captured)
	output.Agent = InspectPaneAgent{
		Type:            detection.Type,
		Variant:         targetPane.Variant,
		Title:           targetPane.Title,
		Command:         targetPane.Command,
		ProcessRunning:  targetPane.Command != "",
		StateConfidence: detection.Confidence,
	}

	// Detect state from output
	if captureErr == nil {
		lines := splitLines(stripANSI(captured))
		output.Agent.State = detectState(lines, targetPane.Title)

		output.Output.Lines = len(lines)
		output.Output.Characters = len(captured)

		// Get last N lines (up to 50 for reasonable output size)
		lastN := 50
		if len(lines) < lastN {
			lastN = len(lines)
		}
		if lastN > 0 {
			output.Output.LastLines = lines[len(lines)-lastN:]
		}

		// Detect errors in output
		output.Output.ErrorsFound = detectErrors(lines)

		// Parse code blocks if requested
		if opts.IncludeCode {
			output.Output.CodeBlocks = parseCodeBlocks(lines)
		}

		// Extract file references
		output.Context.RecentFiles = extractFileReferences(lines)
	}

	// Generate hints
	var suggestions []RobotAction
	var warnings []string

	switch output.Agent.State {
	case "error":
		warnings = append(warnings, "Agent is in error state")
		suggestions = append(suggestions, RobotAction{
			Action:   "investigate",
			Target:   fmt.Sprintf("pane %d", opts.PaneIndex),
			Reason:   "Error detected in output",
			Priority: 2,
		})
	case "waiting":
		suggestions = append(suggestions, RobotAction{
			Action:   "send_prompt",
			Target:   fmt.Sprintf("pane %d", opts.PaneIndex),
			Reason:   "Agent is idle and ready for work",
			Priority: 1,
		})
	}

	if len(output.Output.ErrorsFound) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d error(s) detected in recent output", len(output.Output.ErrorsFound)))
	}

	output.AgentHints = &AgentHints{
		Summary:          fmt.Sprintf("%s agent in %s state, %d lines of output", output.Agent.Type, output.Agent.State, output.Output.Lines),
		Warnings:         warnings,
		SuggestedActions: suggestions,
	}

	return output, nil
}

// PrintInspectPane outputs detailed pane inspection.
// This is a thin wrapper around GetInspectPane() for CLI output.
func PrintInspectPane(opts InspectPaneOptions) error {
	output, err := GetInspectPane(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

func newInspectSessionOutput(session string) *InspectSessionOutput {
	return &InspectSessionOutput{
		RobotResponse: NewRobotResponse(true),
		SchemaID:      defaultRobotSchemaID("inspect-session"),
		SchemaVersion: inspectSessionSchemaVersion,
		Session:       strings.TrimSpace(session),
		Detail: InspectSessionDetail{
			Agents: []InspectAgentDetail{},
		},
	}
}

func newInspectAgentOutput(agentID string) *InspectAgentOutput {
	return &InspectAgentOutput{
		RobotResponse: NewRobotResponse(true),
		SchemaID:      defaultRobotSchemaID("inspect-agent"),
		SchemaVersion: inspectAgentSchemaVersion,
		AgentID:       strings.TrimSpace(agentID),
	}
}

func inspectHealth(status state.HealthStatus, reason string) InspectHealth {
	return InspectHealth{
		Status: statusHealthString(status),
		Reason: strings.TrimSpace(reason),
	}
}

func inspectProjectionInfo(collectedAt, staleAfter time.Time) InspectProjectionInfo {
	return InspectProjectionInfo{
		Fresh:       time.Now().Before(staleAfter),
		CollectedAt: FormatTimestamp(collectedAt),
		StaleAfter:  FormatTimestamp(staleAfter),
	}
}

func inspectAgentDetailFromRuntime(agent state.RuntimeAgent) InspectAgentDetail {
	return InspectAgentDetail{
		ID:               strings.TrimSpace(agent.ID),
		Session:          strings.TrimSpace(agent.SessionName),
		Pane:             strings.TrimSpace(agent.Pane),
		Type:             strings.TrimSpace(agent.AgentType),
		Variant:          strings.TrimSpace(agent.Variant),
		TypeConfidence:   agent.TypeConfidence,
		TypeMethod:       strings.TrimSpace(agent.TypeMethod),
		State:            strings.TrimSpace(string(agent.State)),
		StateReason:      strings.TrimSpace(agent.StateReason),
		PreviousState:    strings.TrimSpace(agent.PreviousState),
		StateChangedAt:   FormatTimestampPtr(agent.StateChangedAt),
		LastOutputAt:     FormatTimestampPtr(agent.LastOutputAt),
		LastOutputAgeSec: agent.LastOutputAgeSec,
		OutputTailLines:  agent.OutputTailLines,
		CurrentBead:      strings.TrimSpace(agent.CurrentBead),
		PendingMail:      agent.PendingMail,
		AgentMailName:    strings.TrimSpace(agent.AgentMailName),
		Health:           inspectHealth(agent.HealthStatus, agent.HealthReason),
		Projection:       inspectProjectionInfo(agent.CollectedAt, agent.StaleAfter),
	}
}

func inspectSessionDetailFromRuntime(sess *state.RuntimeSession, agents []state.RuntimeAgent) InspectSessionDetail {
	detail := InspectSessionDetail{
		Name:           strings.TrimSpace(sess.Name),
		Label:          strings.TrimSpace(sess.Label),
		ProjectPath:    strings.TrimSpace(sess.ProjectPath),
		Attached:       sess.Attached,
		WindowCount:    sess.WindowCount,
		PaneCount:      sess.PaneCount,
		AgentCount:     sess.AgentCount,
		ActiveAgents:   sess.ActiveAgents,
		IdleAgents:     sess.IdleAgents,
		ErrorAgents:    sess.ErrorAgents,
		CreatedAt:      FormatTimestampPtr(sess.CreatedAt),
		LastAttachedAt: FormatTimestampPtr(sess.LastAttachedAt),
		LastActivityAt: FormatTimestampPtr(sess.LastActivityAt),
		Health:         inspectHealth(sess.HealthStatus, sess.HealthReason),
		Projection:     inspectProjectionInfo(sess.CollectedAt, sess.StaleAfter),
		Agents:         make([]InspectAgentDetail, 0, len(agents)),
	}
	for _, agent := range agents {
		detail.Agents = append(detail.Agents, inspectAgentDetailFromRuntime(agent))
	}
	if len(detail.Agents) > 0 {
		detail.AgentCount = len(detail.Agents)
	}
	return detail
}

// GetInspectSession returns projection-backed session detail.
func GetInspectSession(opts InspectSessionOptions) (*InspectSessionOutput, error) {
	output := newInspectSessionOutput(opts.Session)
	if strings.TrimSpace(opts.Session) == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session name required"),
			ErrCodeInvalidFlag,
			"Specify session with --robot-inspect-session=SESSION",
		)
		return output, nil
	}

	store := currentProjectionStore()
	if store == nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("runtime projection store unavailable"),
			ErrCodeNotImplemented,
			"Projection-backed inspect requires the shared runtime store; refresh snapshot/status after projection initialization",
		)
		return output, nil
	}

	session, err := store.GetRuntimeSession(strings.TrimSpace(opts.Session))
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Failed to load session projection")
		return output, nil
	}
	if session == nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session '%s' not found in runtime projection", strings.TrimSpace(opts.Session)),
			ErrCodeSessionNotFound,
			"Use ntm --robot-snapshot or ntm --robot-status to list projected sessions",
		)
		return output, nil
	}

	agents, err := store.GetRuntimeAgentsBySession(session.Name)
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Failed to load session agent projections")
		return output, nil
	}

	output.Session = session.Name
	output.Detail = inspectSessionDetailFromRuntime(session, agents)

	var warnings []string
	var notes []string
	if !output.Detail.Projection.Fresh {
		warnings = append(warnings, "Session projection is stale; refresh snapshot/status if you need the latest state")
	}
	if len(output.Detail.Agents) == 0 {
		notes = append(notes, "No fresh agent projections are attached to this session")
	} else {
		notes = append(notes, "Use returned agent ids with --robot-inspect-agent for per-agent drill-down")
	}

	output.AgentHints = &AgentHints{
		Summary:  fmt.Sprintf("Session %s: %d projected agents, health %s", output.Detail.Name, len(output.Detail.Agents), output.Detail.Health.Status),
		Warnings: warnings,
		Notes:    notes,
	}
	return output, nil
}

// PrintInspectSession outputs projection-backed session inspection.
func PrintInspectSession(opts InspectSessionOptions) error {
	output, err := GetInspectSession(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// GetInspectAgent returns projection-backed agent detail.
func GetInspectAgent(opts InspectAgentOptions) (*InspectAgentOutput, error) {
	output := newInspectAgentOutput(opts.AgentID)
	if strings.TrimSpace(opts.AgentID) == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("agent id required"),
			ErrCodeInvalidFlag,
			"Specify agent id with --robot-inspect-agent=SESSION:PANE",
		)
		return output, nil
	}

	store := currentProjectionStore()
	if store == nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("runtime projection store unavailable"),
			ErrCodeNotImplemented,
			"Projection-backed inspect requires the shared runtime store; refresh snapshot/status after projection initialization",
		)
		return output, nil
	}

	agent, err := store.GetRuntimeAgent(strings.TrimSpace(opts.AgentID))
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Failed to load agent projection")
		return output, nil
	}
	if agent == nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("agent '%s' not found in runtime projection", strings.TrimSpace(opts.AgentID)),
			ErrCodeInvalidFlag,
			"Use ntm --robot-inspect-session=SESSION to list stable agent ids for drill-down",
		)
		return output, nil
	}

	output.AgentID = strings.TrimSpace(agent.ID)
	output.Detail = inspectAgentDetailFromRuntime(*agent)

	var warnings []string
	var notes []string
	if !output.Detail.Projection.Fresh {
		warnings = append(warnings, "Agent projection is stale; refresh snapshot/status if you need the latest state")
	}
	if output.Detail.CurrentBead != "" {
		notes = append(notes, fmt.Sprintf("Current bead: %s", output.Detail.CurrentBead))
	}
	notes = append(notes, fmt.Sprintf("Session drill-down: ntm --robot-inspect-session=%s", output.Detail.Session))

	output.AgentHints = &AgentHints{
		Summary:  fmt.Sprintf("Agent %s: %s %s", output.Detail.ID, output.Detail.Type, output.Detail.State),
		Warnings: warnings,
		Notes:    notes,
	}
	return output, nil
}

// PrintInspectAgent outputs projection-backed agent inspection.
func PrintInspectAgent(opts InspectAgentOptions) error {
	output, err := GetInspectAgent(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Metrics Export (--robot-metrics)
// =============================================================================
// Exports session metrics in various formats for analysis

// MetricsOutput represents comprehensive session metrics
type MetricsOutput struct {
	RobotResponse
	Session      string                  `json:"session,omitempty"`
	Period       string                  `json:"period"` // e.g., "last_24h", "all_time"
	TokenUsage   MetricsTokenUsage       `json:"token_usage"`
	AgentStats   map[string]AgentMetrics `json:"agent_stats"`
	SessionStats MetricsSessionStats     `json:"session_stats"`
	AgentHints   *AgentHints             `json:"_agent_hints,omitempty"`
}

// MetricsTokenUsage contains token consumption data
type MetricsTokenUsage struct {
	TotalTokens    int64            `json:"total_tokens"`
	TotalCost      float64          `json:"total_cost_usd"`
	ByAgent        map[string]int64 `json:"by_agent"`
	ByModel        map[string]int64 `json:"by_model"`
	ContextCurrent map[string]int   `json:"context_current_percent"` // Current context usage per agent
}

// AgentMetrics contains per-agent statistics
type AgentMetrics struct {
	Type            string  `json:"type"`
	PromptsReceived int     `json:"prompts_received"`
	TokensUsed      int64   `json:"tokens_used"`
	AvgResponseTime float64 `json:"avg_response_time_sec"`
	ErrorCount      int     `json:"error_count"`
	RestartCount    int     `json:"restart_count"`
	Uptime          string  `json:"uptime"`
}

// MetricsSessionStats contains session-level statistics
type MetricsSessionStats struct {
	TotalPrompts    int    `json:"total_prompts"`
	TotalAgents     int    `json:"total_agents"`
	ActiveAgents    int    `json:"active_agents"`
	SessionDuration string `json:"session_duration"`
	FilesChanged    int    `json:"files_changed"`
	Commits         int    `json:"commits,omitempty"`
}

// MetricsOptions configures the metrics export
type MetricsOptions struct {
	Session string // Filter to specific session
	Period  string // "1h", "24h", "7d", "all" (default: "24h")
	Format  string // "json", "csv" (default: "json")
}

// GetMetrics returns session metrics data.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetMetrics(opts MetricsOptions) (*MetricsOutput, error) {
	if opts.Period == "" {
		opts.Period = "24h"
	}

	output := &MetricsOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		Period:        opts.Period,
		TokenUsage: MetricsTokenUsage{
			ByAgent:        make(map[string]int64),
			ByModel:        make(map[string]int64),
			ContextCurrent: make(map[string]int),
		},
		AgentStats: make(map[string]AgentMetrics),
	}

	// Get session info if specified
	if opts.Session != "" {
		if !tmux.SessionExists(opts.Session) {
			return &MetricsOutput{
				RobotResponse: NewErrorResponse(
					fmt.Errorf("session '%s' not found", opts.Session),
					ErrCodeSessionNotFound,
					"Use 'ntm list' to see available sessions",
				),
				Session: opts.Session,
				Period:  opts.Period,
				TokenUsage: MetricsTokenUsage{
					ByAgent:        make(map[string]int64),
					ByModel:        make(map[string]int64),
					ContextCurrent: make(map[string]int),
				},
				AgentStats: make(map[string]AgentMetrics),
			}, nil
		}

		panes, err := tmux.GetPanes(opts.Session)
		if err == nil {
			output.SessionStats.TotalAgents = len(panes)

			for _, pane := range panes {
				agentType := string(pane.Type)
				if agentType == "" || agentType == "unknown" {
					continue
				}

				if _, exists := output.AgentStats[pane.Title]; !exists {
					output.AgentStats[pane.Title] = AgentMetrics{
						Type: agentType,
					}
				}
				output.SessionStats.ActiveAgents++
			}
		}
	}

	// Get file change count
	fileStore := tracker.GlobalFileChanges
	if fileStore != nil {
		changes := fileStore.All()
		uniqueFiles := make(map[string]struct{})
		for _, c := range changes {
			if opts.Session == "" || c.Session == opts.Session {
				uniqueFiles[c.Change.Path] = struct{}{}
			}
		}
		output.SessionStats.FilesChanged = len(uniqueFiles)
	}

	sessionDesc := opts.Session
	if sessionDesc == "" {
		sessionDesc = "all sessions"
	}
	output.AgentHints = &AgentHints{
		Summary: fmt.Sprintf("Metrics for %s over %s", sessionDesc, opts.Period),
		Notes:   []string{"Token usage requires integration with provider APIs for accurate data"},
	}

	return output, nil
}

// PrintMetrics outputs session metrics.
// This is a thin wrapper around GetMetrics() for CLI output.
func PrintMetrics(opts MetricsOptions) error {
	output, err := GetMetrics(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Replay Command (--robot-replay)
// =============================================================================
// Replays a command from history, equivalent to the History panel replay action

// ReplayOutput represents the result of a replay operation
type ReplayOutput struct {
	RobotResponse
	HistoryID   string      `json:"history_id"`
	OriginalCmd string      `json:"original_command"`
	Session     string      `json:"session"`
	TargetPanes []int       `json:"target_panes"`
	Replayed    bool        `json:"replayed"`
	AgentHints  *AgentHints `json:"_agent_hints,omitempty"`
}

// ReplayOptions configures the replay operation
type ReplayOptions struct {
	Session   string
	HistoryID string // History entry ID to replay
	DryRun    bool   // Just show what would be replayed
}

// GetReplay returns replay operation result.
// This function returns the data struct directly, enabling CLI/REST parity.
// Note: When DryRun is false, this executes the replay and returns nil (send handles output).
func GetReplay(opts ReplayOptions) (*ReplayOutput, error) {
	output := &ReplayOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		HistoryID:     opts.HistoryID,
		TargetPanes:   []int{},
	}

	// Get history entries
	entries, err := history.ReadRecent(100)
	if err != nil {
		return &ReplayOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("history tracking not available: %w", err),
				ErrCodeDependencyMissing,
				"History is recorded during send operations",
			),
			HistoryID:   opts.HistoryID,
			TargetPanes: []int{},
		}, nil
	}

	// Find the history entry
	var target *history.HistoryEntry
	for i := range entries {
		if entries[i].ID == opts.HistoryID {
			target = &entries[i]
			break
		}
	}

	if target == nil {
		return &ReplayOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("history entry '%s' not found", opts.HistoryID),
				ErrCodeInvalidFlag,
				"Use --robot-history to see available entries",
			),
			HistoryID:   opts.HistoryID,
			TargetPanes: []int{},
		}, nil
	}

	output.OriginalCmd = target.Prompt
	// Convert string targets to int
	for _, t := range target.Targets {
		// Targets are stored as strings, but we expose as ints
		var idx int
		if _, err := fmt.Sscanf(t, "%d", &idx); err == nil {
			output.TargetPanes = append(output.TargetPanes, idx)
		}
	}

	if opts.DryRun {
		output.Replayed = false
		output.AgentHints = &AgentHints{
			Summary: fmt.Sprintf("Would replay: %s", truncateString(target.Prompt, 50)),
			Notes:   []string{"Use without --replay-dry-run to execute"},
		}
		return output, nil
	}

	// Execute the replay by calling send logic
	// Build pane filter from original targets
	paneFilter := append([]string{}, target.Targets...)

	sendOpts := SendOptions{
		Session: target.Session,
		Message: target.Prompt,
		Panes:   paneFilter,
	}

	// Execute the send - GetSend handles the actual operation
	sendOutput, err := GetSend(sendOpts)
	if err != nil {
		return nil, err
	}

	// Return nil to signal that PrintReplay should use send output directly
	// This is a special case where we delegate to another command
	_ = sendOutput // Used by PrintReplay
	return nil, nil
}

// PrintReplay outputs replay operation result.
// This is a thin wrapper around GetReplay() for CLI output.
func PrintReplay(opts ReplayOptions) error {
	output, err := GetReplay(opts)
	if err != nil {
		return err
	}
	// nil output means GetReplay delegated to send
	if output == nil {
		// Execute send directly for non-dry-run case
		entries, _ := history.ReadRecent(100)
		for i := range entries {
			if entries[i].ID == opts.HistoryID {
				sendOpts := SendOptions{
					Session: entries[i].Session,
					Message: entries[i].Prompt,
					Panes:   append([]string{}, entries[i].Targets...),
				}
				return PrintSend(sendOpts)
			}
		}
	}
	return encodeJSON(output)
}

// =============================================================================
// Palette Info (--robot-palette)
// =============================================================================
// Queries command palette information - available commands, favorites, recents

// PaletteOutput represents palette state and available commands
type PaletteOutput struct {
	RobotResponse
	Session    string          `json:"session,omitempty"`
	Commands   []PaletteCmd    `json:"commands"`
	Favorites  []string        `json:"favorites"`
	Pinned     []string        `json:"pinned"`
	Recent     []PaletteRecent `json:"recent"`
	Categories []string        `json:"categories"`
	AgentHints *AgentHints     `json:"_agent_hints,omitempty"`
}

// PaletteCmd represents a single palette command
type PaletteCmd struct {
	Key        string   `json:"key"`
	Label      string   `json:"label"`
	Category   string   `json:"category"`
	Prompt     string   `json:"prompt"`
	Targets    string   `json:"targets,omitempty"` // "all", "claude", etc.
	IsFavorite bool     `json:"is_favorite"`
	IsPinned   bool     `json:"is_pinned"`
	UseCount   int      `json:"use_count"`
	Tags       []string `json:"tags,omitempty"`
}

// PaletteRecent represents a recently used command
type PaletteRecent struct {
	Key     string `json:"key"`
	UsedAt  string `json:"used_at"` // RFC3339
	Session string `json:"session"`
	Success bool   `json:"success"`
}

// PaletteOptions configures the palette query
type PaletteOptions struct {
	Session     string // Filter recents to session
	Category    string // Filter commands by category
	SearchQuery string // Filter commands by search term
}

// GetPalette returns palette information.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetPalette(cfg *config.Config, opts PaletteOptions) (*PaletteOutput, error) {
	if cfg == nil {
		cfg = config.Default()
	}

	output := &PaletteOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		Commands:      []PaletteCmd{},
		Favorites:     []string{},
		Pinned:        []string{},
		Recent:        []PaletteRecent{},
		Categories:    []string{},
	}

	seen := make(map[string]struct{})
	query := strings.ToLower(opts.SearchQuery)
	matchesFilters := func(key, label, category string) bool {
		if opts.Category != "" && category != opts.Category {
			return false
		}
		if opts.SearchQuery == "" {
			return true
		}
		return strings.Contains(strings.ToLower(label), query) ||
			strings.Contains(strings.ToLower(key), query)
	}

	// Get commands from config
	categorySet := make(map[string]struct{})
	for _, cmd := range cfg.Palette {
		if !matchesFilters(cmd.Key, cmd.Label, cmd.Category) {
			continue
		}

		palCmd := PaletteCmd{
			Key:      cmd.Key,
			Label:    cmd.Label,
			Category: cmd.Category,
			Prompt:   cmd.Prompt,
			Tags:     cmd.Tags,
		}

		output.Commands = append(output.Commands, palCmd)
		categorySet[cmd.Category] = struct{}{}
		if cmd.Key != "" {
			seen[cmd.Key] = struct{}{}
		}
	}

	for _, cmd := range kernel.List() {
		key := strings.TrimSpace(cmd.Name)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}

		label := strings.TrimSpace(cmd.Description)
		if label == "" {
			label = key
		}
		category := strings.TrimSpace(cmd.Category)
		if category == "" {
			category = "kernel"
		}

		if !matchesFilters(key, label, category) {
			continue
		}

		// Use the command's description as the prompt text sent to agent panes.
		// NEVER use Examples[0].Command — examples are CLI documentation strings
		// that may contain dangerous arguments (e.g. "rm -rf /") and must not be
		// sent verbatim to AI agents. See ntm#56.
		prompt := strings.TrimSpace(cmd.Description)
		if prompt == "" {
			prompt = label
		}

		palCmd := PaletteCmd{
			Key:      key,
			Label:    label,
			Category: category,
			Prompt:   prompt,
			Tags:     []string{"kernel"},
		}

		output.Commands = append(output.Commands, palCmd)
		categorySet[category] = struct{}{}
		seen[key] = struct{}{}
	}

	// Inject built-in xf search command when xf integration is enabled
	if cfg.Integrations.XF.Enabled {
		xfKey := "xf-search"
		if _, exists := seen[xfKey]; !exists {
			if matchesFilters(xfKey, "XF Archive Search", "xf") {
				output.Commands = append(output.Commands, PaletteCmd{
					Key:      xfKey,
					Label:    "XF Archive Search",
					Category: "xf",
					Prompt:   "Search X/Twitter archive (Ctrl+K)",
					Tags:     []string{"xf", "search", "twitter"},
				})
				categorySet["xf"] = struct{}{}
				seen[xfKey] = struct{}{}
			}
		}
	}

	for cat := range categorySet {
		output.Categories = append(output.Categories, cat)
	}

	output.AgentHints = &AgentHints{
		Summary: fmt.Sprintf("%d commands available across %d categories", len(output.Commands), len(output.Categories)),
		Notes:   []string{"Use --robot-send with a prompt to send commands to agents"},
	}

	return output, nil
}

// PrintPalette outputs palette information.
// This is a thin wrapper around GetPalette() for CLI output.
func PrintPalette(cfg *config.Config, opts PaletteOptions) error {
	output, err := GetPalette(cfg, opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Alerts Management (--robot-dismiss-alert)
// =============================================================================
// Provides alert dismissal capabilities, complementing PrintAlertsDetailed in robot.go.

// TUIAlertsOutput represents active alerts with TUI-parity fields
type TUIAlertsOutput struct {
	RobotResponse
	Session    string         `json:"session,omitempty"`
	Count      int            `json:"count"`
	Alerts     []TUIAlertInfo `json:"alerts"`
	Dismissed  []string       `json:"dismissed,omitempty"` // IDs of dismissed alerts
	AgentHints *AgentHints    `json:"_agent_hints,omitempty"`
}

// TUIAlertInfo represents a single alert with TUI-parity fields
type TUIAlertInfo struct {
	ID          string `json:"id"`
	Type        string `json:"type"`     // "agent_stuck", "disk_low", "mail_backlog", etc.
	Severity    string `json:"severity"` // "info", "warning", "error", "critical"
	Session     string `json:"session,omitempty"`
	Pane        string `json:"pane,omitempty"`
	Message     string `json:"message"`
	CreatedAt   string `json:"created_at"` // RFC3339
	AgeSeconds  int    `json:"age_seconds"`
	Dismissible bool   `json:"dismissible"`
}

// TUIAlertsOptions configures alerts query for TUI parity
type TUIAlertsOptions struct {
	Session  string
	Severity string // Filter by severity
	Type     string // Filter by type
}

// GetAlertsTUI returns current alerts with TUI-parity formatting.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetAlertsTUI(cfg *config.Config, opts TUIAlertsOptions) (*TUIAlertsOutput, error) {
	output := &TUIAlertsOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       opts.Session,
		Alerts:        []TUIAlertInfo{},
	}

	// Get alerts from the alerts package
	alertCfg := alerts.DefaultConfig()
	if cfg != nil && cfg.Alerts.Enabled {
		alertCfg = alerts.Config{
			Enabled:              cfg.Alerts.Enabled,
			AgentStuckMinutes:    cfg.Alerts.AgentStuckMinutes,
			DiskLowThresholdGB:   cfg.Alerts.DiskLowThresholdGB,
			MailBacklogThreshold: cfg.Alerts.MailBacklogThreshold,
			BeadStaleHours:       cfg.Alerts.BeadStaleHours,
		}
	}
	alertList := alerts.GetActiveAlerts(alertCfg)

	now := time.Now()
	for _, a := range alertList {
		// Apply filters
		if opts.Session != "" && a.Session != opts.Session {
			continue
		}
		if opts.Severity != "" && string(a.Severity) != opts.Severity {
			continue
		}
		if opts.Type != "" && string(a.Type) != opts.Type {
			continue
		}

		info := TUIAlertInfo{
			ID:          a.ID,
			Type:        string(a.Type),
			Severity:    string(a.Severity),
			Session:     a.Session,
			Pane:        a.Pane,
			Message:     a.Message,
			CreatedAt:   FormatTimestamp(a.CreatedAt),
			AgeSeconds:  int(now.Sub(a.CreatedAt).Seconds()),
			Dismissible: true,
		}

		output.Alerts = append(output.Alerts, info)
	}

	output.Count = len(output.Alerts)

	// Generate hints
	var warnings []string
	if output.Count > 5 {
		warnings = append(warnings, fmt.Sprintf("%d active alerts - consider addressing", output.Count))
	}

	criticalCount := 0
	for _, a := range output.Alerts {
		if a.Severity == "critical" || a.Severity == "error" {
			criticalCount++
		}
	}

	if criticalCount > 0 {
		warnings = append(warnings, fmt.Sprintf("%d critical/error alerts require attention", criticalCount))
	}

	output.AgentHints = &AgentHints{
		Summary:  fmt.Sprintf("%d active alerts", output.Count),
		Warnings: warnings,
	}

	return output, nil
}

// PrintAlertsTUI outputs current alerts with TUI-parity formatting.
// This is a thin wrapper around GetAlertsTUI() for CLI output.
func PrintAlertsTUI(cfg *config.Config, opts TUIAlertsOptions) error {
	output, err := GetAlertsTUI(cfg, opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// DismissAlertOutput represents the result of dismissing an alert
type DismissAlertOutput struct {
	RobotResponse
	AlertID    string      `json:"alert_id"`
	Dismissed  bool        `json:"dismissed"`
	AgentHints *AgentHints `json:"_agent_hints,omitempty"`
}

// DismissAlertOptions configures alert dismissal
type DismissAlertOptions struct {
	AlertID    string
	Session    string // Scope to session
	DismissAll bool   // Dismiss all alerts matching criteria
}

// GetDismissAlert dismisses an alert and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
// Note: Alert dismissal is session-local and non-persistent in this implementation.
// Future versions may persist dismissals.
func GetDismissAlert(opts DismissAlertOptions) (*DismissAlertOutput, error) {
	output := &DismissAlertOutput{
		RobotResponse: NewRobotResponse(true),
		AlertID:       opts.AlertID,
	}

	if opts.AlertID == "" && !opts.DismissAll {
		return &DismissAlertOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("alert ID required"),
				ErrCodeInvalidFlag,
				"Specify --robot-dismiss-alert=ALERT_ID or use --dismiss-all",
			),
			AlertID: opts.AlertID,
		}, nil
	}

	// Note: Full alert dismissal with persistence requires tracker integration.
	// For now, we track dismissed IDs in a session-local set.
	// This is a best-effort implementation until alerts.Dismiss is available.
	if opts.AlertID != "" {
		// Record the dismissal intent (actual implementation would persist this)
		output.Dismissed = true
		output.AgentHints = &AgentHints{
			Summary: fmt.Sprintf("Alert %s marked for dismissal", opts.AlertID),
			Notes:   []string{"Alert dismissal is session-local; alert may reappear if condition persists"},
		}
	}

	return output, nil
}

// PrintDismissAlert dismisses an alert and outputs the result.
// This is a thin wrapper around GetDismissAlert() for CLI output.
func PrintDismissAlert(opts DismissAlertOptions) error {
	output, err := GetDismissAlert(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Helper Functions
// =============================================================================

// detectErrors scans output lines for error patterns
func detectErrors(lines []string) []string {
	var errors []string
	errorPatterns := []string{
		"error:",
		"Error:",
		"ERROR:",
		"failed:",
		"Failed:",
		"FAILED:",
		"panic:",
		"Panic:",
		"exception:",
		"Exception:",
		"traceback",
		"Traceback",
	}

	for _, line := range lines {
		for _, pattern := range errorPatterns {
			if strings.Contains(line, pattern) {
				// Truncate long error messages
				if len(line) > 200 {
					line = line[:200] + "..."
				}
				errors = append(errors, line)
				break
			}
		}
	}

	// Limit to 10 errors
	if len(errors) > 10 {
		errors = errors[:10]
	}

	return errors
}

// parseCodeBlocks extracts code block information from output
func parseCodeBlocks(lines []string) []CodeBlockInfo {
	var blocks []CodeBlockInfo
	inBlock := false
	var currentBlock CodeBlockInfo

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inBlock {
				// Start of block
				inBlock = true
				currentBlock = CodeBlockInfo{
					LineStart: i,
					Language:  strings.TrimPrefix(trimmed, "```"),
				}
			} else {
				// End of block
				currentBlock.LineEnd = i
				blocks = append(blocks, currentBlock)
				inBlock = false
			}
		}
	}

	return blocks
}

// extractFileReferences finds file paths mentioned in output
func extractFileReferences(lines []string) []string {
	files := make(map[string]struct{})

	for _, line := range lines {
		// Look for common file path patterns
		// This is a simplified heuristic
		words := strings.Fields(line)
		for _, word := range words {
			// Clean up the word
			word = strings.Trim(word, "\"'`()[]{},:;")

			// Check if it looks like a file path
			if strings.Contains(word, "/") || strings.Contains(word, ".") {
				if isLikelyFilePath(word) {
					files[word] = struct{}{}
				}
			}
		}
	}

	var result []string
	for f := range files {
		result = append(result, f)
	}

	// Limit results
	if len(result) > 20 {
		result = result[:20]
	}

	return result
}

// isLikelyFilePath checks if a string looks like a file path
func isLikelyFilePath(s string) bool {
	// Must have an extension or look like a path
	if !strings.Contains(s, ".") && !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "./") {
		return false
	}

	// Common file extensions
	extensions := []string{".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".json", ".yaml", ".yml", ".toml", ".md", ".txt", ".css", ".html", ".sh", ".bash"}
	for _, ext := range extensions {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}

	// Looks like a relative or absolute path
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}

	return false
}

// truncateString truncates a string to max length with ellipsis.
// Respects UTF-8 rune boundaries.
func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return "..."[:max]
	}
	// Find last rune boundary at or before max-3 bytes
	targetLen := max - 3
	prevI := 0
	for i := range s {
		if i > targetLen {
			return s[:prevI] + "..."
		}
		prevI = i
	}
	return s[:prevI] + "..."
}

// =============================================================================
// Bead Listing (--robot-beads-list)
// =============================================================================
// Provides programmatic bead listing for AI agents, mirroring TUI beads panel.

// BeadsListOptions configures the bead listing query
type BeadsListOptions struct {
	Status   string // Filter by status: open, in_progress, closed, blocked
	Priority string // Filter by priority: 0-4 or P0-P4
	Assignee string // Filter by assignee
	Type     string // Filter by type: task, bug, feature, epic, chore
	Limit    int    // Max beads to return
}

// BeadListItem represents a single bead in the list output
type BeadListItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Priority    string   `json:"priority"`
	Type        string   `json:"type"`
	Assignee    string   `json:"assignee,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	BlockedBy   []string `json:"blocked_by,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	IsReady     bool     `json:"is_ready"`
	IsBlocked   bool     `json:"is_blocked"`
	Description string   `json:"description,omitempty"`
}

// BeadsListOutput represents the result of listing beads
type BeadsListOutput struct {
	RobotResponse
	Beads      []BeadListItem   `json:"beads"`
	Total      int              `json:"total"`
	Filtered   int              `json:"filtered"`
	Summary    BeadsListSummary `json:"summary"`
	AgentHints *AgentHints      `json:"_agent_hints,omitempty"`
}

// BeadsListSummary provides counts by status for bead listing
type BeadsListSummary struct {
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Blocked    int `json:"blocked"`
	Closed     int `json:"closed"`
	Ready      int `json:"ready"`
}

// GetBeadsList returns beads list with optional filtering.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBeadsList(opts BeadsListOptions) (*BeadsListOutput, error) {
	output := &BeadsListOutput{
		RobotResponse: NewRobotResponse(true),
		Beads:         []BeadListItem{},
	}

	// Check if bv/bd is installed
	if !bv.IsInstalled() {
		return &BeadsListOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("beads system not available"),
				ErrCodeDependencyMissing,
				"Install bv/bd or run 'bd init' in your project",
			),
			Beads: []BeadListItem{},
		}, nil
	}

	// Build bd list command with filters
	args := []string{"list", "--json"}

	// Add status filter
	if opts.Status != "" {
		args = append(args, "--status="+opts.Status)
	}

	// Add priority filter (normalize P0-P4 to 0-4)
	if opts.Priority != "" {
		priority := opts.Priority
		if len(priority) == 2 && (priority[0] == 'P' || priority[0] == 'p') {
			priority = string(priority[1])
		}
		args = append(args, "--priority="+priority)
	}

	// Add assignee filter
	if opts.Assignee != "" {
		args = append(args, "--assignee="+opts.Assignee)
	}

	// Add type filter
	if opts.Type != "" {
		args = append(args, "--type="+opts.Type)
	}

	// Execute bd list
	result, err := bv.RunBd("", args...)
	if err != nil {
		// Check if this is just "no beads" vs actual error
		if strings.Contains(err.Error(), "no .beads") || strings.Contains(err.Error(), "not initialized") {
			output.AgentHints = &AgentHints{
				Summary: "Beads not initialized in this project",
				Notes:   []string{"Run 'bd init' to initialize beads tracking"},
			}
			return output, nil
		}
		return &BeadsListOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to list beads: %w", err),
				ErrCodeInternalError,
				"Check that bd is installed and .beads/ exists",
			),
			Beads: []BeadListItem{},
		}, nil
	}

	// Parse bd list output
	// Note: bd list returns issue_type (not type), and doesn't include blocked_by
	// The status field already indicates if a bead is blocked
	var rawBeads []struct {
		ID              string   `json:"id"`
		Title           string   `json:"title"`
		Status          string   `json:"status"`
		Priority        int      `json:"priority"`
		IssueType       string   `json:"issue_type"`
		Assignee        string   `json:"assignee"`
		Labels          []string `json:"labels"`
		CreatedAt       string   `json:"created_at"`
		UpdatedAt       string   `json:"updated_at"`
		Description     string   `json:"description"`
		DependencyCount int      `json:"dependency_count"`
	}

	if err := json.Unmarshal([]byte(result), &rawBeads); err != nil {
		// Try parsing as single object (some bd versions return differently)
		return &BeadsListOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to parse bead list: %w", err),
				ErrCodeInternalError,
				"Unexpected bd output format",
			),
			Beads: []BeadListItem{},
		}, nil
	}

	// Compute summary from the full result set (before applying limit)
	// This gives accurate counts - limit only affects returned items, not summary
	// Note: bd status "blocked" means unmet dependencies, "open" means ready to work
	for _, rb := range rawBeads {
		switch rb.Status {
		case "open":
			output.Summary.Open++
			output.Summary.Ready++ // open status means ready (no unmet deps)
		case "in_progress":
			output.Summary.InProgress++
		case "blocked":
			output.Summary.Blocked++
		case "closed":
			output.Summary.Closed++
		}
	}

	output.Total = len(rawBeads)

	// Apply limit for the returned items
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	// Convert to output format (limited)
	for i, rb := range rawBeads {
		if i >= limit {
			break
		}

		// Determine ready/blocked status from bd status field
		isBlocked := rb.Status == "blocked"
		isReady := rb.Status == "open" // open means ready (no unmet deps)

		// Format priority as P0-P4
		priorityStr := fmt.Sprintf("P%d", rb.Priority)

		item := BeadListItem{
			ID:          rb.ID,
			Title:       rb.Title,
			Status:      rb.Status,
			Priority:    priorityStr,
			Type:        rb.IssueType, // Use IssueType from bd output
			Assignee:    rb.Assignee,
			Labels:      rb.Labels,
			CreatedAt:   rb.CreatedAt,
			UpdatedAt:   rb.UpdatedAt,
			IsReady:     isReady,
			IsBlocked:   isBlocked,
			Description: rb.Description,
		}
		output.Beads = append(output.Beads, item)
	}

	output.Filtered = len(output.Beads)

	// Generate agent hints
	var notes []string
	var warnings []string

	if output.Summary.Ready > 0 {
		notes = append(notes, fmt.Sprintf("Claim one of %d ready beads with --robot-bead-claim=ID", output.Summary.Ready))
	}
	if output.Summary.InProgress > 0 {
		notes = append(notes, fmt.Sprintf("Review %d in-progress beads", output.Summary.InProgress))
	}
	if output.Summary.Blocked > 0 {
		warnings = append(warnings, fmt.Sprintf("%d beads are blocked by dependencies", output.Summary.Blocked))
	}
	if output.Total == 0 {
		notes = append(notes, "Create new beads with --robot-bead-create --bead-title='...'")
	}

	output.AgentHints = &AgentHints{
		Summary:  fmt.Sprintf("%d beads (%d ready, %d in progress)", output.Total, output.Summary.Ready, output.Summary.InProgress),
		Notes:    notes,
		Warnings: warnings,
	}

	return output, nil
}

// PrintBeadsList lists beads with optional filtering.
// This is a thin wrapper around GetBeadsList() for CLI output.
func PrintBeadsList(opts BeadsListOptions) error {
	output, err := GetBeadsList(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// =============================================================================
// Bead Management (--robot-bead-claim, --robot-bead-create, --robot-bead-show)
// =============================================================================
// Provides programmatic bead operations for AI agents, mirroring TUI beads panel actions.

// BeadClaimOutput represents the result of claiming a bead
type BeadClaimOutput struct {
	RobotResponse
	BeadID     string      `json:"bead_id"`
	Title      string      `json:"title"`
	PrevStatus string      `json:"prev_status,omitempty"`
	NewStatus  string      `json:"new_status"`
	Claimed    bool        `json:"claimed"`
	AgentHints *AgentHints `json:"_agent_hints,omitempty"`
}

// BeadClaimOptions configures the claim operation
type BeadClaimOptions struct {
	BeadID   string // Bead ID to claim (e.g., "ntm-abc123")
	Assignee string // Optional assignee name
}

// GetBeadClaim claims a bead by setting its status to in_progress.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBeadClaim(opts BeadClaimOptions) (*BeadClaimOutput, error) {
	if opts.BeadID == "" {
		return &BeadClaimOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead ID required"),
				ErrCodeInvalidFlag,
				"Specify --robot-bead-claim=BEAD_ID",
			),
		}, nil
	}

	output := &BeadClaimOutput{
		RobotResponse: NewRobotResponse(true),
		BeadID:        opts.BeadID,
	}

	// Get current bead info first
	showOutput, err := bv.RunBd("", "show", opts.BeadID, "--json")
	if err != nil {
		return &BeadClaimOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead '%s' not found: %w", opts.BeadID, err),
				ErrCodeInvalidFlag,
				"Use 'bd list --status=open' to see available beads",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	// Parse bead info - bd show returns an array
	var beadInfo []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(showOutput), &beadInfo); err != nil || len(beadInfo) == 0 {
		return &BeadClaimOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to parse bead info"),
				ErrCodeInternalError,
				"Bead data may be corrupted",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	output.Title = beadInfo[0].Title
	output.PrevStatus = beadInfo[0].Status

	// Check if already in_progress
	if beadInfo[0].Status == "in_progress" {
		output.NewStatus = "in_progress"
		output.Claimed = false
		output.AgentHints = &AgentHints{
			Summary:  fmt.Sprintf("Bead %s is already in progress", opts.BeadID),
			Warnings: []string{"Bead was already claimed"},
		}
		return output, nil
	}

	// Claim the bead
	args := []string{"update", opts.BeadID, "--status=in_progress", "--json"}
	if opts.Assignee != "" {
		args = append(args, "--assignee="+opts.Assignee)
	}

	_, err = bv.RunBd("", args...)
	if err != nil {
		return &BeadClaimOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to claim bead: %w", err),
				ErrCodeInternalError,
				"Check if bead is blocked by dependencies",
			),
			BeadID:     opts.BeadID,
			Title:      output.Title,
			PrevStatus: output.PrevStatus,
		}, nil
	}

	output.NewStatus = "in_progress"
	output.Claimed = true
	output.AgentHints = &AgentHints{
		Summary: fmt.Sprintf("Claimed bead %s: %s", opts.BeadID, truncateString(output.Title, 50)),
		Notes:   []string{"Use 'bd close " + opts.BeadID + "' when complete"},
	}

	return output, nil
}

// PrintBeadClaim claims a bead by setting its status to in_progress.
// This is a thin wrapper around GetBeadClaim() for CLI output.
func PrintBeadClaim(opts BeadClaimOptions) error {
	output, err := GetBeadClaim(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// BeadCreateOutput represents the result of creating a bead
type BeadCreateOutput struct {
	RobotResponse
	BeadID      string      `json:"bead_id"`
	Title       string      `json:"title"`
	Type        string      `json:"type"`
	Priority    string      `json:"priority"`
	Description string      `json:"description,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Created     bool        `json:"created"`
	AgentHints  *AgentHints `json:"_agent_hints,omitempty"`
}

// BeadCreateOptions configures bead creation
type BeadCreateOptions struct {
	Title       string   // Required: bead title
	Type        string   // task, bug, feature, epic, chore (default: task)
	Priority    int      // 0-4 (default: 2)
	Description string   // Optional description
	Labels      []string // Optional labels
	DependsOn   []string // Optional dependency IDs
}

// GetBeadCreate creates a new bead and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBeadCreate(opts BeadCreateOptions) (*BeadCreateOutput, error) {
	if opts.Title == "" {
		return &BeadCreateOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("title required"),
				ErrCodeInvalidFlag,
				"Specify --bead-title='Your title'",
			),
		}, nil
	}

	// Set defaults
	if opts.Type == "" {
		opts.Type = "task"
	}
	if opts.Priority < 0 || opts.Priority > 4 {
		opts.Priority = 2
	}

	output := &BeadCreateOutput{
		RobotResponse: NewRobotResponse(true),
		Title:         opts.Title,
		Type:          opts.Type,
		Priority:      fmt.Sprintf("P%d", opts.Priority),
		Description:   opts.Description,
		Labels:        opts.Labels,
	}

	// Build bd create command
	args := []string{
		"create",
		"--json",
		"--type", opts.Type,
		"--priority", fmt.Sprintf("%d", opts.Priority),
		"--title", opts.Title,
	}

	if opts.Description != "" {
		args = append(args, "--description", opts.Description)
	}

	if len(opts.Labels) > 0 {
		args = append(args, "--labels", strings.Join(opts.Labels, ","))
	}

	// Execute creation
	createOutput, err := bv.RunBd("", args...)
	if err != nil {
		return &BeadCreateOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to create bead: %w", err),
				ErrCodeInternalError,
				"Check bd is installed and .beads/ directory exists",
			),
			Title:    opts.Title,
			Type:     opts.Type,
			Priority: fmt.Sprintf("P%d", opts.Priority),
		}, nil
	}

	// Parse the result to get the bead ID
	// bd create returns a single object, not an array
	var singleResult struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(createOutput), &singleResult); err != nil {
		// Try array format as fallback
		var arrayResult []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(createOutput), &arrayResult); err != nil || len(arrayResult) == 0 {
			return &BeadCreateOutput{
				RobotResponse: NewErrorResponse(
					fmt.Errorf("failed to parse created bead ID"),
					ErrCodeInternalError,
					"Bead may have been created but ID not returned",
				),
				Title:    opts.Title,
				Type:     opts.Type,
				Priority: fmt.Sprintf("P%d", opts.Priority),
			}, nil
		}
		singleResult.ID = arrayResult[0].ID
	}
	if singleResult.ID == "" {
		return &BeadCreateOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to parse created bead ID"),
				ErrCodeInternalError,
				"Bead may have been created but ID not returned",
			),
			Title:    opts.Title,
			Type:     opts.Type,
			Priority: fmt.Sprintf("P%d", opts.Priority),
		}, nil
	}

	output.BeadID = singleResult.ID
	output.Created = true

	// Add dependencies if specified
	var depWarnings []string
	for _, dep := range opts.DependsOn {
		_, depErr := bv.RunBd("", "dep", "add", output.BeadID, dep)
		if depErr != nil {
			// Non-fatal, just note it
			depWarnings = append(depWarnings,
				fmt.Sprintf("Failed to add dependency %s: %v", dep, depErr))
		}
	}

	output.AgentHints = &AgentHints{
		Summary: fmt.Sprintf("Created %s: %s", output.BeadID, truncateString(output.Title, 40)),
		Notes: []string{
			fmt.Sprintf("Claim with: bd update %s --status=in_progress", output.BeadID),
			fmt.Sprintf("View with: bd show %s", output.BeadID),
		},
		Warnings: depWarnings, // Preserve any dependency warnings
	}

	return output, nil
}

// PrintBeadCreate creates a new bead.
// This is a thin wrapper around GetBeadCreate() for CLI output.
func PrintBeadCreate(opts BeadCreateOptions) error {
	output, err := GetBeadCreate(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// BeadShowOutput represents detailed bead information
type BeadShowOutput struct {
	RobotResponse
	BeadID      string        `json:"bead_id"`
	Title       string        `json:"title"`
	Status      string        `json:"status"`
	Type        string        `json:"type"`
	Priority    string        `json:"priority"`
	Assignee    string        `json:"assignee,omitempty"`
	Description string        `json:"description,omitempty"`
	Labels      []string      `json:"labels,omitempty"`
	CreatedAt   string        `json:"created_at,omitempty"`
	UpdatedAt   string        `json:"updated_at,omitempty"`
	DependsOn   []string      `json:"depends_on,omitempty"`
	Blocks      []string      `json:"blocks,omitempty"`
	Comments    []BeadComment `json:"comments,omitempty"`
	AgentHints  *AgentHints   `json:"_agent_hints,omitempty"`
}

// BeadComment represents a comment on a bead
type BeadComment struct {
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	Body      string `json:"body"`
}

// BeadShowOptions configures the show operation
type BeadShowOptions struct {
	BeadID          string // Bead ID to show
	IncludeComments bool   // Include comments in output
}

// GetBeadShow returns detailed bead information.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBeadShow(opts BeadShowOptions) (*BeadShowOutput, error) {
	if opts.BeadID == "" {
		return &BeadShowOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead ID required"),
				ErrCodeInvalidFlag,
				"Specify --robot-bead-show=BEAD_ID",
			),
		}, nil
	}

	output := &BeadShowOutput{
		RobotResponse: NewRobotResponse(true),
		BeadID:        opts.BeadID,
	}

	// Get bead details
	showOutput, err := bv.RunBd("", "show", opts.BeadID, "--json")
	if err != nil {
		return &BeadShowOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead '%s' not found: %w", opts.BeadID, err),
				ErrCodeInvalidFlag,
				"Use 'bd list' to see available beads",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	// Parse bead info - bd show returns an array with detailed info
	// Dependencies/dependents are arrays of objects with id/title/etc.
	type depInfo struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var beadInfo []struct {
		ID           string    `json:"id"`
		Title        string    `json:"title"`
		Status       string    `json:"status"`
		IssueType    string    `json:"issue_type"`
		Priority     int       `json:"priority"`
		Assignee     string    `json:"assignee"`
		Description  string    `json:"description"`
		Labels       []string  `json:"labels"`
		CreatedAt    string    `json:"created_at"`
		UpdatedAt    string    `json:"updated_at"`
		Dependencies []depInfo `json:"dependencies"`
		Dependents   []depInfo `json:"dependents"`
	}

	if err := json.Unmarshal([]byte(showOutput), &beadInfo); err != nil || len(beadInfo) == 0 {
		return &BeadShowOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to parse bead info"),
				ErrCodeInternalError,
				"Bead data may be corrupted",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	info := beadInfo[0]
	output.Title = info.Title
	output.Status = info.Status
	output.Type = info.IssueType
	output.Priority = fmt.Sprintf("P%d", info.Priority)
	output.Assignee = info.Assignee
	output.Description = info.Description
	output.Labels = info.Labels
	output.CreatedAt = info.CreatedAt
	output.UpdatedAt = info.UpdatedAt

	// Extract dependency IDs from the nested objects
	for _, dep := range info.Dependencies {
		output.DependsOn = append(output.DependsOn, dep.ID)
	}
	for _, dep := range info.Dependents {
		output.Blocks = append(output.Blocks, dep.ID)
	}

	// Generate hints based on status
	var suggestions []RobotAction
	var notes []string

	switch output.Status {
	case "open":
		suggestions = append(suggestions, RobotAction{
			Action:   "claim",
			Target:   opts.BeadID,
			Reason:   "Bead is available to work on",
			Priority: 1,
		})
		notes = append(notes, fmt.Sprintf("Claim with: ntm --robot-bead-claim=%s", opts.BeadID))
	case "in_progress":
		notes = append(notes, fmt.Sprintf("Close when done: bd close %s", opts.BeadID))
		if output.Assignee != "" {
			notes = append(notes, fmt.Sprintf("Currently assigned to: %s", output.Assignee))
		}
	case "blocked":
		if len(output.DependsOn) > 0 {
			notes = append(notes, fmt.Sprintf("Blocked by: %s", strings.Join(output.DependsOn, ", ")))
		}
	}

	if len(output.Blocks) > 0 {
		notes = append(notes, fmt.Sprintf("Completing this unblocks: %s", strings.Join(output.Blocks, ", ")))
	}

	output.AgentHints = &AgentHints{
		Summary:          fmt.Sprintf("%s [%s] %s: %s", output.Priority, output.Status, output.Type, truncateString(output.Title, 40)),
		Notes:            notes,
		SuggestedActions: suggestions,
	}

	return output, nil
}

// PrintBeadShow outputs detailed bead information.
// This is a thin wrapper around GetBeadShow() for CLI output.
func PrintBeadShow(opts BeadShowOptions) error {
	output, err := GetBeadShow(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}

// BeadCloseOutput represents the result of closing a bead
type BeadCloseOutput struct {
	RobotResponse
	BeadID     string      `json:"bead_id"`
	Title      string      `json:"title"`
	PrevStatus string      `json:"prev_status,omitempty"`
	NewStatus  string      `json:"new_status"`
	Closed     bool        `json:"closed"`
	Reason     string      `json:"reason,omitempty"`
	AgentHints *AgentHints `json:"_agent_hints,omitempty"`
}

// BeadCloseOptions configures the close operation
type BeadCloseOptions struct {
	BeadID string // Bead ID to close
	Reason string // Optional closure reason
}

// GetBeadClose closes a bead and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetBeadClose(opts BeadCloseOptions) (*BeadCloseOutput, error) {
	if opts.BeadID == "" {
		return &BeadCloseOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead ID required"),
				ErrCodeInvalidFlag,
				"Specify --robot-bead-close=BEAD_ID",
			),
		}, nil
	}

	output := &BeadCloseOutput{
		RobotResponse: NewRobotResponse(true),
		BeadID:        opts.BeadID,
		Reason:        opts.Reason,
	}

	// Get current bead info first
	showOutput, err := bv.RunBd("", "show", opts.BeadID, "--json")
	if err != nil {
		return &BeadCloseOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("bead '%s' not found: %w", opts.BeadID, err),
				ErrCodeInvalidFlag,
				"Use 'bd list' to see available beads",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	// Parse bead info
	var beadInfo []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(showOutput), &beadInfo); err != nil || len(beadInfo) == 0 {
		return &BeadCloseOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to parse bead info"),
				ErrCodeInternalError,
				"Bead data may be corrupted",
			),
			BeadID: opts.BeadID,
		}, nil
	}

	output.Title = beadInfo[0].Title
	output.PrevStatus = beadInfo[0].Status

	// Check if already closed
	if beadInfo[0].Status == "closed" {
		output.NewStatus = "closed"
		output.Closed = false
		output.AgentHints = &AgentHints{
			Summary:  fmt.Sprintf("Bead %s is already closed", opts.BeadID),
			Warnings: []string{"Bead was already closed"},
		}
		return output, nil
	}

	// Close the bead
	args := []string{"close", opts.BeadID, "--json"}
	if opts.Reason != "" {
		args = append(args, "--reason", opts.Reason)
	}

	_, err = bv.RunBd("", args...)
	if err != nil {
		return &BeadCloseOutput{
			RobotResponse: NewErrorResponse(
				fmt.Errorf("failed to close bead: %w", err),
				ErrCodeInternalError,
				"Check bead status and dependencies",
			),
			BeadID:     opts.BeadID,
			Title:      output.Title,
			PrevStatus: output.PrevStatus,
		}, nil
	}

	output.NewStatus = "closed"
	output.Closed = true
	output.AgentHints = &AgentHints{
		Summary: fmt.Sprintf("Closed bead %s: %s", opts.BeadID, truncateString(output.Title, 40)),
		Notes:   []string{"Remember to run 'bd sync' to push changes"},
	}

	return output, nil
}

// PrintBeadClose closes a bead.
// This is a thin wrapper around GetBeadClose() for CLI output.
func PrintBeadClose(opts BeadCloseOptions) error {
	output, err := GetBeadClose(opts)
	if err != nil {
		return err
	}
	return encodeJSON(output)
}
