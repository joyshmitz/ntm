package robot

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

// =============================================================================
// synthesisStatus tests
// =============================================================================

func TestSynthesisStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      ensemble.EnsembleStatus
		assignments []ensemble.ModeAssignment
		want        string
	}{
		{
			name:   "synthesizing status",
			status: ensemble.EnsembleSynthesizing,
			want:   "running",
		},
		{
			name:   "complete status",
			status: ensemble.EnsembleComplete,
			want:   "complete",
		},
		{
			name:   "error status",
			status: ensemble.EnsembleError,
			want:   "error",
		},
		{
			name:        "no assignments returns not_started",
			status:      ensemble.EnsembleActive,
			assignments: nil,
			want:        "not_started",
		},
		{
			name:        "empty assignments returns not_started",
			status:      ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{},
			want:        "not_started",
		},
		{
			name:   "assignment with error returns error",
			status: ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{
				{ModeID: "a", Status: ensemble.AssignmentDone},
				{ModeID: "b", Status: ensemble.AssignmentError},
			},
			want: "error",
		},
		{
			name:   "pending assignment returns not_started",
			status: ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{
				{ModeID: "a", Status: ensemble.AssignmentDone},
				{ModeID: "b", Status: ensemble.AssignmentPending},
			},
			want: "not_started",
		},
		{
			name:   "active assignment returns not_started",
			status: ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{
				{ModeID: "a", Status: ensemble.AssignmentActive},
			},
			want: "not_started",
		},
		{
			name:   "injecting assignment returns not_started",
			status: ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{
				{ModeID: "a", Status: ensemble.AssignmentInjecting},
			},
			want: "not_started",
		},
		{
			name:   "all done returns ready",
			status: ensemble.EnsembleActive,
			assignments: []ensemble.ModeAssignment{
				{ModeID: "a", Status: ensemble.AssignmentDone},
				{ModeID: "b", Status: ensemble.AssignmentDone},
			},
			want: "ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := synthesisStatus(tt.status, tt.assignments)
			if got != tt.want {
				t.Errorf("synthesisStatus(%v, %v) = %q, want %q", tt.status, tt.assignments, got, tt.want)
			}
		})
	}
}

// =============================================================================
// mergeBudgetConfig tests
// =============================================================================

func TestMergeBudgetConfig_Override(t *testing.T) {
	t.Parallel()

	t.Run("override all fields", func(t *testing.T) {
		t.Parallel()
		base := ensemble.BudgetConfig{
			MaxTokensPerMode:       1000,
			MaxTotalTokens:         10000,
			SynthesisReserveTokens: 500,
			ContextReserveTokens:   200,
			TimeoutPerMode:         5 * time.Minute,
			TotalTimeout:           30 * time.Minute,
			MaxRetries:             2,
		}
		override := ensemble.BudgetConfig{
			MaxTokensPerMode:       2000,
			MaxTotalTokens:         20000,
			SynthesisReserveTokens: 1000,
			ContextReserveTokens:   400,
			TimeoutPerMode:         10 * time.Minute,
			TotalTimeout:           60 * time.Minute,
			MaxRetries:             5,
		}
		got := mergeBudgetConfig(base, override)
		if got.MaxTokensPerMode != 2000 {
			t.Errorf("MaxTokensPerMode = %d, want 2000", got.MaxTokensPerMode)
		}
		if got.MaxTotalTokens != 20000 {
			t.Errorf("MaxTotalTokens = %d, want 20000", got.MaxTotalTokens)
		}
		if got.SynthesisReserveTokens != 1000 {
			t.Errorf("SynthesisReserveTokens = %d, want 1000", got.SynthesisReserveTokens)
		}
		if got.ContextReserveTokens != 400 {
			t.Errorf("ContextReserveTokens = %d, want 400", got.ContextReserveTokens)
		}
		if got.TimeoutPerMode != 10*time.Minute {
			t.Errorf("TimeoutPerMode = %v, want 10m", got.TimeoutPerMode)
		}
		if got.TotalTimeout != 60*time.Minute {
			t.Errorf("TotalTimeout = %v, want 60m", got.TotalTimeout)
		}
		if got.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", got.MaxRetries)
		}
	})

	t.Run("zero overrides keep base values", func(t *testing.T) {
		t.Parallel()
		base := ensemble.BudgetConfig{
			MaxTokensPerMode:       1000,
			MaxTotalTokens:         10000,
			SynthesisReserveTokens: 500,
			ContextReserveTokens:   200,
			TimeoutPerMode:         5 * time.Minute,
			TotalTimeout:           30 * time.Minute,
			MaxRetries:             2,
		}
		override := ensemble.BudgetConfig{} // all zero
		got := mergeBudgetConfig(base, override)
		if got.MaxTokensPerMode != 1000 {
			t.Errorf("MaxTokensPerMode = %d, want 1000", got.MaxTokensPerMode)
		}
		if got.MaxTotalTokens != 10000 {
			t.Errorf("MaxTotalTokens = %d, want 10000", got.MaxTotalTokens)
		}
		if got.MaxRetries != 2 {
			t.Errorf("MaxRetries = %d, want 2", got.MaxRetries)
		}
	})

	t.Run("partial override", func(t *testing.T) {
		t.Parallel()
		base := ensemble.BudgetConfig{
			MaxTokensPerMode: 1000,
			MaxTotalTokens:   10000,
			MaxRetries:       2,
		}
		override := ensemble.BudgetConfig{
			MaxTotalTokens: 50000,
		}
		got := mergeBudgetConfig(base, override)
		if got.MaxTokensPerMode != 1000 {
			t.Errorf("MaxTokensPerMode = %d, want 1000 (unchanged)", got.MaxTokensPerMode)
		}
		if got.MaxTotalTokens != 50000 {
			t.Errorf("MaxTotalTokens = %d, want 50000 (overridden)", got.MaxTotalTokens)
		}
		if got.MaxRetries != 2 {
			t.Errorf("MaxRetries = %d, want 2 (unchanged)", got.MaxRetries)
		}
	})
}

// =============================================================================
// buildEnsembleHints tests
// =============================================================================

func TestBuildEnsembleHints_Actions(t *testing.T) {
	t.Parallel()

	t.Run("pending modes suggest wait", func(t *testing.T) {
		t.Parallel()
		output := EnsembleOutput{
			Summary: EnsembleSummary{
				TotalModes: 3,
				Completed:  1,
				Working:    1,
				Pending:    1,
			},
			Ensemble: EnsembleState{
				Modes: []EnsembleMode{},
			},
		}
		hints := buildEnsembleHints(output)
		if hints == nil {
			t.Fatal("expected non-nil hints")
		}
		if hints.Summary == "" {
			t.Error("expected non-empty summary")
		}
		found := false
		for _, action := range hints.SuggestedActions {
			if action.Action == "wait" {
				found = true
			}
		}
		if !found {
			t.Error("expected 'wait' suggested action when modes are pending")
		}
	})

	t.Run("all complete and ready suggests synthesize", func(t *testing.T) {
		t.Parallel()
		output := EnsembleOutput{
			Summary: EnsembleSummary{
				TotalModes: 3,
				Completed:  3,
				Working:    0,
				Pending:    0,
			},
			Ensemble: EnsembleState{
				Modes: []EnsembleMode{},
				Synthesis: EnsembleSynthesis{
					Status: "ready",
				},
			},
		}
		hints := buildEnsembleHints(output)
		if hints == nil {
			t.Fatal("expected non-nil hints")
		}
		found := false
		for _, action := range hints.SuggestedActions {
			if action.Action == "synthesize" {
				found = true
			}
		}
		if !found {
			t.Error("expected 'synthesize' suggested action when all modes complete")
		}
	})

	t.Run("error mode adds warning", func(t *testing.T) {
		t.Parallel()
		output := EnsembleOutput{
			Summary: EnsembleSummary{
				TotalModes: 2,
				Completed:  2,
			},
			Ensemble: EnsembleState{
				Modes: []EnsembleMode{
					{ID: "a", Status: string(ensemble.AssignmentDone)},
					{ID: "b", Status: string(ensemble.AssignmentError)},
				},
				Synthesis: EnsembleSynthesis{Status: "ready"},
			},
		}
		hints := buildEnsembleHints(output)
		if hints == nil {
			t.Fatal("expected non-nil hints")
		}
		found := false
		for _, w := range hints.Warnings {
			if w == "one or more modes reported errors; review pane output" {
				found = true
			}
		}
		if !found {
			t.Error("expected error warning in hints")
		}
	})

	t.Run("zero total returns nil when no content", func(t *testing.T) {
		t.Parallel()
		output := EnsembleOutput{
			Summary: EnsembleSummary{
				TotalModes: 0,
			},
			Ensemble: EnsembleState{
				Modes: []EnsembleMode{},
			},
		}
		hints := buildEnsembleHints(output)
		// With zero modes, still gets warnings, so hints may not be nil
		// but there should be no summary
		if hints != nil && hints.Summary != "" {
			t.Error("expected empty summary with zero modes")
		}
	})
}

// =============================================================================
// yesNo tests
// =============================================================================

func TestYesNo(t *testing.T) {
	t.Parallel()
	if yesNo(true) != "yes" {
		t.Errorf("yesNo(true) = %q, want %q", yesNo(true), "yes")
	}
	if yesNo(false) != "no" {
		t.Errorf("yesNo(false) = %q, want %q", yesNo(false), "no")
	}
}

// =============================================================================
// escapeMarkdownCell tests
// =============================================================================

func TestEscapeMarkdownCell(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"empty string", "", 0, ""},
		{"no escaping needed", "hello world", 0, "hello world"},
		{"pipe escaped", "foo|bar", 0, "foo\\|bar"},
		{"newline replaced", "foo\nbar", 0, "foo bar"},
		{"carriage return replaced", "foo\rbar", 0, "foo bar"},
		{"multiple pipes", "a|b|c", 0, "a\\|b\\|c"},
		{"mixed special chars", "a|b\nc\rd", 0, "a\\|b c d"},
		{"leading/trailing whitespace trimmed", "  hello  ", 0, "hello"},
		{"truncated to maxLen", "hello world this is long", 10, "hello w..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := escapeMarkdownCell(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("escapeMarkdownCell(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// =============================================================================
// dashboardCounts tests
// =============================================================================

func TestDashboardCounts(t *testing.T) {
	t.Parallel()

	t.Run("empty sessions", func(t *testing.T) {
		t.Parallel()
		total, user, counts := dashboardCounts(nil)
		if total != 0 {
			t.Errorf("total = %d, want 0", total)
		}
		if user != 0 {
			t.Errorf("user = %d, want 0", user)
		}
		if counts["claude"] != 0 || counts["codex"] != 0 {
			t.Errorf("expected zero counts, got %v", counts)
		}
	})

	t.Run("multiple sessions with mixed agents", func(t *testing.T) {
		t.Parallel()
		sessions := []SnapshotSession{
			{
				Name: "proj1",
				Agents: []SnapshotAgent{
					{Type: "claude"},
					{Type: "codex"},
					{Type: "user"},
				},
			},
			{
				Name: "proj2",
				Agents: []SnapshotAgent{
					{Type: "claude"},
					{Type: "gemini"},
				},
			},
		}
		total, user, counts := dashboardCounts(sessions)
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if user != 1 {
			t.Errorf("user = %d, want 1", user)
		}
		if counts["claude"] != 2 {
			t.Errorf("claude = %d, want 2", counts["claude"])
		}
		if counts["codex"] != 1 {
			t.Errorf("codex = %d, want 1", counts["codex"])
		}
		if counts["gemini"] != 1 {
			t.Errorf("gemini = %d, want 1", counts["gemini"])
		}
	})
}

// =============================================================================
// appendUnique tests
// =============================================================================

func TestAppendUnique(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		list  []string
		value string
		want  []string
	}{
		{"add to empty", nil, "a", []string{"a"}},
		{"add new value", []string{"a", "b"}, "c", []string{"a", "b", "c"}},
		{"skip duplicate", []string{"a", "b"}, "a", []string{"a", "b"}},
		{"skip duplicate middle", []string{"a", "b", "c"}, "b", []string{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := appendUnique(tt.list, tt.value)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// isUnknownJSONFlag tests
// =============================================================================

func TestIsUnknownJSONFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"empty string", "", false},
		{"no json mention", "something went wrong", false},
		{"json but no flag error", "json output enabled", false},
		{"unknown flag with json", "unknown flag: --json", true},
		{"flag provided but not defined with json", "flag provided but not defined: -json", true},
		{"case insensitive json", "Unknown flag: --JSON", true},
		{"case insensitive flag", "UNKNOWN FLAG: --json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isUnknownJSONFlag(tt.stderr)
			if got != tt.want {
				t.Errorf("isUnknownJSONFlag(%q) = %v, want %v", tt.stderr, got, tt.want)
			}
		})
	}
}

// =============================================================================
// removeFlag tests
// =============================================================================

func TestRemoveFlag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		flag string
		want []string
	}{
		{"remove existing", []string{"--json", "--verbose"}, "--json", []string{"--verbose"}},
		{"remove absent", []string{"--verbose"}, "--json", []string{"--verbose"}},
		{"remove from empty", nil, "--json", []string{}},
		{"remove multiple occurrences", []string{"--json", "a", "--json"}, "--json", []string{"a"}},
		{"remove only exact match", []string{"--json-output", "--json"}, "--json", []string{"--json-output"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := removeFlag(tt.args, tt.flag)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d; got=%v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// =============================================================================
// resolveEnsembleBudget tests (partial - only empty preset path)
// =============================================================================

func TestResolveEnsembleBudget_EmptyPreset(t *testing.T) {
	t.Parallel()
	// With empty preset, should return default budget
	budget := resolveEnsembleBudget("")
	defaults := ensemble.DefaultBudgetConfig()
	if budget.MaxTotalTokens != defaults.MaxTotalTokens {
		t.Errorf("MaxTotalTokens = %d, want %d", budget.MaxTotalTokens, defaults.MaxTotalTokens)
	}
	if budget.MaxTokensPerMode != defaults.MaxTokensPerMode {
		t.Errorf("MaxTokensPerMode = %d, want %d", budget.MaxTokensPerMode, defaults.MaxTokensPerMode)
	}
}

func TestResolveEnsembleBudget_WhitespacePreset(t *testing.T) {
	t.Parallel()
	budget := resolveEnsembleBudget("   ")
	defaults := ensemble.DefaultBudgetConfig()
	if budget.MaxTotalTokens != defaults.MaxTotalTokens {
		t.Errorf("MaxTotalTokens = %d, want %d", budget.MaxTotalTokens, defaults.MaxTotalTokens)
	}
}
