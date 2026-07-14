package robot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/assign"
	assignmentstore "github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/bv"
	statuspkg "github.com/Dicklesworthstone/ntm/internal/status"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// AssignOptions configures work assignment analysis
type AssignOptions struct {
	Session    string   // tmux session name
	ProjectDir string   // Explicit project directory for Beads reads
	Beads      []string // Specific bead IDs to assign (empty = all ready)
	Strategy   string   // balanced, speed, quality, dependency
}

// AssignOutput is the structured output for --robot-assign
type AssignOutput struct {
	RobotResponse
	Session           string             `json:"session"`
	Strategy          string             `json:"strategy"`
	GeneratedAt       time.Time          `json:"generated_at"`
	Recommendations   []AssignRecommend  `json:"recommendations"`
	BlockedBeads      []BlockedBead      `json:"blocked_beads"`
	IdleAgents        []string           `json:"idle_agents"`
	UnassignableBeads []UnassignableBead `json:"unassignable_beads,omitempty"`
	Summary           AssignSummary      `json:"summary"`
	AgentHints        *AssignAgentHints  `json:"_agent_hints,omitempty"`
}

// AssignRecommend is a single assignment recommendation
type AssignRecommend struct {
	PaneID     string  `json:"pane_id"`     // Stable tmux pane identity (e.g., "%12")
	PaneTarget string  `json:"pane_target"` // Explicit window.pane topology address
	AgentType  string  `json:"agent_type"`  // claude, codex, gemini
	Model      string  `json:"model,omitempty"`
	AssignBead string  `json:"assign_bead"` // Bead ID to assign
	BeadTitle  string  `json:"bead_title"`
	Priority   string  `json:"priority"`   // P0-P4
	Confidence float64 `json:"confidence"` // 0.0-1.0
	Reasoning  string  `json:"reasoning"`
}

// BlockedBead represents a bead that can't be assigned due to dependencies
type BlockedBead struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	BlockedBy []string `json:"blocked_by"`
}

// UnassignableBead represents a bead that can't be assigned for other reasons
type UnassignableBead struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

// AssignSummary provides assignment statistics
type AssignSummary struct {
	TotalAgents       int `json:"total_agents"`
	IdleAgents        int `json:"idle_agents"`
	WorkingAgents     int `json:"working_agents"`
	ReadyBeads        int `json:"ready_beads"`
	BlockedBeads      int `json:"blocked_beads"`
	Recommendations   int `json:"recommendations"`
	UnassignableBeads int `json:"unassignable_beads"`
}

// AssignAgentHints provides actionable suggestions for AI agents
type AssignAgentHints struct {
	Summary           string   `json:"summary,omitempty"`
	SuggestedCommands []string `json:"suggested_commands,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
}

// AgentStrength returns the task type affinity score for an agent/task combination.
// This delegates to the assign package's capability matrix which supports
// configuration overrides and learned score adjustments.
func AgentStrength(agentType, taskType string) float64 {
	return assign.GetAgentScoreByString(agentType, taskType)
}

// DistributeRecommendation is a simplified recommendation for distribute mode
type DistributeRecommendation struct {
	BeadID     string `json:"bead_id"`
	Title      string `json:"title"`
	PaneID     string `json:"pane_id"`
	PaneTarget string `json:"pane_target"`
	AgentType  string `json:"agent_type"`
	Reason     string `json:"reason"`
}

// GetAssignRecommendations returns assignment recommendations for the distribute mode.
// This is a simplified version of PrintAssign that returns data instead of printing JSON.
func GetAssignRecommendations(ctx context.Context, opts AssignOptions) ([]DistributeRecommendation, error) {
	if ctx == nil {
		return nil, fmt.Errorf("assignment recommendation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Session == "" {
		return nil, fmt.Errorf("session name is required")
	}

	exists, err := tmux.SessionExistsContext(ctx, opts.Session)
	if err != nil {
		return nil, fmt.Errorf("check assignment session %s: %w", opts.Session, err)
	}
	if !exists {
		return nil, fmt.Errorf("session '%s' not found", opts.Session)
	}

	// Normalize strategy
	strategy := strings.ToLower(opts.Strategy)
	if strategy == "" {
		strategy = "balanced"
	}

	agents, idleAgentPanes, err := observeAssignAgents(ctx, opts.Session)
	if err != nil {
		return nil, err
	}
	idleAgentPanes, err = excludeDurablyOccupiedAssignAgents(opts.Session, idleAgentPanes)
	if err != nil {
		return nil, fmt.Errorf("exclude active assignment occupancy: %w", err)
	}

	if len(idleAgentPanes) == 0 {
		return nil, nil // No idle agents
	}

	projectDir, err := assignOptionsProjectDir(opts)
	if err != nil {
		return nil, err
	}
	readyBeads, err := bv.GetReadyPreviewContext(ctx, projectDir, 50)
	if err != nil {
		return nil, fmt.Errorf("read ready Beads work: %w", err)
	}

	if len(readyBeads) == 0 {
		return nil, nil // No ready work
	}

	// Filter to specific beads if requested
	if len(opts.Beads) > 0 {
		beadSet := make(map[string]bool)
		for _, b := range opts.Beads {
			beadSet[b] = true
		}
		var filtered []bv.BeadPreview
		for _, b := range readyBeads {
			if beadSet[b.ID] {
				filtered = append(filtered, b)
			}
		}
		readyBeads = filtered
	}

	// Generate recommendations
	recs := generateAssignments(agents, readyBeads, strategy, idleAgentPanes)

	// Convert to DistributeRecommendation format
	var result []DistributeRecommendation
	for _, rec := range recs {
		result = append(result, DistributeRecommendation{
			BeadID:     rec.AssignBead,
			Title:      rec.BeadTitle,
			PaneID:     rec.PaneID,
			PaneTarget: rec.PaneTarget,
			AgentType:  rec.AgentType,
			Reason:     rec.Reasoning,
		})
	}

	return result, nil
}

// GetAssign generates work assignment recommendations and returns the result.
// This function returns the data struct directly, enabling CLI/REST parity.
func GetAssign(ctx context.Context, opts AssignOptions) (*AssignOutput, error) {
	output := &AssignOutput{
		RobotResponse:   NewRobotResponse(true),
		Session:         opts.Session,
		Recommendations: make([]AssignRecommend, 0),
		BlockedBeads:    make([]BlockedBead, 0),
		IdleAgents:      []string{},
	}
	if ctx == nil {
		return nil, fmt.Errorf("robot assignment context is required")
	}
	if err := ctx.Err(); err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeTimeout, "Retry the command after cancellation")
		return output, nil
	}

	if opts.Session == "" {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session name is required"),
			ErrCodeInvalidFlag,
			"Provide session name: ntm --robot-assign=myproject",
		)
		return output, nil
	}

	exists, err := tmux.SessionExistsContext(ctx, opts.Session)
	if err != nil {
		setAssignError(output, err, "Check tmux availability")
		return output, nil
	}
	if !exists {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("session '%s' not found", opts.Session),
			ErrCodeSessionNotFound,
			"Use 'ntm list' to see available sessions",
		)
		return output, nil
	}

	// Normalize strategy
	strategy := strings.ToLower(opts.Strategy)
	if strategy == "" {
		strategy = "balanced"
	}
	validStrategies := map[string]bool{"balanced": true, "speed": true, "quality": true, "dependency": true}
	if !validStrategies[strategy] {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("invalid strategy '%s'", opts.Strategy),
			ErrCodeInvalidFlag,
			"Valid strategies: balanced, speed, quality, dependency",
		)
		return output, nil
	}

	output.Strategy = strategy
	output.GeneratedAt = time.Now().UTC()

	agents, idleAgentPanes, err := observeAssignAgents(ctx, opts.Session)
	if err != nil {
		setAssignError(output, fmt.Errorf("failed to observe assignment candidates: %w", err), "Retry after pane state can be observed freshly and confidently")
		return output, nil
	}
	idleAgentPanes, err = excludeDurablyOccupiedAssignAgents(opts.Session, idleAgentPanes)
	if err != nil {
		output.RobotResponse = NewErrorResponse(
			fmt.Errorf("failed to exclude active assignment occupancy: %w", err),
			ErrCodeInternalError,
			"Repair or migrate the durable assignment ledger before distributing more work",
		)
		return output, nil
	}

	output.IdleAgents = idleAgentPanes

	projectDir, err := assignOptionsProjectDir(opts)
	if err != nil {
		output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, "Provide a readable project directory for Beads")
		return output, nil
	}
	readyBeads, err := bv.GetReadyPreviewContext(ctx, projectDir, 50)
	if err != nil {
		setAssignError(output, fmt.Errorf("read ready Beads work: %w", err), "Ensure br can read the target project's Beads database")
		return output, nil
	}
	inProgress, err := bv.GetInProgressListContext(ctx, projectDir, 50)
	if err != nil {
		setAssignError(output, fmt.Errorf("read in-progress Beads work: %w", err), "Ensure br can read the target project's Beads database")
		return output, nil
	}

	// Filter to specific beads if requested
	if len(opts.Beads) > 0 {
		beadSet := make(map[string]bool)
		for _, b := range opts.Beads {
			beadSet[b] = true
		}
		var filtered []bv.BeadPreview
		for _, b := range readyBeads {
			if beadSet[b.ID] {
				filtered = append(filtered, b)
			}
		}
		readyBeads = filtered
	}

	// Build working agents set from in-progress beads
	workingAgents := len(agents) - len(idleAgentPanes)

	// Generate recommendations based on strategy
	recommendations := generateAssignments(agents, readyBeads, strategy, idleAgentPanes)
	output.Recommendations = recommendations

	// Add blocked beads (beads with unmet dependencies)
	blockedBeads, err := bv.GetBlockedListContext(ctx, projectDir, 20)
	if err != nil {
		setAssignError(output, fmt.Errorf("read blocked Beads work: %w", err), "Ensure br can read the target project's Beads database")
		return output, nil
	}
	for _, b := range blockedBeads {
		output.BlockedBeads = append(output.BlockedBeads, BlockedBead{
			ID:        b.ID,
			Title:     b.Title,
			BlockedBy: []string{},
		})
	}

	// Build summary
	output.Summary = AssignSummary{
		TotalAgents:     len(agents),
		IdleAgents:      len(idleAgentPanes),
		WorkingAgents:   workingAgents,
		ReadyBeads:      len(readyBeads),
		BlockedBeads:    len(output.BlockedBeads),
		Recommendations: len(recommendations),
	}

	// Generate agent hints
	output.AgentHints = generateAssignHints(recommendations, idleAgentPanes, readyBeads, inProgress)

	return output, nil
}

func setAssignError(output *AssignOutput, err error, hint string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		output.RobotResponse = NewErrorResponse(err, ErrCodeTimeout, "Retry the command after cancellation")
		return
	}
	output.RobotResponse = NewErrorResponse(err, ErrCodeInternalError, hint)
}

// PrintAssign handles the --robot-assign command.
// This is a thin wrapper around GetAssign() for CLI output.
func PrintAssign(ctx context.Context, opts AssignOptions) error {
	output, err := GetAssign(ctx, opts)
	if err != nil {
		return err
	}
	return encodeTerminalRobotOutput(output, output.RobotResponse, "robot assign failed")
}

// assignAgentInfo holds agent data for assignment processing
type assignAgentInfo struct {
	paneID     string
	paneTarget string
	agentType  string
	model      string
	state      string
}

func observeAssignAgents(ctx context.Context, session string) ([]assignAgentInfo, []string, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("assignment observation context is required")
	}
	observation, err := newRobotSessionObserver(20).Observe(ctx, session)
	if err != nil {
		return nil, nil, fmt.Errorf("observe assignment session %s: %w", session, err)
	}
	return assignAgentsFromObservation(observation, time.Now())
}

func assignAgentsFromObservation(observation statuspkg.SessionObservation, now time.Time) ([]assignAgentInfo, []string, error) {
	if !statuspkg.DispatchObservationIsCurrent(observation.ObservedAt, now) {
		return nil, nil, fmt.Errorf("assignment observation for session %s is stale", observation.Session)
	}
	agents := make([]assignAgentInfo, 0, len(observation.Panes))
	idleAgentPanes := make([]string, 0, len(observation.Panes))
	for _, pane := range observation.Panes {
		agentType := strings.ToLower(strings.TrimSpace(pane.AgentType))
		if agentType == "user" || agentType == "unknown" || agentType == "" {
			continue
		}
		paneID := strings.TrimSpace(pane.Pane.ID)
		if paneID == "" {
			return nil, nil, fmt.Errorf("assignment observation for session %s has an agent without a stable pane ID", observation.Session)
		}
		agents = append(agents, assignAgentInfo{
			paneID:     paneID,
			paneTarget: pane.Pane.Physical(),
			agentType:  agentType,
			model:      detectModel(agentType, pane.PaneName),
			state:      string(pane.Current.Status.State),
		})
		if statuspkg.DispatchObservationIsCurrent(pane.Current.ObservedAt, now) && pane.SafeToDispatch() {
			idleAgentPanes = append(idleAgentPanes, paneID)
		}
	}
	return agents, idleAgentPanes, nil
}

func assignOptionsProjectDir(opts AssignOptions) (string, error) {
	if projectDir := strings.TrimSpace(opts.ProjectDir); projectDir != "" {
		return projectDir, nil
	}
	projectDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve assignment project directory: %w", err)
	}
	return projectDir, nil
}

func excludeDurablyOccupiedAssignAgents(session string, idleAgentPanes []string) ([]string, error) {
	store, err := assignmentstore.LoadStoreStrict(session)
	if err != nil {
		return nil, err
	}
	return filterDurablyOccupiedAssignAgents(idleAgentPanes, store.ListActive())
}

func filterDurablyOccupiedAssignAgents(idleAgentPanes []string, activeAssignments []*assignmentstore.Assignment) ([]string, error) {
	occupied := make(map[string]struct{}, len(activeAssignments))
	for _, current := range activeAssignments {
		if current == nil {
			continue
		}
		paneID, err := assignmentstore.CanonicalPaneIdentity(current)
		if err != nil {
			return nil, fmt.Errorf("active assignment %s: %w", current.BeadID, err)
		}
		occupied[paneID] = struct{}{}
	}
	available := make([]string, 0, len(idleAgentPanes))
	for _, paneID := range idleAgentPanes {
		if _, active := occupied[strings.TrimSpace(paneID)]; !active {
			available = append(available, paneID)
		}
	}
	return available, nil
}

// generateAssignments creates assignment recommendations based on strategy
func generateAssignments(agents []assignAgentInfo, beads []bv.BeadPreview, strategy string, idleAgents []string) []AssignRecommend {
	var recommendations []AssignRecommend

	// Create a map of idle agents for quick lookup
	idleSet := make(map[string]bool)
	for _, a := range idleAgents {
		idleSet[a] = true
	}

	// Get idle agent details
	var idleAgentDetails []assignAgentInfo
	for _, a := range agents {
		if idleSet[a.paneID] {
			idleAgentDetails = append(idleAgentDetails, a)
		}
	}

	// Assign beads to idle agents based on strategy
	beadIdx := 0
	for _, agent := range idleAgentDetails {
		if beadIdx >= len(beads) {
			break // No more beads to assign
		}

		bead := beads[beadIdx]
		// Calculate confidence based on strategy
		confidence := calculateConfidence(agent.agentType, bead, strategy)
		reasoning := generateReasoning(agent.agentType, bead, strategy)

		recommendations = append(recommendations, AssignRecommend{
			PaneID:     agent.paneID,
			PaneTarget: agent.paneTarget,
			AgentType:  agent.agentType,
			Model:      agent.model,
			AssignBead: bead.ID,
			BeadTitle:  bead.Title,
			Priority:   bead.Priority,
			Confidence: confidence,
			Reasoning:  reasoning,
		})

		beadIdx++
	}

	return recommendations
}

// calculateConfidence determines assignment confidence based on agent-task match
func calculateConfidence(agentType string, bead bv.BeadPreview, strategy string) float64 {
	// Extract task type from bead title/priority
	taskType := inferTaskType(bead)

	// Get capability score from the assign package
	baseConfidence := AgentStrength(agentType, taskType)

	// Adjust based on strategy
	switch strategy {
	case "quality":
		// Quality strategy favors better agent-task matches
		// Using capability matrix scores
	case "speed":
		// Speed strategy slightly favors any available agent
		baseConfidence = (baseConfidence + 0.9) / 2
	case "dependency":
		// Dependency strategy favors high-priority items
		priority := parsePriority(bead.Priority)
		if priority <= 1 { // P0 or P1
			baseConfidence = min(baseConfidence+0.1, 0.95)
		}
	}

	return baseConfidence
}

// inferTaskType attempts to determine task type from bead metadata
func inferTaskType(bead bv.BeadPreview) string {
	title := strings.ToLower(bead.Title)

	// Check for common keywords in priority order
	// Order matters! Check specific types before generic ones.
	type rule struct {
		typ string
		kws []string
	}

	rules := []rule{
		{"bug", []string{"bug", "fix", "broken", "error", "crash"}},
		{"testing", []string{"test", "spec", "coverage"}},
		{"documentation", []string{"doc", "readme", "comment", "documentation"}},
		{"refactor", []string{"refactor", "cleanup", "improve", "consolidate"}},
		{"analysis", []string{"analyze", "investigate", "research", "design"}},
		{"feature", []string{"feature", "implement", "add", "new"}},
	}

	for _, r := range rules {
		for _, kw := range r.kws {
			if strings.Contains(title, kw) {
				return r.typ
			}
		}
	}

	return "task" // Default
}

// parsePriority converts "P0"-"P4" to integer
func parsePriority(p string) int {
	if len(p) == 2 && p[0] == 'P' {
		if n := p[1] - '0'; n <= 4 { // n is byte (unsigned), so >= 0 is always true
			return int(n)
		}
	}
	return 2 // Default to P2
}

// generateReasoning creates a human-readable explanation for the assignment
func generateReasoning(agentType string, bead bv.BeadPreview, strategy string) string {
	taskType := inferTaskType(bead)
	priority := parsePriority(bead.Priority)

	var reasons []string

	// Add task-agent match reasoning
	strength := AgentStrength(agentType, taskType)
	if strength >= 0.8 {
		reasons = append(reasons, fmt.Sprintf("%s excels at %s tasks", agentType, taskType))
	}

	// Add priority reasoning
	switch priority {
	case 0:
		reasons = append(reasons, "critical priority")
	case 1:
		reasons = append(reasons, "high priority")
	}

	// Add strategy-specific reasoning
	switch strategy {
	case "balanced":
		reasons = append(reasons, "balanced workload distribution")
	case "speed":
		reasons = append(reasons, "optimizing for speed")
	case "quality":
		reasons = append(reasons, "optimizing for quality")
	case "dependency":
		reasons = append(reasons, "prioritizing dependency unblocking")
	}

	if len(reasons) == 0 {
		return "available agent matched to available work"
	}

	return strings.Join(reasons, "; ")
}

// generateAssignHints creates actionable hints for AI agents
func generateAssignHints(recs []AssignRecommend, idleAgents []string, readyBeads []bv.BeadPreview, inProgress []bv.BeadInProgress) *AssignAgentHints {
	hints := &AssignAgentHints{}

	// Build summary
	if len(recs) == 0 && len(readyBeads) == 0 {
		hints.Summary = "No work available to assign"
	} else if len(recs) == 0 && len(idleAgents) == 0 {
		hints.Summary = fmt.Sprintf("%d beads ready but no idle agents available", len(readyBeads))
	} else if len(recs) > 0 {
		hints.Summary = fmt.Sprintf("%d assignments recommended for %d idle agents", len(recs), len(idleAgents))
	}

	// Generate suggested commands
	for _, rec := range recs {
		cmd := fmt.Sprintf("br update %s --assignee=%s", rec.AssignBead, rec.PaneID)
		hints.SuggestedCommands = append(hints.SuggestedCommands, cmd)
	}

	// Add warnings
	if len(readyBeads) > len(idleAgents) {
		diff := len(readyBeads) - len(idleAgents)
		hints.Warnings = append(hints.Warnings,
			fmt.Sprintf("%d beads won't be assigned - not enough idle agents", diff))
	}

	if len(inProgress) > 0 {
		staleCount := 0
		for _, b := range inProgress {
			if time.Since(b.UpdatedAt) > 24*time.Hour {
				staleCount++
			}
		}
		if staleCount > 0 {
			hints.Warnings = append(hints.Warnings,
				fmt.Sprintf("%d in-progress beads are stale (>24h since update)", staleCount))
		}
	}

	return hints
}
