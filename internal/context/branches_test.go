package context

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GetAgentCapabilities — cover missing agent types (cursor, windsurf, aider, openai, google)
// ---------------------------------------------------------------------------

func TestGetAgentCapabilities_Cursor(t *testing.T) {
	t.Parallel()
	caps := GetAgentCapabilities("cursor")
	if caps.SupportsBuiltinCompact {
		t.Error("cursor should not support builtin compact")
	}
	if caps.SupportsHistoryClear {
		t.Error("cursor should not support history clear")
	}
}

func TestGetAgentCapabilities_Windsurf(t *testing.T) {
	t.Parallel()
	caps := GetAgentCapabilities("windsurf")
	if caps.SupportsBuiltinCompact {
		t.Error("windsurf should not support builtin compact")
	}
	if caps.SupportsHistoryClear {
		t.Error("windsurf should not support history clear")
	}
}

func TestGetAgentCapabilities_Aider(t *testing.T) {
	t.Parallel()
	caps := GetAgentCapabilities("aider")
	if caps.SupportsBuiltinCompact {
		t.Error("aider should not support builtin compact")
	}
	if caps.SupportsHistoryClear {
		t.Error("aider should not support history clear")
	}
}

func TestGetAgentCapabilities_OpenAI(t *testing.T) {
	t.Parallel()
	caps := GetAgentCapabilities("openai")
	if caps.SupportsBuiltinCompact {
		t.Error("openai should not support builtin compact")
	}
	if caps.SupportsHistoryClear {
		t.Error("openai should not support history clear")
	}

	caps = GetAgentCapabilities("openai-codex")
	if caps.SupportsBuiltinCompact {
		t.Error("openai-codex should not support builtin compact")
	}
	if caps.SupportsHistoryClear {
		t.Error("openai-codex should not support history clear")
	}
}

func TestGetAgentCapabilities_Google(t *testing.T) {
	t.Parallel()
	caps := GetAgentCapabilities("google")
	// google is an alias for gemini
	if caps.SupportsBuiltinCompact {
		t.Error("google should not support builtin compact")
	}
	if !caps.SupportsHistoryClear {
		t.Error("google should support history clear")
	}

	caps = GetAgentCapabilities("google-gemini")
	if caps.SupportsBuiltinCompact {
		t.Error("google-gemini should not support builtin compact")
	}
	if !caps.SupportsHistoryClear {
		t.Error("google-gemini should support history clear")
	}
}

// ---------------------------------------------------------------------------
// MessageCountEstimator.Estimate — cover TokensPerMessage <= 0 default
// ---------------------------------------------------------------------------

func TestMessageCountEstimator_ZeroTokensPerMessage(t *testing.T) {
	t.Parallel()

	e := &MessageCountEstimator{TokensPerMessage: 0}
	state := &ContextState{
		MessageCount: 10,
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// With default 1500 tokens/msg: 10 * 1500 = 15000
	if est.TokensUsed != 15000 {
		t.Errorf("TokensUsed = %d, want 15000", est.TokensUsed)
	}
}

func TestMessageCountEstimator_NegativeTokensPerMessage(t *testing.T) {
	t.Parallel()

	e := &MessageCountEstimator{TokensPerMessage: -100}
	state := &ContextState{
		MessageCount: 10,
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// With default 1500 tokens/msg: 10 * 1500 = 15000
	if est.TokensUsed != 15000 {
		t.Errorf("TokensUsed = %d, want 15000", est.TokensUsed)
	}
}

// ---------------------------------------------------------------------------
// CumulativeTokenEstimator.Estimate — cover invalid discount values
// ---------------------------------------------------------------------------

func TestCumulativeTokenEstimator_ZeroDiscount(t *testing.T) {
	t.Parallel()

	e := &CumulativeTokenEstimator{CompactionDiscount: 0}
	state := &ContextState{
		cumulativeInputTokens:  5000,
		cumulativeOutputTokens: 5000,
		Model:                  "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// With default discount 0.7: 10000 * 0.7 = 7000
	if est.TokensUsed != 7000 {
		t.Errorf("TokensUsed = %d, want 7000", est.TokensUsed)
	}
}

func TestCumulativeTokenEstimator_DiscountGreaterThanOne(t *testing.T) {
	t.Parallel()

	e := &CumulativeTokenEstimator{CompactionDiscount: 1.5}
	state := &ContextState{
		cumulativeInputTokens:  5000,
		cumulativeOutputTokens: 5000,
		Model:                  "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// With default discount 0.7: 10000 * 0.7 = 7000
	if est.TokensUsed != 7000 {
		t.Errorf("TokensUsed = %d, want 7000", est.TokensUsed)
	}
}

func TestCumulativeTokenEstimator_NegativeDiscount(t *testing.T) {
	t.Parallel()

	e := &CumulativeTokenEstimator{CompactionDiscount: -0.5}
	state := &ContextState{
		cumulativeInputTokens:  5000,
		cumulativeOutputTokens: 5000,
		Model:                  "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// With default discount 0.7: 10000 * 0.7 = 7000
	if est.TokensUsed != 7000 {
		t.Errorf("TokensUsed = %d, want 7000", est.TokensUsed)
	}
}

// ---------------------------------------------------------------------------
// DurationActivityEstimator.Estimate — cover all branches
// ---------------------------------------------------------------------------

func TestDurationActivityEstimator_ZeroSessionStart(t *testing.T) {
	t.Parallel()

	e := &DurationActivityEstimator{}
	state := &ContextState{
		Model: "claude-opus-4",
		// SessionStart is zero
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est != nil {
		t.Errorf("expected nil estimate for zero session start, got %+v", est)
	}
}

func TestDurationActivityEstimator_ShortDuration(t *testing.T) {
	t.Parallel()

	e := &DurationActivityEstimator{}
	state := &ContextState{
		SessionStart: time.Now().Add(-30 * time.Second), // Only 30 seconds
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est != nil {
		t.Errorf("expected nil estimate for short duration, got %+v", est)
	}
}

func TestDurationActivityEstimator_HighActivity(t *testing.T) {
	t.Parallel()

	e := &DurationActivityEstimator{}
	state := &ContextState{
		SessionStart: time.Now().Add(-5 * time.Minute),
		MessageCount: 15, // 3 messages/minute (> 2)
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// High activity: 5 min * 1000 tokens/min = 5000
	if est.TokensUsed < 4500 || est.TokensUsed > 5500 {
		t.Errorf("TokensUsed = %d, expected ~5000", est.TokensUsed)
	}
}

func TestDurationActivityEstimator_LowActivity(t *testing.T) {
	t.Parallel()

	e := &DurationActivityEstimator{}
	state := &ContextState{
		SessionStart: time.Now().Add(-10 * time.Minute),
		MessageCount: 2, // 0.2 messages/minute (< 0.5)
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// Low activity: 10 min * 100 tokens/min = 1000
	if est.TokensUsed < 900 || est.TokensUsed > 1100 {
		t.Errorf("TokensUsed = %d, expected ~1000", est.TokensUsed)
	}
}

func TestDurationActivityEstimator_CustomTokensPerMinute(t *testing.T) {
	t.Parallel()

	e := &DurationActivityEstimator{
		TokensPerMinuteActive:   0,  // Should default to 1000
		TokensPerMinuteInactive: -5, // Should default to 100
	}
	state := &ContextState{
		SessionStart: time.Now().Add(-10 * time.Minute),
		MessageCount: 25, // 2.5 messages/minute (> 2, high activity)
		Model:        "claude-opus-4",
	}

	est, err := e.Estimate(state)
	if err != nil {
		t.Fatalf("Estimate error: %v", err)
	}
	if est == nil {
		t.Fatal("expected estimate, got nil")
	}
	// Should use default 1000 tokens/min: 10 * 1000 = 10000
	if est.TokensUsed < 9000 || est.TokensUsed > 11000 {
		t.Errorf("TokensUsed = %d, expected ~10000", est.TokensUsed)
	}
}

// ---------------------------------------------------------------------------
// extractAgentIndex — cover loop that finds no numeric parts
// ---------------------------------------------------------------------------

func TestExtractAgentIndex_NoNumericParts(t *testing.T) {
	t.Parallel()

	// "project__cc_alpha" has parts ["project", "", "cc", "alpha"], none are numeric
	got := extractAgentIndex("project__cc_alpha")
	if got != 1 {
		t.Errorf("extractAgentIndex(\"project__cc_alpha\") = %d, want 1", got)
	}
}

func TestExtractAgentIndex_AllAlpha(t *testing.T) {
	t.Parallel()

	// "a_b_c_d" has parts ["a", "b", "c", "d"], none are numeric
	got := extractAgentIndex("a_b_c_d")
	if got != 1 {
		t.Errorf("extractAgentIndex(\"a_b_c_d\") = %d, want 1", got)
	}
}
