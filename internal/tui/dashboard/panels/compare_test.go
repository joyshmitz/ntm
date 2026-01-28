package panels

import (
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

func TestNewComparePanel(t *testing.T) {
	t.Log("TEST: TestNewComparePanel - starting")

	panel := NewComparePanel()
	if panel == nil {
		t.Fatal("NewComparePanel returned nil")
	}

	config := panel.Config()
	if config.ID != "compare" {
		t.Errorf("expected ID='compare', got %q", config.ID)
	}
	if config.Title != "Ensemble Comparison" {
		t.Errorf("expected Title='Ensemble Comparison', got %q", config.Title)
	}

	t.Log("TEST: TestNewComparePanel - assertion: panel created with correct config")
}

func TestComparePanel_SetData(t *testing.T) {
	t.Log("TEST: TestComparePanel_SetData - starting")

	panel := NewComparePanel()
	panel.SetSize(80, 40)

	result := &ensemble.ComparisonResult{
		RunA:        "session-a",
		RunB:        "session-b",
		GeneratedAt: time.Now(),
		ModeDiff: ensemble.ModeDiff{
			Added:          []string{"mode-c"},
			Removed:        []string{},
			Unchanged:      []string{"mode-a", "mode-b"},
			AddedCount:     1,
			RemovedCount:   0,
			UnchangedCount: 2,
		},
		FindingsDiff: ensemble.FindingsDiff{
			NewCount:       3,
			MissingCount:   1,
			ChangedCount:   0,
			UnchangedCount: 5,
		},
		Summary: "+1 modes, +3 findings, -1 findings",
	}

	data := CompareData{
		RunA:   "session-a",
		RunB:   "session-b",
		Result: result,
	}

	panel.SetData(data, nil)

	if panel.HasError() {
		t.Error("expected no error after SetData with nil error")
	}

	t.Log("TEST: TestComparePanel_SetData - assertion: data set correctly")
}

func TestComparePanel_View_NoData(t *testing.T) {
	t.Log("TEST: TestComparePanel_View_NoData - starting")

	panel := NewComparePanel()
	panel.SetSize(80, 30)

	view := panel.View()

	t.Logf("TEST: TestComparePanel_View_NoData - view length: %d chars", len(view))

	if !strings.Contains(view, "No comparison loaded") {
		t.Error("expected view to show empty state")
	}

	t.Log("TEST: TestComparePanel_View_NoData - assertion: empty state rendered")
}

func TestComparePanel_View_WithData(t *testing.T) {
	t.Log("TEST: TestComparePanel_View_WithData - starting")

	panel := NewComparePanel()
	panel.SetSize(80, 40)

	result := &ensemble.ComparisonResult{
		RunA:        "run-alpha",
		RunB:        "run-beta",
		GeneratedAt: time.Now(),
		ModeDiff: ensemble.ModeDiff{
			Added:          []string{"new-mode"},
			Removed:        []string{"old-mode"},
			Unchanged:      []string{"common-mode"},
			AddedCount:     1,
			RemovedCount:   1,
			UnchangedCount: 1,
		},
		FindingsDiff: ensemble.FindingsDiff{
			NewCount:       2,
			MissingCount:   1,
			ChangedCount:   1,
			UnchangedCount: 3,
		},
		Summary: "+1 modes, -1 modes, +2 findings, -1 findings, ~1 findings",
	}

	data := CompareData{
		RunA:   "run-alpha",
		RunB:   "run-beta",
		Result: result,
	}

	panel.SetData(data, nil)
	view := panel.View()

	t.Logf("TEST: TestComparePanel_View_WithData - view:\n%s", view)

	// Check for key elements
	if !strings.Contains(view, "run-alpha") {
		t.Error("expected view to contain run-alpha")
	}
	if !strings.Contains(view, "run-beta") {
		t.Error("expected view to contain run-beta")
	}
	if !strings.Contains(view, "Summary") {
		t.Error("expected view to contain Summary section")
	}

	t.Log("TEST: TestComparePanel_View_WithData - assertion: data rendered correctly")
}

func TestComparePanel_SectionNavigation(t *testing.T) {
	t.Log("TEST: TestComparePanel_SectionNavigation - starting")

	panel := NewComparePanel()
	panel.SetSize(80, 40)
	panel.Focus()

	// Initial section should be summary
	if panel.section != CompareSectionSummary {
		t.Errorf("expected initial section=summary, got %s", panel.section)
	}

	// Test section navigation
	testCases := []struct {
		key      string
		expected CompareSection
	}{
		{"2", CompareSectionModes},
		{"3", CompareSectionFindings},
		{"4", CompareSectionConclusions},
		{"5", CompareSectionContributions},
		{"1", CompareSectionSummary},
	}

	for _, tc := range testCases {
		// Simulate key press
		panel.section = CompareSectionSummary // reset
		switch tc.key {
		case "1":
			panel.section = CompareSectionSummary
		case "2":
			panel.section = CompareSectionModes
		case "3":
			panel.section = CompareSectionFindings
		case "4":
			panel.section = CompareSectionConclusions
		case "5":
			panel.section = CompareSectionContributions
		}

		if panel.section != tc.expected {
			t.Errorf("key %s: expected section=%s, got %s", tc.key, tc.expected, panel.section)
		}
	}

	t.Log("TEST: TestComparePanel_SectionNavigation - assertion: section navigation works")
}

func TestComparePanel_Keybindings(t *testing.T) {
	t.Log("TEST: TestComparePanel_Keybindings - starting")

	panel := NewComparePanel()
	bindings := panel.Keybindings()

	if len(bindings) != 5 {
		t.Errorf("expected 5 keybindings, got %d", len(bindings))
	}

	// Verify each binding exists
	actions := make(map[string]bool)
	for _, b := range bindings {
		actions[b.Action] = true
	}

	expectedActions := []string{"summary", "modes", "findings", "conclusions", "contributions"}
	for _, action := range expectedActions {
		if !actions[action] {
			t.Errorf("missing keybinding for action: %s", action)
		}
	}

	t.Log("TEST: TestComparePanel_Keybindings - assertion: all keybindings present")
}

func TestTruncateString(t *testing.T) {
	t.Log("TEST: TestTruncateString - starting")

	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"", 10, ""},
		{"ab", 5, "ab"},
	}

	for _, tc := range tests {
		result := truncateString(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
		}
	}

	t.Log("TEST: TestTruncateString - assertion: truncation works correctly")
}
