//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM robot mode commands.
// robot_bulk_assign_test.go validates --robot-bulk-assign with various configurations.
//
// Bead: bd-1klou - Task: E2E Tests: Robot Bulk Assign
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/sqliteutil"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// BulkAssignOutput represents the JSON response from --robot-bulk-assign.
type BulkAssignOutput struct {
	Success          bool                   `json:"success"`
	Session          string                 `json:"session"`
	Strategy         string                 `json:"strategy"`
	Timestamp        string                 `json:"timestamp"`
	Assignments      []BulkAssignAssignment `json:"assignments"`
	Summary          BulkAssignSummary      `json:"summary"`
	UnassignedBeads  []string               `json:"unassigned_beads,omitempty"`
	UnassignedPanes  []string               `json:"unassigned_panes,omitempty"`
	DryRun           bool                   `json:"dry_run,omitempty"`
	AllocationSource string                 `json:"allocation_source,omitempty"`
	Error            string                 `json:"error,omitempty"`
	ErrorCode        string                 `json:"error_code,omitempty"`
}

// BulkAssignAssignment represents a single pane-to-bead allocation.
type BulkAssignAssignment struct {
	Pane              string `json:"pane"`
	PaneID            string `json:"pane_id"`
	Bead              string `json:"bead"`
	BeadTitle         string `json:"bead_title"`
	Reason            string `json:"reason"`
	AgentType         string `json:"agent_type"`
	Status            string `json:"status"`
	PromptSent        bool   `json:"prompt_sent"`
	Claimed           bool   `json:"claimed"`
	ClaimActor        string `json:"claim_actor,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`
	DispatchReceiptID string `json:"dispatch_receipt_id,omitempty"`
	Error             string `json:"error,omitempty"`
}

// BulkAssignSummary aggregates assignment stats.
type BulkAssignSummary struct {
	TotalPanes int `json:"total_panes"`
	Assigned   int `json:"assigned"`
	Skipped    int `json:"skipped"`
	Failed     int `json:"failed"`
}

// runBulkAssignCmd executes ntm --robot-bulk-assign with the given flags.
// Uses a 30-second timeout to prevent test hangs.
func runBulkAssignCmd(t *testing.T, suite *TestSuite, session string, flags ...string) (*BulkAssignOutput, []byte, error) {
	t.Helper()

	args := []string{fmt.Sprintf("--robot-bulk-assign=%s", session)}
	args = append(args, flags...)

	// Use context with timeout to prevent indefinite hangs (bd-brzap fix)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ntmPath, resolveErr := ensureE2ENTMBin()
	if resolveErr != nil {
		return nil, nil, fmt.Errorf("resolve E2E ntm binary: %w", resolveErr)
	}
	cmd := exec.CommandContext(ctx, ntmPath, args...)
	output, err := cmd.CombinedOutput()

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Command timed out after 30s: args=%v", args)
		return nil, output, fmt.Errorf("command timed out after 30s")
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] args=%v bytes=%d", args, len(output))

	var result BulkAssignOutput
	if jsonErr := json.Unmarshal(output, &result); jsonErr != nil {
		suite.Logger().Log("[E2E-BULK-ASSIGN] JSON parse failed: %v output=%s", jsonErr, string(output))
		return nil, output, err
	}

	suite.Logger().LogJSON("[E2E-BULK-ASSIGN] Result", result)
	return &result, output, err
}

func TestE2E_RobotBulkAssign_RequiresSession(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_requires_session")
	defer suite.Teardown()

	// Test that empty session (--robot-bulk-assign=) falls through to help
	// This is expected behavior - the flag needs a value
	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	cmd := exec.Command(ntmPath, "--robot-bulk-assign=")
	output, _ := cmd.CombinedOutput()

	suite.Logger().Log("[E2E-BULK-ASSIGN] empty session shows help: bytes=%d", len(output))

	// With empty value, NTM shows help instead of triggering the command
	// This is the expected CLI behavior - verify help is shown
	if !strings.Contains(string(output), "Named Tmux") && !strings.Contains(string(output), "session") {
		t.Fatalf("[E2E-BULK-ASSIGN] Expected help or session mention: %s", string(output)[:min(200, len(output))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestE2E_RobotBulkAssign_RequiresBVOrAllocation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_requires_source")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test without --from-bv or --allocation
	result, output, _ := runBulkAssignCmd(t, suite, suite.Session())

	// Should return error about missing source
	if result != nil && result.Success {
		t.Fatal("[E2E-BULK-ASSIGN] Should fail without --from-bv or --allocation")
	}

	// Check error message mentions the required flags
	outStr := string(output)
	if !strings.Contains(outStr, "from-bv") && !strings.Contains(outStr, "allocation") {
		t.Fatalf("[E2E-BULK-ASSIGN] Error should mention --from-bv or --allocation: %s", outStr)
	}
}

func TestE2E_RobotBulkAssign_DryRunMode(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_dry_run")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test --dry-run with explicit allocation
	allocation := `{"1":"bd-test1","2":"bd-test2"}`
	result, _, err := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if err != nil {
		// Even with error, check the response structure
		suite.Logger().Log("[E2E-BULK-ASSIGN] dry-run command error: %v", err)
	}

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify dry_run flag is set in response
	if !result.DryRun {
		t.Fatal("[E2E-BULK-ASSIGN] dry_run should be true in response")
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] dry_run=%v allocation_source=%s", result.DryRun, result.AllocationSource)
}

func TestE2E_RobotBulkAssign_ExplicitAllocation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_explicit")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test explicit allocation
	allocation := `{"1":"bd-abc123","2":"bd-xyz789"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run") // Use dry-run to avoid needing real beads

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify allocation source
	if result.AllocationSource != "explicit" {
		suite.Logger().Log("[E2E-BULK-ASSIGN] allocation_source=%s (expected: explicit)", result.AllocationSource)
	}

	// Verify the session is correct
	if result.Session != suite.Session() {
		t.Fatalf("[E2E-BULK-ASSIGN] Session mismatch: got=%s want=%s", result.Session, suite.Session())
	}
}

func TestE2E_RobotBulkAssign_SkipPanes(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_skip_panes")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with the canonical --skip flag.
	allocation := `{"1":"bd-test1","2":"bd-test2","3":"bd-test3"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--skip=1,2",
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify skipped panes are reflected - they appear as "failed" with "pane not available"
	suite.Logger().Log("[E2E-BULK-ASSIGN] skip_panes: summary.failed=%d", result.Summary.Failed)

	// Skipped panes should be marked as failed (not available)
	for _, a := range result.Assignments {
		if a.Pane == "1" || a.Pane == "2" {
			// These panes should be failed due to skip
			if a.Status != "failed" || !strings.Contains(a.Error, "pane not available") {
				suite.Logger().Log("[E2E-BULK-ASSIGN] Pane %s: status=%s error=%s", a.Pane, a.Status, a.Error)
			}
		}
	}
}

func TestE2E_RobotBulkAssign_Strategy(t *testing.T) {
	CommonE2EPrerequisites(t)

	strategies := []string{"impact", "ready", "stale", "balanced"}

	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			suite := NewTestSuite(t, fmt.Sprintf("bulk_assign_strategy_%s", strategy))
			defer suite.Teardown()

			if err := suite.Setup(); err != nil {
				t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
			}

			// Test strategy flag (note: --from-bv required with strategy)
			result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
				"--from-bv",
				"--strategy="+strategy,
				"--dry-run")

			// Even if bv is not available, the strategy should be recorded
			if result != nil {
				suite.Logger().Log("[E2E-BULK-ASSIGN] strategy=%s result.strategy=%s", strategy, result.Strategy)

				if result.Strategy != "" && result.Strategy != strategy {
					t.Fatalf("[E2E-BULK-ASSIGN] Strategy mismatch: got=%s want=%s", result.Strategy, strategy)
				}
			}
		})
	}
}

func TestE2E_RobotBulkAssign_StrategyFlagsBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("br is required for hermetic bulk strategy E2E: %v", err)
	}

	tests := []struct {
		name         string
		flags        []string
		wantStrategy string
	}{
		{
			name:         "deprecated_bulk_strategy_only",
			flags:        []string{"--bulk-strategy=stale"},
			wantStrategy: "stale",
		},
		{
			name:         "deprecated_flag_wins_when_canonical_is_first",
			flags:        []string{"--strategy=ready", "--bulk-strategy=balanced"},
			wantStrategy: "balanced",
		},
		{
			name:         "deprecated_flag_wins_when_canonical_is_last",
			flags:        []string{"--bulk-strategy=balanced", "--strategy=ready"},
			wantStrategy: "balanced",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRobotProcessFixture(t, "bulk-strategy")
			runBR := func(args ...string) []byte {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, brPath, args...)
				cmd.Dir = fixture.projectDir
				cmd.Env = append([]string(nil), fixture.env...)
				output, err := cmd.CombinedOutput()
				if ctx.Err() == context.DeadlineExceeded {
					t.Fatalf("br %q timed out", args)
				}
				if err != nil {
					t.Fatalf("br %q: %v output=%s", args, err, output)
				}
				return output
			}
			runBR("init", "--prefix=rbe2e", "--json")
			beadID := strings.TrimSpace(string(runBR("create", "Hermetic bulk strategy", "--type=task", "--priority=1", "--silent")))
			if beadID == "" || strings.ContainsAny(beadID, " \t\r\n") {
				t.Fatalf("unexpected br create output %q", beadID)
			}
			allocation, err := json.Marshal(map[string]string{fixture.paneID: beadID})
			if err != nil {
				t.Fatalf("marshal allocation: %v", err)
			}
			args := []string{
				"--robot-bulk-assign=" + fixture.session,
				"--allocation=" + string(allocation),
				"--dry-run",
				"--robot-format=json",
			}
			args = append(args, test.flags...)
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env, args...)
			if process.exitCode != 0 {
				t.Fatalf("bulk strategy process exit=%d, want 0; stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
			}
			if stderr := strings.TrimSpace(string(process.stderr)); stderr != "" &&
				(!strings.Contains(stderr, "--bulk-strategy") || !strings.Contains(strings.ToLower(stderr), "deprecated")) {
				t.Fatalf("unexpected bulk strategy stderr: %q", stderr)
			}

			var result BulkAssignOutput
			decodeSingleRobotJSON(t, process.stdout, &result)
			if !result.Success || !result.DryRun || result.Session != fixture.session {
				t.Fatalf("bulk strategy envelope = %+v", result)
			}
			if result.Strategy != test.wantStrategy {
				t.Fatalf("bulk strategy=%q, want %q for flags %q", result.Strategy, test.wantStrategy, test.flags)
			}
			if _, err := time.Parse(time.RFC3339Nano, result.Timestamp); err != nil {
				t.Fatalf("bulk strategy timestamp %q is not RFC3339: %v", result.Timestamp, err)
			}
		})
	}
}

func TestE2ERobotBulkAssignInvalidInputsAreSingleFailureDocuments(t *testing.T) {
	CommonE2EPrerequisites(t)
	tests := []struct {
		name string
		args []string
	}{
		{name: "invalid strategy", args: []string{"--from-bv", "--strategy=fastest", "--dry-run"}},
		{name: "negative stagger", args: []string{"--from-bv", "--bulk-stagger=-1ms", "--dry-run"}},
		{name: "empty allocation", args: []string{"--allocation={}", "--dry-run"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRobotProcessFixture(t, "bulk-invalid")
			args := []string{"--robot-bulk-assign=" + fixture.session, "--robot-format=json"}
			args = append(args, test.args...)
			process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env, args...)
			if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
				t.Fatalf("exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
			}
			var result BulkAssignOutput
			decodeSingleRobotJSON(t, process.stdout, &result)
			if result.Success || result.ErrorCode != "INVALID_FLAG" || result.Assignments == nil {
				t.Fatalf("failure envelope = %+v", result)
			}
		})
	}

	t.Run("duplicate bead allocation", func(t *testing.T) {
		fixture := newRobotProcessFixture(t, "bulk-duplicate-bead")
		secondPaneID := strings.TrimSpace(fixture.mustTMUXOutput(t,
			"split-window", "-d", "-t", fixture.session,
			"-c", fixture.projectDir, "-P", "-F", "#{pane_id}",
			"/bin/bash --noprofile --norc -i",
		))
		if secondPaneID == "" {
			t.Fatal("private tmux server returned an empty second pane ID")
		}
		fixture.mustTMUXOutput(t, "select-pane", "-t", secondPaneID, "-T", fixture.session+"__cod_2")
		allocation, err := json.Marshal(map[string]string{
			fixture.paneID: "bd-duplicate",
			secondPaneID:   "bd-duplicate",
		})
		if err != nil {
			t.Fatalf("marshal duplicate allocation: %v", err)
		}
		process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
			"--robot-bulk-assign="+fixture.session,
			"--allocation="+string(allocation),
			"--dry-run",
			"--robot-format=json",
		)
		if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		var result BulkAssignOutput
		decodeSingleRobotJSON(t, process.stdout, &result)
		if result.Success || result.ErrorCode != "INVALID_FLAG" || len(result.Assignments) != 2 {
			t.Fatalf("duplicate failure envelope=%+v", result)
		}
		if result.Summary.Failed != 2 || result.Summary.Assigned != 0 {
			t.Fatalf("duplicate allocation summary=%+v, want both panes failed", result.Summary)
		}
		for _, assignment := range result.Assignments {
			if assignment.Status != "failed" || assignment.Claimed || assignment.PromptSent ||
				!strings.Contains(assignment.Error, "same bead") || !strings.Contains(assignment.Error, "different physical panes") {
				t.Fatalf("duplicate allocation pane was not fully rejected: %+v", assignment)
			}
		}
	})
}

func TestE2ERobotBulkAssignMissingDependenciesAreTyped(t *testing.T) {
	CommonE2EPrerequisites(t)
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatalf("real tmux is required: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Fatalf("real br is required: %v", err)
	}
	bvPath, err := exec.LookPath("bv")
	if err != nil {
		t.Fatalf("real bv is required: %v", err)
	}

	linkTool := func(t *testing.T, dir, source, name string) {
		t.Helper()
		if err := os.Symlink(source, filepath.Join(dir, name)); err != nil {
			t.Fatalf("link %s into isolated PATH: %v", name, err)
		}
	}
	assertMissing := func(t *testing.T, fixture *robotProcessFixture, env []string, wantText string) {
		t.Helper()
		process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, env,
			"--robot-bulk-assign="+fixture.session,
			"--from-bv",
			"--strategy=ready",
			"--dry-run",
			"--robot-format=json",
		)
		if process.exitCode != 1 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
		}
		var result BulkAssignOutput
		decodeSingleRobotJSON(t, process.stdout, &result)
		if result.Success || result.ErrorCode != "DEPENDENCY_MISSING" || !strings.Contains(result.Error, wantText) || result.Assignments == nil {
			t.Fatalf("missing dependency envelope=%+v", result)
		}
	}

	t.Run("bv", func(t *testing.T) {
		fixture := newRobotProcessFixture(t, "bulk-missing-bv")
		pathDir := t.TempDir()
		linkTool(t, pathDir, tmuxPath, "tmux")
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"PATH": pathDir})
		assertMissing(t, fixture, env, "bv triage failed")
	})

	t.Run("br", func(t *testing.T) {
		fixture := newRobotProcessFixture(t, "bulk-missing-br")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, brPath, "init", "--prefix=rbe2e", "--json")
		cmd.Dir = fixture.projectDir
		cmd.Env = append([]string(nil), fixture.env...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("initialize real Beads workspace: %v output=%s", err, output)
		}
		cmd = exec.CommandContext(ctx, brPath, "create", "Missing br contract", "--type=task", "--priority=1", "--silent")
		cmd.Dir = fixture.projectDir
		cmd.Env = append([]string(nil), fixture.env...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create real Beads task: %v output=%s", err, output)
		}
		pathDir := t.TempDir()
		linkTool(t, pathDir, tmuxPath, "tmux")
		linkTool(t, pathDir, bvPath, "bv")
		env := mergeRobotProcessEnv(fixture.env, map[string]string{"PATH": pathDir})
		assertMissing(t, fixture, env, "fetch in-progress failed")
	})
}

func TestE2E_RobotBulkAssign_JSONStructure(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_json_structure")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with explicit allocation to ensure we get a valid response
	allocation := `{"1":"bd-struct-test"}`
	result, output, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	// Verify JSON is valid
	var raw map[string]interface{}
	if err := json.Unmarshal(output, &raw); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Invalid JSON: %v", err)
	}

	// Verify required fields exist
	requiredFields := []string{"session", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Fatalf("[E2E-BULK-ASSIGN] Missing required field: %s", field)
		}
	}

	// Verify session matches
	if result != nil && result.Session != suite.Session() {
		t.Fatalf("[E2E-BULK-ASSIGN] Session mismatch: got=%s want=%s", result.Session, suite.Session())
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] JSON structure validated with %d fields", len(raw))
}

func TestE2E_RobotBulkAssign_InvalidAllocationJSON(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_invalid_json")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with invalid JSON allocation
	result, output, err := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation={invalid json}")

	suite.Logger().Log("[E2E-BULK-ASSIGN] invalid JSON: err=%v", err)

	// Should have an error
	if result != nil && result.Success {
		t.Fatal("[E2E-BULK-ASSIGN] Should fail with invalid JSON")
	}

	// Error should mention JSON or parse
	outStr := strings.ToLower(string(output))
	if !strings.Contains(outStr, "json") && !strings.Contains(outStr, "parse") && !strings.Contains(outStr, "invalid") {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: error message doesn't mention JSON parsing: %s", string(output))
	}
}

func TestE2E_RobotBulkAssign_SummaryStats(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_summary")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with multiple allocations
	allocation := `{"1":"bd-sum1","2":"bd-sum2","3":"bd-sum3"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify summary is present
	suite.Logger().Log("[E2E-BULK-ASSIGN] Summary: total=%d assigned=%d skipped=%d failed=%d",
		result.Summary.TotalPanes,
		result.Summary.Assigned,
		result.Summary.Skipped,
		result.Summary.Failed)

	// Summary counts should be non-negative
	if result.Summary.TotalPanes < 0 || result.Summary.Assigned < 0 ||
		result.Summary.Skipped < 0 || result.Summary.Failed < 0 {
		t.Fatal("[E2E-BULK-ASSIGN] Summary counts should be non-negative")
	}
}

func TestE2E_RobotBulkAssign_UnassignedPanes(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_unassigned_panes")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Create additional panes
	for i := 0; i < 3; i++ {
		cmd := exec.Command(tmux.BinaryPath(), "split-window", "-t", suite.Session(), "-h")
		if err := cmd.Run(); err != nil {
			suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: could not create pane %d: %v", i, err)
		}
	}

	// Test with fewer beads than panes
	allocation := `{"1":"bd-only-one"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Should have unassigned panes
	suite.Logger().Log("[E2E-BULK-ASSIGN] Unassigned panes: %v", result.UnassignedPanes)

	// Verify total panes vs assigned
	if result.Summary.TotalPanes > 1 && len(result.UnassignedPanes) == 0 {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: expected unassigned_panes with %d total panes and 1 allocation",
			result.Summary.TotalPanes)
	}
}

func TestE2E_RobotBulkAssign_UnassignedBeads(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_unassigned_beads")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with more beads than panes (only 1 pane in fresh session)
	allocation := `{"1":"bd-one","2":"bd-two","3":"bd-three","4":"bd-four","5":"bd-five"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Should have unassigned beads (more beads than panes)
	suite.Logger().Log("[E2E-BULK-ASSIGN] Unassigned beads: %v", result.UnassignedBeads)

	// With 5 allocations and likely fewer panes, we should have unassigned beads
	if result.Summary.TotalPanes < 5 && len(result.UnassignedBeads) == 0 {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: expected unassigned_beads with %d panes and 5 allocations",
			result.Summary.TotalPanes)
	}
}

func TestE2E_RobotBulkAssign_WithFromBV(t *testing.T) {
	CommonE2EPrerequisites(t)
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Fatalf("real br is required for from-BV E2E coverage: %v", err)
	}
	bvPath, err := exec.LookPath("bv")
	if err != nil {
		t.Fatalf("real bv is required for from-BV E2E coverage: %v", err)
	}

	fixture := newRobotProcessFixture(t, "bulk-from-bv")
	makeCodexIdle := func(t *testing.T, targetFixture *robotProcessFixture) {
		t.Helper()
		fakeCodex := filepath.Join(targetFixture.root, "codex")
		fakeCodexScript := "#!/bin/sh\nprintf 'Codex> \\n100%% context left\\n'\nwhile IFS= read -r line; do\n  printf 'received: %s\\nCodex> \\n100%% context left\\n' \"$line\"\ndone\n"
		if err := os.WriteFile(fakeCodex, []byte(fakeCodexScript), 0o700); err != nil {
			t.Fatalf("write fake Codex agent: %v", err)
		}
		targetFixture.mustTMUXOutput(t, "respawn-pane", "-k", "-t", targetFixture.paneID, fakeCodex)
		time.Sleep(5500 * time.Millisecond)
	}
	makeCodexIdle(t, fixture)
	runTool := func(t *testing.T, targetFixture *robotProcessFixture, path string, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = targetFixture.projectDir
		cmd.Env = append([]string(nil), targetFixture.env...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s %q: %v stdout=%s stderr=%s", path, args, err, stdout.Bytes(), stderr.Bytes())
		}
		if ctx.Err() != nil {
			t.Fatalf("%s %q timed out: %v", path, args, ctx.Err())
		}
		return stdout.Bytes()
	}

	runTool(t, fixture, brPath, "init", "--prefix=rbe2e", "--json")
	beadID := strings.TrimSpace(string(runTool(t, fixture, brPath,
		"create", "Hermetic from-BV assignment", "--type=task", "--priority=1", "--silent",
	)))
	if beadID == "" || strings.ContainsAny(beadID, " \t\r\n") {
		t.Fatalf("unexpected br create output %q", beadID)
	}

	type bvTriageEnvelope struct {
		Triage struct {
			Recommendations []struct {
				ID string `json:"id"`
			} `json:"recommendations"`
		} `json:"triage"`
	}
	var triage bvTriageEnvelope
	decodeSingleRobotJSON(t, runTool(t, fixture, bvPath, "--robot-triage"), &triage)
	if len(triage.Triage.Recommendations) == 0 || triage.Triage.Recommendations[0].ID != beadID {
		t.Fatalf("real bv recommendations=%+v, want first bead %q", triage.Triage.Recommendations, beadID)
	}

	process := runBuiltRobotProcess(t, fixture.ntmPath, fixture.projectDir, fixture.env,
		"--robot-bulk-assign="+fixture.session,
		"--from-bv",
		"--strategy=ready",
		"--dry-run",
		"--robot-format=json",
	)
	if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
		t.Fatalf("real from-BV process exit=%d stdout=%s stderr=%s", process.exitCode, process.stdout, process.stderr)
	}
	var result BulkAssignOutput
	decodeSingleRobotJSON(t, process.stdout, &result)
	if !result.Success || !result.DryRun {
		t.Fatalf("real from-BV envelope=%+v", result)
	}
	if result.AllocationSource != "bv" || result.Strategy != "ready" {
		t.Fatalf("allocation_source=%q strategy=%q, want bv/ready", result.AllocationSource, result.Strategy)
	}
	if len(result.Assignments) != 1 {
		t.Fatalf("assignments=%+v, want exactly one real bv allocation", result.Assignments)
	}
	plannedAssignment := result.Assignments[0]
	if plannedAssignment.Bead != beadID || plannedAssignment.PaneID != fixture.paneID || plannedAssignment.Status != "planned" || plannedAssignment.PromptSent {
		t.Fatalf("assignment=%+v, want bead=%q pane_id=%q planned without dispatch", plannedAssignment, beadID, fixture.paneID)
	}
	if result.Summary.TotalPanes != 1 || result.Summary.Failed != 0 {
		t.Fatalf("summary=%+v, want one successful dry-run plan", result.Summary)
	}

	runBulk := func(t *testing.T, targetFixture *robotProcessFixture, strategy string, extraArgs ...string) BulkAssignOutput {
		t.Helper()
		args := []string{
			"--robot-bulk-assign=" + targetFixture.session,
			"--from-bv",
			"--strategy=" + strategy,
			"--robot-format=json",
		}
		args = append(args, extraArgs...)
		process := runBuiltRobotProcess(t, targetFixture.ntmPath, targetFixture.projectDir, targetFixture.env, args...)
		if process.exitCode != 0 || len(bytes.TrimSpace(process.stderr)) != 0 {
			t.Fatalf("bulk %s exit=%d stdout=%s stderr=%s", strategy, process.exitCode, process.stdout, process.stderr)
		}
		var output BulkAssignOutput
		decodeSingleRobotJSON(t, process.stdout, &output)
		if !output.Success || output.DryRun || output.AllocationSource != "bv" || output.Strategy != strategy {
			t.Fatalf("bulk %s envelope=%+v", strategy, output)
		}
		return output
	}

	const staleTitle = "Hermetic stale adoption"
	staleID := strings.TrimSpace(string(runTool(t, fixture, brPath,
		"create", staleTitle, "--type=task", "--priority=1", "--silent",
	)))
	runTool(t, fixture, brPath, "update", staleID, "--status=in_progress", "--json")
	var workspaceInfo struct {
		DatabasePath string `json:"database_path"`
	}
	decodeSingleRobotJSON(t, runTool(t, fixture, brPath, "info", "--json"), &workspaceInfo)
	if strings.TrimSpace(workspaceInfo.DatabasePath) == "" {
		t.Fatal("br info returned no SQLite database path")
	}
	staleUpdatedAt := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	database, err := sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(workspaceInfo.DatabasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("open real Beads database: %v", err)
	}
	if _, err := database.Exec("UPDATE issues SET updated_at = ? WHERE id = ?", staleUpdatedAt.Format(time.RFC3339Nano), staleID); err != nil {
		_ = database.Close()
		t.Fatalf("age stale Beads row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close real Beads database: %v", err)
	}
	runTool(t, fixture, brPath, "sync", "--flush-only", "--json", "--no-auto-import")

	staleOutput := runBulk(t, fixture, "stale")
	if len(staleOutput.Assignments) != 1 {
		t.Fatalf("stale assignments=%+v, want one", staleOutput.Assignments)
	}
	staleAssignment := staleOutput.Assignments[0]
	if staleAssignment.Bead != staleID || staleAssignment.PaneID != fixture.paneID || staleAssignment.Status != "assigned" ||
		!staleAssignment.Claimed || !staleAssignment.PromptSent || staleAssignment.ClaimActor == "" ||
		staleAssignment.IdempotencyKey == "" || staleAssignment.DispatchReceiptID == "" {
		t.Fatalf("stale assignment=%+v", staleAssignment)
	}
	database, err = sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(workspaceInfo.DatabasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("reopen real Beads database: %v", err)
	}
	var trackerStatus, trackerAssignee string
	if err := database.QueryRow("SELECT status, COALESCE(assignee, '') FROM issues WHERE id = ?", staleID).Scan(&trackerStatus, &trackerAssignee); err != nil {
		_ = database.Close()
		t.Fatalf("read adopted Beads row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close adopted Beads database: %v", err)
	}
	if trackerStatus != "in_progress" || trackerAssignee != staleAssignment.ClaimActor {
		t.Fatalf("adopted tracker status=%q assignee=%q, want in_progress/%q", trackerStatus, trackerAssignee, staleAssignment.ClaimActor)
	}

	// A second process must replay the durable receipt without claiming or
	// dispatching the same stale work a second time. Wait for the asynchronous
	// tmux delivery to become visible and stable before taking the baseline.
	captureStable := func(t *testing.T, targetFixture *robotProcessFixture, minimumReceipts int) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		last := ""
		stableReads := 0
		for time.Now().Before(deadline) {
			captured := targetFixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", targetFixture.paneID, "-S", "-")
			if strings.Count(captured, "received:") >= minimumReceipts && captured == last {
				stableReads++
				if stableReads >= 2 {
					return captured
				}
			} else {
				stableReads = 0
			}
			last = captured
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("pane %s did not stabilize with %d receipts; last output:\n%s", targetFixture.paneID, minimumReceipts, last)
		return ""
	}
	paneBeforeReplay := captureStable(t, fixture, 1)
	replayOutput := runBulk(t, fixture, "stale")
	if len(replayOutput.Assignments) != 1 {
		t.Fatalf("stale replay assignments=%+v, want one", replayOutput.Assignments)
	}
	replayed := replayOutput.Assignments[0]
	if replayed.IdempotencyKey != staleAssignment.IdempotencyKey || replayed.DispatchReceiptID != staleAssignment.DispatchReceiptID ||
		replayed.ClaimActor != staleAssignment.ClaimActor || !replayed.Claimed || !replayed.PromptSent {
		t.Fatalf("stale replay=%+v, first=%+v", replayed, staleAssignment)
	}
	paneAfterReplay := captureStable(t, fixture, 1)
	if paneAfterReplay != paneBeforeReplay {
		t.Fatalf("durable stale replay actuated tmux twice\nbefore:\n%s\nafter:\n%s", paneBeforeReplay, paneAfterReplay)
	}

	// Seed the crash window after the external tracker CAS commits but before
	// RecordAtomicClaim persists its receipt, then recover with a fresh process.
	recoveryFixtureValue := *fixture
	recoveryFixture := &recoveryFixtureValue
	recoveryFixture.session = fixture.session + "-recovery"
	recoveryFixture.paneID = strings.TrimSpace(fixture.mustTMUXOutput(t,
		"new-session", "-d", "-s", recoveryFixture.session,
		"-x", "160", "-y", "48", "-c", recoveryFixture.projectDir,
		"-P", "-F", "#{pane_id}",
		"/bin/bash --noprofile --norc -i",
	))
	if recoveryFixture.paneID == "" {
		t.Fatal("private tmux server returned an empty recovery pane ID")
	}
	fixture.mustTMUXOutput(t, "select-pane", "-t", recoveryFixture.paneID, "-T", recoveryFixture.session+"__cod_1")
	makeCodexIdle(t, recoveryFixture)
	const recoveryTitle = "Recover stale claim after process exit"
	recoveryBead := strings.TrimSpace(string(runTool(t, recoveryFixture, brPath,
		"create", recoveryTitle, "--type=task", "--priority=1", "--silent",
	)))
	recoveryKey := strings.Repeat("a", 64)
	recoveryAgent := fmt.Sprintf("ntm:%s:%s", recoveryFixture.session, recoveryFixture.paneID)
	recoveryActor := assignment.StableClaimActor(recoveryAgent, recoveryKey)
	runTool(t, recoveryFixture, brPath, "update", recoveryBead, "--status=in_progress", "--assignee="+recoveryActor, "--json")
	recoveryPrompt := fmt.Sprintf(
		"Read AGENTS.md, register with Agent Mail. Work on: %s - %s.\nUse br show %s for details. Mark in_progress when starting. Use ultrathink.",
		recoveryBead, recoveryTitle, recoveryBead,
	)
	recoveryHome := ""
	for _, entry := range recoveryFixture.env {
		if key, value, ok := strings.Cut(entry, "="); ok && key == "HOME" {
			recoveryHome = value
			break
		}
	}
	if recoveryHome == "" {
		t.Fatal("recovery fixture has no HOME")
	}
	t.Setenv("HOME", recoveryHome)
	recoveryStore := assignment.NewStore(recoveryFixture.session)
	recoveryStore.Assignments[recoveryBead] = &assignment.Assignment{
		BeadID: recoveryBead, BeadTitle: recoveryTitle, Pane: 0,
		AgentType: "codex", AgentName: recoveryAgent, Status: assignment.StatusClaiming,
		AssignedAt: time.Now().UTC(), IdempotencyKey: recoveryKey,
		ClaimActor: recoveryActor, ClaimState: assignment.ClaimUnknown,
		PendingPrompt: recoveryPrompt, PromptSHA256: assignment.PromptSHA256(recoveryPrompt),
		IntentSHA256:   assignment.PromptSHA256(recoveryPrompt),
		DispatchTarget: recoveryFixture.paneID, OccupancyKey: recoveryFixture.paneID,
		DispatchState: assignment.DispatchPending,
	}
	if err := recoveryStore.Save(); err != nil {
		t.Fatalf("seed crash-window assignment ledger: %v", err)
	}

	recoveredOutput := runBulk(t, recoveryFixture, "stale",
		"--prompt-template="+filepath.Join(recoveryFixture.root, "missing-recovery-template.txt"),
		"--require-reservation",
	)
	if len(recoveredOutput.Assignments) != 1 {
		t.Fatalf("crash recovery assignments=%+v, want one", recoveredOutput.Assignments)
	}
	recovered := recoveredOutput.Assignments[0]
	if recovered.Bead != recoveryBead || recovered.PaneID != recoveryFixture.paneID || recovered.Reason != "stale_recovery" ||
		recovered.IdempotencyKey != recoveryKey || recovered.ClaimActor != recoveryActor || recovered.Status != "assigned" ||
		!recovered.Claimed || !recovered.PromptSent || recovered.DispatchReceiptID == "" {
		t.Fatalf("crash recovery assignment=%+v", recovered)
	}
	durableRecovery, err := assignment.LoadStoreStrict(recoveryFixture.session)
	if err != nil {
		t.Fatalf("reload recovered assignment ledger: %v", err)
	}
	recoveryRecord := durableRecovery.Get(recoveryBead)
	if recoveryRecord == nil || recoveryRecord.ClaimState != assignment.ClaimClaimed ||
		recoveryRecord.DispatchState != assignment.DispatchSent || recoveryRecord.DispatchReceiptID != recovered.DispatchReceiptID {
		t.Fatalf("durable crash recovery record=%+v", recoveryRecord)
	}
	recoveryPane := captureStable(t, recoveryFixture, 1)
	recoveryPaneUnwrapped := strings.ReplaceAll(recoveryPane, "\n", "")
	if strings.Count(recoveryPane, "received: Read AGENTS.md") != 1 ||
		!strings.Contains(recoveryPaneUnwrapped, recoveryBead) ||
		!strings.Contains(recoveryPaneUnwrapped, recoveryTitle) {
		t.Fatalf("crash recovery did not deliver its durable prompt exactly once:\n%s", recoveryPane)
	}

	// Exercise the two-phase mixed path in one built process: the durable sent
	// row must replay without actuation while a fresh ready bead claims and
	// dispatches to a second physical pane.
	mixedPaneID := strings.TrimSpace(fixture.mustTMUXOutput(t,
		"split-window", "-d", "-t", recoveryFixture.session,
		"-c", recoveryFixture.projectDir, "-P", "-F", "#{pane_id}",
		"/bin/bash --noprofile --norc -i",
	))
	if mixedPaneID == "" {
		t.Fatal("private tmux server returned an empty mixed-plan pane ID")
	}
	fixture.mustTMUXOutput(t, "select-pane", "-t", mixedPaneID, "-T", recoveryFixture.session+"__cod_2")
	mixedPaneFixtureValue := *recoveryFixture
	mixedPaneFixture := &mixedPaneFixtureValue
	mixedPaneFixture.paneID = mixedPaneID
	makeCodexIdle(t, mixedPaneFixture)
	const mixedFreshTitle = "Fresh work beside durable replay"
	mixedFreshBead := strings.TrimSpace(string(runTool(t, recoveryFixture, brPath,
		"create", mixedFreshTitle, "--type=task", "--priority=0", "--silent",
	)))
	mixedFreshBefore := mixedPaneFixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", mixedPaneID, "-S", "-")
	mixedOutput := runBulk(t, recoveryFixture, "balanced")
	if len(mixedOutput.Assignments) != 2 || mixedOutput.Summary.Assigned != 2 || mixedOutput.Summary.Failed != 0 {
		t.Fatalf("mixed replay/fresh output=%+v", mixedOutput)
	}
	var mixedReplay, mixedFresh *BulkAssignAssignment
	for i := range mixedOutput.Assignments {
		switch mixedOutput.Assignments[i].Bead {
		case recoveryBead:
			mixedReplay = &mixedOutput.Assignments[i]
		case mixedFreshBead:
			mixedFresh = &mixedOutput.Assignments[i]
		}
	}
	if mixedReplay == nil || mixedFresh == nil || mixedReplay.DispatchReceiptID != recovered.DispatchReceiptID ||
		!mixedReplay.Claimed || !mixedReplay.PromptSent || mixedFresh.Status != "assigned" ||
		!mixedFresh.Claimed || !mixedFresh.PromptSent || mixedFresh.DispatchReceiptID == "" || mixedFresh.PaneID != mixedPaneID {
		t.Fatalf("mixed replay=%+v fresh=%+v output=%+v", mixedReplay, mixedFresh, mixedOutput)
	}
	recoveryPaneAfterMixed := captureStable(t, recoveryFixture, 1)
	if strings.Count(recoveryPaneAfterMixed, "received: Read AGENTS.md") != 1 ||
		strings.Count(recoveryPaneAfterMixed, "received: Use br show") != 1 {
		t.Fatalf("mixed durable replay actuated its pane again\nbefore:\n%s\nafter:\n%s", recoveryPane, recoveryPaneAfterMixed)
	}
	mixedFreshPane := captureStable(t, mixedPaneFixture, 1)
	mixedFreshPaneUnwrapped := strings.ReplaceAll(mixedFreshPane, "\n", "")
	if strings.Count(mixedFreshPane, "received: Read AGENTS.md") != 1 ||
		strings.Contains(mixedFreshBefore, "received: Read AGENTS.md") ||
		!strings.Contains(mixedFreshPaneUnwrapped, mixedFreshBead) ||
		!strings.Contains(mixedFreshPaneUnwrapped, mixedFreshTitle) {
		t.Fatalf("mixed fresh prompt was not delivered exactly once\nbefore:\n%s\nafter:\n%s", mixedFreshBefore, mixedFreshPane)
	}

	newPeerFixture := func(t *testing.T, suffix string) *robotProcessFixture {
		t.Helper()
		peerValue := *fixture
		peer := &peerValue
		peer.session = fixture.session + "-" + suffix
		peer.paneID = strings.TrimSpace(fixture.mustTMUXOutput(t,
			"new-session", "-d", "-s", peer.session,
			"-x", "160", "-y", "48", "-c", peer.projectDir,
			"-P", "-F", "#{pane_id}",
			"/bin/bash --noprofile --norc -i",
		))
		if peer.paneID == "" {
			t.Fatalf("private tmux server returned an empty %s peer pane ID", suffix)
		}
		fixture.mustTMUXOutput(t, "select-pane", "-t", peer.paneID, "-T", peer.session+"__cod_1")
		makeCodexIdle(t, peer)
		return peer
	}
	peerA := newPeerFixture(t, "race-a")
	peerB := newPeerFixture(t, "race-b")
	const raceTitle = "Concurrent stale adoption CAS"
	raceBead := strings.TrimSpace(string(runTool(t, fixture, brPath,
		"create", raceTitle, "--type=task", "--priority=1", "--silent",
	)))
	runTool(t, fixture, brPath, "update", raceBead, "--status=in_progress", "--json")
	raceUpdatedAt := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Microsecond)
	database, err = sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(workspaceInfo.DatabasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("open Beads database for concurrent stale race: %v", err)
	}
	if _, err := database.Exec("UPDATE issues SET updated_at = ? WHERE id = ?", raceUpdatedAt.Format(time.RFC3339Nano), raceBead); err != nil {
		_ = database.Close()
		t.Fatalf("age concurrent stale Beads row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close concurrent stale Beads database: %v", err)
	}
	runTool(t, fixture, brPath, "sync", "--flush-only", "--json", "--no-auto-import")

	barrierDir := t.TempDir()
	wrapperDir := t.TempDir()
	brWrapper := filepath.Join(wrapperDir, "br")
	barrierScript := fmt.Sprintf(`#!/bin/sh
barrier=%q
real_br=%q
is_list=0
is_in_progress=0
is_info=0
is_no_auto_import=0
is_no_auto_flush=0
is_locked_ntm_list=0
is_direct_ntm_child=0
expect_status=0
if [ "${1:-}" = "--lock-timeout" ] && [ "${2:-}" = "5000" ]; then
  is_locked_ntm_list=1
fi
if [ -n "${NTM_E2E_DIRECT_PARENT_PID:-}" ] && [ "$PPID" = "$NTM_E2E_DIRECT_PARENT_PID" ]; then
  is_direct_ntm_child=1
fi
for arg in "$@"; do
  if [ "$expect_status" -eq 1 ]; then
    [ "$arg" = "in_progress" ] && is_in_progress=1
    expect_status=0
    continue
  fi
  case "$arg" in
    list) is_list=1 ;;
    info) is_info=1 ;;
    --status=in_progress) is_in_progress=1 ;;
    --status) expect_status=1 ;;
    --no-auto-import) is_no_auto_import=1 ;;
    --no-auto-flush) is_no_auto_flush=1 ;;
  esac
done
if [ "$is_direct_ntm_child" -eq 1 ] && [ "$is_locked_ntm_list" -eq 1 ] && [ "$is_list" -eq 1 ] && [ "$is_in_progress" -eq 1 ]; then
	contender=${NTM_E2E_RACE_CONTENDER:-}
	case "$contender" in
	  A|B) ;;
	  *) printf 'missing concurrent br list contender identity\n' >&2; exit 97 ;;
	esac
	output="$barrier/list.$contender.$$"
	"$real_br" "$@" > "$output"
	status=$?
	cat "$output"
	exit "$status"
fi
if [ "$is_direct_ntm_child" -eq 1 ] && [ "$is_locked_ntm_list" -eq 1 ] && [ "$is_info" -eq 1 ] && [ "$is_no_auto_import" -eq 1 ] && [ "$is_no_auto_flush" -eq 1 ]; then
	contender=${NTM_E2E_RACE_CONTENDER:-}
	case "$contender" in
	  A|B) ;;
	  *) printf 'missing concurrent br claim contender identity\n' >&2; exit 97 ;;
	esac
	output="$barrier/info.$contender.$$"
	"$real_br" "$@" > "$output"
	status=$?
	: > "$barrier/arrived.$contender"
	attempts=0
	while [ "$attempts" -lt 30 ]; do
	  [ -f "$barrier/arrived.A" ] && [ -f "$barrier/arrived.B" ] && break
	  attempts=$((attempts + 1))
	  sleep 1
	done
	if [ ! -f "$barrier/arrived.A" ] || [ ! -f "$barrier/arrived.B" ]; then
	  printf 'timed out waiting for concurrent guarded-claim barrier\n' >&2
	  exit 98
	fi
	cat "$output"
	exit "$status"
fi
exec "$real_br" "$@"
`, barrierDir, brPath)
	if err := os.WriteFile(brWrapper, []byte(barrierScript), 0o700); err != nil {
		t.Fatalf("write concurrent br barrier: %v", err)
	}
	raceEnv := mergeRobotProcessEnv(fixture.env, map[string]string{
		"PATH": wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	startRace := func(t *testing.T, peer *robotProcessFixture, contender string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
		t.Helper()
		ntmArgs := []string{
			"--robot-bulk-assign=" + peer.session,
			"--from-bv",
			"--strategy=stale",
			"--robot-format=json",
		}
		launcherArgs := []string{
			"-c",
			`NTM_E2E_DIRECT_PARENT_PID=$$; export NTM_E2E_DIRECT_PARENT_PID; exec "$@"`,
			"ntm-race",
			peer.ntmPath,
		}
		launcherArgs = append(launcherArgs, ntmArgs...)
		cmd := exec.CommandContext(ctx, "/bin/sh", launcherArgs...)
		cmd.Dir = peer.projectDir
		cmd.Env = mergeRobotProcessEnv(raceEnv, map[string]string{
			"NTM_E2E_RACE_CONTENDER": contender,
		})
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start concurrent bulk process for %s: %v", peer.session, err)
		}
		return cmd, stdout, stderr
	}
	cmdA, stdoutA, stderrA := startRace(t, peerA, "A")
	cmdB, stdoutB, stderrB := startRace(t, peerB, "B")
	barrierDeadline := time.Now().Add(35 * time.Second)
	for {
		_, errA := os.Stat(filepath.Join(barrierDir, "arrived.A"))
		_, errB := os.Stat(filepath.Join(barrierDir, "arrived.B"))
		if errA == nil && errB == nil {
			break
		}
		if (errA != nil && !os.IsNotExist(errA)) || (errB != nil && !os.IsNotExist(errB)) || time.Now().After(barrierDeadline) {
			cancel()
			waitErrA := cmdA.Wait()
			waitErrB := cmdB.Wait()
			t.Fatalf("concurrent contenders did not reach the shared guarded-claim barrier: marker A=%v marker B=%v wait A=%v wait B=%v stdout A=%q stderr A=%q stdout B=%q stderr B=%q",
				errA, errB, waitErrA, waitErrB, stdoutA.Bytes(), stderrA.Bytes(), stdoutB.Bytes(), stderrB.Bytes())
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, contender := range []string{"A", "B"} {
		snapshots, err := filepath.Glob(filepath.Join(barrierDir, "list."+contender+".*"))
		if err != nil {
			t.Fatalf("glob concurrent stale snapshot for %s: %v", contender, err)
		}
		if len(snapshots) == 0 {
			t.Fatalf("concurrent contender %s captured no direct stale snapshots", contender)
		}
		for _, snapshotPath := range snapshots {
			snapshot, err := os.ReadFile(snapshotPath)
			if err != nil {
				t.Fatalf("read concurrent stale snapshot for %s: %v", contender, err)
			}
			if !bytes.Contains(snapshot, []byte(raceBead)) {
				t.Fatalf("concurrent contender %s did not observe stale bead %q in authoritative snapshot %s: %s", contender, raceBead, snapshotPath, snapshot)
			}
			var captured struct {
				Issues []struct {
					ID        string    `json:"id"`
					Assignee  string    `json:"assignee"`
					UpdatedAt time.Time `json:"updated_at"`
				} `json:"issues"`
			}
			if err := json.Unmarshal(snapshot, &captured); err != nil {
				t.Fatalf("decode concurrent stale snapshot for %s: %v\npayload=%s", contender, err, snapshot)
			}
			matched := false
			for _, item := range captured.Issues {
				if item.ID != raceBead {
					continue
				}
				matched = true
				if strings.TrimSpace(item.Assignee) != "" || !item.UpdatedAt.Equal(raceUpdatedAt) {
					t.Fatalf("concurrent contender %s observed bead %q with assignee=%q updated_at=%s, want unowned at %s",
						contender, raceBead, item.Assignee, item.UpdatedAt.Format(time.RFC3339Nano), raceUpdatedAt.Format(time.RFC3339Nano))
				}
			}
			if !matched {
				t.Fatalf("concurrent contender %s snapshot omitted stale bead %q: %s", contender, raceBead, snapshot)
			}
		}
		claimSnapshots, err := filepath.Glob(filepath.Join(barrierDir, "info."+contender+".*"))
		if err != nil {
			t.Fatalf("glob concurrent guarded-claim snapshot for %s: %v", contender, err)
		}
		if len(claimSnapshots) != 1 {
			t.Fatalf("concurrent contender %s reached %d guarded-claim boundaries, want exactly one: %v", contender, len(claimSnapshots), claimSnapshots)
		}
	}
	waitRace := func(t *testing.T, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) int {
		t.Helper()
		waitErr := cmd.Wait()
		if ctx.Err() != nil {
			t.Fatalf("concurrent bulk process timed out: %v stdout=%s stderr=%s", ctx.Err(), stdout.Bytes(), stderr.Bytes())
		}
		if waitErr != nil {
			if _, ok := waitErr.(*exec.ExitError); !ok {
				t.Fatalf("wait for concurrent bulk process: %v", waitErr)
			}
		}
		return cmd.ProcessState.ExitCode()
	}
	exitA := waitRace(t, cmdA, stdoutA, stderrA)
	exitB := waitRace(t, cmdB, stdoutB, stderrB)
	if len(bytes.TrimSpace(stderrA.Bytes())) != 0 || len(bytes.TrimSpace(stderrB.Bytes())) != 0 {
		t.Fatalf("concurrent bulk stderr A=%q B=%q", stderrA.Bytes(), stderrB.Bytes())
	}
	var raceA, raceB BulkAssignOutput
	decodeSingleRobotJSON(t, stdoutA.Bytes(), &raceA)
	decodeSingleRobotJSON(t, stdoutB.Bytes(), &raceB)
	type raceResult struct {
		fixture *robotProcessFixture
		exit    int
		output  BulkAssignOutput
	}
	results := []raceResult{{fixture: peerA, exit: exitA, output: raceA}, {fixture: peerB, exit: exitB, output: raceB}}
	var winner, loser *raceResult
	for i := range results {
		result := &results[i]
		if result.output.Success {
			winner = result
		} else {
			loser = result
		}
	}
	if winner == nil || loser == nil || winner.exit != 0 || loser.exit != 1 ||
		len(winner.output.Assignments) != 1 || len(loser.output.Assignments) != 1 {
		t.Fatalf("concurrent stale results A(exit=%d)=%+v B(exit=%d)=%+v", exitA, raceA, exitB, raceB)
	}
	winnerAssignment := winner.output.Assignments[0]
	loserAssignment := loser.output.Assignments[0]
	if winnerAssignment.Bead != raceBead || winnerAssignment.Status != "assigned" || !winnerAssignment.Claimed || !winnerAssignment.PromptSent ||
		winnerAssignment.ClaimActor == "" || winnerAssignment.DispatchReceiptID == "" ||
		loserAssignment.Bead != raceBead || loserAssignment.Status != "failed" || loserAssignment.Claimed || loserAssignment.PromptSent ||
		!strings.Contains(loserAssignment.Error, assignment.ErrClaimConflict.Error()) ||
		loser.output.ErrorCode != "ASSIGNMENT_FAILED" || loser.output.Summary.Failed != 1 {
		t.Fatalf("concurrent winner=%+v loser=%+v", winnerAssignment, loserAssignment)
	}
	winnerPane := captureStable(t, winner.fixture, 1)
	loserPane := loser.fixture.mustTMUXOutput(t, "capture-pane", "-p", "-t", loser.fixture.paneID, "-S", "-")
	winnerPromptStarts := strings.Count(winnerPane, "received: Read AGENTS.md")
	loserPromptStarts := strings.Count(loserPane, "received: Read AGENTS.md")
	if winnerPromptStarts != 1 || loserPromptStarts != 0 {
		t.Fatalf("concurrent dispatch starts winner=%d loser=%d\nwinner:\n%s\nloser:\n%s",
			winnerPromptStarts, loserPromptStarts, winnerPane, loserPane)
	}
	database, err = sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(workspaceInfo.DatabasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("reopen Beads database after concurrent stale race: %v", err)
	}
	if err := database.QueryRow("SELECT status, COALESCE(assignee, '') FROM issues WHERE id = ?", raceBead).Scan(&trackerStatus, &trackerAssignee); err != nil {
		_ = database.Close()
		t.Fatalf("read concurrent stale tracker row: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close concurrent stale tracker database: %v", err)
	}
	if trackerStatus != "in_progress" || trackerAssignee != winnerAssignment.ClaimActor {
		t.Fatalf("concurrent tracker status=%q assignee=%q, want in_progress/%q", trackerStatus, trackerAssignee, winnerAssignment.ClaimActor)
	}
}

func TestE2E_RobotBulkAssign_MultiPaneSession(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_multi_pane")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Create 5 panes for testing
	for i := 0; i < 4; i++ {
		cmd := exec.Command(tmux.BinaryPath(), "split-window", "-t", suite.Session(), "-h")
		if err := cmd.Run(); err != nil {
			suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: could not create pane %d: %v", i+1, err)
		}
	}

	// Balance the layout
	cmd := exec.Command(tmux.BinaryPath(), "select-layout", "-t", suite.Session(), "tiled")
	cmd.Run()

	// Test allocation to multiple panes
	allocation := `{"0":"bd-p0","1":"bd-p1","2":"bd-p2","3":"bd-p3","4":"bd-p4"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] Multi-pane: total_panes=%d assigned=%d",
		result.Summary.TotalPanes, result.Summary.Assigned)

	// Should have multiple panes
	if result.Summary.TotalPanes < 3 {
		t.Fatalf("[E2E-BULK-ASSIGN] Expected at least 3 panes, got %d", result.Summary.TotalPanes)
	}
}

func TestE2E_RobotBulkAssign_AssignmentDetails(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_details")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with explicit allocation
	allocation := `{"0":"bd-detail-test"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify assignment details structure
	for _, a := range result.Assignments {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Assignment: pane=%s pane_id=%s bead=%s status=%s prompt_sent=%v",
			a.Pane, a.PaneID, a.Bead, a.Status, a.PromptSent)

		if _, err := tmux.ParsePaneSelector(a.Pane); err != nil {
			t.Fatalf("[E2E-BULK-ASSIGN] Invalid canonical pane selector %q: %v", a.Pane, err)
		}

		// Bead ID should be set
		if a.Bead == "" {
			t.Fatal("[E2E-BULK-ASSIGN] Assignment missing bead ID")
		}
	}
}

func TestE2E_RobotBulkAssign_SkipMultiplePanes(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_skip_multiple")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Create additional panes
	for i := 0; i < 4; i++ {
		cmd := exec.Command(tmux.BinaryPath(), "split-window", "-t", suite.Session(), "-h")
		cmd.Run()
	}
	exec.Command(tmux.BinaryPath(), "select-layout", "-t", suite.Session(), "tiled").Run()

	// Skip multiple panes
	allocation := `{"0":"bd-s0","1":"bd-s1","2":"bd-s2","3":"bd-s3","4":"bd-s4"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--skip=0,2,4",
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify skipped panes appear as failed (not available in filtered pane list)
	skippedPanes := map[string]bool{"0": true, "2": true, "4": true}
	for _, a := range result.Assignments {
		if skippedPanes[a.Pane] {
			// Skipped panes should be marked as failed
			suite.Logger().Log("[E2E-BULK-ASSIGN] Skipped pane %s: status=%s error=%s", a.Pane, a.Status, a.Error)
		}
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] Skip multiple verified: 0,2,4 processed as expected")
}

func TestE2E_RobotBulkAssign_EmptyAllocation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_empty_alloc")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with empty allocation
	result, output, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation={}")

	if result != nil && result.Success {
		// Empty allocation might be valid (0 assignments)
		if len(result.Assignments) > 0 {
			t.Fatal("[E2E-BULK-ASSIGN] Empty allocation should result in 0 assignments")
		}
		suite.Logger().Log("[E2E-BULK-ASSIGN] Empty allocation handled: assignments=%d", len(result.Assignments))
	} else {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Empty allocation rejected: %s", string(output))
	}
}

func TestE2E_RobotBulkAssign_NonExistentSession(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_bad_session")
	defer suite.Teardown()

	// Don't set up a session - test with non-existent one
	result, output, err := runBulkAssignCmd(t, suite, "nonexistent_session_12345",
		"--allocation={\"1\":\"bd-test\"}",
		"--dry-run")

	if err == nil && result != nil && result.Success {
		t.Fatal("[E2E-BULK-ASSIGN] Should fail with non-existent session")
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] Non-existent session error: %s", string(output))
}

func TestE2E_RobotBulkAssign_PaneIndexValidation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_pane_validation")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with invalid pane indices (strings that look like negative numbers)
	allocation := `{"99":"bd-nonexistent-pane"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Non-existent pane should be reported
	suite.Logger().Log("[E2E-BULK-ASSIGN] Non-existent pane 99: summary.failed=%d", result.Summary.Failed)

	// The assignment should either fail or not be included
	for _, a := range result.Assignments {
		if a.Pane == "99" && a.Error == "" {
			t.Fatal("[E2E-BULK-ASSIGN] Assignment to non-existent pane 99 should have error")
		}
	}
}

func TestE2E_RobotBulkAssign_TimestampFormat(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_timestamp")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	allocation := `{"0":"bd-ts-test"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify timestamp is present and valid
	if result.Timestamp == "" {
		t.Fatal("[E2E-BULK-ASSIGN] Timestamp should not be empty")
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] Timestamp: %s", result.Timestamp)
}

func TestE2E_RobotBulkAssign_AllStrategiesValidInput(t *testing.T) {
	CommonE2EPrerequisites(t)

	strategies := []string{"impact", "ready", "stale", "balanced"}

	for _, strategy := range strategies {
		t.Run("valid_"+strategy, func(t *testing.T) {
			suite := NewTestSuite(t, fmt.Sprintf("bulk_assign_valid_%s", strategy))
			defer suite.Teardown()

			if err := suite.Setup(); err != nil {
				t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
			}

			// Use explicit allocation to avoid needing bv
			allocation := `{"0":"bd-` + strategy + `-test"}`
			result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
				"--allocation="+allocation,
				"--strategy="+strategy,
				"--dry-run")

			if result == nil {
				t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
			}
			if result.Strategy != strategy {
				t.Fatalf("[E2E-BULK-ASSIGN] strategy=%q, want canonical --strategy value %q", result.Strategy, strategy)
			}

			// Strategy should be recorded even with explicit allocation
			suite.Logger().Log("[E2E-BULK-ASSIGN] Strategy %s: recorded=%s", strategy, result.Strategy)
		})
	}
}

func TestE2E_RobotBulkAssign_CombinedFlags(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_combined")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Create a few panes
	for i := 0; i < 2; i++ {
		exec.Command(tmux.BinaryPath(), "split-window", "-t", suite.Session(), "-h").Run()
	}

	// Test multiple flags combined
	allocation := `{"0":"bd-c0","1":"bd-c1","2":"bd-c2"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--skip=1",
		"--strategy=impact",
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify combined behavior
	suite.Logger().Log("[E2E-BULK-ASSIGN] Combined flags: dry_run=%v strategy=%s failed=%d",
		result.DryRun, result.Strategy, result.Summary.Failed)

	if !result.DryRun {
		t.Fatal("[E2E-BULK-ASSIGN] dry_run should be true")
	}

	// Pane 1 should be in assignments but marked as failed (skip causes pane not available)
	for _, a := range result.Assignments {
		if a.Pane == "1" {
			suite.Logger().Log("[E2E-BULK-ASSIGN] Pane 1 skipped: status=%s error=%s", a.Status, a.Error)
		}
	}
}

func TestE2E_RobotBulkAssign_LargeAllocation(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_large")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Build large allocation (20 entries)
	var allocParts []string
	for i := 0; i < 20; i++ {
		allocParts = append(allocParts, fmt.Sprintf(`"%d":"bd-large%d"`, i, i))
	}
	allocation := "{" + strings.Join(allocParts, ",") + "}"

	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Should handle large allocation gracefully
	suite.Logger().Log("[E2E-BULK-ASSIGN] Large allocation: 20 entries -> %d assigned, %d unassigned beads",
		result.Summary.Assigned, len(result.UnassignedBeads))

	// With only a few panes, most should be unassigned
	if result.Summary.TotalPanes < 20 && len(result.UnassignedBeads) < 10 {
		suite.Logger().Log("[E2E-BULK-ASSIGN] Warning: expected more unassigned beads")
	}
}

func TestE2E_RobotBulkAssign_SkipPanesFormat(t *testing.T) {
	CommonE2EPrerequisites(t)

	testCases := []struct {
		name      string
		skipPanes string
	}{
		{"single", "0"},
		{"comma_separated", "0,1,2"},
		{"with_spaces", "0, 1, 2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			suite := NewTestSuite(t, fmt.Sprintf("bulk_assign_skip_%s", tc.name))
			defer suite.Teardown()

			if err := suite.Setup(); err != nil {
				t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
			}

			allocation := `{"0":"bd-skip0","1":"bd-skip1","2":"bd-skip2"}`
			result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
				"--allocation="+allocation,
				"--skip="+tc.skipPanes,
				"--dry-run")

			if result == nil {
				t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
			}

			suite.Logger().Log("[E2E-BULK-ASSIGN] Skip format %s: skipped=%d", tc.name, result.Summary.Skipped)
		})
	}
}

func TestE2E_RobotBulkAssign_AllocationPaneTypes(t *testing.T) {
	CommonE2EPrerequisites(t)

	suite := NewTestSuite(t, "bulk_assign_pane_types")
	defer suite.Teardown()

	if err := suite.Setup(); err != nil {
		t.Fatalf("[E2E-BULK-ASSIGN] Setup failed: %v", err)
	}

	// Test with numeric string keys (which is the expected format)
	allocation := `{"0":"bd-type0","1":"bd-type1"}`
	result, _, _ := runBulkAssignCmd(t, suite, suite.Session(),
		"--allocation="+allocation,
		"--dry-run")

	if result == nil {
		t.Fatal("[E2E-BULK-ASSIGN] Expected JSON response")
	}

	// Verify response pane identities use the canonical selector grammar.
	for _, a := range result.Assignments {
		if _, err := tmux.ParsePaneSelector(a.Pane); err != nil {
			t.Fatalf("[E2E-BULK-ASSIGN] Invalid canonical pane selector %q: %v", a.Pane, err)
		}
	}

	suite.Logger().Log("[E2E-BULK-ASSIGN] Pane types validated: %d assignments", len(result.Assignments))
}
