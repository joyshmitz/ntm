package pipeline

import (
	"context"
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

// executeBranch resolves the branch predicate, looks up the matching branch,
// and returns the result. Branch dispatch (executing the matched steps) is
// handled by bd-w6nth.2; this bead resolves the key and validates lookup.
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

	_, lookupErr := lookupBranch(step.Branches, key)
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

	result.Status = StatusCompleted
	result.Output = key
	result.FinishedAt = time.Now()
	return result
}
