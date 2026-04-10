package tui

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

func TestSynthesisProgressViewWaiting(t *testing.T) {
	progress := NewSynthesisProgress(60)
	progress.SetData(SynthesisProgressData{
		Phase:   SynthesisWaiting,
		Ready:   1,
		Pending: 2,
		Total:   3,
	})

	view := progress.View()
	if !strings.Contains(view, "Ready: 1/3") {
		t.Errorf("expected ready count in view, got %q", view)
	}
	if !strings.Contains(view, "Start synthesis") {
		t.Errorf("expected disabled start button, got %q", view)
	}
}

func TestSynthesisProgressViewCollectingIncludesTierAndTokens(t *testing.T) {
	progress := NewSynthesisProgress(80)
	progress.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Ready: 1,
		Total: 2,
		Lines: []SynthesisProgressLine{
			{
				Pane:     "proj__cc_1",
				ModeCode: "A1",
				Tier:     ensemble.TierCore,
				Tokens:   123,
				Status:   "done",
			},
		},
	})

	view := progress.View()
	if !strings.Contains(view, "A1") {
		t.Errorf("expected mode code in view, got %q", view)
	}
	if !strings.Contains(view, "CORE") {
		t.Errorf("expected tier chip in view, got %q", view)
	}
	if !strings.Contains(view, "123tok") {
		t.Errorf("expected token count in view, got %q", view)
	}
}

func TestSynthesisProgressViewCompleteShowsResultPath(t *testing.T) {
	progress := NewSynthesisProgress(60)
	progress.SetData(SynthesisProgressData{
		Phase:      SynthesisComplete,
		ResultPath: "/tmp/synthesis.json",
	})

	view := progress.View()
	if !strings.Contains(view, "Synthesis complete") {
		t.Errorf("expected completion label, got %q", view)
	}
	if !strings.Contains(view, "/tmp/synthesis.json") {
		t.Errorf("expected result path in view, got %q", view)
	}
}

func TestSynthesisPhaseString(t *testing.T) {

	tests := []struct {
		phase SynthesisPhase
		want  string
	}{
		{SynthesisWaiting, "waiting"},
		{SynthesisCollecting, "collecting"},
		{SynthesisSynthesizing, "synthesizing"},
		{SynthesisComplete, "complete"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.phase.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewSynthesisProgress(t *testing.T) {

	sp := NewSynthesisProgress(80)
	if sp == nil {
		t.Fatal("NewSynthesisProgress returned nil")
	}
	if sp.Width != 80 {
		t.Errorf("Width = %d, want 80", sp.Width)
	}
	if sp.lastPhase != SynthesisWaiting {
		t.Errorf("lastPhase = %q, want %q", sp.lastPhase, SynthesisWaiting)
	}
}

func TestNewSynthesisProgress_SmallWidth(t *testing.T) {

	sp := NewSynthesisProgress(10)
	if sp == nil {
		t.Fatal("NewSynthesisProgress returned nil")
	}
	// Bar width clamped to minimum 12
	if sp.Width != 10 {
		t.Errorf("Width = %d, want 10", sp.Width)
	}
}

func TestSynthesisProgress_SetData(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:   SynthesisCollecting,
		Ready:   2,
		Total:   5,
		Pending: 3,
	})

	if sp.data.Phase != SynthesisCollecting {
		t.Errorf("data.Phase = %q, want %q", sp.data.Phase, SynthesisCollecting)
	}
	if sp.data.Ready != 2 {
		t.Errorf("data.Ready = %d, want 2", sp.data.Ready)
	}
	if sp.data.Total != 5 {
		t.Errorf("data.Total = %d, want 5", sp.data.Total)
	}
}

func TestSynthesisProgress_SetData_EmptyPhaseDefaultsToWaiting(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase: "",
		Ready: 1,
		Total: 3,
	})

	if sp.data.Phase != SynthesisWaiting {
		t.Errorf("empty phase should default to waiting, got %q", sp.data.Phase)
	}
}

func TestSynthesisProgress_CurrentProgress_ExplicitProgress(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:    SynthesisSynthesizing,
		Progress: 0.75,
	})

	got := sp.currentProgress()
	if got != 0.75 {
		t.Errorf("currentProgress() = %v, want 0.75", got)
	}
}

func TestSynthesisProgress_CurrentProgress_ComputedFromReadyTotal(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Ready: 3,
		Total: 6,
	})

	got := sp.currentProgress()
	if got != 0.5 {
		t.Errorf("currentProgress() = %v, want 0.5", got)
	}
}

func TestSynthesisProgress_CurrentProgress_ZeroTotal(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisWaiting,
		Total: 0,
	})

	got := sp.currentProgress()
	if got != 0 {
		t.Errorf("currentProgress() = %v, want 0", got)
	}
}

func TestSynthesisProgress_CurrentProgress_ClampAboveOne(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:    SynthesisSynthesizing,
		Progress: 1.5,
	})

	got := sp.currentProgress()
	if got != 1.0 {
		t.Errorf("currentProgress() = %v, want 1.0 (clamped)", got)
	}
}

func TestSynthesisProgress_TotalTokens_FromInputTokens(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:       SynthesisSynthesizing,
		InputTokens: 5000,
	})

	got := sp.totalTokens()
	if got != 5000 {
		t.Errorf("totalTokens() = %d, want 5000", got)
	}
}

func TestSynthesisProgress_TotalTokens_FromLines(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Lines: []SynthesisProgressLine{
			{Tokens: 100},
			{Tokens: 200},
			{Tokens: 300},
		},
	})

	got := sp.totalTokens()
	if got != 600 {
		t.Errorf("totalTokens() = %d, want 600", got)
	}
}

func TestSynthesisProgress_TotalTokens_InputTokensTakesPrecedence(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:       SynthesisCollecting,
		InputTokens: 999,
		Lines: []SynthesisProgressLine{
			{Tokens: 100},
			{Tokens: 200},
		},
	})

	got := sp.totalTokens()
	if got != 999 {
		t.Errorf("totalTokens() = %d, want 999 (InputTokens takes precedence)", got)
	}
}

func TestSynthesisProgress_ViewWaiting_NoTotal(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:   SynthesisWaiting,
		Ready:   0,
		Pending: 3,
		Total:   0,
	})

	view := sp.View()
	if !strings.Contains(view, "Ready: 0") {
		t.Errorf("expected ready count in view, got %q", view)
	}
	if !strings.Contains(view, "Pending: 3") {
		t.Errorf("expected pending count in view, got %q", view)
	}
}

func TestSynthesisProgress_ViewCollecting_NoLines(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Ready: 0,
		Total: 3,
		Lines: nil,
	})

	view := sp.View()
	if !strings.Contains(view, "Collecting") {
		t.Errorf("expected collecting header, got %q", view)
	}
	if !strings.Contains(view, "Awaiting window outputs") {
		t.Errorf("expected awaiting message, got %q", view)
	}
}

func TestSynthesisProgress_ViewCollecting_MultipleLines(t *testing.T) {

	sp := NewSynthesisProgress(80)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Ready: 2,
		Total: 3,
		Lines: []SynthesisProgressLine{
			{Pane: "proj__cc_1", ModeCode: "A1", Tier: ensemble.TierCore, Tokens: 100, Status: "done"},
			{Pane: "proj__cod_1", ModeCode: "B2", Tier: ensemble.TierAdvanced, Tokens: 200, Status: "active"},
			{Pane: "proj__gmi_1", ModeCode: "C3", Tier: ensemble.TierExperimental, Tokens: 0, Status: "pending"},
		},
	})

	view := sp.View()
	if !strings.Contains(view, "A1") {
		t.Errorf("expected mode code A1, got %q", view)
	}
	if !strings.Contains(view, "CORE") {
		t.Errorf("expected CORE tier, got %q", view)
	}
	if !strings.Contains(view, "ADV") {
		t.Errorf("expected ADV tier, got %q", view)
	}
	if !strings.Contains(view, "EXP") {
		t.Errorf("expected EXP tier, got %q", view)
	}
	if !strings.Contains(view, "100tok") {
		t.Errorf("expected 100tok, got %q", view)
	}
	if !strings.Contains(view, "200tok") {
		t.Errorf("expected 200tok, got %q", view)
	}
}

func TestSynthesisProgress_ViewSynthesizing(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:           SynthesisSynthesizing,
		Strategy:        "consensus",
		SynthesizerMode: "opus",
		InputTokens:     4200,
		Progress:        0.5,
	})

	view := sp.View()
	if !strings.Contains(view, "Synthesizing") {
		t.Errorf("expected Synthesizing label, got %q", view)
	}
	if !strings.Contains(view, "consensus") {
		t.Errorf("expected strategy in view, got %q", view)
	}
	if !strings.Contains(view, "opus") {
		t.Errorf("expected synthesizer mode in view, got %q", view)
	}
	if !strings.Contains(view, "4200") {
		t.Errorf("expected input tokens in view, got %q", view)
	}
}

func TestSynthesisProgress_ViewSynthesizing_EmptyStrategy(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:    SynthesisSynthesizing,
		Strategy: "",
	})

	view := sp.View()
	if !strings.Contains(view, "—") {
		t.Errorf("expected dash placeholder for empty strategy, got %q", view)
	}
}

func TestSynthesisProgress_ViewComplete_NoResultPath(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:      SynthesisComplete,
		ResultPath: "",
	})

	view := sp.View()
	if !strings.Contains(view, "Synthesis complete") {
		t.Errorf("expected complete label, got %q", view)
	}
	if !strings.Contains(view, "See ensemble output") {
		t.Errorf("expected fallback message for empty result path, got %q", view)
	}
}

func TestSynthesisProgress_ViewComplete_DoneBadge(t *testing.T) {

	sp := NewSynthesisProgress(60)
	sp.SetData(SynthesisProgressData{
		Phase:      SynthesisComplete,
		ResultPath: "/out/result.json",
	})

	view := sp.View()
	if !strings.Contains(view, "DONE") {
		t.Errorf("expected DONE badge in view, got %q", view)
	}
}

func TestSynthesisProgress_DefaultWidth(t *testing.T) {

	sp := NewSynthesisProgress(0)
	sp.Width = 0
	sp.SetData(SynthesisProgressData{Phase: SynthesisWaiting})

	view := sp.View()
	if view == "" {
		t.Error("expected non-empty view for zero width (should use default 60)")
	}
}

func TestSynthesisProgress_Init(t *testing.T) {

	sp := NewSynthesisProgress(60)
	// Init should not panic
	_ = sp.Init()
}

func TestSynthesisProgress_Update_WithMsg(t *testing.T) {

	sp := NewSynthesisProgress(60)
	model, cmd := sp.Update(SynthesisProgressMsg{
		Data: SynthesisProgressData{
			Phase:   SynthesisCollecting,
			Ready:   1,
			Total:   3,
			Pending: 2,
		},
	})

	updated := model.(*SynthesisProgress)
	if updated.data.Phase != SynthesisCollecting {
		t.Errorf("phase after update = %q, want %q", updated.data.Phase, SynthesisCollecting)
	}
	// cmd may or may not be nil depending on progress bar
	_ = cmd
}

func TestShortenPane(t *testing.T) {

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with double underscore", "session__cc_1", "cc_1"},
		{"multiple separators", "long__path__final", "final"},
		{"no separator", "plain_name", "plain_name"},
		{"empty", "", ""},
		{"separator at end", "name__", "name__"},
		{"just separator", "__", "__"},
		{"one char after", "__x", "x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shortenPane(tc.input)
			if got != tc.want {
				t.Errorf("shortenPane(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestClampFloat(t *testing.T) {

	tests := []struct {
		name      string
		v, lo, hi float64
		want      float64
	}{
		{"in range", 0.5, 0, 1, 0.5},
		{"below min", -0.5, 0, 1, 0},
		{"above max", 1.5, 0, 1, 1},
		{"at min", 0, 0, 1, 0},
		{"at max", 1, 0, 1, 1},
		{"narrow range", 5, 3, 3, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampFloat(tc.v, tc.lo, tc.hi)
			if got != tc.want {
				t.Errorf("clampFloat(%v, %v, %v) = %v, want %v", tc.v, tc.lo, tc.hi, got, tc.want)
			}
		})
	}
}

func TestClampInt(t *testing.T) {

	tests := []struct {
		name      string
		v, lo, hi int
		want      int
	}{
		{"in range", 5, 0, 10, 5},
		{"below min", -1, 0, 10, 0},
		{"above max", 15, 0, 10, 10},
		{"at min", 0, 0, 10, 0},
		{"at max", 10, 0, 10, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := clampInt(tc.v, tc.lo, tc.hi)
			if got != tc.want {
				t.Errorf("clampInt(%d, %d, %d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
			}
		})
	}
}

func TestRenderTierChip(t *testing.T) {

	tests := []struct {
		name string
		tier ensemble.ModeTier
		want string
	}{
		{"core", ensemble.TierCore, "CORE"},
		{"advanced", ensemble.TierAdvanced, "ADV"},
		{"experimental", ensemble.TierExperimental, "EXP"},
		{"empty", ensemble.ModeTier(""), ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderTierChip(tc.tier)
			if tc.want == "" {
				if got != "" {
					t.Errorf("renderTierChip(%q) = %q, want empty", tc.tier, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("renderTierChip(%q) = %q, does not contain %q", tc.tier, got, tc.want)
			}
		})
	}
}

func TestSynthesisProgressLine_RenderLine_EmptyModeCode(t *testing.T) {

	sp := NewSynthesisProgress(80)
	sp.SetData(SynthesisProgressData{
		Phase: SynthesisCollecting,
		Ready: 1,
		Total: 1,
		Lines: []SynthesisProgressLine{
			{Pane: "proj__cc_1", ModeCode: "", Tier: ensemble.TierCore, Tokens: 50, Status: "done"},
		},
	})

	view := sp.View()
	// Empty mode code should show dash placeholder
	if !strings.Contains(view, "—") {
		t.Errorf("expected dash for empty mode code, got %q", view)
	}
}

func TestSynthesisProgressLine_StatusColors(t *testing.T) {

	statuses := []string{"done", "active", "error", "pending", ""}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			sp := NewSynthesisProgress(80)
			sp.SetData(SynthesisProgressData{
				Phase: SynthesisCollecting,
				Ready: 1,
				Total: 1,
				Lines: []SynthesisProgressLine{
					{Pane: "proj__cc_1", ModeCode: "A1", Tier: ensemble.TierCore, Tokens: 50, Status: status},
				},
			})
			view := sp.View()
			if view == "" {
				t.Error("expected non-empty view")
			}
		})
	}
}

func TestSynthesisProgress_PhaseTransitions(t *testing.T) {

	sp := NewSynthesisProgress(60)

	phases := []SynthesisPhase{
		SynthesisWaiting,
		SynthesisCollecting,
		SynthesisSynthesizing,
		SynthesisComplete,
	}

	for i, phase := range phases {
		sp.SetData(SynthesisProgressData{Phase: phase})
		if sp.data.Phase != phase {
			t.Errorf("step %d: phase = %q, want %q", i, sp.data.Phase, phase)
		}

		view := sp.View()
		if view == "" {
			t.Errorf("step %d: empty view for phase %q", i, phase)
		}
	}
}
