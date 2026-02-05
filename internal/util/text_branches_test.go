package util

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ExtractNewOutput — cover maxOverlap >= chunkSize optimisation (line 86-87)
// ---------------------------------------------------------------------------

func TestExtractNewOutput_LongNoOverlap(t *testing.T) {
	t.Parallel()

	// Both strings > 40 chars (chunkSize) with zero overlap.
	// Forces fallback path with maxOverlap >= chunkSize → clamped to chunkSize-1.
	before := strings.Repeat("x", 50)
	after := strings.Repeat("y", 50)

	got := ExtractNewOutput(before, after)
	if got != after {
		t.Errorf("expected full after string, got %q", got)
	}
}

func TestExtractNewOutput_LongPartialSmallOverlap(t *testing.T) {
	t.Parallel()

	// before ends with "hello", after starts with "hello".
	// Both > 40 chars total. Chunk search (first 40 chars of after) won't match
	// because after[:40] starts with "hello" followed by 'z's which don't appear
	// as a 40-char block in before.
	before := strings.Repeat("a", 50) + "hello"
	after := "hello" + strings.Repeat("z", 50)

	got := ExtractNewOutput(before, after)
	want := strings.Repeat("z", 50)
	if got != want {
		t.Errorf("ExtractNewOutput() = %q (len %d), want %q (len %d)", got, len(got), want, len(want))
	}
}

// ---------------------------------------------------------------------------
// Truncate — cover final return (line 133): all rune boundaries ≤ targetLen
// ---------------------------------------------------------------------------

func TestTruncate_MultibyteFinalReturn(t *testing.T) {
	t.Parallel()

	// "🎉🎉" is 8 bytes (4 per emoji). With n=7, targetLen=4.
	// Rune starts at byte 0 and 4 — both ≤ 4. The for-range loop completes
	// without triggering the early return, falling through to line 133.
	input := "🎉🎉"
	n := 7
	got := Truncate(input, n)
	want := "🎉..."
	if got != want {
		t.Errorf("Truncate(%q, %d) = %q, want %q", input, n, got, want)
	}
}

func TestTruncate_ThreeMultibyteChars(t *testing.T) {
	t.Parallel()

	// "日本語" is 9 bytes (3 per char). With n=8, targetLen=5.
	// Rune at 0 (≤5) → prevI=0; rune at 3 (≤5) → prevI=3; rune at 6 (>5) → return s[:3]+"..."
	input := "日本語"
	n := 8
	got := Truncate(input, n)
	want := "日..."
	if got != want {
		t.Errorf("Truncate(%q, %d) = %q, want %q", input, n, got, want)
	}
}
