package scanner

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// FormatImpactReport — missing branches
// ---------------------------------------------------------------------------

func TestFormatImpactReport_GraphAvailable(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		TotalFindings:  1,
		GraphAvailable: true,
		RecommendedOrder: []ImpactAnalysis{
			{
				Finding:     Finding{File: "main.go", Line: 5, Severity: SeverityWarning, Message: "issue"},
				ImpactScore: 5.0,
			},
		},
	}

	report := FormatImpactReport(result)
	if strings.Contains(report, "bv not available") {
		t.Error("should not mention bv unavailable when graph is available")
	}
}

func TestFormatImpactReport_BlocksCount(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		TotalFindings:  1,
		GraphAvailable: true,
		RecommendedOrder: []ImpactAnalysis{
			{
				Finding:     Finding{File: "main.go", Line: 10, Severity: SeverityCritical, Message: "blocker"},
				BlocksCount: 5,
				ImpactScore: 15.0,
			},
		},
	}

	report := FormatImpactReport(result)
	if !strings.Contains(report, "blocks 5 tasks") {
		t.Errorf("expected 'blocks 5 tasks', got:\n%s", report)
	}
}

func TestFormatImpactReport_HotspotCentrality(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		TotalFindings:  1,
		GraphAvailable: true,
		Hotspots: []Hotspot{
			{File: "core.go", FindingCount: 3, Centrality: 0.85},
		},
	}

	report := FormatImpactReport(result)
	if !strings.Contains(report, "centrality: 0.85") {
		t.Errorf("expected centrality info, got:\n%s", report)
	}
}

func TestFormatImpactReport_HotspotNoCentrality(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{
		TotalFindings: 1,
		Hotspots: []Hotspot{
			{File: "util.go", FindingCount: 2, Centrality: 0},
		},
	}

	report := FormatImpactReport(result)
	if strings.Contains(report, "centrality") {
		t.Errorf("expected no centrality for zero value, got:\n%s", report)
	}
}

func TestFormatImpactReport_TruncateOver10(t *testing.T) {
	t.Parallel()

	recs := make([]ImpactAnalysis, 15)
	for i := range recs {
		recs[i] = ImpactAnalysis{
			Finding:     Finding{File: "f.go", Line: i + 1, Severity: SeverityInfo, Message: "msg"},
			ImpactScore: float64(15 - i),
		}
	}

	result := &AnalysisResult{
		TotalFindings:    15,
		RecommendedOrder: recs,
	}

	report := FormatImpactReport(result)
	if !strings.Contains(report, "... and 5 more") {
		t.Errorf("expected truncation message '... and 5 more', got:\n%s", report)
	}
}

func TestFormatImpactReport_Empty(t *testing.T) {
	t.Parallel()

	result := &AnalysisResult{}
	report := FormatImpactReport(result)
	if !strings.Contains(report, "Scan Impact Analysis") {
		t.Error("expected header even for empty result")
	}
	if strings.Contains(report, "High-Impact Findings") {
		t.Error("should not show High-Impact section for empty findings")
	}
}

// ---------------------------------------------------------------------------
// shortenPath — ensure it's exercised
// ---------------------------------------------------------------------------

func TestShortenPath_Branches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"simple", "main.go", "main.go"},
		{"nested", "internal/scanner/analysis.go", "internal/scanner/analysis.go"},
		{"absolute", "/home/user/project/internal/foo.go", "/home/user/project/internal/foo.go"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shortenPath(tc.path)
			if got == "" {
				t.Errorf("shortenPath(%q) returned empty", tc.path)
			}
		})
	}
}
