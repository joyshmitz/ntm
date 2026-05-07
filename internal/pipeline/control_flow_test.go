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
		Branches: map[string]interface{}{
			"fresh-pass": "do-fresh",
		},
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
		Branches: map[string]interface{}{
			"audit-only": "do-audit",
		},
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
		"fresh-pass": map[string]interface{}{"steps": []string{"a", "b"}},
		"audit-only": "single-step",
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
		"a": "step-a",
		"b": "step-b",
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
		"a":       "step-a",
		"default": "fallback-step",
	}

	val, err := lookupBranch(branches, "unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "fallback-step" {
		t.Errorf("got %v, want %q", val, "fallback-step")
	}
}

// ---------------------------------------------------------------------------
// executeBranch integration tests
// ---------------------------------------------------------------------------

func TestExecuteBranch_LiteralMatch(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-lit",
		Branch: "fresh-pass",
		Branches: map[string]interface{}{
			"fresh-pass": "run-fresh",
			"audit-only": "run-audit",
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if result.Output != "fresh-pass" {
		t.Errorf("output=%q, want %q", result.Output, "fresh-pass")
	}
}

func TestExecuteBranch_ShellMatch(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-shell",
		Branch: "$(echo audit-only)",
		Branches: map[string]interface{}{
			"fresh-pass": "run-fresh",
			"audit-only": "run-audit",
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if result.Output != "audit-only" {
		t.Errorf("output=%q, want %q", result.Output, "audit-only")
	}
}

func TestExecuteBranch_NoMatch_Error(t *testing.T) {
	e := newBranchTestExecutor()
	step := &Step{
		ID:     "br-nomatch",
		Branch: "unknown-key",
		Branches: map[string]interface{}{
			"a": "step-a",
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
			"expected": "step-expected",
			"default":  "step-fallback",
		},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusCompleted {
		t.Fatalf("status=%s, want completed; error=%v", result.Status, result.Error)
	}
	if result.Output != "something-unexpected" {
		t.Errorf("output=%q, want %q", result.Output, "something-unexpected")
	}
}

func TestExecuteBranch_DryRun(t *testing.T) {
	e := newBranchTestExecutor()
	e.config.DryRun = true
	step := &Step{
		ID:     "br-dry",
		Branch: "$(echo hello)",
		Branches: map[string]interface{}{
			"hello": "step-hello",
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
		ID:       "br-shellfail",
		Branch:   "$(exit 42)",
		Branches: map[string]interface{}{"x": "y"},
	}

	result := e.executeBranch(context.Background(), step, &Workflow{Name: "test"})
	if result.Status != StatusFailed {
		t.Fatalf("status=%s, want failed", result.Status)
	}
	if result.Error == nil || !strings.Contains(result.Error.Message, "branch predicate failed") {
		t.Errorf("expected predicate-failed error, got: %v", result.Error)
	}
}
