package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/bv"
)

// ScoreConfig controls how work assignments are scored.
type ScoreConfig struct {
	PreferCriticalPath  bool    // Weight critical path items higher
	PenalizeFileOverlap bool    // Avoid assigning overlapping files
	UseAgentProfiles    bool    // Match work to agent capabilities
	BudgetAware         bool    // Consider token budgets
	ContextThreshold    float64 // Max context usage before penalizing (percentage 0-100, default 80)
}

// DefaultScoreConfig returns a reasonable default configuration.
func DefaultScoreConfig() ScoreConfig {
	return ScoreConfig{
		PreferCriticalPath:  true,
		PenalizeFileOverlap: true,
		UseAgentProfiles:    true,
		BudgetAware:         true,
		ContextThreshold:    80,
	}
}

// ScoredAssignment pairs an assignment with its computed score breakdown.
type ScoredAssignment struct {
	Assignment     *WorkAssignment
	Recommendation *bv.TriageRecommendation
	Agent          *AgentState
	TotalScore     float64
	ScoreBreakdown AssignmentScoreBreakdown
}

// AssignmentScoreBreakdown shows how the score was computed.
type AssignmentScoreBreakdown struct {
	BaseScore          float64 `json:"base_score"`           // From bv triage score
	AgentTypeBonus     float64 `json:"agent_type_bonus"`     // Bonus for agent-task match
	CriticalPathBonus  float64 `json:"critical_path_bonus"`  // Bonus for critical path items
	FileOverlapPenalty float64 `json:"file_overlap_penalty"` // Penalty for file conflicts
	ContextPenalty     float64 `json:"context_penalty"`      // Penalty for high context usage
}

// WorkAssignment represents a work assignment to an agent.
type WorkAssignment struct {
	BeadID         string    `json:"bead_id"`
	BeadTitle      string    `json:"bead_title"`
	AgentPaneID    string    `json:"agent_pane_id"`
	AgentMailName  string    `json:"agent_mail_name,omitempty"`
	AgentType      string    `json:"agent_type"`
	AssignedAt     time.Time `json:"assigned_at"`
	Priority       int       `json:"priority"`
	Score          float64   `json:"score"`
	FilesToReserve []string  `json:"files_to_reserve,omitempty"`
}

// AssignmentResult contains the result of an assignment attempt.
type AssignmentResult struct {
	Success      bool            `json:"success"`
	Assignment   *WorkAssignment `json:"assignment,omitempty"`
	Error        string          `json:"error,omitempty"`
	Reservations []string        `json:"reservations,omitempty"`
	MessageSent  bool            `json:"message_sent"`
}

// AssignWork assigns work to idle agents based on bv triage.
func (c *SessionCoordinator) AssignWork(ctx context.Context) ([]AssignmentResult, error) {
	if !c.config.AutoAssign {
		return nil, nil
	}

	// Get idle agents
	idleAgents := c.GetIdleAgents()
	if len(idleAgents) == 0 {
		return nil, nil
	}

	// Get triage recommendations
	triage, err := bv.GetTriage(c.projectKey)
	if err != nil {
		return nil, fmt.Errorf("getting triage: %w", err)
	}

	if triage == nil || len(triage.Triage.Recommendations) == 0 {
		return nil, nil
	}

	var results []AssignmentResult

	// Match agents to recommendations
	for _, agent := range idleAgents {
		if len(triage.Triage.Recommendations) == 0 {
			break // No more work to assign
		}

		// Find best match for this agent
		assignment, rec := c.findBestMatch(agent, triage.Triage.Recommendations)
		if assignment == nil {
			continue
		}

		// Attempt the assignment
		result := c.attemptAssignment(ctx, assignment, rec)
		results = append(results, result)

		if result.Success {
			// Remove this recommendation from the list
			triage.Triage.Recommendations = removeRecommendation(triage.Triage.Recommendations, rec.ID)

			// Emit event
			select {
			case c.events <- CoordinatorEvent{
				Type:      EventWorkAssigned,
				Timestamp: time.Now(),
				AgentID:   agent.PaneID,
				Details: map[string]any{
					"bead_id":    assignment.BeadID,
					"bead_title": assignment.BeadTitle,
					"agent_type": agent.AgentType,
					"score":      assignment.Score,
				},
			}:
			default:
			}
		}
	}

	return results, nil
}

// findBestMatch finds the best work recommendation for an agent.
func (c *SessionCoordinator) findBestMatch(agent *AgentState, recommendations []bv.TriageRecommendation) (*WorkAssignment, *bv.TriageRecommendation) {
	for _, rec := range recommendations {
		// Skip if blocked (status indicates blocked state)
		if rec.Status == "blocked" {
			continue
		}

		// Create assignment
		assignment := &WorkAssignment{
			BeadID:      rec.ID,
			BeadTitle:   rec.Title,
			AgentPaneID: agent.PaneID,
			AgentType:   agent.AgentType,
			AssignedAt:  time.Now(),
			Priority:    rec.Priority,
			Score:       rec.Score,
		}

		// Check agent mail name mapping
		if agent.AgentMailName != "" {
			assignment.AgentMailName = agent.AgentMailName
		}

		return assignment, &rec
	}

	return nil, nil
}

// attemptAssignment attempts to assign work to an agent.
func (c *SessionCoordinator) attemptAssignment(ctx context.Context, assignment *WorkAssignment, rec *bv.TriageRecommendation) AssignmentResult {
	result := AssignmentResult{
		Assignment: assignment,
	}

	// Reserve files if we know what files will be touched
	// For now, we don't pre-reserve since we don't know the files yet
	// The agent should reserve files when it starts working

	// Send assignment message if mail client available
	if c.mailClient != nil && assignment.AgentMailName != "" {
		body := c.formatAssignmentMessage(assignment, rec)
		_, err := c.mailClient.SendMessage(ctx, agentmail.SendMessageOptions{
			ProjectKey:  c.projectKey,
			SenderName:  c.agentName,
			To:          []string{assignment.AgentMailName},
			Subject:     fmt.Sprintf("Work Assignment: %s", assignment.BeadTitle),
			BodyMD:      body,
			Importance:  "normal",
			AckRequired: true,
		})

		if err != nil {
			result.Error = fmt.Sprintf("sending message: %v", err)
			return result
		}
		result.MessageSent = true
	}

	result.Success = true
	return result
}

// formatAssignmentMessage formats a work assignment message.
func (c *SessionCoordinator) formatAssignmentMessage(assignment *WorkAssignment, rec *bv.TriageRecommendation) string {
	var sb strings.Builder

	sb.WriteString("# Work Assignment\n\n")
	sb.WriteString(fmt.Sprintf("**Bead:** %s\n", assignment.BeadID))
	sb.WriteString(fmt.Sprintf("**Title:** %s\n", assignment.BeadTitle))
	sb.WriteString(fmt.Sprintf("**Priority:** P%d\n", assignment.Priority))
	sb.WriteString(fmt.Sprintf("**Score:** %.2f\n\n", assignment.Score))

	if len(rec.Reasons) > 0 {
		sb.WriteString("## Why This Task\n\n")
		for _, reason := range rec.Reasons {
			sb.WriteString(fmt.Sprintf("- %s\n", reason))
		}
		sb.WriteString("\n")
	}

	if len(rec.UnblocksIDs) > 0 {
		sb.WriteString("## Impact\n\n")
		sb.WriteString(fmt.Sprintf("Completing this will unblock %d other tasks:\n", len(rec.UnblocksIDs)))
		for _, id := range rec.UnblocksIDs {
			if sb.Len() > 1500 {
				sb.WriteString("- ...\n")
				break
			}
			sb.WriteString(fmt.Sprintf("- %s\n", id))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("1. Review the bead with `bd show " + assignment.BeadID + "`\n")
	sb.WriteString("2. Claim the work with `bd update " + assignment.BeadID + " --status in_progress`\n")
	sb.WriteString("3. Reserve any files you'll modify\n")
	sb.WriteString("4. Implement and test\n")
	sb.WriteString("5. Close with `bd close " + assignment.BeadID + "`\n")
	sb.WriteString("6. Commit with `.beads/` changes\n\n")

	sb.WriteString("Please acknowledge this message when you begin work.\n")

	return sb.String()
}

// removeRecommendation removes a recommendation by ID from the list.
func removeRecommendation(recs []bv.TriageRecommendation, id string) []bv.TriageRecommendation {
	if len(recs) == 0 {
		return nil
	}
	result := make([]bv.TriageRecommendation, 0, len(recs))
	for _, r := range recs {
		if r.ID != id {
			result = append(result, r)
		}
	}
	return result
}

// GetAssignableWork returns work items that could be assigned to idle agents.
func (c *SessionCoordinator) GetAssignableWork(ctx context.Context) ([]bv.TriageRecommendation, error) {
	triage, err := bv.GetTriage(c.projectKey)
	if err != nil {
		return nil, err
	}

	if triage == nil {
		return nil, nil
	}

	// Filter to unblocked items
	var assignable []bv.TriageRecommendation
	for _, rec := range triage.Triage.Recommendations {
		if rec.Status != "blocked" {
			assignable = append(assignable, rec)
		}
	}

	return assignable, nil
}

// SuggestAssignment suggests the best work for a specific agent without assigning.
func (c *SessionCoordinator) SuggestAssignment(ctx context.Context, paneID string) (*WorkAssignment, error) {
	agent := c.GetAgentByPaneID(paneID)
	if agent == nil {
		return nil, fmt.Errorf("agent not found: %s", paneID)
	}

	triage, err := bv.GetTriage(c.projectKey)
	if err != nil {
		return nil, err
	}

	if triage == nil || len(triage.Triage.Recommendations) == 0 {
		return nil, nil
	}

	assignment, _ := c.findBestMatch(agent, triage.Triage.Recommendations)
	return assignment, nil
}

// ScoreAndSelectAssignments computes optimal agent-task pairings using multi-factor scoring.
// It returns a list of scored assignments sorted by total score (highest first).
func ScoreAndSelectAssignments(
	idleAgents []*AgentState,
	triage *bv.TriageResponse,
	config ScoreConfig,
	existingReservations map[string][]string, // agent -> reserved file patterns
) []ScoredAssignment {
	if len(idleAgents) == 0 || triage == nil || len(triage.Triage.Recommendations) == 0 {
		return nil
	}

	var candidates []ScoredAssignment

	// Score all possible agent-task combinations
	for _, agent := range idleAgents {
		for i := range triage.Triage.Recommendations {
			rec := &triage.Triage.Recommendations[i]

			// Skip blocked items
			if rec.Status == "blocked" {
				continue
			}

			scored := scoreAssignment(agent, rec, config, existingReservations)
			if scored.TotalScore > 0 {
				candidates = append(candidates, scored)
			}
		}
	}

	// Sort by total score (highest first)
	sortScoredAssignments(candidates)

	// Select non-conflicting assignments (each agent gets at most one task)
	var selected []ScoredAssignment
	assignedAgents := make(map[string]bool)
	assignedTasks := make(map[string]bool)

	for _, candidate := range candidates {
		agentID := candidate.Agent.PaneID
		taskID := candidate.Recommendation.ID

		if assignedAgents[agentID] || assignedTasks[taskID] {
			continue
		}

		selected = append(selected, candidate)
		assignedAgents[agentID] = true
		assignedTasks[taskID] = true
	}

	return selected
}

// sortScoredAssignments sorts assignments by total score (highest first).
func sortScoredAssignments(candidates []ScoredAssignment) {
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].TotalScore > candidates[i].TotalScore {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
}

// scoreAssignment computes the score for a single agent-task pairing.
func scoreAssignment(
	agent *AgentState,
	rec *bv.TriageRecommendation,
	config ScoreConfig,
	existingReservations map[string][]string,
) ScoredAssignment {
	breakdown := AssignmentScoreBreakdown{
		BaseScore: rec.Score,
	}

	// Agent type matching
	if config.UseAgentProfiles {
		breakdown.AgentTypeBonus = computeAgentTypeBonus(agent.AgentType, rec)
	}

	// Critical path bonus
	if config.PreferCriticalPath && rec.Breakdown != nil {
		breakdown.CriticalPathBonus = computeCriticalPathBonus(rec.Breakdown)
	}

	// File overlap penalty
	if config.PenalizeFileOverlap && existingReservations != nil {
		breakdown.FileOverlapPenalty = computeFileOverlapPenalty(agent, existingReservations)
	}

	// Context/budget penalty
	// Note: ContextUsage is in percentage scale (0-100), not ratio (0-1)
	if config.BudgetAware {
		threshold := config.ContextThreshold
		if threshold == 0 {
			threshold = 80 // 80% threshold (percentage scale)
		}
		breakdown.ContextPenalty = computeContextPenalty(agent.ContextUsage, threshold)
	}

	totalScore := breakdown.BaseScore +
		breakdown.AgentTypeBonus +
		breakdown.CriticalPathBonus -
		breakdown.FileOverlapPenalty -
		breakdown.ContextPenalty

	return ScoredAssignment{
		Assignment: &WorkAssignment{
			BeadID:        rec.ID,
			BeadTitle:     rec.Title,
			AgentPaneID:   agent.PaneID,
			AgentMailName: agent.AgentMailName,
			AgentType:     agent.AgentType,
			AssignedAt:    time.Now(),
			Priority:      rec.Priority,
			Score:         totalScore,
		},
		Recommendation: rec,
		Agent:          agent,
		TotalScore:     totalScore,
		ScoreBreakdown: breakdown,
	}
}

// computeAgentTypeBonus returns a bonus based on agent-task compatibility.
// Claude (cc) is better for complex tasks (epics, features), Codex (cod) for quick fixes.
func computeAgentTypeBonus(agentType string, rec *bv.TriageRecommendation) float64 {
	taskComplexity := estimateTaskComplexity(rec)

	switch agentType {
	case "cc", "claude":
		// Claude excels at complex, multi-step work
		if taskComplexity >= 0.7 {
			return 0.15 // 15% bonus for complex tasks
		} else if taskComplexity <= 0.3 {
			return -0.05 // Small penalty for simple tasks (overkill)
		}
	case "cod", "codex":
		// Codex is great for quick, focused fixes
		if taskComplexity <= 0.3 {
			return 0.15 // 15% bonus for simple tasks
		} else if taskComplexity >= 0.7 {
			return -0.1 // Penalty for complex tasks
		}
	case "gmi", "gemini":
		// Gemini is balanced
		if taskComplexity >= 0.4 && taskComplexity <= 0.6 {
			return 0.05 // Small bonus for medium complexity
		}
	}

	return 0
}

// estimateTaskComplexity returns a 0-1 score based on task characteristics.
func estimateTaskComplexity(rec *bv.TriageRecommendation) float64 {
	complexity := 0.5 // Start with medium

	// Task type affects complexity
	switch rec.Type {
	case "epic":
		complexity += 0.3
	case "feature":
		complexity += 0.2
	case "bug":
		complexity += 0.0 // Varies
	case "task":
		complexity -= 0.1
	case "chore":
		complexity -= 0.2
	}

	// Priority affects perceived complexity (urgent items often simpler)
	if rec.Priority == 0 {
		complexity -= 0.1 // Critical items often need quick fixes
	} else if rec.Priority >= 3 {
		complexity += 0.1 // Backlog items often bigger
	}

	// Number of items unblocked indicates scope
	if len(rec.UnblocksIDs) >= 5 {
		complexity += 0.15
	} else if len(rec.UnblocksIDs) >= 3 {
		complexity += 0.1
	}

	// Clamp to 0-1
	if complexity < 0 {
		complexity = 0
	} else if complexity > 1 {
		complexity = 1
	}

	return complexity
}

// computeCriticalPathBonus gives bonus for items with high graph centrality.
func computeCriticalPathBonus(breakdown *bv.ScoreBreakdown) float64 {
	bonus := 0.0

	// High PageRank means central to the project
	if breakdown.Pagerank > 0.05 {
		bonus += breakdown.Pagerank * 2 // Up to ~0.15 bonus
	}

	// High blocker ratio means it unblocks many things
	if breakdown.BlockerRatio > 0.05 {
		bonus += breakdown.BlockerRatio * 1.5
	}

	// Time-to-impact indicates depth in critical path
	if breakdown.TimeToImpact > 0.04 {
		bonus += 0.05
	}

	return bonus
}

// computeFileOverlapPenalty penalizes agents who already have many file reservations.
func computeFileOverlapPenalty(agent *AgentState, reservations map[string][]string) float64 {
	agentReservations := reservations[agent.PaneID]
	if len(agentReservations) == 0 {
		agentReservations = agent.Reservations
	}

	// Penalty increases with number of reservations
	// This encourages spreading work across agents
	count := len(agentReservations)
	if count == 0 {
		return 0
	} else if count <= 2 {
		return 0.05
	} else if count <= 5 {
		return 0.1
	}
	return 0.2
}

// computeContextPenalty penalizes agents with high context window usage.
// Both contextUsage and threshold are in percentage scale (0-100).
func computeContextPenalty(contextUsage float64, threshold float64) float64 {
	if contextUsage <= threshold {
		return 0
	}

	// Linear penalty above threshold, normalized to score scale (0-1)
	// e.g., 10% over threshold → 0.05 penalty; 20% over → 0.10 penalty
	excess := contextUsage - threshold
	return (excess / 100) * 0.5
}
