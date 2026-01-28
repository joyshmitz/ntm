package ensemble

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

const coverageBarWidth = 4

// CoverageMap tracks category coverage for an ensemble run.
type CoverageMap struct {
	Categories map[ModeCategory]CategoryCoverage
	catalog    *ModeCatalog
	used       map[ModeCategory]map[string]struct{}
}

// CategoryCoverage stores per-category usage stats.
type CategoryCoverage struct {
	Category   ModeCategory
	TotalModes int
	UsedModes  []string
	Coverage   float64 // used/total
}

// CoverageReport summarizes coverage across categories.
type CoverageReport struct {
	Overall     float64
	PerCategory map[ModeCategory]CategoryCoverage
	BlindSpots  []ModeCategory
	Suggestions []string
}

// NewCoverageMap creates a new coverage tracker for a catalog.
func NewCoverageMap(catalog *ModeCatalog) *CoverageMap {
	result := &CoverageMap{
		Categories: make(map[ModeCategory]CategoryCoverage),
		catalog:    catalog,
		used:       make(map[ModeCategory]map[string]struct{}),
	}
	for _, category := range AllCategories() {
		total := 0
		if catalog != nil {
			total = len(catalog.ListByCategory(category))
		}
		result.Categories[category] = CategoryCoverage{
			Category:   category,
			TotalModes: total,
			UsedModes:  []string{},
			Coverage:   0,
		}
	}
	return result
}

// RecordMode records a mode as used for coverage tracking.
func (c *CoverageMap) RecordMode(modeID string) {
	if c == nil || c.catalog == nil || modeID == "" {
		return
	}
	mode := c.catalog.GetMode(modeID)
	if mode == nil {
		return
	}
	category := mode.Category
	if _, ok := c.used[category]; !ok {
		c.used[category] = make(map[string]struct{})
	}
	if _, exists := c.used[category][modeID]; exists {
		return
	}
	c.used[category][modeID] = struct{}{}

	coverage, ok := c.Categories[category]
	if !ok {
		coverage = CategoryCoverage{Category: category}
	}
	coverage.UsedModes = append(coverage.UsedModes, modeID)
	c.Categories[category] = coverage
}

// CalculateCoverage computes overall and per-category coverage statistics.
func (c *CoverageMap) CalculateCoverage() *CoverageReport {
	report := &CoverageReport{
		PerCategory: make(map[ModeCategory]CategoryCoverage),
	}
	if c == nil {
		return report
	}

	categories := AllCategories()
	totalCategories := len(categories)
	coveredCategories := 0
	blindSpots := make([]ModeCategory, 0)

	for _, category := range categories {
		coverage, ok := c.Categories[category]
		if !ok {
			coverage = CategoryCoverage{Category: category}
		}
		if c.catalog != nil {
			coverage.TotalModes = len(c.catalog.ListByCategory(category))
		}
		used := len(coverage.UsedModes)
		if coverage.TotalModes > 0 {
			coverage.Coverage = float64(used) / float64(coverage.TotalModes)
		} else {
			coverage.Coverage = 0
		}

		sortedUsed := append([]string(nil), coverage.UsedModes...)
		sort.Strings(sortedUsed)
		coverage.UsedModes = sortedUsed
		c.Categories[category] = coverage

		if used > 0 {
			coveredCategories++
		} else {
			blindSpots = append(blindSpots, category)
		}

		report.PerCategory[category] = coverage
		slog.Debug("coverage category counts",
			"category", category.String(),
			"total_modes", coverage.TotalModes,
			"used_modes", used,
		)
	}

	if totalCategories > 0 {
		report.Overall = float64(coveredCategories) / float64(totalCategories)
	}
	report.BlindSpots = blindSpots
	report.Suggestions = c.suggestModes(blindSpots)

	slog.Debug("coverage blind spots detected",
		"blind_spots", formatCategoryList(blindSpots),
	)
	slog.Debug("coverage suggestions generated",
		"suggestions", report.Suggestions,
	)

	return report
}

// Render produces a human-readable summary of category coverage.
func (c *CoverageMap) Render() string {
	if c == nil {
		return "No coverage data available"
	}
	report := c.CalculateCoverage()
	if report == nil {
		return "No coverage data available"
	}

	var b strings.Builder
	b.WriteString("Category Coverage:\n")

	for _, category := range AllCategories() {
		coverage := report.PerCategory[category]
		bar := coverageBar(len(coverage.UsedModes), coverage.TotalModes, coverageBarWidth)
		fmt.Fprintf(&b, "[%s] %-12s %s %d/%d modes\n",
			category.CategoryLetter(),
			category.String(),
			bar,
			len(coverage.UsedModes),
			coverage.TotalModes,
		)
	}

	fmt.Fprintf(&b, "\nOverall Coverage: %.2f\n", report.Overall)

	if len(report.BlindSpots) > 0 {
		b.WriteString("\nBlind Spots: ")
		b.WriteString(formatCategoryList(report.BlindSpots))
		b.WriteString("\n")
	}

	if len(report.Suggestions) > 0 {
		b.WriteString("\nSuggestions:\n")
		for _, suggestion := range report.Suggestions {
			fmt.Fprintf(&b, "- %s\n", suggestion)
		}
	}

	return b.String()
}

func coverageBar(used, total, width int) string {
	if width <= 0 {
		return ""
	}
	filled := 0
	if total > 0 {
		filled = (used*width + total/2) / total
		if filled > width {
			filled = width
		}
		if filled < 0 {
			filled = 0
		}
	}
	return strings.Repeat("#", filled) + strings.Repeat("-", width-filled)
}

func (c *CoverageMap) suggestModes(blindSpots []ModeCategory) []string {
	if c == nil || c.catalog == nil || len(blindSpots) == 0 {
		return nil
	}

	suggestions := make([]string, 0, len(blindSpots))
	for _, category := range blindSpots {
		candidates := c.catalog.ListByCategory(category)
		if len(candidates) == 0 {
			suggestions = append(suggestions,
				fmt.Sprintf("No available modes to cover %s reasoning (%s)", category.String(), category.CategoryLetter()),
			)
			continue
		}

		sort.SliceStable(candidates, func(i, j int) bool {
			left := candidates[i]
			right := candidates[j]
			leftTier := tierRank(left.Tier)
			rightTier := tierRank(right.Tier)
			if leftTier != rightTier {
				return leftTier < rightTier
			}
			leftCost := estimateTypicalCost(&left)
			rightCost := estimateTypicalCost(&right)
			if leftCost != rightCost {
				return leftCost < rightCost
			}
			if left.Code != right.Code {
				return left.Code < right.Code
			}
			return left.ID < right.ID
		})

		best := candidates[0]
		label := best.ID
		if best.Code != "" {
			label = fmt.Sprintf("%s (%s)", best.ID, best.Code)
		}
		suggestions = append(suggestions,
			fmt.Sprintf("Add %s to cover %s reasoning (%s)", label, category.String(), category.CategoryLetter()),
		)
	}
	return suggestions
}

func tierRank(tier ModeTier) int {
	switch tier {
	case TierCore:
		return 0
	case TierAdvanced:
		return 1
	case TierExperimental:
		return 2
	default:
		return 3
	}
}

func formatCategoryList(categories []ModeCategory) string {
	if len(categories) == 0 {
		return ""
	}
	labels := make([]string, 0, len(categories))
	for _, category := range categories {
		labels = append(labels, fmt.Sprintf("%s reasoning (%s)", category.String(), category.CategoryLetter()))
	}
	return strings.Join(labels, ", ")
}
