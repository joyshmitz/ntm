//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/agentmail"
	"github.com/Dicklesworthstone/ntm/internal/handoff"
	"github.com/Dicklesworthstone/ntm/internal/ratelimit"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

const (
	spawnAssignmentProjectID     = 77
	spawnAssignmentAgentID       = 88
	spawnAssignmentReservationID = 701
	spawnAssignmentPath          = "internal/robot/**"
	spawnAssignmentRecipient     = "ExactRecipient"
	spawnAssignmentDisplayName   = "DisplayAlias"
	spawnAssignmentToken         = "e2e-registration-token"
)

// TestE2ESpawnAssignmentProductionCLI crosses the complete production spawn
// assignment path: a built ntm process, private real tmux server, real br
// database, persisted pane identity, concrete Agent Mail HTTP client, atomic
// ledger, and tmux prompt delivery. The replay is a second OS process.
func TestE2ESpawnAssignmentProductionCLI(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newSpawnAssignmentCLIFixture(t)
	args := fixture.spawnArgs()

	firstResult := fixture.runNTM(t, args...)
	first := decodeSpawnAssignmentOutput(t, firstResult)
	firstAssignment := assertSpawnAssignmentOutput(t, first, fixture)
	fixture.waitForMarkerCount(t, 1)
	fixture.assertBead(t, "in_progress", firstAssignment.ClaimActor)

	firstRecord := fixture.readAssignment(t)
	assertSpawnAssignmentRecord(t, firstRecord, firstAssignment, fixture)
	firstMCPCounts := fixture.stub.assertCleanMutationCount(t, 1)

	firstDispatchedAt := *firstRecord.DispatchedAt
	firstReservationExpiresAt := *firstRecord.ReservationExpiresAt

	// Re-run the identical spawn intent from a fresh process. The existing
	// session is intentionally reused; its launch command is harmless input to
	// the fake agent, while the work marker must not be delivered again.
	secondResult := fixture.runNTM(t, args...)
	second := decodeSpawnAssignmentOutput(t, secondResult)
	secondAssignment := assertSpawnAssignmentOutput(t, second, fixture)

	if secondAssignment.IdempotencyKey != firstAssignment.IdempotencyKey ||
		secondAssignment.ClaimActor != firstAssignment.ClaimActor ||
		secondAssignment.DispatchReceiptID != firstAssignment.DispatchReceiptID ||
		!equalInts(secondAssignment.ReservationIDs, firstAssignment.ReservationIDs) {
		t.Fatalf("spawn replay identity changed: first=%+v second=%+v", firstAssignment, secondAssignment)
	}
	fixture.assertMarkerCount(t, 1)
	fixture.assertBead(t, "in_progress", firstAssignment.ClaimActor)
	secondMCPCounts := fixture.stub.assertCleanMutationCount(t, 1)
	if secondMCPCounts.ensure < firstMCPCounts.ensure || secondMCPCounts.list < firstMCPCounts.list {
		t.Fatalf("Agent Mail discovery counters moved backwards: first=%+v second=%+v", firstMCPCounts, secondMCPCounts)
	}

	replayed := fixture.readAssignment(t)
	if replayed.ClaimAttempts != 1 || replayed.ReservationAttempts != 1 || replayed.DispatchAttempts != 1 ||
		replayed.IdempotencyKey != firstRecord.IdempotencyKey ||
		replayed.DispatchReceiptID != firstRecord.DispatchReceiptID ||
		replayed.DispatchedAt == nil || !replayed.DispatchedAt.Equal(firstDispatchedAt) ||
		replayed.ReservationExpiresAt == nil || !replayed.ReservationExpiresAt.Equal(firstReservationExpiresAt) {
		t.Fatalf("spawn replay mutated durable side-effect receipts: before=%+v after=%+v", firstRecord, replayed)
	}
}

// TestE2EServeRESTSpawnBuiltBinary proves the production HTTP server keeps
// spawn responses truthful while launching only into its private tmux server.
func TestE2EServeRESTSpawnBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate serve E2E port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release serve E2E port: %v", err)
	}

	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	tmuxRoot := filepath.Join(root, "tmux")
	for _, dir := range []string{projectDir, homeDir, configDir, fakeBin, tmuxRoot, filepath.Join(root, "data")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create REST spawn fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated REST spawn E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated shell config: %v", err)
	}
	launchMarker := filepath.Join(root, "fake-claude-launched")
	writeSpawnLaunchMarkerAgent(t, filepath.Join(fakeBin, "claude"), launchMarker)

	env := spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  tmuxRoot,
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"AGENT_MAIL_URL":               "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":             "",
		"HTTP_PROXY":                   "",
		"HTTPS_PROXY":                  "",
		"ALL_PROXY":                    "",
		"NO_PROXY":                     "127.0.0.1,localhost",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	})

	logPath := filepath.Join(root, "serve.log")
	serverLog, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create serve log: %v", err)
	}
	serverCtx, stopServer := context.WithCancel(context.Background())
	serverCmd := exec.CommandContext(
		serverCtx,
		ntmPath,
		"serve",
		"--host=127.0.0.1",
		fmt.Sprintf("--port=%d", port),
	)
	serverCmd.Dir = projectDir
	serverCmd.Env = append([]string(nil), env...)
	serverCmd.Stdout = serverLog
	serverCmd.Stderr = serverLog
	if err := serverCmd.Start(); err != nil {
		_ = serverLog.Close()
		t.Fatalf("start ntm serve: %v", err)
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- serverCmd.Wait()
	}()
	t.Cleanup(func() {
		stopServer()
		select {
		case <-serverDone:
		case <-time.After(5 * time.Second):
			if serverCmd.Process != nil {
				_ = serverCmd.Process.Kill()
			}
			<-serverDone
		}
		_ = serverLog.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), env...)
		_ = cmd.Run()
	})

	readServerLog := func() string {
		data, readErr := os.ReadFile(logPath)
		if readErr != nil {
			return fmt.Sprintf("<read serve log: %v>", readErr)
		}
		return string(data)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}
	t.Cleanup(client.CloseIdleConnections)
	deadline := time.Now().Add(20 * time.Second)
	for {
		request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/health", nil)
		if requestErr != nil {
			t.Fatalf("create serve readiness request: %v", requestErr)
		}
		resp, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("ntm serve did not become ready: %v log=%s", requestErr, readServerLog())
		}
		time.Sleep(100 * time.Millisecond)
	}

	type restSpawnAgent struct {
		Pane  string `json:"pane"`
		Type  string `json:"type"`
		Ready bool   `json:"ready"`
	}
	type restSpawnEnvelope struct {
		Success   bool             `json:"success"`
		Timestamp string           `json:"timestamp"`
		Session   string           `json:"session"`
		Agents    []restSpawnAgent `json:"agents"`
		Error     string           `json:"error"`
		ErrorCode string           `json:"error_code"`
		Hint      string           `json:"hint"`
	}
	postSpawn := func(t *testing.T, session string) (int, restSpawnEnvelope) {
		t.Helper()
		request, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			baseURL+"/api/v1/sessions/"+url.PathEscape(session)+"/agents/spawn",
			strings.NewReader(`{"cc_count":1,"wait_ready":true}`),
		)
		if err != nil {
			t.Fatalf("create REST spawn request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("REST spawn request: %v log=%s", err, readServerLog())
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		closeErr := response.Body.Close()
		if err != nil {
			t.Fatalf("read REST spawn response: %v", err)
		}
		if closeErr != nil {
			t.Fatalf("close REST spawn response: %v", closeErr)
		}
		var envelope restSpawnEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("decode REST spawn response: %v raw=%s", err, body)
		}
		return response.StatusCode, envelope
	}

	session := fmt.Sprintf("ntm-e2e-rest-spawn-%d-%d", os.Getpid(), time.Now().UnixNano())
	status, spawned := postSpawn(t, session)
	if status != http.StatusOK || !spawned.Success || spawned.Timestamp == "" || spawned.Session != session || spawned.Error != "" || spawned.ErrorCode != "" {
		t.Fatalf("successful REST spawn status=%d envelope=%+v log=%s", status, spawned, readServerLog())
	}
	if len(spawned.Agents) != 2 {
		t.Fatalf("spawned agents = %+v, want user and claude", spawned.Agents)
	}
	claudeReady := false
	for _, agent := range spawned.Agents {
		if agent.Pane == "" {
			t.Fatalf("spawned agent missing pane: %+v", agent)
		}
		if agent.Type == "claude" && agent.Ready {
			claudeReady = true
		}
	}
	if !claudeReady {
		t.Fatalf("spawned agents did not include ready fake claude: %+v", spawned.Agents)
	}
	if _, err := os.Stat(launchMarker); err != nil {
		t.Fatalf("fake claude launch marker: %v", err)
	}

	checkSession := func(t *testing.T, commandEnv []string, target string) ([]byte, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "has-session", "-t", target)
		if commandEnv != nil {
			cmd.Env = append([]string(nil), commandEnv...)
		}
		return cmd.CombinedOutput()
	}
	if output, err := checkSession(t, env, session); err != nil {
		t.Fatalf("private tmux session missing: %v output=%s", err, output)
	}
	if _, err := checkSession(t, nil, session); err == nil {
		t.Fatalf("REST spawn leaked session %q onto the host tmux socket", session)
	}

	invalidSession := "invalid--reserved-label-separator"
	status, failed := postSpawn(t, invalidSession)
	if status != http.StatusBadRequest || failed.Success || failed.Timestamp == "" ||
		failed.ErrorCode != "INVALID_FLAG" || failed.Error == "" || failed.Hint == "" {
		t.Fatalf("invalid REST spawn status=%d envelope=%+v log=%s", status, failed, readServerLog())
	}
	if _, err := checkSession(t, env, invalidSession); err == nil {
		t.Fatalf("invalid REST spawn created private session %q", invalidSession)
	}
}

// TestE2ESpawnAssignmentPartialCoverageBuiltBinary proves a built robot-spawn
// process reports every eligible agent even when triage has less work than the
// spawned topology, without duplicating any assignment side effect.
func TestE2ESpawnAssignmentPartialCoverageBuiltBinary(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newSpawnAssignmentCLIFixture(t)
	baseArgs := fixture.spawnArgs()
	args := make([]string, 0, len(baseArgs)-1)
	for _, arg := range baseArgs {
		switch arg {
		case "--spawn-cc=1":
			arg = "--spawn-cc=2"
		case "--timeout=8s":
			arg = "--timeout=20s"
		case "--spawn-names=" + spawnAssignmentDisplayName:
			arg = "--spawn-names=" + spawnAssignmentDisplayName + ",NoWorkAgent"
		case "--spawn-wait":
			continue
		}
		args = append(args, arg)
	}

	result := fixture.runNTM(t, args...)
	if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("partial-coverage spawn exit=%d, want 1; stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(result.stdout)))
	var output spawnAssignmentOutput
	if err := decoder.Decode(&output); err != nil {
		t.Fatalf("decode partial-coverage spawn JSON: %v raw=%s", err, result.stdout)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("partial-coverage spawn emitted multiple JSON documents: err=%v extra=%v raw=%s", err, trailing, result.stdout)
	}
	if output.Success || output.Timestamp == "" || output.Session != fixture.session || output.WorkingDir != fixture.projectDir ||
		output.Mode != "orchestrator" || output.AssignStrategy != "top-n" || output.ErrorCode != "ASSIGNMENT_FAILED" || output.Error == "" {
		t.Fatalf("partial-coverage spawn envelope=%+v", output)
	}
	if len(output.Agents) != 2 || len(output.Assignments) != 2 {
		t.Fatalf("partial-coverage spawn agents/assignments=%+v/%+v", output.Agents, output.Assignments)
	}
	for i, agent := range output.Agents {
		if agent.Type != "claude" || agent.Error != "" || agent.Title != fmt.Sprintf("%s__cc_%d", fixture.session, i+1) {
			t.Fatalf("partial-coverage spawned agent[%d]=%+v", i, agent)
		}
	}
	if output.Agents[0].Name != spawnAssignmentDisplayName || output.Agents[1].Name != "NoWorkAgent" {
		t.Fatalf("partial-coverage spawned names=%+v", output.Agents)
	}

	const noWorkError = "no work assignment was produced for this eligible agent"
	var assigned *spawnAssignmentResponse
	var noWork *spawnAssignmentResponse
	claimedAndSent := 0
	for i := range output.Assignments {
		assignment := &output.Assignments[i]
		if assignment.Claimed && assignment.PromptSent {
			claimedAndSent++
			assigned = assignment
		}
		if assignment.ClaimError == noWorkError {
			noWork = assignment
		}
	}
	if claimedAndSent != 1 || assigned == nil || noWork == nil {
		t.Fatalf("partial-coverage assignment split=%+v", output.Assignments)
	}
	if assigned.AgentType != "claude" || assigned.BeadID != fixture.beadID || assigned.BeadTitle != fixture.beadTitle ||
		assigned.Priority != "P1" || assigned.ClaimActor == "" || assigned.IdempotencyKey == "" || assigned.DispatchReceiptID == "" ||
		!equalInts(assigned.ReservationIDs, []int{spawnAssignmentReservationID}) || assigned.ClaimError != "" || assigned.PromptError != "" {
		t.Fatalf("partial-coverage assigned receipt=%+v", assigned)
	}
	decodedKey, err := hex.DecodeString(assigned.IdempotencyKey)
	if err != nil || len(decodedKey) != 32 {
		t.Fatalf("partial-coverage idempotency key=%q bytes=%d err=%v", assigned.IdempotencyKey, len(decodedKey), err)
	}
	wantActor := spawnAssignmentRecipient + "/ntm-" + assigned.IdempotencyKey[:12]
	if assigned.ClaimActor != wantActor || !strings.Contains(assigned.DispatchReceiptID, fixture.paneID) {
		t.Fatalf("partial-coverage atomic identity receipt=%+v want_actor=%q pane_id=%q", assigned, wantActor, fixture.paneID)
	}
	if noWork.AgentType != "claude" || noWork.Pane == "" || noWork.Pane == assigned.Pane || noWork.BeadID != "" || noWork.BeadTitle != "" ||
		noWork.Priority != "" || noWork.Claimed || noWork.PromptSent || noWork.ClaimActor != "" || noWork.IdempotencyKey != "" ||
		noWork.DispatchReceiptID != "" || len(noWork.ReservationIDs) != 0 || noWork.PromptError != "" {
		t.Fatalf("partial-coverage synthesized no-work receipt=%+v", noWork)
	}

	fixture.waitForMarkerCount(t, 1)
	markerCountsBefore := fixture.sessionMarkerCounts(t)
	if markerCountsBefore[fixture.paneID] != 1 || totalSpawnMarkerCount(markerCountsBefore) != 1 {
		t.Fatalf("partial-coverage prompt counts=%v, want exactly one on assigned pane %s", markerCountsBefore, fixture.paneID)
	}
	fixture.assertBead(t, "in_progress", assigned.ClaimActor)
	beadBefore := fixture.mustBR(t, "show", fixture.beadID, "--json")
	record := fixture.readAssignment(t)
	assertSpawnAssignmentRecord(t, record, *assigned, fixture)
	ledgerPath := filepath.Join(fixture.homeDir, ".ntm", "sessions", fixture.session, "assignments.json")
	ledgerBefore, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("read partial-coverage assignment ledger: %v", err)
	}
	var ledger spawnAssignmentLedger
	if err := json.Unmarshal(ledgerBefore, &ledger); err != nil || len(ledger.Assignments) != 1 || ledger.Assignments[fixture.beadID] == nil {
		t.Fatalf("partial-coverage ledger=%+v err=%v raw=%s", ledger, err, ledgerBefore)
	}
	mailBefore := fixture.stub.assertCleanMutationCount(t, 1)

	time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
	fixture.assertBead(t, "in_progress", assigned.ClaimActor)
	beadAfter := fixture.mustBR(t, "show", fixture.beadID, "--json")
	if !bytes.Equal(bytes.TrimSpace(beadAfter), bytes.TrimSpace(beadBefore)) {
		t.Fatalf("partial-coverage bead mutated after response: before=%s after=%s", beadBefore, beadAfter)
	}
	ledgerAfter, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("reread partial-coverage assignment ledger: %v", err)
	}
	if !bytes.Equal(ledgerAfter, ledgerBefore) {
		t.Fatalf("partial-coverage ledger mutated after response: before=%s after=%s", ledgerBefore, ledgerAfter)
	}
	markerCountsAfter := fixture.sessionMarkerCounts(t)
	if !equalSpawnMarkerCounts(markerCountsAfter, markerCountsBefore) {
		t.Fatalf("partial-coverage prompt side effects continued after response: before=%v after=%v", markerCountsBefore, markerCountsAfter)
	}
	mailAfter := fixture.stub.assertCleanMutationCount(t, 1)
	if mailAfter != mailBefore {
		t.Fatalf("partial-coverage Agent Mail calls continued after response: before=%+v after=%+v", mailBefore, mailAfter)
	}
}

// TestE2ESpawnAssignFailureReturnsNonzeroSingleJSON proves the built CLI does
// not report a successful process when the requested post-spawn assignment
// phase fails after tmux topology has already been created.
func TestE2ESpawnAssignFailureReturnsNonzeroSingleJSON(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	fixture := newAtomicAssignmentCLIFixture(t)
	projectsBase := filepath.Join(fixture.root, "spawn-assign-projects")
	projectDir := filepath.Join(projectsBase, fixture.session)
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create spawn assignment failure project: %v", err)
	}
	fixture.mustBRAt(t, projectDir, "init", "--prefix=spawnfail", "--json")

	result := fixture.runNTM(t, map[string]string{"NTM_PROJECTS_BASE": projectsBase},
		"--json", "spawn", fixture.session,
		"--cod=1", "--no-user", "--no-hooks", "--no-cass-context", "--no-recovery",
		"--assign", "--ready-timeout=1s", "--assign-timeout=2s",
	)
	if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("spawn assignment failure exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	var envelope struct {
		Success   bool     `json:"success"`
		ErrorCode string   `json:"error_code"`
		Error     string   `json:"error"`
		Errors    []string `json:"errors"`
		Spawn     struct {
			Session string `json:"session"`
		} `json:"spawn"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode spawn assignment failure: %v raw=%s", err, result.stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("spawn assignment failure emitted multiple JSON documents: err=%v extra=%v raw=%s", err, extra, result.stdout)
	}
	if envelope.Success || envelope.ErrorCode != "ASSIGNMENT_FAILED" || envelope.Error == "" || len(envelope.Errors) == 0 || envelope.Spawn.Session != fixture.session {
		t.Fatalf("spawn assignment failure envelope=%+v", envelope)
	}
}

// TestE2ESpawnAssignmentSignalCancellationMatrix proves that command-root
// cancellation reaches every production assignment workflow and that each
// command joins its external workers before returning a single terminal JSON
// document. The blocked commands are real child processes; Beads and tmux are
// isolated but otherwise use their production binaries and durable stores.
func TestE2ESpawnAssignmentSignalCancellationMatrix(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("normal_assign_SIGINT_cancels_BV_discovery", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		marker := fmt.Sprintf("NTM_CANCEL_NORMAL_%d", time.Now().UnixNano())
		beadID := fixture.createBead(t, marker)
		env, startedDir := installSpawnSignalBlockingCommand(t, fixture.env, "bv", "", "*")
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGINT,
			"--json", "assign", fixture.session,
			"--repo="+fixture.projectDir,
			"--beads="+beadID,
			"--limit=1",
			"--auto",
			"--reserve-files=false",
			"--timeout=30s",
		)
		assertSpawnSignalJSONFailure(t, result, "")
		fixture.assertBead(t, beadID, "open", "")
		fixture.assertLedgerHasNoAssignment(t, beadID)
		fixture.assertMarkerCounts(t, marker, map[int]int{0: 0, 1: 0})
	})

	t.Run("direct_assign_SIGTERM_cancels_atomic_delivery_before_enter", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		marker := fmt.Sprintf("NTM_CANCEL_DIRECT_%d", time.Now().UnixNano())
		beadID := fixture.createBead(t, marker)
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, fixture.env, fixture.tmuxPath)
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGTERM,
			atomicDirectArgs(fixture, beadID, marker, false)...,
		)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
		logData, err := os.ReadFile(sendLog)
		if err != nil {
			t.Fatalf("read direct-assignment staged tmux send log: %v", err)
		}
		logText := string(logData)
		if strings.Count(logText, "send-keys") != 1 || !strings.Contains(logText, " -l ") || strings.Contains(logText, " Enter") {
			t.Fatalf("canceled direct assignment tmux calls=%q, want one literal stage and no Enter", logText)
		}
		record := fixture.readLedgerAssignment(t, beadID)
		if record.ClaimState != "claimed" || record.ClaimActor == "" || record.ClaimedAt == nil ||
			record.DispatchState != "sending" || record.DispatchAttempts != 1 || record.DispatchedAt != nil ||
			record.DispatchReceiptID != "" || record.PromptSent != "" {
			t.Fatalf("canceled direct assignment durable boundary = %+v", record)
		}
		fixture.assertBead(t, beadID, "in_progress", record.ClaimActor)
		fixture.assertMarkerCounts(t, marker, map[int]int{0: 0, 1: 0})
	})

	t.Run("direct_send_SIGINT_cancels_after_staging_before_enter", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		marker := fmt.Sprintf("NTM_CANCEL_SEND_%d", time.Now().UnixNano())
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, fixture.env, fixture.tmuxPath)
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGINT,
			"--json", "send", fixture.session,
			"--pane="+fixture.panes[0].ID,
			"--no-hooks",
			"--no-cass-check",
			marker,
		)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")

		// Cancellation lands during the one-second double-Enter delay. Waiting
		// past the complete legacy protocol proves no orphaned timer or process
		// can submit either Enter after the command has returned.
		time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
		logData, err := os.ReadFile(sendLog)
		if err != nil {
			t.Fatalf("read staged tmux send log: %v", err)
		}
		logText := string(logData)
		if strings.Count(logText, "send-keys") != 1 || !strings.Contains(logText, " -l ") || strings.Contains(logText, " Enter") {
			t.Fatalf("canceled direct send tmux calls=%q, want one literal stage and no Enter", logText)
		}
		fixture.assertMarkerCounts(t, marker, map[int]int{0: 0, 1: 0})
		assertSpawnTimelineExcludesMarker(t, filepath.Join(fixture.root, "data", "ntm", "timelines"), marker)
	})

	t.Run("cli_add_SIGTERM_cancels_agent_launch_before_enter", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		fakeAgentDir := filepath.Join(fixture.root, "add-agent-bin")
		if err := os.MkdirAll(fakeAgentDir, 0o700); err != nil {
			t.Fatalf("create add fake-agent directory: %v", err)
		}
		launchMarker := filepath.Join(fixture.root, "add-agent-launched")
		writeSpawnLaunchMarkerAgent(t, filepath.Join(fakeAgentDir, "codex"), launchMarker)
		env := atomicAssignmentMergeEnv(fixture.env, map[string]string{
			"PATH": fakeAgentDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(fixture.env, "PATH"),
		})
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, env, fixture.tmuxPath)
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGTERM,
			"--json", "add", fixture.session, "--cod=1", "--no-cass-context",
		)
		assertSpawnSignalCodedJSONError(t, result, "TIMEOUT")

		// The pane split and prompt stage may already have completed. Waiting past
		// the Enter delay proves the canceled command cannot submit or launch the
		// agent after its process has returned.
		time.Sleep(tmux.DefaultEnterDelay + 250*time.Millisecond)
		assertSpawnStagedWithoutEnter(t, sendLog)
		if _, err := os.Stat(launchMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("added agent launched after cancellation: marker error=%v", err)
		}
	})

	t.Run("cli_spawn_SIGINT_cancels_agent_launch_before_enter", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		spawnSession := fmt.Sprintf("ntm-e2e-cli-launch-%d", time.Now().UnixNano())
		projectsBase := filepath.Join(fixture.root, "spawn-projects")
		spawnDir := filepath.Join(projectsBase, spawnSession)
		fakeAgentDir := filepath.Join(fixture.root, "spawn-agent-bin")
		if err := os.MkdirAll(spawnDir, 0o700); err != nil {
			t.Fatalf("create CLI spawn project: %v", err)
		}
		if err := os.MkdirAll(fakeAgentDir, 0o700); err != nil {
			t.Fatalf("create CLI spawn fake-agent directory: %v", err)
		}
		launchMarker := filepath.Join(fixture.root, "cli-agent-launched")
		writeSpawnLaunchMarkerAgent(t, filepath.Join(fakeAgentDir, "claude"), launchMarker)
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, fixture.env, fixture.tmuxPath)
		env = atomicAssignmentMergeEnv(env, map[string]string{
			"NTM_PROJECTS_BASE": projectsBase,
			"PATH":              fakeAgentDir + string(os.PathListSeparator) + atomicAssignmentEnvValue(env, "PATH"),
		})
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, spawnDir, env, startedDir, 1, syscall.SIGINT,
			"--json", "spawn", spawnSession,
			"--cc=1", "--no-user", "--no-hooks", "--no-cass-context", "--no-recovery",
		)
		assertSpawnSignalCodedJSONError(t, result, "TIMEOUT")
		time.Sleep(500 * time.Millisecond)
		assertSpawnStagedWithoutEnter(t, sendLog)
		if _, err := os.Stat(launchMarker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("CLI agent launched after cancellation: marker error=%v", err)
		}
	})

	t.Run("robot_spawn_SIGTERM_cancels_agent_launch_before_enter", func(t *testing.T) {
		fixture := newSpawnAssignmentCLIFixture(t)
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, fixture.env, fixture.tmuxPath)
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGTERM, fixture.spawnArgs()...)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		time.Sleep(500 * time.Millisecond)
		assertSpawnStagedWithoutEnter(t, sendLog)
		fixture.assertBead(t, "open", "")
		fixture.assertMarkerCount(t, 0)
		fixture.stub.assertCleanMutationCount(t, 0)
	})

	t.Run("distribute_SIGINT_cancels_after_staging_before_enter", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		marker := fmt.Sprintf("NTM_CANCEL_DISTRIBUTE_%d", time.Now().UnixNano())
		beadID := fixture.createBead(t, marker)
		fixture.primePaneForSafeDispatch(t, 0)
		fixture.primePaneForSafeDispatch(t, 1)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(fixture.root, "config"))
		registry := agentmail.NewSessionAgentRegistry(fixture.session, fixture.projectDir)
		for pane, endpoint := range fixture.panes {
			registry.AddAgent(endpoint.Title, endpoint.ID, fmt.Sprintf("DistributeAgent%d", pane))
		}
		if err := agentmail.SaveSessionAgentRegistry(registry); err != nil {
			t.Fatalf("save distribute session project mapping: %v", err)
		}
		env, startedDir, sendLog := installSpawnSignalStagingTMUX(t, fixture.env, fixture.tmuxPath)
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 1, syscall.SIGINT,
			"--json", "send", fixture.session,
			"--distribute",
			"--dist-limit=1",
			"--dist-auto",
		)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
		assertSpawnStagedWithoutEnter(t, sendLog)
		fixture.assertBead(t, beadID, "open", "")
		fixture.assertLedgerHasNoAssignment(t, beadID)
		stagedCopies := 0
		for pane := range fixture.panes {
			stagedCopies += strings.Count(fixture.capturePane(t, pane), marker)
		}
		if stagedCopies != 1 {
			t.Fatalf("canceled distribute marker staged copies=%d, want exactly one unsubmitted copy", stagedCopies)
		}
	})

	t.Run("robot_bulk_parallel_SIGTERM_joins_claim_workers", func(t *testing.T) {
		fixture := newAtomicAssignmentCLIFixture(t)
		marker := fmt.Sprintf("NTM_CANCEL_BULK_%d", time.Now().UnixNano())
		firstBead := fixture.createBead(t, marker+"_ONE")
		secondBead := fixture.createBead(t, marker+"_TWO")
		fixture.primePaneForSafeDispatch(t, 0)
		fixture.primePaneForSafeDispatch(t, 1)
		env, startedDir := installSpawnSignalBlockingCommand(t, fixture.env, "tmux", fixture.tmuxPath, "capture-pane")
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, env, startedDir, 2, syscall.SIGTERM,
			"--robot-format=json",
			"--robot-bulk-assign="+fixture.session,
			"--from-bv",
			"--bulk-strategy=ready",
			"--bulk-parallel",
		)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		for _, beadID := range []string{firstBead, secondBead} {
			fixture.assertBead(t, beadID, "open", "")
			assertAtomicSignalCancellationNoActuation(t, fixture, beadID)
		}
		fixture.assertMarkerCounts(t, marker, map[int]int{0: 0, 1: 0})
	})

	t.Run("robot_spawn_SIGINT_cancels_assignment_claim", func(t *testing.T) {
		fixture := newSpawnAssignmentCLIFixture(t)
		startedDir := t.TempDir()
		fixture.stub.blockAgentsPath = filepath.Join(startedDir, "agents-read")
		fixture.stub.blockAgentsAt = 3 // two projection reads, then assignment admission
		result := runSpawnSignalCanceledCLI(t, fixture.ntmPath, fixture.projectDir, fixture.env, startedDir, 1, syscall.SIGINT, fixture.spawnArgs()...)
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		fixture.assertBead(t, "open", "")
		fixture.assertMarkerCount(t, 0)
		counts := fixture.stub.assertCleanMutationCount(t, 0)
		if counts.ensure < 2 || counts.list < fixture.stub.blockAgentsAt {
			t.Fatalf("spawn assignment cancellation stopped before assignment admission: counts=%+v", counts)
		}
		assertSpawnSignalCancellationNoActuation(t, fixture)
	})
}

// TestE2ESpawnPromptCanonicalDispatch runs the built CLI against a private
// real tmux server. It proves user and init prompts reach the exact spawned
// pane in order, final-message redaction applies at delivery, the init phase
// exposes a canonical receipt, and the identity preamble names the physical
// pane deterministically.
func TestE2ESpawnPromptCanonicalDispatch(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

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
		t.Skipf("br is required for spawn init E2E: %v", err)
	}

	root := t.TempDir()
	session := fmt.Sprintf("ntm-e2e-spawn-prompt-%d-%d", os.Getpid(), time.Now().UnixNano())
	projectsBase := filepath.Join(root, "projects")
	projectDir := filepath.Join(projectsBase, session)
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{projectDir, homeDir, configDir, fakeBin, filepath.Join(root, "data"), filepath.Join(root, "tmux")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create spawn prompt fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated zsh configuration: %v", err)
	}
	writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))
	writeSpawnEmptyBV(t, filepath.Join(fakeBin, "bv"))
	if err := os.MkdirAll(filepath.Join(configDir, "ntm"), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ntm", "config.toml"), []byte(strings.Join([]string{
		"[agent_mail]",
		"enabled = false",
		"",
		"[redaction]",
		"mode = \"redact\"",
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write spawn prompt config: %v", err)
	}

	env := spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  filepath.Join(root, "tmux"),
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_PROJECTS_BASE":            projectsBase,
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"AGENT_MAIL_URL":               "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":             "",
		"HTTP_PROXY":                   "",
		"HTTPS_PROXY":                  "",
		"ALL_PROXY":                    "",
		"NO_PROXY":                     "127.0.0.1,localhost",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), env...)
		_ = cmd.Run()
	})

	brCtx, brCancel := context.WithTimeout(context.Background(), 15*time.Second)
	brCmd := exec.CommandContext(brCtx, brPath, "init", "--prefix=spawnprompt", "--json")
	brCmd.Dir = projectDir
	brCmd.Env = append([]string(nil), env...)
	brOutput, brErr := brCmd.CombinedOutput()
	brCancel()
	if brErr != nil {
		t.Fatalf("initialize isolated Beads fixture: %v output=%s", brErr, brOutput)
	}

	const secret = "hunter2hunter2"
	userMarker := "SPAWN_USER_PROMPT_MARKER"
	initMarker := "SPAWN_INIT_PROMPT_MARKER"
	args := []string{
		"--json", "spawn", session,
		"--cc=1", "--no-user", "--no-hooks", "--no-cass-context", "--no-recovery",
		"--prompt=" + userMarker + " password=" + secret,
		"--assign", "--init-prompt=" + initMarker + " password=" + secret,
		"--with-agent-name", "--ready-timeout=8s", "--assign-timeout=5s",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cmd := exec.CommandContext(ctx, ntmPath, args...)
	cmd.Dir = projectDir
	cmd.Env = append([]string(nil), env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	cancel()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("spawn prompt CLI timed out: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if runErr != nil || strings.TrimSpace(stderr.String()) != "" {
		debugCtx, debugCancel := context.WithTimeout(context.Background(), 5*time.Second)
		debugCmd := exec.CommandContext(debugCtx, tmuxPath, "capture-pane", "-p", "-t", session, "-S", "-200")
		debugCmd.Env = append([]string(nil), env...)
		debugOutput, debugErr := debugCmd.CombinedOutput()
		debugCancel()
		t.Fatalf(
			"spawn prompt CLI error=%v stdout=%s stderr=%s pane_capture_error=%v pane=%q",
			runErr, stdout.String(), stderr.String(), debugErr, debugOutput,
		)
	}

	var output spawnPromptCLIOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("decode spawn prompt output: %v raw=%s", err, stdout.String())
	}
	if !output.Spawn.Created || output.Spawn.Session != session || !output.Init.PromptSent || output.Init.AgentsReached != 1 {
		t.Fatalf("spawn/init envelope = %+v", output)
	}
	if len(output.Init.Receipts) != 1 {
		t.Fatalf("init receipts = %+v, want one", output.Init.Receipts)
	}
	receipt := output.Init.Receipts[0]
	if receipt.Status != "delivered" || receipt.Protocol != "double_enter" || receipt.Target.Ref.PaneID == "" ||
		receipt.Target.AgentType != "cc" || receipt.Redaction.Mode != "redact" || receipt.Redaction.Findings == 0 {
		t.Fatalf("init receipt = %+v", receipt)
	}

	captureCtx, captureCancel := context.WithTimeout(context.Background(), 5*time.Second)
	captureCmd := exec.CommandContext(captureCtx, tmuxPath, "capture-pane", "-p", "-t", receipt.Target.Ref.PaneID, "-S", "-2000")
	captureCmd.Env = append([]string(nil), env...)
	captured, captureErr := captureCmd.CombinedOutput()
	captureCancel()
	if captureErr != nil {
		t.Fatalf("capture spawned pane: %v output=%s", captureErr, captured)
	}
	paneOutput := string(captured)
	compactOutput := strings.ReplaceAll(paneOutput, "\n", "")
	userDelivery := "RECEIVED:" + userMarker
	initDelivery := "RECEIVED:" + initMarker
	userIndex := strings.Index(compactOutput, userDelivery)
	identity := fmt.Sprintf("You are agent `%s_claude_%d`", session, receipt.Target.Ref.PaneIndex)
	identityIndex := strings.Index(compactOutput, identity)
	initIndex := strings.Index(compactOutput, initDelivery)
	if userIndex < 0 || identityIndex <= userIndex || initIndex <= identityIndex {
		t.Fatalf("prompt order user=%d identity=%d init=%d pane=%q", userIndex, identityIndex, initIndex, paneOutput)
	}
	if strings.Contains(paneOutput, secret) || !strings.Contains(paneOutput, "[REDACTED:PASSWORD:") {
		t.Fatalf("final-message redaction failed: pane=%q", paneOutput)
	}
	if strings.Count(compactOutput, userDelivery) != 1 || strings.Count(compactOutput, initDelivery) != 1 {
		t.Fatalf("prompt delivery count mismatch: pane=%q", paneOutput)
	}
}

// TestE2ESpawnPromptFailureTruthfulness exercises prompt failures through the
// built CLI and a private real tmux server. Every JSON case must fail nonzero
// with one terminal document, while reporting that the session/panes created
// before the failure still exist. Human mode must return the same failure as a
// normal command error and must not print a success footer.
func TestE2ESpawnPromptFailureTruthfulness(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	root := t.TempDir()
	projectsBase := filepath.Join(root, "projects")
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{
		projectsBase,
		homeDir,
		configDir,
		fakeBin,
		filepath.Join(root, "data"),
		filepath.Join(root, "tmux"),
		filepath.Join(configDir, "ntm"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create prompt failure fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated zsh configuration: %v", err)
	}
	writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))

	configPath := filepath.Join(configDir, "ntm", "config.toml")
	writeConfig := func(t *testing.T, promptConfig string) {
		t.Helper()
		contents := strings.Join([]string{
			"[agents]",
			"claude = \"claude {{if .Model}}--model {{shellQuote .Model}}{{end}} {{if .SystemPromptFile}}--append-system-prompt-file {{shellQuote .SystemPromptFile}}{{end}}\"",
			"",
			"[agent_mail]",
			"enabled = false",
			"",
			"[redaction]",
			"mode = \"off\"",
			"",
			promptConfig,
		}, "\n")
		if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write prompt failure config: %v", err)
		}
	}
	writeConfig(t, "")

	env := spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  filepath.Join(root, "tmux"),
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_PROJECTS_BASE":            projectsBase,
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"AGENT_MAIL_URL":               "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":             "",
		"HTTP_PROXY":                   "",
		"HTTPS_PROXY":                  "",
		"ALL_PROXY":                    "",
		"NO_PROXY":                     "127.0.0.1,localhost",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), env...)
		_ = cmd.Run()
	})

	failingTmux := filepath.Join(fakeBin, "tmux-fail-requested-prompt")
	failingTmuxScript := `#!/bin/sh
if [ "${1:-}" = "send-keys" ]; then
    case "$*" in
        *"$NTM_E2E_FAIL_PROMPT"*)
            printf 'injected requested prompt failure for %s\n' "$NTM_E2E_FAIL_PROMPT" >&2
            exit 97
            ;;
    esac
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
	if err := os.WriteFile(failingTmux, []byte(failingTmuxScript), 0o700); err != nil {
		t.Fatalf("write prompt-failing tmux wrapper: %v", err)
	}
	blockingTmux := filepath.Join(fakeBin, "tmux-block-requested-prompt")
	blockingTmuxScript := `#!/bin/sh
if [ "${1:-}" = "send-keys" ]; then
    case "$*" in
        *"$NTM_E2E_FAIL_PROMPT"*)
            : > "$NTM_E2E_PROMPT_STARTED"
            while :; do sleep 1; done
            ;;
    esac
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
	if err := os.WriteFile(blockingTmux, []byte(blockingTmuxScript), 0o700); err != nil {
		t.Fatalf("write prompt-blocking tmux wrapper: %v", err)
	}

	type processResult struct {
		stdout   []byte
		stderr   []byte
		exitCode int
	}
	runCLI := func(t *testing.T, dir string, extraEnv map[string]string, args ...string) processResult {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath, args...)
		cmd.Dir = dir
		cmd.Env = spawnAssignmentMergeEnv(env, extraEnv)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("prompt failure command timed out: args=%q stdout=%s stderr=%s", args, stdout.String(), stderr.String())
		}
		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatalf("run prompt failure command: %v", runErr)
			}
			exitCode = exitErr.ExitCode()
		}
		return processResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
	}
	runCanceledCLI := func(t *testing.T, dir string, extraEnv map[string]string, startedPath string, args ...string) processResult {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath, args...)
		cmd.Dir = dir
		cmd.Env = spawnAssignmentMergeEnv(env, extraEnv)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start cancellable prompt command: %v", err)
		}
		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(startedPath); err == nil {
				break
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat prompt dispatch marker: %v", err)
			}
			select {
			case runErr := <-waitDone:
				t.Fatalf("prompt command exited before cancellation point: %v stdout=%s stderr=%s", runErr, stdout.String(), stderr.String())
			default:
			}
			if time.Now().After(deadline) {
				t.Fatalf("prompt command never reached blocked dispatch: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			time.Sleep(25 * time.Millisecond)
		}
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("signal blocked prompt command: %v", err)
		}
		var runErr error
		select {
		case runErr = <-waitDone:
		case <-ctx.Done():
			t.Fatalf("blocked prompt command did not stop after signal: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatalf("wait for canceled prompt command: %v", runErr)
			}
			exitCode = exitErr.ExitCode()
		}
		return processResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
	}

	type failureEnvelope struct {
		Success         bool     `json:"success"`
		GeneratedAt     string   `json:"generated_at"`
		Session         string   `json:"session"`
		Error           string   `json:"error"`
		ErrorCode       string   `json:"error_code"`
		Code            string   `json:"code"`
		PartialMutation bool     `json:"partial_mutation"`
		SessionMayExist bool     `json:"session_may_exist"`
		AffectedPaneIDs []string `json:"affected_pane_ids"`
	}
	assertJSONFailure := func(t *testing.T, result processResult, session, code, errorFragment string) failureEnvelope {
		t.Helper()
		if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("prompt failure exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		decoder := json.NewDecoder(bytes.NewReader(result.stdout))
		var envelope failureEnvelope
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("decode prompt failure document: %v raw=%s", err, result.stdout)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatalf("prompt failure emitted multiple JSON documents: err=%v extra=%v raw=%s", err, extra, result.stdout)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(result.stdout, &raw); err != nil {
			t.Fatalf("decode raw prompt failure document: %v", err)
		}
		if value, ok := raw["success"]; !ok || string(value) != "false" {
			t.Fatalf("prompt failure omitted explicit success:false: raw=%s", result.stdout)
		}
		if envelope.Success || envelope.GeneratedAt == "" || envelope.Session != session ||
			envelope.ErrorCode != code || envelope.Code != code ||
			!envelope.PartialMutation || !envelope.SessionMayExist ||
			len(envelope.AffectedPaneIDs) == 0 || !strings.Contains(envelope.Error, errorFragment) {
			t.Fatalf("prompt failure envelope=%+v, want session=%s code=%s fragment=%q", envelope, session, code, errorFragment)
		}
		if bytes.Contains(result.stdout, []byte(`"created":true`)) || bytes.Contains(result.stdout, []byte(`"total_added"`)) {
			t.Fatalf("prompt failure leaked a success payload: %s", result.stdout)
		}
		return envelope
	}
	assertSessionExists := func(t *testing.T, session string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "has-session", "-t", session)
		cmd.Env = append([]string(nil), env...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("partially mutated session %s is missing: %v output=%s", session, err, output)
		}
	}
	createProject := func(t *testing.T, session string) string {
		t.Helper()
		dir := filepath.Join(projectsBase, session)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create prompt failure project %s: %v", session, err)
		}
		return dir
	}
	createShellSession := func(t *testing.T, session, dir string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "new-session", "-d", "-s", session, "-c", dir)
		cmd.Env = append([]string(nil), env...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create add fixture session %s: %v output=%s", session, err, output)
		}
	}
	failingEnv := func(marker string) map[string]string {
		return map[string]string{
			"NTM_TMUX_BINARY":     failingTmux,
			"NTM_E2E_REAL_TMUX":   tmuxPath,
			"NTM_E2E_FAIL_PROMPT": marker,
		}
	}
	baseSpawnArgs := func(session string) []string {
		return []string{"spawn", session, "--cc=1", "--no-user", "--no-hooks", "--no-cass-context", "--no-recovery"}
	}

	t.Run("add_explicit_prompt_json_send_failure", func(t *testing.T) {
		writeConfig(t, "")
		session := fmt.Sprintf("ntm-e2e-add-prompt-json-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		createShellSession(t, session, dir)
		marker := "ADD_EXPLICIT_PROMPT_FAILURE"
		result := runCLI(t, dir, failingEnv(marker),
			"--json", "add", session, "--cc=1", "--no-cass-context", "--prompt="+marker,
		)
		assertJSONFailure(t, result, session, "PROMPT_SEND_FAILED", "sending explicit prompt")
		assertSessionExists(t, session)
	})

	t.Run("add_explicit_prompt_human_send_failure", func(t *testing.T) {
		writeConfig(t, "")
		session := fmt.Sprintf("ntm-e2e-add-prompt-human-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		createShellSession(t, session, dir)
		marker := "ADD_HUMAN_PROMPT_FAILURE"
		result := runCLI(t, dir, failingEnv(marker),
			"add", session, "--cc=1", "--no-cass-context", "--prompt="+marker,
		)
		if result.exitCode != 1 || !strings.Contains(string(result.stderr), "sending explicit prompt") {
			t.Fatalf("human add prompt failure exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		if bytes.Contains(result.stdout, []byte("Added 1 agent")) || json.Valid(result.stdout) {
			t.Fatalf("human add prompt failure printed false success/JSON: %s", result.stdout)
		}
		assertSessionExists(t, session)
	})

	t.Run("add_explicit_prompt_cancellation_is_timeout", func(t *testing.T) {
		writeConfig(t, "")
		session := fmt.Sprintf("ntm-e2e-add-prompt-cancel-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		createShellSession(t, session, dir)
		marker := "ADD_CANCEL_PROMPT_FAILURE"
		startedPath := filepath.Join(root, session+"-dispatch-started")
		result := runCanceledCLI(t, dir, map[string]string{
			"NTM_TMUX_BINARY":        blockingTmux,
			"NTM_E2E_REAL_TMUX":      tmuxPath,
			"NTM_E2E_FAIL_PROMPT":    marker,
			"NTM_E2E_PROMPT_STARTED": startedPath,
		}, startedPath,
			"--json", "add", session, "--cc=1", "--no-cass-context", "--prompt="+marker,
		)
		assertJSONFailure(t, result, session, "TIMEOUT", "added-agent prompt canceled")
		assertSessionExists(t, session)
	})

	t.Run("add_explicit_prompt_waits_for_agent_readiness", func(t *testing.T) {
		writeConfig(t, "")
		readyFile := filepath.Join(root, fmt.Sprintf("add-agent-ready-%d", time.Now().UnixNano()))
		writeSpawnGatedFakeClaude(t, filepath.Join(fakeBin, "claude"), readyFile)
		defer writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))

		session := fmt.Sprintf("ntm-e2e-add-prompt-ready-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		createShellSession(t, session, dir)
		marker := fmt.Sprintf("ADD_READY_PROMPT_%d", time.Now().UnixNano())

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath,
			"--json", "add", session, "--cc=1", "--no-cass-context", "--prompt="+marker,
		)
		cmd.Dir = dir
		cmd.Env = append([]string(nil), env...)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start readiness-gated add: %v", err)
		}

		var paneID string
		readPane := func() string {
			t.Helper()
			if paneID == "" {
				listCtx, listCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer listCancel()
				listCmd := exec.CommandContext(listCtx, tmuxPath,
					"list-panes", "-s", "-t", session, "-F", "#{pane_id}|#{pane_title}",
				)
				listCmd.Env = append([]string(nil), env...)
				if output, err := listCmd.Output(); err == nil {
					for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
						parts := strings.SplitN(line, "|", 2)
						if len(parts) == 2 && strings.Contains(parts[1], "__cc_") {
							paneID = parts[0]
							break
						}
					}
				}
			}
			if paneID == "" {
				return ""
			}
			captureCtx, captureCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer captureCancel()
			captureCmd := exec.CommandContext(captureCtx, tmuxPath, "capture-pane", "-p", "-t", paneID, "-S", "-200")
			captureCmd.Env = append([]string(nil), env...)
			output, _ := captureCmd.Output()
			return string(output)
		}

		waitingDeadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(waitingDeadline) {
			captured := readPane()
			if strings.Contains(captured, "WAITING_FOR_E2E_READY") {
				if strings.Contains(captured, marker) {
					t.Fatalf("prompt reached pane before readiness gate opened: %s", captured)
				}
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		if captured := readPane(); !strings.Contains(captured, "WAITING_FOR_E2E_READY") {
			t.Fatalf("added agent never reached readiness gate: pane=%q stdout=%s stderr=%s", captured, stdout.String(), stderr.String())
		} else if strings.Contains(captured, marker) {
			t.Fatalf("prompt was actuated before readiness: %s", captured)
		}
		if err := os.WriteFile(readyFile, []byte("ready\n"), 0o600); err != nil {
			t.Fatalf("open added-agent readiness gate: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("readiness-gated add failed: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
		if strings.TrimSpace(stderr.String()) != "" {
			t.Fatalf("readiness-gated add stderr=%q, want empty", stderr.String())
		}
		var response struct {
			TotalAdded int `json:"total_added"`
			NewPanes   []struct {
				PaneID      string `json:"pane_id"`
				PaneTarget  string `json:"pane_target"`
				WindowIndex int    `json:"window_index"`
				Index       int    `json:"index"`
				Title       string `json:"title"`
			} `json:"new_panes"`
		}
		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode readiness-gated add response: %v raw=%s", err, stdout.String())
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatalf("readiness-gated add emitted multiple JSON documents: err=%v extra=%v raw=%s", err, extra, stdout.String())
		}
		if response.TotalAdded != 1 || len(response.NewPanes) != 1 || response.NewPanes[0].PaneID != paneID ||
			response.NewPanes[0].PaneTarget != fmt.Sprintf("%d.%d", response.NewPanes[0].WindowIndex, response.NewPanes[0].Index) {
			t.Fatalf("readiness-gated add response=%+v", response)
		}
		deliveryDeadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deliveryDeadline) {
			if strings.Contains(readPane(), "RECEIVED:"+marker) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("prompt was not delivered after readiness gate opened: pane=%q", readPane())
	})

	t.Run("spawn_recovery_deadline_is_bounded_and_prevents_late_prompt", func(t *testing.T) {
		writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))
		writeConfig(t, strings.Join([]string{
			"[recovery]",
			"enabled = true",
			"include_agent_mail = false",
			"include_cm_memories = false",
			"include_beads_context = true",
			"auto_inject_on_spawn = true",
		}, "\n"))
		brStarted := filepath.Join(root, fmt.Sprintf("recovery-br-started-%d", time.Now().UnixNano()))
		quotedStarted := strings.ReplaceAll(brStarted, "'", "'\"'\"'")
		blockingBR := fmt.Sprintf(`#!/bin/sh
for arg in "$@"; do
    if [ "$arg" = "list" ]; then
        : > '%s'
        while :; do sleep 1; done
    fi
done
printf '[]\n'
`, quotedStarted)
		if err := os.WriteFile(filepath.Join(fakeBin, "br"), []byte(blockingBR), 0o700); err != nil {
			t.Fatalf("write recovery-blocking br: %v", err)
		}

		session := fmt.Sprintf("ntm-e2e-recovery-timeout-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o700); err != nil {
			t.Fatalf("create recovery beads directory: %v", err)
		}
		marker := fmt.Sprintf("RECOVERY_TIMEOUT_MUST_NOT_DELIVER_%d", time.Now().UnixNano())
		startedAt := time.Now()
		result := runCLI(t, dir, nil,
			"--json", "spawn", session, "--cc=1", "--no-user", "--no-hooks", "--no-cass-context", "--prompt="+marker,
		)
		elapsed := time.Since(startedAt)
		assertJSONFailure(t, result, session, "TIMEOUT", "spawn recovery canceled")
		if elapsed > 12*time.Second {
			t.Fatalf("recovery timeout took %s, want bounded near 5s", elapsed)
		}
		if _, err := os.Stat(brStarted); err != nil {
			t.Fatalf("blocking br was not reached: %v", err)
		}
		assertSessionExists(t, session)
		capture := func() string {
			t.Helper()
			captureCtx, captureCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer captureCancel()
			captureCmd := exec.CommandContext(captureCtx, tmuxPath, "capture-pane", "-p", "-t", session+":0.0", "-S", "-200")
			captureCmd.Env = append([]string(nil), env...)
			output, _ := captureCmd.Output()
			return string(output)
		}
		if output := capture(); strings.Contains(output, marker) {
			t.Fatalf("recovery timeout delivered prompt before exit: %s", output)
		}
		time.Sleep(750 * time.Millisecond)
		if output := capture(); strings.Contains(output, marker) {
			t.Fatalf("recovery timeout produced late prompt actuation: %s", output)
		}
	})

	t.Run("spawn_partial_recovery_is_structured_in_success_json", func(t *testing.T) {
		writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))
		writeConfig(t, strings.Join([]string{
			"[recovery]",
			"enabled = true",
			"include_agent_mail = false",
			"include_cm_memories = false",
			"include_beads_context = true",
			"auto_inject_on_spawn = true",
		}, "\n"))
		failingBR := `#!/bin/sh
printf 'injected recovery source failure\n' >&2
exit 73
`
		if err := os.WriteFile(filepath.Join(fakeBin, "br"), []byte(failingBR), 0o700); err != nil {
			t.Fatalf("write recovery-failing br: %v", err)
		}

		session := fmt.Sprintf("ntm-e2e-recovery-partial-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o700); err != nil {
			t.Fatalf("create partial recovery beads directory: %v", err)
		}
		result := runCLI(t, dir, nil,
			"--json", "spawn", session, "--cc=1", "--no-user", "--no-hooks", "--no-cass-context",
		)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("partial recovery spawn exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var response struct {
			Created  bool `json:"created"`
			Recovery *struct {
				Enabled   bool     `json:"enabled"`
				Applied   bool     `json:"applied"`
				Partial   bool     `json:"partial"`
				ErrorCode string   `json:"error_code"`
				Warnings  []string `json:"warnings"`
			} `json:"recovery"`
		}
		decoder := json.NewDecoder(bytes.NewReader(result.stdout))
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode partial recovery spawn: %v raw=%s", err, result.stdout)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatalf("partial recovery spawn emitted multiple JSON documents: err=%v extra=%v raw=%s", err, extra, result.stdout)
		}
		if !response.Created || response.Recovery == nil || !response.Recovery.Enabled || response.Recovery.Applied ||
			!response.Recovery.Partial || response.Recovery.ErrorCode != "PARTIAL_RECOVERY" || len(response.Recovery.Warnings) != 1 ||
			!strings.Contains(response.Recovery.Warnings[0], "beads") {
			t.Fatalf("partial recovery status=%+v", response)
		}
	})

	for _, test := range []struct {
		name string
		flag string
	}{
		{name: "persona_prompt_preparation", flag: "--persona=reviewer"},
		{name: "profile_prompt_preparation", flag: "--profiles=reviewer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeConfig(t, "")
			session := fmt.Sprintf("ntm-e2e-%s-%d", strings.ReplaceAll(test.name, "_", "-"), time.Now().UnixNano())
			dir := createProject(t, session)
			ntmDir := filepath.Join(dir, ".ntm")
			if err := os.MkdirAll(ntmDir, 0o700); err != nil {
				t.Fatalf("create persona .ntm directory: %v", err)
			}
			if err := os.WriteFile(filepath.Join(ntmDir, "prompts"), []byte("path collision"), 0o600); err != nil {
				t.Fatalf("create persona prompt path collision: %v", err)
			}
			args := append([]string{"--json"}, baseSpawnArgs(session)...)
			args = append(args, test.flag)
			result := runCLI(t, dir, nil, args...)
			assertJSONFailure(t, result, session, "INTERNAL_ERROR", "prompts path is not a directory")
			assertSessionExists(t, session)
		})
	}

	t.Run("persona_launch_prompt_send_failure", func(t *testing.T) {
		writeConfig(t, "")
		session := fmt.Sprintf("ntm-e2e-persona-send-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		args := append([]string{"--json"}, baseSpawnArgs(session)...)
		args = append(args, "--persona=reviewer")
		result := runCLI(t, dir, failingEnv("reviewer.md"), args...)
		assertJSONFailure(t, result, session, "PROMPT_SEND_FAILED", "sending persona/profile reviewer launch prompt")
		assertSessionExists(t, session)
	})

	t.Run("configured_default_prompt_resolution_failure", func(t *testing.T) {
		missingPrompt := filepath.Join(root, "missing-default-prompt.md")
		writeConfig(t, fmt.Sprintf("[prompts]\ncc_default_file = %q\n", missingPrompt))
		session := fmt.Sprintf("ntm-e2e-default-resolve-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		args := append([]string{"--json"}, baseSpawnArgs(session)...)
		result := runCLI(t, dir, nil, args...)
		assertJSONFailure(t, result, session, "INTERNAL_ERROR", "default prompt resolution")
		assertSessionExists(t, session)
	})

	t.Run("configured_default_prompt_human_resolution_failure", func(t *testing.T) {
		missingPrompt := filepath.Join(root, "missing-human-default-prompt.md")
		writeConfig(t, fmt.Sprintf("[prompts]\ncc_default_file = %q\n", missingPrompt))
		session := fmt.Sprintf("ntm-e2e-default-human-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		result := runCLI(t, dir, nil, baseSpawnArgs(session)...)
		if result.exitCode != 1 || !strings.Contains(string(result.stderr), "default prompt resolution") {
			t.Fatalf("human default prompt failure exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		if bytes.Contains(result.stdout, []byte("Session ready")) || json.Valid(result.stdout) {
			t.Fatalf("human default prompt failure printed false success/JSON: %s", result.stdout)
		}
		assertSessionExists(t, session)
	})

	t.Run("configured_default_prompt_send_failure", func(t *testing.T) {
		marker := "DEFAULT_PROMPT_SEND_FAILURE"
		writeConfig(t, fmt.Sprintf("[prompts]\ncc_default = %q\n", marker))
		session := fmt.Sprintf("ntm-e2e-default-send-%d", time.Now().UnixNano())
		dir := createProject(t, session)
		args := append([]string{"--json"}, baseSpawnArgs(session)...)
		result := runCLI(t, dir, failingEnv(marker), args...)
		assertJSONFailure(t, result, session, "PROMPT_SEND_FAILED", "spawn prompt setup failed")
		assertSessionExists(t, session)
	})
}

// TestE2ESpawnResumeGlobalJSONSingleDocument proves a globally enabled JSON
// resume owns the complete spawn response. The nested spawn lifecycle must stay
// silent, and a handoff dispatch failure must remain one truthful nonzero JSON
// document with the per-pane outcome attached.
func TestE2ESpawnResumeGlobalJSONSingleDocument(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	root := t.TempDir()
	projectsBase := filepath.Join(root, "projects")
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{projectsBase, homeDir, configDir, fakeBin, filepath.Join(root, "data"), filepath.Join(root, "tmux")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create resume spawn fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated zsh configuration: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "ntm"), 0o700); err != nil {
		t.Fatalf("create resume spawn config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ntm", "config.toml"), []byte("[agent_mail]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("write resume spawn config: %v", err)
	}
	writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))

	baseEnv := spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  filepath.Join(root, "tmux"),
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_PROJECTS_BASE":            projectsBase,
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"AGENT_MAIL_URL":               "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":             "",
		"HTTP_PROXY":                   "",
		"HTTPS_PROXY":                  "",
		"ALL_PROXY":                    "",
		"NO_PROXY":                     "127.0.0.1,localhost",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), baseEnv...)
		_ = cmd.Run()
	})

	type resumeSpawnOutput struct {
		Success   bool   `json:"success"`
		Action    string `json:"action"`
		ErrorCode string `json:"error_code,omitempty"`
		Error     string `json:"error,omitempty"`
		SpawnInfo *struct {
			Session     string   `json:"session"`
			PaneCount   int      `json:"pane_count"`
			PanesFailed int      `json:"panes_failed"`
			PaneIDs     []string `json:"pane_ids,omitempty"`
		} `json:"spawn_info,omitempty"`
	}

	runResume := func(t *testing.T, session string, extraEnv map[string]string) (resumeSpawnOutput, int) {
		t.Helper()
		projectDir := filepath.Join(projectsBase, session)
		if err := os.MkdirAll(projectDir, 0o700); err != nil {
			t.Fatalf("create resume project directory: %v", err)
		}
		h := handoff.New(session)
		h.Goal = "Resume through one global JSON document"
		h.Now = "Deliver this handoff to the spawned agent"
		h.Status = handoff.StatusComplete
		h.Outcome = handoff.OutcomeSucceeded
		handoffPath, err := handoff.NewWriter(projectDir).Write(h, "resume-global-json")
		if err != nil {
			t.Fatalf("write resume handoff: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ntmPath,
			"--json", "resume", session, "--from="+handoffPath, "--spawn", "--cc=1",
		)
		cmd.Dir = projectDir
		cmd.Env = spawnAssignmentMergeEnv(baseEnv, extraEnv)
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		if ctx.Err() == context.DeadlineExceeded {
			t.Fatalf("global-JSON resume spawn timed out: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				t.Fatalf("run global-JSON resume spawn: %v", runErr)
			}
			exitCode = exitErr.ExitCode()
		}
		if strings.TrimSpace(stderr.String()) != "" {
			t.Fatalf("global-JSON resume spawn stderr=%q", stderr.String())
		}

		decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
		var output resumeSpawnOutput
		if err := decoder.Decode(&output); err != nil {
			t.Fatalf("decode global-JSON resume spawn: %v raw=%s", err, stdout.String())
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			t.Fatalf("global-JSON resume spawn emitted nested/trailing output: err=%v trailing=%v raw=%s", err, trailing, stdout.String())
		}
		return output, exitCode
	}

	t.Run("success", func(t *testing.T) {
		session := fmt.Sprintf("ntm-e2e-resume-json-ok-%d-%d", os.Getpid(), time.Now().UnixNano())
		output, exitCode := runResume(t, session, nil)
		if exitCode != 0 || !output.Success || output.Action != "spawn" || output.ErrorCode != "" || output.Error != "" {
			t.Fatalf("successful global-JSON resume exit=%d output=%+v", exitCode, output)
		}
		if output.SpawnInfo == nil || output.SpawnInfo.Session != session || output.SpawnInfo.PaneCount != 1 || output.SpawnInfo.PanesFailed != 0 || len(output.SpawnInfo.PaneIDs) != 1 {
			t.Fatalf("successful global-JSON resume spawn info=%+v", output.SpawnInfo)
		}
	})

	t.Run("dispatch_failure", func(t *testing.T) {
		wrapperPath := filepath.Join(fakeBin, "tmux-fail-paste")
		wrapper := `#!/bin/sh
if [ "$1" = "paste-buffer" ]; then
    printf 'injected handoff paste failure\n' >&2
    exit 91
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
			t.Fatalf("write failing tmux wrapper: %v", err)
		}
		session := fmt.Sprintf("ntm-e2e-resume-json-fail-%d-%d", os.Getpid(), time.Now().UnixNano())
		output, exitCode := runResume(t, session, map[string]string{
			"NTM_TMUX_BINARY":   wrapperPath,
			"NTM_E2E_REAL_TMUX": tmuxPath,
		})
		if exitCode != 1 || output.Success || output.Action != "spawn" || output.ErrorCode != "RESUME_FAILED" || output.Error == "" {
			t.Fatalf("failed global-JSON resume exit=%d output=%+v", exitCode, output)
		}
		if output.SpawnInfo == nil || output.SpawnInfo.Session != session || output.SpawnInfo.PaneCount != 0 || output.SpawnInfo.PanesFailed != 1 || len(output.SpawnInfo.PaneIDs) != 0 {
			t.Fatalf("failed global-JSON resume spawn info=%+v", output.SpawnInfo)
		}
	})

	t.Run("sigint", func(t *testing.T) {
		session := fmt.Sprintf("ntm-e2e-resume-json-cancel-%d-%d", os.Getpid(), time.Now().UnixNano())
		projectDir := filepath.Join(projectsBase, session)
		if err := os.MkdirAll(projectDir, 0o700); err != nil {
			t.Fatalf("create canceled resume spawn project: %v", err)
		}
		h := handoff.New(session)
		h.Goal = fmt.Sprintf("NTM_RESUME_SPAWN_CANCEL_%d", time.Now().UnixNano())
		h.Now = "Cancel after staging the resume handoff"
		h.Status = handoff.StatusComplete
		h.Outcome = handoff.OutcomeSucceeded
		handoffPath, err := handoff.NewWriter(projectDir).Write(h, "resume-spawn-cancel")
		if err != nil {
			t.Fatalf("write canceled resume spawn handoff: %v", err)
		}

		stateRoot := filepath.Join(root, fmt.Sprintf("resume-spawn-cancel-%d", time.Now().UnixNano()))
		if err := os.MkdirAll(stateRoot, 0o700); err != nil {
			t.Fatalf("create canceled resume spawn state: %v", err)
		}
		stagedPath := filepath.Join(stateRoot, "staged")
		enterLog := filepath.Join(stateRoot, "enter.log")
		commandLog := filepath.Join(stateRoot, "commands.log")
		wrapperPath := filepath.Join(fakeBin, fmt.Sprintf("tmux-resume-spawn-cancel-%d", time.Now().UnixNano()))
		wrapper := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$NTM_E2E_RESUME_SPAWN_COMMANDS"
command_name=${1:-}
target=
enter=0
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then target=$argument; fi
    if [ "$argument" = "Enter" ]; then enter=1; fi
    previous=$argument
done
if [ "$command_name" = "paste-buffer" ]; then
    "$NTM_E2E_REAL_TMUX" "$@"
    status=$?
    printf '%s\n' "$target" > "$NTM_E2E_RESUME_SPAWN_STAGED"
    exit "$status"
fi
if [ "$command_name" = "send-keys" ] && [ "$enter" -eq 1 ]; then
    printf '%s\n' "$target" >> "$NTM_E2E_RESUME_SPAWN_ENTER_LOG"
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
			t.Fatalf("write canceled resume spawn tmux wrapper: %v", err)
		}

		cmd := exec.Command(ntmPath,
			"--json", "resume", session, "--from="+handoffPath, "--spawn", "--cc=1",
		)
		cmd.Dir = projectDir
		cmd.Env = spawnAssignmentMergeEnv(baseEnv, map[string]string{
			"NTM_TMUX_BINARY":                wrapperPath,
			"NTM_E2E_REAL_TMUX":              tmuxPath,
			"NTM_E2E_RESUME_SPAWN_STAGED":    stagedPath,
			"NTM_E2E_RESUME_SPAWN_ENTER_LOG": enterLog,
			"NTM_E2E_RESUME_SPAWN_COMMANDS":  commandLog,
		})
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start canceled resume spawn: %v", err)
		}
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()
		finished := false
		defer func() {
			if !finished {
				_ = cmd.Process.Kill()
				select {
				case <-waited:
				case <-time.After(time.Second):
				}
			}
		}()

		waitForResumeInjectStage(t, cmd.Process, waited, stagedPath, &stdout, &stderr)
		enterBefore, err := os.ReadFile(enterLog)
		if err != nil {
			t.Fatalf("read pre-cancel resume spawn Enter log: %v", err)
		}
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("interrupt staged resume spawn: %v", err)
		}
		waitErr := waitForResumeInjectExit(t, cmd.Process, waited, &stdout, &stderr)
		finished = true
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("canceled resume spawn returned no process status: %v", waitErr)
		}
		result := spawnSignalProcessResult{
			stdout:   stdout.Bytes(),
			stderr:   stderr.Bytes(),
			exitCode: exitErr.ExitCode(),
		}
		assertSpawnSignalJSONFailure(t, result, "TIMEOUT")
		var output resumeSpawnOutput
		if err := json.Unmarshal(result.stdout, &output); err != nil {
			t.Fatalf("decode canceled resume spawn JSON: %v raw=%s", err, result.stdout)
		}
		if output.Success || output.Action != "spawn" || output.ErrorCode != "TIMEOUT" || output.Error == "" {
			t.Fatalf("canceled resume spawn output=%+v", output)
		}

		commandsBefore, err := os.ReadFile(commandLog)
		if err != nil {
			t.Fatalf("read returned resume spawn command log: %v", err)
		}
		time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
		enterAfter, err := os.ReadFile(enterLog)
		if err != nil {
			t.Fatalf("read post-cancel resume spawn Enter log: %v", err)
		}
		if !bytes.Equal(enterAfter, enterBefore) {
			t.Fatalf("resume spawn submitted Enter after cancellation: before=%q after=%q", enterBefore, enterAfter)
		}
		commandsAfter, err := os.ReadFile(commandLog)
		if err != nil {
			t.Fatalf("read post-cancel resume spawn command log: %v", err)
		}
		if !bytes.Equal(commandsAfter, commandsBefore) {
			t.Fatalf("resume spawn issued tmux commands after cancellation: before=%q after=%q", commandsBefore, commandsAfter)
		}
	})
}

type resumeInjectProcessOutput struct {
	Success   bool   `json:"success"`
	Action    string `json:"action"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Inject    *struct {
		Session     string `json:"session"`
		PanesSent   int    `json:"panes_sent"`
		PanesFailed int    `json:"panes_failed"`
	} `json:"inject_info,omitempty"`
}

type resumeInjectE2EFixture struct {
	canonical  *canonicalPaneFixture
	projectDir string
	handoff    string
	marker     string
	user       string
	agents     []string
}

// TestE2EResumeInjectUnifiedDispatch crosses the built-process and real-tmux
// boundary for the resume injection path. It proves the shared dispatcher
// excludes the user pane, keeps going after one target fails, reports complete
// failure truthfully, and joins a canceled double-Enter delivery before the
// terminal JSON document is emitted.
func TestE2EResumeInjectUnifiedDispatch(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("success", func(t *testing.T) {
		fixture := newResumeInjectE2EFixture(t)
		result := fixture.run(t, nil)
		var output resumeInjectProcessOutput
		decodeCLIJSONSuccess(t, result, &output)
		fixture.assertOutput(t, output, 3, 0, "")
		fixture.assertMarkerTargets(t, fixture.agents)
	})

	t.Run("partial", func(t *testing.T) {
		fixture := newResumeInjectE2EFixture(t)
		failedAddress := fixture.agents[1]
		failedPaneID := fixture.canonical.panes[failedAddress].ID
		wrapper := fixture.writeFailureWrapper(t)
		result := fixture.run(t, map[string]string{
			"NTM_TMUX_BINARY":          wrapper,
			"NTM_E2E_REAL_TMUX":        fixture.canonical.tmuxPath,
			"NTM_E2E_RESUME_FAIL_PANE": failedPaneID,
		})
		var output resumeInjectProcessOutput
		decodeCLIJSONFailure(t, result, &output)
		fixture.assertOutput(t, output, 2, 1, "RESUME_FAILED")
		fixture.assertMarkerTargets(t, []string{fixture.agents[0], fixture.agents[2]})
	})

	t.Run("all_fail", func(t *testing.T) {
		fixture := newResumeInjectE2EFixture(t)
		wrapper := fixture.writeFailureWrapper(t)
		result := fixture.run(t, map[string]string{
			"NTM_TMUX_BINARY":          wrapper,
			"NTM_E2E_REAL_TMUX":        fixture.canonical.tmuxPath,
			"NTM_E2E_RESUME_FAIL_ALL":  "1",
			"NTM_E2E_RESUME_FAIL_PANE": "",
		})
		var output resumeInjectProcessOutput
		decodeCLIJSONFailure(t, result, &output)
		fixture.assertOutput(t, output, 0, 3, "RESUME_FAILED")
		fixture.assertMarkerTargets(t, nil)
	})

	t.Run("sigint", func(t *testing.T) {
		fixture := newResumeInjectE2EFixture(t)
		wrapper, stagedPath, enterLog, commandLog := fixture.writeCancellationWrapper(t)
		cmd := exec.Command(fixture.canonical.ntmPath,
			"--json", "resume", fixture.canonical.session,
			"--from="+fixture.handoff, "--inject",
		)
		cmd.Dir = fixture.projectDir
		cmd.Env = mergeProcessEnv(fixture.canonical.env, map[string]string{
			"NTM_TMUX_BINARY":          wrapper,
			"NTM_E2E_REAL_TMUX":        fixture.canonical.tmuxPath,
			"NTM_E2E_RESUME_STAGED":    stagedPath,
			"NTM_E2E_RESUME_ENTER_LOG": enterLog,
			"NTM_E2E_RESUME_COMMANDS":  commandLog,
		})
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start resume injection cancellation process: %v", err)
		}
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()
		finished := false
		defer func() {
			if !finished {
				_ = cmd.Process.Kill()
				select {
				case <-waited:
				case <-time.After(time.Second):
				}
			}
		}()

		waitForResumeInjectStage(t, cmd.Process, waited, stagedPath, &stdout, &stderr)
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("interrupt staged resume injection: %v", err)
		}
		waitErr := waitForResumeInjectExit(t, cmd.Process, waited, &stdout, &stderr)
		finished = true
		exitErr := new(exec.ExitError)
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("canceled resume injection returned no process status: %v", waitErr)
		}
		processResult := spawnSignalProcessResult{
			stdout:   stdout.Bytes(),
			stderr:   stderr.Bytes(),
			exitCode: exitErr.ExitCode(),
		}
		assertSpawnSignalJSONFailure(t, processResult, "TIMEOUT")
		var output resumeInjectProcessOutput
		if err := json.Unmarshal(processResult.stdout, &output); err != nil {
			t.Fatalf("decode canceled resume injection JSON: %v raw=%s", err, processResult.stdout)
		}
		if output.Success || output.Action != "inject" || output.ErrorCode != "TIMEOUT" || output.Error == "" {
			t.Fatalf("canceled resume injection output=%+v", output)
		}

		commandsBefore, err := os.ReadFile(commandLog)
		if err != nil {
			t.Fatalf("read canceled resume tmux command log: %v", err)
		}
		stagedTarget, err := os.ReadFile(stagedPath)
		if err != nil {
			t.Fatalf("read canceled resume staged target: %v", err)
		}
		if strings.TrimSpace(string(stagedTarget)) != fixture.canonical.panes[fixture.agents[0]].ID {
			t.Fatalf("resume staged target=%q, want first agent %s", stagedTarget, fixture.canonical.panes[fixture.agents[0]].ID)
		}

		time.Sleep(tmux.DoubleEnterFirstDelay + tmux.DoubleEnterSecondDelay + 250*time.Millisecond)
		enterData, err := os.ReadFile(enterLog)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read canceled resume Enter log: %v", err)
		}
		if strings.TrimSpace(string(enterData)) != "" {
			t.Fatalf("resume injection submitted Enter after cancellation: %q", enterData)
		}
		commandsAfter, err := os.ReadFile(commandLog)
		if err != nil {
			t.Fatalf("reread canceled resume tmux command log: %v", err)
		}
		if !bytes.Equal(commandsAfter, commandsBefore) {
			t.Fatalf("resume injection issued tmux commands after cancellation: before=%q after=%q", commandsBefore, commandsAfter)
		}
		for _, address := range fixture.agents[1:] {
			if strings.Contains(fixture.canonical.capturePane(t, address), fixture.marker) {
				t.Fatalf("canceled resume injection staged marker in pending pane %s", address)
			}
		}
		if strings.Contains(fixture.canonical.capturePane(t, fixture.user), fixture.marker) {
			t.Fatalf("canceled resume injection leaked marker to user pane %s", fixture.user)
		}
	})
}

func newResumeInjectE2EFixture(t *testing.T) *resumeInjectE2EFixture {
	t.Helper()
	canonical := newCanonicalPaneFixture(t)
	userAddress := "0.0"
	userPane := canonical.panes[userAddress]
	canonical.mustTMUX(t, "select-pane", "-t", userPane.ID, "-T", canonical.session)
	userPane.Title = canonical.session
	userPane.Type = tmux.AgentUser
	canonical.panes[userAddress] = userPane

	marker := fmt.Sprintf("NTM_RESUME_INJECT_%d", time.Now().UnixNano())
	projectDir := filepath.Join(canonical.runtimeRoot, "resume-project")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatalf("create resume injection project: %v", err)
	}
	h := handoff.New(canonical.session)
	h.Goal = marker
	h.Now = "Continue the resume injection end-to-end proof"
	h.Status = handoff.StatusComplete
	h.Outcome = handoff.OutcomeSucceeded
	h.Next = []string{"Verify unified delivery receipts"}
	handoffPath, err := handoff.NewWriter(projectDir).Write(h, "resume-inject-e2e")
	if err != nil {
		t.Fatalf("write resume injection handoff: %v", err)
	}
	return &resumeInjectE2EFixture{
		canonical:  canonical,
		projectDir: projectDir,
		handoff:    handoffPath,
		marker:     marker,
		user:       userAddress,
		agents:     []string{"0.1", "1.0", "1.1"},
	}
}

func (f *resumeInjectE2EFixture) run(t *testing.T, extraEnv map[string]string) robotProcessResult {
	t.Helper()
	return f.canonical.runNTMInDir(t, f.projectDir, extraEnv,
		"--json", "resume", f.canonical.session, "--from="+f.handoff, "--inject",
	)
}

func (f *resumeInjectE2EFixture) assertOutput(t *testing.T, output resumeInjectProcessOutput, sent, failed int, errorCode string) {
	t.Helper()
	if output.Success != (errorCode == "") || output.Action != "inject" || output.ErrorCode != errorCode {
		t.Fatalf("resume injection terminal output=%+v", output)
	}
	if errorCode == "" && output.Error != "" {
		t.Fatalf("successful resume injection reported error=%q", output.Error)
	}
	if errorCode != "" && output.Error == "" {
		t.Fatalf("failed resume injection omitted error: %+v", output)
	}
	if output.Inject == nil || output.Inject.Session != f.canonical.session || output.Inject.PanesSent != sent || output.Inject.PanesFailed != failed {
		t.Fatalf("resume injection receipt=%+v, want session=%s sent=%d failed=%d", output.Inject, f.canonical.session, sent, failed)
	}
}

func (f *resumeInjectE2EFixture) assertMarkerTargets(t *testing.T, want []string) {
	t.Helper()
	wantSet := make(map[string]struct{}, len(want))
	for _, address := range want {
		wantSet[address] = struct{}{}
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		ready := true
		for _, address := range want {
			if !strings.Contains(f.canonical.capturePane(t, address), f.marker) {
				ready = false
				break
			}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("resume marker %q did not reach expected panes %v", f.marker, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for address := range f.canonical.panes {
		_, expected := wantSet[address]
		contains := strings.Contains(f.canonical.capturePane(t, address), f.marker)
		if contains != expected {
			t.Errorf("resume marker %q presence in %s=%t, want %t", f.marker, address, contains, expected)
		}
	}
}

func (f *resumeInjectE2EFixture) writeFailureWrapper(t *testing.T) string {
	t.Helper()
	path := filepath.Join(f.canonical.runtimeRoot, "bin", fmt.Sprintf("tmux-resume-fail-%d", time.Now().UnixNano()))
	script := `#!/bin/sh
set -eu
command_name=${1:-}
target=
literal=0
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then target=$argument; fi
    if [ "$argument" = "-l" ]; then literal=1; fi
    previous=$argument
done
if { [ "$command_name" = "paste-buffer" ] || { [ "$command_name" = "send-keys" ] && [ "$literal" -eq 1 ]; }; } &&
   { [ "${NTM_E2E_RESUME_FAIL_ALL:-0}" = "1" ] || [ "$target" = "${NTM_E2E_RESUME_FAIL_PANE:-}" ]; }; then
    printf 'injected resume delivery failure for %s\n' "$target" >&2
    exit 91
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write resume injection failure wrapper: %v", err)
	}
	return path
}

func (f *resumeInjectE2EFixture) writeCancellationWrapper(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := filepath.Join(f.canonical.runtimeRoot, fmt.Sprintf("resume-cancel-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create resume cancellation state: %v", err)
	}
	path := filepath.Join(f.canonical.runtimeRoot, "bin", fmt.Sprintf("tmux-resume-cancel-%d", time.Now().UnixNano()))
	stagedPath := filepath.Join(root, "staged")
	enterLog := filepath.Join(root, "enter.log")
	commandLog := filepath.Join(root, "commands.log")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$NTM_E2E_RESUME_COMMANDS"
command_name=${1:-}
target=
literal=0
enter=0
previous=
for argument in "$@"; do
    if [ "$previous" = "-t" ]; then target=$argument; fi
    if [ "$argument" = "-l" ]; then literal=1; fi
    if [ "$argument" = "Enter" ]; then enter=1; fi
    previous=$argument
done
if [ "$command_name" = "paste-buffer" ] || { [ "$command_name" = "send-keys" ] && [ "$literal" -eq 1 ]; }; then
    "$NTM_E2E_REAL_TMUX" "$@"
    status=$?
    printf '%s\n' "$target" > "$NTM_E2E_RESUME_STAGED"
    exit "$status"
fi
if [ "$command_name" = "send-keys" ] && [ "$enter" -eq 1 ]; then
    printf '%s\n' "$target" >> "$NTM_E2E_RESUME_ENTER_LOG"
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write resume injection cancellation wrapper: %v", err)
	}
	return path, stagedPath, enterLog, commandLog
}

func waitForResumeInjectStage(t *testing.T, process *os.Process, waited <-chan error, stagedPath string, stdout, stderr *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(stagedPath); err == nil {
			return
		}
		select {
		case waitErr := <-waited:
			t.Fatalf("resume injection exited before staging: %v stdout=%s stderr=%s", waitErr, stdout.String(), stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			_ = process.Signal(syscall.SIGQUIT)
			t.Fatalf("resume injection did not stage before timeout: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForResumeInjectExit(t *testing.T, process *os.Process, waited <-chan error, stdout, stderr *bytes.Buffer) error {
	t.Helper()
	select {
	case waitErr := <-waited:
		return waitErr
	case <-time.After(15 * time.Second):
		_ = process.Signal(syscall.SIGQUIT)
		t.Fatalf("resume injection did not join after cancellation: stdout=%s stderr=%s", stdout.String(), stderr.String())
		return nil
	}
}

// TestE2ESpawnPromptInterruptCancelsPendingDispatch proves an interrupted
// built spawn process cancels assignment readiness before init dispatch. The
// fake agent becomes dispatchable only after the process exits; any continued
// post-interrupt init path would then deliver the marker.
func TestE2ESpawnPromptInterruptCancelsPendingDispatch(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	root := t.TempDir()
	session := fmt.Sprintf("ntm-e2e-spawn-interrupt-%d-%d", os.Getpid(), time.Now().UnixNano())
	projectsBase := filepath.Join(root, "projects")
	projectDir := filepath.Join(projectsBase, session)
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	readyFile := filepath.Join(root, "agent-ready")
	for _, dir := range []string{projectDir, homeDir, configDir, fakeBin, filepath.Join(root, "data"), filepath.Join(root, "tmux")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create interrupted spawn fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated zsh configuration: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "ntm"), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ntm", "config.toml"), []byte("[agent_mail]\nenabled = false\n"), 0o600); err != nil {
		t.Fatalf("write interrupted spawn config: %v", err)
	}
	writeSpawnGatedFakeClaude(t, filepath.Join(fakeBin, "claude"), readyFile)

	env := spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  filepath.Join(root, "tmux"),
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_PROJECTS_BASE":            projectsBase,
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"AGENT_MAIL_URL":               "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":             "",
		"HTTP_PROXY":                   "",
		"HTTPS_PROXY":                  "",
		"ALL_PROXY":                    "",
		"NO_PROXY":                     "127.0.0.1,localhost",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), env...)
		_ = cmd.Run()
	})

	marker := fmt.Sprintf("SPAWN_INTERRUPT_MUST_NOT_DELIVER_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ntmPath,
		"--json", "spawn", session,
		"--cc=1", "--no-user", "--no-hooks", "--no-cass-context", "--no-recovery",
		"--assign", "--init-prompt="+marker, "--ready-timeout=30s", "--assign-timeout=5s",
	)
	cmd.Dir = projectDir
	cmd.Env = append([]string(nil), env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start interrupted spawn: %v", err)
	}

	var beforeInterrupt []byte
	readyDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(readyDeadline) {
		captureCtx, captureCancel := context.WithTimeout(context.Background(), time.Second)
		captureCmd := exec.CommandContext(captureCtx, tmuxPath, "capture-pane", "-p", "-t", session, "-S", "-200")
		captureCmd.Env = append([]string(nil), env...)
		captured, captureErr := captureCmd.CombinedOutput()
		captureCancel()
		if captureErr == nil && bytes.Contains(captured, []byte("WAITING_FOR_E2E_READY")) {
			beforeInterrupt = captured
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !bytes.Contains(beforeInterrupt, []byte("WAITING_FOR_E2E_READY")) {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		t.Fatalf("fake agent did not enter its readiness gate: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt spawn process: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr == nil {
			t.Fatal("interrupted spawn exited successfully, want a cancellation error")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("interrupted spawn did not cancel assignment readiness before returning")
	}

	if err := os.WriteFile(readyFile, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("release fake agent readiness gate: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	captureCtx, captureCancel := context.WithTimeout(context.Background(), 5*time.Second)
	captureCmd := exec.CommandContext(captureCtx, tmuxPath, "capture-pane", "-p", "-t", session, "-S", "-2000")
	captureCmd.Env = append([]string(nil), env...)
	afterInterrupt, captureErr := captureCmd.CombinedOutput()
	captureCancel()
	if captureErr != nil {
		t.Fatalf("capture interrupted spawn pane: %v output=%s", captureErr, afterInterrupt)
	}
	if bytes.Contains(afterInterrupt, []byte(marker)) {
		t.Fatalf("prompt marker was delivered after interrupted spawn returned: pane=%q", afterInterrupt)
	}
}

// TestE2ESpawnInterruptCancelsPrelaunchWaits exercises the two longest waits
// before agent process actuation. The fake executables leave an on-disk marker
// as their first instruction, so marker absence proves cancellation returned
// before tmux could launch either agent.
func TestE2ESpawnInterruptCancelsPrelaunchWaits(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	ntmPath, err := ensureE2ENTMBin()
	if err != nil {
		t.Fatalf("resolve E2E ntm binary: %v", err)
	}
	fixtureRoot := t.TempDir()

	t.Run("configured pane initialization delay", func(t *testing.T) {
		runSpawnPrelaunchCancellationE2E(t, ntmPath, filepath.Join(fixtureRoot, "i"), spawnPrelaunchCancellationCase{
			name:        "pane-init",
			agentFlag:   "--cc=1",
			agentBinary: "claude",
			config:      "[agent_mail]\nenabled = false\n\n[tmux]\npane_init_delay_ms = 30000\n",
			waitForBlockedPhase: func(t *testing.T, _, _ string, _ []string, outputPath string) bool {
				t.Helper()
				return waitForSpawnE2ECondition(10*time.Second, func() bool {
					out, readErr := os.ReadFile(outputPath)
					return readErr == nil && bytes.Contains(out, []byte("Waiting for panes to initialize"))
				})
			},
		})
	})

	t.Run("Codex cooldown", func(t *testing.T) {
		runSpawnPrelaunchCancellationE2E(t, ntmPath, filepath.Join(fixtureRoot, "c"), spawnPrelaunchCancellationCase{
			name:        "codex-cooldown",
			agentFlag:   "--cod=1",
			agentBinary: "codex",
			config:      "[agent_mail]\nenabled = false\n",
			extraArgs:   []string{"--no-user"},
			extraEnv:    map[string]string{"NTM_DISABLE_CODEX_PREFLIGHT": "1"},
			prepare: func(t *testing.T, projectDir string) {
				t.Helper()
				tracker := ratelimit.NewRateLimitTracker(projectDir)
				tracker.RecordRateLimitWithCooldown("openai", "spawn", 30)
				if err := tracker.SaveToDir(projectDir); err != nil {
					t.Fatalf("persist Codex cooldown fixture: %v", err)
				}
			},
			waitForBlockedPhase: func(t *testing.T, _, _ string, _ []string, outputPath string) bool {
				t.Helper()
				return waitForSpawnE2ECondition(10*time.Second, func() bool {
					out, readErr := os.ReadFile(outputPath)
					return readErr == nil && bytes.Contains(out, []byte("Codex cooldown active; waiting"))
				})
			},
		})
	})
}

type spawnPrelaunchCancellationCase struct {
	name                string
	agentFlag           string
	agentBinary         string
	config              string
	extraArgs           []string
	extraEnv            map[string]string
	prepare             func(*testing.T, string)
	waitForBlockedPhase func(*testing.T, string, string, []string, string) bool
}

func runSpawnPrelaunchCancellationE2E(t *testing.T, ntmPath, root string, tc spawnPrelaunchCancellationCase) {
	t.Helper()
	tmuxPath, err := exec.LookPath(tmux.BinaryPath())
	if err != nil {
		t.Fatalf("resolve tmux: %v", err)
	}

	session := fmt.Sprintf("ntm-e2e-spawn-%s-%d-%d", tc.name, os.Getpid(), time.Now().UnixNano())
	projectsBase := filepath.Join(root, "projects")
	projectDir := filepath.Join(projectsBase, session)
	homeDir := filepath.Join(root, "home")
	configDir := filepath.Join(root, "config")
	fakeBin := filepath.Join(root, "bin")
	launchMarker := filepath.Join(root, "agent-launched")
	outputPath := filepath.Join(root, "spawn-output.log")
	for _, dir := range []string{projectDir, homeDir, configDir, fakeBin, filepath.Join(root, "data"), filepath.Join(root, "tmux")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create prelaunch fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".zshrc"), []byte("# isolated E2E shell\n"), 0o600); err != nil {
		t.Fatalf("create isolated zsh configuration: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "ntm"), 0o700); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "ntm", "config.toml"), []byte(tc.config), 0o600); err != nil {
		t.Fatalf("write prelaunch config: %v", err)
	}
	writeSpawnLaunchMarkerAgent(t, filepath.Join(fakeBin, tc.agentBinary), launchMarker)
	if tc.prepare != nil {
		tc.prepare(t, projectDir)
	}

	overrides := map[string]string{
		"HOME":                         homeDir,
		"XDG_CONFIG_HOME":              configDir,
		"XDG_DATA_HOME":                filepath.Join(root, "data"),
		"TMUX_TMPDIR":                  filepath.Join(root, "tmux"),
		"PATH":                         fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"NTM_PROJECTS_BASE":            projectsBase,
		"NTM_DISABLE_INTERNAL_MONITOR": "1",
		"NTM_TEST_MODE":                "1",
		"NO_COLOR":                     "1",
		"TERM":                         "xterm-256color",
	}
	for key, value := range tc.extraEnv {
		overrides[key] = value
	}
	env := spawnAssignmentIsolatedEnv(overrides)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, tmuxPath, "kill-server")
		cmd.Env = append([]string(nil), env...)
		_ = cmd.Run()
	})

	logFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open prelaunch output: %v", err)
	}
	defer logFile.Close()

	args := []string{"spawn", session, tc.agentFlag, "--no-hooks", "--no-cass-context", "--no-recovery"}
	args = append(args, tc.extraArgs...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ntmPath, args...)
	cmd.Dir = projectDir
	cmd.Env = append([]string(nil), env...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start prelaunch spawn: %v", err)
	}

	if !tc.waitForBlockedPhase(t, tmuxPath, session, env, outputPath) {
		_ = cmd.Process.Signal(os.Interrupt)
		_ = cmd.Wait()
		_ = logFile.Sync()
		out, _ := os.ReadFile(outputPath)
		t.Fatalf("spawn never entered expected blocked phase: output=%s", out)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt prelaunch spawn: %v", err)
	}
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case waitErr := <-waitDone:
		if waitErr == nil {
			t.Fatal("interrupted prelaunch spawn exited successfully")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("prelaunch wait did not honor command cancellation")
	}
	if err := logFile.Sync(); err != nil {
		t.Fatalf("sync prelaunch output: %v", err)
	}
	if _, err := os.Stat(launchMarker); !errors.Is(err, os.ErrNotExist) {
		out, _ := os.ReadFile(outputPath)
		t.Fatalf("agent executable ran after cancellation: marker_error=%v output=%s", err, out)
	}
}

func waitForSpawnE2ECondition(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type spawnSignalProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

func installSpawnSignalBlockingCommand(t *testing.T, baseEnv []string, commandName, realPath, matchArg string) ([]string, string) {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	startedDir := filepath.Join(root, "started")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create blocking command directory: %v", err)
	}
	if err := os.MkdirAll(startedDir, 0o700); err != nil {
		t.Fatalf("create blocking command marker directory: %v", err)
	}
	fifoPath := filepath.Join(root, "blocked.fifo")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("create blocking command FIFO: %v", err)
	}
	script := `#!/bin/sh
set -eu
if [ "$NTM_E2E_BLOCK_MATCH" = "*" ] || [ "${1:-}" = "$NTM_E2E_BLOCK_MATCH" ]; then
    : > "$NTM_E2E_BLOCK_STARTED/$$"
    exec 3< "$NTM_E2E_BLOCK_FIFO"
    exit 97
fi
exec "$NTM_E2E_BLOCK_REAL" "$@"
`
	commandPath := filepath.Join(binDir, commandName)
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write blocking %s wrapper: %v", commandName, err)
	}
	if realPath == "" {
		realPath = "/bin/false"
	}
	pathValue := atomicAssignmentEnvValue(baseEnv, "PATH")
	if pathValue == "" {
		pathValue = os.Getenv("PATH")
	}
	overrides := map[string]string{
		"PATH":                  binDir + string(os.PathListSeparator) + pathValue,
		"NTM_E2E_BLOCK_FIFO":    fifoPath,
		"NTM_E2E_BLOCK_MATCH":   matchArg,
		"NTM_E2E_BLOCK_REAL":    realPath,
		"NTM_E2E_BLOCK_STARTED": startedDir,
	}
	if commandName == "tmux" {
		overrides["NTM_TMUX_BINARY"] = commandPath
	}
	return atomicAssignmentMergeEnv(baseEnv, overrides), startedDir
}

// installSpawnSignalStagingTMUX records successful tmux send-keys calls and
// signals the test only after the literal prompt has been staged. The CLI is
// then interrupted while its context-aware double-Enter delay is active.
func installSpawnSignalStagingTMUX(t *testing.T, baseEnv []string, realPath string) ([]string, string, string) {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	startedDir := filepath.Join(root, "started")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("create staging tmux wrapper directory: %v", err)
	}
	if err := os.MkdirAll(startedDir, 0o700); err != nil {
		t.Fatalf("create staging tmux marker directory: %v", err)
	}
	logPath := filepath.Join(root, "send-keys.log")
	script := `#!/bin/sh
set -eu
literal=0
if [ "${1:-}" = "send-keys" ] || [ "${1:-}" = "paste-buffer" ]; then
    for arg in "$@"; do
        if [ "$arg" = "-l" ]; then literal=1; fi
    done
    printf '%s\n' "$*" >> "$NTM_E2E_TMUX_SEND_LOG"
fi
"$NTM_E2E_TMUX_REAL" "$@"
if { [ "${1:-}" = "send-keys" ] && [ "$literal" -eq 1 ]; } || [ "${1:-}" = "paste-buffer" ]; then
    : > "$NTM_E2E_BLOCK_STARTED/$$"
fi
`
	commandPath := filepath.Join(binDir, "tmux")
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write staging tmux wrapper: %v", err)
	}
	pathValue := atomicAssignmentEnvValue(baseEnv, "PATH")
	if pathValue == "" {
		pathValue = os.Getenv("PATH")
	}
	return atomicAssignmentMergeEnv(baseEnv, map[string]string{
		"PATH":                  binDir + string(os.PathListSeparator) + pathValue,
		"NTM_TMUX_BINARY":       commandPath,
		"NTM_E2E_TMUX_REAL":     realPath,
		"NTM_E2E_TMUX_SEND_LOG": logPath,
		"NTM_E2E_BLOCK_STARTED": startedDir,
	}), startedDir, logPath
}

func assertSpawnTimelineExcludesMarker(t *testing.T, timelineDir, marker string) {
	t.Helper()
	err := filepath.WalkDir(timelineDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(marker)) {
			return fmt.Errorf("timeline %s contains canceled prompt marker", path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}

func runSpawnSignalCanceledCLI(t *testing.T, ntmPath, dir string, env []string, startedDir string, wantStarted int, sig syscall.Signal, args ...string) spawnSignalProcessResult {
	t.Helper()
	cmd := exec.Command(ntmPath, args...)
	cmd.Dir = dir
	cmd.Env = append([]string(nil), env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start signal cancellation command %q: %v", args, err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waitForSpawnSignalStarts(t, cmd.Process, done, startedDir, wantStarted, 20*time.Second, args, &stdout, &stderr)
	if err := cmd.Process.Signal(sig); err != nil {
		t.Fatalf("signal cancellation command %q with %s: %v", args, sig, err)
	}
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Signal(syscall.SIGQUIT)
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		t.Fatalf("signal cancellation command did not join after %s: args=%q stdout=%s stderr=%s", sig, args, stdout.String(), stderr.String())
	}
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("wait for signal cancellation command %q: %v", args, waitErr)
		}
		exitCode = exitErr.ExitCode()
	}
	return spawnSignalProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func waitForSpawnSignalStarts(t *testing.T, process *os.Process, done <-chan error, startedDir string, want int, timeout time.Duration, args []string, stdout, stderr *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		entries, err := os.ReadDir(startedDir)
		if err == nil && len(entries) >= want {
			return
		}
		select {
		case waitErr := <-done:
			t.Fatalf("signal cancellation command exited before blocked workers started: args=%q markers=%d want=%d wait_err=%v stdout=%s stderr=%s", args, len(entries), want, waitErr, stdout.String(), stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			_ = process.Signal(syscall.SIGQUIT)
			select {
			case <-done:
			case <-time.After(time.Second):
				_ = process.Kill()
				<-done
			}
			t.Fatalf("signal cancellation command did not start %d blocked worker(s): args=%q markers=%d read_err=%v stdout=%s stderr=%s", want, args, len(entries), err, stdout.String(), stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func assertSpawnSignalJSONFailure(t *testing.T, result spawnSignalProcessResult, wantErrorCode string) map[string]any {
	t.Helper()
	if result.exitCode != 1 {
		t.Fatalf("canceled CLI exit=%d, want 1; stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	if trimmed := bytes.TrimSpace(result.stderr); len(trimmed) != 0 {
		t.Fatalf("canceled JSON CLI wrote stderr=%s", trimmed)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("decode canceled CLI JSON: %v raw=%s", err, result.stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("canceled CLI emitted more than one JSON document: err=%v extra=%v raw=%s", err, extra, result.stdout)
	}
	if success, ok := document["success"].(bool); !ok || success {
		t.Fatalf("canceled CLI success=%v, want false: %s", document["success"], result.stdout)
	}
	if wantErrorCode != "" {
		gotErrorCode := document["error_code"]
		if gotErrorCode == nil {
			if structured, ok := document["error"].(map[string]any); ok {
				gotErrorCode = structured["code"]
			}
		}
		if gotErrorCode != wantErrorCode {
			t.Fatalf("canceled CLI error_code=%v, want %s: %s", gotErrorCode, wantErrorCode, result.stdout)
		}
	}
	return document
}

func assertSpawnSignalCodedJSONError(t *testing.T, result spawnSignalProcessResult, wantCode string) {
	t.Helper()
	if result.exitCode != 1 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("canceled coded CLI exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	decoder := json.NewDecoder(bytes.NewReader(result.stdout))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("decode canceled coded JSON: %v raw=%s", err, result.stdout)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("canceled coded CLI emitted multiple JSON documents: %v raw=%s", err, result.stdout)
	}
	if document["code"] != wantCode || strings.TrimSpace(fmt.Sprint(document["error"])) == "" {
		t.Fatalf("canceled coded CLI document=%v, want code=%s", document, wantCode)
	}
}

func assertSpawnStagedWithoutEnter(t *testing.T, logPath string) {
	t.Helper()
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read staged tmux send log: %v", err)
	}
	logText := string(logData)
	staged := strings.Contains(logText, "send-keys") || strings.Contains(logText, "paste-buffer")
	if !staged || strings.Contains(logText, " Enter") {
		t.Fatalf("staged tmux calls=%q, want prompt staging and no Enter", logText)
	}
}

func assertAtomicSignalCancellationNoActuation(t *testing.T, fixture *atomicAssignmentCLIFixture, beadID string) {
	t.Helper()
	ledger, err := fixture.readLedger()
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read atomic cancellation ledger: %v", err)
	}
	record := ledger.Assignments[beadID]
	if record == nil {
		return
	}
	if record.ClaimState == "claimed" || record.ClaimedAt != nil || record.ReservationAttempts != 0 ||
		record.ReservationCompleted || record.DispatchAttempts != 0 || record.DispatchedAt != nil ||
		record.DispatchReceiptID != "" || record.PromptSent != "" {
		t.Fatalf("canceled atomic assignment crossed an actuation boundary: %+v", record)
	}
}

func assertSpawnSignalCancellationNoActuation(t *testing.T, fixture *spawnAssignmentCLIFixture) {
	t.Helper()
	path := filepath.Join(fixture.homeDir, ".ntm", "sessions", fixture.session, "assignments.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read spawn cancellation ledger: %v", err)
	}
	var ledger spawnAssignmentLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode spawn cancellation ledger: %v raw=%s", err, data)
	}
	record := ledger.Assignments[fixture.beadID]
	if record == nil {
		return
	}
	if record.ClaimState == "claimed" || record.ClaimedAt != nil || record.ReservationAttempts != 0 ||
		record.ReservationCompleted || record.DispatchAttempts != 0 || record.DispatchedAt != nil ||
		record.DispatchReceiptID != "" || record.PromptSent != "" {
		t.Fatalf("canceled spawn assignment crossed an actuation boundary: %+v", record)
	}
}

type spawnAssignmentCLIFixture struct {
	ntmPath        string
	tmuxPath       string
	brPath         string
	session        string
	projectDir     string
	homeDir        string
	configDir      string
	env            []string
	paneID         string
	paneIndex      int
	beadID         string
	beadTitle      string
	marker         string
	expectedPrompt string
	stub           *spawnAssignmentMCPStub
}

type spawnAssignmentProcessResult struct {
	stdout   []byte
	stderr   []byte
	exitCode int
}

type spawnPromptCLIOutput struct {
	Spawn  spawnResponse   `json:"spawn"`
	Init   spawnPromptInit `json:"init"`
	Assign json.RawMessage `json:"assign"`
}

type spawnPromptInit struct {
	PromptSent    bool                 `json:"prompt_sent"`
	AgentsReached int                  `json:"agents_reached"`
	Receipts      []spawnPromptReceipt `json:"receipts"`
}

type spawnPromptReceipt struct {
	Target struct {
		Ref struct {
			PaneID      string `json:"pane_id"`
			WindowIndex int    `json:"window_index"`
			PaneIndex   int    `json:"pane_index"`
		} `json:"ref"`
		Address   string `json:"address"`
		AgentType string `json:"agent_type"`
	} `json:"target"`
	Status    string `json:"status"`
	Protocol  string `json:"protocol"`
	Redaction struct {
		Mode     string `json:"mode"`
		Findings int    `json:"findings"`
	} `json:"redaction"`
}

type spawnAssignmentOutput struct {
	Success        bool                      `json:"success"`
	Timestamp      string                    `json:"timestamp"`
	Session        string                    `json:"session"`
	WorkingDir     string                    `json:"working_dir"`
	Agents         []spawnAssignmentAgent    `json:"agents"`
	Mode           string                    `json:"mode"`
	AssignStrategy string                    `json:"assign_strategy"`
	Assignments    []spawnAssignmentResponse `json:"assignments"`
	Error          string                    `json:"error"`
	ErrorCode      string                    `json:"error_code"`
}

type spawnAssignmentAgent struct {
	Pane  string `json:"pane"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Title string `json:"title"`
	Ready bool   `json:"ready"`
	Error string `json:"error"`
}

type spawnAssignmentResponse struct {
	Pane              string `json:"pane"`
	AgentType         string `json:"agent_type"`
	BeadID            string `json:"bead_id"`
	BeadTitle         string `json:"bead_title"`
	Priority          string `json:"priority"`
	Claimed           bool   `json:"claimed"`
	PromptSent        bool   `json:"prompt_sent"`
	ClaimActor        string `json:"claim_actor"`
	IdempotencyKey    string `json:"idempotency_key"`
	DispatchReceiptID string `json:"dispatch_receipt_id"`
	ReservationIDs    []int  `json:"reservation_ids"`
	ClaimError        string `json:"claim_error"`
	PromptError       string `json:"prompt_error"`
}

type spawnAssignmentLedger struct {
	SessionName string                            `json:"session_name"`
	Assignments map[string]*spawnAssignmentRecord `json:"assignments"`
	Version     int                               `json:"version"`
}

type spawnAssignmentRecord struct {
	BeadID                string     `json:"bead_id"`
	BeadTitle             string     `json:"bead_title"`
	Pane                  int        `json:"pane"`
	AgentType             string     `json:"agent_type"`
	AgentName             string     `json:"agent_name"`
	Status                string     `json:"status"`
	PromptSent            string     `json:"prompt_sent"`
	IdempotencyKey        string     `json:"idempotency_key"`
	ClaimActor            string     `json:"claim_actor"`
	ClaimState            string     `json:"claim_state"`
	ClaimStatus           string     `json:"claim_status"`
	ClaimAttempts         int        `json:"claim_attempts"`
	ClaimedAt             *time.Time `json:"claimed_at"`
	ReservationRequired   bool       `json:"reservation_required"`
	ReservationInputPaths []string   `json:"reservation_input_paths"`
	ReservationState      string     `json:"reservation_state"`
	ReservationAttempts   int        `json:"reservation_attempts"`
	ReservationCompleted  bool       `json:"reservation_completed"`
	ReservationAgent      string     `json:"reservation_agent"`
	ReservationTarget     string     `json:"reservation_target"`
	ReservationRequested  []string   `json:"reservation_requested"`
	ReservedPaths         []string   `json:"reserved_paths"`
	ReservationIDs        []int      `json:"reservation_ids"`
	ReservationExpiresAt  *time.Time `json:"reservation_expires_at"`
	ReservationError      string     `json:"reservation_error"`
	DispatchState         string     `json:"dispatch_state"`
	DispatchTarget        string     `json:"dispatch_target"`
	OccupancyKey          string     `json:"occupancy_key"`
	PendingPrompt         string     `json:"pending_prompt"`
	DispatchAttempts      int        `json:"dispatch_attempts"`
	DispatchStartedAt     *time.Time `json:"dispatch_started_at"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	DispatchReceiptID     string     `json:"dispatch_receipt_id"`
}

type spawnAssignmentBead struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
}

func newSpawnAssignmentCLIFixture(t *testing.T) *spawnAssignmentCLIFixture {
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
		t.Skipf("br is required for spawn assignment E2E: %v", err)
	}

	root := t.TempDir()
	tmuxRoot := filepath.Join("/tmp", fmt.Sprintf("ntm-spawn-tmux-%d-%d", os.Getpid(), time.Now().UnixNano()))
	fixture := &spawnAssignmentCLIFixture{
		ntmPath:    ntmPath,
		tmuxPath:   tmuxPath,
		brPath:     brPath,
		session:    fmt.Sprintf("ntm-e2e-spawn-assign-%d-%d", os.Getpid(), time.Now().UnixNano()),
		projectDir: filepath.Join(root, "project"),
		homeDir:    filepath.Join(root, "home"),
		configDir:  filepath.Join(root, "config"),
		marker:     fmt.Sprintf("NTM_SPAWN_ASSIGN_%d", time.Now().UnixNano()),
	}
	fixture.beadTitle = fixture.marker

	fakeBin := filepath.Join(root, "bin")
	for _, dir := range []string{
		fixture.projectDir,
		fixture.homeDir,
		fixture.configDir,
		filepath.Join(root, "data"),
		tmuxRoot,
		fakeBin,
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}

	fixture.env = spawnAssignmentIsolatedEnv(map[string]string{
		"HOME":                fixture.homeDir,
		"XDG_CONFIG_HOME":     fixture.configDir,
		"XDG_DATA_HOME":       filepath.Join(root, "data"),
		"TMUX_TMPDIR":         tmuxRoot,
		"PATH":                fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"AGENT_MAIL_URL":      "http://127.0.0.1:1/mcp/",
		"AGENT_MAIL_TOKEN":    "",
		"HTTP_PROXY":          "",
		"HTTPS_PROXY":         "",
		"ALL_PROXY":           "",
		"NO_PROXY":            "127.0.0.1,localhost",
		"NO_COLOR":            "1",
		"TERM":                "xterm-256color",
		"NTM_CONFIG":          "",
		"NTM_OUTPUT_FORMAT":   "",
		"NTM_ROBOT_FORMAT":    "",
		"TOON_DEFAULT_FORMAT": "",
	})

	writeSpawnFakeClaude(t, filepath.Join(fakeBin, "claude"))
	fixture.mustBR(t, "init", "--prefix=spawne2e", "--json")
	fixture.beadID = strings.TrimSpace(string(fixture.mustBR(
		t, "create", fixture.beadTitle, "--type=task", "--priority=1", "--silent",
	)))
	if fixture.beadID == "" || strings.ContainsAny(fixture.beadID, " \t\r\n") {
		t.Fatalf("unexpected br create output %q", fixture.beadID)
	}
	fixture.assertBead(t, "open", "")
	writeSpawnFakeBV(t, filepath.Join(fakeBin, "bv"), fixture.beadID, fixture.beadTitle)

	fixture.stub = &spawnAssignmentMCPStub{
		projectDir: fixture.projectDir,
		beadID:     fixture.beadID,
		recipient:  spawnAssignmentRecipient,
		path:       spawnAssignmentPath,
		token:      spawnAssignmentToken,
	}
	server := httptest.NewUnstartedServer(fixture.stub)
	server.Config.ReadHeaderTimeout = 2 * time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 2 * time.Second
	server.Config.IdleTimeout = 2 * time.Second
	server.Start()
	t.Cleanup(server.Close)
	fixture.env = spawnAssignmentMergeEnv(fixture.env, map[string]string{
		"AGENT_MAIL_URL": server.URL + "/mcp/",
	})

	tmuxConfig := filepath.Join(root, "tmux.conf")
	tmuxConfigBody := strings.Join([]string{
		"set -g base-index 0",
		"setw -g pane-base-index 0",
		"set -g renumber-windows off",
		"set -g status off",
		"setw -g allow-rename off",
		"setw -g automatic-rename off",
		"",
	}, "\n")
	if err := os.WriteFile(tmuxConfig, []byte(tmuxConfigBody), 0o600); err != nil {
		t.Fatalf("write tmux config: %v", err)
	}
	fixture.mustTMUX(t, "-f", tmuxConfig, "new-session", "-d", "-s", fixture.session,
		"-x", "160", "-y", "48", "-c", fixture.projectDir, "bash --noprofile --norc")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = fixture.runTMUX(ctx, "kill-server")
	})
	fixture.waitForInitialPane(t)
	fixture.seedAgentRegistry(t)
	fixture.expectedPrompt = expectedSpawnWorkPrompt(fixture.beadID, fixture.beadTitle)

	return fixture
}

func (f *spawnAssignmentCLIFixture) spawnArgs() []string {
	return []string{
		"--robot-format=json",
		"--robot-spawn=" + f.session,
		"--spawn-cc=1",
		"--spawn-no-user",
		"--spawn-dir=" + f.projectDir,
		"--spawn-wait",
		"--timeout=8s",
		"--spawn-assign-work",
		"--strategy=top-n",
		"--spawn-names=" + spawnAssignmentDisplayName,
		"--require-reservation",
		"--reservation-paths=" + spawnAssignmentPath,
	}
}

func (f *spawnAssignmentCLIFixture) runNTM(t *testing.T, args ...string) spawnAssignmentProcessResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.ntmPath, args...)
	cmd.Dir = f.projectDir
	cmd.Env = append([]string(nil), f.env...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("ntm spawn assignment timed out: %q", args)
	}
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run ntm spawn assignment: %v", err)
		}
	}
	t.Logf("[E2E-SPAWN-ASSIGN] exit=%d stdout=%s stderr=%s", exitCode,
		truncateString(stdout.String(), 800), truncateString(stderr.String(), 800))
	return spawnAssignmentProcessResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exitCode: exitCode}
}

func decodeSpawnAssignmentOutput(t *testing.T, result spawnAssignmentProcessResult) spawnAssignmentOutput {
	t.Helper()
	if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
		t.Fatalf("spawn assignment exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
	}
	var output spawnAssignmentOutput
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(result.stdout)))
	if err := decoder.Decode(&output); err != nil {
		t.Fatalf("decode spawn JSON: %v raw=%s", err, result.stdout)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("spawn output contains trailing data: err=%v raw=%s", err, result.stdout)
	}
	return output
}

func assertSpawnAssignmentOutput(t *testing.T, output spawnAssignmentOutput, f *spawnAssignmentCLIFixture) spawnAssignmentResponse {
	t.Helper()
	if !output.Success || output.Timestamp == "" || output.Session != f.session ||
		output.WorkingDir != f.projectDir || output.Mode != "orchestrator" ||
		output.AssignStrategy != "top-n" || output.Error != "" || output.ErrorCode != "" {
		t.Fatalf("spawn output envelope = %+v", output)
	}
	if len(output.Agents) != 1 {
		t.Fatalf("spawn agents = %+v", output.Agents)
	}
	agent := output.Agents[0]
	wantTitle := f.session + "__cc_1"
	if agent.Pane != fmt.Sprintf("0.%d", f.paneIndex) || agent.Name != spawnAssignmentDisplayName ||
		agent.Type != "claude" || agent.Title != wantTitle || !agent.Ready || agent.Error != "" {
		t.Fatalf("spawn agent = %+v", agent)
	}
	if len(output.Assignments) != 1 {
		t.Fatalf("spawn assignments = %+v", output.Assignments)
	}
	assignment := output.Assignments[0]
	if assignment.Pane != fmt.Sprint(f.paneIndex) || assignment.AgentType != "claude" ||
		assignment.BeadID != f.beadID || assignment.BeadTitle != f.beadTitle || assignment.Priority != "P1" ||
		!assignment.Claimed || !assignment.PromptSent || assignment.ClaimActor == "" ||
		assignment.IdempotencyKey == "" || assignment.DispatchReceiptID == "" ||
		!equalInts(assignment.ReservationIDs, []int{spawnAssignmentReservationID}) ||
		assignment.ClaimError != "" || assignment.PromptError != "" {
		t.Fatalf("spawn assignment = %+v", assignment)
	}
	decodedKey, err := hex.DecodeString(assignment.IdempotencyKey)
	if err != nil || len(decodedKey) != 32 {
		t.Fatalf("idempotency key %q is not 256-bit hex: bytes=%d err=%v", assignment.IdempotencyKey, len(decodedKey), err)
	}
	wantActor := spawnAssignmentRecipient + "/ntm-" + assignment.IdempotencyKey[:12]
	if assignment.ClaimActor != wantActor {
		t.Fatalf("claim actor = %q, want exact registered identity %q", assignment.ClaimActor, wantActor)
	}
	if !strings.Contains(assignment.DispatchReceiptID, f.paneID) {
		t.Fatalf("dispatch receipt %q does not identify stable pane %q", assignment.DispatchReceiptID, f.paneID)
	}
	return assignment
}

func assertSpawnAssignmentRecord(t *testing.T, record *spawnAssignmentRecord, response spawnAssignmentResponse, f *spawnAssignmentCLIFixture) {
	t.Helper()
	if record.BeadID != f.beadID || record.BeadTitle != f.beadTitle || record.Pane != f.paneIndex ||
		record.AgentType != "claude" || record.AgentName != spawnAssignmentRecipient || record.Status != "assigned" ||
		record.PromptSent != f.expectedPrompt || record.IdempotencyKey != response.IdempotencyKey ||
		record.ClaimActor != response.ClaimActor || record.ClaimState != "claimed" ||
		record.ClaimStatus != "in_progress" || record.ClaimAttempts != 1 || record.ClaimedAt == nil {
		t.Fatalf("durable claim identity/state = %+v", record)
	}
	if !record.ReservationRequired || !equalStrings(record.ReservationInputPaths, []string{spawnAssignmentPath}) ||
		record.ReservationState != "reserved" || record.ReservationAttempts != 1 || !record.ReservationCompleted ||
		record.ReservationAgent != spawnAssignmentRecipient || record.ReservationTarget != f.paneID ||
		!equalStrings(record.ReservationRequested, []string{spawnAssignmentPath}) ||
		!equalStrings(record.ReservedPaths, []string{spawnAssignmentPath}) ||
		!equalInts(record.ReservationIDs, []int{spawnAssignmentReservationID}) ||
		record.ReservationExpiresAt == nil || !record.ReservationExpiresAt.After(time.Now()) || record.ReservationError != "" {
		t.Fatalf("durable reservation receipt = %+v", record)
	}
	if record.DispatchState != "sent" || record.DispatchTarget != f.paneID || record.OccupancyKey != f.paneID ||
		record.PendingPrompt != "" || record.DispatchAttempts != 1 || record.DispatchStartedAt == nil ||
		record.DispatchedAt == nil || record.DispatchReceiptID != response.DispatchReceiptID {
		t.Fatalf("durable dispatch receipt = %+v", record)
	}
	if record.ClaimedAt.After(*record.DispatchStartedAt) || record.DispatchStartedAt.After(*record.DispatchedAt) {
		t.Fatalf("claim-reserve-dispatch order violated: claim=%s dispatch-start=%s dispatched=%s",
			record.ClaimedAt, record.DispatchStartedAt, record.DispatchedAt)
	}
}

func (f *spawnAssignmentCLIFixture) seedAgentRegistry(t *testing.T) {
	t.Helper()
	registry := agentmail.NewSessionAgentRegistry(f.session, f.projectDir)
	registry.AddAgent(f.session+"__cc_1", f.paneID, spawnAssignmentRecipient)
	registry.SetRegistrationToken(spawnAssignmentRecipient, spawnAssignmentToken)
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatalf("marshal Agent Mail pane registry: %v", err)
	}
	path := filepath.Join(f.configDir, "ntm", "sessions", f.session,
		agentmail.ProjectSlugFromPath(f.projectDir), "agent_registry.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create Agent Mail registry directory: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write Agent Mail pane registry: %v", err)
	}
}

func (f *spawnAssignmentCLIFixture) waitForInitialPane(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		output, err := f.tmuxOutput(ctx, "list-panes", "-t", f.session,
			"-F", "#{window_index}|#{pane_index}|#{pane_id}")
		cancel()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(output)), "|")
			var window int
			if len(parts) == 3 {
				if _, scanErr := fmt.Sscanf(parts[0]+" "+parts[1], "%d %d", &window, &f.paneIndex); scanErr == nil &&
					window == 0 && strings.HasPrefix(parts[2], "%") {
					f.paneID = parts[2]
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("private tmux session did not expose its initial pane")
}

func (f *spawnAssignmentCLIFixture) waitForMarkerCount(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.markerCount(t) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	f.assertMarkerCount(t, want)
}

func (f *spawnAssignmentCLIFixture) assertMarkerCount(t *testing.T, want int) {
	t.Helper()
	if got := f.markerCount(t); got != want {
		t.Fatalf("work prompt marker count = %d, want %d; pane=%q", got, want, f.capturePane(t))
	}
}

func (f *spawnAssignmentCLIFixture) markerCount(t *testing.T) int {
	t.Helper()
	return strings.Count(f.capturePane(t), f.marker)
}

func (f *spawnAssignmentCLIFixture) sessionMarkerCounts(t *testing.T) map[string]int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := f.tmuxOutput(ctx, "list-panes", "-s", "-t", f.session, "-F", "#{pane_id}")
	if err != nil {
		t.Fatalf("list spawned panes for marker count: %v", err)
	}
	counts := make(map[string]int)
	for _, paneID := range strings.Fields(string(output)) {
		captured, captureErr := f.tmuxOutput(ctx, "capture-pane", "-p", "-t", paneID, "-S", "-2000")
		if captureErr != nil {
			t.Fatalf("capture spawned pane %s for marker count: %v", paneID, captureErr)
		}
		counts[paneID] = strings.Count(string(captured), f.marker)
	}
	return counts
}

func totalSpawnMarkerCount(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

func equalSpawnMarkerCounts(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for paneID, count := range left {
		if right[paneID] != count {
			return false
		}
	}
	return true
}

func (f *spawnAssignmentCLIFixture) capturePane(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := f.tmuxOutput(ctx, "capture-pane", "-p", "-t", f.paneID, "-S", "-2000")
	if err != nil {
		t.Fatalf("capture fake spawned agent: %v", err)
	}
	return string(output)
}

func (f *spawnAssignmentCLIFixture) assertBead(t *testing.T, wantStatus, wantAssignee string) {
	t.Helper()
	output := f.mustBR(t, "show", f.beadID, "--json")
	var rows []spawnAssignmentBead
	if err := json.Unmarshal(output, &rows); err != nil {
		var row spawnAssignmentBead
		if objectErr := json.Unmarshal(output, &row); objectErr != nil {
			t.Fatalf("decode br show: array=%v object=%v raw=%s", err, objectErr, output)
		}
		rows = []spawnAssignmentBead{row}
	}
	if len(rows) != 1 || rows[0].ID != f.beadID || rows[0].Status != wantStatus || rows[0].Assignee != wantAssignee {
		t.Fatalf("bead state = %+v, want id=%s status=%s assignee=%s", rows, f.beadID, wantStatus, wantAssignee)
	}
}

func (f *spawnAssignmentCLIFixture) readAssignment(t *testing.T) *spawnAssignmentRecord {
	t.Helper()
	path := filepath.Join(f.homeDir, ".ntm", "sessions", f.session, "assignments.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spawn assignment ledger: %v", err)
	}
	var ledger spawnAssignmentLedger
	if err := json.Unmarshal(data, &ledger); err != nil {
		t.Fatalf("decode spawn assignment ledger: %v raw=%s", err, data)
	}
	if ledger.SessionName != f.session || ledger.Version < 4 {
		t.Fatalf("spawn assignment ledger header = session:%q version:%d", ledger.SessionName, ledger.Version)
	}
	record := ledger.Assignments[f.beadID]
	if record == nil {
		t.Fatalf("spawn assignment ledger missing %s: %+v", f.beadID, ledger.Assignments)
	}
	return record
}

func (f *spawnAssignmentCLIFixture) mustBR(t *testing.T, args ...string) []byte {
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

func (f *spawnAssignmentCLIFixture) mustTMUX(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.runTMUX(ctx, args...); err != nil {
		t.Fatalf("tmux %s: %v", strings.Join(args, " "), err)
	}
}

func (f *spawnAssignmentCLIFixture) runTMUX(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (f *spawnAssignmentCLIFixture) tmuxOutput(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.tmuxPath, args...)
	cmd.Env = append([]string(nil), f.env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func writeSpawnFakeClaude(t *testing.T, path string) {
	t.Helper()
	content := `#!/bin/sh
stty -echo
print_idle_prompt() {
    printf '\342\227\217 Ready\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n\342\235\257 \n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n'
}
printf 'Claude Code v0.0.0\n'
print_idle_prompt
while IFS= read -r line; do
    printf 'RECEIVED:%s\n' "$line"
    print_idle_prompt
done
`
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake Claude executable: %v", err)
	}
}

func writeSpawnGatedFakeClaude(t *testing.T, path, readyFile string) {
	t.Helper()
	quotedReadyFile := strings.ReplaceAll(readyFile, "'", "'\"'\"'")
	content := fmt.Sprintf(`#!/bin/sh
stty -echo
ready_file='%s'
printf 'WAITING_FOR_E2E_READY\n'
while [ ! -f "$ready_file" ]; do
    sleep 0.05
done
printf '\342\227\217 Ready\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n\342\235\257 \n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n'
while IFS= read -r line; do
    printf 'RECEIVED:%%s\n' "$line"
done
`, quotedReadyFile)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write gated fake Claude executable: %v", err)
	}
}

func writeSpawnLaunchMarkerAgent(t *testing.T, path, markerPath string) {
	t.Helper()
	quotedMarker := strings.ReplaceAll(markerPath, "'", "'\"'\"'")
	content := fmt.Sprintf(`#!/bin/sh
printf 'launched\n' > '%s'
stty -echo
printf 'agent>\n'
while IFS= read -r line; do
    printf 'RECEIVED:%%s\nagent>\n' "$line"
done
`, quotedMarker)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write launch-marker agent executable: %v", err)
	}
}

func writeSpawnFakeBV(t *testing.T, path, beadID, title string) {
	t.Helper()
	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"data_hash":    "spawn-assignment-e2e",
		"triage": map[string]any{
			"meta": map[string]any{
				"version": "e2e", "generated_at": time.Now().UTC().Format(time.RFC3339Nano),
				"phase2_ready": true, "issue_count": 1, "compute_time_ms": 1,
			},
			"quick_ref": map[string]any{
				"open_count": 1, "actionable_count": 1, "blocked_count": 0,
				"in_progress_count": 0, "top_picks": []any{},
			},
			"recommendations": []map[string]any{{
				"id": beadID, "title": title, "type": "task", "status": "ready",
				"priority": 1, "score": 100.0, "action": "claim",
				"reasons": []string{"spawn CLI E2E"},
			}},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fake bv triage: %v", err)
	}
	content := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" != \"--robot-triage\" ]; then\n  echo \"unexpected bv args: $*\" >&2\n  exit 64\nfi\nprintf '%%s\\n' '%s'\n", encoded)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake bv executable: %v", err)
	}
}

func writeSpawnEmptyBV(t *testing.T, path string) {
	t.Helper()
	triage := `{"generated_at":"2026-07-13T00:00:00Z","data_hash":"spawn-prompt-e2e","triage":{"meta":{"version":"e2e","generated_at":"2026-07-13T00:00:00Z","phase2_ready":true,"issue_count":0,"compute_time_ms":1},"quick_ref":{"open_count":0,"actionable_count":0,"blocked_count":0,"in_progress_count":0,"top_picks":[]},"recommendations":[]}}`
	plan := `{"generated_at":"2026-07-13T00:00:00Z","plan":{"tracks":[]}}`
	insights := `{"Bottlenecks":[],"Keystones":[],"Hubs":[],"Authorities":[],"Cycles":[]}`
	content := fmt.Sprintf(`#!/bin/sh
case "$1" in
  --robot-triage) printf '%%s\n' '%s' ;;
  --robot-plan) printf '%%s\n' '%s' ;;
  --robot-insights) printf '%%s\n' '%s' ;;
  *) echo "unexpected bv args: $*" >&2; exit 64 ;;
esac
`, triage, plan, insights)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write empty fake bv executable: %v", err)
	}
}

func expectedSpawnWorkPrompt(beadID, title string) string {
	return fmt.Sprintf("Work on bead %s: %s\n\nUse `br show %s` to see full details.\n"+
		"This bead has been marked as in_progress.\n\nContext:\n- spawn CLI E2E\n\n"+
		"When done, close it with: `br close %s --reason \"Completed\"`", beadID, title, beadID, beadID)
}

func spawnAssignmentIsolatedEnv(overrides map[string]string) []string {
	replaced := map[string]struct{}{
		"HOME": {}, "XDG_CONFIG_HOME": {}, "XDG_DATA_HOME": {}, "XDG_STATE_HOME": {}, "XDG_CACHE_HOME": {},
		"PWD": {}, "OLDPWD": {}, "GIT_DIR": {}, "GIT_WORK_TREE": {}, "BR_DB": {}, "BD_DB": {}, "BEADS_DB": {}, "AGENT_NAME": {},
		"PATH": {},
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

func spawnAssignmentMergeEnv(base []string, overrides map[string]string) []string {
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

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

type spawnAssignmentMCPStub struct {
	mu         sync.Mutex
	projectDir string
	beadID     string
	recipient  string
	path       string
	token      string
	ensure     int
	list       int
	reserve    int
	errors     []string
	// blockAgentsPath makes the agents resource wait for request cancellation
	// after recording a marker. It is used only by the signal-cancellation E2E.
	blockAgentsPath string
	blockAgentsAt   int
}

func (s *spawnAssignmentMCPStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/mcp/" {
		s.failRPC(w, nil, -32600, fmt.Sprintf("unexpected HTTP request %s %s", r.Method, r.URL.Path))
		return
	}
	var request struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	reader := http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(reader).Decode(&request); err != nil {
		s.failRPC(w, nil, -32700, "decode request: "+err.Error())
		return
	}
	if request.JSONRPC != "2.0" {
		s.failRPC(w, request.ID, -32600, "jsonrpc must be 2.0")
		return
	}

	switch request.Method {
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			s.failRPC(w, request.ID, -32602, "decode tool call: "+err.Error())
			return
		}
		s.handleTool(w, request.ID, params.Name, params.Arguments)
	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			s.failRPC(w, request.ID, -32602, "decode resource read: "+err.Error())
			return
		}
		s.handleResource(r.Context(), w, request.ID, params.URI)
	default:
		s.failRPC(w, request.ID, -32601, "unexpected method: "+request.Method)
	}
}

func (s *spawnAssignmentMCPStub) handleTool(w http.ResponseWriter, id any, name string, args map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch name {
	case "health_check":
		s.writeResult(w, id, map[string]any{
			"status":    "ok",
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		})
	case "ensure_project":
		if got, _ := args["human_key"].(string); got != s.projectDir {
			s.failRPCLocked(w, id, -32602, fmt.Sprintf("ensure_project human_key=%q want=%q", got, s.projectDir))
			return
		}
		s.ensure++
		s.writeResult(w, id, map[string]any{
			"id": spawnAssignmentProjectID, "slug": "spawn-assignment-e2e",
			"human_key": s.projectDir, "created_at": time.Now().UTC().Format(time.RFC3339Nano),
		})
	case "fetch_inbox", "list_file_reservations", "list_reservations":
		s.writeResult(w, id, []any{})
	case "file_reservation_paths":
		paths, ok := anyStringSlice(args["paths"])
		ttl, ttlOK := args["ttl_seconds"].(float64)
		if project, _ := args["project_key"].(string); project != s.projectDir ||
			args["agent_name"] != s.recipient || !ok || !equalStrings(paths, []string{s.path}) ||
			args["exclusive"] != true || !ttlOK || int(ttl) != 3600 ||
			args["reason"] != "bead assignment: "+s.beadID || args["registration_token"] != s.token {
			s.failRPCLocked(w, id, -32602, fmt.Sprintf("invalid reservation arguments: %#v", args))
			return
		}
		s.reserve++
		now := time.Now().UTC()
		s.writeResult(w, id, map[string]any{
			"granted": []map[string]any{{
				"id": spawnAssignmentReservationID, "path_pattern": s.path,
				"agent_name": s.recipient, "project_id": spawnAssignmentProjectID,
				"exclusive": true, "reason": "bead assignment: " + s.beadID,
				"created_ts": now.Format(time.RFC3339Nano),
				"expires_ts": now.Add(time.Hour).Format(time.RFC3339Nano),
			}},
			"conflicts": []any{},
		})
	default:
		s.failRPCLocked(w, id, -32601, "unexpected tool: "+name)
	}
}

func (s *spawnAssignmentMCPStub) handleResource(ctx context.Context, w http.ResponseWriter, id any, resourceURI string) {
	s.mu.Lock()
	if strings.HasPrefix(resourceURI, "resource://file_reservations/") {
		defer s.mu.Unlock()
		if !strings.Contains(resourceURI, url.QueryEscape(s.projectDir)) && !strings.Contains(resourceURI, url.PathEscape(s.projectDir)) {
			s.failRPCLocked(w, id, -32602, "unexpected reservation resource URI: "+resourceURI)
			return
		}
		s.writeResult(w, id, map[string]any{
			"contents": []map[string]any{{
				"uri": resourceURI, "mimeType": "application/json", "text": "[]",
			}},
		})
		return
	}
	const prefix = "resource://agents/"
	if !strings.HasPrefix(resourceURI, prefix) {
		s.failRPCLocked(w, id, -32602, "unexpected resource URI: "+resourceURI)
		s.mu.Unlock()
		return
	}
	project, err := url.PathUnescape(strings.TrimPrefix(resourceURI, prefix))
	if err != nil || project != s.projectDir {
		s.failRPCLocked(w, id, -32602, fmt.Sprintf("agents project=%q err=%v want=%q", project, err, s.projectDir))
		s.mu.Unlock()
		return
	}
	s.list++
	blockPath := s.blockAgentsPath
	blockAt := s.blockAgentsAt
	listCount := s.list
	s.mu.Unlock()
	if blockPath != "" && (blockAt <= 0 || listCount == blockAt) {
		if err := os.WriteFile(blockPath, []byte("started\n"), 0o600); err != nil {
			s.mu.Lock()
			s.errors = append(s.errors, "write agents block marker: "+err.Error())
			s.mu.Unlock()
			return
		}
		<-ctx.Done()
		return
	}
	agentsJSON, err := json.Marshal(map[string]any{
		"agents": []map[string]any{{
			"id": spawnAssignmentAgentID, "name": s.recipient, "program": "claude-code",
			"model": "e2e", "task_description": "spawn assignment E2E",
			"project_id":     spawnAssignmentProjectID,
			"inception_ts":   time.Now().UTC().Format(time.RFC3339Nano),
			"last_active_ts": time.Now().UTC().Format(time.RFC3339Nano),
		}},
	})
	if err != nil {
		s.failRPC(w, id, -32603, "encode agents resource: "+err.Error())
		return
	}
	s.writeResult(w, id, map[string]any{
		"contents": []map[string]any{{
			"uri": resourceURI, "mimeType": "application/json", "text": string(agentsJSON),
		}},
	})
}

func (s *spawnAssignmentMCPStub) writeResult(w http.ResponseWriter, id, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *spawnAssignmentMCPStub) failRPC(w http.ResponseWriter, id any, code int, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRPCLocked(w, id, code, message)
}

func (s *spawnAssignmentMCPStub) failRPCLocked(w http.ResponseWriter, id any, code int, message string) {
	s.errors = append(s.errors, message)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

type spawnAssignmentMCPCounts struct {
	ensure  int
	list    int
	reserve int
}

func (s *spawnAssignmentMCPStub) assertCleanMutationCount(t *testing.T, reserve int) spawnAssignmentMCPCounts {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) != 0 || s.ensure == 0 || s.list == 0 || s.reserve != reserve {
		t.Fatalf("Agent Mail MCP calls ensure/list/reserve=%d/%d/%d want discovery>0/reserve=%d errors=%v",
			s.ensure, s.list, s.reserve, reserve, s.errors)
	}
	return spawnAssignmentMCPCounts{ensure: s.ensure, list: s.list, reserve: s.reserve}
}

func anyStringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		result = append(result, text)
	}
	return result, true
}
