package handoff

import (
	"testing"
)

// ---------------------------------------------------------------------------
// IsValid — 0% → 100%
// ---------------------------------------------------------------------------

func TestIsValid_ValidHandoff(t *testing.T) {
	t.Parallel()

	h := New("test-session").
		WithGoalAndNow("Implement feature", "Write tests").
		WithStatus(StatusComplete, OutcomeSucceeded)

	if !h.IsValid() {
		t.Error("expected valid handoff to return true")
	}
}

func TestIsValid_InvalidHandoff(t *testing.T) {
	t.Parallel()

	h := &Handoff{} // empty, missing required fields
	if h.IsValid() {
		t.Error("expected invalid handoff to return false")
	}
}

// ---------------------------------------------------------------------------
// MustValidate — 0% → 100%
// ---------------------------------------------------------------------------

func TestMustValidate_ValidHandoff(t *testing.T) {
	t.Parallel()

	h := New("test-session").
		WithGoalAndNow("Implement feature", "Write tests").
		WithStatus(StatusComplete, OutcomeSucceeded)

	// Should not panic
	h.MustValidate()
}

func TestMustValidate_InvalidHandoff_Panics(t *testing.T) {
	t.Parallel()

	h := &Handoff{} // empty, missing required fields

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected MustValidate to panic on invalid handoff")
		}
	}()

	h.MustValidate()
}
