package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"
)

func createForeachTestExecutor(t *testing.T, workflow *Workflow) *Executor {
	t.Helper()
	cfg := DefaultExecutorConfig("test")
	cfg.DefaultTimeout = 2 * time.Second
	e := NewExecutor(cfg)
	e.graph = NewDependencyGraph(workflow)
	e.state = &ExecutionState{
		RunID:      "test-run",
		WorkflowID: workflow.Name,
		Status:     StatusRunning,
		StartedAt:  time.Now(),
		Steps:      make(map[string]StepResult),
		Variables:  make(map[string]interface{}),
	}
	e.defaults = workflow.Defaults
	e.limits = workflow.Settings.Limits.EffectiveLimits()
	return e
}

func TestExecuteStepOnceForeachSequentialDispatchesOrderedResults(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-sequential",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID: "fanout",
		Foreach: &ForeachConfig{
			Items: `["a","b","c"]`,
			Steps: []Step{{
				ID:      "echo",
				Command: `printf '%s' '${item}'`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeStepOnce(context.Background(), step, workflow)

	if result.Status != StatusCompleted {
		t.Fatalf("foreach status = %s, error = %#v", result.Status, result.Error)
	}
	iterations := foreachIterationsFromResult(t, result)
	if len(iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(iterations))
	}
	for i, want := range []string{"a", "b", "c"} {
		if len(iterations[i].Results) != 1 {
			t.Fatalf("iteration %d results = %d, want 1", i, len(iterations[i].Results))
		}
		if got := iterations[i].Results[0].Output; got != want {
			t.Fatalf("iteration %d output = %q, want %q", i, got, want)
		}
		stepID := "fanout_iter" + string(rune('0'+i)) + "_echo"
		if stored := e.state.Steps[stepID]; stored.Output != want {
			t.Fatalf("stored result %s output = %q, want %q", stepID, stored.Output, want)
		}
	}
}

func TestExecuteForeachParallelMaxConcurrentCompletesAllIterations(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-parallel",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID: "parallel_fanout",
		Foreach: &ForeachConfig{
			Items:         `["a","b","c"]`,
			Parallel:      true,
			MaxConcurrent: 2,
			Steps: []Step{{
				ID:      "echo",
				Command: `printf '%s' '${item}'`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeForeach(context.Background(), step, workflow)

	if result.Status != StatusCompleted {
		t.Fatalf("foreach status = %s, error = %#v", result.Status, result.Error)
	}
	iterations := foreachIterationsFromResult(t, result)
	if len(iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(iterations))
	}
	seen := map[string]bool{}
	for _, iteration := range iterations {
		if len(iteration.Results) != 1 {
			t.Fatalf("iteration %d results = %d, want 1", iteration.Index, len(iteration.Results))
		}
		seen[iteration.Results[0].Output] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !seen[want] {
			t.Fatalf("parallel foreach missing output %q in %#v", want, seen)
		}
	}
}

func TestExecuteForeachContinueKeepsOtherIterationsAfterFailure(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-continue",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID:      "continue_fanout",
		OnError: ErrorActionContinue,
		Foreach: &ForeachConfig{
			Items: `["one","bad","two"]`,
			Steps: []Step{{
				ID:      "maybe",
				Command: `case '${item}' in bad) echo failed; exit 7;; *) printf '%s' '${item}';; esac`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeForeach(context.Background(), step, workflow)

	if result.Status != StatusCompleted {
		t.Fatalf("foreach status = %s, error = %#v", result.Status, result.Error)
	}
	iterations := foreachIterationsFromResult(t, result)
	if len(iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(iterations))
	}
	if iterations[0].Results[0].Status != StatusCompleted || iterations[0].Results[0].Output != "one" {
		t.Fatalf("iteration 0 result = %#v, want completed one", iterations[0].Results[0])
	}
	if iterations[1].Results[0].Status != StatusFailed {
		t.Fatalf("iteration 1 status = %s, want failed", iterations[1].Results[0].Status)
	}
	if iterations[2].Results[0].Status != StatusCompleted || iterations[2].Results[0].Output != "two" {
		t.Fatalf("iteration 2 result = %#v, want completed two", iterations[2].Results[0])
	}
	if !strings.Contains(result.Output, "1 failed") {
		t.Fatalf("foreach output = %q, want failure count", result.Output)
	}
}

func TestExecuteForeachFilterExcludesIterationsBeforeDispatch(t *testing.T) {
	workflow := &Workflow{
		SchemaVersion: SchemaVersion,
		Name:          "foreach-filter",
		Settings:      DefaultWorkflowSettings(),
	}
	step := &Step{
		ID: "filtered_fanout",
		Foreach: &ForeachConfig{
			Items:  `[{"id":"a","role":"keep"},{"id":"b","role":"drop"},{"id":"c","role":"keep"}]`,
			Filter: `role==keep`,
			Steps: []Step{{
				ID:      "echo",
				Command: `printf '%s' '${item.id}'`,
			}},
		},
	}
	workflow.Steps = []Step{*step}
	e := createForeachTestExecutor(t, workflow)

	result := e.executeForeach(context.Background(), step, workflow)

	if result.Status != StatusCompleted {
		t.Fatalf("foreach status = %s, error = %#v", result.Status, result.Error)
	}
	iterations := foreachIterationsFromResult(t, result)
	if len(iterations) != 3 {
		t.Fatalf("iterations = %d, want 3", len(iterations))
	}
	var outputs []string
	var skipped int
	for _, iteration := range iterations {
		if iteration.Skipped {
			skipped++
			if iteration.SkipKind != SkipKindForeachFilter {
				t.Fatalf("skip kind = %q, want %q", iteration.SkipKind, SkipKindForeachFilter)
			}
			continue
		}
		if len(iteration.Results) != 1 {
			t.Fatalf("iteration %d results = %d, want 1", iteration.Index, len(iteration.Results))
		}
		outputs = append(outputs, iteration.Results[0].Output)
	}
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if strings.Join(outputs, ",") != "a,c" {
		t.Fatalf("dispatched outputs = %v, want [a c]", outputs)
	}
	if got := len(e.state.Steps); got != 2 {
		t.Fatalf("stored dispatched steps = %d, want 2", got)
	}
}

func foreachIterationsFromResult(t *testing.T, result StepResult) []foreachIterationResult {
	t.Helper()
	iterations, ok := result.ParsedData.([]foreachIterationResult)
	if !ok {
		t.Fatalf("ParsedData type = %T, want []foreachIterationResult", result.ParsedData)
	}
	return iterations
}
