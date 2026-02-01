package ensemble

import (
	"strings"
	"testing"
	"time"
)

func TestCalculateRedundancy_ExactMatch(t *testing.T) {
	// Two modes with identical findings should have similarity 1.0
	outputs := []ModeOutput{
		{
			ModeID: "mode-a",
			Thesis: "Test thesis A",
			TopFindings: []Finding{
				{Finding: "Finding one", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Finding two", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
		{
			ModeID: "mode-b",
			Thesis: "Test thesis B",
			TopFindings: []Finding{
				{Finding: "Finding one", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Finding two", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	if len(analysis.PairwiseScores) != 1 {
		t.Fatalf("expected 1 pairwise score, got %d", len(analysis.PairwiseScores))
	}

	pair := analysis.PairwiseScores[0]
	if pair.Similarity < 0.99 {
		t.Errorf("expected similarity ~1.0 for exact match, got %.2f", pair.Similarity)
	}
	if pair.UniqueToA != 0 || pair.UniqueToB != 0 {
		t.Errorf("expected 0 unique findings for exact match, got uniqueA=%d, uniqueB=%d",
			pair.UniqueToA, pair.UniqueToB)
	}
	if pair.SharedFindings != 2 {
		t.Errorf("expected 2 shared findings, got %d", pair.SharedFindings)
	}
}

func TestCalculateRedundancy_DisjointFindings(t *testing.T) {
	// Two modes with completely different findings should have similarity 0.0
	outputs := []ModeOutput{
		{
			ModeID: "mode-a",
			Thesis: "Test thesis A",
			TopFindings: []Finding{
				{Finding: "Alpha finding", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Beta finding", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
		{
			ModeID: "mode-b",
			Thesis: "Test thesis B",
			TopFindings: []Finding{
				{Finding: "Gamma finding", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Delta finding", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	pair := analysis.PairwiseScores[0]
	if pair.Similarity > 0.01 {
		t.Errorf("expected similarity ~0.0 for disjoint findings, got %.2f", pair.Similarity)
	}
	if pair.SharedFindings != 0 {
		t.Errorf("expected 0 shared findings, got %d", pair.SharedFindings)
	}
	if pair.UniqueToA != 2 {
		t.Errorf("expected 2 unique to A, got %d", pair.UniqueToA)
	}
	if pair.UniqueToB != 2 {
		t.Errorf("expected 2 unique to B, got %d", pair.UniqueToB)
	}
}

func TestCalculateRedundancy_PartialOverlap(t *testing.T) {
	// Two modes with 1 shared, 1 unique each → Dice = 2*1/(2+2) = 0.5
	outputs := []ModeOutput{
		{
			ModeID: "mode-a",
			Thesis: "Test thesis A",
			TopFindings: []Finding{
				{Finding: "Shared finding", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Unique to A", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
		{
			ModeID: "mode-b",
			Thesis: "Test thesis B",
			TopFindings: []Finding{
				{Finding: "Shared finding", Impact: ImpactHigh, Confidence: 0.9},
				{Finding: "Unique to B", Impact: ImpactMedium, Confidence: 0.8},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	pair := analysis.PairwiseScores[0]
	// Dice for findings: 2*1/(2+2) = 0.5
	// No recommendations in either mode, so we use findings similarity directly.
	expectedFindingsSim := 0.5

	if pair.Similarity < expectedFindingsSim-0.05 || pair.Similarity > expectedFindingsSim+0.05 {
		t.Errorf("expected similarity ~%.2f for partial overlap, got %.2f",
			expectedFindingsSim, pair.Similarity)
	}
	if pair.SharedFindings != 1 {
		t.Errorf("expected 1 shared finding, got %d", pair.SharedFindings)
	}
	if pair.UniqueToA != 1 {
		t.Errorf("expected 1 unique to A, got %d", pair.UniqueToA)
	}
	if pair.UniqueToB != 1 {
		t.Errorf("expected 1 unique to B, got %d", pair.UniqueToB)
	}
}

func TestCalculateRedundancy_RecommendationWeight(t *testing.T) {
	// Test that recommendations contribute to similarity
	outputs := []ModeOutput{
		{
			ModeID: "mode-a",
			Thesis: "Test thesis A",
			TopFindings: []Finding{
				{Finding: "Unique finding A", Impact: ImpactHigh, Confidence: 0.9},
			},
			Recommendations: []Recommendation{
				{Recommendation: "Shared recommendation", Priority: ImpactHigh},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
		{
			ModeID: "mode-b",
			Thesis: "Test thesis B",
			TopFindings: []Finding{
				{Finding: "Unique finding B", Impact: ImpactHigh, Confidence: 0.9},
			},
			Recommendations: []Recommendation{
				{Recommendation: "Shared recommendation", Priority: ImpactHigh},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	pair := analysis.PairwiseScores[0]
	// Findings: disjoint → Jaccard = 0
	// Recommendations: identical → Jaccard = 1.0
	// Weighted: 0.8 * 0 + 0.2 * 1.0 = 0.2
	expectedSim := 0.2

	if pair.Similarity < expectedSim-0.05 || pair.Similarity > expectedSim+0.05 {
		t.Errorf("expected similarity ~%.2f (recommendation weight), got %.2f",
			expectedSim, pair.Similarity)
	}
}

func TestCalculateRedundancy_SingleMode(t *testing.T) {
	outputs := []ModeOutput{
		{
			ModeID: "mode-a",
			Thesis: "Single mode",
			TopFindings: []Finding{
				{Finding: "Finding", Impact: ImpactHigh, Confidence: 0.9},
			},
			Confidence:  0.85,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	if analysis.OverallScore != 0 {
		t.Errorf("expected overall score 0 for single mode, got %.2f", analysis.OverallScore)
	}
	if len(analysis.PairwiseScores) != 0 {
		t.Errorf("expected no pairwise scores for single mode, got %d", len(analysis.PairwiseScores))
	}
}

func TestCalculateRedundancy_ThreeModes(t *testing.T) {
	outputs := []ModeOutput{
		{
			ModeID:      "mode-a",
			Thesis:      "A",
			TopFindings: []Finding{{Finding: "F1", Impact: ImpactHigh, Confidence: 0.9}},
			Confidence:  0.8,
			GeneratedAt: time.Now(),
		},
		{
			ModeID:      "mode-b",
			Thesis:      "B",
			TopFindings: []Finding{{Finding: "F1", Impact: ImpactHigh, Confidence: 0.9}},
			Confidence:  0.8,
			GeneratedAt: time.Now(),
		},
		{
			ModeID:      "mode-c",
			Thesis:      "C",
			TopFindings: []Finding{{Finding: "F2", Impact: ImpactHigh, Confidence: 0.9}},
			Confidence:  0.8,
			GeneratedAt: time.Now(),
		},
	}

	analysis := CalculateRedundancy(outputs)

	if analysis == nil {
		t.Fatal("expected non-nil analysis")
	}

	// Should have 3 pairs: A-B, A-C, B-C
	if len(analysis.PairwiseScores) != 3 {
		t.Errorf("expected 3 pairwise scores, got %d", len(analysis.PairwiseScores))
	}

	// A-B should be high similarity, A-C and B-C should be low
	var highSim, lowSim int
	for _, pair := range analysis.PairwiseScores {
		if pair.Similarity > 0.5 {
			highSim++
		} else {
			lowSim++
		}
	}
	if highSim != 1 {
		t.Errorf("expected 1 high similarity pair, got %d", highSim)
	}
	if lowSim != 2 {
		t.Errorf("expected 2 low similarity pairs, got %d", lowSim)
	}
}

func TestRedundancyAnalysis_SuggestReplacements(t *testing.T) {
	analysis := &RedundancyAnalysis{
		PairwiseScores: []PairSimilarity{
			{ModeA: "mode-a", ModeB: "mode-b", Similarity: 0.9},
		},
	}

	catalog, err := NewModeCatalog([]ReasoningMode{
		{ID: "mode-a", Name: "A", Category: CategoryFormal, Tier: TierCore, ShortDesc: "A"},
		{ID: "mode-b", Name: "B", Category: CategoryFormal, Tier: TierCore, ShortDesc: "B"},
		{ID: "alt-1", Name: "Alt", Category: CategoryUncertainty, Tier: TierCore, ShortDesc: "Alt"},
	}, "test")
	if err != nil {
		t.Fatalf("NewModeCatalog error: %v", err)
	}

	suggestions := analysis.SuggestReplacements(catalog)
	if len(suggestions) == 0 {
		t.Fatal("expected suggestions")
	}

	joined := strings.Join(suggestions, "\n")
	if !strings.Contains(joined, "mode-b") || !strings.Contains(joined, "alt-1") {
		t.Fatalf("unexpected suggestions: %v", suggestions)
	}
}

func TestGetHighRedundancyPairs(t *testing.T) {
	analysis := &RedundancyAnalysis{
		OverallScore: 0.4,
		PairwiseScores: []PairSimilarity{
			{ModeA: "a", ModeB: "b", Similarity: 0.8},
			{ModeA: "a", ModeB: "c", Similarity: 0.3},
			{ModeA: "b", ModeB: "c", Similarity: 0.6},
		},
	}

	// Threshold 0.5
	high := analysis.GetHighRedundancyPairs(0.5)
	if len(high) != 2 {
		t.Errorf("expected 2 pairs above 0.5, got %d", len(high))
	}

	// Threshold 0.7
	high = analysis.GetHighRedundancyPairs(0.7)
	if len(high) != 1 {
		t.Errorf("expected 1 pair above 0.7, got %d", len(high))
	}

	// Threshold 0.9
	high = analysis.GetHighRedundancyPairs(0.9)
	if len(high) != 0 {
		t.Errorf("expected 0 pairs above 0.9, got %d", len(high))
	}
}

func TestGetHighRedundancyPairs_NilAnalysis(t *testing.T) {
	var analysis *RedundancyAnalysis
	high := analysis.GetHighRedundancyPairs(0.5)
	if high != nil {
		t.Error("expected nil for nil analysis")
	}
}

func TestRedundancyAnalysis_Render(t *testing.T) {
	analysis := &RedundancyAnalysis{
		OverallScore: 0.34,
		PairwiseScores: []PairSimilarity{
			{ModeA: "F1", ModeB: "E2", Similarity: 0.23, SharedFindings: 1, UniqueToA: 2, UniqueToB: 3},
			{ModeA: "F1", ModeB: "K1", Similarity: 0.67, SharedFindings: 5, UniqueToA: 1, UniqueToB: 1},
		},
		Recommendations: []string{"Consider replacing K1 with different mode"},
	}

	output := analysis.Render()

	// Check key elements are present
	if !simContains(output, "Overall Score: 0.34") {
		t.Error("expected overall score in output")
	}
	if !simContains(output, "acceptable") {
		t.Error("expected interpretation in output")
	}
	if !simContains(output, "F1 ↔ E2") {
		t.Error("expected first pair in output")
	}
	if !simContains(output, "F1 ↔ K1") {
		t.Error("expected second pair in output")
	}
	if !simContains(output, "moderate") {
		t.Error("expected moderate classification for 0.67 similarity")
	}
	if !simContains(output, "Recommendation:") {
		t.Error("expected recommendation in output")
	}
}

func TestRedundancyAnalysis_Render_Nil(t *testing.T) {
	var analysis *RedundancyAnalysis
	output := analysis.Render()
	if output != "No redundancy data available" {
		t.Errorf("unexpected output for nil analysis: %s", output)
	}
}

func TestNormalizeFinding(t *testing.T) {
	// Finding with evidence pointer
	f1 := Finding{Finding: "Test Finding", EvidencePointer: "file.go:42"}
	key1 := normalizeFinding(f1)
	if key1 == "" {
		t.Error("expected non-empty key")
	}

	// Same finding without evidence should produce different key
	f2 := Finding{Finding: "Test Finding"}
	key2 := normalizeFinding(f2)
	if key1 == key2 {
		t.Error("expected different keys for findings with/without evidence")
	}

	// Same finding with same evidence should produce same key
	f3 := Finding{Finding: "Test Finding", EvidencePointer: "file.go:42"}
	key3 := normalizeFinding(f3)
	if key1 != key3 {
		t.Error("expected same key for identical findings")
	}
}

func TestJaccardSimilarityFromSets(t *testing.T) {
	tests := []struct {
		name     string
		a        map[string]struct{}
		b        map[string]struct{}
		expected float64
	}{
		{
			name:     "both empty",
			a:        map[string]struct{}{},
			b:        map[string]struct{}{},
			expected: 0.0, // No findings = no similarity to measure
		},
		{
			name:     "one empty",
			a:        map[string]struct{}{"x": {}},
			b:        map[string]struct{}{},
			expected: 0.0,
		},
		{
			name:     "identical",
			a:        map[string]struct{}{"x": {}, "y": {}},
			b:        map[string]struct{}{"x": {}, "y": {}},
			expected: 1.0,
		},
		{
			name:     "disjoint",
			a:        map[string]struct{}{"a": {}, "b": {}},
			b:        map[string]struct{}{"c": {}, "d": {}},
			expected: 0.0,
		},
		{
			name:     "partial overlap",
			a:        map[string]struct{}{"a": {}, "b": {}},
			b:        map[string]struct{}{"b": {}, "c": {}},
			expected: 1.0 / 3.0, // intersection=1, union=3
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := jaccardSimilarityFromSets(tc.a, tc.b)
			if result < tc.expected-0.01 || result > tc.expected+0.01 {
				t.Errorf("expected %.3f, got %.3f", tc.expected, result)
			}
		})
	}
}

func TestCountSetOverlap(t *testing.T) {
	a := map[string]struct{}{"x": {}, "y": {}, "z": {}}
	b := map[string]struct{}{"y": {}, "z": {}, "w": {}}

	shared, uniqueA, uniqueB := countSetOverlap(a, b)

	if shared != 2 {
		t.Errorf("expected 2 shared, got %d", shared)
	}
	if uniqueA != 1 {
		t.Errorf("expected 1 unique to A, got %d", uniqueA)
	}
	if uniqueB != 1 {
		t.Errorf("expected 1 unique to B, got %d", uniqueB)
	}
}

func TestInterpretScore(t *testing.T) {
	tests := []struct {
		score    float64
		contains string
	}{
		{0.8, "high redundancy"},
		{0.6, "moderate"},
		{0.35, "acceptable"},
		{0.1, "low redundancy"},
	}

	for _, tc := range tests {
		result := interpretScore(tc.score)
		if !simContains(result, tc.contains) {
			t.Errorf("score %.2f: expected to contain %q, got %q", tc.score, tc.contains, result)
		}
	}
}

func TestClassifySimilarity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		score float64
		want  string
	}{
		{0.9, "HIGH"},
		{0.7, "HIGH"},
		{0.5, "moderate"},
		{0.4, "moderate"},
		{0.3, "low"},
		{0.0, "low"},
	}

	for _, tc := range tests {
		got := classifySimilarity(tc.score)
		if got != tc.want {
			t.Errorf("classifySimilarity(%.1f) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestDiversityNote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		score float64
		want  string
	}{
		{0.8, "overlapping insights"},
		{0.5, "overlapping insights"},
		{0.49, "good diversity"},
		{0.0, "good diversity"},
	}

	for _, tc := range tests {
		got := diversityNote(tc.score)
		if got != tc.want {
			t.Errorf("diversityNote(%.2f) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestJaccardSimilarityForNonEmpty(t *testing.T) {
	t.Parallel()

	t.Run("both empty", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarityForNonEmpty(nil, nil)
		if got != 0.0 {
			t.Errorf("both empty = %f, want 0", got)
		}
	})

	t.Run("one empty", func(t *testing.T) {
		t.Parallel()
		a := map[string]struct{}{"x": {}}
		got := jaccardSimilarityForNonEmpty(a, nil)
		if got != 0.0 {
			t.Errorf("one empty = %f, want 0", got)
		}
	})

	t.Run("identical sets", func(t *testing.T) {
		t.Parallel()
		a := map[string]struct{}{"x": {}, "y": {}}
		b := map[string]struct{}{"x": {}, "y": {}}
		got := jaccardSimilarityForNonEmpty(a, b)
		if got != 1.0 {
			t.Errorf("identical = %f, want 1.0", got)
		}
	})

	t.Run("disjoint sets", func(t *testing.T) {
		t.Parallel()
		a := map[string]struct{}{"x": {}}
		b := map[string]struct{}{"y": {}}
		got := jaccardSimilarityForNonEmpty(a, b)
		if got != 0.0 {
			t.Errorf("disjoint = %f, want 0.0", got)
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		t.Parallel()
		a := map[string]struct{}{"x": {}, "y": {}}
		b := map[string]struct{}{"y": {}, "z": {}}
		got := jaccardSimilarityForNonEmpty(a, b)
		// intersection=1, union=3
		want := 1.0 / 3.0
		if got < want-0.001 || got > want+0.001 {
			t.Errorf("partial = %f, want %f", got, want)
		}
	})
}

func TestGenerateRedundancyRecommendations(t *testing.T) {
	t.Parallel()

	t.Run("high redundancy pairs", func(t *testing.T) {
		t.Parallel()
		analysis := &RedundancyAnalysis{
			OverallScore: 0.6,
			PairwiseScores: []PairSimilarity{
				{ModeA: "mode-a", ModeB: "mode-b", Similarity: 0.8, SharedFindings: 5},
			},
		}
		recs := generateRedundancyRecommendations(analysis, 0.5)
		if len(recs) == 0 {
			t.Fatal("expected recommendations")
		}
		found := false
		for _, r := range recs {
			if simContains(r, "mode-b") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected recommendation mentioning mode-b, got %v", recs)
		}
	})

	t.Run("low redundancy", func(t *testing.T) {
		t.Parallel()
		analysis := &RedundancyAnalysis{
			OverallScore:   0.1,
			PairwiseScores: nil,
		}
		recs := generateRedundancyRecommendations(analysis, 0.5)
		found := false
		for _, r := range recs {
			if simContains(r, "Good mode diversity") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected good diversity message, got %v", recs)
		}
	})

	t.Run("high overall score", func(t *testing.T) {
		t.Parallel()
		analysis := &RedundancyAnalysis{
			OverallScore:   0.6,
			PairwiseScores: nil,
		}
		recs := generateRedundancyRecommendations(analysis, 0.5)
		found := false
		for _, r := range recs {
			if simContains(r, "High overall redundancy") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected high redundancy message, got %v", recs)
		}
	})
}

// =============================================================================
// jaccardSimilarity (token-based)
// =============================================================================

func TestJaccardSimilarity(t *testing.T) {
	t.Parallel()

	set := func(elems ...string) map[string]struct{} {
		m := make(map[string]struct{}, len(elems))
		for _, e := range elems {
			m[e] = struct{}{}
		}
		return m
	}

	t.Run("both empty", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarity(nil, nil)
		if got != 1.0 {
			t.Errorf("both empty = %f, want 1.0 (identical empty sets)", got)
		}
	})

	t.Run("one empty", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarity(set("a"), nil)
		if got != 0.0 {
			t.Errorf("one empty = %f, want 0.0", got)
		}
	})

	t.Run("identical", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarity(set("a", "b"), set("a", "b"))
		if got != 1.0 {
			t.Errorf("identical = %f, want 1.0", got)
		}
	})

	t.Run("disjoint", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarity(set("a"), set("b"))
		if got != 0.0 {
			t.Errorf("disjoint = %f, want 0.0", got)
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		t.Parallel()
		got := jaccardSimilarity(set("a", "b"), set("b", "c"))
		want := 1.0 / 3.0
		if got < want-0.001 || got > want+0.001 {
			t.Errorf("partial = %f, want %f", got, want)
		}
	})
}

// =============================================================================
// tokenize (ensemble)
// =============================================================================

func TestTokenizeEnsemble(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty", "", 0},
		{"single word", "hello", 1},
		{"multiple words", "hello world foo", 3},
		{"with whitespace", "  hello   world  ", 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tokenize(tc.text)
			if len(got) != tc.want {
				t.Errorf("tokenize(%q) returned %d tokens, want %d", tc.text, len(got), tc.want)
			}
		})
	}
}

// =============================================================================
// normalizeText
// =============================================================================

func TestNormalizeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"lowercase", "HELLO WORLD", "hello world"},
		{"trims whitespace", "  hello  ", "hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeText(tc.input)
			if got != tc.want {
				t.Errorf("normalizeText(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Helper function
func simContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && simFindSubstring(s, substr)))
}

func simFindSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
