package pipeline

import (
	"errors"
	"testing"
)

func TestRotateAdjudicatorSkipsChampionsAndRecentAdjudicator(t *testing.T) {
	got, err := rotateAdjudicator(
		[]string{"p1", "p2", "p3", "p4", "p5"},
		[]string{"p1", "p2"},
		[]string{"p3"},
	)
	if err != nil {
		t.Fatalf("rotateAdjudicator() error = %v", err)
	}
	if got != "p4" {
		t.Fatalf("rotateAdjudicator() = %q, want p4", got)
	}
}

func TestRotateAdjudicatorNoPriorAdjudicationUsesFirstNonChampion(t *testing.T) {
	got, err := rotateAdjudicator(
		[]string{"p1", "p2", "p3", "p4", "p5"},
		[]string{"p1", "p2"},
		nil,
	)
	if err != nil {
		t.Fatalf("rotateAdjudicator() error = %v", err)
	}
	if got != "p3" {
		t.Fatalf("rotateAdjudicator() = %q, want p3", got)
	}
}

func TestRotateAdjudicatorErrorsWhenOnlyChampionsAvailable(t *testing.T) {
	got, err := rotateAdjudicator(
		[]string{"p1", "p2"},
		[]string{"p1", "p2"},
		nil,
	)
	if !errors.Is(err, errNoAdjudicatorPane) {
		t.Fatalf("rotateAdjudicator() error = %v, want %v", err, errNoAdjudicatorPane)
	}
	if got != "" {
		t.Fatalf("rotateAdjudicator() = %q, want empty pane", got)
	}
}

func TestRotateAdjudicatorUsesLongestHistoryGap(t *testing.T) {
	got, err := rotateAdjudicator(
		[]string{"p1", "p2", "p3", "p4", "p5"},
		[]string{"p1", "p2"},
		[]string{"p5", "p4", "p3"},
	)
	if err != nil {
		t.Fatalf("rotateAdjudicator() error = %v", err)
	}
	if got != "p5" {
		t.Fatalf("rotateAdjudicator() = %q, want p5", got)
	}
}

func TestByModelFamilyReturnsFirstMatchingPane(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cc"},
		{ID: "p3", ModelFamily: "cod"},
		{ID: "p4", ModelFamily: "gmi"},
	}

	got, err := byModelFamily(panes, "cc")
	if err != nil {
		t.Fatalf("byModelFamily() error = %v", err)
	}
	if got != "p1" {
		t.Fatalf("byModelFamily() = %q, want p1", got)
	}
}

func TestByModelFamilyReturnsSingleMatchingPane(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cc"},
		{ID: "p3", ModelFamily: "cod"},
		{ID: "p4", ModelFamily: "gmi"},
	}

	got, err := byModelFamily(panes, "cod")
	if err != nil {
		t.Fatalf("byModelFamily() error = %v", err)
	}
	if got != "p3" {
		t.Fatalf("byModelFamily() = %q, want p3", got)
	}
}

func TestByModelFamilyErrorsWhenNoPaneMatches(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cod"},
	}

	got, err := byModelFamily(panes, "ollama")
	if !errors.Is(err, errNoModelFamilyPane) {
		t.Fatalf("byModelFamily() error = %v, want %v", err, errNoModelFamilyPane)
	}
	if got != "" {
		t.Fatalf("byModelFamily() = %q, want empty pane", got)
	}
}

func TestByModelFamilyDifferenceReturnsDifferentFamilyPane(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cod"},
	}

	got, warned, err := byModelFamilyDifference(panes, "cc")
	if err != nil {
		t.Fatalf("byModelFamilyDifference() error = %v", err)
	}
	if warned {
		t.Fatal("byModelFamilyDifference() warned, want no warning")
	}
	if got != "p2" {
		t.Fatalf("byModelFamilyDifference() = %q, want p2", got)
	}
}

func TestByModelFamilyDifferenceFallsBackWhenAllPanesShareFamily(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p1", ModelFamily: "cc"},
		{ID: "p2", ModelFamily: "cc"},
	}

	got, warned, err := byModelFamilyDifference(panes, "cc")
	if err != nil {
		t.Fatalf("byModelFamilyDifference() error = %v", err)
	}
	if !warned {
		t.Fatal("byModelFamilyDifference() warned = false, want true")
	}
	if got != "p1" {
		t.Fatalf("byModelFamilyDifference() = %q, want p1", got)
	}
}

func TestByModelFamilyDifferenceErrorsWhenNoPanesAvailable(t *testing.T) {
	got, warned, err := byModelFamilyDifference(nil, "cc")
	if !errors.Is(err, errNoPaneForStrategy) {
		t.Fatalf("byModelFamilyDifference() error = %v, want %v", err, errNoPaneForStrategy)
	}
	if warned {
		t.Fatal("byModelFamilyDifference() warned = true, want false")
	}
	if got != "" {
		t.Fatalf("byModelFamilyDifference() = %q, want empty pane", got)
	}
}

func TestRoundRobinByDomainReturnsOwningPane(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p2", Domains: []string{"H-001", "H-005"}},
		{ID: "p3", Domains: []string{"H-002"}},
	}

	got, err := roundRobinByDomain(panes, "H-005", 0)
	if err != nil {
		t.Fatalf("roundRobinByDomain() error = %v", err)
	}
	if got != "p2" {
		t.Fatalf("roundRobinByDomain() = %q, want p2", got)
	}
}

func TestRoundRobinByDomainFallsBackToRoundRobin(t *testing.T) {
	panes := []paneStrategyPane{
		{ID: "p2"},
		{ID: "p3"},
		{ID: "p4"},
	}

	got, err := roundRobinByDomain(panes, "H-999", 4)
	if err != nil {
		t.Fatalf("roundRobinByDomain() error = %v", err)
	}
	if got != "p3" {
		t.Fatalf("roundRobinByDomain() = %q, want p3", got)
	}
}

func TestParsePaneDomainRoster(t *testing.T) {
	roster := `
pane p2:
  domain: [H-001, H-005]
pane p3:
  domain: [H-002]
`

	got, err := parsePaneDomainRoster(roster)
	if err != nil {
		t.Fatalf("parsePaneDomainRoster() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parsePaneDomainRoster() returned %d panes, want 2", len(got))
	}
	if got[0].ID != "p2" || len(got[0].Domains) != 2 || got[0].Domains[1] != "H-005" {
		t.Fatalf("first parsed pane = %#v, want p2 with H-005", got[0])
	}
}

func TestParsePaneDomainRosterRejectsMalformedDomainList(t *testing.T) {
	_, err := parsePaneDomainRoster(`
pane p2:
  domain: H-001, H-005
`)
	if !errors.Is(err, errMalformedPaneDomainRoster) {
		t.Fatalf("parsePaneDomainRoster() error = %v, want %v", err, errMalformedPaneDomainRoster)
	}
}
