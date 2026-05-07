package pipeline

import (
	"errors"
)

var errNoAdjudicatorPane = errors.New("no available adjudicator pane")
var errNoModelFamilyPane = errors.New("no pane matches model family")
var errNoPaneForStrategy = errors.New("no pane available for assignment strategy")

type paneStrategyPane struct {
	ID          string
	ModelFamily string
	Domains     []string
}

// rotateAdjudicator chooses an adjudicator pane from orderedPanes while
// excluding the current debate champions. Among eligible panes, it picks the
// pane with the longest gap since it last adjudicated. Ties keep orderedPanes
// order, which makes the strategy deterministic.
func rotateAdjudicator(orderedPanes []string, champions []string, adjudicatorHistory []string) (string, error) {
	championSet := make(map[string]struct{}, len(champions))
	for _, paneID := range champions {
		if paneID != "" {
			championSet[paneID] = struct{}{}
		}
	}

	lastSeen := make(map[string]int, len(adjudicatorHistory))
	for idx, paneID := range adjudicatorHistory {
		if paneID != "" {
			lastSeen[paneID] = idx
		}
	}

	bestPane := ""
	bestGap := -1
	for _, paneID := range orderedPanes {
		if paneID == "" {
			continue
		}
		if _, champion := championSet[paneID]; champion {
			continue
		}

		gap := len(adjudicatorHistory) + 1
		if idx, ok := lastSeen[paneID]; ok {
			gap = len(adjudicatorHistory) - idx
		}
		if gap > bestGap {
			bestPane = paneID
			bestGap = gap
		}
	}
	if bestPane == "" {
		return "", errNoAdjudicatorPane
	}
	return bestPane, nil
}

// byModelFamily chooses the first pane whose model family matches the current
// foreach item. The ordered input preserves deterministic routing when several
// panes share a family.
func byModelFamily(orderedPanes []paneStrategyPane, modelFamily string) (string, error) {
	if modelFamily == "" {
		return "", errNoModelFamilyPane
	}
	for _, pane := range orderedPanes {
		if pane.ID != "" && pane.ModelFamily == modelFamily {
			return pane.ID, nil
		}
	}
	return "", errNoModelFamilyPane
}

// byModelFamilyDifference chooses the first pane whose model family differs
// from the item's authoring family. If every pane has the same family, it
// falls back to the first available pane and reports warnFallback=true.
func byModelFamilyDifference(orderedPanes []paneStrategyPane, authorModelFamily string) (paneID string, warnFallback bool, err error) {
	firstPane := ""
	for _, pane := range orderedPanes {
		if pane.ID == "" {
			continue
		}
		if firstPane == "" {
			firstPane = pane.ID
		}
		if pane.ModelFamily != authorModelFamily {
			return pane.ID, false, nil
		}
	}
	if firstPane == "" {
		return "", false, errNoPaneForStrategy
	}
	return firstPane, true, nil
}
