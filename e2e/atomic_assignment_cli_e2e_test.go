//go:build e2e
// +build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
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
	tmuxConfig string
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
			BeadTitle  string `json:"bead_title"`
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
	BeadTitle             string     `json:"bead_title"`
	Pane                  int        `json:"pane"`
	AgentType             string     `json:"agent_type"`
	AgentName             string     `json:"agent_name"`
	Status                string     `json:"status"`
	AssignedAt            time.Time  `json:"assigned_at"`
	CompletedAt           *time.Time `json:"completed_at"`
	RetryCount            int        `json:"retry_count"`
	PromptSent            string     `json:"prompt_sent"`
	IdempotencyKey        string     `json:"idempotency_key"`
	ClaimActor            string     `json:"claim_actor"`
	ClaimState            string     `json:"claim_state"`
	ClaimStatus           string     `json:"claim_status"`
	ClaimAttempts         int        `json:"claim_attempts"`
	ClaimedAt             *time.Time `json:"claimed_at"`
	ReservationRequired   bool       `json:"reservation_required"`
	ReservationDiscovery  bool       `json:"reservation_discovery"`
	ReservationInputPaths []string   `json:"reservation_input_paths"`
	ReservationState      string     `json:"reservation_state"`
	ReservationAttempts   int        `json:"reservation_attempts"`
	ReservationCompleted  bool       `json:"reservation_completed"`
	ReservedPaths         []string   `json:"reserved_paths"`
	ReservationIDs        []int      `json:"reservation_ids"`
	ReservationExpiresAt  *time.Time `json:"reservation_expires_at"`
	ReservationError      string     `json:"reservation_error"`
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
	LastDispatchError     string     `json:"last_dispatch_error"`
	ClearState            string     `json:"clear_state"`
	ClearError            string     `json:"clear_error"`
}

type atomicAssignmentRetryEnvelope struct {
	Command    string `json:"command"`
	Subcommand string `json:"subcommand"`
	Session    string `json:"session"`
	Success    bool   `json:"success"`
	Data       *struct {
		Retried []struct {
			BeadID     string `json:"bead_id"`
			Pane       int    `json:"pane"`
			Status     string `json:"status"`
			PromptSent bool   `json:"prompt_sent"`
			RetryCount int    `json:"retry_count"`
		} `json:"retried"`
		Skipped []struct {
			BeadID string `json:"bead_id"`
			Reason string `json:"reason"`
		} `json:"skipped"`
		Summary struct {
			TotalFailed  int `json:"total_failed"`
			RetriedCount int `json:"retried_count"`
			SkippedCount int `json:"skipped_count"`
		} `json:"summary"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type atomicAssignmentReassignEnvelope struct {
	Command    string `json:"command"`
	Subcommand string `json:"subcommand"`
	Session    string `json:"session"`
	Success    bool   `json:"success"`
	Data       *struct {
		BeadID                       string `json:"bead_id"`
		BeadTitle                    string `json:"bead_title"`
		Pane                         int    `json:"pane"`
		AgentType                    string `json:"agent_type"`
		AgentName                    string `json:"agent_name"`
		Status                       string `json:"status"`
		PromptSent                   bool   `json:"prompt_sent"`
		PreviousPane                 int    `json:"previous_pane"`
		PreviousAgent                string `json:"previous_agent"`
		PreviousAgentType            string `json:"previous_agent_type"`
		PreviousStatus               string `json:"previous_status"`
		FileReservationsTransferred  bool   `json:"file_reservations_transferred"`
		FileReservationsReleasedFrom int    `json:"file_reservations_released_from"`
		FileReservationsCreatedFor   int    `json:"file_reservations_created_for"`
	} `json:"data"`
	Error *struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

type atomicAssignmentBead struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func TestE2EAtomicAssignmentIsolatedEnvScrubsHostOverrides(t *testing.T) {
	for _, key := range []string{"BR_DB", "BD_DB", "BEADS_DB", "GIT_DIR", "GIT_WORK_TREE", "AGENT_NAME", "PWD", "OLDPWD"} {
		t.Setenv(key, "/should/not/escape")
	}
	env := atomicAssignmentIsolatedEnv(map[string]string{"HOME": t.TempDir()})
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "BR_DB", "BD_DB", "BEADS_DB", "GIT_DIR", "GIT_WORK_TREE", "AGENT_NAME", "PWD", "OLDPWD":
			t.Fatalf("isolated process environment retained %s", key)
		}
	}
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

func TestE2EAtomicAssignmentReservationRecoveryBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	const zeroTitle = "Zero grant internal/cli/zero_grant.go"
	const partialTitle = "Partial grant internal/cli/partial_grant.go internal/robot/partial_grant.go"
	const unknownTitle = "Unknown response internal/cli/unknown_response.go"
	zeroBeadID := fixture.createBead(t, zeroTitle)
	partialBeadID := fixture.createBead(t, partialTitle)
	unknownBeadID := fixture.createBead(t, unknownTitle)
	templatePath := filepath.Join(fixture.projectDir, "atomic-reservation-template.txt")
	templateBody := fmt.Sprintf("NTM_ATOMIC_RESERVATION_%d::{BEAD_ID}::{TITLE}", time.Now().UnixNano())
	if err := os.WriteFile(templatePath, []byte(templateBody), 0o600); err != nil {
		t.Fatalf("write reservation prompt template: %v", err)
	}
	reservationPrompt := func(beadID, title string) string {
		return strings.ReplaceAll(strings.ReplaceAll(templateBody, "{BEAD_ID}", beadID), "{TITLE}", title)
	}
	reservationArgs := func(pane int, beadID string) []string {
		return []string{
			"assign", fixture.session, "--repo=" + fixture.projectDir,
			fmt.Sprintf("--pane=%d", pane), "--beads=" + beadID,
			"--template=custom", "--template-file=" + templatePath,
			"--force", "--ignore-deps", "--timeout=15s", "--json",
		}
	}
	zeroPrompt := reservationPrompt(zeroBeadID, zeroTitle)
	partialPrompt := reservationPrompt(partialBeadID, partialTitle)
	unknownPrompt := reservationPrompt(unknownBeadID, unknownTitle)

	var stubMu sync.Mutex
	modes := map[string]string{zeroBeadID: "zero", partialBeadID: "partial", unknownBeadID: "drop_after_grant"}
	reserveCalls := make(map[string]int)
	activeReservations := make(map[string][]map[string]any)
	var releasedIDs []int
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/liveness" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.JSONRPC == "2.0" && request.Method == "resources/read" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32601, "message": "resource view not supported"},
			})
			return
		}
		if request.JSONRPC != "2.0" || request.Method != "tools/call" {
			http.Error(w, "expected JSON-RPC tools/call", http.StatusBadRequest)
			return
		}
		writeResult := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
		switch request.Params.Name {
		case "health_check":
			writeResult(map[string]any{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339Nano)})
		case "file_reservation_paths":
			reason, _ := request.Params.Arguments["reason"].(string)
			beadID := strings.TrimPrefix(reason, "bead assignment: ")
			paths, pathsOK := anyStringSlice(request.Params.Arguments["paths"])
			if request.Params.Arguments["project_key"] != fixture.projectDir || !pathsOK || len(paths) == 0 || beadID == reason {
				http.Error(w, fmt.Sprintf("unexpected reservation args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			stubMu.Lock()
			mode := modes[beadID]
			reserveCalls[beadID]++
			stubMu.Unlock()
			makeGrant := func(id int, path string) map[string]any {
				now := time.Now().UTC()
				return map[string]any{
					"id": id, "path_pattern": path, "agent_name": request.Params.Arguments["agent_name"],
					"project_id": 1, "exclusive": true, "reason": reason,
					"created_ts": now.Format(time.RFC3339Nano), "expires_ts": now.Add(time.Hour).Format(time.RFC3339Nano),
				}
			}
			switch mode {
			case "zero":
				writeResult(map[string]any{
					"granted":   []any{},
					"conflicts": []map[string]any{{"path": paths[0], "holders": []string{"OtherAgent"}}},
				})
			case "partial":
				if len(paths) < 2 {
					http.Error(w, fmt.Sprintf("partial fixture needs two paths: %v", paths), http.StatusBadRequest)
					return
				}
				writeResult(map[string]any{
					"granted":   []map[string]any{makeGrant(9201, paths[0])},
					"conflicts": []map[string]any{{"path": paths[1], "holders": []string{"OtherAgent"}}},
				})
			case "success":
				granted := make([]map[string]any, 0, len(paths))
				for index, path := range paths {
					granted = append(granted, makeGrant(9101+index, path))
				}
				stubMu.Lock()
				activeReservations[beadID] = append([]map[string]any(nil), granted...)
				stubMu.Unlock()
				writeResult(map[string]any{"granted": granted, "conflicts": []any{}})
			case "drop_after_grant":
				granted := make([]map[string]any, 0, len(paths))
				for index, path := range paths {
					granted = append(granted, makeGrant(9301+index, path))
				}
				stubMu.Lock()
				activeReservations[beadID] = append([]map[string]any(nil), granted...)
				stubMu.Unlock()
				panic(http.ErrAbortHandler)
			default:
				http.Error(w, "unknown reservation mode for "+beadID, http.StatusBadRequest)
			}
		case "list_file_reservations":
			stubMu.Lock()
			var active []map[string]any
			for _, reservations := range activeReservations {
				active = append(active, reservations...)
			}
			stubMu.Unlock()
			writeResult(active)
		case "release_file_reservations":
			ids, idsOK := atomicAssignmentAnyIntSlice(request.Params.Arguments["file_reservation_ids"])
			if request.Params.Arguments["project_key"] != fixture.projectDir || !idsOK || len(ids) == 0 {
				http.Error(w, fmt.Sprintf("unexpected release args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			stubMu.Lock()
			releasedIDs = append(releasedIDs, ids...)
			stubMu.Unlock()
			writeResult(map[string]any{"released": len(ids)})
		default:
			http.Error(w, "unexpected tool: "+request.Params.Name, http.StatusNotFound)
		}
	}))
	defer mailServer.Close()
	env := map[string]string{"AGENT_MAIL_URL": mailServer.URL + "/"}

	zeroFailure := fixture.runNTM(t, env, reservationArgs(0, zeroBeadID)...)
	if zeroFailure.exitCode != 1 || len(bytes.TrimSpace(zeroFailure.stderr)) != 0 {
		t.Fatalf("zero-grant assignment exit=%d stdout=%s stderr=%s", zeroFailure.exitCode, zeroFailure.stdout, zeroFailure.stderr)
	}
	var zeroFailureEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, zeroFailure.stdout, &zeroFailureEnvelope)
	if zeroFailureEnvelope.Success || zeroFailureEnvelope.Error == nil || zeroFailureEnvelope.Error.Code != "ASSIGN_ERROR" {
		t.Fatalf("zero-grant envelope = %+v", zeroFailureEnvelope)
	}
	zeroPending := fixture.readLedgerAssignment(t, zeroBeadID)
	if zeroPending.ReservationState != "failed" || zeroPending.ReservationAttempts != 1 || zeroPending.ReservationError == "" ||
		len(zeroPending.ReservationIDs) != 0 || len(zeroPending.ReservedPaths) != 0 || zeroPending.DispatchAttempts != 0 {
		t.Fatalf("zero-grant durable state = %+v", zeroPending)
	}
	fixture.assertMarkerCounts(t, zeroPrompt, map[int]int{0: 0, 1: 0})
	stubMu.Lock()
	modes[zeroBeadID] = "success"
	stubMu.Unlock()
	fixture.primePaneForSafeDispatch(t, 0)
	// The production observer requires the explicit prompt to remain quiet for
	// its five-second activity threshold before it authorizes actuation.
	time.Sleep(5500 * time.Millisecond)
	zeroRetry := fixture.runNTM(t, env,
		"assign", fixture.session, "--repo="+fixture.projectDir,
		"--retry="+zeroBeadID, "--timeout=15s", "--json",
	)
	if zeroRetry.exitCode != 0 || len(bytes.TrimSpace(zeroRetry.stderr)) != 0 {
		t.Fatalf("zero-grant recovery exit=%d stdout=%s stderr=%s", zeroRetry.exitCode, zeroRetry.stdout, zeroRetry.stderr)
	}
	fixture.waitForMarkerCount(t, 0, zeroPrompt, 1)
	zeroRecovered := fixture.readLedgerAssignment(t, zeroBeadID)
	if zeroRecovered.ReservationState != "reserved" || !zeroRecovered.ReservationCompleted || len(zeroRecovered.ReservationIDs) == 0 ||
		zeroRecovered.DispatchState != "sent" || zeroRecovered.DispatchAttempts != 1 {
		t.Fatalf("zero-grant recovery durable state = %+v", zeroRecovered)
	}

	partialFailure := fixture.runNTM(t, env, reservationArgs(1, partialBeadID)...)
	if partialFailure.exitCode != 1 || len(bytes.TrimSpace(partialFailure.stderr)) != 0 {
		t.Fatalf("partial-grant assignment exit=%d stdout=%s stderr=%s", partialFailure.exitCode, partialFailure.stdout, partialFailure.stderr)
	}
	partialPending := fixture.readLedgerAssignment(t, partialBeadID)
	if partialPending.ReservationState != "reserved" || partialPending.ReservationCompleted ||
		!reflect.DeepEqual(partialPending.ReservationIDs, []int{9201}) || len(partialPending.ReservedPaths) != 1 ||
		!strings.Contains(partialPending.ReservationError, "file reservation must be reconciled or released") || partialPending.DispatchAttempts != 0 {
		t.Fatalf("partial-grant durable state = %+v", partialPending)
	}
	stubMu.Lock()
	partialReserveCalls := reserveCalls[partialBeadID]
	stubMu.Unlock()
	partialRetry := fixture.runNTM(t, env,
		"assign", fixture.session, "--repo="+fixture.projectDir,
		"--retry="+partialBeadID, "--timeout=15s", "--json",
	)
	if partialRetry.exitCode != 1 || len(bytes.TrimSpace(partialRetry.stderr)) != 0 {
		t.Fatalf("partial-grant retry exit=%d stdout=%s stderr=%s", partialRetry.exitCode, partialRetry.stdout, partialRetry.stderr)
	}
	var partialRetryEnvelope atomicAssignmentRetryEnvelope
	decodeAtomicAssignmentJSON(t, partialRetry.stdout, &partialRetryEnvelope)
	if partialRetryEnvelope.Success || partialRetryEnvelope.Error == nil || partialRetryEnvelope.Error.Code != "RETRY_SKIPPED" ||
		partialRetryEnvelope.Data == nil || partialRetryEnvelope.Data.Summary.RetriedCount != 0 || partialRetryEnvelope.Data.Summary.SkippedCount != 1 {
		t.Fatalf("partial-grant retry envelope = %+v", partialRetryEnvelope)
	}
	stubMu.Lock()
	if reserveCalls[partialBeadID] != partialReserveCalls {
		t.Fatalf("partial-grant retry repeated reservation: before=%d after=%d", partialReserveCalls, reserveCalls[partialBeadID])
	}
	stubMu.Unlock()
	fixture.assertMarkerCounts(t, partialPrompt, map[int]int{0: 0, 1: 0})

	partialClear := fixture.runNTM(t, env,
		"--json", "assign", fixture.session, "--repo="+fixture.projectDir,
		"--clear="+partialBeadID, "--timeout=15s",
	)
	if partialClear.exitCode != 0 || len(bytes.TrimSpace(partialClear.stderr)) != 0 {
		t.Fatalf("partial-grant clear exit=%d stdout=%s stderr=%s", partialClear.exitCode, partialClear.stdout, partialClear.stderr)
	}
	fixture.assertLedgerHasNoAssignment(t, partialBeadID)
	stubMu.Lock()
	if !reflect.DeepEqual(releasedIDs, []int{9201}) {
		t.Fatalf("released reservation IDs=%v, want [9201]", releasedIDs)
	}
	stubMu.Unlock()

	unknownFailure := fixture.runNTM(t, env, reservationArgs(1, unknownBeadID)...)
	if unknownFailure.exitCode != 1 || len(bytes.TrimSpace(unknownFailure.stderr)) != 0 {
		t.Fatalf("unknown-response assignment exit=%d stdout=%s stderr=%s", unknownFailure.exitCode, unknownFailure.stdout, unknownFailure.stderr)
	}
	unknownPending := fixture.readLedgerAssignment(t, unknownBeadID)
	if unknownPending.ReservationState != "unknown" || unknownPending.ReservationAttempts != 1 || unknownPending.ReservationError == "" ||
		len(unknownPending.ReservationIDs) != 0 || unknownPending.DispatchAttempts != 0 {
		t.Fatalf("unknown-response durable state = %+v", unknownPending)
	}
	fixture.assertMarkerCounts(t, unknownPrompt, map[int]int{0: 0, 1: 0})
	fixture.primePaneForSafeDispatch(t, 1)
	time.Sleep(5500 * time.Millisecond)
	unknownRetry := fixture.runNTM(t, env,
		"assign", fixture.session, "--repo="+fixture.projectDir,
		"--retry="+unknownBeadID, "--timeout=15s", "--json",
	)
	if unknownRetry.exitCode != 0 || len(bytes.TrimSpace(unknownRetry.stderr)) != 0 {
		t.Fatalf("unknown-response recovery exit=%d stdout=%s stderr=%s", unknownRetry.exitCode, unknownRetry.stdout, unknownRetry.stderr)
	}
	fixture.waitForMarkerCount(t, 1, unknownPrompt, 1)
	unknownRecovered := fixture.readLedgerAssignment(t, unknownBeadID)
	if unknownRecovered.ReservationState != "reserved" || !unknownRecovered.ReservationCompleted ||
		!reflect.DeepEqual(unknownRecovered.ReservationIDs, []int{9301, 9302}) || unknownRecovered.DispatchState != "sent" || unknownRecovered.DispatchAttempts != 1 {
		t.Fatalf("unknown-response recovered state = %+v", unknownRecovered)
	}
	stubMu.Lock()
	if reserveCalls[unknownBeadID] != 1 {
		t.Fatalf("unknown-response recovery repeated reservation %d times", reserveCalls[unknownBeadID])
	}
	stubMu.Unlock()
}

func TestE2EAtomicAssignmentCustomTemplateProcessIdentity(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	const title = "Custom template process identity"
	beadID := fixture.createBead(t, title)
	templatePath := filepath.Join(fixture.projectDir, "atomic-direct-template.txt")
	marker := fmt.Sprintf("NTM_ATOMIC_TEMPLATE_%d", time.Now().UnixNano())
	originalTemplate := marker + "::{BEAD_ID}::{TITLE}\n"
	changedTemplate := marker + "::changed::{BEAD_ID}::{TITLE}\n"
	originalPrompt := strings.ReplaceAll(strings.ReplaceAll(originalTemplate, "{BEAD_ID}", beadID), "{TITLE}", title)
	changedPrompt := strings.ReplaceAll(strings.ReplaceAll(changedTemplate, "{BEAD_ID}", beadID), "{TITLE}", title)
	if err := os.WriteFile(templatePath, []byte(originalTemplate), 0o600); err != nil {
		t.Fatalf("write direct template: %v", err)
	}
	args := atomicDirectTemplateArgs(fixture, 0, beadID, templatePath)

	firstResult := fixture.runNTM(t, nil, args...)
	if firstResult.exitCode != 0 || len(bytes.TrimSpace(firstResult.stderr)) != 0 {
		t.Fatalf("custom-template assignment exit=%d stdout=%s stderr=%s", firstResult.exitCode, firstResult.stdout, firstResult.stderr)
	}
	var first atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, firstResult.stdout, &first)
	assertDirectAssignmentEnvelope(t, first, fixture.session, beadID, originalPrompt, fixture.panes[0])
	fixture.waitForMarkerCount(t, 0, marker, 1)
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	firstRecord := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentRecord(t, firstRecord, beadID, originalPrompt, fixture.panes[0], "codex")

	replayResult := fixture.runNTM(t, nil, args...)
	if replayResult.exitCode != 0 || len(bytes.TrimSpace(replayResult.stderr)) != 0 {
		t.Fatalf("custom-template replay exit=%d stdout=%s stderr=%s", replayResult.exitCode, replayResult.stdout, replayResult.stderr)
	}
	var replay atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, replayResult.stdout, &replay)
	assertDirectAssignmentEnvelope(t, replay, fixture.session, beadID, originalPrompt, fixture.panes[0])
	assertSameDirectReceipt(t, first, replay)
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})

	if err := os.WriteFile(templatePath, []byte(changedTemplate), 0o600); err != nil {
		t.Fatalf("change direct template: %v", err)
	}
	changedResult := fixture.runNTM(t, nil, args...)
	if changedResult.exitCode == 0 || len(bytes.TrimSpace(changedResult.stderr)) != 0 {
		t.Fatalf("changed-template intent exit=%d stdout=%s stderr=%s", changedResult.exitCode, changedResult.stdout, changedResult.stderr)
	}
	var changed atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, changedResult.stdout, &changed)
	if changed.Success || changed.Error == nil || changed.Error.Code != "CLAIM_CONFLICT" {
		t.Fatalf("changed-template intent envelope = %+v", changed)
	}
	fixture.assertMarkerCounts(t, changedPrompt, map[int]int{0: 0, 1: 0})
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	afterConflict := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentReceiptUnchanged(t, firstRecord, afterConflict)

	if err := os.WriteFile(templatePath, []byte(originalTemplate), 0o600); err != nil {
		t.Fatalf("restore direct template: %v", err)
	}
	restoredResult := fixture.runNTM(t, nil, args...)
	if restoredResult.exitCode != 0 || len(bytes.TrimSpace(restoredResult.stderr)) != 0 {
		t.Fatalf("restored-template replay exit=%d stdout=%s stderr=%s", restoredResult.exitCode, restoredResult.stdout, restoredResult.stderr)
	}
	var restored atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, restoredResult.stdout, &restored)
	assertDirectAssignmentEnvelope(t, restored, fixture.session, beadID, originalPrompt, fixture.panes[0])
	assertSameDirectReceipt(t, first, restored)
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	assertAtomicAssignmentReceiptUnchanged(t, firstRecord, fixture.readLedgerAssignment(t, beadID))
}

func TestE2EAtomicAssignmentConcurrentBuiltProcesses(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	const title = "Concurrent built process assignment"
	beadID := fixture.createBead(t, title)
	templatePath := filepath.Join(fixture.projectDir, "atomic-contended-template.txt")
	marker := fmt.Sprintf("NTM_ATOMIC_CONTENDED_%d", time.Now().UnixNano())
	templateBody := marker + "::{BEAD_ID}::{TITLE}"
	prompt := strings.ReplaceAll(strings.ReplaceAll(templateBody, "{BEAD_ID}", beadID), "{TITLE}", title)
	if err := os.WriteFile(templatePath, []byte(templateBody), 0o600); err != nil {
		t.Fatalf("write contended template: %v", err)
	}
	args := atomicDirectTemplateArgs(fixture, 0, beadID, templatePath)
	gateDir := filepath.Join(fixture.root, "contended-claim-bin")
	if err := os.MkdirAll(gateDir, 0o700); err != nil {
		t.Fatalf("create contended claim wrapper directory: %v", err)
	}
	enteredPath := filepath.Join(gateDir, "claim-finalization-entered")
	releasePath := filepath.Join(gateDir, "release-claim-finalization")
	claimWrapper := strings.Join([]string{
		"#!/bin/sh",
		"real_br=" + tmux.ShellQuote(fixture.brPath),
		"entered=" + tmux.ShellQuote(enteredPath),
		"release=" + tmux.ShellQuote(releasePath),
		`"$real_br" "$@"`,
		"status=$?",
		"sync_command=0",
		"flush_only=0",
		`for arg in "$@"; do`,
		`  if [ "$arg" = "sync" ]; then sync_command=1; fi`,
		`  if [ "$arg" = "--flush-only" ]; then flush_only=1; fi`,
		"done",
		`guarded_sync=$((sync_command * flush_only))`,
		`if [ "$status" -eq 0 ] && [ "$guarded_sync" -eq 1 ]; then`,
		`  printf 'guarded-sync\n' >> "$entered"`,
		`  while [ ! -e "$release" ]; do sleep 0.01; done`,
		"fi",
		`exit "$status"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(gateDir, "br"), []byte(claimWrapper), 0o700); err != nil {
		t.Fatalf("write contended claim wrapper: %v", err)
	}
	path := gateDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(fixture.env, "PATH")
	afterStart := func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			data, err := os.ReadFile(enteredPath)
			if err == nil && len(data) > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no contender reached guarded claim finalization: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(250 * time.Millisecond)
		data, err := os.ReadFile(enteredPath)
		if err != nil {
			t.Fatalf("read contended claim counter: %v", err)
		}
		if got := strings.Count(string(data), "guarded-sync\n"); got != 1 {
			t.Fatalf("%d contenders crossed guarded claim finalization while the first held the atomic lock; want 1", got)
		}
		if err := os.WriteFile(releasePath, []byte("release\n"), 0o600); err != nil {
			t.Fatalf("release contended claim gate: %v", err)
		}
	}
	results := fixture.runNTMConcurrentAfterStart(t, 2, map[string]string{"PATH": path}, afterStart, args...)
	envelopes := make([]atomicAssignmentDirectEnvelope, len(results))
	for i, result := range results {
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("contender %d exit=%d stdout=%s stderr=%s", i, result.exitCode, result.stdout, result.stderr)
		}
		decodeAtomicAssignmentJSON(t, result.stdout, &envelopes[i])
		assertDirectAssignmentEnvelope(t, envelopes[i], fixture.session, beadID, prompt, fixture.panes[0])
	}
	assertSameDirectReceipt(t, envelopes[0], envelopes[1])
	fixture.waitForMarkerCount(t, 0, marker, 1)
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	record := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentRecord(t, record, beadID, prompt, fixture.panes[0], "codex")
	if record.ClaimAttempts != 1 {
		t.Fatalf("contenders performed %d durable claim attempts, want 1: %+v", record.ClaimAttempts, record)
	}
	claimCalls, err := os.ReadFile(enteredPath)
	if err != nil {
		t.Fatalf("read final contended claim counter: %v", err)
	}
	if got := strings.Count(string(claimCalls), "guarded-sync\n"); got != 1 {
		t.Fatalf("contenders performed %d guarded claim finalizations, want exactly 1", got)
	}

	changedTemplate := templateBody + "::different"
	if err := os.WriteFile(templatePath, []byte(changedTemplate), 0o600); err != nil {
		t.Fatalf("change contended template: %v", err)
	}
	conflictResult := fixture.runNTM(t, nil, args...)
	if conflictResult.exitCode == 0 || len(bytes.TrimSpace(conflictResult.stderr)) != 0 {
		t.Fatalf("post-contention conflict exit=%d stdout=%s stderr=%s", conflictResult.exitCode, conflictResult.stdout, conflictResult.stderr)
	}
	var conflict atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, conflictResult.stdout, &conflict)
	if conflict.Success || conflict.Error == nil || conflict.Error.Code != "CLAIM_CONFLICT" {
		t.Fatalf("post-contention conflict envelope = %+v", conflict)
	}
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	assertAtomicAssignmentReceiptUnchanged(t, record, fixture.readLedgerAssignment(t, beadID))
}

func TestE2EAtomicAssignmentTerminalGenerationBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	beadID := fixture.createBead(t, "Terminal assignment generation")
	prompt := fmt.Sprintf("NTM_ATOMIC_TERMINAL_%d", time.Now().UnixNano())
	args := atomicDirectArgs(fixture, beadID, prompt, false)

	firstResult := fixture.runNTM(t, nil, args...)
	if firstResult.exitCode != 0 || len(bytes.TrimSpace(firstResult.stderr)) != 0 {
		t.Fatalf("initial generation exit=%d stdout=%s stderr=%s", firstResult.exitCode, firstResult.stdout, firstResult.stderr)
	}
	var first atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, firstResult.stdout, &first)
	assertDirectAssignmentEnvelope(t, first, fixture.session, beadID, prompt, fixture.panes[0])
	fixture.waitForMarkerCount(t, 0, prompt, 1)
	firstRecord := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentRecord(t, firstRecord, beadID, prompt, fixture.panes[0], "codex")

	fixture.mustBR(t, "close", beadID, "--reason=terminal-generation-e2e", "--json")
	fixture.assertBead(t, beadID, "closed", firstRecord.ClaimActor)
	fixture.setLedgerAssignmentStatus(t, beadID, "completed")

	closedResult := fixture.runNTM(t, nil, args...)
	if closedResult.exitCode == 0 || len(bytes.TrimSpace(closedResult.stderr)) != 0 {
		t.Fatalf("closed generation retry exit=%d stdout=%s stderr=%s", closedResult.exitCode, closedResult.stdout, closedResult.stderr)
	}
	var closed atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, closedResult.stdout, &closed)
	if closed.Success || closed.Error == nil || closed.Error.Code != "BEAD_NOT_REOPENED" || !strings.Contains(closed.Error.Message, "tracker status is \"closed\"") {
		t.Fatalf("closed generation retry envelope = %+v", closed)
	}
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 1, 1: 0})
	assertAtomicAssignmentReceiptUnchanged(t, firstRecord, fixture.readLedgerAssignment(t, beadID))
	fixture.assertBead(t, beadID, "closed", firstRecord.ClaimActor)

	fixture.mustBR(t, "reopen", beadID, "--reason=terminal-generation-e2e", "--json")
	fixture.assertBead(t, beadID, "open", firstRecord.ClaimActor)

	secondResult := fixture.runNTM(t, nil, args...)
	if secondResult.exitCode != 0 || len(bytes.TrimSpace(secondResult.stderr)) != 0 {
		t.Fatalf("second generation exit=%d stdout=%s stderr=%s", secondResult.exitCode, secondResult.stdout, secondResult.stderr)
	}
	var second atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, secondResult.stdout, &second)
	assertDirectAssignmentEnvelope(t, second, fixture.session, beadID, prompt, fixture.panes[0])
	fixture.waitForMarkerCount(t, 0, prompt, 2)
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 2, 1: 0})
	secondRecord := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentRecordWithClaimIdentity(t, secondRecord, beadID, prompt, fixture.panes[0], "codex", false)
	if secondRecord.IdempotencyKey == firstRecord.IdempotencyKey {
		t.Fatalf("terminal generation reused idempotency key %q", secondRecord.IdempotencyKey)
	}
	if secondRecord.ClaimActor != firstRecord.ClaimActor {
		t.Fatalf("terminal generation changed retained br actor: first=%q second=%q", firstRecord.ClaimActor, secondRecord.ClaimActor)
	}
	if secondRecord.DispatchReceiptID == firstRecord.DispatchReceiptID ||
		(secondRecord.DispatchedAt != nil && firstRecord.DispatchedAt != nil && secondRecord.DispatchedAt.Equal(*firstRecord.DispatchedAt)) {
		t.Fatalf("terminal generation reused delivery receipt: first=%+v second=%+v", firstRecord, secondRecord)
	}
	fixture.assertBead(t, beadID, "in_progress", firstRecord.ClaimActor)
}

func TestE2EAtomicAssignmentFreshTerminalBeadsAreGuarded(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	closedBeadID := fixture.createBead(t, "Fresh closed guarded assignment")
	tombstonedBeadID := fixture.createBead(t, "Fresh tombstoned guarded assignment")
	fixture.mustBR(t, "close", closedBeadID, "--reason=fresh-terminal-e2e", "--json")
	fixture.mustBR(t, "delete", tombstonedBeadID, "--reason=fresh-terminal-e2e", "--json")

	for index, test := range []struct {
		name   string
		beadID string
		status string
	}{
		{name: "closed", beadID: closedBeadID, status: "closed"},
		{name: "tombstoned", beadID: tombstonedBeadID, status: "tombstone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompt := fmt.Sprintf("NTM_ATOMIC_FRESH_TERMINAL_%s_%d", strings.ToUpper(test.name), time.Now().UnixNano())
			result := fixture.runNTM(t, nil, atomicDirectArgsForPane(fixture, index, test.beadID, prompt, false)...)
			if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
				t.Fatalf("fresh %s assignment exit=%d stdout=%s stderr=%s", test.name, result.exitCode, result.stdout, result.stderr)
			}
			var envelope atomicAssignmentDirectEnvelope
			decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
			if envelope.Success || envelope.Error == nil || envelope.Error.Code != "CLAIM_CONFLICT" ||
				!strings.Contains(strings.ToLower(envelope.Error.Message), test.status) {
				t.Fatalf("fresh %s guarded envelope = %+v", test.name, envelope)
			}
			fixture.assertBead(t, test.beadID, test.status, "")
			fixture.assertMarkerCounts(t, prompt, map[int]int{0: 0, 1: 0})
			record := fixture.readLedgerAssignment(t, test.beadID)
			if record.Status != "failed" || record.ClaimState != "failed" || record.ClaimAttempts != 1 ||
				record.DispatchAttempts != 0 || record.PromptSent != "" || record.DispatchReceiptID != "" || record.DispatchedAt != nil {
				t.Fatalf("fresh %s refusal durable state = %+v", test.name, record)
			}
		})
	}

	bulkClosedBeadID := fixture.createBead(t, "Fresh closed guarded bulk assignment")
	bulkTombstonedBeadID := fixture.createBead(t, "Fresh tombstoned guarded bulk assignment")
	fixture.mustBR(t, "close", bulkClosedBeadID, "--reason=fresh-terminal-bulk-e2e", "--json")
	fixture.mustBR(t, "delete", bulkTombstonedBeadID, "--reason=fresh-terminal-bulk-e2e", "--json")
	bulkTemplate := filepath.Join(fixture.projectDir, "atomic-fresh-terminal-bulk-template.txt")
	bulkTemplateBody := fmt.Sprintf("NTM_ATOMIC_FRESH_TERMINAL_BULK_%d_{bead_id}", time.Now().UnixNano())
	if err := os.WriteFile(bulkTemplate, []byte(bulkTemplateBody), 0o600); err != nil {
		t.Fatalf("write fresh terminal bulk template: %v", err)
	}
	fixture.primePaneForSafeDispatch(t, 1)
	time.Sleep(5500 * time.Millisecond)
	for _, test := range []struct {
		name   string
		beadID string
		status string
	}{
		{name: "closed_bulk", beadID: bulkClosedBeadID, status: "closed"},
		{name: "tombstoned_bulk", beadID: bulkTombstonedBeadID, status: "tombstone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prompt := strings.ReplaceAll(bulkTemplateBody, "{bead_id}", test.beadID)
			result := fixture.runNTM(t, nil, atomicBulkArgs(fixture, test.beadID, bulkTemplate)...)
			if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
				t.Fatalf("fresh %s assignment exit=%d stdout=%s stderr=%s", test.name, result.exitCode, result.stdout, result.stderr)
			}
			var envelope atomicAssignmentBulkEnvelope
			decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
			if envelope.Success || envelope.Summary.Assigned != 0 || envelope.Summary.Failed != 1 || len(envelope.Assignments) != 1 ||
				envelope.Assignments[0].Bead != test.beadID || envelope.Assignments[0].Claimed || envelope.Assignments[0].PromptSent ||
				!strings.Contains(strings.ToLower(envelope.Assignments[0].Error), test.status) {
				t.Fatalf("fresh %s guarded bulk envelope = %+v", test.name, envelope)
			}
			fixture.assertBead(t, test.beadID, test.status, "")
			fixture.assertMarkerCounts(t, prompt, map[int]int{0: 0, 1: 0})
			record := fixture.readLedgerAssignment(t, test.beadID)
			if record.Status != "failed" || record.ClaimState != "failed" || record.DispatchAttempts != 0 || record.DispatchReceiptID != "" {
				t.Fatalf("fresh %s bulk refusal durable state = %+v", test.name, record)
			}
		})
	}
}

func TestE2EAtomicAssignmentCloseForceClearReopenGeneration(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	beadID := fixture.createBead(t, "Close clear reopen assignment generation")
	prompt := fmt.Sprintf("NTM_ATOMIC_CLOSE_CLEAR_REOPEN_%d", time.Now().UnixNano())
	args := atomicDirectArgs(fixture, beadID, prompt, false)

	firstResult := fixture.runNTM(t, nil, args...)
	if firstResult.exitCode != 0 || len(bytes.TrimSpace(firstResult.stderr)) != 0 {
		t.Fatalf("initial lifecycle assignment exit=%d stdout=%s stderr=%s", firstResult.exitCode, firstResult.stdout, firstResult.stderr)
	}
	fixture.waitForMarkerCount(t, 0, prompt, 1)
	first := fixture.readLedgerAssignment(t, beadID)
	fixture.mustBR(t, "close", beadID, "--reason=close-clear-reopen-e2e", "--json")
	fixture.assertBead(t, beadID, "closed", first.ClaimActor)
	fixture.setLedgerAssignmentStatus(t, beadID, "completed")

	clearResult := fixture.runNTM(t, nil,
		"--json", "assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--clear="+beadID,
		"--force",
		"--timeout=15s",
	)
	if clearResult.exitCode != 0 || len(bytes.TrimSpace(clearResult.stderr)) != 0 {
		t.Fatalf("force clear terminal assignment exit=%d stdout=%s stderr=%s", clearResult.exitCode, clearResult.stdout, clearResult.stderr)
	}
	var clearEnvelope struct {
		Success bool `json:"success"`
		Data    struct {
			Cleared []struct {
				BeadID  string `json:"bead_id"`
				Success bool   `json:"success"`
			} `json:"cleared"`
			Summary struct {
				ClearedCount int `json:"cleared_count"`
				FailedCount  int `json:"failed_count"`
			} `json:"summary"`
		} `json:"data"`
	}
	decodeAtomicAssignmentJSON(t, clearResult.stdout, &clearEnvelope)
	if !clearEnvelope.Success || clearEnvelope.Data.Summary.ClearedCount != 1 || clearEnvelope.Data.Summary.FailedCount != 0 ||
		len(clearEnvelope.Data.Cleared) != 1 || clearEnvelope.Data.Cleared[0].BeadID != beadID || !clearEnvelope.Data.Cleared[0].Success {
		t.Fatalf("force clear terminal envelope = %+v", clearEnvelope)
	}
	fixture.assertLedgerHasNoAssignment(t, beadID)
	fixture.assertBead(t, beadID, "closed", "")

	fixture.mustBR(t, "reopen", beadID, "--reason=close-clear-reopen-e2e", "--json")
	fixture.assertBead(t, beadID, "open", "")
	secondResult := fixture.runNTM(t, nil, args...)
	if secondResult.exitCode != 0 || len(bytes.TrimSpace(secondResult.stderr)) != 0 {
		t.Fatalf("reopened lifecycle assignment exit=%d stdout=%s stderr=%s", secondResult.exitCode, secondResult.stdout, secondResult.stderr)
	}
	fixture.waitForMarkerCount(t, 0, prompt, 2)
	second := fixture.readLedgerAssignment(t, beadID)
	if second.IdempotencyKey == first.IdempotencyKey || second.ClaimActor == first.ClaimActor ||
		second.DispatchReceiptID == first.DispatchReceiptID || second.DispatchAttempts != 1 {
		t.Fatalf("reopened generation reused terminal identity: first=%+v second=%+v", first, second)
	}
	fixture.assertBead(t, beadID, "in_progress", second.ClaimActor)
}

func TestE2EAtomicAssignmentClearPaneLeaseFailureIsDurable(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	beadID := fixture.createBead(t, "Clear pane lease barrier")
	prompt := fmt.Sprintf("NTM_ATOMIC_CLEAR_%d", time.Now().UnixNano())
	assigned := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, prompt, false)...)
	if assigned.exitCode != 0 || len(bytes.TrimSpace(assigned.stderr)) != 0 {
		t.Fatalf("seed assignment exit=%d stdout=%s stderr=%s", assigned.exitCode, assigned.stdout, assigned.stderr)
	}
	fixture.waitForMarkerCount(t, 0, prompt, 1)
	before := fixture.readLedgerAssignment(t, beadID)
	fixture.setLedgerAssignmentReservations(t, beadID, []string{"internal/cli/**"}, []int{987654})

	cleared := fixture.runNTM(t, nil,
		"--json", "assign", fixture.session,
		"--clear-pane=0", "--timeout=2s",
	)
	if cleared.exitCode != 1 || len(bytes.TrimSpace(cleared.stderr)) != 0 {
		t.Fatalf("clear-pane exit=%d stdout=%s stderr=%s", cleared.exitCode, cleared.stdout, cleared.stderr)
	}
	var envelope struct {
		Success bool `json:"success"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data struct {
			Cleared []struct {
				BeadID  string `json:"bead_id"`
				Success bool   `json:"success"`
				Error   string `json:"error"`
			} `json:"cleared"`
			Summary struct {
				ClearedCount int `json:"cleared_count"`
				FailedCount  int `json:"failed_count"`
			} `json:"summary"`
		} `json:"data"`
	}
	decodeAtomicAssignmentJSON(t, cleared.stdout, &envelope)
	if envelope.Success || envelope.Error == nil || envelope.Error.Code != "CLEAR_FAILED" ||
		envelope.Data.Summary.ClearedCount != 0 || envelope.Data.Summary.FailedCount != 1 || len(envelope.Data.Cleared) != 1 ||
		envelope.Data.Cleared[0].BeadID != beadID || envelope.Data.Cleared[0].Success ||
		!strings.Contains(envelope.Data.Cleared[0].Error, "Agent Mail is unavailable") {
		t.Fatalf("clear-pane failure envelope = %+v", envelope)
	}
	after := fixture.readLedgerAssignment(t, beadID)
	if after.ClearState != "reservation_releasing" || !strings.Contains(after.ClearError, "Agent Mail is unavailable") ||
		!reflect.DeepEqual(after.ReservationIDs, []int{987654}) || !reflect.DeepEqual(after.ReservedPaths, []string{"internal/cli/**"}) {
		t.Fatalf("clear-pane failure did not retain release barrier: %+v", after)
	}
	assertAtomicAssignmentReceiptUnchanged(t, before, after)
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 1, 1: 0})

	var releaseCalls atomic.Int32
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/liveness" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.JSONRPC == "2.0" && request.Method == "tools/call" && request.Params.Name == "health_check" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339Nano)},
			})
			return
		}
		ids, idsOK := atomicAssignmentAnyIntSlice(request.Params.Arguments["file_reservation_ids"])
		if request.JSONRPC != "2.0" || request.Method != "tools/call" || request.Params.Name != "release_file_reservations" ||
			request.Params.Arguments["project_key"] != fixture.projectDir || request.Params.Arguments["agent_name"] != after.AgentName ||
			!idsOK || !reflect.DeepEqual(ids, []int{987654}) {
			http.Error(w, fmt.Sprintf("unexpected reservation release: method=%q name=%q args=%#v", request.Method, request.Params.Name, request.Params.Arguments), http.StatusBadRequest)
			return
		}
		releaseCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  map[string]any{"released": 1},
		})
	}))
	defer mailServer.Close()

	recovered := fixture.runNTM(t, map[string]string{"AGENT_MAIL_URL": mailServer.URL + "/"},
		"--json", "assign", fixture.session,
		"--clear-pane=0", "--timeout=2s",
	)
	if recovered.exitCode != 0 || len(bytes.TrimSpace(recovered.stderr)) != 0 {
		t.Fatalf("recovered clear-pane exit=%d stdout=%s stderr=%s", recovered.exitCode, recovered.stdout, recovered.stderr)
	}
	var recoveredEnvelope struct {
		Success bool `json:"success"`
		Data    struct {
			Cleared []struct {
				BeadID  string `json:"bead_id"`
				Success bool   `json:"success"`
			} `json:"cleared"`
			Summary struct {
				ClearedCount int `json:"cleared_count"`
				FailedCount  int `json:"failed_count"`
			} `json:"summary"`
		} `json:"data"`
	}
	decodeAtomicAssignmentJSON(t, recovered.stdout, &recoveredEnvelope)
	if !recoveredEnvelope.Success || recoveredEnvelope.Data.Summary.ClearedCount != 1 || recoveredEnvelope.Data.Summary.FailedCount != 0 ||
		len(recoveredEnvelope.Data.Cleared) != 1 || recoveredEnvelope.Data.Cleared[0].BeadID != beadID || !recoveredEnvelope.Data.Cleared[0].Success {
		t.Fatalf("recovered clear-pane envelope = %+v", recoveredEnvelope)
	}
	if releaseCalls.Load() != 1 {
		t.Fatalf("reservation release calls=%d, want exactly 1", releaseCalls.Load())
	}
	ledger, err := fixture.readLedger()
	if err != nil {
		t.Fatalf("read ledger after recovered clear: %v", err)
	}
	if _, exists := ledger.Assignments[beadID]; exists {
		t.Fatalf("recovered clear retained assignment %s: %+v", beadID, ledger.Assignments[beadID])
	}

	reassigned := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, prompt, false)...)
	if reassigned.exitCode != 0 || len(bytes.TrimSpace(reassigned.stderr)) != 0 {
		t.Fatalf("post-clear reassignment exit=%d stdout=%s stderr=%s", reassigned.exitCode, reassigned.stdout, reassigned.stderr)
	}
	fixture.waitForMarkerCount(t, 0, prompt, 2)
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 2, 1: 0})
	reassignedRecord := fixture.readLedgerAssignment(t, beadID)
	if reassignedRecord.IdempotencyKey == before.IdempotencyKey || reassignedRecord.DispatchReceiptID == before.DispatchReceiptID {
		t.Fatalf("post-clear assignment reused prior receipt: before=%+v after=%+v", before, reassignedRecord)
	}
}

func TestE2EAtomicAssignmentClearFailedBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	failedBeadID := fixture.createBead(t, "Completion detector failure")
	retainedBeadID := fixture.createBead(t, "Healthy assignment retained")
	failedPrompt := fmt.Sprintf("NTM_ATOMIC_FAILED_%d", time.Now().UnixNano())
	retainedPrompt := fmt.Sprintf("NTM_ATOMIC_RETAINED_%d", time.Now().UnixNano())
	for _, seed := range []struct {
		pane   int
		beadID string
		prompt string
	}{
		{pane: 0, beadID: failedBeadID, prompt: failedPrompt},
		{pane: 1, beadID: retainedBeadID, prompt: retainedPrompt},
	} {
		result := fixture.runNTM(t, nil, atomicDirectArgsForPane(fixture, seed.pane, seed.beadID, seed.prompt, false)...)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("seed pane %d exit=%d stdout=%s stderr=%s", seed.pane, result.exitCode, result.stdout, result.stderr)
		}
		fixture.waitForMarkerCount(t, seed.pane, seed.prompt, 1)
	}
	retainedBefore := fixture.readLedgerAssignment(t, retainedBeadID)
	fixture.mustTMUX(t, "send-keys", "-t", fixture.panes[0].ID, "-l", "ERROR: fatal assignment failure")
	fixture.mustTMUX(t, "send-keys", "-t", fixture.panes[0].ID, "Enter")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	watch := exec.CommandContext(ctx, fixture.ntmPath,
		"assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--watch",
		"--watch-interval=100ms",
		"--dry-run",
		"--reserve-files=false",
		"--quiet",
	)
	watch.Dir = fixture.projectDir
	watch.Env = append([]string(nil), fixture.env...)
	var watchStdout bytes.Buffer
	var watchStderr bytes.Buffer
	watch.Stdout = &watchStdout
	watch.Stderr = &watchStderr
	if err := watch.Start(); err != nil {
		t.Fatalf("start assignment watch: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		ledger, err := fixture.readLedger()
		if err == nil {
			if record := ledger.Assignments[failedBeadID]; record != nil && record.Status == "failed" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("completion detector did not mark %s failed: read_err=%v stdout=%s stderr=%s", failedBeadID, err, watchStdout.String(), watchStderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := watch.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal assignment watch: %v", err)
	}
	if err := watch.Wait(); err != nil {
		t.Fatalf("assignment watch did not exit cleanly: %v stdout=%s stderr=%s", err, watchStdout.String(), watchStderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("assignment watch exceeded deadline: %v", ctx.Err())
	}
	if len(bytes.TrimSpace(watchStderr.Bytes())) != 0 {
		t.Fatalf("assignment watch stderr=%s", watchStderr.String())
	}
	retainedAfterDetection := fixture.readLedgerAssignment(t, retainedBeadID)
	if retainedAfterDetection.Status != "assigned" && retainedAfterDetection.Status != "working" {
		t.Fatalf("healthy assignment entered unexpected state during failure detection: %+v", retainedAfterDetection)
	}
	assertAtomicAssignmentReceiptUnchanged(t, retainedBefore, retainedAfterDetection)

	cleared := fixture.runNTM(t, nil,
		"--json", "assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--clear-failed",
		"--timeout=15s",
	)
	if cleared.exitCode != 0 || len(bytes.TrimSpace(cleared.stderr)) != 0 {
		t.Fatalf("clear-failed exit=%d stdout=%s stderr=%s", cleared.exitCode, cleared.stdout, cleared.stderr)
	}
	var envelope struct {
		Success    bool   `json:"success"`
		Subcommand string `json:"subcommand"`
		Data       struct {
			Cleared []struct {
				BeadID  string `json:"bead_id"`
				Success bool   `json:"success"`
			} `json:"cleared"`
			Summary struct {
				ClearedCount int `json:"cleared_count"`
				FailedCount  int `json:"failed_count"`
			} `json:"summary"`
		} `json:"data"`
	}
	decodeAtomicAssignmentJSON(t, cleared.stdout, &envelope)
	if !envelope.Success || envelope.Subcommand != "clear-failed" || envelope.Data.Summary.ClearedCount != 1 ||
		envelope.Data.Summary.FailedCount != 0 || len(envelope.Data.Cleared) != 1 ||
		envelope.Data.Cleared[0].BeadID != failedBeadID || !envelope.Data.Cleared[0].Success {
		t.Fatalf("clear-failed envelope = %+v", envelope)
	}
	fixture.assertLedgerHasNoAssignment(t, failedBeadID)
	assertAtomicAssignmentReceiptUnchanged(t, retainedBefore, fixture.readLedgerAssignment(t, retainedBeadID))
	fixture.assertMarkerCounts(t, retainedPrompt, map[int]int{0: 0, 1: 1})
}

func TestE2EAtomicCoordinatorRunOnceBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	beadID := fixture.createBead(t, "Coordinator once delivery")
	for pane := range fixture.panes {
		fixture.primePaneForSafeDispatch(t, pane)
	}
	time.Sleep(5500 * time.Millisecond)

	for key, value := range map[string]string{
		"HOME":            fixture.homeDir,
		"XDG_CONFIG_HOME": filepath.Join(fixture.root, "config"),
		"XDG_DATA_HOME":   filepath.Join(fixture.root, "data"),
	} {
		t.Setenv(key, value)
	}
	registry := agentmail.NewSessionAgentRegistry(fixture.session, fixture.projectDir)
	for pane, endpoint := range fixture.panes {
		registry.AddAgent(endpoint.Title, endpoint.ID, fmt.Sprintf("CoordinatorAgent%d", pane))
	}
	if err := agentmail.SaveSessionAgentRegistry(registry); err != nil {
		t.Fatalf("save coordinator Agent Mail registry: %v", err)
	}
	configPath := filepath.Join(fixture.root, "config", "ntm", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("create coordinator config directory: %v", err)
	}
	coordinatorConfig := strings.Join([]string{
		"[coordinator]",
		"auto_assign = true",
		"assign_only_idle = true",
		"send_digests = false",
		"poll_interval = \"100ms\"",
		"digest_interval = \"1h\"",
		"idle_threshold = 0",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(coordinatorConfig), 0o600); err != nil {
		t.Fatalf("write coordinator config: %v", err)
	}

	binDir := filepath.Join(fixture.root, "coordinator-bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create coordinator bin directory: %v", err)
	}
	bvPayloadPath := filepath.Join(fixture.root, "triage.json")
	writeBVPayload := func(id, title string) {
		t.Helper()
		payload := fmt.Sprintf(`{"generated_at":"2026-07-12T00:00:00Z","data_hash":"e2e","triage":{"meta":{"version":"1","generated_at":"2026-07-12T00:00:00Z","phase2_ready":true,"issue_count":1},"quick_ref":{"open_count":1,"actionable_count":1,"blocked_count":0,"in_progress_count":0,"top_picks":[]},"recommendations":[{"id":%q,"title":%q,"type":"task","status":"open","priority":1,"score":1,"action":"assign","reasons":[]}]}}`, id, title)
		if err := os.WriteFile(bvPayloadPath, []byte(payload), 0o600); err != nil {
			t.Fatalf("write bv payload: %v", err)
		}
	}
	writeBVPayload(beadID, "Coordinator once delivery")
	bvScript := "#!/bin/sh\n" +
		"if [ \"$#\" -ne 1 ] || [ \"$1\" != \"--robot-triage\" ]; then\n" +
		"  printf 'unexpected bv args: %s\\n' \"$*\" >&2\n" +
		"  exit 64\n" +
		"fi\n" +
		"cat \"$NTM_E2E_BV_PAYLOAD\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "bv"), []byte(bvScript), 0o700); err != nil {
		t.Fatalf("write bv fixture: %v", err)
	}

	var sendCount atomic.Int32
	var reserveCount atomic.Int32
	var releaseCount atomic.Int32
	var reservationMu sync.Mutex
	var releasedReservationIDs []int
	var activeCoordinatorReservations []map[string]any
	var dropReservationResponse atomic.Bool
	var invalidReceipt atomic.Bool
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/liveness" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var request struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Method == "tools/call" && request.Params.Name == "health_check" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"result": map[string]any{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339Nano)},
			})
			return
		}
		if request.Method != "tools/call" {
			http.Error(w, "unexpected Agent Mail method", http.StatusNotFound)
			return
		}
		writeResult := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
		switch request.Params.Name {
		case "send_message":
			recipients, _ := request.Params.Arguments["to"].([]any)
			if len(recipients) != 1 {
				http.Error(w, "expected one recipient", http.StatusBadRequest)
				return
			}
			if request.Method == "resources/read" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": request.ID,
					"error": map[string]any{"code": -32601, "message": "resource view not supported"},
				})
				return
			}
			recipient, _ := recipients[0].(string)
			messageID := int(sendCount.Add(1)) + 7000
			if invalidReceipt.Load() {
				writeResult(map[string]any{"count": 0, "deliveries": []any{}})
				return
			}
			writeResult(map[string]any{
				"count": 1,
				"deliveries": []map[string]any{{
					"project": fixture.projectDir,
					"payload": map[string]any{"id": messageID, "to": []string{recipient}},
				}},
			})
		case "file_reservation_paths":
			paths, ok := anyStringSlice(request.Params.Arguments["paths"])
			agentName, _ := request.Params.Arguments["agent_name"].(string)
			reason, _ := request.Params.Arguments["reason"].(string)
			if !ok || len(paths) == 0 || agentName == "" || request.Params.Arguments["project_key"] != fixture.projectDir {
				http.Error(w, fmt.Sprintf("unexpected coordinator reservation args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			baseID := int(reserveCount.Add(1))*100 + 8000
			now := time.Now().UTC()
			granted := make([]map[string]any, 0, len(paths))
			for index, path := range paths {
				granted = append(granted, map[string]any{
					"id": baseID + index, "path_pattern": path, "agent_name": agentName,
					"project_id": 1, "exclusive": true, "reason": reason,
					"created_ts": now.Format(time.RFC3339Nano), "expires_ts": now.Add(time.Hour).Format(time.RFC3339Nano),
				})
			}
			reservationMu.Lock()
			activeCoordinatorReservations = append(activeCoordinatorReservations, granted...)
			reservationMu.Unlock()
			if dropReservationResponse.Swap(false) {
				panic(http.ErrAbortHandler)
			}
			writeResult(map[string]any{"granted": granted, "conflicts": []any{}})
		case "list_file_reservations":
			reservationMu.Lock()
			active := append([]map[string]any(nil), activeCoordinatorReservations...)
			reservationMu.Unlock()
			writeResult(active)
		case "release_file_reservations":
			ids, ok := atomicAssignmentAnyIntSlice(request.Params.Arguments["file_reservation_ids"])
			if !ok || len(ids) == 0 || request.Params.Arguments["project_key"] != fixture.projectDir {
				http.Error(w, fmt.Sprintf("unexpected coordinator release args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			reservationMu.Lock()
			releasedReservationIDs = append(releasedReservationIDs, ids...)
			wanted := make(map[int]struct{}, len(ids))
			for _, id := range ids {
				wanted[id] = struct{}{}
			}
			remaining := activeCoordinatorReservations[:0]
			for _, reservation := range activeCoordinatorReservations {
				id, _ := reservation["id"].(int)
				if _, released := wanted[id]; !released {
					remaining = append(remaining, reservation)
				}
			}
			activeCoordinatorReservations = remaining
			reservationMu.Unlock()
			releaseCount.Add(1)
			writeResult(map[string]any{"released": len(ids)})
		default:
			http.Error(w, "unexpected Agent Mail tool "+request.Params.Name, http.StatusNotFound)
		}
	}))
	defer mailServer.Close()

	env := map[string]string{
		"PATH":               binDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(fixture.env, "PATH"),
		"NTM_E2E_BV_PAYLOAD": bvPayloadPath,
		"AGENT_MAIL_URL":     mailServer.URL + "/",
	}
	runOnce := func() (atomicAssignmentProcessResult, struct {
		Success     bool   `json:"success"`
		Once        bool   `json:"once"`
		AutoAssign  bool   `json:"auto_assign"`
		ErrorCode   string `json:"error_code"`
		Error       string `json:"error"`
		Assignments []struct {
			Success        bool   `json:"success"`
			MessageSent    bool   `json:"message_sent"`
			IdempotencyKey string `json:"idempotency_key"`
		} `json:"assignments"`
	}) {
		t.Helper()
		result := fixture.runNTM(t, env, "--json", "coordinator", "run", fixture.session, "--once")
		var envelope struct {
			Success     bool   `json:"success"`
			Once        bool   `json:"once"`
			AutoAssign  bool   `json:"auto_assign"`
			ErrorCode   string `json:"error_code"`
			Error       string `json:"error"`
			Assignments []struct {
				Success        bool   `json:"success"`
				MessageSent    bool   `json:"message_sent"`
				IdempotencyKey string `json:"idempotency_key"`
			} `json:"assignments"`
		}
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		return result, envelope
	}

	firstResult, first := runOnce()
	if firstResult.exitCode != 0 || len(bytes.TrimSpace(firstResult.stderr)) != 0 || !first.Success || !first.Once || !first.AutoAssign ||
		len(first.Assignments) != 1 || !first.Assignments[0].Success || !first.Assignments[0].MessageSent || first.Assignments[0].IdempotencyKey == "" {
		t.Fatalf("first coordinator cycle result=%+v envelope=%+v", firstResult, first)
	}
	if sendCount.Load() != 1 {
		t.Fatalf("first coordinator cycle deliveries=%d, want 1", sendCount.Load())
	}
	firstRecord := fixture.readLedgerAssignment(t, beadID)
	if firstRecord.DispatchState != "sent" || firstRecord.DispatchReceiptID != "7001" || firstRecord.IdempotencyKey != first.Assignments[0].IdempotencyKey {
		t.Fatalf("coordinator durable receipt = %+v", firstRecord)
	}
	fixture.assertBead(t, beadID, "in_progress", firstRecord.ClaimActor)

	secondResult, second := runOnce()
	if secondResult.exitCode != 0 || len(bytes.TrimSpace(secondResult.stderr)) != 0 || !second.Success || !second.Once || !second.AutoAssign || len(second.Assignments) != 0 {
		t.Fatalf("second coordinator cycle result=%+v envelope=%+v", secondResult, second)
	}
	if sendCount.Load() != 1 {
		t.Fatalf("coordinator restart deliveries=%d, want exactly 1", sendCount.Load())
	}
	assertAtomicAssignmentReceiptUnchanged(t, firstRecord, fixture.readLedgerAssignment(t, beadID))

	recoveryTitle := "Coordinator reserve recovery internal/coordinator/recovery.go"
	recoveryBeadID := fixture.createBead(t, recoveryTitle)
	writeBVPayload(recoveryBeadID, recoveryTitle)
	dropReservationResponse.Store(true)
	recoveryFailureResult, recoveryFailure := runOnce()
	if recoveryFailureResult.exitCode != 1 || len(bytes.TrimSpace(recoveryFailureResult.stderr)) != 0 || recoveryFailure.Success ||
		recoveryFailure.ErrorCode != "ASSIGNMENT_FAILED" || len(recoveryFailure.Assignments) != 1 || recoveryFailure.Assignments[0].Success {
		t.Fatalf("coordinator reservation-loss cycle result=%+v envelope=%+v", recoveryFailureResult, recoveryFailure)
	}
	recoveryPending := fixture.readLedgerAssignment(t, recoveryBeadID)
	if recoveryPending.ReservationState != "unknown" || recoveryPending.ReservationAttempts != 1 ||
		recoveryPending.DispatchAttempts != 0 || recoveryPending.IdempotencyKey == "" {
		t.Fatalf("coordinator reservation-loss ledger = %+v", recoveryPending)
	}
	reserveCallsBeforeRecovery := reserveCount.Load()
	recoveryResult, recoveredEnvelope := runOnce()
	if recoveryResult.exitCode != 0 || len(bytes.TrimSpace(recoveryResult.stderr)) != 0 || !recoveredEnvelope.Success ||
		len(recoveredEnvelope.Assignments) != 1 || !recoveredEnvelope.Assignments[0].Success || !recoveredEnvelope.Assignments[0].MessageSent ||
		recoveredEnvelope.Assignments[0].IdempotencyKey != recoveryPending.IdempotencyKey {
		t.Fatalf("coordinator reservation recovery result=%+v envelope=%+v", recoveryResult, recoveredEnvelope)
	}
	if reserveCount.Load() != reserveCallsBeforeRecovery || sendCount.Load() != 2 {
		t.Fatalf("coordinator reservation recovery reserve=%d/%d send=%d, want no repeat and 2 sends", reserveCallsBeforeRecovery, reserveCount.Load(), sendCount.Load())
	}
	recoveryRecord := fixture.readLedgerAssignment(t, recoveryBeadID)
	if recoveryRecord.ReservationState != "reserved" || !recoveryRecord.ReservationCompleted || len(recoveryRecord.ReservationIDs) != 1 ||
		recoveryRecord.DispatchState != "sent" || recoveryRecord.DispatchReceiptID == "" {
		t.Fatalf("coordinator recovered reservation ledger = %+v", recoveryRecord)
	}
	fixture.mustBR(t, "close", recoveryBeadID, "--reason=coordinator-recovery-e2e", "--json")
	writeBVPayload(beadID, "Coordinator once delivery")
	closedRecoveryResult, closedRecovery := runOnce()
	if closedRecoveryResult.exitCode != 0 || len(bytes.TrimSpace(closedRecoveryResult.stderr)) != 0 || !closedRecovery.Success || len(closedRecovery.Assignments) != 0 {
		t.Fatalf("coordinator recovered lease cleanup result=%+v envelope=%+v", closedRecoveryResult, closedRecovery)
	}
	closedRecoveryRecord := fixture.readLedgerAssignment(t, recoveryBeadID)
	if closedRecoveryRecord.Status != "completed" || closedRecoveryRecord.ReservationState != "released" ||
		len(closedRecoveryRecord.ReservationIDs) != 0 || releaseCount.Load() != 1 {
		t.Fatalf("coordinator recovered lease cleanup ledger=%+v releases=%d", closedRecoveryRecord, releaseCount.Load())
	}

	pollingTitle := "Coordinator polling internal/coordinator/e2e_reserved.go"
	pollingBeadID := fixture.createBead(t, pollingTitle)
	writeBVPayload(pollingBeadID, pollingTitle)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	longRun := exec.CommandContext(ctx, fixture.ntmPath, "--json", "coordinator", "run", fixture.session)
	longRun.Dir = fixture.projectDir
	longRun.Env = atomicAssignmentMergeEnv(fixture.env, env)
	stdout, err := longRun.StdoutPipe()
	if err != nil {
		t.Fatalf("coordinator stdout pipe: %v", err)
	}
	var longRunStderr bytes.Buffer
	longRun.Stderr = &longRunStderr
	if err := longRun.Start(); err != nil {
		t.Fatalf("start long-running coordinator: %v", err)
	}
	startupLine := make(chan struct {
		line []byte
		err  error
	}, 1)
	go func() {
		line, readErr := bufio.NewReader(stdout).ReadBytes('\n')
		startupLine <- struct {
			line []byte
			err  error
		}{line: line, err: readErr}
	}()
	var startup struct {
		Success    bool `json:"success"`
		Once       bool `json:"once"`
		AutoAssign bool `json:"auto_assign"`
	}
	select {
	case received := <-startupLine:
		if received.err != nil {
			t.Fatalf("read coordinator startup envelope: %v stderr=%s", received.err, longRunStderr.String())
		}
		decodeAtomicAssignmentJSON(t, received.line, &startup)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for coordinator startup envelope")
	}
	if !startup.Success || startup.Once || !startup.AutoAssign {
		t.Fatalf("long-running coordinator startup = %+v", startup)
	}
	waitForRecord := func(id string, predicate func(*atomicAssignmentRecord) bool, description string) *atomicAssignmentRecord {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			ledger, readErr := fixture.readLedger()
			if readErr == nil {
				if record := ledger.Assignments[id]; record != nil && predicate(record) {
					return record
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s for %s: read_err=%v stderr=%s", description, id, readErr, longRunStderr.String())
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	pollingRecord := waitForRecord(pollingBeadID, func(record *atomicAssignmentRecord) bool {
		return record.DispatchState == "sent" && record.DispatchReceiptID != ""
	}, "ticker-driven durable dispatch")
	if sendCount.Load() != 3 {
		t.Fatalf("ticker-driven coordinator deliveries=%d, want 3 total", sendCount.Load())
	}
	fixture.assertBead(t, pollingBeadID, "in_progress", pollingRecord.ClaimActor)

	fixture.mustBR(t, "close", pollingBeadID, "--reason=coordinator-completion-e2e", "--json")
	successorBeadID := fixture.createBead(t, "Coordinator completion successor")
	writeBVPayload(successorBeadID, "Coordinator completion successor")
	completedPolling := waitForRecord(pollingBeadID, func(record *atomicAssignmentRecord) bool {
		return record.Status == "completed" && record.CompletedAt != nil
	}, "production completion transition")
	if completedPolling.ReservationState != "released" || completedPolling.ReservationCompleted ||
		len(completedPolling.ReservationIDs) != 0 || len(completedPolling.ReservedPaths) != 0 {
		t.Fatalf("completion retained coordinator reservation handles: %+v", completedPolling)
	}
	if reserveCount.Load() != 2 || releaseCount.Load() != 2 {
		t.Fatalf("coordinator reservation lifecycle reserve=%d release=%d, want 2/2", reserveCount.Load(), releaseCount.Load())
	}
	reservationMu.Lock()
	if len(releasedReservationIDs) != 2 || releasedReservationIDs[0] <= 0 || releasedReservationIDs[1] <= 0 || releasedReservationIDs[0] == releasedReservationIDs[1] {
		t.Fatalf("coordinator released reservation IDs = %v", releasedReservationIDs)
	}
	reservationMu.Unlock()
	successorRecord := waitForRecord(successorBeadID, func(record *atomicAssignmentRecord) bool {
		return record.DispatchState == "sent" && record.DispatchReceiptID != ""
	}, "post-completion successor dispatch")
	if successorRecord.OccupancyKey != pollingRecord.OccupancyKey {
		t.Fatalf("successor did not reuse the completion-freed pane: completed=%q successor=%q", pollingRecord.OccupancyKey, successorRecord.OccupancyKey)
	}
	if sendCount.Load() != 4 {
		t.Fatalf("post-completion coordinator deliveries=%d, want 4 total", sendCount.Load())
	}
	fixture.assertBead(t, successorBeadID, "in_progress", successorRecord.ClaimActor)

	// Keep triage pointed at the still-active first assignment while the
	// successor reaches terminal state, so the freed pane cannot be filled by
	// a repeated recommendation for a just-closed work item.
	writeBVPayload(beadID, "Coordinator once delivery")
	fixture.mustBR(t, "close", successorBeadID, "--reason=coordinator-completion-e2e", "--json")
	waitForRecord(successorBeadID, func(record *atomicAssignmentRecord) bool {
		return record.Status == "completed" && record.CompletedAt != nil
	}, "successor completion transition")
	if err := longRun.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal coordinator: %v", err)
	}
	if err := longRun.Wait(); err != nil {
		t.Fatalf("long-running coordinator did not exit cleanly: %v stderr=%s", err, longRunStderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("long-running coordinator exceeded deadline: %v", ctx.Err())
	}
	if len(bytes.TrimSpace(longRunStderr.Bytes())) != 0 {
		t.Fatalf("long-running coordinator stderr=%s", longRunStderr.String())
	}

	failureBeadID := fixture.createBead(t, "Coordinator invalid receipt")
	writeBVPayload(failureBeadID, "Coordinator invalid receipt")
	invalidReceipt.Store(true)
	failureResult, failure := runOnce()
	if failureResult.exitCode != 1 || len(bytes.TrimSpace(failureResult.stderr)) != 0 || failure.Success ||
		failure.ErrorCode != "ASSIGNMENT_FAILED" || failure.Error == "" || len(failure.Assignments) != 1 ||
		failure.Assignments[0].Success || failure.Assignments[0].MessageSent {
		t.Fatalf("failed coordinator cycle result=%+v envelope=%+v", failureResult, failure)
	}
	if sendCount.Load() != 5 {
		t.Fatalf("failed coordinator cycle deliveries=%d, want one attempted delivery after four durable sends", sendCount.Load())
	}
	failureRecord := fixture.readLedgerAssignment(t, failureBeadID)
	if failureRecord.DispatchState != "sending" || failureRecord.DispatchReceiptID != "" || failureRecord.DispatchAttempts != 1 {
		t.Fatalf("failed coordinator durable outcome = %+v", failureRecord)
	}
}

func TestE2EAtomicAssignmentAutoBuiltProcessRestart(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	const title = "Automatic built process assignment"
	beadID := fixture.createBead(t, title)
	templatePath := filepath.Join(fixture.projectDir, "atomic-auto-template.txt")
	marker := fmt.Sprintf("NTM_ATOMIC_AUTO_%d", time.Now().UnixNano())
	templateBody := marker + "::{BEAD_ID}::{TITLE}"
	prompt := strings.ReplaceAll(strings.ReplaceAll(templateBody, "{BEAD_ID}", beadID), "{TITLE}", title)
	if err := os.WriteFile(templatePath, []byte(templateBody), 0o600); err != nil {
		t.Fatalf("write automatic assignment template: %v", err)
	}
	fixture.primePaneForSafeDispatch(t, 0)
	// Production observation requires both an explicit agent prompt and five
	// seconds without pane activity before authorizing an automatic dispatch.
	time.Sleep(5500 * time.Millisecond)
	args := []string{
		"assign", fixture.session,
		"--repo=" + fixture.projectDir,
		"--auto",
		"--beads=" + beadID,
		"--limit=1",
		"--cod-only",
		"--template=custom",
		"--template-file=" + templatePath,
		"--reserve-files=false",
		"--ignore-deps",
		"--timeout=15s",
		"--quiet",
	}

	first := fixture.runNTM(t, nil, args...)
	if first.exitCode != 0 || len(bytes.TrimSpace(first.stderr)) != 0 {
		t.Fatalf("automatic assignment exit=%d stdout=%s stderr=%s", first.exitCode, first.stdout, first.stderr)
	}
	fixture.waitForMarkerCount(t, 0, marker, 1)
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	firstRecord := fixture.readLedgerAssignment(t, beadID)
	assertAtomicAssignmentRecord(t, firstRecord, beadID, prompt, fixture.panes[0], "cod")

	second := fixture.runNTM(t, nil, args...)
	if second.exitCode != 0 || len(bytes.TrimSpace(second.stderr)) != 0 {
		t.Fatalf("automatic restart exit=%d stdout=%s stderr=%s", second.exitCode, second.stdout, second.stderr)
	}
	fixture.assertMarkerCounts(t, marker, map[int]int{0: 1, 1: 0})
	assertAtomicAssignmentReceiptUnchanged(t, firstRecord, fixture.readLedgerAssignment(t, beadID))
	fixture.assertBead(t, beadID, "in_progress", firstRecord.ClaimActor)
}

func TestE2EAtomicAssignmentRepositoryScopeBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	overrideRepo := filepath.Join(fixture.root, "override-project")
	if err := os.MkdirAll(overrideRepo, 0o700); err != nil {
		t.Fatalf("create override repository: %v", err)
	}
	fixture.mustBRAt(t, overrideRepo, "init", "--prefix=scopeb", "--json")
	overrideBeadID := strings.TrimSpace(string(fixture.mustBRAt(t, overrideRepo,
		"create", "Authoritative CLI repository override", "--type=task", "--priority=1", "--silent")))
	if overrideBeadID == "" {
		t.Fatal("override repository bead ID is empty")
	}
	overridePrompt := fmt.Sprintf("NTM_ATOMIC_REPO_OVERRIDE_%d", time.Now().UnixNano())
	cliResult := fixture.runNTM(t, nil,
		"assign", fixture.session,
		"--repo="+overrideRepo,
		"--pane=0",
		"--beads="+overrideBeadID,
		"--prompt="+overridePrompt,
		"--reserve-files=false",
		"--force",
		"--ignore-deps",
		"--timeout=15s",
		"--json",
	)
	if cliResult.exitCode != 0 || len(bytes.TrimSpace(cliResult.stderr)) != 0 {
		t.Fatalf("authoritative --repo assignment exit=%d stdout=%s stderr=%s", cliResult.exitCode, cliResult.stdout, cliResult.stderr)
	}
	fixture.waitForMarkerCount(t, 0, overridePrompt, 1)
	overrideRecord := fixture.readLedgerAssignment(t, overrideBeadID)
	fixture.assertBeadAt(t, overrideRepo, overrideBeadID, "in_progress", overrideRecord.ClaimActor)
	fixture.assertBeadAbsentAt(t, fixture.projectDir, overrideBeadID)

	remoteBeadID := fixture.createBead(t, "Robot explicit-session repository scope")
	remoteTemplate := filepath.Join(fixture.projectDir, "atomic-remote-scope-template.txt")
	remoteTemplateBody := fmt.Sprintf("NTM_ATOMIC_REMOTE_SCOPE_%d_{bead_id}", time.Now().UnixNano())
	if err := os.WriteFile(remoteTemplate, []byte(remoteTemplateBody), 0o600); err != nil {
		t.Fatalf("write remote scope template: %v", err)
	}
	fixture.primePaneForSafeDispatch(t, 1)
	robotResult := fixture.runNTMInDir(t, overrideRepo, nil, atomicBulkArgs(fixture, remoteBeadID, remoteTemplate)...)
	if robotResult.exitCode != 0 || len(bytes.TrimSpace(robotResult.stderr)) != 0 {
		t.Fatalf("remote robot assignment exit=%d stdout=%s stderr=%s", robotResult.exitCode, robotResult.stdout, robotResult.stderr)
	}
	var robotEnvelope atomicAssignmentBulkEnvelope
	decodeAtomicAssignmentJSON(t, robotResult.stdout, &robotEnvelope)
	assertBulkAssignmentEnvelope(t, robotEnvelope, remoteBeadID, "1")
	remotePrompt := strings.ReplaceAll(remoteTemplateBody, "{bead_id}", remoteBeadID)
	fixture.waitForMarkerCount(t, 1, remotePrompt, 1)
	remoteRecord := fixture.readLedgerAssignment(t, remoteBeadID)
	fixture.assertBead(t, remoteBeadID, "in_progress", remoteRecord.ClaimActor)
	fixture.assertBeadAbsentAt(t, overrideRepo, remoteBeadID)
}

func TestE2EAtomicAssignmentRemoteRepositoryDependenciesAndDefaultPrompt(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	callerRepo := filepath.Join(fixture.root, "caller-project")
	if err := os.MkdirAll(callerRepo, 0o700); err != nil {
		t.Fatalf("create caller repository: %v", err)
	}
	fixture.mustBRAt(t, callerRepo, "init", "--prefix=caller", "--json")

	const title = "Remote repository default prompt title"
	blockerID := fixture.createBead(t, "Remote repository blocker")
	beadID := fixture.createBead(t, title)
	fixture.mustBR(t, "dep", "add", beadID, blockerID, "--type=blocks", "--json")
	args := []string{
		"assign", fixture.session,
		"--repo=" + fixture.projectDir,
		"--pane=0",
		"--beads=" + beadID,
		"--reserve-files=false",
		"--force",
		"--timeout=15s",
		"--json",
	}
	expectedPrompt := fmt.Sprintf("Work on bead %s: %s. Check dependencies first with `br dep tree %s`.", beadID, title, beadID)

	blocked := fixture.runNTMInDir(t, callerRepo, nil, args...)
	if blocked.exitCode != 1 || len(bytes.TrimSpace(blocked.stderr)) != 0 {
		t.Fatalf("remote blocked assignment exit=%d stdout=%s stderr=%s", blocked.exitCode, blocked.stdout, blocked.stderr)
	}
	var blockedEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, blocked.stdout, &blockedEnvelope)
	if blockedEnvelope.Success || blockedEnvelope.Error == nil || blockedEnvelope.Error.Code != "BLOCKED" ||
		!strings.Contains(blockedEnvelope.Error.Message, blockerID) {
		t.Fatalf("remote blocked envelope = %+v", blockedEnvelope)
	}
	fixture.assertBead(t, beadID, "open", "")
	fixture.assertLedgerHasNoAssignment(t, beadID)
	fixture.assertMarkerCounts(t, expectedPrompt, map[int]int{0: 0, 1: 0})
	fixture.assertBeadAbsentAt(t, callerRepo, beadID)

	fixture.mustBR(t, "close", blockerID, "--reason=remote-repository-e2e", "--json")
	assigned := fixture.runNTMInDir(t, callerRepo, nil, args...)
	if assigned.exitCode != 0 || len(bytes.TrimSpace(assigned.stderr)) != 0 {
		t.Fatalf("remote default-template assignment exit=%d stdout=%s stderr=%s", assigned.exitCode, assigned.stdout, assigned.stderr)
	}
	var assignedEnvelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, assigned.stdout, &assignedEnvelope)
	if !assignedEnvelope.Success || assignedEnvelope.Data == nil || assignedEnvelope.Data.Assignment.BeadID != beadID ||
		assignedEnvelope.Data.Assignment.BeadTitle != title || assignedEnvelope.Data.Assignment.Prompt != expectedPrompt ||
		!assignedEnvelope.Data.Assignment.PromptSent {
		t.Fatalf("remote default-template envelope = %+v", assignedEnvelope)
	}
	fixture.waitForMarkerCount(t, 0, expectedPrompt, 1)
	record := fixture.readLedgerAssignment(t, beadID)
	if record.BeadTitle != title || record.PromptSent != expectedPrompt || record.DispatchState != "sent" {
		t.Fatalf("remote default-template durable assignment = %+v", record)
	}
	fixture.assertBead(t, beadID, "in_progress", record.ClaimActor)
	fixture.assertBeadAbsentAt(t, callerRepo, beadID)
}

func TestE2EAtomicAssignmentClaimedPendingRetryBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	prompt := fmt.Sprintf("NTM_ATOMIC_PENDING_RETRY_%d", time.Now().UnixNano())
	originalPaneID := fixture.panes[0].ID
	beadID, pending := fixture.createClaimedPendingViaTmuxOutage(t, "same-pane-id", "Claimed pending built process retry", prompt)

	fixture.startAgentPanes(t)
	if fixture.panes[0].ID != originalPaneID {
		t.Fatalf("private tmux restart changed recovery pane ID: before=%s after=%s", originalPaneID, fixture.panes[0].ID)
	}
	fixture.primePaneForSafeDispatch(t, 0)
	time.Sleep(5500 * time.Millisecond)
	retry := fixture.runNTM(t, nil,
		"assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--retry="+beadID,
		"--reserve-files=false",
		"--timeout=15s",
		"--json",
	)
	if retry.exitCode != 0 || len(bytes.TrimSpace(retry.stderr)) != 0 {
		t.Fatalf("pending retry exit=%d stdout=%s stderr=%s", retry.exitCode, retry.stdout, retry.stderr)
	}
	var retryEnvelope atomicAssignmentRetryEnvelope
	decodeAtomicAssignmentJSON(t, retry.stdout, &retryEnvelope)
	if !retryEnvelope.Success || retryEnvelope.Command != "assign" || retryEnvelope.Subcommand != "retry" ||
		retryEnvelope.Session != fixture.session || retryEnvelope.Error != nil || retryEnvelope.Data == nil ||
		retryEnvelope.Data.Summary.TotalFailed != 1 || retryEnvelope.Data.Summary.RetriedCount != 1 ||
		retryEnvelope.Data.Summary.SkippedCount != 0 || len(retryEnvelope.Data.Retried) != 1 ||
		len(retryEnvelope.Data.Skipped) != 0 {
		t.Fatalf("pending retry envelope = %+v", retryEnvelope)
	}
	retried := retryEnvelope.Data.Retried[0]
	if retried.BeadID != beadID || retried.Pane != 0 || retried.Status != "assigned" || !retried.PromptSent || retried.RetryCount != 1 {
		t.Fatalf("pending retry item = %+v", retried)
	}
	fixture.waitForMarkerCount(t, 0, prompt, 1)
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 1, 1: 0})
	recovered := fixture.readLedgerAssignment(t, beadID)
	if recovered.IdempotencyKey != pending.IdempotencyKey || recovered.ClaimActor != pending.ClaimActor ||
		recovered.ClaimAttempts != 1 || recovered.DispatchState != "sent" || recovered.DispatchAttempts != 2 ||
		recovered.PendingPrompt != "" || recovered.PromptSent != prompt || recovered.DispatchReceiptID == "" ||
		recovered.DispatchedAt == nil || recovered.RetryCount != 1 {
		t.Fatalf("pending retry did not preserve atomic identity: pending=%+v recovered=%+v", pending, recovered)
	}
	fixture.assertBead(t, beadID, "in_progress", pending.ClaimActor)
}

func TestE2EAtomicAssignmentPendingRetryRefusesChangedPhysicalPane(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	prompt := fmt.Sprintf("NTM_ATOMIC_PENDING_TOPOLOGY_%d", time.Now().UnixNano())
	originalPaneID := fixture.panes[0].ID
	beadID, pending := fixture.createClaimedPendingViaTmuxOutage(t, "changed-pane-id", "Pending retry topology change", prompt)

	// Start a throwaway pane first in the restarted private tmux server. This
	// consumes the former physical pane ID while leaving the production session
	// with the same window-local pane indexes.
	dummySession := fixture.session + "-pane-id-shift"
	agentCommand := "bash --noprofile --norc -c 'stty -echo; exec cat'"
	fixture.mustTMUX(t, "-f", fixture.tmuxConfig, "new-session", "-d", "-s", dummySession, "-x", "80", "-y", "24", "-c", fixture.projectDir, agentCommand)
	fixture.startAgentPanes(t)
	if fixture.panes[0].ID == originalPaneID {
		t.Fatalf("topology-change fixture reused original pane ID %s", originalPaneID)
	}
	if fixture.panes[0].Index != pending.Pane {
		t.Fatalf("topology-change fixture did not preserve local pane index: pending=%d current=%d", pending.Pane, fixture.panes[0].Index)
	}
	fixture.primePaneForSafeDispatch(t, 0)

	retry := fixture.runNTM(t, nil,
		"assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--retry="+beadID,
		"--reserve-files=false",
		"--timeout=15s",
		"--json",
	)
	if retry.exitCode != 1 || len(bytes.TrimSpace(retry.stderr)) != 0 {
		t.Fatalf("changed-pane retry exit=%d stdout=%s stderr=%s", retry.exitCode, retry.stdout, retry.stderr)
	}
	var envelope atomicAssignmentRetryEnvelope
	decodeAtomicAssignmentJSON(t, retry.stdout, &envelope)
	if envelope.Success || envelope.Command != "assign" || envelope.Subcommand != "retry" || envelope.Session != fixture.session ||
		envelope.Error == nil || envelope.Error.Code != "RETRY_SKIPPED" || envelope.Data == nil || envelope.Data.Summary.TotalFailed != 1 ||
		envelope.Data.Summary.RetriedCount != 0 || envelope.Data.Summary.SkippedCount != 1 || len(envelope.Data.Retried) != 0 ||
		len(envelope.Data.Skipped) != 1 || envelope.Data.Skipped[0].BeadID != beadID ||
		!strings.Contains(envelope.Data.Skipped[0].Reason, "original physical pane "+originalPaneID+" is unavailable") {
		t.Fatalf("changed-pane retry envelope = %+v", envelope)
	}
	fixture.assertMarkerCounts(t, prompt, map[int]int{0: 0, 1: 0})
	after := fixture.readLedgerAssignment(t, beadID)
	if after.IdempotencyKey != pending.IdempotencyKey || after.ClaimActor != pending.ClaimActor ||
		after.ClaimAttempts != pending.ClaimAttempts || after.DispatchState != "pending" ||
		after.DispatchAttempts != pending.DispatchAttempts || after.PendingPrompt != pending.PendingPrompt ||
		after.DispatchReceiptID != "" || after.DispatchedAt != nil || after.RetryCount != pending.RetryCount {
		t.Fatalf("changed-pane retry mutated pending atomic identity: before=%+v after=%+v", pending, after)
	}
	fixture.assertBead(t, beadID, "in_progress", pending.ClaimActor)
}

func TestE2EAtomicAssignmentRetryTargetValidationBuiltProcess(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("pending_recovery_rejects_conflicting_explicit_target", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		prompt := fmt.Sprintf("NTM_ATOMIC_PENDING_EXPLICIT_%d", time.Now().UnixNano())
		originalPaneID := fixture.panes[0].ID
		beadID, pending := fixture.createClaimedPendingViaTmuxOutage(t, "explicit-target", "Pending explicit target validation", prompt)
		fixture.startAgentPanes(t)
		if fixture.panes[0].ID != originalPaneID {
			t.Fatalf("private tmux restart changed recovery pane ID: before=%s after=%s", originalPaneID, fixture.panes[0].ID)
		}

		conflict := fixture.runNTM(t, nil,
			"assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--retry="+beadID,
			"--to-pane="+fixture.panes[1].ID,
			"--reserve-files=false",
			"--timeout=15s",
			"--json",
		)
		if conflict.exitCode != 1 || len(bytes.TrimSpace(conflict.stderr)) != 0 {
			t.Fatalf("conflicting pending target exit=%d stdout=%s stderr=%s", conflict.exitCode, conflict.stdout, conflict.stderr)
		}
		var conflictEnvelope atomicAssignmentRetryEnvelope
		decodeAtomicAssignmentJSON(t, conflict.stdout, &conflictEnvelope)
		if conflictEnvelope.Success || conflictEnvelope.Error == nil || conflictEnvelope.Error.Code != "RETRY_SKIPPED" ||
			conflictEnvelope.Data == nil || len(conflictEnvelope.Data.Skipped) != 1 ||
			!strings.Contains(conflictEnvelope.Data.Skipped[0].Reason, "cannot retarget") {
			t.Fatalf("conflicting pending target envelope = %+v", conflictEnvelope)
		}
		if after := fixture.readLedgerAssignment(t, beadID); !reflect.DeepEqual(after, pending) {
			t.Fatalf("conflicting pending target mutated durable identity: before=%+v after=%+v", pending, after)
		}
		fixture.assertMarkerCounts(t, prompt, map[int]int{0: 0, 1: 0})

		fixture.primePaneForSafeDispatch(t, 0)
		time.Sleep(5500 * time.Millisecond)
		accepted := fixture.runNTM(t, nil,
			"assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--retry="+beadID,
			"--to-pane="+fixture.panes[0].Target,
			"--reserve-files=false",
			"--timeout=15s",
			"--json",
		)
		if accepted.exitCode != 0 || len(bytes.TrimSpace(accepted.stderr)) != 0 {
			t.Fatalf("matching pending target exit=%d stdout=%s stderr=%s", accepted.exitCode, accepted.stdout, accepted.stderr)
		}
		var acceptedEnvelope atomicAssignmentRetryEnvelope
		decodeAtomicAssignmentJSON(t, accepted.stdout, &acceptedEnvelope)
		if !acceptedEnvelope.Success || acceptedEnvelope.Data == nil || acceptedEnvelope.Data.Summary.RetriedCount != 1 ||
			acceptedEnvelope.Data.Summary.SkippedCount != 0 {
			t.Fatalf("matching pending target envelope = %+v", acceptedEnvelope)
		}
		fixture.waitForMarkerCount(t, 0, prompt, 1)
	})

	t.Run("user_pane_rejected_by_id_and_window_pane", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		beadID := fixture.createBead(t, "Retry user-pane validation")
		seedPrompt := fmt.Sprintf("NTM_ATOMIC_RETRY_USER_SEED_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, seedPrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed user-pane retry exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForMarkerCount(t, 0, seedPrompt, 1)
		fixture.driveAssignmentStatus(t, fixture.panes[0], beadID, "ERROR: fatal retry target fixture", "failed")
		before := fixture.readLedgerAssignment(t, beadID)
		userPane := fixture.panes[1]
		fixture.mustTMUX(t, "select-pane", "-t", userPane.ID, "-T", fixture.session+"__user_2")
		retryMarker := fmt.Sprintf("NTM_ATOMIC_RETRY_USER_TARGET_%d", time.Now().UnixNano())
		templatePath := filepath.Join(fixture.projectDir, "atomic-retry-user-template.txt")
		if err := os.WriteFile(templatePath, []byte(retryMarker+"::{BEAD_ID}"), 0o600); err != nil {
			t.Fatalf("write user-pane retry template: %v", err)
		}

		for _, selector := range []string{userPane.ID, userPane.Target} {
			result := fixture.runNTM(t, nil,
				"assign", fixture.session,
				"--repo="+fixture.projectDir,
				"--retry="+beadID,
				"--to-pane="+selector,
				"--template=custom",
				"--template-file="+templatePath,
				"--reserve-files=false",
				"--timeout=15s",
				"--json",
			)
			if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
				t.Fatalf("user-pane selector %s exit=%d stdout=%s stderr=%s", selector, result.exitCode, result.stdout, result.stderr)
			}
			var envelope atomicAssignmentRetryEnvelope
			decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
			if envelope.Success || envelope.Error == nil || envelope.Error.Code != "RETRY_SKIPPED" || envelope.Data == nil ||
				len(envelope.Data.Skipped) != 1 || !strings.Contains(envelope.Data.Skipped[0].Reason, "not an agent pane (type: user)") {
				t.Fatalf("user-pane selector %s envelope = %+v", selector, envelope)
			}
			if after := fixture.readLedgerAssignment(t, beadID); !reflect.DeepEqual(after, before) {
				t.Fatalf("user-pane selector %s mutated failed assignment: before=%+v after=%+v", selector, before, after)
			}
			fixture.assertMarkerCounts(t, retryMarker, map[int]int{0: 0, 1: 0})
		}
	})
}

func TestE2EAtomicAssignmentReassignFailureContracts(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("send_failure_is_nonzero_and_pending_replacement_recovers", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		beadID := fixture.createBead(t, "Reassign durable send failure")
		sourcePrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_FAILURE_SOURCE_%d", time.Now().UnixNano())
		targetPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_FAILURE_TARGET_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, sourcePrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed send-failure reassign exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForMarkerCount(t, 0, sourcePrompt, 1)
		fixture.driveAssignmentStatus(t, fixture.panes[0], beadID, "• Working (4s · esc to interrupt)", "working")
		before := fixture.readLedgerAssignment(t, beadID)
		originalPaneIDs := map[int]string{0: fixture.panes[0].ID, 1: fixture.panes[1].ID}
		path, fired := fixture.armGuardedClaimInfoThenKillTmux(t, "reassign-send-failure")

		failed := fixture.runNTM(t, map[string]string{"PATH": path},
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--reassign="+beadID,
			"--to-pane="+fixture.panes[1].ID,
			"--prompt="+targetPrompt,
			"--force",
			"--reserve-files=false",
			"--timeout=15s",
		)
		if failed.exitCode != 1 || len(bytes.TrimSpace(failed.stderr)) != 0 {
			t.Fatalf("send-failure reassign exit=%d stdout=%s stderr=%s", failed.exitCode, failed.stdout, failed.stderr)
		}
		var failedEnvelope atomicAssignmentReassignEnvelope
		decodeAtomicAssignmentJSON(t, failed.stdout, &failedEnvelope)
		if failedEnvelope.Success || failedEnvelope.Error == nil || failedEnvelope.Error.Code != "SEND_ERROR" ||
			failedEnvelope.Error.Details["dispatch_state"] != "pending" {
			t.Fatalf("send-failure reassign envelope = %+v", failedEnvelope)
		}
		if _, err := os.Stat(fired); err != nil {
			t.Fatalf("reassign guarded-info outage did not fire: %v", err)
		}
		pending := fixture.readLedgerAssignment(t, beadID)
		if pending.Status != "claimed" || pending.ClaimState != "claimed" || pending.ClaimActor != before.ClaimActor ||
			pending.IdempotencyKey == before.IdempotencyKey || pending.OccupancyKey != originalPaneIDs[1] ||
			pending.DispatchTarget != originalPaneIDs[1] || pending.DispatchState != "pending" || pending.DispatchAttempts != 1 ||
			pending.PendingPrompt != targetPrompt || pending.PromptSent != "" || pending.LastDispatchError == "" ||
			pending.DispatchedAt != nil || pending.DispatchReceiptID != "" || pending.ClearState != "" {
			t.Fatalf("send-failure replacement is not durably retryable: before=%+v pending=%+v", before, pending)
		}
		fixture.assertBead(t, beadID, "in_progress", before.ClaimActor)

		fixture.startAgentPanes(t)
		for pane, id := range originalPaneIDs {
			if fixture.panes[pane].ID != id {
				t.Fatalf("private tmux restart changed pane %d ID: before=%s after=%s", pane, id, fixture.panes[pane].ID)
			}
		}
		fixture.assertMarkerCounts(t, targetPrompt, map[int]int{0: 0, 1: 0})
		fixture.primePaneForSafeDispatch(t, 1)
		time.Sleep(5500 * time.Millisecond)
		recovered := fixture.runNTM(t, nil,
			"assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--retry="+beadID,
			"--to-pane="+fixture.panes[1].Target,
			"--reserve-files=false",
			"--timeout=15s",
			"--json",
		)
		if recovered.exitCode != 0 || len(bytes.TrimSpace(recovered.stderr)) != 0 {
			t.Fatalf("recover send-failure replacement exit=%d stdout=%s stderr=%s", recovered.exitCode, recovered.stdout, recovered.stderr)
		}
		fixture.waitForMarkerCount(t, 1, targetPrompt, 1)
		after := fixture.readLedgerAssignment(t, beadID)
		if after.IdempotencyKey != pending.IdempotencyKey || after.ClaimActor != pending.ClaimActor ||
			after.DispatchState != "sent" || after.DispatchAttempts != 2 || after.DispatchReceiptID == "" || after.RetryCount != 1 {
			t.Fatalf("recovered replacement changed atomic identity: pending=%+v after=%+v", pending, after)
		}
	})

	t.Run("redaction_block_does_not_send_or_persist_secret", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		beadID := fixture.createBead(t, "Reassign redaction block")
		sourcePrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_REDACT_SOURCE_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, sourcePrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed redaction reassign exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForMarkerCount(t, 0, sourcePrompt, 1)
		fixture.driveAssignmentStatus(t, fixture.panes[0], beadID, "• Working (4s · esc to interrupt)", "working")
		before := fixture.readLedgerAssignment(t, beadID)
		secret := "NTM_E2E_REASSIGN_SECRET_987654321"
		blockedPrompt := "Use password=" + secret + " while reassigning"

		blocked := fixture.runNTM(t, nil,
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--reassign="+beadID,
			"--to-pane="+fixture.panes[1].ID,
			"--prompt="+blockedPrompt,
			"--force",
			"--reserve-files=false",
			"--redact=block",
			"--timeout=15s",
		)
		if blocked.exitCode != 1 || len(bytes.TrimSpace(blocked.stderr)) != 0 {
			t.Fatalf("redaction-blocked reassign exit=%d stdout=%s stderr=%s", blocked.exitCode, blocked.stdout, blocked.stderr)
		}
		fixture.assertSecretAbsent(t, secret, blocked.stdout, blocked.stderr)
		var blockedEnvelope atomicAssignmentReassignEnvelope
		decodeAtomicAssignmentJSON(t, blocked.stdout, &blockedEnvelope)
		if blockedEnvelope.Success || blockedEnvelope.Error == nil || blockedEnvelope.Error.Code != "REDACTION_BLOCKED" {
			t.Fatalf("redaction-blocked reassign envelope = %+v", blockedEnvelope)
		}
		fixture.assertMarkerCounts(t, secret, map[int]int{0: 0, 1: 0})
		fixture.assertAssignmentArtifactsExclude(t, secret)
		barrier := fixture.readLedgerAssignment(t, beadID)
		if barrier.Status != "working" || barrier.ClearState != "leases_released" || barrier.ClaimActor != before.ClaimActor ||
			barrier.IdempotencyKey != before.IdempotencyKey || barrier.DispatchTarget != before.DispatchTarget ||
			barrier.DispatchReceiptID != before.DispatchReceiptID || barrier.DispatchAttempts != before.DispatchAttempts {
			t.Fatalf("redaction refusal did not retain a retryable source barrier: before=%+v after=%+v", before, barrier)
		}

		cleanPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_REDACT_CLEAN_%d", time.Now().UnixNano())
		clean := fixture.runNTM(t, nil,
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--reassign="+beadID,
			"--to-pane="+fixture.panes[1].ID,
			"--prompt="+cleanPrompt,
			"--force",
			"--reserve-files=false",
			"--timeout=15s",
		)
		if clean.exitCode != 0 || len(bytes.TrimSpace(clean.stderr)) != 0 {
			t.Fatalf("clean reassign after redaction block exit=%d stdout=%s stderr=%s", clean.exitCode, clean.stdout, clean.stderr)
		}
		fixture.waitForMarkerCount(t, 1, cleanPrompt, 1)
		after := fixture.readLedgerAssignment(t, beadID)
		if after.ClaimActor != before.ClaimActor || after.IdempotencyKey == before.IdempotencyKey || after.DispatchState != "sent" {
			t.Fatalf("clean reassign after redaction block lost atomic identity: before=%+v after=%+v", before, after)
		}
	})

	t.Run("force_refuses_durably_occupied_target", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		sourceBeadID := fixture.createBead(t, "Reassign occupied source")
		targetBeadID := fixture.createBead(t, "Reassign occupied target")
		sourcePrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_OCCUPIED_SOURCE_%d", time.Now().UnixNano())
		targetPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_OCCUPIED_TARGET_%d", time.Now().UnixNano())
		for _, seed := range []struct {
			pane   int
			beadID string
			prompt string
		}{
			{pane: 0, beadID: sourceBeadID, prompt: sourcePrompt},
			{pane: 1, beadID: targetBeadID, prompt: targetPrompt},
		} {
			result := fixture.runNTM(t, nil, atomicDirectArgsForPane(fixture, seed.pane, seed.beadID, seed.prompt, false)...)
			if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
				t.Fatalf("seed occupied pane %d exit=%d stdout=%s stderr=%s", seed.pane, result.exitCode, result.stdout, result.stderr)
			}
			fixture.waitForMarkerCount(t, seed.pane, seed.prompt, 1)
		}
		fixture.driveAssignmentStatus(t, fixture.panes[0], sourceBeadID, "• Working (4s · esc to interrupt)", "working")
		sourceBefore := fixture.readLedgerAssignment(t, sourceBeadID)
		targetBefore := fixture.readLedgerAssignment(t, targetBeadID)
		candidatePrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_OCCUPIED_CANDIDATE_%d", time.Now().UnixNano())

		result := fixture.runNTM(t, nil,
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--reassign="+sourceBeadID,
			"--to-pane="+fixture.panes[1].ID,
			"--prompt="+candidatePrompt,
			"--force",
			"--reserve-files=false",
			"--timeout=15s",
		)
		if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("force occupied reassign exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope atomicAssignmentReassignEnvelope
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if envelope.Success || envelope.Error == nil || envelope.Error.Code != "TARGET_BUSY" ||
			!strings.Contains(envelope.Error.Message, targetBeadID) {
			t.Fatalf("force occupied reassign envelope = %+v", envelope)
		}
		fixture.assertMarkerCounts(t, candidatePrompt, map[int]int{0: 0, 1: 0})
		if sourceAfter := fixture.readLedgerAssignment(t, sourceBeadID); !reflect.DeepEqual(sourceAfter, sourceBefore) {
			t.Fatalf("force occupied reassign mutated source: before=%+v after=%+v", sourceBefore, sourceAfter)
		}
		if targetAfter := fixture.readLedgerAssignment(t, targetBeadID); !reflect.DeepEqual(targetAfter, targetBefore) {
			t.Fatalf("force occupied reassign mutated target: before=%+v after=%+v", targetBefore, targetAfter)
		}
		fixture.assertBead(t, sourceBeadID, "in_progress", sourceBefore.ClaimActor)
		fixture.assertBead(t, targetBeadID, "in_progress", targetBefore.ClaimActor)
	})
}

func TestE2EAtomicAssignmentReassignTransfersReservation(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	const title = "Transfer reservation internal/cli/reassign_transfer.go"
	reservedPaths := []string{"internal/cli/reassign_transfer.go", "internal/cli/reassign_transfer/**/*"}
	beadID := fixture.createBead(t, title)
	source := fixture.panes[0]
	target := fixture.panes[1]

	var reservationMu sync.Mutex
	var grantCalls int
	var releaseCalls int
	var releasedIDs []int
	var grantedAgents []string
	var expectedReleaseAgent string
	var activeReservations []map[string]any
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health/liveness" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		var request struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
			Params  struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeResult := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		}
		if request.Method == "resources/read" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": request.ID,
				"error": map[string]any{"code": -32601, "message": "resource view not supported"},
			})
			return
		}
		if request.Method != "tools/call" {
			http.Error(w, "expected JSON-RPC tools/call", http.StatusBadRequest)
			return
		}
		switch request.Params.Name {
		case "health_check":
			writeResult(map[string]any{"status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339Nano)})
		case "file_reservation_paths":
			paths, pathsOK := anyStringSlice(request.Params.Arguments["paths"])
			agentName, _ := request.Params.Arguments["agent_name"].(string)
			reason, _ := request.Params.Arguments["reason"].(string)
			if request.Params.Arguments["project_key"] != fixture.projectDir || !pathsOK ||
				!reflect.DeepEqual(paths, reservedPaths) || agentName == "" || !strings.Contains(reason, beadID) {
				http.Error(w, fmt.Sprintf("unexpected reassign reservation args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			reservationMu.Lock()
			grantCalls++
			grantedAgents = append(grantedAgents, agentName)
			baseID := 9600 + grantCalls*10
			now := time.Now().UTC()
			granted := make([]map[string]any, 0, len(paths))
			for index, path := range paths {
				granted = append(granted, map[string]any{
					"id": baseID + index, "path_pattern": path, "agent_name": agentName,
					"project_id": 1, "exclusive": true, "reason": reason,
					"created_ts": now.Format(time.RFC3339Nano), "expires_ts": now.Add(time.Hour).Format(time.RFC3339Nano),
				})
			}
			activeReservations = append([]map[string]any(nil), granted...)
			reservationMu.Unlock()
			writeResult(map[string]any{"granted": granted, "conflicts": []any{}})
		case "list_file_reservations":
			reservationMu.Lock()
			active := append([]map[string]any(nil), activeReservations...)
			reservationMu.Unlock()
			writeResult(active)
		case "release_file_reservations":
			ids, idsOK := atomicAssignmentAnyIntSlice(request.Params.Arguments["file_reservation_ids"])
			agentName, _ := request.Params.Arguments["agent_name"].(string)
			reservationMu.Lock()
			wantAgent := expectedReleaseAgent
			reservationMu.Unlock()
			if request.Params.Arguments["project_key"] != fixture.projectDir || !idsOK || len(ids) != len(reservedPaths) ||
				wantAgent == "" || agentName != wantAgent {
				http.Error(w, fmt.Sprintf("unexpected reassign release args: %#v", request.Params.Arguments), http.StatusBadRequest)
				return
			}
			reservationMu.Lock()
			releaseCalls++
			releasedIDs = append(releasedIDs, ids...)
			activeReservations = nil
			reservationMu.Unlock()
			writeResult(map[string]any{"released": len(ids)})
		default:
			http.Error(w, "unexpected Agent Mail tool: "+request.Params.Name, http.StatusNotFound)
		}
	}))
	defer mailServer.Close()
	env := map[string]string{"AGENT_MAIL_URL": mailServer.URL + "/"}

	initialPrompt := fmt.Sprintf("Work on bead %s: %s. Check dependencies first with `br dep tree %s`.", beadID, title, beadID)
	seed := fixture.runNTM(t, env,
		"assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--pane="+source.ID,
		"--beads="+beadID,
		"--template=impl",
		"--force",
		"--ignore-deps",
		"--timeout=15s",
		"--json",
	)
	if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
		t.Fatalf("seed reserved reassign exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
	}
	fixture.waitForMarkerCount(t, 0, initialPrompt, 1)
	fixture.driveAssignmentStatus(t, source, beadID, "• Working (4s · esc to interrupt)", "working")
	before := fixture.readLedgerAssignment(t, beadID)
	if !before.ReservationRequired || !before.ReservationDiscovery || !before.ReservationCompleted || before.ReservationState != "reserved" ||
		!reflect.DeepEqual(before.ReservedPaths, reservedPaths) || len(before.ReservationIDs) != len(reservedPaths) || before.ReservationExpiresAt == nil {
		t.Fatalf("seed reservation metadata = %+v", before)
	}
	reservationMu.Lock()
	expectedReleaseAgent = before.AgentName
	reservationMu.Unlock()
	reassignedPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_RESERVED_TARGET_%d", time.Now().UnixNano())
	result := fixture.runNTM(t, env,
		"--json", "assign", fixture.session,
		"--repo="+fixture.projectDir,
		"--reassign="+beadID,
		"--to-pane="+target.ID,
		"--prompt="+reassignedPrompt,
		"--force",
		"--reserve-files=true",
		"--timeout=15s",
	)
	if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("reserved reassign exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	var envelope atomicAssignmentReassignEnvelope
	decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
	if !envelope.Success || envelope.Error != nil || envelope.Data == nil || envelope.Data.BeadID != beadID ||
		envelope.Data.AgentName == "" || envelope.Data.AgentName == before.AgentName || !envelope.Data.PromptSent ||
		!envelope.Data.FileReservationsTransferred || envelope.Data.FileReservationsReleasedFrom != 1 ||
		envelope.Data.FileReservationsCreatedFor != len(reservedPaths) {
		if envelope.Data == nil {
			t.Fatalf("reserved reassign envelope = %+v", envelope)
		}
		t.Fatalf("reserved reassign envelope data = %+v error=%+v", *envelope.Data, envelope.Error)
	}
	fixture.waitForMarkerCount(t, 1, reassignedPrompt, 1)
	after := fixture.readLedgerAssignment(t, beadID)
	if after.ClaimActor != before.ClaimActor || after.IdempotencyKey == before.IdempotencyKey || after.AgentName != envelope.Data.AgentName ||
		after.OccupancyKey != target.ID || after.DispatchTarget != target.ID || after.DispatchState != "sent" ||
		!after.ReservationRequired || !after.ReservationDiscovery || !after.ReservationCompleted || after.ReservationState != "reserved" ||
		!reflect.DeepEqual(after.ReservedPaths, reservedPaths) || len(after.ReservationIDs) != len(reservedPaths) ||
		reflect.DeepEqual(after.ReservationIDs, before.ReservationIDs) || after.ReservationExpiresAt == nil {
		t.Fatalf("reserved reassign durable transfer: before=%+v after=%+v", before, after)
	}
	reservationMu.Lock()
	gotGrantCalls := grantCalls
	gotReleaseCalls := releaseCalls
	gotReleasedIDs := append([]int(nil), releasedIDs...)
	gotGrantedAgents := append([]string(nil), grantedAgents...)
	active := append([]map[string]any(nil), activeReservations...)
	reservationMu.Unlock()
	if gotGrantCalls != 2 || gotReleaseCalls != 1 || !reflect.DeepEqual(gotReleasedIDs, before.ReservationIDs) ||
		!reflect.DeepEqual(gotGrantedAgents, []string{before.AgentName, after.AgentName}) || len(active) != len(reservedPaths) {
		t.Fatalf("reservation transfer calls grants=%d releases=%d released=%v agents=%v active=%v", gotGrantCalls, gotReleaseCalls, gotReleasedIDs, gotGrantedAgents, active)
	}
	fixture.assertBead(t, beadID, "in_progress", before.ClaimActor)
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

	t.Run("legacy_local_index_occupancy_blocks_duplicate_window_index", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		panes := fixture.addSecondAgentWindow(t)
		legacyBeadID := fixture.createBead(t, "Legacy duplicate-index occupancy")
		candidateBeadID := fixture.createBead(t, "Candidate duplicate-index occupancy")
		source := panes["0.0"]
		target := panes["1.0"]
		legacyPrompt := fmt.Sprintf("NTM_ATOMIC_LEGACY_OCCUPANCY_%d", time.Now().UnixNano())
		candidatePrompt := fmt.Sprintf("NTM_ATOMIC_LEGACY_CANDIDATE_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, source.ID, legacyBeadID, legacyPrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed legacy occupancy exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForEndpointMarkerCount(t, source, legacyPrompt, 1)
		before := fixture.readLedgerAssignment(t, legacyBeadID)
		fixture.setLedgerAssignmentLegacyPaneTarget(t, legacyBeadID)

		result := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, target.ID, candidateBeadID, candidatePrompt, false)...)
		if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("legacy duplicate-index occupancy exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope atomicAssignmentDirectEnvelope
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if envelope.Success || envelope.Error == nil || envelope.Error.Code != "TARGET_BUSY" ||
			!strings.Contains(envelope.Error.Message, "target already has active work") ||
			!strings.Contains(envelope.Error.Message, legacyBeadID) || envelope.Data == nil ||
			envelope.Data.Assignment.BeadID != candidateBeadID || envelope.Data.Assignment.Prompt != "" ||
			envelope.Data.Assignment.PromptSent || envelope.Data.Receipt != nil {
			t.Fatalf("legacy duplicate-index occupancy envelope = %+v", envelope)
		}
		fixture.assertBead(t, candidateBeadID, "open", "")
		fixture.assertLedgerHasNoAssignment(t, candidateBeadID)
		fixture.assertEndpointMarkerCounts(t, candidatePrompt, panes, nil)
		legacy := fixture.readLedgerAssignment(t, legacyBeadID)
		if legacy.OccupancyKey != "" || legacy.DispatchTarget != "" || legacy.Pane != target.Index || source.ID == target.ID {
			t.Fatalf("fixture did not preserve conservative legacy duplicate index: source=%+v target=%+v record=%+v", source, target, legacy)
		}
		if legacy.IdempotencyKey != before.IdempotencyKey || legacy.DispatchReceiptID != before.DispatchReceiptID || legacy.DispatchAttempts != before.DispatchAttempts {
			t.Fatalf("legacy occupancy refusal mutated owner receipt: before=%+v after=%+v", before, legacy)
		}
	})

	t.Run("clear_pane_targets_one_physical_duplicate_index", func(t *testing.T) {
		retainedBeadID := fixture.createBead(t, "Retained duplicate-index assignment")
		clearedBeadID := fixture.createBead(t, "Cleared duplicate-index assignment")
		retainedPane := panes["0.0"]
		clearedPane := panes["1.0"]
		retainedPrompt := fmt.Sprintf("NTM_ATOMIC_CLEAR_RETAIN_%d", time.Now().UnixNano())
		clearedPrompt := fmt.Sprintf("NTM_ATOMIC_CLEAR_TARGET_%d", time.Now().UnixNano())
		for _, assignment := range []struct {
			pane   atomicAssignmentPane
			beadID string
			prompt string
		}{
			{pane: retainedPane, beadID: retainedBeadID, prompt: retainedPrompt},
			{pane: clearedPane, beadID: clearedBeadID, prompt: clearedPrompt},
		} {
			result := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, assignment.pane.Target, assignment.beadID, assignment.prompt, false)...)
			if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
				t.Fatalf("seed %s assignment exit=%d stdout=%s stderr=%s", assignment.pane.Target, result.exitCode, result.stdout, result.stderr)
			}
			fixture.waitForEndpointMarkerCount(t, assignment.pane, assignment.prompt, 1)
		}
		retainedBefore := fixture.readLedgerAssignment(t, retainedBeadID)
		clearedBefore := fixture.readLedgerAssignment(t, clearedBeadID)

		result := fixture.runNTM(t, nil,
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--clear-pane="+clearedPane.ID,
			"--timeout=15s",
		)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("canonical clear-pane exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope struct {
			Success bool `json:"success"`
			Data    struct {
				Cleared []struct {
					BeadID  string `json:"bead_id"`
					Success bool   `json:"success"`
				} `json:"cleared"`
				Summary struct {
					ClearedCount int `json:"cleared_count"`
					FailedCount  int `json:"failed_count"`
				} `json:"summary"`
			} `json:"data"`
		}
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if !envelope.Success || envelope.Data.Summary.ClearedCount != 1 || envelope.Data.Summary.FailedCount != 0 ||
			len(envelope.Data.Cleared) != 1 || envelope.Data.Cleared[0].BeadID != clearedBeadID || !envelope.Data.Cleared[0].Success {
			t.Fatalf("canonical clear-pane envelope = %+v", envelope)
		}
		fixture.assertLedgerHasNoAssignment(t, clearedBeadID)
		assertAtomicAssignmentReceiptUnchanged(t, retainedBefore, fixture.readLedgerAssignment(t, retainedBeadID))
		fixture.assertEndpointMarkerCounts(t, retainedPrompt, panes, map[string]int{retainedPane.Target: 1})
		fixture.assertEndpointMarkerCounts(t, clearedPrompt, panes, map[string]int{clearedPane.Target: 1})
		if clearedBefore.OccupancyKey == retainedBefore.OccupancyKey || clearedBefore.Pane != retainedBefore.Pane {
			t.Fatalf("fixture did not exercise duplicate local pane indexes: retained=%+v cleared=%+v", retainedBefore, clearedBefore)
		}
	})

	t.Run("reassign_pane_id_targets_one_physical_duplicate_index", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		panes := fixture.addSecondAgentWindow(t)
		beadID := fixture.createBead(t, "Canonical physical reassignment")
		source := panes["0.0"]
		target := panes["1.0"]
		originalPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_SOURCE_%d", time.Now().UnixNano())
		reassignedPrompt := fmt.Sprintf("NTM_ATOMIC_REASSIGN_TARGET_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, source.ID, beadID, originalPrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed reassignment exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForEndpointMarkerCount(t, source, originalPrompt, 1)
		fixture.driveAssignmentStatus(t, source, beadID, "• Working (4s · esc to interrupt)", "working")
		before := fixture.readLedgerAssignment(t, beadID)
		fixture.primeEndpointForSafeDispatch(t, target)

		result := fixture.runNTM(t, nil,
			"--json", "assign", fixture.session, "--repo="+fixture.projectDir,
			"--reassign="+beadID, "--to-pane="+target.ID,
			"--prompt="+reassignedPrompt, "--force", "--reserve-files=false", "--timeout=15s",
		)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("canonical reassign exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope struct {
			Success bool `json:"success"`
			Data    struct {
				BeadID       string `json:"bead_id"`
				Pane         int    `json:"pane"`
				PreviousPane int    `json:"previous_pane"`
				PromptSent   bool   `json:"prompt_sent"`
			} `json:"data"`
		}
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if !envelope.Success || envelope.Data.BeadID != beadID || envelope.Data.Pane != target.Index ||
			envelope.Data.PreviousPane != source.Index || !envelope.Data.PromptSent {
			t.Fatalf("canonical reassign envelope = %+v", envelope)
		}
		fixture.waitForEndpointMarkerCount(t, target, reassignedPrompt, 1)
		fixture.assertEndpointMarkerCounts(t, reassignedPrompt, panes, map[string]int{target.Target: 1})
		reassigned := fixture.readLedgerAssignment(t, beadID)
		if reassigned.OccupancyKey != target.ID || reassigned.DispatchTarget != target.ID || reassigned.Pane != target.Index || reassigned.Status != "assigned" {
			t.Fatalf("canonical reassign durable target = %+v", reassigned)
		}
		if reassigned.ClaimActor != before.ClaimActor || reassigned.IdempotencyKey == before.IdempotencyKey ||
			reassigned.ClaimState != "claimed" || reassigned.ClaimStatus != "in_progress" || reassigned.ClaimAttempts != 1 ||
			reassigned.ReservationRequired || reassigned.ReservationCompleted || len(reassigned.ReservationIDs) != 0 || len(reassigned.ReservedPaths) != 0 {
			t.Fatalf("canonical reassign did not preserve claim actor and disabled reservation contract: before=%+v after=%+v", before, reassigned)
		}
		fixture.assertBead(t, beadID, "in_progress", before.ClaimActor)
		if source.Index != target.Index || source.ID == target.ID {
			t.Fatalf("fixture did not exercise duplicate local indexes: source=%+v target=%+v", source, target)
		}
	})

	t.Run("retry_window_pane_targets_one_physical_duplicate_index", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		panes := fixture.addSecondAgentWindow(t)
		beadID := fixture.createBead(t, "Canonical physical retry")
		source := panes["0.1"]
		target := panes["1.1"]
		originalPrompt := fmt.Sprintf("NTM_ATOMIC_RETRY_SOURCE_%d", time.Now().UnixNano())
		retryMarker := fmt.Sprintf("NTM_ATOMIC_RETRY_TARGET_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgsForSelector(fixture, source.ID, beadID, originalPrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed retry exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForEndpointMarkerCount(t, source, originalPrompt, 1)
		before := fixture.readLedgerAssignment(t, beadID)
		fixture.driveAssignmentStatus(t, source, beadID, "ERROR: fatal assignment failure", "failed")
		fixture.primeEndpointForSafeDispatch(t, target)
		time.Sleep(5500 * time.Millisecond)
		templatePath := filepath.Join(fixture.projectDir, "atomic-canonical-retry-template.txt")
		if err := os.WriteFile(templatePath, []byte(retryMarker+"::{BEAD_ID}"), 0o600); err != nil {
			t.Fatalf("write canonical retry template: %v", err)
		}

		result := fixture.runNTM(t, nil,
			"assign", fixture.session, "--repo="+fixture.projectDir,
			"--retry="+beadID, "--to-pane="+target.Target,
			"--template=custom", "--template-file="+templatePath,
			"--reserve-files=false", "--timeout=15s", "--json",
		)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("canonical retry exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var envelope atomicAssignmentRetryEnvelope
		decodeAtomicAssignmentJSON(t, result.stdout, &envelope)
		if !envelope.Success || envelope.Data == nil || envelope.Data.Summary.RetriedCount != 1 ||
			envelope.Data.Summary.SkippedCount != 0 || len(envelope.Data.Retried) != 1 || envelope.Data.Retried[0].BeadID != beadID {
			t.Fatalf("canonical retry envelope = %+v", envelope)
		}
		prompt := retryMarker + "::" + beadID
		fixture.waitForEndpointMarkerCount(t, target, prompt, 1)
		fixture.assertEndpointMarkerCounts(t, prompt, panes, map[string]int{target.Target: 1})
		retried := fixture.readLedgerAssignment(t, beadID)
		if retried.OccupancyKey != target.ID || retried.DispatchTarget != target.ID || retried.Pane != target.Index ||
			retried.Status != "assigned" || retried.IdempotencyKey == before.IdempotencyKey || retried.DispatchReceiptID == "" {
			t.Fatalf("canonical retry durable target = %+v before=%+v", retried, before)
		}
		if source.Index != target.Index || source.ID == target.ID {
			t.Fatalf("fixture did not exercise duplicate local indexes: source=%+v target=%+v", source, target)
		}
	})

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

func TestE2EAtomicAssignmentHumanOutputRedaction(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("direct_prints_only_durable_redacted_prompt", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		beadID := fixture.createBead(t, "Human direct redaction")
		secret := "sk-proj-NTM_HUMAN_DIRECT_1234567890123456789012345678901234567890"
		rawPrompt := "Use OPENAI_API_KEY=" + secret + " for the direct assignment"
		templatePath := filepath.Join(fixture.projectDir, "atomic-human-direct-redaction-template.txt")
		if err := os.WriteFile(templatePath, []byte(rawPrompt), 0o600); err != nil {
			t.Fatalf("write human direct redaction template: %v", err)
		}

		result := fixture.runNTM(t, nil,
			"assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--pane="+fixture.panes[0].ID,
			"--beads="+beadID,
			"--template=custom",
			"--template-file="+templatePath,
			"--force",
			"--ignore-deps",
			"--reserve-files=false",
			"--redact=redact",
			"--timeout=15s",
		)
		if result.exitCode != 0 {
			t.Fatalf("human direct redaction exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		fixture.assertSecretAbsent(t, secret, result.stdout, result.stderr)
		if !bytes.Contains(result.stdout, []byte("[REDACTED:")) {
			t.Fatalf("human direct output omitted redacted prompt: %s", result.stdout)
		}
		fixture.waitForMarkerCount(t, 0, "[REDACTED:", 1)
		fixture.assertMarkerCounts(t, secret, map[int]int{0: 0, 1: 0})
		record := fixture.readLedgerAssignment(t, beadID)
		if strings.Contains(record.PromptSent, secret) || !strings.Contains(record.PromptSent, "[REDACTED:") {
			t.Fatalf("human direct durable prompt = %q", record.PromptSent)
		}
		fixture.assertAssignmentArtifactsExclude(t, secret)
	})

	t.Run("reassign_prints_only_durable_redacted_prompt", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		beadID := fixture.createBead(t, "Human reassign redaction")
		sourcePrompt := fmt.Sprintf("NTM_ATOMIC_HUMAN_REASSIGN_SOURCE_%d", time.Now().UnixNano())
		seed := fixture.runNTM(t, nil, atomicDirectArgs(fixture, beadID, sourcePrompt, false)...)
		if seed.exitCode != 0 || len(bytes.TrimSpace(seed.stderr)) != 0 {
			t.Fatalf("seed human reassign redaction exit=%d stdout=%s stderr=%s", seed.exitCode, seed.stdout, seed.stderr)
		}
		fixture.waitForMarkerCount(t, 0, sourcePrompt, 1)
		fixture.driveAssignmentStatus(t, fixture.panes[0], beadID, "• Working (4s · esc to interrupt)", "working")
		secret := "sk-proj-NTM_HUMAN_REASSIGN_1234567890123456789012345678901234567890"
		rawPrompt := "Use OPENAI_API_KEY=" + secret + " for reassignment"
		templatePath := filepath.Join(fixture.projectDir, "atomic-human-reassign-redaction-template.txt")
		if err := os.WriteFile(templatePath, []byte(rawPrompt), 0o600); err != nil {
			t.Fatalf("write human reassign redaction template: %v", err)
		}

		result := fixture.runNTM(t, nil,
			"assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--reassign="+beadID,
			"--to-pane="+fixture.panes[1].ID,
			"--template=custom",
			"--template-file="+templatePath,
			"--force",
			"--reserve-files=false",
			"--redact=redact",
			"--timeout=15s",
		)
		if result.exitCode != 0 {
			t.Fatalf("human reassign redaction exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		fixture.assertSecretAbsent(t, secret, result.stdout, result.stderr)
		if !bytes.Contains(result.stdout, []byte("[REDACTED:")) {
			t.Fatalf("human reassign output omitted redacted prompt: %s", result.stdout)
		}
		fixture.waitForMarkerCount(t, 1, "[REDACTED:", 1)
		fixture.assertMarkerCounts(t, secret, map[int]int{0: 0, 1: 0})
		record := fixture.readLedgerAssignment(t, beadID)
		if strings.Contains(record.PromptSent, secret) || !strings.Contains(record.PromptSent, "[REDACTED:") {
			t.Fatalf("human reassign durable prompt = %q", record.PromptSent)
		}
		fixture.assertAssignmentArtifactsExclude(t, secret)
	})
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

	fixture.tmuxConfig = filepath.Join(root, "tmux.conf")
	config := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g renumber-windows off",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(fixture.tmuxConfig, []byte(config), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	t.Cleanup(func() {
		_ = fixture.runTMUX(context.Background(), "kill-server")
	})
	fixture.startAgentPanes(t)
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
		"--timeout=15s",
	}
	if !requireReservation {
		args = append(args, "--reserve-files=false")
	}
	args = append(args, "--json")
	return args
}

func atomicDirectTemplateArgs(f *atomicAssignmentCLIFixture, pane int, beadID, templatePath string) []string {
	return []string{
		"assign", f.session,
		"--repo=" + f.projectDir,
		fmt.Sprintf("--pane=%d", pane),
		"--beads=" + beadID,
		"--template=custom",
		"--template-file=" + templatePath,
		"--reserve-files=false",
		"--force",
		"--ignore-deps",
		"--timeout=15s",
		"--json",
	}
}

func (f *atomicAssignmentCLIFixture) armClaimThenKillTmux(t *testing.T, name string) (path string, fired string) {
	t.Helper()
	wrapperDir := filepath.Join(f.root, "claim-outage-bin-"+name)
	if err := os.MkdirAll(wrapperDir, 0o700); err != nil {
		t.Fatalf("create br outage wrapper directory: %v", err)
	}
	sentinel := filepath.Join(wrapperDir, "stop-private-tmux-after-claim")
	fired = sentinel + ".fired"
	if err := os.WriteFile(sentinel, []byte("armed\n"), 0o600); err != nil {
		t.Fatalf("arm br outage wrapper: %v", err)
	}
	wrapper := strings.Join([]string{
		"#!/bin/sh",
		"real_br=" + tmux.ShellQuote(f.brPath),
		"real_tmux=" + tmux.ShellQuote(f.tmuxPath),
		"sentinel=" + tmux.ShellQuote(sentinel),
		"fired=" + tmux.ShellQuote(fired),
		`"$real_br" "$@"`,
		"status=$?",
		"sync_command=0",
		"flush_only=0",
		`for arg in "$@"; do`,
		`  if [ "$arg" = "sync" ]; then sync_command=1; fi`,
		`  if [ "$arg" = "--flush-only" ]; then flush_only=1; fi`,
		"done",
		`guarded_sync=$((sync_command * flush_only))`,
		`if [ "$status" -eq 0 ] && [ "$guarded_sync" -eq 1 ] && [ -e "$sentinel" ] && [ ! -e "$fired" ]; then`,
		`  mv "$sentinel" "$fired"`,
		`  "$real_tmux" kill-server >/dev/null 2>&1 || true`,
		"fi",
		`exit "$status"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(wrapperDir, "br"), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write br outage wrapper: %v", err)
	}
	return wrapperDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(f.env, "PATH"), fired
}

func (f *atomicAssignmentCLIFixture) armGuardedClaimInfoThenKillTmux(t *testing.T, name string) (path string, fired string) {
	t.Helper()
	wrapperDir := filepath.Join(f.root, "claim-info-outage-bin-"+name)
	if err := os.MkdirAll(wrapperDir, 0o700); err != nil {
		t.Fatalf("create guarded-info outage wrapper directory: %v", err)
	}
	sentinel := filepath.Join(wrapperDir, "stop-private-tmux-after-guarded-info")
	fired = sentinel + ".fired"
	if err := os.WriteFile(sentinel, []byte("armed\n"), 0o600); err != nil {
		t.Fatalf("arm guarded-info outage wrapper: %v", err)
	}
	wrapper := strings.Join([]string{
		"#!/bin/sh",
		"real_br=" + tmux.ShellQuote(f.brPath),
		"real_tmux=" + tmux.ShellQuote(f.tmuxPath),
		"sentinel=" + tmux.ShellQuote(sentinel),
		"fired=" + tmux.ShellQuote(fired),
		`"$real_br" "$@"`,
		"status=$?",
		"info_command=0",
		"no_auto_flush=0",
		`for arg in "$@"; do`,
		`  if [ "$arg" = "info" ]; then info_command=1; fi`,
		`  if [ "$arg" = "--no-auto-flush" ]; then no_auto_flush=1; fi`,
		"done",
		`guarded_info=$((info_command * no_auto_flush))`,
		`if [ "$status" -eq 0 ] && [ "$guarded_info" -eq 1 ] && [ -e "$sentinel" ] && [ ! -e "$fired" ]; then`,
		`  mv "$sentinel" "$fired"`,
		`  "$real_tmux" kill-server >/dev/null 2>&1 || true`,
		"fi",
		`exit "$status"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(wrapperDir, "br"), []byte(wrapper), 0o700); err != nil {
		t.Fatalf("write guarded-info outage wrapper: %v", err)
	}
	return wrapperDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(f.env, "PATH"), fired
}

func (f *atomicAssignmentCLIFixture) createClaimedPendingViaTmuxOutage(t *testing.T, name, title, prompt string) (string, *atomicAssignmentRecord) {
	t.Helper()
	beadID := f.createBead(t, title)
	path, fired := f.armClaimThenKillTmux(t, name)
	failed := f.runNTM(t, map[string]string{"PATH": path}, atomicDirectArgs(f, beadID, prompt, false)...)
	if failed.exitCode == 0 || len(bytes.TrimSpace(failed.stderr)) != 0 {
		t.Fatalf("post-claim outage exit=%d stdout=%s stderr=%s", failed.exitCode, failed.stdout, failed.stderr)
	}
	var envelope atomicAssignmentDirectEnvelope
	decodeAtomicAssignmentJSON(t, failed.stdout, &envelope)
	if envelope.Success || envelope.Error == nil || envelope.Error.Code != "SEND_ERROR" {
		if envelope.Error == nil {
			t.Fatalf("post-claim outage envelope has no error: %+v", envelope)
		}
		t.Fatalf("post-claim outage error code=%q message=%q", envelope.Error.Code, envelope.Error.Message)
	}
	pending := f.readLedgerAssignment(t, beadID)
	if pending.Status != "claimed" || pending.ClaimState != "claimed" || pending.ClaimStatus != "in_progress" ||
		pending.ClaimAttempts != 1 || pending.DispatchState != "pending" || pending.DispatchAttempts != 1 ||
		pending.PendingPrompt != prompt || pending.PromptSent != "" || pending.LastDispatchError == "" ||
		pending.DispatchedAt != nil || pending.DispatchReceiptID != "" {
		t.Fatalf("post-claim outage was not durably recoverable: %+v", pending)
	}
	f.assertBead(t, beadID, "in_progress", pending.ClaimActor)
	if _, err := os.Stat(fired); err != nil {
		t.Fatalf("guarded claim finalization outage wrapper did not fire: %v", err)
	}
	return beadID, pending
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
	f.assertBeadAt(t, f.projectDir, beadID, wantStatus, wantAssignee)
}

func (f *atomicAssignmentCLIFixture) assertBeadAt(t *testing.T, dir, beadID, wantStatus, wantAssignee string) {
	t.Helper()
	output := f.mustBRAt(t, dir, "show", beadID, "--json")
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

func (f *atomicAssignmentCLIFixture) assertBeadAbsentAt(t *testing.T, dir, beadID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.brPath, "show", beadID, "--json")
	cmd.Dir = dir
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("br show %s timed out in %s", beadID, dir)
	}
	if err == nil {
		t.Fatalf("bead %s unexpectedly exists in %s: %s", beadID, dir, output)
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

func (f *atomicAssignmentCLIFixture) setLedgerAssignmentStatus(t *testing.T, beadID, status string) {
	t.Helper()
	data, err := os.ReadFile(f.ledgerPath())
	if err != nil {
		t.Fatalf("read assignment ledger for status update: %v", err)
	}
	var ledger map[string]json.RawMessage
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode assignment ledger for status update: %v", err)
	}
	var assignments map[string]json.RawMessage
	if err := json.Unmarshal(ledger["assignments"], &assignments); err != nil {
		t.Fatalf("decode assignments for status update: %v", err)
	}
	rawRecord, ok := assignments[beadID]
	if !ok {
		t.Fatalf("assignment ledger missing %s", beadID)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(rawRecord, &record); err != nil {
		t.Fatalf("decode assignment %s for status update: %v", beadID, err)
	}
	encodedStatus, _ := json.Marshal(status)
	completedAt, _ := json.Marshal(time.Now().UTC())
	record["status"] = encodedStatus
	record["completed_at"] = completedAt
	assignments[beadID], err = json.Marshal(record)
	if err != nil {
		t.Fatalf("encode assignment %s status update: %v", beadID, err)
	}
	ledger["assignments"], err = json.Marshal(assignments)
	if err != nil {
		t.Fatalf("encode assignment map status update: %v", err)
	}
	updated, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		t.Fatalf("encode assignment ledger status update: %v", err)
	}
	if err := os.WriteFile(f.ledgerPath(), append(updated, '\n'), 0o600); err != nil {
		t.Fatalf("write assignment ledger status update: %v", err)
	}
}

func (f *atomicAssignmentCLIFixture) setLedgerAssignmentReservations(t *testing.T, beadID string, paths []string, ids []int) {
	t.Helper()
	data, err := os.ReadFile(f.ledgerPath())
	if err != nil {
		t.Fatalf("read assignment ledger for reservation update: %v", err)
	}
	var ledger map[string]json.RawMessage
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode assignment ledger for reservation update: %v", err)
	}
	var assignments map[string]json.RawMessage
	if err := json.Unmarshal(ledger["assignments"], &assignments); err != nil {
		t.Fatalf("decode assignments for reservation update: %v", err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(assignments[beadID], &record); err != nil {
		t.Fatalf("decode assignment %s for reservation update: %v", beadID, err)
	}
	for key, value := range map[string]any{
		"reservation_completed": true,
		"reservation_state":     "granted",
		"reservation_agent":     recordString(record["agent_name"]),
		"reservation_target":    recordString(record["occupancy_key"]),
		"reserved_paths":        paths,
		"reservation_ids":       ids,
	} {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatalf("encode assignment %s field %s: %v", beadID, key, marshalErr)
		}
		record[key] = encoded
	}
	assignments[beadID], err = json.Marshal(record)
	if err != nil {
		t.Fatalf("encode assignment %s reservation update: %v", beadID, err)
	}
	ledger["assignments"], err = json.Marshal(assignments)
	if err != nil {
		t.Fatalf("encode assignment map reservation update: %v", err)
	}
	updated, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		t.Fatalf("encode assignment ledger reservation update: %v", err)
	}
	if err := os.WriteFile(f.ledgerPath(), append(updated, '\n'), 0o600); err != nil {
		t.Fatalf("write assignment ledger reservation update: %v", err)
	}
}

func (f *atomicAssignmentCLIFixture) setLedgerAssignmentLegacyPaneTarget(t *testing.T, beadID string) {
	t.Helper()
	data, err := os.ReadFile(f.ledgerPath())
	if err != nil {
		t.Fatalf("read assignment ledger for legacy target update: %v", err)
	}
	var ledger map[string]json.RawMessage
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode assignment ledger for legacy target update: %v", err)
	}
	var assignments map[string]json.RawMessage
	if err := json.Unmarshal(ledger["assignments"], &assignments); err != nil {
		t.Fatalf("decode assignments for legacy target update: %v", err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(assignments[beadID], &record); err != nil {
		t.Fatalf("decode assignment %s for legacy target update: %v", beadID, err)
	}
	delete(record, "occupancy_key")
	delete(record, "dispatch_target")
	assignments[beadID], err = json.Marshal(record)
	if err != nil {
		t.Fatalf("encode assignment %s legacy target update: %v", beadID, err)
	}
	ledger["assignments"], err = json.Marshal(assignments)
	if err != nil {
		t.Fatalf("encode assignment map legacy target update: %v", err)
	}
	updated, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		t.Fatalf("encode assignment ledger legacy target update: %v", err)
	}
	if err := os.WriteFile(f.ledgerPath(), append(updated, '\n'), 0o600); err != nil {
		t.Fatalf("write assignment ledger legacy target update: %v", err)
	}
}

func recordString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func assertAtomicAssignmentRecord(t *testing.T, record *atomicAssignmentRecord, beadID, prompt string, pane atomicAssignmentPane, agentType string) {
	t.Helper()
	assertAtomicAssignmentRecordWithClaimIdentity(t, record, beadID, prompt, pane, agentType, true)
}

func assertAtomicAssignmentRecordWithClaimIdentity(t *testing.T, record *atomicAssignmentRecord, beadID, prompt string, pane atomicAssignmentPane, agentType string, requireKeyedClaimActor bool) {
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
	if requireKeyedClaimActor && !strings.HasSuffix(record.ClaimActor, "/ntm-"+record.IdempotencyKey[:12]) {
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

func assertSameDirectReceipt(t *testing.T, first, replay atomicAssignmentDirectEnvelope) {
	t.Helper()
	if first.Data == nil || first.Data.Receipt == nil || replay.Data == nil || replay.Data.Receipt == nil {
		t.Fatalf("missing direct receipt: first=%+v replay=%+v", first.Data, replay.Data)
	}
	if first.Data.Receipt.WorkItemID != replay.Data.Receipt.WorkItemID ||
		first.Data.Receipt.Pane != replay.Data.Receipt.Pane ||
		first.Data.Receipt.Prompt != replay.Data.Receipt.Prompt ||
		first.Data.Receipt.Transport != replay.Data.Receipt.Transport ||
		first.Data.Receipt.Timestamp != replay.Data.Receipt.Timestamp {
		t.Fatalf("direct replay receipt changed: first=%+v replay=%+v", first.Data.Receipt, replay.Data.Receipt)
	}
}

func assertAtomicAssignmentReceiptUnchanged(t *testing.T, first, replay *atomicAssignmentRecord) {
	t.Helper()
	if first.IdempotencyKey != replay.IdempotencyKey || first.ClaimActor != replay.ClaimActor ||
		first.ClaimAttempts != replay.ClaimAttempts || first.DispatchAttempts != replay.DispatchAttempts ||
		first.DispatchedAt == nil || replay.DispatchedAt == nil || !first.DispatchedAt.Equal(*replay.DispatchedAt) ||
		first.DispatchReceiptID != replay.DispatchReceiptID || first.PromptSent != replay.PromptSent {
		t.Fatalf("durable receipt changed: first=%+v replay=%+v", first, replay)
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

func (f *atomicAssignmentCLIFixture) startAgentPanes(t *testing.T) {
	t.Helper()
	agentCommand := "bash --noprofile --norc -c 'stty -echo; exec cat'"
	f.mustTMUX(t, "-f", f.tmuxConfig, "new-session", "-d", "-s", f.session, "-x", "160", "-y", "48", "-c", f.projectDir, agentCommand)
	f.mustTMUX(t, "split-window", "-d", "-t", f.session+":0", "-c", f.projectDir, agentCommand)
	f.mustTMUX(t, "select-pane", "-t", f.session+":0.0", "-T", f.session+"__cod_1")
	f.mustTMUX(t, "select-pane", "-t", f.session+":0.1", "-T", f.session+"__cc_2")
	f.waitForPanes(t)
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

func (f *atomicAssignmentCLIFixture) driveAssignmentStatus(t *testing.T, pane atomicAssignmentPane, beadID, paneOutput, wantStatus string) {
	t.Helper()
	f.mustTMUX(t, "send-keys", "-t", pane.ID, "-l", paneOutput)
	f.mustTMUX(t, "send-keys", "-t", pane.ID, "Enter")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	watch := exec.CommandContext(ctx, f.ntmPath,
		"assign", f.session,
		"--repo="+f.projectDir,
		"--watch",
		"--watch-interval=100ms",
		"--dry-run",
		"--reserve-files=false",
		"--quiet",
	)
	watch.Dir = f.projectDir
	watch.Env = append([]string(nil), f.env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	watch.Stdout = &stdout
	watch.Stderr = &stderr
	if err := watch.Start(); err != nil {
		t.Fatalf("start assignment status watch: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		ledger, readErr := f.readLedger()
		if readErr == nil {
			if record := ledger.Assignments[beadID]; record != nil && record.Status == wantStatus {
				break
			}
		}
		if time.Now().After(deadline) {
			var durable *atomicAssignmentRecord
			if ledger != nil {
				durable = ledger.Assignments[beadID]
			}
			t.Fatalf("assignment detector did not mark %s %s: durable=%+v pane_output=%q read_err=%v stdout=%s stderr=%s",
				beadID, wantStatus, durable, f.captureEndpoint(t, pane), readErr, stdout.String(), stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := watch.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal assignment status watch: %v", err)
	}
	if err := watch.Wait(); err != nil {
		t.Fatalf("assignment status watch did not exit cleanly: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("assignment status watch exceeded deadline: %v", ctx.Err())
	}
	if len(bytes.TrimSpace(stderr.Bytes())) != 0 {
		t.Fatalf("assignment status watch stderr=%s", stderr.String())
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
	f.primeEndpointForSafeDispatch(t, endpoint)
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
	return f.runNTMInDir(t, f.projectDir, env, args...)
}

func (f *atomicAssignmentCLIFixture) runNTMInDir(t *testing.T, dir string, env map[string]string, args ...string) atomicAssignmentProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.ntmPath, args...)
	cmd.Dir = dir
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

func (f *atomicAssignmentCLIFixture) runNTMConcurrent(t *testing.T, count int, env map[string]string, args ...string) []atomicAssignmentProcessResult {
	t.Helper()
	return f.runNTMConcurrentAfterStart(t, count, env, nil, args...)
}

func (f *atomicAssignmentCLIFixture) runNTMConcurrentAfterStart(t *testing.T, count int, env map[string]string, afterStart func(), args ...string) []atomicAssignmentProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type runningProcess struct {
		cmd         *exec.Cmd
		stdout      bytes.Buffer
		stderr      bytes.Buffer
		gateReader  *os.File
		gateWriter  *os.File
		readyReader *os.File
		readyWriter *os.File
	}
	processes := make([]runningProcess, count)
	for i := range processes {
		gateReader, gateWriter, err := os.Pipe()
		if err != nil {
			t.Fatalf("create contender %d gate: %v", i, err)
		}
		readyReader, readyWriter, err := os.Pipe()
		if err != nil {
			_ = gateReader.Close()
			_ = gateWriter.Close()
			t.Fatalf("create contender %d readiness pipe: %v", i, err)
		}
		processes[i].gateReader = gateReader
		processes[i].gateWriter = gateWriter
		processes[i].readyReader = readyReader
		processes[i].readyWriter = readyWriter
		barrierArgs := []string{"-c", `printf x >&4; IFS= read -r _ <&3; exec "$@"`, "ntm-e2e-barrier", f.ntmPath}
		barrierArgs = append(barrierArgs, args...)
		processes[i].cmd = exec.CommandContext(ctx, "/bin/sh", barrierArgs...)
		processes[i].cmd.Dir = f.projectDir
		processes[i].cmd.Env = atomicAssignmentMergeEnv(f.env, env)
		processes[i].cmd.ExtraFiles = []*os.File{gateReader, readyWriter}
		processes[i].cmd.Stdout = &processes[i].stdout
		processes[i].cmd.Stderr = &processes[i].stderr
	}
	for i := range processes {
		if err := processes[i].cmd.Start(); err != nil {
			t.Fatalf("start ntm contender %d %q: %v", i, args, err)
		}
		_ = processes[i].gateReader.Close()
		_ = processes[i].readyWriter.Close()
	}
	for i := range processes {
		if err := processes[i].readyReader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("set contender %d readiness deadline: %v", i, err)
		}
		var ready [1]byte
		if _, err := io.ReadFull(processes[i].readyReader, ready[:]); err != nil {
			t.Fatalf("wait for contender %d at start barrier: %v", i, err)
		}
		_ = processes[i].readyReader.Close()
	}
	for i := range processes {
		if _, err := processes[i].gateWriter.Write([]byte("go\n")); err != nil {
			t.Fatalf("release contender %d start barrier: %v", i, err)
		}
		_ = processes[i].gateWriter.Close()
	}
	if afterStart != nil {
		afterStart()
	}

	results := make([]atomicAssignmentProcessResult, count)
	for i := range processes {
		err := processes[i].cmd.Wait()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("concurrent ntm commands timed out: %q", args)
		}
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("wait for ntm contender %d %q: %v", i, args, err)
			}
		}
		results[i] = atomicAssignmentProcessResult{
			stdout:   append([]byte(nil), processes[i].stdout.Bytes()...),
			stderr:   append([]byte(nil), processes[i].stderr.Bytes()...),
			exitCode: exitCode,
		}
		t.Logf("[E2E-ATOMIC] contender=%d exit=%d args=%q stdout=%s stderr=%s", i, exitCode, args,
			truncateString(processes[i].stdout.String(), 500), truncateString(processes[i].stderr.String(), 500))
	}
	return results
}

func (f *atomicAssignmentCLIFixture) mustBR(t *testing.T, args ...string) []byte {
	t.Helper()
	return f.mustBRAt(t, f.projectDir, args...)
}

func (f *atomicAssignmentCLIFixture) mustBRAt(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.brPath, args...)
	cmd.Dir = dir
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

func atomicAssignmentAnyIntSlice(value any) ([]int, bool) {
	values, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		number, ok := value.(float64)
		if !ok || number != float64(int(number)) {
			return nil, false
		}
		result = append(result, int(number))
	}
	return result, true
}

func atomicAssignmentIsolatedEnv(overrides map[string]string) []string {
	replaced := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {}, "XDG_STATE_HOME": {}, "XDG_CACHE_HOME": {}, "PWD": {}, "OLDPWD": {},
		"GIT_DIR": {}, "GIT_WORK_TREE": {}, "BR_DB": {}, "BD_DB": {}, "BEADS_DB": {}, "AGENT_NAME": {},
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

func atomicAssignmentEnvValue(env []string, name string) string {
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if key == name {
			return value
		}
	}
	return ""
}
