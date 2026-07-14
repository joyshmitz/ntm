package robot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// =============================================================================
// normalizeBulkAssignStrategy tests
// =============================================================================

func TestNormalizeBulkAssignStrategy(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"empty string", "", "impact", false},
		{"impact", "impact", "impact", false},
		{"ready", "ready", "ready", false},
		{"stale", "stale", "stale", false},
		{"balanced", "balanced", "balanced", false},
		{"unknown", "foobar", "", true},
		{"uppercase", "IMPACT", "impact", false},
		{"mixed case", "Ready", "ready", false},
		{"with spaces", "  stale  ", "stale", false},
		{"with tabs", "\tbalanced\t", "balanced", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeBulkAssignStrategy(tc.input)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Errorf("normalizeBulkAssignStrategy(%q) = (%q, %v), want (%q, error=%v)", tc.input, got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestBulkAssignFailureClassPreservesTypedCausePriority(t *testing.T) {
	tests := []struct {
		name        string
		assignments []BulkAssignAssignment
		want        string
	}{
		{name: "operational", assignments: []BulkAssignAssignment{{Status: "failed", failureCause: errors.New("dispatch failed")}}, want: "ASSIGNMENT_FAILED"},
		{name: "pane", assignments: []BulkAssignAssignment{{Status: "failed", failureCode: ErrCodePaneNotFound}}, want: ErrCodePaneNotFound},
		{name: "invalid", assignments: []BulkAssignAssignment{{Status: "failed", failureCode: ErrCodeInvalidFlag}}, want: ErrCodeInvalidFlag},
		{name: "deadline beats invalid", assignments: []BulkAssignAssignment{{Status: "failed", failureCode: ErrCodeInvalidFlag}, {Status: "failed", failureCause: fmt.Errorf("title fetch: %w", context.DeadlineExceeded)}}, want: ErrCodeTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, _ := bulkAssignFailureClass(test.assignments)
			if code != test.want {
				t.Fatalf("bulkAssignFailureClass() = %q, want %q", code, test.want)
			}
		})
	}
}

// =============================================================================
// formatBulkAssignDeps tests
// =============================================================================

func TestFormatBulkAssignDeps(t *testing.T) {

	tests := []struct {
		name string
		deps []string
		want string
	}{
		{"nil", nil, "none"},
		{"empty", []string{}, "none"},
		{"single", []string{"bd-abc"}, "bd-abc"},
		{"multiple", []string{"bd-abc", "bd-def", "bd-ghi"}, "bd-abc, bd-def, bd-ghi"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatBulkAssignDeps(tc.deps)
			if got != tc.want {
				t.Errorf("formatBulkAssignDeps(%v) = %q, want %q", tc.deps, got, tc.want)
			}
		})
	}
}

// =============================================================================
// bulkAssignReason tests
// =============================================================================

func TestBulkAssignReason(t *testing.T) {

	t.Run("impact source", func(t *testing.T) {
		bead := bulkBead{Source: bulkSourceImpact, UnblocksCount: 5}
		got := bulkAssignReason(bead)
		want := "highest_unblocks (5 items)"
		if got != want {
			t.Errorf("bulkAssignReason(impact) = %q, want %q", got, want)
		}
	})

	t.Run("stale source with time", func(t *testing.T) {
		ts := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
		bead := bulkBead{Source: bulkSourceStale, UpdatedAt: ts}
		got := bulkAssignReason(bead)
		want := "stale_in_progress (2026-01-15T10:00:00Z)"
		if got != want {
			t.Errorf("bulkAssignReason(stale) = %q, want %q", got, want)
		}
	})

	t.Run("stale source zero time", func(t *testing.T) {
		bead := bulkBead{Source: bulkSourceStale}
		got := bulkAssignReason(bead)
		want := "stale_in_progress (unknown)"
		if got != want {
			t.Errorf("bulkAssignReason(stale-zero) = %q, want %q", got, want)
		}
	})

	t.Run("ready source with priority", func(t *testing.T) {
		bead := bulkBead{Source: bulkSourceReady, Priority: 2}
		got := bulkAssignReason(bead)
		want := "ready_priority P2"
		if got != want {
			t.Errorf("bulkAssignReason(ready-p2) = %q, want %q", got, want)
		}
	})

	t.Run("ready source zero priority", func(t *testing.T) {
		bead := bulkBead{Source: bulkSourceReady, Priority: 0}
		got := bulkAssignReason(bead)
		want := "ready_priority"
		if got != want {
			t.Errorf("bulkAssignReason(ready-p0) = %q, want %q", got, want)
		}
	})
}

// =============================================================================
// parseBulkAssignAllocation tests
// =============================================================================

func TestParseBulkAssignAllocation(t *testing.T) {

	t.Run("valid allocation", func(t *testing.T) {
		result, err := parseBulkAssignAllocation(`{"1": "bd-abc", "2": "bd-def"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result["1"] != "bd-abc" {
			t.Errorf("result[1] = %q, want %q", result["1"], "bd-abc")
		}
		if result["2"] != "bd-def" {
			t.Errorf("result[2] = %q, want %q", result["2"], "bd-def")
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := parseBulkAssignAllocation("")
		if err == nil {
			t.Error("expected error for empty string")
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		_, err := parseBulkAssignAllocation("   ")
		if err == nil {
			t.Error("expected error for whitespace-only input")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := parseBulkAssignAllocation("{invalid}")
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("non-integer key", func(t *testing.T) {
		_, err := parseBulkAssignAllocation(`{"abc": "bd-xyz"}`)
		if err == nil {
			t.Error("expected error for non-integer pane index")
		}
	})

	t.Run("trims whitespace in keys and values", func(t *testing.T) {
		result, err := parseBulkAssignAllocation(`{" 3 ": " bd-trimmed "}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result["3"] != "bd-trimmed" {
			t.Errorf("result[3] = %q, want %q", result["3"], "bd-trimmed")
		}
	})
}
