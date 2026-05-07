package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestForeachContinueAggregatesAllIterationFailures(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-error-aggregation",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID:      "fanout",
		OnError: ErrorActionContinue,
		Foreach: &ForeachConfig{
			Items: `["ok-0","bad-1","bad-2","ok-3","bad-4"]`,
			Steps: []Step{{
				ID:      "maybe",
				Command: `case '${item}' in bad-*) echo '${item} failed'; exit 7;; *) printf '%s' '${item}';; esac`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeForeach(context.Background(), step, workflow)

	if result.Status != StatusCompleted {
		t.Fatalf("foreach status = %s, want completed; error=%+v", result.Status, result.Error)
	}
	if result.Error == nil {
		t.Fatal("foreach error = nil, want aggregate error metadata")
	}
	if got := len(result.Error.Aggregated); got != 3 {
		t.Fatalf("aggregated errors = %d, want 3: %+v", got, result.Error.Aggregated)
	}
	if !strings.Contains(result.Error.Message, "3 of 5 iterations failed") {
		t.Fatalf("message = %q, want aggregate count", result.Error.Message)
	}
	if !strings.Contains(result.Error.Details, `"iteration":1`) || !strings.Contains(result.Error.Details, `"iteration":4`) {
		t.Fatalf("details = %q, want iteration summaries", result.Error.Details)
	}
}

func TestForeachFailFastAggregatesFirstFailureAndCancelsRemaining(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-fail-fast-aggregation",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID:      "fanout",
		OnError: ErrorActionFailFast,
		Foreach: &ForeachConfig{
			Items: `["bad-0","bad-1","bad-2","bad-3","bad-4"]`,
			Steps: []Step{{
				ID:      "maybe",
				Command: `echo '${item} failed'; exit 7`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeForeach(context.Background(), step, workflow)

	if result.Status != StatusFailed {
		t.Fatalf("foreach status = %s, want failed", result.Status)
	}
	if result.Error == nil || len(result.Error.Aggregated) != 1 {
		t.Fatalf("aggregate error = %+v, want exactly one failed iteration", result.Error)
	}
	iterations := foreachIterationsFromResult(t, result)
	if len(iterations) != 5 {
		t.Fatalf("iterations = %d, want 5 including cancelled remaining", len(iterations))
	}
	for _, iteration := range iterations[1:] {
		if len(iteration.Results) != 1 || iteration.Results[0].Status != StatusCancelled {
			t.Fatalf("remaining iteration = %#v, want cancelled result", iteration)
		}
	}
}

func TestParallelAggregatesMultipleSubstepFailures(t *testing.T) {
	cfg := DefaultExecutorConfig("test")
	cfg.DryRun = true
	cfg.ProjectDir = t.TempDir()
	e := NewExecutor(cfg)
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "parallel-error-aggregation",
		Settings:      DefaultWorkflowSettings(),
		Steps: []Step{{
			ID: "parallel_group",
			Parallel: ParallelSpec{Steps: []Step{
				{ID: "ok", Prompt: "ok"},
				{ID: "bad_one", PromptFile: "/definitely/missing-one.txt"},
				{ID: "bad_two", PromptFile: "/definitely/missing-two.txt"},
				{ID: "ok_two", Prompt: "ok"},
				{ID: "ok_three", Prompt: "ok"},
			}},
		}},
	}
	e.graph = NewDependencyGraph(workflow)
	e.state = &ExecutionState{
		RunID:         "test-run",
		WorkflowID:    workflow.Name,
		Status:        StatusRunning,
		StartedAt:     time.Now(),
		Steps:         make(map[string]StepResult),
		Variables:     make(map[string]interface{}),
		ParallelState: make(map[string]ParallelGroupState),
	}

	result := e.executeParallel(context.Background(), &workflow.Steps[0], workflow)

	if result.Status != StatusFailed {
		t.Fatalf("parallel status = %s, want failed", result.Status)
	}
	if result.Error == nil {
		t.Fatal("parallel error = nil, want aggregate error")
	}
	if got := len(result.Error.Aggregated); got != 2 {
		t.Fatalf("aggregated errors = %d, want 2: %+v", got, result.Error.Aggregated)
	}
	if !strings.Contains(result.Error.Message, "2 of 5 parallel steps failed") {
		t.Fatalf("message = %q, want parallel failure count", result.Error.Message)
	}
	if !strings.Contains(result.Error.Details, "bad_one") || !strings.Contains(result.Error.Details, "bad_two") {
		t.Fatalf("details = %q, want both failed sub-step IDs", result.Error.Details)
	}
}
