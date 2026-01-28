package ensemble

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestCoverageMap_CalculateCoverage(t *testing.T) {
	catalog, modeIDs := testModeCatalogAllCategories(t)
	coverage := NewCoverageMap(catalog)

	coverage.RecordMode(modeIDs[0])
	coverage.RecordMode(modeIDs[1])
	coverage.RecordMode(modeIDs[0])

	report := coverage.CalculateCoverage()
	if report == nil {
		t.Fatal("expected non-nil report")
	}

	expectedOverall := 2.0 / float64(len(AllCategories()))
	if math.Abs(report.Overall-expectedOverall) > 0.0001 {
		t.Errorf("Overall coverage = %.3f, want %.3f", report.Overall, expectedOverall)
	}

	formal := report.PerCategory[CategoryFormal]
	if formal.TotalModes != 1 {
		t.Errorf("Formal total modes = %d, want 1", formal.TotalModes)
	}
	if len(formal.UsedModes) != 1 {
		t.Errorf("Formal used modes = %d, want 1", len(formal.UsedModes))
	}
	if formal.Coverage != 1.0 {
		t.Errorf("Formal coverage = %.2f, want 1.0", formal.Coverage)
	}

	uncertainty := report.PerCategory[CategoryUncertainty]
	if uncertainty.Coverage != 0 {
		t.Errorf("Uncertainty coverage = %.2f, want 0", uncertainty.Coverage)
	}

	if len(report.BlindSpots) != len(AllCategories())-2 {
		t.Errorf("BlindSpots count = %d, want %d", len(report.BlindSpots), len(AllCategories())-2)
	}
	if containsCategory(report.BlindSpots, CategoryFormal) {
		t.Error("expected Formal to be covered, but it appears in blind spots")
	}
	if !containsCategory(report.BlindSpots, CategoryUncertainty) {
		t.Error("expected Uncertainty to be a blind spot")
	}
}

func TestCoverageMap_Render_DeterministicOrdering(t *testing.T) {
	catalog, modeIDs := testModeCatalogAllCategories(t)
	coverage := NewCoverageMap(catalog)

	coverage.RecordMode(modeIDs[1])
	coverage.RecordMode(modeIDs[0])

	output := coverage.Render()
	if !strings.Contains(output, "Category Coverage:") {
		t.Fatalf("render missing header: %s", output)
	}

	lines := strings.Split(output, "\n")
	got := make([]string, 0, len(AllCategories()))
	for _, line := range lines {
		if len(line) >= 3 && strings.HasPrefix(line, "[") && line[2] == ']' {
			got = append(got, line[:3])
		}
	}

	want := make([]string, 0, len(AllCategories()))
	for _, category := range AllCategories() {
		want = append(want, "["+category.CategoryLetter()+"]")
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("render category order = %v, want %v", got, want)
	}
}

func TestCoverageMap_Suggestions_PreferCore(t *testing.T) {
	catalog, _ := testModeCatalogAllCategories(t)
	modes := catalog.ListModes()
	modes = append(modes, ReasoningMode{
		ID:        "formal-adv",
		Code:      "A9",
		Name:      "Formal Advanced",
		Category:  CategoryFormal,
		Tier:      TierAdvanced,
		ShortDesc: "advanced formal",
	})

	updatedCatalog, err := NewModeCatalog(modes, "1.0.0")
	if err != nil {
		t.Fatalf("NewModeCatalog: %v", err)
	}

	coverage := NewCoverageMap(updatedCatalog)
	coverage.RecordMode("ampliative")

	report := coverage.CalculateCoverage()
	var formalSuggestion string
	for _, suggestion := range report.Suggestions {
		if strings.Contains(suggestion, "Formal reasoning (A)") {
			formalSuggestion = suggestion
			break
		}
	}
	if formalSuggestion == "" {
		t.Fatal("expected suggestion for Formal category")
	}
	if strings.Contains(formalSuggestion, "formal-adv") {
		t.Errorf("expected core mode suggestion, got %q", formalSuggestion)
	}
	if !strings.Contains(formalSuggestion, "formal (A1)") {
		t.Errorf("expected formal core suggestion, got %q", formalSuggestion)
	}
}

func containsCategory(categories []ModeCategory, target ModeCategory) bool {
	for _, category := range categories {
		if category == target {
			return true
		}
	}
	return false
}
