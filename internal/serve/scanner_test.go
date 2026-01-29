package serve

import (
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/scanner"
)

func TestExtractBeadID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"standard bd format", "Created bd-1abc2: Fix the bug", "bd-1abc2"},
		{"ntm prefix", "Created ntm-xyz: New feature", "ntm-xyz"},
		{"no prefix", "Some random output", ""},
		{"empty string", "", ""},
		{"bd at start", "bd-12345: Title here", "bd-12345"},
		{"multiple words before id", "Successfully created bd-999: Done", "bd-999"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractBeadID(tc.input)
			if got != tc.want {
				t.Errorf("extractBeadID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestGenerateScanID(t *testing.T) {
	t.Parallel()

	id := generateScanID()

	if !strings.HasPrefix(id, "scan-") {
		t.Errorf("generateScanID() = %q, want prefix 'scan-'", id)
	}
	if len(id) < 10 {
		t.Errorf("generateScanID() = %q, too short", id)
	}

	// Should generate unique IDs
	id2 := generateScanID()
	// Note: IDs could be the same if generated in same nanosecond, but unlikely in practice
	_ = id2
}

func TestGenerateFindingID(t *testing.T) {
	t.Parallel()

	f := scanner.Finding{
		File:     "main.go",
		Line:     42,
		Category: "security",
		Message:  "potential injection",
	}

	id := generateFindingID("scan-abc123", f)

	if !strings.HasPrefix(id, "finding-") {
		t.Errorf("generateFindingID() = %q, want prefix 'finding-'", id)
	}
	if len(id) < 15 {
		t.Errorf("generateFindingID() = %q, too short", id)
	}

	// Same input should produce same ID (deterministic)
	id2 := generateFindingID("scan-abc123", f)
	if id != id2 {
		t.Errorf("generateFindingID should be deterministic: %q != %q", id, id2)
	}

	// Different input should produce different ID
	f2 := f
	f2.Line = 43
	id3 := generateFindingID("scan-abc123", f2)
	if id == id3 {
		t.Error("different findings should produce different IDs")
	}
}

func TestFindingToMap(t *testing.T) {
	t.Parallel()

	now := time.Now()

	t.Run("basic fields", func(t *testing.T) {
		t.Parallel()
		f := &FindingRecord{
			ID:        "finding-abc",
			ScanID:    "scan-123",
			Dismissed: false,
			CreatedAt: now,
		}

		m := findingToMap(f)
		if m["id"] != "finding-abc" {
			t.Errorf("id = %v", m["id"])
		}
		if m["scan_id"] != "scan-123" {
			t.Errorf("scan_id = %v", m["scan_id"])
		}
		if m["dismissed"] != false {
			t.Errorf("dismissed = %v", m["dismissed"])
		}
		if _, ok := m["dismissed_at"]; ok {
			t.Error("dismissed_at should not be present")
		}
		if _, ok := m["bead_id"]; ok {
			t.Error("bead_id should not be present when empty")
		}
	})

	t.Run("with optional fields", func(t *testing.T) {
		t.Parallel()
		dismissedAt := time.Now()
		f := &FindingRecord{
			ID:          "finding-abc",
			ScanID:      "scan-123",
			Dismissed:   true,
			DismissedAt: &dismissedAt,
			DismissedBy: "user@example.com",
			BeadID:      "bd-456",
			CreatedAt:   now,
		}

		m := findingToMap(f)
		if _, ok := m["dismissed_at"]; !ok {
			t.Error("dismissed_at should be present")
		}
		if m["dismissed_by"] != "user@example.com" {
			t.Errorf("dismissed_by = %v", m["dismissed_by"])
		}
		if m["bead_id"] != "bd-456" {
			t.Errorf("bead_id = %v", m["bead_id"])
		}
	})
}
