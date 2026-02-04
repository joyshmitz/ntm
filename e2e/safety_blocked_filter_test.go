//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// [E2E-SAFETY] Tests for ntm safety blocked filtering.
package e2e

import (
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/policy"
)

func TestSafetyBlocked_JSON_Filters(t *testing.T) {
	suite := NewSafetyTestSuite(t, "blocked-filters")

	now := time.Now()
	entries := []policy.BlockedEntry{
		{
			Timestamp: now.Add(-30 * time.Minute),
			Command:   "rm -rf /",
			Pattern:   `rm\\s+-rf\\s+/$`,
			Reason:    "Recursive delete of root is catastrophic",
			Action:    policy.ActionBlock,
		},
		{
			Timestamp: now.Add(-3 * time.Hour),
			Command:   "git reset --hard",
			Pattern:   `git\\s+reset\\s+--hard`,
			Reason:    "Hard reset loses uncommitted changes",
			Action:    policy.ActionBlock,
		},
		{
			Timestamp: now.Add(-48 * time.Hour),
			Command:   "rm -rf ~",
			Pattern:   `rm\\s+-rf\\s+~`,
			Reason:    "Recursive delete of home directory",
			Action:    policy.ActionBlock,
		},
	}

	if _, err := suite.writeBlockedLog(entries); err != nil {
		t.Fatalf("[E2E-SAFETY] Failed to write blocked log: %v", err)
	}

	// Filter to last 2 hours; should exclude the 3h and 48h entries.
	resp, _, _, err := suite.runSafetyBlocked(2, 20)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse blocked response: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for safety blocked, got error: %v", err)
	}

	if resp.Count != 1 {
		t.Fatalf("[E2E-SAFETY] Expected 1 entry within 2 hours, got %d", resp.Count)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("[E2E-SAFETY] Expected 1 entry within 2 hours, got %d", len(resp.Entries))
	}
	if resp.Entries[0].Command != "rm -rf /" {
		t.Errorf("[E2E-SAFETY] Expected recent entry command %q, got %q", "rm -rf /", resp.Entries[0].Command)
	}

	// Limit to 1 entry over a wider window to ensure limit is applied.
	resp, _, _, err = suite.runSafetyBlocked(72, 1)
	if resp == nil {
		t.Fatalf("[E2E-SAFETY] Failed to parse blocked response for limit: %v", err)
	}
	if err != nil {
		t.Fatalf("[E2E-SAFETY] Expected exit code 0 for safety blocked with limit, got error: %v", err)
	}
	if resp.Count != 1 || len(resp.Entries) != 1 {
		t.Fatalf("[E2E-SAFETY] Expected 1 entry after limit, got count=%d len=%d", resp.Count, len(resp.Entries))
	}
}
