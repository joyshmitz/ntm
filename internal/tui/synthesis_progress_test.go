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
