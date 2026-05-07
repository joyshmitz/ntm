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
