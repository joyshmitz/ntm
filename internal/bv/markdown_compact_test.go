package bv

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// renderCompactTriage — missing branches at 80.8%
// ---------------------------------------------------------------------------

func TestRenderCompactTriage_TopPicksWithReasons(t *testing.T) {
	t.Parallel()

	triage := &TriageResponse{
		Triage: TriageData{
			QuickRef: TriageQuickRef{
				ActionableCount: 3,
				BlockedCount:    1,
				InProgressCount: 2,
				TopPicks: []TriageTopPick{
					{ID: "bd-1", Title: "First task", Score: 0.95, Reasons: []string{"high priority", "unblocks others"}},
					{ID: "bd-2", Title: "Second task", Score: 0.80, Reasons: []string{"quick win"}},
				},
			},
		},
	}

	opts := DefaultMarkdownOptions()
	opts.Compact = true
	result := RenderTriageMarkdown(triage, opts)

	if !strings.Contains(result, "bd-1") {
		t.Errorf("expected first pick ID, got:\n%s", result)
	}
	if !strings.Contains(result, "high priority") {
		t.Errorf("expected reason in output, got:\n%s", result)
	}
}

func TestRenderCompactTriage_TopPicksNoReasons(t *testing.T) {
	t.Parallel()

	triage := &TriageResponse{
		Triage: TriageData{
			QuickRef: TriageQuickRef{
				ActionableCount: 1,
				TopPicks: []TriageTopPick{
					{ID: "bd-x", Title: "No reasons", Score: 0.5},
				},
			},
		},
	}

	opts := DefaultMarkdownOptions()
	opts.Compact = true
	result := RenderTriageMarkdown(triage, opts)

	if !strings.Contains(result, "bd-x") {
		t.Errorf("expected pick ID, got:\n%s", result)
	}
	// Should NOT contain pipe separator for reason when reasons is empty.
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "bd-x") && strings.Contains(line, "|") {
			t.Errorf("expected no reason separator, got: %s", line)
		}
	}
}

func TestRenderCompactTriage_QuickWins(t *testing.T) {
	t.Parallel()

	triage := &TriageResponse{
		Triage: TriageData{
			QuickRef: TriageQuickRef{
				ActionableCount: 5,
			},
			QuickWins: []TriageRecommendation{
				{ID: "bd-qw1", Title: "Quick one"},
				{ID: "bd-qw2", Title: "Quick two"},
			},
		},
	}

	opts := DefaultMarkdownOptions()
	opts.Compact = true
	result := RenderTriageMarkdown(triage, opts)

	if !strings.Contains(result, "Quick wins:") {
		t.Errorf("expected Quick wins section, got:\n%s", result)
	}
	if !strings.Contains(result, "bd-qw1") {
		t.Errorf("expected first quick win, got:\n%s", result)
	}
}

func TestRenderCompactTriage_Commands(t *testing.T) {
	t.Parallel()

	triage := &TriageResponse{
		Triage: TriageData{
			QuickRef: TriageQuickRef{
				ActionableCount: 1,
			},
			Commands: map[string]string{
				"next": "br update bd-1 --status in_progress",
				"list": "br list --status open",
			},
		},
	}

	opts := DefaultMarkdownOptions()
	opts.Compact = true
	result := RenderTriageMarkdown(triage, opts)

	if !strings.Contains(result, "Commands:") {
		t.Errorf("expected Commands section, got:\n%s", result)
	}
	if !strings.Contains(result, "br update bd-1") {
		t.Errorf("expected command text, got:\n%s", result)
	}
}

func TestRenderCompactTriage_TopPicksTruncated(t *testing.T) {
	t.Parallel()

	picks := make([]TriageTopPick, 10)
	for i := range picks {
		picks[i] = TriageTopPick{ID: "bd-p", Title: "Pick", Score: 0.5}
	}

	triage := &TriageResponse{
		Triage: TriageData{
			QuickRef: TriageQuickRef{
				ActionableCount: 10,
				TopPicks:        picks,
			},
		},
	}

	opts := DefaultMarkdownOptions()
	opts.Compact = true
	opts.MaxRecommendations = 3
	result := RenderTriageMarkdown(triage, opts)

	// Should show at most MaxRecommendations picks.
	count := strings.Count(result, "- `bd-p`")
	if count > 3 {
		t.Errorf("expected at most 3 picks, got %d in:\n%s", count, result)
	}
}
