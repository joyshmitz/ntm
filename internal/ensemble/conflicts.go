package ensemble

import (
	"fmt"
	"log/slog"
	"sort"
)

const defaultHighConflictThreshold = 2

// ConflictTracker aggregates conflicts across mode outputs.
type ConflictTracker struct {
	Conflicts []Conflict
	Source    string
}

// Conflict captures a disagreement between two modes.
type Conflict struct {
	Topic      string
	ModeA      string
	ModeB      string
	PositionA  string
	PositionB  string
	Severity   ConflictSeverity
	Resolved   bool
	Resolution string
}

// ConflictDensity summarizes disagreement frequency across mode pairs.
type ConflictDensity struct {
	TotalConflicts      int
	ResolvedConflicts   int
	UnresolvedConflicts int
	ConflictsPerPair    float64
	HighConflictPairs   []string
	Source              string
}

// NewConflictTracker creates a tracker for conflict analysis.
func NewConflictTracker() *ConflictTracker {
	return &ConflictTracker{
		Conflicts: []Conflict{},
	}
}

// FromAudit converts DisagreementAuditor output into conflict pairs.
func (c *ConflictTracker) FromAudit(report *AuditReport) []Conflict {
	if c == nil {
		return nil
	}
	c.Source = "auditor"
	c.Conflicts = convertAuditConflicts(report)
	slog.Debug("conflict density source",
		"source", c.Source,
		"conflict_count", len(c.Conflicts),
	)
	return c.Conflicts
}

// DetectConflicts runs a heuristic audit to detect conflicts as a fallback.
func (c *ConflictTracker) DetectConflicts(outputs []ModeOutput) []Conflict {
	if c == nil {
		return nil
	}
	c.Source = "fallback"
	if len(outputs) == 0 {
		c.Conflicts = nil
		slog.Debug("conflict density source",
			"source", c.Source,
			"conflict_count", 0,
		)
		return nil
	}
	auditor := NewDisagreementAuditor(outputs, nil)
	c.Conflicts = convertDetailedConflicts(auditor.IdentifyConflicts())
	slog.Debug("conflict density source",
		"source", c.Source,
		"conflict_count", len(c.Conflicts),
	)
	return c.Conflicts
}

// GetDensity returns conflict density metrics for the current conflicts.
func (c *ConflictTracker) GetDensity(totalPairs int) *ConflictDensity {
	density := &ConflictDensity{
		Source: "",
	}
	if c == nil {
		return density
	}
	density.Source = c.Source
	density.TotalConflicts = len(c.Conflicts)

	for _, conflict := range c.Conflicts {
		if conflict.Resolved {
			density.ResolvedConflicts++
		}
	}
	density.UnresolvedConflicts = density.TotalConflicts - density.ResolvedConflicts

	if totalPairs > 0 {
		density.ConflictsPerPair = float64(density.TotalConflicts) / float64(totalPairs)
	}

	density.HighConflictPairs = c.GetHighConflictPairs(defaultHighConflictThreshold)
	slog.Debug("conflict density calculated",
		"source", density.Source,
		"total_conflicts", density.TotalConflicts,
		"resolved_conflicts", density.ResolvedConflicts,
		"unresolved_conflicts", density.UnresolvedConflicts,
		"conflicts_per_pair", density.ConflictsPerPair,
		"high_conflict_pairs", density.HighConflictPairs,
	)
	return density
}

// GetHighConflictPairs returns mode pairs with conflicts >= threshold.
func (c *ConflictTracker) GetHighConflictPairs(threshold int) []string {
	if c == nil || threshold <= 0 {
		return nil
	}

	counts := make(map[string]int)
	for _, conflict := range c.Conflicts {
		modeA, modeB := normalizePair(conflict.ModeA, conflict.ModeB)
		if modeA == "" || modeB == "" {
			continue
		}
		key := fmt.Sprintf("%s <-> %s", modeA, modeB)
		counts[key]++
	}

	pairs := make([]string, 0, len(counts))
	for pair, count := range counts {
		if count >= threshold {
			pairs = append(pairs, pair)
		}
	}
	sort.Strings(pairs)
	return pairs
}

func convertAuditConflicts(report *AuditReport) []Conflict {
	if report == nil {
		return nil
	}
	return convertDetailedConflicts(report.Conflicts)
}

func convertDetailedConflicts(conflicts []DetailedConflict) []Conflict {
	if len(conflicts) == 0 {
		return nil
	}

	out := make([]Conflict, 0)
	for _, conflict := range conflicts {
		positions := conflict.Positions
		for i := 0; i < len(positions); i++ {
			for j := i + 1; j < len(positions); j++ {
				left := positions[i]
				right := positions[j]
				if left.ModeID == "" || right.ModeID == "" {
					continue
				}
				out = append(out, Conflict{
					Topic:      conflict.Topic,
					ModeA:      left.ModeID,
					ModeB:      right.ModeID,
					PositionA:  left.Position,
					PositionB:  right.Position,
					Severity:   conflict.Severity,
					Resolved:   false,
					Resolution: conflict.ResolutionPath,
				})
			}
		}
	}

	return out
}

func normalizePair(a, b string) (string, string) {
	if a <= b {
		return a, b
	}
	return b, a
}
