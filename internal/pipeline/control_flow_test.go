package pipeline

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveBranch + lookupBranch tests (bd-w6nth.1)
// ---------------------------------------------------------------------------

func newBranchTestExecutor() *Executor {
	cfg := DefaultExecutorConfig("test-session")
	cfg.RunID = "run-branch-test"
	e := NewExecutor(cfg)
	e.state = &ExecutionState{
		RunID:      "run-branch-test",
		WorkflowID: "test",
		Variables:  map[string]interface{}{},
		Steps:      map[string]StepResult{},
	}
	return e
}

func TestResolveBranch_Literal(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "branch-lit",
		Branch: "fresh-pass",
	}

	key, err := e.resolveBranch(context.Background(), step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "fresh-pass" {
		t.Errorf("got %q, want %q", key, "fresh-pass")
	}
}

func TestResolveBranch_ShellCommand(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "branch-shell",
		Branch: "$(echo audit-only)",
	}

	key, err := e.resolveBranch(context.Background(), step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "audit-only" {
		t.Errorf("got %q, want %q", key, "audit-only")
	}
}

func TestResolveBranch_ShellTrimWhitespace(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "branch-ws",
		Branch: `$(printf "  spaced  \n")`,
	}

	key, err := e.resolveBranch(context.Background(), step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "spaced" {
		t.Errorf("got %q, want %q", key, "spaced")
	}
}

func TestResolveBranch_ShellFailure(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "branch-fail",
		Branch: "$(exit 1)",
	}

	_, err := e.resolveBranch(context.Background(), step)
	if err == nil {
		t.Fatal("expected error for failing shell command")
	}
}

func TestResolveBranch_VariableSubstitution(t *testing.T) {
	e := newBranchTestExecutor()
	e.state.Variables["mode"] = "fast"
	e.defaults = map[string]interface{}{"prefix": "run"}

	step := &Step{
		ID:     "branch-vars",
		Branch: "${vars.mode}",
	}

	key, err := e.resolveBranch(context.Background(), step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "fast" {
		t.Errorf("got %q, want %q", key, "fast")
	}
}

func TestLookupBranch_MatchFound(t *testing.T) {
	branches := map[string]interface{}{
		"fresh-pass": map[string]interface{}{"command": "echo fresh"},
		"audit-only": map[string]interface{}{"command": "echo audit"},
	}

	val, err := lookupBranch(branches, "fresh-pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val == nil {
		t.Fatal("expected non-nil value")
	}
}

func TestLookupBranch_NoMatch_Error(t *testing.T) {
	branches := map[string]interface{}{
		"a": map[string]interface{}{"command": "echo a"},
		"b": map[string]interface{}{"command": "echo b"},
	}

	_, err := lookupBranch(branches, "c")
	if err == nil {
		t.Fatal("expected error for unmatched branch key")
	}
	if !strings.Contains(err.Error(), "branch produced no matching key: c") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLookupBranch_DefaultFallback(t *testing.T) {
	branches := map[string]interface{}{
		"a":       map[string]interface{}{"command": "echo a"},
		"default": map[string]interface{}{"command": "echo fallback"},
	}

	val, err := lookupBranch(branches, "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val == nil {
		t.Fatal("expected non-nil value")
	}
}

// ---------------------------------------------------------------------------
// parseBranchSteps tests (bd-w6nth.2)
// ---------------------------------------------------------------------------

func TestParseBranchSteps_SingleStep(t *testing.T) {
	val := map[string]interface{}{
		"id":      "step-a",
		"command": "echo hello",
	}
	steps, err := parseBranchSteps(val, "parent", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("got %d steps, want 1", len(steps))
	}
	if steps[0].Command != "echo hello" {
		t.Errorf("command=%q, want %q", steps[0].Command, "echo hello")
	}
}

func TestParseBranchSteps_ListOfSteps(t *testing.T) {
	val := []interface{}{
		map[string]interface{}{"id": "s1", "command": "echo one"},
		map[string]interface{}{"id": "s2", "command": "echo two"},
		map[string]interface{}{"id": "s3", "command": "echo three"},
	}
	steps, err := parseBranchSteps(val, "parent", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if steps[2].Command != "echo three" {
		t.Errorf("step[2].command=%q, want %q", steps[2].Command, "echo three")
	}
}

func TestParseBranchSteps_AutoGeneratesID(t *testing.T) {
	val := map[string]interface{}{
		"command": "echo auto-id",
	}
	steps, err := parseBranchSteps(val, "dispatch", "fresh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if steps[0].ID != "dispatch.fresh.0" {
		t.Errorf("ID=%q, want %q", steps[0].ID, "dispatch.fresh.0")
	}
}

// ---------------------------------------------------------------------------
// executeBranch integration tests (bd-w6nth.1 + bd-w6nth.2)
// ---------------------------------------------------------------------------

func TestExecuteBranch_SingleCommandStep(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-cmd",
		Branch: "fresh-pass",
		Branches: map[string]interface{}{
			"fresh-pass": map[string]interface{}{
				"id":      "do-fresh",
				"command": "echo fresh-output",
			},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "fresh-output") {
		t.Errorf("output=%q, want to contain %q", result.Output, "fresh-output")
	}
}

func TestExecuteBranch_ShellDispatch(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-shell-disp",
		Branch: "$(echo audit-only)",
		Branches: map[string]interface{}{
			"fresh-pass": map[string]interface{}{"command": "echo fresh"},
			"audit-only": map[string]interface{}{"command": "echo audit-result"},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "audit-result") {
		t.Errorf("output=%q, want to contain %q", result.Output, "audit-result")
	}
}

func TestExecuteBranch_MultipleSteps(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-multi",
		Branch: "investigate",
		Branches: map[string]interface{}{
			"investigate": []interface{}{
				map[string]interface{}{"id": "inv-1", "command": "echo step-one"},
				map[string]interface{}{"id": "inv-2", "command": "echo step-two"},
				map[string]interface{}{"id": "inv-3", "command": "echo step-three"},
			},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "step-one") || !strings.Contains(result.Output, "step-three") {
		t.Errorf("output should contain all step outputs, got: %q", result.Output)
	}
}

func TestExecuteBranch_NoMatch_Error(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-nomatch",
		Branch: "unknown-key",
		Branches: map[string]interface{}{
			"a": map[string]interface{}{"command": "echo a"},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusFailed {
		t.Fatalf("status=%s, want failed", result.Status)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "branch produced no matching key") {
		t.Errorf("expected 'no matching key' error, got: %v", result.Error)
	}
}

func TestExecuteBranch_DefaultFallback(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-default",
		Branch: "$(echo something-unexpected)",
		Branches: map[string]interface{}{
			"expected": map[string]interface{}{"command": "echo expected"},
			"default":  map[string]interface{}{"command": "echo fallback-ran"},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if !strings.Contains(result.Output, "fallback-ran") {
		t.Errorf("expected fallback output, got: %q", result.Output)
	}
}

func TestExecuteBranch_DryRun(t *testing.T) {
	e := newBranchTestExecutor()
	e.config.DryRun = true
	step := &Step{
		ID:     "br-dry",
		Branch: "$(echo hello)",
		Branches: map[string]interface{}{
			"hello": map[string]interface{}{"command": "echo should-not-run"},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed", result.Status)
	}
	if !strings.Contains(result.Output, "DRY RUN") {
		t.Errorf("expected DRY RUN in output, got: %q", result.Output)
	}
}

func TestExecuteBranch_ShellFailure(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-shellfail",
		Branch: "$(exit 42)",
		Branches: map[string]interface{}{
			"x": map[string]interface{}{"command": "echo x"},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusFailed {
		t.Fatalf("status=%s, want failed", result.Status)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "branch predicate failed") {
		t.Errorf("expected predicate-failed error, got: %v", result.Error)
	}
}

func TestExecuteBranch_BodyStepFails(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-fail-body",
		Branch: "go",
		Branches: map[string]interface{}{
			"go": []interface{}{
				map[string]interface{}{"id": "ok-step", "command": "echo ok"},
				map[string]interface{}{"id": "fail-step", "command": "exit 1"},
				map[string]interface{}{"id": "skip-step", "command": "echo should-not-run"},
			},
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusFailed {
		t.Fatalf("status=%s, want failed", result.Status)
	}
}

func TestExecuteBranch_VariableScopeCleanup(t *testing.T) {
	e := newBranchTestExecutor()
	e.state.Variables["keep_me"] = "preserved"

	step := &Step{
		ID:     "br-scope",
		Branch: "go",
		Branches: map[string]interface{}{
			"go": map[string]interface{}{
				"id":      "scope-step",
				"command": "echo scoped",
			},
		},
	}

	e.executeBranch(context.Background(), step, &Workflow{Name: "test"})

	if e.state.Variables["keep_me"] != "preserved" {
		t.Errorf("pre-existing variable lost after branch execution")
	}
}
