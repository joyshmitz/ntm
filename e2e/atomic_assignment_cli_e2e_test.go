//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// This test crosses every production process boundary used by assignment:
// the built ntm binary, a private real tmux server, a real br SQLite database,
// and the durable assignment ledger under an isolated HOME. The agent panes
// run cat with terminal echo disabled so one transport submission produces
// exactly one observable marker.

type atomicAssignmentCLIFixture struct {
	ntmPath    string
	tmuxPath   string
	brPath     string
	session    string
	root       string
	projectDir string
	homeDir    string
	env        []string
	panes      map[int]atomicAssignmentPane
}

type atomicAssignmentPane struct {
	Window int
	Index  int
	Target string
	ID     string
	Title  string
}

type atomicAssignmentProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type atomicAssignmentDirectEnvelope struct {
	Command string `json:"command"`
	Session string `json:"session"`
	Success bool   `json:"success"`
	Data    *struct {
		Assignment struct {
			BeadID     string `json:"bead_id"`
			Pane       int    `json:"pane"`
			PaneTarget string `json:"pane_target"`
			PaneID     string `json:"pane_id"`
			AgentType  string `json:"agent_type"`
			Prompt     string `json:"prompt"`
			PromptSent bool   `json:"prompt_sent"`
		} `json:"assignment"`
		Receipt *struct {
			WorkItemID string `json:"work_item_id"`
			Pane       struct {
				Session     string `json:"session"`
				Target      string `json:"target"`
				WindowIndex int    `json:"window_index"`
				Index       int    `json:"index"`
				ID          string `json:"id"`
			} `json:"pane"`
			Prompt struct {
				Length     int    `json:"length"`
				HashSHA256 string `json:"hash_sha256"`
			} `json:"prompt"`
			Transport struct {
				Sent       bool   `json:"sent"`
				DeliveryID string `json:"delivery_id"`
				Error      string `json:"error"`
			} `json:"transport"`
			Timestamp string `json:"timestamp"`
		} `json:"receipt"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type atomicAssignmentBulkEnvelope struct {
	Success     bool `json:"success"`
	Assignments []struct {
		Pane              string `json:"pane"`
		PaneID            string `json:"pane_id"`
		Bead              string `json:"bead"`
		AgentType         string `json:"agent_type"`
		Status            string `json:"status"`
		PromptSent        bool   `json:"prompt_sent"`
		Claimed           bool   `json:"claimed"`
		ClaimActor        string `json:"claim_actor"`
		IdempotencyKey    string `json:"idempotency_key"`
		DispatchReceiptID string `json:"dispatch_receipt_id"`
		Error             string `json:"error"`
	} `json:"assignments"`
	Summary struct {
		Assigned int `json:"assigned"`
		Failed   int `json:"failed"`
	} `json:"summary"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

type atomicAssignmentLedger struct {
	SessionName string                             `json:"session_name"`
	Assignments map[string]*atomicAssignmentRecord `json:"assignments"`
	Version     int                                `json:"version"`
}

type atomicAssignmentRecord struct {
	BeadID                string     `json:"bead_id"`
	Pane                  int        `json:"pane"`
	AgentType             string     `json:"agent_type"`
	AgentName             string     `json:"agent_name"`
	Status                string     `json:"status"`
	AssignedAt            time.Time  `json:"assigned_at"`
	PromptSent            string     `json:"prompt_sent"`
	IdempotencyKey        string     `json:"idempotency_key"`
	ClaimActor            string     `json:"claim_actor"`
	ClaimStatus           string     `json:"claim_status"`
	ClaimedAt             *time.Time `json:"claimed_at"`
	ReservationRequired   bool       `json:"reservation_required"`
	ReservationDiscovery  bool       `json:"reservation_discovery"`
	ReservationInputPaths []string   `json:"reservation_input_paths"`
	ReservationCompleted  bool       `json:"reservation_completed"`
	DispatchState         string     `json:"dispatch_state"`
	DispatchTarget        string     `json:"dispatch_target"`
	OccupancyKey          string     `json:"occupancy_key"`
	PromptSHA256          string     `json:"prompt_sha256"`
	IntentSHA256          string     `json:"intent_sha256"`
	PendingPrompt         string     `json:"pending_prompt"`
	DispatchAttempts      int        `json:"dispatch_attempts"`
	DispatchStartedAt     *time.Time `json:"dispatch_started_at"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	DispatchReceiptID     string     `json:"dispatch_receipt_id"`
}

type atomicAssignmentBead struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func TestE2EAtomicAssignmentProductionCLI(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	directBead := fixture.createBead(t, "Atomic direct assignment")
	bulkBead := fixture.createBead(t, "Atomic bulk assignment")
	directPrompt := fmt.Sprintf("NTM_ATOMIC_DIRECT_%d", time.Now().UnixNano())

	// Reservation is enabled by default. An unavailable Agent Mail endpoint
	// must stop before the br claim and before tmux transport.
	failed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, directBead, directPrompt, true)...)
	if failed.exitCode == 0 {
		t.Fatalf("reservation-required assignment exited 0: stdout=%s stderr=%s", failed.stdout, failed.stderr)
	}
	if len(bytes.TrimSpace(failed.stderr)) != 0 {
		t.Fatalf("reservation failure stderr = %q", failed.stderr)
	}
	var failedEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, failed.stdout, &failedEnvelope)
	if failedEnvelope.Success || failedEnvelope.Error == nil || failedEnvelope.Error.Code != "RESERVATION_REQUIRED" ||
		!strings.Contains(failedEnvelope.Error.Message, "file reservation is required but unavailable") {
		t.Fatalf("reservation failure envelope = %+v", failedEnvelope)
	}
	fixture.assertBead(t, directBead, "open", "")
	fixture.assertLedgerHasNoAssignment(t, directBead)
	fixture.assertMarkerCounts(t, directPrompt, map[int]int{0: 0, 1: 0})

	// Explicitly disabling reservations permits the same intent to proceed.
	// The command is a new OS process and uses the production direct adapter.
	succeeded := fixture.runNTM(t, nil, atomicDirectArgs(fixture, directBead, directPrompt, false)...)
	if succeeded.exitCode != 0 {
		t.Fatalf("direct assignment exit=%d stdout=%s stderr=%s", succeeded.exitCode, succeeded.stdout, succeeded.stderr)
	}
	if len(bytes.TrimSpace(succeeded.stderr)) != 0 {
		t.Fatalf("direct assignment stderr = %q", succeeded.stderr)
	}
	var directEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, succeeded.stdout, &directEnvelope)
	assertDirectAssignmentEnvelope(t, directEnvelope, fixture.session, directBead, directPrompt, fixture.panes[0])
	fixture.waitForMarkerCount(t, 0, directPrompt, 1)
	fixture.assertMarkerCounts(t, directPrompt, map[int]int{0: 1, 1: 0})

	directRecord := fixture.readLedgerAssignment(t, directBead)
	assertAtomicAssignmentRecord(t, directRecord, directBead, directPrompt, fixture.panes[0], "codex")
	fixture.assertBead(t, directBead, "in_progress", directRecord.ClaimActor)
	directKey := directRecord.IdempotencyKey
	directDispatchedAt := *directRecord.DispatchedAt
	directReceiptID := directRecord.DispatchReceiptID

	// A second built CLI process with the same raw intent must replay the
	// durable receipt without another claim, observation, or tmux delivery.
	replay := fixture.runNTM(t, nil, atomicDirectArgs(fixture, directBead, directPrompt, false)...)
	if replay.exitCode != 0 || len(bytes.TrimSpace(replay.stderr)) != 0 {
		t.Fatalf("direct replay exit=%d stdout=%s stderr=%s", replay.exitCode, replay.stdout, replay.stderr)
	}
	var replayEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, replay.stdout, &replayEnvelope)
	assertDirectAssignmentEnvelope(t, replayEnvelope, fixture.session, directBead, directPrompt, fixture.panes[0])
	if directEnvelope.Data == nil || directEnvelope.Data.Receipt == nil || replayEnvelope.Data == nil || replayEnvelope.Data.Receipt == nil ||
		directEnvelope.Data.Receipt.Transport.DeliveryID != replayEnvelope.Data.Receipt.Transport.DeliveryID ||
		directEnvelope.Data.Receipt.Timestamp != replayEnvelope.Data.Receipt.Timestamp {
		t.Fatalf("direct replay receipt changed: first=%+v replay=%+v", directEnvelope.Data, replayEnvelope.Data)
	}
	fixture.assertMarkerCounts(t, directPrompt, map[int]int{0: 1, 1: 0})
	reloadedDirect := fixture.readLedgerAssignment(t, directBead)
	if reloadedDirect.IdempotencyKey != directKey || reloadedDirect.DispatchAttempts != 1 ||
		reloadedDirect.DispatchedAt == nil || !reloadedDirect.DispatchedAt.Equal(directDispatchedAt) ||
		reloadedDirect.DispatchReceiptID != directReceiptID {
		t.Fatalf("replay process mutated durable direct receipt: before=%+v after=%+v", directRecord, reloadedDirect)
	}
	fixture.assertBead(t, directBead, "in_progress", directRecord.ClaimActor)

	changedPrompt := directPrompt + "_CHANGED"
	changed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, directBead, changedPrompt, false)...)
	if changed.exitCode == 0 || len(bytes.TrimSpace(changed.stderr)) != 0 {
		t.Fatalf("changed direct intent exit=%d stdout=%s stderr=%s", changed.exitCode, changed.stdout, changed.stderr)
	}
	var changedEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, changed.stdout, &changedEnvelope)
	if changedEnvelope.Success || changedEnvelope.Error == nil || changedEnvelope.Error.Code != "CLAIM_CONFLICT" {
		t.Fatalf("changed direct intent envelope = %+v", changedEnvelope)
	}
	fixture.assertMarkerCounts(t, changedPrompt, map[int]int{0: 0, 1: 0})

	// Robot bulk assignment has its own production adapter and intentionally
	// reuses a matching persisted idempotency key. Run it twice from separate
	// CLI processes and prove the second response is a side-effect-free replay.
	bulkTemplate := filepath.Join(fixture.projectDir, "atomic-bulk-template.txt")
	bulkTemplateBody := fmt.Sprintf("NTM_ATOMIC_BULK_%d_{bead_id}", time.Now().UnixNano())
	if err := os.WriteFile(bulkTemplate, []byte(bulkTemplateBody), 0o600); err != nil {
		t.Fatalf("write bulk prompt template: %v", err)
	}
	bulkPrompt := strings.ReplaceAll(bulkTemplateBody, "{bead_id}", bulkBead)
	bulkArgs := atomicBulkArgs(fixture, bulkBead, bulkTemplate)

	fixture.primePaneForSafeDispatch(t, 1)
	bulkFirstResult := fixture.runNTM(t, nil, bulkArgs...)
	if bulkFirstResult.exitCode != 0 {
		t.Fatalf("bulk assignment exit=%d stdout=%s stderr=%s", bulkFirstResult.exitCode, bulkFirstResult.stdout, bulkFirstResult.stderr)
	}
	if len(bytes.TrimSpace(bulkFirstResult.stderr)) != 0 {
		t.Fatalf("bulk assignment stderr = %q", bulkFirstResult.stderr)
	}
	var bulkFirst atomicAssignmentBulkEnvelope
	decodeAtomicAssignmentJSON(t, bulkFirstResult.stdout, &bulkFirst)
	bulkFirstAssignment := assertBulkAssignmentEnvelope(t, bulkFirst, bulkBead, "1")
	fixture.waitForMarkerCount(t, 1, bulkPrompt, 1)
	fixture.assertMarkerCounts(t, bulkPrompt, map[int]int{0: 0, 1: 1})

	bulkRecord := fixture.readLedgerAssignment(t, bulkBead)
	assertAtomicAssignmentRecord(t, bulkRecord, bulkBead, bulkPrompt, fixture.panes[1], "claude")
	fixture.assertBead(t, bulkBead, "in_progress", bulkRecord.ClaimActor)
	if bulkFirstAssignment.IdempotencyKey != bulkRecord.IdempotencyKey ||
		bulkFirstAssignment.ClaimActor != bulkRecord.ClaimActor ||
		bulkFirstAssignment.DispatchReceiptID != bulkRecord.DispatchReceiptID {
		t.Fatalf("bulk response does not match ledger: response=%+v ledger=%+v", bulkFirstAssignment, bulkRecord)
	}

	// The first assignment leaves its marker as the pane's trailing line. Put
	// the fake agent back at a recognizable idle prompt so replay crosses the
	// same production observation gate before reaching the durable receipt.
	fixture.primePaneForSafeDispatch(t, 1)
	bulkSecondResult := fixture.runNTM(t, nil, bulkArgs...)
	if bulkSecondResult.exitCode != 0 {
		t.Fatalf("bulk replay exit=%d stdout=%s stderr=%s", bulkSecondResult.exitCode, bulkSecondResult.stdout, bulkSecondResult.stderr)
	}
	var bulkSecond atomicAssignmentBulkEnvelope
	decodeAtomicAssignmentJSON(t, bulkSecondResult.stdout, &bulkSecond)
	bulkSecondAssignment := assertBulkAssignmentEnvelope(t, bulkSecond, bulkBead, "1")
	if bulkSecondAssignment.IdempotencyKey != bulkFirstAssignment.IdempotencyKey ||
		bulkSecondAssignment.DispatchReceiptID != bulkFirstAssignment.DispatchReceiptID ||
		bulkSecondAssignment.ClaimActor != bulkFirstAssignment.ClaimActor {
		t.Fatalf("bulk replay identity changed: first=%+v second=%+v", bulkFirstAssignment, bulkSecondAssignment)
	}
	fixture.assertMarkerCounts(t, bulkPrompt, map[int]int{0: 0, 1: 1})
	reloadedBulk := fixture.readLedgerAssignment(t, bulkBead)
	if reloadedBulk.IdempotencyKey != bulkRecord.IdempotencyKey || reloadedBulk.DispatchAttempts != 1 ||
		reloadedBulk.DispatchedAt == nil || bulkRecord.DispatchedAt == nil ||
		!reloadedBulk.DispatchedAt.Equal(*bulkRecord.DispatchedAt) ||
		reloadedBulk.DispatchReceiptID != bulkRecord.DispatchReceiptID {
		t.Fatalf("bulk replay mutated durable delivery: before=%+v after=%+v", bulkRecord, reloadedBulk)
	}
	fixture.assertBead(t, bulkBead, "in_progress", bulkRecord.ClaimActor)
}

func TestE2EAtomicBulkAssignmentCanonicalMultiWindow(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	panes := fixture.addSecondAgentWindow(t)
	templatePath := filepath.Join(fixture.projectDir, "atomic-multi-window-template.txt")
	templateBody := fmt.Sprintf("NTM_ATOMIC_MULTI_%d_{bead_id}", time.Now().UnixNano())
	if err := os.WriteFile(templatePath, []byte(templateBody), 0o600); err != nil {
		t.Fatalf("write multi-window prompt template: %v", err)
	}
	promptFor := func(beadID string) string {
		return strings.ReplaceAll(templateBody, "{bead_id}", beadID)
	}
	runBulk := func(tb *testing.T, allocation map[string]string, extra ...string) (atomicAssignmentProcessResult, atomicAssignmentBulkEnvelope) {
		tb.Helper()
		encoded, err := json.Marshal(allocation)
		if err != nil {
			tb.Fatalf("encode allocation: %v", err)
		}
		args := []string{
			"--robot-format=json",
			"--robot-bulk-assign=" + fixture.session,
			"--allocation=" + string(encoded),
			"--template=" + templatePath,
		}
		args = append(args, extra...)
		result := fixture.runNTM(tb, nil, args...)
		var envelope atomicAssignmentBulkEnvelope
		decodeAtomicAssignmentJSON(tb, result.stdout, &envelope)
		return result, envelope
	}
	assertOneSuccess := func(tb *testing.T, result atomicAssignmentProcessResult, envelope atomicAssignmentBulkEnvelope, beadID string, pane atomicAssignmentPane, agentType string) {
		tb.Helper()
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			tb.Fatalf("bulk assignment exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		if !envelope.Success || envelope.Summary.Assigned != 1 || envelope.Summary.Failed != 0 || len(envelope.Assignments) != 1 {
			tb.Fatalf("bulk envelope = %+v", envelope)
		}
		assignment := envelope.Assignments[0]
		if assignment.Pane != pane.Target || assignment.PaneID != pane.ID || assignment.Bead != beadID ||
			assignment.AgentType != agentType || assignment.Status != "assigned" || !assignment.PromptSent ||
			!assignment.Claimed || assignment.ClaimActor == "" || assignment.IdempotencyKey == "" ||
			assignment.DispatchReceiptID == "" || assignment.Error != "" {
			tb.Fatalf("bulk assignment = %+v", assignment)
		}
		record := fixture.readLedgerAssignment(tb, beadID)
		assertAtomicAssignmentRecord(tb, record, beadID, promptFor(beadID), pane, agentType)
		if assignment.ClaimActor != record.ClaimActor || assignment.IdempotencyKey != record.IdempotencyKey || assignment.DispatchReceiptID != record.DispatchReceiptID {
			tb.Fatalf("bulk response does not match durable receipt: response=%+v ledger=%+v", assignment, record)
		}
		fixture.assertBead(tb, beadID, "in_progress", record.ClaimActor)
	}
	assertFailedBeforeSideEffects := func(tb *testing.T, result atomicAssignmentProcessResult, envelope atomicAssignmentBulkEnvelope, beadIDs []string, marker string) {
		tb.Helper()
		if result.exitCode == 0 || envelope.Success || envelope.Summary.Failed == 0 {
			tb.Fatalf("invalid allocation did not fail: exit=%d envelope=%+v", result.exitCode, envelope)
		}
		for _, beadID := range beadIDs {
			fixture.assertBead(tb, beadID, "open", "")
			fixture.assertLedgerHasNoAssignment(tb, beadID)
		}
		fixture.assertEndpointMarkerCounts(tb, marker, panes, nil)
	}

	t.Run("window_pane_and_id_aliases_dispatch_once", func(t *testing.T) {
		beadID := fixture.createBead(t, "Canonical alias deduplication")
		target := panes["1.1"]
		fixture.primeEndpointForSafeDispatch(t, target)
		result, envelope := runBulk(t, map[string]string{target.Target: beadID, target.ID: beadID})
		assertOneSuccess(t, result, envelope, beadID, target, "claude")
		fixture.waitForEndpointMarkerCount(t, target, promptFor(beadID), 1)
		fixture.assertEndpointMarkerCounts(t, promptFor(beadID), panes, map[string]int{target.Target: 1})
	})

	t.Run("pane_id_selects_exact_duplicate_local_index", func(t *testing.T) {
		beadID := fixture.createBead(t, "Canonical pane ID assignment")
		target := panes["0.1"]
		fixture.primeEndpointForSafeDispatch(t, target)
		result, envelope := runBulk(t, map[string]string{target.ID: beadID})
		assertOneSuccess(t, result, envelope, beadID, target, "claude")
		fixture.waitForEndpointMarkerCount(t, target, promptFor(beadID), 1)
		fixture.assertEndpointMarkerCounts(t, promptFor(beadID), panes, map[string]int{target.Target: 1})
	})

	t.Run("canonical_skip_excludes_only_selected_physical_pane", func(t *testing.T) {
		beadID := fixture.createBead(t, "Canonical skipped assignment")
		target := panes["1.0"]
		result, envelope := runBulk(t, map[string]string{target.Target: beadID}, "--skip="+target.ID)
		assertFailedBeforeSideEffects(t, result, envelope, []string{beadID}, promptFor(beadID))
		if len(envelope.Assignments) != 1 || !strings.Contains(envelope.Assignments[0].Error, "not found") {
			t.Fatalf("skip failure did not identify the excluded target: %+v", envelope.Assignments)
		}
	})

	t.Run("bare_window_selector_is_ambiguous_before_claim", func(t *testing.T) {
		beadID := fixture.createBead(t, "Ambiguous window assignment")
		result, envelope := runBulk(t, map[string]string{"1": beadID})
		assertFailedBeforeSideEffects(t, result, envelope, []string{beadID}, promptFor(beadID))
		if len(envelope.Assignments) != 1 || !strings.Contains(envelope.Assignments[0].Error, "matched 2 panes") {
			t.Fatalf("ambiguous selector failure = %+v", envelope.Assignments)
		}
	})

	t.Run("conflicting_aliases_fail_before_either_claim", func(t *testing.T) {
		firstBead := fixture.createBead(t, "Conflicting alias one")
		secondBead := fixture.createBead(t, "Conflicting alias two")
		target := panes["0.0"]
		result, envelope := runBulk(t, map[string]string{target.Target: firstBead, target.ID: secondBead})
		assertFailedBeforeSideEffects(t, result, envelope, []string{firstBead, secondBead}, promptFor(firstBead))
		fixture.assertEndpointMarkerCounts(t, promptFor(secondBead), panes, nil)
		if len(envelope.Assignments) != 2 {
			t.Fatalf("conflicting alias results = %+v", envelope.Assignments)
		}
		for _, assignment := range envelope.Assignments {
			if !strings.Contains(assignment.Error, "same physical pane") {
				t.Fatalf("conflicting alias failure = %+v", assignment)
			}
		}
	})

	t.Run("direct_window_pane_replays_through_pane_id_alias", func(t *testing.T) {
		beadID := fixture.createBead(t, "Canonical direct assignment")
		target := panes["1.0"]
		prompt := fmt.Sprintf("NTM_ATOMIC_DIRECT_MULTI_%d", time.Now().UnixNano())
		firstResult := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, target.Target, beadID, prompt, false)...)
		if firstResult.exitCode != 0 || len(bytes.TrimSpace(firstResult.stderr)) != 0 {
			t.Fatalf("direct W.P assignment exit=%d stdout=%s stderr=%s", firstResult.exitCode, firstResult.stdout, firstResult.stderr)
		}
		var first atomicAssignmentDirectEnvelope
		decodeAtomicAssignmentJSON(t, firstResult.stdout, &first)
		assertDirectAssignmentEnvelope(t, first, fixture.session, beadID, prompt, target)
		if first.Data == nil || first.Data.Receipt == nil || first.Data.Assignment.PaneTarget != target.Target || first.Data.Assignment.PaneID != target.ID ||
			first.Data.Receipt.Pane.Target != target.Target || first.Data.Receipt.Pane.WindowIndex != target.Window {
			t.Fatalf("direct canonical pane identity = %+v", first.Data)
		}
		fixture.waitForEndpointMarkerCount(t, target, prompt, 1)
		fixture.assertEndpointMarkerCounts(t, prompt, panes, map[string]int{target.Target: 1})
		firstRecord := fixture.readLedgerAssignment(t, beadID)
		assertAtomicAssignmentRecord(t, firstRecord, beadID, prompt, target, "codex")

		replayResult := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, target.ID, beadID, prompt, false)...)
		if replayResult.exitCode != 0 || len(bytes.TrimSpace(replayResult.stderr)) != 0 {
			t.Fatalf("direct %%N replay exit=%d stdout=%s stderr=%s", replayResult.exitCode, replayResult.stdout, replayResult.stderr)
		}
		var replay atomicAssignmentDirectEnvelope
		decodeAtomicAssignmentJSON(t, replayResult.stdout, &replay)
		assertDirectAssignmentEnvelope(t, replay, fixture.session, beadID, prompt, target)
		if replay.Data == nil || replay.Data.Receipt == nil || first.Data.Receipt.Transport.DeliveryID != replay.Data.Receipt.Transport.DeliveryID ||
			first.Data.Receipt.Timestamp != replay.Data.Receipt.Timestamp {
			t.Fatalf("direct alias replay receipt changed: first=%+v replay=%+v", first.Data, replay.Data)
		}
		fixture.assertEndpointMarkerCounts(t, prompt, panes, map[string]int{target.Target: 1})
		replayedRecord := fixture.readLedgerAssignment(t, beadID)
		if replayedRecord.IdempotencyKey != firstRecord.IdempotencyKey || replayedRecord.DispatchAttempts != 1 || replayedRecord.DispatchReceiptID != firstRecord.DispatchReceiptID {
			t.Fatalf("direct alias replay mutated durable state: first=%+v replay=%+v", firstRecord, replayedRecord)
		}
	})

	t.Run("direct_bare_window_selector_fails_before_claim", func(t *testing.T) {
		beadID := fixture.createBead(t, "Ambiguous direct window assignment")
		prompt := fmt.Sprintf("NTM_ATOMIC_DIRECT_AMBIGUOUS_%d", time.Now().UnixNano())
		result := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, "1", beadID, prompt, false)...)
		if result.exitCode == 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("ambiguous direct selector exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope atomicAssignmentDirectEnvelope
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if envelope.Success || envelope.Error == nil || envelope.Error.Code != "PANE_AMBIGUOUS" || !strings.Contains(envelope.Error.Message, "matched 2 panes") {
			t.Fatalf("ambiguous direct selector envelope = %+v", envelope)
		}
		fixture.assertBead(t, beadID, "open", "")
		fixture.assertLedgerHasNoAssignment(t, beadID)
		fixture.assertEndpointMarkerCounts(t, prompt, panes, nil)
	})
}

func TestE2EAtomicAssignmentPromptSafety(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	redactBead := fixture.createBead(t, "Atomic prompt redaction")
	blockBead := fixture.createBead(t, "Atomic prompt blocking")
	secret := "sk-proj-NTM_E2E_SECRET_1234567890123456789012345678901234567890"
	rawPrompt := "Use OPENAI_API_KEY=" + secret + " for this assignment"

	redactArgs := append(atomicDirectArgs(fixture, redactBead, rawPrompt, false), "--redact=redact")
	redacted := fixture.runNTM(t, nil, redactArgs...)
	if redacted.exitCode != 0 {
		t.Fatalf("redacted assignment exit=%d stdout=%s stderr=%s", redacted.exitCode, redacted.stdout, redacted.stderr)
	}
	fixture.assertSecretAbsent(t, secret, redacted.stdout, redacted.stderr)
	var envelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, redacted.stdout, &envelope)
	if !envelope.Success || envelope.Data == nil || envelope.Data.Assignment.Prompt == "" || strings.Contains(envelope.Data.Assignment.Prompt, secret) {
		t.Fatalf("redacted assignment envelope = %+v", envelope)
	}
	durablePrompt := envelope.Data.Assignment.Prompt
	fixture.waitForMarkerCount(t, 0, "Use OPENAI_API_KEY=", 1)
	fixture.assertMarkerCounts(t, secret, map[int]int{0: 0, 1: 0})
	record := fixture.readLedgerAssignment(t, redactBead)
	if record.PromptSent != durablePrompt || strings.Contains(record.PromptSent, secret) || record.PendingPrompt != "" {
		t.Fatalf("durable redacted prompt = %+v", record)
	}
	rawHash := sha256.Sum256([]byte(rawPrompt))
	durableHash := sha256.Sum256([]byte(durablePrompt))
	if record.IntentSHA256 != hex.EncodeToString(rawHash[:]) || record.PromptSHA256 != hex.EncodeToString(durableHash[:]) {
		t.Fatalf("prompt hashes = intent:%q durable:%q", record.IntentSHA256, record.PromptSHA256)
	}
	fixture.assertAssignmentArtifactsExclude(t, secret)

	blockedPrompt := "Use password=NTM_E2E_BLOCKED_SECRET_987654321 for this assignment"
	blockedArgs := append(atomicDirectArgsForPane(fixture, 1, blockBead, blockedPrompt, false), "--redact=block")
	blocked := fixture.runNTM(t, nil, blockedArgs...)
	if blocked.exitCode == 0 {
		t.Fatalf("blocked assignment exited 0: stdout=%s stderr=%s", blocked.stdout, blocked.stderr)
	}
	fixture.assertSecretAbsent(t, "NTM_E2E_BLOCKED_SECRET_987654321", blocked.stdout, blocked.stderr)
	var blockedEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, blocked.stdout, &blockedEnvelope)
	if blockedEnvelope.Success || blockedEnvelope.Error == nil || blockedEnvelope.Error.Code != "ASSIGN_ERROR" ||
		!strings.Contains(strings.ToLower(blockedEnvelope.Error.Message), "block") {
		t.Fatalf("blocked assignment envelope = %+v", blockedEnvelope)
	}
	fixture.assertBead(t, blockBead, "open", "")
	fixture.assertLedgerHasNoAssignment(t, blockBead)
	fixture.assertMarkerCounts(t, blockedPrompt, map[int]int{0: 0, 1: 0})
	fixture.assertAssignmentArtifactsExclude(t, "NTM_E2E_BLOCKED_SECRET_987654321")
}

func newAtomicAssignmentCLIFixture(t *testing.T) *atomicAssignmentCLIFixture {
	t.Helper()

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("br is required for atomic assignment E2E: %v", err)
	}

	root := t.TempDir()
	tmuxRoot := filepath.Join("/tmp", fmt.Sprintf("ntm-atomic-tmux-%d-%d", os.Getpid(), time.Now().UnixNano()))
	fixture := &atomicAssignmentCLIFixture{
		ntmPath:    ntmPath,
		tmuxPath:   tmuxPath,
		brPath:     brPath,
		session:    fmt.Sprintf("ntm-e2e-atomic-%d-%d", os.Getpid(), time.Now().UnixNano()),
		root:       root,
		projectDir: filepath.Join(root, "project"),
		homeDir:    filepath.Join(root, "home"),
		panes:      make(map[int]atomicAssignmentPane),
	}
	for _, dir := range []string{
		fixture.projectDir,
		fixture.homeDir,
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		tmuxRoot,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}
	fixture.env = atomicAssignmentIsolatedEnv(map[string]string{
		"HOME":                fixture.homeDir,
		"XDG_CONFIG_HOME":     filepath.Join(root, "config"),
		"XDG_DATA_HOME":       filepath.Join(root, "data"),
		"TMUX_TMPDIR":         tmuxRoot,
		"AGENT_MAIL_URL":      "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":    "",
		"HTTP_PROXY":          "",
		"HTTPS_PROXY":         "",
		"ALL_PROXY":           "",
		"NO_PROXY":            "127.0.0.1,localhost",
		"NO_COLOR":            "1",
		"TERM":                "xterm-256color",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "",
		"TOON_DEFAULT_FORMAT": "",
	})

	fixture.mustBR(t, "init", "--prefix=ate2e", "--json")

	tmuxConfig := filepath.Join(root, "tmux.conf")
	config := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g renumber-windows off",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(tmuxConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	agentCommand := "bash --noprofile --norc -c 'stty -echo; exec cat'"
	fixture.mustTMUX(t, "-f", tmuxConfig, "new-session", "-d", "-s", fixture.session, "-x", "160", "-y", "48", "-c", fixture.projectDir, agentCommand)
	t.Cleanup(func() {
		_ = fixture.runTMUX(context.Background(), "kill-server")
	})
	fixture.mustTMUX(t, "split-window", "-d", "-t", fixture.session+":0", "-c", fixture.projectDir, agentCommand)
	fixture.mustTMUX(t, "select-pane", "-t", fixture.session+":0.0", "-T", fixture.session+"__cod_1")
	fixture.mustTMUX(t, "select-pane", "-t", fixture.session+":0.1", "-T", fixture.session+"__cc_2")
	fixture.waitForPanes(t)
	return fixture
}

func atomicDirectArgs(f *atomicAssignmentCLIFixture, beadID, prompt string, requireReservation bool) []string {
	return atomicDirectArgsForPane(f, 0, beadID, prompt, requireReservation)
}

func atomicDirectArgsForPane(f *atomicAssignmentCLIFixture, pane int, beadID, prompt string, requireReservation bool) []string {
	return atomicDirectArgsForSelector(f, fmt.Sprintf("%d", pane), beadID, prompt, requireReservation)
}

func atomicDirectArgsForSelector(f *atomicAssignmentCLIFixture, selector, beadID, prompt string, requireReservation bool) []string {
	args := []string{
		"assign", f.session,
		"--repo=" + f.projectDir,
		"--pane=" + selector,
		"--beads=" + beadID,
		"--prompt=" + prompt,
		"--force",
		"--ignore-deps",
		"--timeout=5s",
	}
	if !requireReservation {
		args = append(args, "--reserve-files=false")
	}
	args = append(args, "--json")
	return args
}

func atomicBulkArgs(f *atomicAssignmentCLIFixture, beadID, templatePath string) []string {
	allocation, _ := json.Marshal(map[string]string{"1": beadID})
	return []string{
		"--robot-format=json",
		"--robot-bulk-assign=" + f.session,
		"--allocation=" + string(allocation),
		"--template=" + templatePath,
	}
}

func (f *atomicAssignmentCLIFixture) createBead(t *testing.T, title string) string {
	t.Helper()
	output := f.mustBR(t, "create", title, "--type=task", "--priority=1", "--silent")
	id := strings.TrimSpace(string(output))
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		t.Fatalf("unexpected br create output %q", output)
	}
	f.assertBead(t, id, "open", "")
	return id
}

func (f *atomicAssignmentCLIFixture) assertBead(t *testing.T, beadID, wantStatus, wantAssignee string) {
	t.Helper()
	output := f.mustBR(t, "show", beadID, "--json")
	var rows []atomicAssignmentBead
	if err := json.Unmarshal(output, &rows); err != nil {
		var row atomicAssignmentBead
		if objectErr := json.Unmarshal(output, &row); objectErr != nil {
			t.Fatalf("decode br show output: array=%v object=%v raw=%s", err, objectErr, output)
		}
		rows = []atomicAssignmentBead{row}
	}
	if len(rows) != 1 {
		t.Fatalf("br show %s returned %d rows: %s", beadID, len(rows), output)
	}
	if rows[0].ID != beadID || rows[0].Status != wantStatus || rows[0].Assignee != wantAssignee {
		t.Fatalf("bead state = %+v, want id=%s status=%s assignee=%s", rows[0], beadID, wantStatus, wantAssignee)
	}
}

func (f *atomicAssignmentCLIFixture) ledgerPath() string {
	return filepath.Join(f.homeDir, ".ntm", "sessions", f.session, "assignments.json")
}

func (f *atomicAssignmentCLIFixture) assertLedgerHasNoAssignment(t *testing.T, beadID string) {
	t.Helper()
	ledger, err := f.readLedger()
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read ledger after reservation refusal: %v", err)
	}
	if _, exists := ledger.Assignments[beadID]; exists {
		t.Fatalf("reservation refusal persisted assignment for %s: %+v", beadID, ledger.Assignments[beadID])
	}
}

func (f *atomicAssignmentCLIFixture) assertSecretAbsent(t *testing.T, secret string, payloads ...[]byte) {
	t.Helper()
	for index, payload := range payloads {
		if bytes.Contains(payload, []byte(secret)) {
			t.Fatalf("secret leaked in payload %d: %s", index, payload)
		}
	}
}

func (f *atomicAssignmentCLIFixture) assertAssignmentArtifactsExclude(t *testing.T, secret string) {
	t.Helper()
	for _, path := range []string{f.ledgerPath(), f.ledgerPath() + ".bak"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read assignment artifact %s: %v", path, err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("secret leaked in assignment artifact %s", path)
		}
	}
}

func (f *atomicAssignmentCLIFixture) readLedgerAssignment(t *testing.T, beadID string) *atomicAssignmentRecord {
	t.Helper()
	ledger, err := f.readLedger()
	if err != nil {
		t.Fatalf("read assignment ledger: %v", err)
	}
	if ledger.SessionName != f.session || ledger.Version < 2 {
		t.Fatalf("ledger header = session:%q version:%d", ledger.SessionName, ledger.Version)
	}
	record := ledger.Assignments[beadID]
	if record == nil {
		t.Fatalf("assignment ledger missing %s: %+v", beadID, ledger.Assignments)
	}
	return record
}

func (f *atomicAssignmentCLIFixture) readLedger() (*atomicAssignmentLedger, error) {
	data, err := os.ReadFile(f.ledgerPath())
	if err != nil {
		return nil, err
	}
	var ledger atomicAssignmentLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, err
	}
	return &ledger, nil
}

func assertAtomicAssignmentRecord(t *testing.T, record *atomicAssignmentRecord, beadID, prompt string, pane atomicAssignmentPane, agentType string) {
	t.Helper()
	if record.BeadID != beadID || record.Pane != pane.Index || record.AgentType != agentType ||
		record.Status != "assigned" || record.PromptSent != prompt {
		t.Fatalf("assignment identity/state = %+v", record)
	}
	if record.AgentName == "" || record.IdempotencyKey == "" || record.ClaimActor == "" || record.ClaimStatus != "in_progress" {
		t.Fatalf("claim metadata incomplete: %+v", record)
	}
	decodedKey, err := hex.DecodeString(record.IdempotencyKey)
	if err != nil || len(decodedKey) != sha256.Size {
		t.Fatalf("idempotency key %q is not a 256-bit hex key: decoded=%d err=%v", record.IdempotencyKey, len(decodedKey), err)
	}
	if !strings.HasSuffix(record.ClaimActor, "/ntm-"+record.IdempotencyKey[:12]) {
		t.Fatalf("claim actor %q does not carry idempotency identity %q", record.ClaimActor, record.IdempotencyKey[:12])
	}
	if record.ClaimedAt == nil || record.DispatchStartedAt == nil || record.DispatchedAt == nil {
		t.Fatalf("claim/dispatch timestamps incomplete: %+v", record)
	}
	if record.ClaimedAt.After(*record.DispatchStartedAt) || record.DispatchStartedAt.After(*record.DispatchedAt) {
		t.Fatalf("claim-before-dispatch order violated: claimed=%s started=%s dispatched=%s", record.ClaimedAt, record.DispatchStartedAt, record.DispatchedAt)
	}
	canonicalTarget := pane.Target
	if pane.Window == 0 {
		canonicalTarget = fmt.Sprintf("%d", pane.Index)
	}
	dispatchTargetMatches := record.DispatchTarget == pane.ID || record.DispatchTarget == canonicalTarget
	if record.DispatchState != "sent" || !dispatchTargetMatches || record.OccupancyKey != pane.ID || record.DispatchAttempts != 1 || record.DispatchReceiptID == "" {
		t.Fatalf("dispatch metadata incomplete: %+v", record)
	}
	if !strings.Contains(record.DispatchReceiptID, pane.ID) {
		t.Fatalf("dispatch receipt %q does not identify target %q", record.DispatchReceiptID, pane.ID)
	}
	if record.PendingPrompt != "" {
		t.Fatalf("sent assignment retained pending prompt %q", record.PendingPrompt)
	}
	hash := sha256.Sum256([]byte(prompt))
	if record.PromptSHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("prompt hash = %q, want %x", record.PromptSHA256, hash)
	}
	if record.ReservationRequired || record.ReservationDiscovery || record.ReservationCompleted || len(record.ReservationInputPaths) != 0 {
		t.Fatalf("reservation-disabled assignment persisted reservation state: %+v", record)
	}
}

func assertDirectAssignmentEnvelope(t *testing.T, envelope atomicAssignmentDirectEnvelope, session, beadID, prompt string, pane atomicAssignmentPane) {
	t.Helper()
	if !envelope.Success || envelope.Command != "assign" || envelope.Session != session || envelope.Error != nil || envelope.Data == nil {
		t.Fatalf("direct envelope = %+v", envelope)
	}
	assignment := envelope.Data.Assignment
	if assignment.BeadID != beadID || assignment.Pane != pane.Index || assignment.AgentType != "codex" ||
		assignment.Prompt != prompt || !assignment.PromptSent {
		t.Fatalf("direct assignment response = %+v", assignment)
	}
	receipt := envelope.Data.Receipt
	if receipt == nil || receipt.WorkItemID != beadID || receipt.Pane.Session != session ||
		receipt.Pane.Index != pane.Index || receipt.Pane.ID != pane.ID || !receipt.Transport.Sent || receipt.Transport.Error != "" {
		t.Fatalf("direct dispatch receipt = %+v", receipt)
	}
	hash := sha256.Sum256([]byte(prompt))
	if receipt.Prompt.Length != len(prompt) || receipt.Prompt.HashSHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("direct prompt receipt = %+v", receipt.Prompt)
	}
}

func assertBulkAssignmentEnvelope(t *testing.T, envelope atomicAssignmentBulkEnvelope, beadID, pane string) *struct {
	Pane              string `json:"pane"`
	PaneID            string `json:"pane_id"`
	Bead              string `json:"bead"`
	AgentType         string `json:"agent_type"`
	Status            string `json:"status"`
	PromptSent        bool   `json:"prompt_sent"`
	Claimed           bool   `json:"claimed"`
	ClaimActor        string `json:"claim_actor"`
	IdempotencyKey    string `json:"idempotency_key"`
	DispatchReceiptID string `json:"dispatch_receipt_id"`
	Error             string `json:"error"`
} {
	t.Helper()
	if !envelope.Success || envelope.Error != "" || envelope.ErrorCode != "" || envelope.Summary.Assigned != 1 || envelope.Summary.Failed != 0 {
		t.Fatalf("bulk envelope summary = %+v", envelope)
	}
	if len(envelope.Assignments) != 1 {
		t.Fatalf("bulk assignments = %+v", envelope.Assignments)
	}
	assignment := &envelope.Assignments[0]
	if assignment.Bead != beadID || assignment.Pane != pane || assignment.AgentType != "claude" ||
		assignment.Status != "assigned" || !assignment.PromptSent || !assignment.Claimed ||
		assignment.ClaimActor == "" || assignment.IdempotencyKey == "" || assignment.DispatchReceiptID == "" || assignment.Error != "" {
		t.Fatalf("bulk assignment = %+v", assignment)
	}
	return assignment
}

func decodeAtomicAssignmentJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(bytes.TrimSpace(data), target); err != nil {
		t.Fatalf("decode JSON: %v raw=%s", err, data)
	}
}

func (f *atomicAssignmentCLIFixture) waitForPanes(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		output, err := f.tmuxOutput(context.Background(), "list-panes", "-s", "-t", f.session, "-F", "#{window_index}|#{pane_index}|#{pane_id}|#{pane_title}|#{pane_current_command}")
		if err == nil {
			panes := make(map[int]atomicAssignmentPane)
			ready := true
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				parts := strings.SplitN(line, "|", 5)
				if len(parts) != 5 {
					ready = false
					break
				}
				var window, index int
				if _, scanErr := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &window, &index); scanErr != nil || window != 0 || parts[4] != "cat" {
					ready = false
					break
				}
				panes[index] = atomicAssignmentPane{
					Window: window,
					Index:  index,
					Target: fmt.Sprintf("%d.%d", window, index),
					ID:     parts[2],
					Title:  parts[3],
				}
			}
			if ready && len(panes) == 2 && panes[0].Title == f.session+"__cod_1" && panes[1].Title == f.session+"__cc_2" {
				f.panes = panes
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("isolated cat panes did not become ready: output=%s err=%v", output, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) addSecondAgentWindow(t *testing.T) map[string]atomicAssignmentPane {
	t.Helper()
	agentCommand := "bash --noprofile --norc -c 'stty -echo; exec cat'"
	f.mustTMUX(t, "new-window", "-d", "-t", f.session+":1", "-c", f.projectDir, agentCommand)
	f.mustTMUX(t, "split-window", "-d", "-t", f.session+":1", "-c", f.projectDir, agentCommand)
	f.mustTMUX(t, "select-pane", "-t", f.session+":1.0", "-T", f.session+"__cod_3")
	f.mustTMUX(t, "select-pane", "-t", f.session+":1.1", "-T", f.session+"__cc_4")
	return f.waitForTopology(t, 4)
}

func (f *atomicAssignmentCLIFixture) waitForTopology(t *testing.T, want int) map[string]atomicAssignmentPane {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		output, err := f.tmuxOutput(context.Background(), "list-panes", "-s", "-t", f.session, "-F", "#{window_index}|#{pane_index}|#{pane_id}|#{pane_title}|#{pane_current_command}")
		if err == nil {
			panes := make(map[string]atomicAssignmentPane)
			ready := true
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				parts := strings.SplitN(line, "|", 5)
				if len(parts) != 5 {
					ready = false
					break
				}
				var window, index int
				if _, scanErr := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &window, &index); scanErr != nil || parts[4] != "cat" {
					ready = false
					break
				}
				target := fmt.Sprintf("%d.%d", window, index)
				panes[target] = atomicAssignmentPane{
					Window: window,
					Index:  index,
					Target: target,
					ID:     parts[2],
					Title:  parts[3],
				}
			}
			if ready && len(panes) == want {
				return panes
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("isolated topology did not reach %d ready cat panes: output=%s err=%v", want, output, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) primeEndpointForSafeDispatch(t *testing.T, pane atomicAssignmentPane) {
	t.Helper()
	prompt := "claude>"
	if strings.Contains(pane.Title, "__cod_") {
		prompt = "codex>"
	}
	f.mustTMUX(t, "send-keys", "-t", pane.ID, "-l", prompt)
	f.mustTMUX(t, "send-keys", "-t", pane.ID, "Enter")

	deadline := time.Now().Add(5 * time.Second)
	for {
		capture := strings.TrimSpace(f.captureEndpoint(t, pane))
		if strings.HasSuffix(capture, prompt) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %s did not reach explicit idle prompt %q: %q", pane.Target, prompt, capture)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) captureEndpoint(t *testing.T, pane atomicAssignmentPane) string {
	t.Helper()
	output, err := f.tmuxOutput(context.Background(), "capture-pane", "-p", "-t", pane.ID, "-S", "-200")
	if err != nil {
		t.Fatalf("capture pane %s (%s): %v", pane.Target, pane.ID, err)
	}
	return string(output)
}

func (f *atomicAssignmentCLIFixture) waitForEndpointMarkerCount(t *testing.T, pane atomicAssignmentPane, marker string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		capture := f.captureEndpoint(t, pane)
		if strings.Count(capture, marker) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %s marker %q count=%d want=%d capture=%q", pane.Target, marker, strings.Count(capture, marker), want, capture)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) assertEndpointMarkerCounts(t *testing.T, marker string, panes map[string]atomicAssignmentPane, wants map[string]int) {
	t.Helper()
	for target, pane := range panes {
		want := wants[target]
		capture := f.captureEndpoint(t, pane)
		if got := strings.Count(capture, marker); got != want {
			t.Fatalf("pane %s marker %q count=%d want=%d capture=%q", target, marker, got, want, capture)
		}
	}
}

func (f *atomicAssignmentCLIFixture) waitForMarkerCount(t *testing.T, pane int, marker string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		capture := f.capturePane(t, pane)
		if strings.Count(capture, marker) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d marker %q count=%d want=%d capture=%q", pane, marker, strings.Count(capture, marker), want, capture)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) assertMarkerCounts(t *testing.T, marker string, wants map[int]int) {
	t.Helper()
	for pane, want := range wants {
		capture := f.capturePane(t, pane)
		if got := strings.Count(capture, marker); got != want {
			t.Fatalf("pane %d marker %q count=%d want=%d capture=%q", pane, marker, got, want, capture)
		}
	}
}

func (f *atomicAssignmentCLIFixture) primePaneForSafeDispatch(t *testing.T, pane int) {
	t.Helper()
	endpoint, ok := f.panes[pane]
	if !ok {
		t.Fatalf("unknown fixture pane %d", pane)
	}
	f.mustTMUX(t, "send-keys", "-t", endpoint.ID, "-l", "claude>")
	f.mustTMUX(t, "send-keys", "-t", endpoint.ID, "Enter")

	deadline := time.Now().Add(5 * time.Second)
	for {
		capture := strings.TrimSpace(f.capturePane(t, pane))
		if strings.HasSuffix(capture, "claude>") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %d did not reach explicit idle prompt: %q", pane, capture)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (f *atomicAssignmentCLIFixture) capturePane(t *testing.T, pane int) string {
	t.Helper()
	endpoint, ok := f.panes[pane]
	if !ok {
		t.Fatalf("unknown fixture pane %d", pane)
	}
	output, err := f.tmuxOutput(context.Background(), "capture-pane", "-p", "-t", endpoint.ID, "-S", "-200")
	if err != nil {
		t.Fatalf("capture pane %d: %v", pane, err)
	}
	return string(output)
}

func (f *atomicAssignmentCLIFixture) runNTM(t *testing.T, env map[string]string, args ...string) atomicAssignmentProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.ntmPath, args...)
	cmd.Dir = f.projectDir
	cmd.Env = atomicAssignmentMergeEnv(f.env, env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm command timed out: %q", args)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run ntm %q: %v", args, err)
		}
	}
	t.Logf("[E2E-ATOMIC] exit=%d args=%q stdout=%s stderr=%s", exitCode, args, truncateString(stdout.String(), 500), truncateString(stderr.String(), 500))
	return atomicAssignmentProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func (f *atomicAssignmentCLIFixture) mustBR(t *testing.T, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.brPath, args...)
	cmd.Dir = f.projectDir
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("br command timed out: %q", args)
	}
	if err != nil {
		t.Fatalf("br %q: %v output=%s", args, err, output)
	}
	return output
}

func (f *atomicAssignmentCLIFixture) mustTMUX(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.runTMUX(ctx, args...); err != nil {
		t.Fatalf("tmux %s: %v", strings.Join(args, " "), err)
	}
}

func (f *atomicAssignmentCLIFixture) runTMUX(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *atomicAssignmentCLIFixture) tmuxOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func atomicAssignmentIsolatedEnv(overrides map[string]string) []string {
	replaced := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {},
		"TMUX": {}, "TMUX_PANE": {}, "TMUX_TMPDIR": {},
		"NTM_CONFIG": {}, "NTM_OUTPUT_FORMAT": {}, "NTM_ROBOT_FORMAT": {}, "TOON_DEFAULT_FORMAT": {},
		"AGENT_MAIL_URL": {}, "AGENT_MAIL_TOKEN": {},
		"HTTP_PROXY": {}, "HTTPS_PROXY": {}, "ALL_PROXY": {}, "NO_PROXY": {},
	}
	for key := range overrides {
		replaced[key] = struct{}{}
	}
	result := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := replaced[key]; !skip {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}

func atomicAssignmentMergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return append([]string(nil), base...)
	}
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	sort.Strings(result)
	return result
}
