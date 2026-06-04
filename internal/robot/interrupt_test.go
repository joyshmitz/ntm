package robot

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// TestSelectInterruptTargetsEmptyOnUnmatchedFilter documents the targeting case
// at the heart of #172: a --panes filter that matches nothing resolves to an
// empty target set. On a window-per-agent layout every pane shares window-local
// index 0, so a filter like --panes=2 matches no pane.
func TestSelectInterruptTargetsEmptyOnUnmatchedFilter(t *testing.T) {
	// Window-per-agent layout: three windows, each a single pane at index 0.
	panes := []tmux.Pane{
		{ID: "%1", Index: 0, WindowIndex: 0, Type: tmux.AgentType("claude"), Title: "s__cc_1"},
		{ID: "%2", Index: 0, WindowIndex: 1, Type: tmux.AgentType("codex"), Title: "s__cod_1"},
		{ID: "%3", Index: 0, WindowIndex: 2, Type: tmux.AgentType("gemini"), Title: "s__gmi_1"},
	}

	// --panes=2 matches nothing (no pane has window-local index 2).
	filter := map[string]bool{"2": true}
	got := selectInterruptTargets(panes, filter, false)
	if len(got) != 0 {
		t.Fatalf("expected empty target set for --panes=2 on window-per-agent layout, got %d", len(got))
	}
}

// TestMarkInterruptFailuresFlipsEnvelope verifies the fail-loud behavior (#172):
// when one or more interrupt actions failed but the envelope still claims
// success, mark it failed; do not clobber an already-failed envelope; do not
// flip when there are no failures.
func TestMarkInterruptFailuresFlipsEnvelope(t *testing.T) {
	t.Run("flips on recorded failure", func(t *testing.T) {
		out := &InterruptOutput{
			RobotResponse: NewRobotResponse(true),
			Failed:        []InterruptError{{Pane: "1", Reason: "failed to send Ctrl+C"}},
		}
		markInterruptFailures(InterruptOptions{Session: "proj"}, out)
		if out.Success {
			t.Errorf("expected success=false after a failed action")
		}
		if out.ErrorCode != ErrCodeInternalError {
			t.Errorf("expected error_code=%q, got %q", ErrCodeInternalError, out.ErrorCode)
		}
		if out.Hint == "" {
			t.Errorf("expected a remediation hint")
		}
	})

	t.Run("no flip without failures", func(t *testing.T) {
		out := &InterruptOutput{RobotResponse: NewRobotResponse(true)}
		markInterruptFailures(InterruptOptions{Session: "proj"}, out)
		if !out.Success {
			t.Errorf("expected success to stay true with no failures")
		}
	})

	t.Run("does not clobber existing error envelope", func(t *testing.T) {
		out := &InterruptOutput{
			RobotResponse: NewErrorResponse(nil, ErrCodeTimeout, "increase timeout"),
			Failed:        []InterruptError{{Pane: "1", Reason: "boom"}},
		}
		markInterruptFailures(InterruptOptions{Session: "proj"}, out)
		if out.ErrorCode != ErrCodeTimeout {
			t.Errorf("expected timeout error_code preserved, got %q", out.ErrorCode)
		}
	})
}

// TestInterruptEmptyTargetHint verifies the empty-target remediation hint lists
// the panes that exist and warns about window-local addressing under --panes.
func TestInterruptEmptyTargetHint(t *testing.T) {
	panes := []tmux.Pane{
		{ID: "%1", Index: 0, WindowIndex: 0},
		{ID: "%2", Index: 1, WindowIndex: 0},
	}
	hint := interruptEmptyTargetHint(InterruptOptions{Session: "proj", Panes: []string{"5"}}, panes)
	if !strings.Contains(hint, "window-local") {
		t.Errorf("expected window-local warning under --panes, got %q", hint)
	}
	if !strings.Contains(hint, "0") || !strings.Contains(hint, "1") {
		t.Errorf("expected present pane indices 0 and 1 in hint, got %q", hint)
	}

	hintNoFilter := interruptEmptyTargetHint(InterruptOptions{Session: "proj"}, panes)
	if strings.Contains(hintNoFilter, "window-local") {
		t.Errorf("did not expect window-local warning without --panes, got %q", hintNoFilter)
	}
}

// TestGetInterruptUnknownSessionFailsLoud exercises the real GetInterrupt path
// for a session that does not exist (no live tmux needed): it must report
// success:false / SESSION_NOT_FOUND.
func TestGetInterruptUnknownSessionFailsLoud(t *testing.T) {
	out, err := GetInterrupt(InterruptOptions{Session: "ntm-nonexistent-session-for-test-172"})
	if err != nil {
		t.Fatalf("GetInterrupt returned unexpected error: %v", err)
	}
	if out.Success {
		t.Errorf("expected success=false for nonexistent session")
	}
	if out.ErrorCode != ErrCodeSessionNotFound {
		t.Errorf("expected error_code=%q, got %q", ErrCodeSessionNotFound, out.ErrorCode)
	}
}
