package context

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewPendingRotationOutput
// ---------------------------------------------------------------------------

func TestNewPendingRotationOutput(t *testing.T) {
	t.Parallel()

	t.Run("basic_fields", func(t *testing.T) {
		t.Parallel()
		p := &PendingRotation{
			AgentID:        "myproject__cc_1",
			SessionName:    "myproject",
			PaneID:         "%3",
			ContextPercent: 85.5,
			CreatedAt:      time.Now(),
			TimeoutAt:      time.Now().Add(5 * time.Minute),
			DefaultAction:  ConfirmRotate,
		}

		out := NewPendingRotationOutput(p)

		if out.Type != "rotation_pending" {
			t.Errorf("Type = %q, want %q", out.Type, "rotation_pending")
		}
		if out.AgentID != "myproject__cc_1" {
			t.Errorf("AgentID = %q, want %q", out.AgentID, "myproject__cc_1")
		}
		if out.SessionName != "myproject" {
			t.Errorf("SessionName = %q, want %q", out.SessionName, "myproject")
		}
		if out.ContextPercent != 85.5 {
			t.Errorf("ContextPercent = %v, want 85.5", out.ContextPercent)
		}
		if !out.AwaitingConfirm {
			t.Error("AwaitingConfirm should be true")
		}
		if out.DefaultAction != "rotate" {
			t.Errorf("DefaultAction = %q, want %q", out.DefaultAction, "rotate")
		}
		if len(out.AvailableActions) != 4 {
			t.Errorf("AvailableActions length = %d, want 4", len(out.AvailableActions))
		}
		if out.GeneratedAt == "" {
			t.Error("GeneratedAt should not be empty")
		}
		// TimeoutSeconds should be positive (5 minutes in the future)
		if out.TimeoutSeconds <= 0 {
			t.Errorf("TimeoutSeconds = %d, want > 0", out.TimeoutSeconds)
		}
	})

	t.Run("expired_timeout_clamped_to_zero", func(t *testing.T) {
		t.Parallel()
		p := &PendingRotation{
			AgentID:       "myproject__cod_1",
			SessionName:   "myproject",
			TimeoutAt:     time.Now().Add(-10 * time.Second),
			DefaultAction: ConfirmCompact,
		}

		out := NewPendingRotationOutput(p)

		if out.TimeoutSeconds != 0 {
			t.Errorf("TimeoutSeconds = %d, want 0 for expired rotation", out.TimeoutSeconds)
		}
		if out.DefaultAction != "compact" {
			t.Errorf("DefaultAction = %q, want %q", out.DefaultAction, "compact")
		}
	})

	t.Run("available_actions_complete", func(t *testing.T) {
		t.Parallel()
		p := &PendingRotation{
			TimeoutAt:     time.Now().Add(time.Minute),
			DefaultAction: ConfirmIgnore,
		}

		out := NewPendingRotationOutput(p)

		want := map[string]bool{"rotate": true, "compact": true, "ignore": true, "postpone": true}
		for _, action := range out.AvailableActions {
			if !want[action] {
				t.Errorf("unexpected action %q", action)
			}
			delete(want, action)
		}
		for action := range want {
			t.Errorf("missing action %q", action)
		}
	})
}

// ---------------------------------------------------------------------------
// RemainingSeconds
// ---------------------------------------------------------------------------

func TestRemainingSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timeoutAt time.Time
		wantMin   int
		wantMax   int
	}{
		{
			name:      "five_minutes_future",
			timeoutAt: time.Now().Add(5 * time.Minute),
			wantMin:   295, // ~5min minus tolerance
			wantMax:   305,
		},
		{
			name:      "expired_returns_zero",
			timeoutAt: time.Now().Add(-1 * time.Minute),
			wantMin:   0,
			wantMax:   0,
		},
		{
			name:      "just_expired_returns_zero",
			timeoutAt: time.Now().Add(-1 * time.Second),
			wantMin:   0,
			wantMax:   0,
		},
		{
			name:      "one_second_future",
			timeoutAt: time.Now().Add(1 * time.Second),
			wantMin:   0,
			wantMax:   2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &PendingRotation{TimeoutAt: tc.timeoutAt}
			got := p.RemainingSeconds()
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("RemainingSeconds() = %d, want [%d, %d]", got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsExpired
// ---------------------------------------------------------------------------

func TestIsExpired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timeoutAt time.Time
		want      bool
	}{
		{
			name:      "future_not_expired",
			timeoutAt: time.Now().Add(5 * time.Minute),
			want:      false,
		},
		{
			name:      "past_expired",
			timeoutAt: time.Now().Add(-5 * time.Minute),
			want:      true,
		},
		{
			name:      "just_past_expired",
			timeoutAt: time.Now().Add(-1 * time.Second),
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := &PendingRotation{TimeoutAt: tc.timeoutAt}
			if got := p.IsExpired(); got != tc.want {
				t.Errorf("IsExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}
