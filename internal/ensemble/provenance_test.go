package ensemble

import "testing"

func TestGenerateFindingID_Deterministic(t *testing.T) {
	first := GenerateFindingID("mode-a", "Same finding")
	second := GenerateFindingID("mode-a", "Same finding")
	if first != second {
		t.Fatalf("GenerateFindingID should be deterministic, got %q and %q", first, second)
	}

	other := GenerateFindingID("mode-b", "Same finding")
	if other == first {
		t.Fatalf("GenerateFindingID should differ across modes, got %q for both", other)
	}
}

func TestMergeOutputsWithProvenance_RecordsMerge(t *testing.T) {
	tracker := NewProvenanceTracker("question", []string{"mode-a", "mode-b"})
	outputs := []ModeOutput{
		{
			ModeID:     "mode-a",
			Thesis:     "A",
			Confidence: 0.9,
			TopFindings: []Finding{
				{Finding: "Shared finding", Impact: ImpactHigh, Confidence: 0.9},
			},
		},
		{
			ModeID:     "mode-b",
			Thesis:     "B",
			Confidence: 0.8,
			TopFindings: []Finding{
				{Finding: "Shared finding", Impact: ImpactHigh, Confidence: 0.8},
			},
		},
	}

	merged := MergeOutputsWithProvenance(outputs, DefaultMergeConfig(), tracker)
	if len(merged.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(merged.Findings))
	}

	primaryID := GenerateFindingID("mode-a", "Shared finding")
	secondaryID := GenerateFindingID("mode-b", "Shared finding")

	primary, ok := tracker.GetChain(primaryID)
	if !ok {
		t.Fatalf("expected primary chain %s", primaryID)
	}
	if !containsStringProvenance(primary.MergedFrom, secondaryID) {
		t.Fatalf("expected primary chain to merge %s, got %+v", secondaryID, primary.MergedFrom)
	}

	mergedChain, ok := tracker.GetChain(secondaryID)
	if !ok {
		t.Fatalf("expected merged chain %s", secondaryID)
	}
	if mergedChain.MergedInto != primaryID {
		t.Fatalf("expected merged chain to point to %s, got %s", primaryID, mergedChain.MergedInto)
	}

	if merged.Findings[0].ProvenanceID != primaryID {
		t.Fatalf("expected merged provenance id %s, got %s", primaryID, merged.Findings[0].ProvenanceID)
	}
}

func TestSynthesizer_RecordsSynthesisCitations(t *testing.T) {
	tracker := NewProvenanceTracker("question", []string{"mode-a"})
	synth, err := NewSynthesizer(SynthesisConfig{Strategy: StrategyManual})
	if err != nil {
		t.Fatalf("NewSynthesizer error: %v", err)
	}

	outputs := []ModeOutput{
		{
			ModeID:     "mode-a",
			Thesis:     "A",
			Confidence: 0.9,
			TopFindings: []Finding{
				{Finding: "Cited finding", Impact: ImpactHigh, Confidence: 0.9},
			},
		},
	}

	_, err = synth.Synthesize(&SynthesisInput{
		Outputs:          outputs,
		OriginalQuestion: "question",
		Provenance:       tracker,
	})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	findingID := GenerateFindingID("mode-a", "Cited finding")
	chain, ok := tracker.GetChain(findingID)
	if !ok {
		t.Fatalf("expected chain %s", findingID)
	}
	if len(chain.SynthesisCitations) == 0 {
		t.Fatalf("expected synthesis citations to be recorded")
	}
}

func containsStringProvenance(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
