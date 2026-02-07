package webhook

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/events"
)

// =============================================================================
// isWebhookDispatchType — 0% → 100%
// =============================================================================

func TestIsWebhookDispatchType_ValidTypes(t *testing.T) {
	t.Parallel()
	validTypes := []string{
		events.WebhookAgentError,
		events.WebhookAgentStarted,
		events.WebhookAgentStopped,
		events.WebhookAgentCrashed,
		events.WebhookAgentRestarted,
		events.WebhookAgentIdle,
		events.WebhookAgentBusy,
		events.WebhookAgentRateLimit,
		events.WebhookAgentCompleted,
		events.WebhookRotationNeeded,
		events.WebhookSessionCreated,
		events.WebhookSessionKilled,
		events.WebhookSessionEnded,
		events.WebhookBeadAssigned,
		events.WebhookBeadCompleted,
		events.WebhookBeadFailed,
		events.WebhookHealthDegraded,
	}
	for _, et := range validTypes {
		if !isWebhookDispatchType(et) {
			t.Errorf("isWebhookDispatchType(%q) = false, want true", et)
		}
	}
}

func TestIsWebhookDispatchType_Invalid(t *testing.T) {
	t.Parallel()
	invalidTypes := []string{
		"unknown",
		"",
		"custom.event",
		"agent.unknown",
	}
	for _, et := range invalidTypes {
		if isWebhookDispatchType(et) {
			t.Errorf("isWebhookDispatchType(%q) = true, want false", et)
		}
	}
}

// =============================================================================
// stringFromAny — 0% → 100%
// =============================================================================

func TestStringFromAny_String(t *testing.T) {
	t.Parallel()
	if got := stringFromAny("hello"); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestStringFromAny_Nil(t *testing.T) {
	t.Parallel()
	if got := stringFromAny(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestStringFromAny_Int(t *testing.T) {
	t.Parallel()
	got := stringFromAny(42)
	if got != "42" {
		t.Errorf("got %q", got)
	}
}

type testStringer struct{}

func (t testStringer) String() string { return "stringer-output" }

func TestStringFromAny_Stringer(t *testing.T) {
	t.Parallel()
	if got := stringFromAny(testStringer{}); got != "stringer-output" {
		t.Errorf("got %q", got)
	}
}

// =============================================================================
// firstNonEmptyString — 0% → 100%
// =============================================================================

func TestFirstNonEmptyString_Found(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyString("", "  ", "hello", "world"); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestFirstNonEmptyString_AllEmpty(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyString("", " ", "\t"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFirstNonEmptyString_First(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyString("first", "second"); got != "first" {
		t.Errorf("got %q", got)
	}
}

func TestFirstNonEmptyString_None(t *testing.T) {
	t.Parallel()
	if got := firstNonEmptyString(); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// =============================================================================
// DefaultRetryConfig — 0% → 100%
// =============================================================================

func TestDefaultRetryConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultRetryConfig()
	if !cfg.Enabled {
		t.Error("expected enabled")
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DefaultMaxRetries)
	}
	if cfg.BaseDelay != 1*time.Second {
		t.Errorf("BaseDelay = %v", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("MaxDelay = %v", cfg.MaxDelay)
	}
}
