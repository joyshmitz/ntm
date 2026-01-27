package ensemble

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// RedundancyAnalysis measures output similarity across modes.
// Higher overall score indicates more redundant (wasteful) mode selection.
type RedundancyAnalysis struct {
	// OverallScore ranges from 0-1, where higher means more redundant.
	OverallScore float64 `json:"overall_score" yaml:"overall_score"`

	// PairwiseScores holds similarity data for each mode pair.
	PairwiseScores []PairSimilarity `json:"pairwise_scores" yaml:"pairwise_scores"`

	// Recommendations are suggestions for reducing redundancy.
	Recommendations []string `json:"recommendations,omitempty" yaml:"recommendations,omitempty"`
}

// PairSimilarity measures similarity between two specific modes.
type PairSimilarity struct {
	// ModeA is the first mode ID.
	ModeA string `json:"mode_a" yaml:"mode_a"`

	// ModeB is the second mode ID.
	ModeB string `json:"mode_b" yaml:"mode_b"`

	// Similarity ranges from 0-1, where 1 means identical findings.
	Similarity float64 `json:"similarity" yaml:"similarity"`

	// SharedFindings is the count of findings appearing in both modes.
	SharedFindings int `json:"shared_findings" yaml:"shared_findings"`

	// UniqueToA is findings only in ModeA.
	UniqueToA int `json:"unique_to_a" yaml:"unique_to_a"`

	// UniqueToB is findings only in ModeB.
	UniqueToB int `json:"unique_to_b" yaml:"unique_to_b"`
}

// RedundancyConfig controls how redundancy is calculated.
type RedundancyConfig struct {
	// FindingsWeight controls how much findings similarity contributes (default: 0.8).
	FindingsWeight float64 `json:"findings_weight" yaml:"findings_weight"`

	// RecommendationsWeight controls how much recommendations similarity contributes (default: 0.2).
	RecommendationsWeight float64 `json:"recommendations_weight" yaml:"recommendations_weight"`

	// HighRedundancyThreshold is the similarity threshold for flagging pairs (default: 0.5).
	HighRedundancyThreshold float64 `json:"high_redundancy_threshold" yaml:"high_redundancy_threshold"`
}

// DefaultRedundancyConfig returns the default configuration.
func DefaultRedundancyConfig() RedundancyConfig {
	return RedundancyConfig{
		FindingsWeight:          0.8,
		RecommendationsWeight:   0.2,
		HighRedundancyThreshold: 0.5,
	}
}

// CalculateRedundancy computes similarity analysis for a set of mode outputs.
// Uses default configuration.
func CalculateRedundancy(outputs []ModeOutput) *RedundancyAnalysis {
	return CalculateRedundancyWithConfig(outputs, DefaultRedundancyConfig())
}

// CalculateRedundancyWithConfig computes similarity analysis with custom config.
func CalculateRedundancyWithConfig(outputs []ModeOutput, cfg RedundancyConfig) *RedundancyAnalysis {
	if len(outputs) < 2 {
		return &RedundancyAnalysis{
			OverallScore:    0,
			PairwiseScores:  nil,
			Recommendations: []string{"Need at least 2 modes to analyze redundancy"},
		}
	}

	// Build normalized finding sets per mode
	modeFindingSets := make(map[string]map[string]struct{}, len(outputs))
	modeRecSets := make(map[string]map[string]struct{}, len(outputs))

	for _, output := range outputs {
		findingSet := make(map[string]struct{})
		for _, f := range output.TopFindings {
			key := normalizeFinding(f)
			findingSet[key] = struct{}{}
		}
		modeFindingSets[output.ModeID] = findingSet

		recSet := make(map[string]struct{})
		for _, r := range output.Recommendations {
			key := normalizeText(r.Recommendation)
			recSet[key] = struct{}{}
		}
		modeRecSets[output.ModeID] = recSet
	}

	// Compute pairwise similarities
	var pairs []PairSimilarity
	var totalSimilarity float64
	pairCount := 0

	for i := 0; i < len(outputs); i++ {
		for j := i + 1; j < len(outputs); j++ {
			modeA := outputs[i].ModeID
			modeB := outputs[j].ModeID

			findingsA := modeFindingSets[modeA]
			findingsB := modeFindingSets[modeB]
			recsA := modeRecSets[modeA]
			recsB := modeRecSets[modeB]

			// Compute Jaccard similarity for findings
			findingsSim := jaccardSimilarityFromSets(findingsA, findingsB)

			// Compute weighted similarity
			// Only include recommendations weight if at least one mode has recommendations
			var combinedSim float64
			if len(recsA) == 0 && len(recsB) == 0 {
				// No recommendations in either mode - only count findings
				combinedSim = findingsSim
			} else {
				// At least one mode has recommendations - use weighted combination
				recsSim := jaccardSimilarityForNonEmpty(recsA, recsB)
				combinedSim = cfg.FindingsWeight*findingsSim + cfg.RecommendationsWeight*recsSim
			}

			// Count shared and unique findings
			shared, uniqueA, uniqueB := countSetOverlap(findingsA, findingsB)

			pair := PairSimilarity{
				ModeA:          modeA,
				ModeB:          modeB,
				Similarity:     combinedSim,
				SharedFindings: shared,
				UniqueToA:      uniqueA,
				UniqueToB:      uniqueB,
			}
			pairs = append(pairs, pair)

			totalSimilarity += combinedSim
			pairCount++

			// Log top redundant pairs for debugging
			if combinedSim >= cfg.HighRedundancyThreshold {
				slog.Debug("high redundancy pair detected",
					"mode_a", modeA,
					"mode_b", modeB,
					"similarity", fmt.Sprintf("%.2f", combinedSim),
					"shared_findings", shared)
			}
		}
	}

	// Sort pairs by similarity descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Similarity > pairs[j].Similarity
	})

	// Calculate overall redundancy score (average pairwise similarity)
	overallScore := 0.0
	if pairCount > 0 {
		overallScore = totalSimilarity / float64(pairCount)
	}

	analysis := &RedundancyAnalysis{
		OverallScore:   overallScore,
		PairwiseScores: pairs,
	}

	// Generate recommendations based on analysis
	analysis.Recommendations = generateRedundancyRecommendations(analysis, cfg.HighRedundancyThreshold)

	return analysis
}

// GetHighRedundancyPairs returns pairs above the given similarity threshold.
func (r *RedundancyAnalysis) GetHighRedundancyPairs(threshold float64) []PairSimilarity {
	if r == nil {
		return nil
	}

	var result []PairSimilarity
	for _, pair := range r.PairwiseScores {
		if pair.Similarity >= threshold {
			result = append(result, pair)
		}
	}
	return result
}

// SuggestReplacements suggests alternative modes for redundant pairs.
func (r *RedundancyAnalysis) SuggestReplacements(catalog *ModeCatalog) []string {
	if r == nil || catalog == nil {
		return nil
	}

	highRedundancy := r.GetHighRedundancyPairs(0.5)
	if len(highRedundancy) == 0 {
		return []string{"No high-redundancy pairs found - mode selection is diverse"}
	}

	var suggestions []string
	suggestedModes := make(map[string]bool)

	for _, pair := range highRedundancy {
		// Get the categories of the redundant modes
		modeA := catalog.GetMode(pair.ModeA)
		modeB := catalog.GetMode(pair.ModeB)

		if modeA == nil || modeB == nil {
			continue
		}

		// If both modes are in the same category, suggest a mode from a different category
		if modeA.Category == modeB.Category {
			// Find an alternative from a different category
			for _, cat := range AllCategories() {
				if cat == modeA.Category {
					continue
				}
				alternatives := catalog.ListByCategory(cat)
				for _, alt := range alternatives {
					if alt.Tier == TierCore && !suggestedModes[alt.ID] {
						suggestion := fmt.Sprintf("Consider replacing %s with %s (%s) for more diverse analysis",
							pair.ModeB, alt.ID, alt.Category)
						suggestions = append(suggestions, suggestion)
						suggestedModes[alt.ID] = true
						break
					}
				}
				if len(suggestions) > 0 {
					break
				}
			}
		}
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Redundant modes span multiple categories - consider domain-specific alternatives")
	}

	return suggestions
}

// Render produces a human-readable redundancy report.
func (r *RedundancyAnalysis) Render() string {
	if r == nil {
		return "No redundancy data available"
	}

	var b strings.Builder

	// Header
	b.WriteString("Redundancy Analysis:\n")

	// Overall score with interpretation
	interpretation := interpretScore(r.OverallScore)
	fmt.Fprintf(&b, "Overall Score: %.2f (%s)\n\n", r.OverallScore, interpretation)

	// Pairwise similarities
	if len(r.PairwiseScores) > 0 {
		b.WriteString("Pairwise Similarity:\n")
		for _, pair := range r.PairwiseScores {
			level := classifySimilarity(pair.Similarity)
			fmt.Fprintf(&b, "%s ↔ %s: %.2f (%s - %s)\n",
				pair.ModeA, pair.ModeB, pair.Similarity, level, diversityNote(pair.Similarity))
		}
		b.WriteString("\n")
	}

	// Recommendations
	if len(r.Recommendations) > 0 {
		for _, rec := range r.Recommendations {
			fmt.Fprintf(&b, "Recommendation: %s\n", rec)
		}
	}

	return b.String()
}

// normalizeFinding creates a normalized key for a finding.
// Combines the finding text and evidence pointer when present.
func normalizeFinding(f Finding) string {
	key := normalizeText(f.Finding)
	if f.EvidencePointer != "" {
		key += "|" + normalizeText(f.EvidencePointer)
	}
	return key
}

// jaccardSimilarityFromSets computes Jaccard similarity between two string sets.
// Returns 0 if both sets are empty (no common ground to compare).
func jaccardSimilarityFromSets(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0 // No findings = no similarity to measure
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	intersection := 0
	for key := range a {
		if _, ok := b[key]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// jaccardSimilarityForNonEmpty computes Jaccard similarity, returning 0 if either set is empty.
// Used for recommendation comparison where empty-vs-non-empty should not contribute.
func jaccardSimilarityForNonEmpty(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	intersection := 0
	for key := range a {
		if _, ok := b[key]; ok {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// countSetOverlap counts shared and unique elements between two sets.
func countSetOverlap(a, b map[string]struct{}) (shared, uniqueA, uniqueB int) {
	for key := range a {
		if _, ok := b[key]; ok {
			shared++
		} else {
			uniqueA++
		}
	}
	for key := range b {
		if _, ok := a[key]; !ok {
			uniqueB++
		}
	}
	return
}

// generateRedundancyRecommendations creates actionable recommendations.
func generateRedundancyRecommendations(analysis *RedundancyAnalysis, threshold float64) []string {
	var recs []string

	highRedundancy := analysis.GetHighRedundancyPairs(threshold)
	if len(highRedundancy) > 0 {
		// Log the recommendation inputs
		for _, pair := range highRedundancy {
			slog.Debug("generating redundancy recommendation",
				"mode_a", pair.ModeA,
				"mode_b", pair.ModeB,
				"overlap_count", pair.SharedFindings)
		}

		// Create specific recommendations
		for _, pair := range highRedundancy {
			if pair.Similarity >= 0.7 {
				recs = append(recs, fmt.Sprintf(
					"Consider replacing %s with different mode (%.0f%% overlap with %s)",
					pair.ModeB, pair.Similarity*100, pair.ModeA))
			}
		}
	}

	// Add general recommendation based on overall score
	if analysis.OverallScore >= 0.5 {
		recs = append(recs, "High overall redundancy - consider more diverse mode selection")
	} else if analysis.OverallScore < 0.2 {
		recs = append(recs, "Good mode diversity - findings coverage is well distributed")
	}

	return recs
}

// interpretScore returns a human-readable interpretation of the overall score.
func interpretScore(score float64) string {
	switch {
	case score >= 0.7:
		return "high redundancy - significant overlap"
	case score >= 0.5:
		return "moderate redundancy"
	case score >= 0.3:
		return "acceptable"
	default:
		return "low redundancy - good diversity"
	}
}

// classifySimilarity returns a classification label for a similarity score.
func classifySimilarity(score float64) string {
	switch {
	case score >= 0.7:
		return "HIGH"
	case score >= 0.4:
		return "moderate"
	default:
		return "low"
	}
}

// diversityNote returns a diversity note based on similarity.
func diversityNote(score float64) string {
	if score >= 0.5 {
		return "overlapping insights"
	}
	return "good diversity"
}
