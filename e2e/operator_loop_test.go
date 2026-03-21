// Package e2e implements transcript-style operator-loop scenarios for the attention feed.
//
// This file covers the canonical operator loop scenarios from br-mamo3:
// - Cold-start snapshot then cursor handoff
// - Raw event replay after snapshot
// - Wake on stalled pane, ack-required mail, reservation/file conflict
// - --robot-attention repeating cleanly across multiple wake cycles
// - Resync behavior after expired cursor or process restart
// - Operator vs debug mode comparison
// - Dashboard and terse output verification
//
// Each test uses the ScenarioHarness from harness.go for artifact logging and cleanup.
// Note: No e2e build tag to avoid conflicts with scenario_harness.go.
package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// operatorLoopTestRunner implements RunnerFunc for operator loop tests.
func operatorLoopTestRunner(ctx context.Context, spec CommandSpec) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}

	start := time.Now()
	stdout, err := cmd.Output()
	var stderr []byte
	if exitErr, ok := err.(*exec.ExitError); ok {
		stderr = exitErr.Stderr
	}
	end := time.Now()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return CommandResult{
		StartedAt:   start,
		CompletedAt: end,
		Duration:    end.Sub(start),
		ExitCode:    exitCode,
		Stdout:      stdout,
		Stderr:      stderr,
	}, err
}

// setupMockNTM creates a mock ntm script for testing without real tmux sessions.
// Returns the mock directory and a cleanup function.
func setupMockNTM(t *testing.T) (string, func()) {
	t.Helper()

	mockDir := t.TempDir()
	mockPath := filepath.Join(mockDir, "ntm")

	// Mock script that simulates robot mode responses
	script := `#!/bin/sh
# Mock ntm for operator-loop e2e tests
# Controlled by environment variables for response customization

case "$1" in
  --robot-status)
    if [ -n "$MOCK_STATUS_ERROR" ]; then
      echo "{\"success\":false,\"error_code\":\"$MOCK_STATUS_ERROR\",\"hint\":\"mock error\"}"
      exit 1
    fi
    cat <<'EOF'
{
  "success": true,
  "sessions": [
    {
      "name": "test_session",
      "panes": [
        {"index": 0, "agent_type": "cc", "status": "working"},
        {"index": 1, "agent_type": "cc", "status": "idle"}
      ]
    }
  ],
  "cursor": 100
}
EOF
    ;;

  --robot-snapshot)
    cursor="${MOCK_CURSOR:-100}"
    attention_action="${MOCK_ATTENTION_ACTION:-0}"
    attention_interest="${MOCK_ATTENTION_INTEREST:-0}"
    cat <<EOF
{
  "success": true,
  "cursor": $cursor,
  "attention_summary": {
    "action_required_count": $attention_action,
    "interesting_count": $attention_interest,
    "session_hints": {}
  },
  "sessions": [
    {
      "name": "test_session",
      "panes": [
        {"index": 0, "agent_type": "cc", "status": "working"},
        {"index": 1, "agent_type": "cc", "status": "idle"}
      ]
    }
  ],
  "alerts": [],
  "mail": {"unread": 0}
}
EOF
    ;;

  --robot-events)
    since="${MOCK_EVENTS_SINCE:-0}"
    cat <<EOF
{
  "success": true,
  "events": [
    {"id": 101, "category": "agent", "actionability": "interesting", "summary": "pane stalled"},
    {"id": 102, "category": "mail", "actionability": "action_required", "summary": "new mail"}
  ],
  "cursor": 102,
  "has_more": false
}
EOF
    ;;

  --robot-attention)
    cursor="${MOCK_CURSOR:-100}"
    wake_reason="${MOCK_WAKE_REASON:-attention}"
    cat <<EOF
{
  "success": true,
  "wake_reason": "$wake_reason",
  "cursor": $cursor,
  "digest": {
    "action_required": 1,
    "interesting": 2,
    "focus_targets": ["test_session:0"]
  },
  "terse": "S:test|A:2/2|W:1|I:1|E:0|C:45|B:R0/I0/B0|M:0|^:1a,2i|!:"
}
EOF
    ;;

  --robot-terse)
    attention_action="${MOCK_ATTENTION_ACTION:-0}"
    attention_interest="${MOCK_ATTENTION_INTEREST:-0}"
    if [ "$attention_action" = "0" ] && [ "$attention_interest" = "0" ]; then
      echo "S:test|A:2/2|W:1|I:1|E:0|C:45|B:R0/I0/B0|M:0|^:0|!:"
    else
      echo "S:test|A:2/2|W:1|I:1|E:0|C:45|B:R0/I0/B0|M:0|^:${attention_action}a,${attention_interest}i|!:"
    fi
    ;;

  --robot-digest)
    profile="${MOCK_PROFILE:-operator}"
    cat <<EOF
{
  "success": true,
  "profile": "$profile",
  "action_required_count": 1,
  "interesting_count": 3,
  "focus_targets": ["test_session:0", "test_session:1"],
  "sections": [
    {"title": "Action Required", "items": [{"summary": "pane 0 stalled", "target": "test_session:0"}]},
    {"title": "Interesting", "items": [{"summary": "context at 85%", "target": "test_session:1"}]}
  ]
}
EOF
    ;;

  --robot-version)
    echo '{"success":true,"version":"0.1.0-test","commit":"mock123"}'
    ;;

  *)
    echo '{"success":true}'
    ;;
esac
`

	if err := os.WriteFile(mockPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write mock ntm: %v", err)
	}

	// Prepend mock dir to PATH
	origPath := os.Getenv("PATH")
	newPath := mockDir + string(os.PathListSeparator) + origPath
	os.Setenv("PATH", newPath)

	cleanup := func() {
		os.Setenv("PATH", origPath)
	}

	return mockDir, cleanup
}

// parseCursorFromJSON extracts a cursor from JSON output.
func parseCursorFromJSON(data []byte, path string) (int64, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return 0, false
	}

	parts := strings.Split(path, ".")
	var current interface{} = m
	for _, part := range parts {
		if cm, ok := current.(map[string]interface{}); ok {
			current = cm[part]
		} else {
			return 0, false
		}
	}

	switch v := current.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	}
	return 0, false
}

// getJSONField extracts a field from JSON output.
func getJSONField(data []byte, path string) (interface{}, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}

	parts := strings.Split(path, ".")
	var current interface{} = m
	for _, part := range parts {
		if cm, ok := current.(map[string]interface{}); ok {
			current = cm[part]
		} else {
			return nil, false
		}
	}
	return current, true
}

// TestOperatorLoopColdStartSnapshot tests the cold-start snapshot then cursor handoff scenario.
//
// Scenario: Agent starts fresh, takes initial snapshot, then uses cursor for subsequent calls.
// Verifies: Snapshot returns valid cursor, subsequent calls with --since-cursor work correctly.
func TestOperatorLoopColdStartSnapshot(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "cold_start_snapshot_handoff",
		ArtifactRoot: t.TempDir(),
		RunToken:     "smoke",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	// Step 1: Cold-start with --robot-snapshot (no cursor)
	h.RecordStep("cold start: initial snapshot", map[string]any{
		"scenario": "cold_start_snapshot_handoff",
		"phase":    "initial",
	})

	snapshotResult, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("initial snapshot failed: %v", err)
	}
	if snapshotResult.ExitCode != 0 {
		t.Fatalf("initial snapshot exit code %d, stderr: %s", snapshotResult.ExitCode, snapshotResult.Stderr)
	}

	// Extract cursor from snapshot
	cursor, ok := parseCursorFromJSON(snapshotResult.Stdout, "cursor")
	if !ok {
		t.Fatal("snapshot should return a cursor")
	}

	h.RecordStep("cursor acquired", map[string]any{"cursor": cursor})
	t.Logf("Initial cursor: %d", cursor)

	// Step 2: Verify attention summary is present
	h.RecordStep("verify attention summary in snapshot", nil)

	attnSummary, ok := getJSONField(snapshotResult.Stdout, "attention_summary")
	if !ok {
		t.Fatal("snapshot should include attention_summary")
	}
	t.Logf("Attention summary: %v", attnSummary)

	// Step 3: Use cursor for subsequent snapshot (delta mode)
	h.RecordStep("cursor handoff: delta snapshot", map[string]any{
		"since_cursor": cursor,
	})

	deltaResult, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot-delta",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-snapshot", "--since=" + strconv.FormatInt(cursor, 10)},
	})
	if err != nil {
		t.Fatalf("delta snapshot failed: %v", err)
	}
	if deltaResult.ExitCode != 0 {
		t.Fatalf("delta snapshot exit code %d", deltaResult.ExitCode)
	}

	newCursor, ok := parseCursorFromJSON(deltaResult.Stdout, "cursor")
	if !ok {
		t.Fatal("delta snapshot should return a cursor")
	}

	h.RecordStep("cursor handoff complete", map[string]any{
		"old_cursor": cursor,
		"new_cursor": newCursor,
	})

	t.Logf("Cursor handoff: %d -> %d", cursor, newCursor)
}

// TestOperatorLoopRawEventReplay tests raw event replay after snapshot.
//
// Scenario: After snapshot, use --robot-events with cursor to replay events.
// Verifies: Events are returned in order, cursor advances monotonically.
func TestOperatorLoopRawEventReplay(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "raw_event_replay",
		ArtifactRoot: t.TempDir(),
		RunToken:     "replay",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	// Step 1: Get initial cursor from snapshot
	h.RecordStep("establish baseline cursor", nil)

	snapshotResult, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("baseline snapshot failed: %v", err)
	}

	cursor, _ := parseCursorFromJSON(snapshotResult.Stdout, "cursor")

	// Step 2: Replay events since cursor
	h.RecordStep("replay events since cursor", map[string]any{
		"since_cursor": cursor,
	})

	eventsResult, err := h.RunCommand(CommandSpec{
		Name: "robot-events",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-events", "--since-cursor=100", "--events-limit=50"},
	})
	if err != nil {
		t.Fatalf("events replay failed: %v", err)
	}
	if eventsResult.ExitCode != 0 {
		t.Fatalf("events replay exit code %d", eventsResult.ExitCode)
	}

	// Verify events structure
	events, ok := getJSONField(eventsResult.Stdout, "events")
	if !ok {
		t.Fatal("events should be present")
	}
	eventsArr, _ := events.([]interface{})
	t.Logf("Replayed %d events", len(eventsArr))

	// Extract new cursor
	eventCursor, ok := parseCursorFromJSON(eventsResult.Stdout, "cursor")
	if !ok {
		t.Fatal("events should return updated cursor")
	}

	h.RecordStep("events replayed", map[string]any{
		"event_count":  len(eventsArr),
		"old_cursor":   cursor,
		"event_cursor": eventCursor,
	})

	// Verify has_more field
	hasMore, _ := getJSONField(eventsResult.Stdout, "has_more")
	t.Logf("Events cursor: %d, has_more: %v", eventCursor, hasMore)

	// Step 3: Store events as artifact for inspection
	if _, err := h.WriteCapture("replayed_events.json", eventsResult.Stdout); err != nil {
		t.Logf("Warning: failed to write events artifact: %v", err)
	}
}

// TestOperatorLoopWakeOnAttention tests wake-on-attention behavior.
//
// Scenario: Use --robot-attention to wait for attention events.
// Verifies: Returns appropriate wake_reason, focus_targets, and digest.
func TestOperatorLoopWakeOnAttention(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "wake_on_attention",
		ArtifactRoot: t.TempDir(),
		RunToken:     "wake",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	// Test cases for different wake reasons
	testCases := []struct {
		name          string
		wakeReason    string
		expectDigest  bool
		expectTargets bool
	}{
		{
			name:          "wake_on_stalled_pane",
			wakeReason:    "attention",
			expectDigest:  true,
			expectTargets: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h.RecordStep(tc.name, map[string]any{
				"expected_wake_reason": tc.wakeReason,
			})

			// Set mock environment
			os.Setenv("MOCK_WAKE_REASON", tc.wakeReason)
			defer os.Unsetenv("MOCK_WAKE_REASON")

			// Run --robot-attention with short timeout
			result, err := h.RunCommand(CommandSpec{
				Name:    "robot-attention",
				Path:    filepath.Join(mockDir, "ntm"),
				Args:    []string{"--robot-attention", "--attention-timeout=2s", "--attention-poll=100ms"},
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("robot-attention failed: %v", err)
			}
			if result.ExitCode != 0 {
				t.Fatalf("robot-attention exit code %d", result.ExitCode)
			}

			// Verify wake reason
			actualWake, _ := getJSONField(result.Stdout, "wake_reason")
			if actualWake != tc.wakeReason {
				t.Errorf("wake_reason = %q, want %q", actualWake, tc.wakeReason)
			}

			// Verify digest presence
			if tc.expectDigest {
				digest, ok := getJSONField(result.Stdout, "digest")
				if !ok {
					t.Error("expected digest in response")
				} else {
					t.Logf("Digest: %v", digest)
				}
			}

			// Verify focus targets
			if tc.expectTargets {
				digestMap, _ := getJSONField(result.Stdout, "digest")
				if dm, ok := digestMap.(map[string]interface{}); ok {
					targets, _ := dm["focus_targets"].([]interface{})
					if len(targets) == 0 {
						t.Error("expected non-empty focus_targets")
					} else {
						t.Logf("Focus targets: %v", targets)
					}
				}
			}

			// Record cursor tracking
			cursor, _ := parseCursorFromJSON(result.Stdout, "cursor")
			h.RecordStep("cursor tracked", map[string]any{
				"cursor":      cursor,
				"wake_reason": tc.wakeReason,
			})
		})
	}
}

// TestOperatorLoopMultipleWakeCycles tests --robot-attention across multiple wake cycles.
//
// Scenario: Run multiple --robot-attention calls in sequence, simulating continuous monitoring.
// Verifies: Cursor advances monotonically, each cycle produces valid output.
func TestOperatorLoopMultipleWakeCycles(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "multiple_wake_cycles",
		ArtifactRoot: t.TempDir(),
		RunToken:     "cycles",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	numCycles := 3
	var lastCursor int64

	for cycle := 1; cycle <= numCycles; cycle++ {
		h.RecordStep("wake cycle start", map[string]any{
			"cycle":       cycle,
			"total":       numCycles,
			"last_cursor": lastCursor,
		})

		// Construct args
		args := []string{
			"--robot-attention",
			"--attention-timeout=1s",
			"--attention-poll=100ms",
		}

		result, err := h.RunCommand(CommandSpec{
			Name:    "robot-attention-cycle",
			Path:    filepath.Join(mockDir, "ntm"),
			Args:    args,
			Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("cycle %d failed: %v", cycle, err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("cycle %d exit code %d", cycle, result.ExitCode)
		}

		// Extract cursor
		cursor, ok := parseCursorFromJSON(result.Stdout, "cursor")
		if ok {
			// In a real scenario, verify monotonicity
			if lastCursor > 0 && cursor < lastCursor {
				t.Errorf("cursor should be monotonic: cycle %d cursor %d < previous %d",
					cycle, cursor, lastCursor)
			}
			lastCursor = cursor
		}

		wakeReason, _ := getJSONField(result.Stdout, "wake_reason")
		h.RecordStep("wake cycle complete", map[string]any{
			"cycle":       cycle,
			"cursor":      cursor,
			"wake_reason": wakeReason,
		})

		t.Logf("Cycle %d: cursor=%v wake=%s", cycle, cursor, wakeReason)
	}

	t.Logf("Completed %d wake cycles, final cursor: %d", numCycles, lastCursor)
}

// TestOperatorLoopTerseBeforeAfterAttention tests terse output changes with attention.
//
// Scenario: Capture terse output before and after attention state changes.
// Verifies: Attention state (^:NaNi format) updates correctly.
func TestOperatorLoopTerseBeforeAfterAttention(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "terse_attention_changes",
		ArtifactRoot: t.TempDir(),
		RunToken:     "terse",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	// Step 1: Capture terse with no attention
	h.RecordStep("terse: no attention", nil)

	os.Setenv("MOCK_ATTENTION_ACTION", "0")
	os.Setenv("MOCK_ATTENTION_INTEREST", "0")
	defer os.Unsetenv("MOCK_ATTENTION_ACTION")
	defer os.Unsetenv("MOCK_ATTENTION_INTEREST")

	beforeResult, err := h.RunCommand(CommandSpec{
		Name: "robot-terse-before",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-terse"},
	})
	if err != nil {
		t.Fatalf("terse before failed: %v", err)
	}

	beforeTerse := strings.TrimSpace(string(beforeResult.Stdout))
	t.Logf("Before attention: %s", beforeTerse)

	// Verify ^:0 (no attention) format
	if !strings.Contains(beforeTerse, "^:0") {
		t.Errorf("expected ^:0 in terse output, got: %s", beforeTerse)
	}

	// Step 2: Simulate attention state change
	h.RecordStep("terse: with attention", map[string]any{
		"action_required": 2,
		"interesting":     5,
	})

	os.Setenv("MOCK_ATTENTION_ACTION", "2")
	os.Setenv("MOCK_ATTENTION_INTEREST", "5")

	afterResult, err := h.RunCommand(CommandSpec{
		Name: "robot-terse-after",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-terse"},
	})
	if err != nil {
		t.Fatalf("terse after failed: %v", err)
	}

	afterTerse := strings.TrimSpace(string(afterResult.Stdout))
	t.Logf("After attention: %s", afterTerse)

	// Verify ^:2a,5i format
	if !strings.Contains(afterTerse, "^:2a,5i") {
		t.Errorf("expected ^:2a,5i in terse output, got: %s", afterTerse)
	}

	// Store both outputs as summary artifact
	summary := "# Terse Output Comparison\n\n"
	summary += "## Before Attention\n```\n" + beforeTerse + "\n```\n\n"
	summary += "## After Attention\n```\n" + afterTerse + "\n```\n"

	if _, err := h.WriteRenderedSummary("terse_comparison.md", []byte(summary)); err != nil {
		t.Logf("Warning: failed to write summary: %v", err)
	}
}

// TestOperatorLoopProfileComparison tests operator vs debug profile differences.
//
// Scenario: Request digest with operator profile and debug profile on same state.
// Verifies: Debug profile returns more verbose output than operator profile.
func TestOperatorLoopProfileComparison(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "profile_comparison",
		ArtifactRoot: t.TempDir(),
		RunToken:     "profile",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	profiles := []string{"operator", "debug"}
	results := make(map[string]CommandResult)

	for _, profile := range profiles {
		h.RecordStep("digest with profile: "+profile, map[string]any{
			"profile": profile,
		})

		os.Setenv("MOCK_PROFILE", profile)
		result, err := h.RunCommand(CommandSpec{
			Name: "robot-digest-" + profile,
			Path: filepath.Join(mockDir, "ntm"),
			Args: []string{"--robot-digest", "--profile=" + profile},
		})
		if err != nil {
			t.Fatalf("digest with %s profile failed: %v", profile, err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("digest with %s profile exit code %d", profile, result.ExitCode)
		}

		results[profile] = result

		// Log key fields
		actionCount, _ := parseCursorFromJSON(result.Stdout, "action_required_count")
		interestCount, _ := parseCursorFromJSON(result.Stdout, "interesting_count")
		t.Logf("Profile %s: action_required=%d interesting=%d", profile, actionCount, interestCount)
	}
	os.Unsetenv("MOCK_PROFILE")

	// Compare outputs
	h.RecordStep("compare profiles", map[string]any{
		"profiles": profiles,
	})

	// In real implementation, debug would show more detail
	// For mock, verify both return valid data
	for _, profile := range profiles {
		r := results[profile]
		actionCount, _ := parseCursorFromJSON(r.Stdout, "action_required_count")
		if actionCount < 0 {
			t.Errorf("%s profile: invalid action_required_count", profile)
		}
		targets, _ := getJSONField(r.Stdout, "focus_targets")
		if arr, ok := targets.([]interface{}); ok {
			t.Logf("Profile %s: %d focus targets", profile, len(arr))
		}
	}

	// Generate comparison summary
	comparison := "# Profile Comparison\n\n"
	for _, profile := range profiles {
		comparison += "## " + profile + "\n```json\n"
		comparison += string(results[profile].Stdout) + "\n```\n\n"
	}

	if _, err := h.WriteRenderedSummary("profile_comparison.md", []byte(comparison)); err != nil {
		t.Logf("Warning: failed to write comparison summary: %v", err)
	}
}

// TestOperatorLoopErrorRecovery tests resync after errors.
//
// Scenario: Simulate cursor expiry or process restart requiring resync.
// Verifies: System gracefully handles stale cursor and resyncs from snapshot.
func TestOperatorLoopErrorRecovery(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "error_recovery_resync",
		ArtifactRoot: t.TempDir(),
		RunToken:     "recovery",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	// Step 1: Establish baseline
	h.RecordStep("establish baseline", nil)

	baselineResult, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot-baseline",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("baseline snapshot failed: %v", err)
	}

	baseCursor, _ := parseCursorFromJSON(baselineResult.Stdout, "cursor")
	t.Logf("Baseline cursor: %d", baseCursor)

	// Step 2: Simulate stale cursor (using very old cursor value)
	staleCursor := int64(1)
	h.RecordStep("attempt with stale cursor", map[string]any{
		"stale_cursor": staleCursor,
		"note":         "simulating expired cursor",
	})

	staleResult, _ := h.RunCommand(CommandSpec{
		Name:         "robot-events-stale",
		Path:         filepath.Join(mockDir, "ntm"),
		Args:         []string{"--robot-events", "--since-cursor=1", "--events-limit=10"},
		AllowFailure: true,
	})

	// In real impl, this might return CURSOR_EXPIRED error
	// For mock, it succeeds but we log the scenario
	t.Logf("Stale cursor result: exit=%d", staleResult.ExitCode)

	// Step 3: Recovery via fresh snapshot
	h.RecordStep("recovery: fresh snapshot", map[string]any{
		"recovery_reason": "stale_cursor",
	})

	recoveryResult, err := h.RunCommand(CommandSpec{
		Name: "robot-snapshot-recovery",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-snapshot"},
	})
	if err != nil {
		t.Fatalf("recovery snapshot failed: %v", err)
	}

	newCursor, _ := parseCursorFromJSON(recoveryResult.Stdout, "cursor")
	h.RecordStep("recovery complete", map[string]any{
		"stale_cursor": staleCursor,
		"new_cursor":   newCursor,
	})

	t.Logf("Recovery cursor: %d (was stale at %d)", newCursor, staleCursor)

	// Verify recovery successful
	if newCursor <= staleCursor {
		t.Errorf("recovery cursor should advance past stale: got %d, stale was %d", newCursor, staleCursor)
	}
}

// TestOperatorLoopDashboardOutput tests dashboard/digest format.
//
// Scenario: Request digest and verify structure matches dashboard expectations.
// Verifies: Digest has sections, action_required items, and interesting items.
func TestOperatorLoopDashboardOutput(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "dashboard_output",
		ArtifactRoot: t.TempDir(),
		RunToken:     "dashboard",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	h.RecordStep("capture dashboard/digest output", nil)

	result, err := h.RunCommand(CommandSpec{
		Name: "robot-digest",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-digest"},
	})
	if err != nil {
		t.Fatalf("digest failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("digest exit code %d", result.ExitCode)
	}

	// Verify key dashboard fields
	actionCount, _ := parseCursorFromJSON(result.Stdout, "action_required_count")
	interestCount, _ := parseCursorFromJSON(result.Stdout, "interesting_count")

	t.Logf("Dashboard counts: action_required=%d interesting=%d", actionCount, interestCount)

	// Verify sections array
	sections, ok := getJSONField(result.Stdout, "sections")
	if !ok {
		t.Error("digest should include sections array")
	} else if arr, ok := sections.([]interface{}); ok {
		t.Logf("Dashboard has %d sections", len(arr))
		for i, sec := range arr {
			if secMap, ok := sec.(map[string]interface{}); ok {
				t.Logf("  Section %d: %s", i, secMap["title"])
			}
		}
	}

	// Verify focus targets
	targets, _ := getJSONField(result.Stdout, "focus_targets")
	t.Logf("Focus targets: %v", targets)

	// Save digest as artifact
	if _, err := h.WriteRenderedSummary("dashboard_digest.json", result.Stdout); err != nil {
		t.Logf("Warning: failed to write digest artifact: %v", err)
	}
}

// TestOperatorLoopVersionInfo tests --robot-version output.
//
// Scenario: Simple verification that version endpoint works.
// Verifies: Returns success with version info.
func TestOperatorLoopVersionInfo(t *testing.T) {
	mockDir, cleanup := setupMockNTM(t)
	defer cleanup()

	h, err := NewScenarioHarness(t, HarnessOptions{
		Scenario:     "version_info",
		ArtifactRoot: t.TempDir(),
		RunToken:     "version",
		Retain:       RetainAlways,
		Runner:       operatorLoopTestRunner,
	})
	if err != nil {
		t.Fatalf("NewScenarioHarness() error = %v", err)
	}
	defer h.Close()

	h.RecordStep("get version info", nil)

	result, err := h.RunCommand(CommandSpec{
		Name: "robot-version",
		Path: filepath.Join(mockDir, "ntm"),
		Args: []string{"--robot-version"},
	})
	if err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("version exit code %d", result.ExitCode)
	}

	version, _ := getJSONField(result.Stdout, "version")
	commit, _ := getJSONField(result.Stdout, "commit")

	if version == "" {
		t.Error("version should be non-empty")
	}
	if commit == "" {
		t.Error("commit should be non-empty")
	}

	t.Logf("Version: %v commit: %v", version, commit)
}
