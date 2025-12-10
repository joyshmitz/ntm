package cass

import (
	"context"
)

type DuplicateCheckResult struct {
	Query           string      `json:"query"`
	DuplicatesFound bool        `json:"duplicates_found"`
	SimilarSessions []SearchHit `json:"similar_sessions"`
	Recommendation  string      `json:"recommendation"` // "proceed", "review", "skip"
}

func (c *Client) CheckDuplicates(ctx context.Context, query, workspace, since string, threshold float64) (*DuplicateCheckResult, error) {
	if threshold <= 0 {
		threshold = 0.7
	}
	if since == "" {
		since = "7d"
	}

	// Search for similar sessions
	// Note: CASS search score is usually relevant for similarity
	resp, err := c.Search(ctx, SearchOptions{
		Query:     query,
		Workspace: workspace,
		Since:     since,
		Limit:     5,
	})
	if err != nil {
		return nil, err
	}

	result := &DuplicateCheckResult{
		Query:           query,
		SimilarSessions: []SearchHit{},
		Recommendation:  "proceed",
	}

	for _, hit := range resp.Hits {
		if hit.Score >= threshold {
			result.SimilarSessions = append(result.SimilarSessions, hit)
		}
	}

	if len(result.SimilarSessions) > 0 {
		result.DuplicatesFound = true
		result.Recommendation = "review_before_proceeding"
	}

	return result, nil
}
