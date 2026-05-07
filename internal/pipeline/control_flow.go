package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// resolveBranch evaluates the branch predicate and returns the matching key.
// Two modes:
//   - Literal: "fresh-pass" → returns "fresh-pass"
//   - Shell expression: "$(cmd)" → executes cmd, returns trimmed stdout
//
// Variable substitution is applied before evaluation.
func (e *Executor) resolveBranch(ctx context.Context, step *Step) (string, error) {
	expr := e.substituteVariables(step.Branch)

	if strings.HasPrefix(expr, "$(") && strings.HasSuffix(expr, ")") {
		shellCmd := expr[2 : len(expr)-1]
		slog.Info("branch shell predicate executing",
			"run_id", e.state.RunID,
			"workflow", e.state.WorkflowID,
			"step_id", step.ID,
			"agent_type", "branch",
			"command", shellCmd,
		)

		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", shellCmd)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		if e.config.ProjectDir != "" {
			cmd.Dir = e.config.ProjectDir
		}

		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("branch shell command failed: %w", err)
		}

		return strings.TrimSpace(string(out)), nil
	}

	return expr, nil
}

// lookupBranch looks up the key in step.Branches, falling back to "default".
func lookupBranch(branches map[string]interface{}, key string) (interface{}, error) {
	if val, ok := branches[key]; ok {
		return val, nil
	}
	if val, ok := branches["default"]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("branch produced no matching key: %s", key)
}

// parseBranchSteps converts an interface{} branch value into a slice of Steps.
// Handles both single-step maps and lists of step maps via JSON round-trip.
func parseBranchSteps(val interface{}, parentID, branchKey string) ([]Step, error) {
	data, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal branch value: %w", err)
	}

	// Try list first
	var steps []Step
	if err := json.Unmarshal(data, &steps); err == nil && len(steps) > 0 {
		for i := range steps {
			if steps[i].ID == "" {
				steps[i].ID = fmt.Sprintf("%s.%s.%d", parentID, branchKey, i)
			}
		}
		return steps, nil
	}

	// Try single step
	var single Step
	if err := json.Unmarshal(data, &single); err == nil {
		if single.ID == "" {
			single.ID = fmt.Sprintf("%s.%s.0", parentID, branchKey)
		}
		return []Step{single}, nil
	}

	return nil, fmt.Errorf("branch value for key %q is neither a step nor a list of steps", branchKey)
}

// executeBranch resolves the branch predicate, looks up the matching branch,
// parses the branch body into steps, and executes them sequentially.
func (e *Executor) executeBranch(ctx context.Context, step *Step, workflow *Workflow) StepResult {
	result := StepResult{
		StepID:    step.ID,
		Status:    StatusRunning,
		StartedAt: time.Now(),
	}

	slog.Info("branch step starting",
		"run_id", e.state.RunID,
		"workflow", e.state.WorkflowID,
		"step_id", step.ID,
		"agent_type", "branch",
	)

	if e.config.DryRun {
		result.Status = StatusCompleted
		result.Output = fmt.Sprintf("[DRY RUN] Would evaluate branch predicate: %s", truncatePrompt(step.Branch, 80))
		result.FinishedAt = time.Now()
		return result
	}

	key, err := e.resolveBranch(ctx, step)
	if err != nil {
		result.Status = StatusFailed
		result.Error = &StepError{
			Type:      "branch",
			Message:   fmt.Sprintf("branch predicate failed: %v", err),
			Timestamp: time.Now(),
		}
		result.FinishedAt = time.Now()
		slog.Error("branch predicate failed",
			"run_id", e.state.RunID,
			"step_id", step.ID,
			"error", err,
		)
		return result
	}

	branchVal, lookupErr := lookupBranch(step.Branches, key)
	if lookupErr != nil {
		result.Status = StatusFailed
		result.Error = &StepError{
			Type:      "branch",
			Message:   lookupErr.Error(),
			Timestamp: time.Now(),
		}
		result.FinishedAt = time.Now()
		slog.Error("branch lookup failed",
			"run_id", e.state.RunID,
			"step_id", step.ID,
			"branch_key", key,
			"error", lookupErr,
		)
		return result
	}

	slog.Info("branch step resolved",
		"run_id", e.state.RunID,
		"step_id", step.ID,
		"branch_key", key,
	)

	branchSteps, parseErr := parseBranchSteps(branchVal, step.ID, key)
	if parseErr != nil {
		result.Status = StatusFailed
		result.Error = &StepError{
			Type:      "branch",
			Message:   fmt.Sprintf("failed to parse branch body: %v", parseErr),
			Timestamp: time.Now(),
		}
		result.FinishedAt = time.Now()
		return result
	}

	// Snapshot variables before branch body for scoping.
	e.varMu.RLock()
	varSnapshot := captureAllVariables(e.state.Variables)
	e.varMu.RUnlock()
	defer func() {
		e.varMu.Lock()
		restoreAllVariables(e.state, varSnapshot)
		e.varMu.Unlock()
	}()

	// Execute branch steps sequentially
	var outputs []string
	allPassed := true
	for i, bs := range branchSteps {
		select {
		case <-ctx.Done():
			result.Status = StatusCancelled
			result.FinishedAt = time.Now()
			return result
		default:
		}

		slog.Info("branch body step executing",
			"run_id", e.state.RunID,
			"step_id", step.ID,
			"branch_key", key,
			"body_step", bs.ID,
			"iteration", i,
		)

		sr := e.executeStepOnce(ctx, &bs, workflow)
		outputs = append(outputs, sr.Output)

		e.stateMu.Lock()
		e.state.Steps[bs.ID] = sr
		e.stateMu.Unlock()

		if sr.Status == StatusFailed || sr.Status == StatusCancelled {
			allPassed = false
			result.Status = sr.Status
			result.Error = sr.Error
			break
		}
	}

	if allPassed {
		result.Status = StatusCompleted
	}
	result.Output = strings.Join(outputs, "\n")
	result.FinishedAt = time.Now()
	return result
}
