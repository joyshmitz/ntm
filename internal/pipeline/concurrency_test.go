package pipeline

import (
	"fmt"
	"sync"
	"testing"
)

func TestApplyResumeStateConcurrentWithSnapshots(t *testing.T) {
	const stepCount = 200

	workflow := &Workflow{
		Name:  "resume-concurrency",
		Steps: make([]Step, 0, stepCount),
	}
	steps := make(map[string]StepResult, stepCount)
	variables := make(map[string]interface{}, stepCount*2)

	for i := 0; i < stepCount; i++ {
		stepID := fmt.Sprintf("step-%03d", i)
		outputVar := "out_" + stepID
		workflow.Steps = append(workflow.Steps, Step{ID: stepID, OutputVar: outputVar})

		status := StatusCompleted
		if i%3 == 0 {
			status = StatusFailed
		}
		steps[stepID] = StepResult{StepID: stepID, Status: status, Output: "value"}
		variables["steps."+stepID+".output"] = "value"
		variables["steps."+stepID+".data"] = map[string]interface{}{"ok": true}
		variables[outputVar] = "value"
		variables[outputVar+"_parsed"] = map[string]interface{}{"ok": true}
	}
	for i := 0; i < 50; i++ {
		stepID := fmt.Sprintf("orphan-%03d", i)
		steps[stepID] = StepResult{StepID: stepID, Status: StatusCompleted}
	}

	executor := NewExecutor(DefaultExecutorConfig("session"))
	executor.graph = NewDependencyGraph(workflow)
	executor.state = &ExecutionState{
		RunID:      "run-concurrency",
		WorkflowID: workflow.Name,
		Status:     StatusRunning,
		Steps:      steps,
		Variables:  variables,
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				if snapshot := executor.snapshotState(); snapshot == nil {
					t.Error("snapshotState returned nil")
					return
				}
			}
		}()
	}

	close(start)
	executor.applyResumeState()
	wg.Wait()

	executor.stateMu.RLock()
	for stepID, result := range executor.state.Steps {
		if result.Status == StatusFailed {
			t.Fatalf("rerunnable step %s still present after applyResumeState", stepID)
		}
	}
	if _, ok := executor.state.Steps["orphan-000"]; ok {
		t.Fatal("orphan step still present after applyResumeState")
	}
	executor.stateMu.RUnlock()

	executor.varMu.RLock()
	if _, ok := executor.state.Variables["out_step-000"]; ok {
		t.Fatal("rerunnable output var still present after applyResumeState")
	}
	if _, ok := executor.state.Variables["steps.step-000.output"]; ok {
		t.Fatal("rerunnable steps.* output still present after applyResumeState")
	}
	if _, ok := executor.state.Variables["out_step-001"]; !ok {
		t.Fatal("completed output var should be preserved")
	}
	executor.varMu.RUnlock()
}
