package cass

import (
	"context"
	"encoding/json"
	"fmt"
)

// Search performs a search query against CASS
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Build arguments for: cass robot search <query> [flags]
	args := []string{"robot", "search", opts.Query}
	
	if opts.Limit > 0 {
		args = append(args, fmt.Sprintf("--limit=%d", opts.Limit))
	}
	if opts.Offset > 0 {
		args = append(args, fmt.Sprintf("--offset=%d", opts.Offset))
	}
	if opts.Agent != "" {
		args = append(args, fmt.Sprintf("--agent=%s", opts.Agent))
	}
	if opts.Workspace != "" {
		args = append(args, fmt.Sprintf("--workspace=%s", opts.Workspace))
	}
	if opts.Since != "" {
		args = append(args, fmt.Sprintf("--since=%s", opts.Since))
	}
	
	output, err := c.executor.Run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var response SearchResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	return &response, nil
}
