package cass

import (
	"context"
	"encoding/json"
	"fmt"
)

// Status checks the health of the CASS service/index
func (c *Client) Status(ctx context.Context) (*StatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := []string{"robot", "status"}
	
	output, err := c.executor.Run(ctx, args...)
	if err != nil {
		return nil, err
	}

	var response StatusResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return &response, nil
}
