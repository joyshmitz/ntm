package context

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// truncateToTokens — 75% → 100% (edge cases)
// ---------------------------------------------------------------------------

func TestTruncateToTokens_ShortText(t *testing.T) {
	t.Parallel()

	// Text shorter than maxTokens*4 chars should be returned unchanged.
	short := "Hello world."
	got := truncateToTokens(short, 1000)
	if got != short {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestTruncateToTokens_ExactBoundary(t *testing.T) {
	t.Parallel()

	// Text with exactly maxTokens*4 chars should be returned unchanged.
	text := strings.Repeat("a", 200)
	got := truncateToTokens(text, 50) // 50*4 = 200
	if got != text {
		t.Errorf("expected unchanged text for exact boundary, got len=%d", len(got))
	}
}

func TestTruncateToTokens_NoPeriodOrNewline(t *testing.T) {
	t.Parallel()

	// Long text with no sentence boundaries should still truncate.
	text := strings.Repeat("x", 1000)
	got := truncateToTokens(text, 50) // 50*4 = 200 chars max
	if !strings.Contains(got, "[Summary truncated") {
		t.Error("expected truncation notice")
	}
	if len(got) > 250 { // some slack for the notice
		t.Errorf("expected truncated text, got len=%d", len(got))
	}
}

func TestTruncateToTokens_PeriodInLastQuarter(t *testing.T) {
	t.Parallel()

	// Text with a period in the last quarter should cut at the period.
	// Build text: 180 chars of filler, then a period, then more filler.
	text := strings.Repeat("a", 180) + ". " + strings.Repeat("b", 200)
	got := truncateToTokens(text, 50) // 50*4 = 200 chars max
	if !strings.Contains(got, "[Summary truncated") {
		t.Error("expected truncation notice")
	}
	// Should have cut at the period (position 181), not mid-word.
	if !strings.HasPrefix(got, strings.Repeat("a", 180)+".") {
		t.Errorf("expected cut at period boundary, got len=%d", len(got))
	}
}

func TestTruncateToTokens_NewlineInLastQuarter(t *testing.T) {
	t.Parallel()

	// Text with a newline (but no period) in the last quarter.
	text := strings.Repeat("a", 180) + "\n" + strings.Repeat("b", 200)
	got := truncateToTokens(text, 50) // 50*4 = 200 chars max
	if !strings.Contains(got, "[Summary truncated") {
		t.Error("expected truncation notice")
	}
}

// ---------------------------------------------------------------------------
// truncateAtRuneBoundary — additional edge cases
// ---------------------------------------------------------------------------

func TestTruncateAtRuneBoundary_NegativeMax(t *testing.T) {
	t.Parallel()

	// Negative maxBytes should return empty string (same as 0).
	got := truncateAtRuneBoundary("hello", -1)
	if got != "" {
		t.Errorf("expected empty for negative max, got %q", got)
	}
}

func TestTruncateAtRuneBoundary_SingleMultibyteRune(t *testing.T) {
	t.Parallel()

	// Emoji: 4 bytes. maxBytes=2 should return empty (can't fit the rune).
	got := truncateAtRuneBoundary("😀hello", 2)
	if got != "" {
		t.Errorf("expected empty when can't fit first rune, got %q", got)
	}
}
