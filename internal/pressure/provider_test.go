package pressure

import (
	"math"
	"testing"
)

// bd-mmzvs: parseMemoryRatio must distinguish "MemAvailable line not
// present" from "MemAvailable observed as zero". A stripped /proc
// (old kernel, minimal container) without MemAvailable must report
// (0, false), not silently surface used=1.0 from an absent-equals-zero
// numerator.

func TestParseMemoryRatio_StandardMeminfoYieldsUsedRatio(t *testing.T) {
	t.Parallel()
	// 8 GiB total, 6 GiB available → used = 1 - 6/8 = 0.25.
	raw := "MemTotal:        8388608 kB\nMemAvailable:    6291456 kB\nBuffers:           12345 kB\n"
	got, ok := parseMemoryRatio(raw)
	if !ok {
		t.Fatalf("ok = false, want true for standard meminfo: %q", raw)
	}
	if math.Abs(got-0.25) > 1e-9 {
		t.Errorf("used = %v, want 0.25", got)
	}
}

func TestParseMemoryRatio_MissingMemAvailableReturnsNoData(t *testing.T) {
	t.Parallel()
	// MemTotal present but MemAvailable absent — the pre-fix bug
	// would produce used=1.0 (100%) by treating absent as zero.
	raw := "MemTotal:        8388608 kB\nMemFree:         1234567 kB\nBuffers:           12345 kB\n"
	got, ok := parseMemoryRatio(raw)
	if ok {
		t.Fatalf("ok = true with used=%v, want (0,false) when MemAvailable absent", got)
	}
	if got != 0 {
		t.Errorf("missing-MemAvailable used = %v, want 0", got)
	}
}

func TestParseMemoryRatio_EmptyInputReturnsNoData(t *testing.T) {
	t.Parallel()
	if got, ok := parseMemoryRatio(""); ok || got != 0 {
		t.Errorf("parseMemoryRatio(empty) = (%v, %v), want (0, false)", got, ok)
	}
}

func TestParseMemoryRatio_TotalZeroReturnsNoData(t *testing.T) {
	t.Parallel()
	raw := "MemTotal:              0 kB\nMemAvailable:    1234567 kB\n"
	if got, ok := parseMemoryRatio(raw); ok || got != 0 {
		t.Errorf("parseMemoryRatio(total=0) = (%v, %v), want (0, false)", got, ok)
	}
}

func TestParseMemoryRatio_MemAvailableZeroIsValidFullPressure(t *testing.T) {
	t.Parallel()
	// MemAvailable observed as zero is genuine 100% pressure (rare but
	// possible on a host with no reclaimable pages). Distinct from the
	// missing-line case above: the line IS present, value IS zero.
	raw := "MemTotal:        8388608 kB\nMemAvailable:          0 kB\n"
	got, ok := parseMemoryRatio(raw)
	if !ok {
		t.Fatalf("ok = false, want true when MemAvailable is observed-zero")
	}
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("used = %v, want 1.0", got)
	}
}

func TestParseMemoryRatio_NegativeAvailableClampsToFullPressure(t *testing.T) {
	t.Parallel()
	// Defensive: if /proc somehow surfaces MemAvailable > MemTotal,
	// the clamp to [0,1] keeps the ratio sane.
	raw := "MemTotal:        1000 kB\nMemAvailable:    5000 kB\n"
	got, ok := parseMemoryRatio(raw)
	if !ok {
		t.Fatalf("ok = false, want true even with available > total")
	}
	if got != 0 {
		// available=5000, total=1000: 1 - 5000/1000 = -4 → clamp to 0
		t.Errorf("clamped used = %v, want 0 (clamp of negative)", got)
	}
}

func TestParseMemoryRatio_IgnoresMalformedLines(t *testing.T) {
	t.Parallel()
	raw := "garbage line\nMemTotal: invalid\nMemTotal:        8388608 kB\nMemAvailable:    4194304 kB\nMemFree only-one-field\n"
	got, ok := parseMemoryRatio(raw)
	if !ok {
		t.Fatalf("ok = false, want true with mostly-valid input")
	}
	if math.Abs(got-0.5) > 1e-9 {
		t.Errorf("used = %v, want 0.5 (4 GiB used of 8 GiB)", got)
	}
}
