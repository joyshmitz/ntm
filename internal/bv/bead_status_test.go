package bv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	assignmentstore "github.com/Dicklesworthstone/ntm/internal/assignment"
	"github.com/Dicklesworthstone/ntm/internal/sqliteutil"
)

func TestParseBeadStatusOutput_Object(t *testing.T) {
	t.Parallel()

	status, err := parseBeadStatusOutput(`{"id":"bd-123","status":"in_progress"}`)
	if err != nil {
		t.Fatalf("parseBeadStatusOutput returned error: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("status = %q, want %q", status, "in_progress")
	}
}

func TestParseBeadStatusOutput_Array(t *testing.T) {
	t.Parallel()

	status, err := parseBeadStatusOutput(`[{"id":"bd-123","status":"closed"}]`)
	if err != nil {
		t.Fatalf("parseBeadStatusOutput returned error: %v", err)
	}
	if status != "closed" {
		t.Fatalf("status = %q, want %q", status, "closed")
	}
}

func TestParseBeadStatusOutput_MissingStatus(t *testing.T) {
	t.Parallel()

	_, err := parseBeadStatusOutput(`{"id":"bd-123"}`)
	if err == nil {
		t.Fatal("expected error for missing status")
	}
}

func TestParseBeadStatusOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseBeadStatusOutput(`{`)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseBeadClaimOutput(t *testing.T) {
	t.Parallel()

	claim, err := parseBeadClaimOutput(`[{
		"id":"ntm-123",
		"title":"Atomic assignment",
		"status":"in_progress",
		"priority":1,
		"updated_at":"2026-07-11T12:00:00Z"
	}]`)
	if err != nil {
		t.Fatalf("parseBeadClaimOutput: %v", err)
	}
	if claim.ID != "ntm-123" || claim.Status != "in_progress" {
		t.Fatalf("claim=%+v", claim)
	}
	if claim.Title != "Atomic assignment" {
		t.Fatalf("claim title=%q", claim.Title)
	}
}

func TestParseBeadClaimOutputRejectsNonClaimedState(t *testing.T) {
	t.Parallel()

	if _, err := parseBeadClaimOutput(`[{"id":"ntm-123","status":"open"}]`); err == nil {
		t.Fatal("expected non-in_progress claim output to fail")
	}
	if _, err := parseBeadClaimOutput(`[]`); err == nil {
		t.Fatal("expected empty claim output to fail")
	}
}

func TestBeadClaimArgsUseAtomicPrimitive(t *testing.T) {
	t.Parallel()

	want := []string{"update", "ntm-123", "--claim", "--actor", "BlueLake/ntm-key", "--json"}
	if got := beadClaimArgs("ntm-123", "BlueLake/ntm-key"); !reflect.DeepEqual(got, want) {
		t.Fatalf("beadClaimArgs=%q, want %q", got, want)
	}
}

func TestClaimBeadNonTerminalGuardRefusesCloseAfterReopenRead(t *testing.T) {
	requireRealBR(t)
	dir := t.TempDir()
	runRealBR(t, dir, "init", "--quiet")
	beadID := createRealBRBead(t, dir, "guarded reopen race")
	runRealBR(t, dir, "close", beadID, "--reason", "first close", "--json")
	runRealBR(t, dir, "reopen", beadID, "--reason", "explicit reopen", "--json")
	status, err := GetBeadStatus(dir, beadID)
	if err != nil || status != "open" {
		t.Fatalf("advisory reopen status=%q err=%v, want open", status, err)
	}

	// This close is the exact interleaving the old show->claim sequence lost:
	// br --claim would reopen this row. The guarded compare-and-set must not.
	runRealBR(t, dir, "close", beadID, "--reason", "concurrent close", "--json")
	ctx := assignmentstore.WithNonTerminalClaimGuard(context.Background())
	_, claimErr := ClaimBead(ctx, dir, beadID, "GuardedActor")
	if !errors.Is(claimErr, ErrBeadTerminal) || !errors.Is(claimErr, ErrBeadAlreadyClaimed) {
		t.Fatalf("ClaimBead error=%v, want terminal claim refusal", claimErr)
	}
	status, err = GetBeadStatus(dir, beadID)
	if err != nil || status != "closed" {
		t.Fatalf("guarded claim final status=%q err=%v, want closed", status, err)
	}
}

func TestClaimBeadNonTerminalGuardPreservesBeadsMutationInvariants(t *testing.T) {
	requireRealBR(t)
	dir := t.TempDir()
	runRealBR(t, dir, "init", "--quiet")
	const title = "guarded claim hash parity"
	const actor = "GuardedActor"
	ordinaryID := createRealBRBead(t, dir, title)
	guardedID := createRealBRBead(t, dir, title)
	if _, err := ClaimBead(context.Background(), dir, ordinaryID, actor); err != nil {
		t.Fatalf("ordinary ClaimBead: %v", err)
	}
	ctx := assignmentstore.WithNonTerminalClaimGuard(context.Background())
	guarded, err := ClaimBead(ctx, dir, guardedID, actor)
	if err != nil {
		t.Fatalf("guarded ClaimBead: %v", err)
	}
	if guarded.ID != guardedID || guarded.Actor != actor || guarded.Status != "in_progress" {
		t.Fatalf("guarded claim receipt=%+v", guarded)
	}

	databasePath := realBRDatabasePath(t, dir)
	database, err := sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(databasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("open Beads database: %v", err)
	}
	defer database.Close()
	var ordinaryHash, guardedHash, status, assignee string
	if err := database.QueryRow("SELECT content_hash FROM issues WHERE id = ?", ordinaryID).Scan(&ordinaryHash); err != nil {
		t.Fatalf("read ordinary content hash: %v", err)
	}
	if err := database.QueryRow("SELECT content_hash, status, assignee FROM issues WHERE id = ?", guardedID).Scan(&guardedHash, &status, &assignee); err != nil {
		t.Fatalf("read guarded issue: %v", err)
	}
	if ordinaryHash == "" || guardedHash != ordinaryHash {
		tx, txErr := database.BeginTx(t.Context(), nil)
		if txErr != nil {
			t.Fatalf("begin diagnostic read: %v", txErr)
		}
		ordinaryIssue, ordinaryErr := loadGuardedClaimIssue(t.Context(), tx, ordinaryID)
		guardedIssue, guardedErr := loadGuardedClaimIssue(t.Context(), tx, guardedID)
		_ = tx.Rollback()
		t.Fatalf("guarded content hash=%q, want ordinary br hash %q; ordinary=%+v (%v) guarded=%+v (%v)", guardedHash, ordinaryHash, ordinaryIssue, ordinaryErr, guardedIssue, guardedErr)
	}
	if status != "in_progress" || assignee != actor {
		t.Fatalf("guarded durable issue status=%q assignee=%q", status, assignee)
	}
	for eventType, want := range map[string]int{"status_changed": 1, "assignee_changed": 1} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = ?", guardedID, eventType).Scan(&count); err != nil {
			t.Fatalf("count %s events: %v", eventType, err)
		}
		if count != want {
			t.Fatalf("%s event count=%d, want %d", eventType, count, want)
		}
	}
	var dirtyCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM dirty_issues WHERE issue_id = ?", guardedID).Scan(&dirtyCount); err != nil {
		t.Fatalf("count dirty markers: %v", err)
	}
	if dirtyCount != 0 {
		t.Fatalf("guarded claim left %d dirty markers after flush", dirtyCount)
	}

	jsonl, err := os.ReadFile(filepath.Join(dir, ".beads", "issues.jsonl"))
	if err != nil {
		t.Fatalf("read exported issues.jsonl: %v", err)
	}
	var exported map[string]any
	for _, line := range strings.Split(string(jsonl), "\n") {
		if !strings.Contains(line, `"id":"`+guardedID+`"`) {
			continue
		}
		if err := json.Unmarshal([]byte(line), &exported); err != nil {
			t.Fatalf("parse guarded JSONL row: %v", err)
		}
		break
	}
	if exported == nil || exported["status"] != "in_progress" || exported["assignee"] != actor {
		t.Fatalf("guarded JSONL row=%v", exported)
	}
}

func TestReleaseBeadClaimPreservesBeadsMutationInvariants(t *testing.T) {
	requireRealBR(t)
	dir := t.TempDir()
	runRealBR(t, dir, "init", "--quiet")
	const actor = "ReleaseActor"
	const title = "claim release hash parity"
	ordinaryID := createRealBRBead(t, dir, title)
	guardedID := createRealBRBead(t, dir, title)
	if _, err := ClaimBead(context.Background(), dir, ordinaryID, actor); err != nil {
		t.Fatalf("claim ordinary bead: %v", err)
	}
	if _, err := ClaimBead(context.Background(), dir, guardedID, actor); err != nil {
		t.Fatalf("claim guarded bead: %v", err)
	}
	runRealBR(t, dir, "update", ordinaryID, "--status", "open", "--assignee", "", "--actor", actor, "--json")
	released, err := ReleaseBeadClaim(t.Context(), dir, guardedID, actor)
	if err != nil || !released {
		t.Fatalf("ReleaseBeadClaim() released=%v error=%v", released, err)
	}

	databasePath := realBRDatabasePath(t, dir)
	database, err := sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(databasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("open Beads database: %v", err)
	}
	defer database.Close()
	var ordinaryHash, guardedHash, status string
	var assignee sql.NullString
	if err := database.QueryRow("SELECT content_hash FROM issues WHERE id = ?", ordinaryID).Scan(&ordinaryHash); err != nil {
		t.Fatalf("read ordinary released hash: %v", err)
	}
	if err := database.QueryRow("SELECT content_hash, status, assignee FROM issues WHERE id = ?", guardedID).Scan(&guardedHash, &status, &assignee); err != nil {
		t.Fatalf("read guarded released issue: %v", err)
	}
	if ordinaryHash == "" || guardedHash != ordinaryHash {
		t.Fatalf("guarded released hash=%q, want ordinary br hash %q", guardedHash, ordinaryHash)
	}
	if status != "open" || strings.TrimSpace(assignee.String) != "" {
		t.Fatalf("released issue status=%q assignee=%q", status, assignee.String)
	}
	for eventType, want := range map[string]int{"status_changed": 2, "assignee_changed": 2} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM events WHERE issue_id = ? AND event_type = ?", guardedID, eventType).Scan(&count); err != nil {
			t.Fatalf("count %s events: %v", eventType, err)
		}
		if count != want {
			t.Fatalf("%s event count=%d, want %d", eventType, count, want)
		}
	}
	var dirtyCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM dirty_issues WHERE issue_id = ?", guardedID).Scan(&dirtyCount); err != nil {
		t.Fatalf("count released dirty markers: %v", err)
	}
	if dirtyCount != 0 {
		t.Fatalf("released claim left %d dirty markers after flush", dirtyCount)
	}

	jsonl, err := os.ReadFile(filepath.Join(dir, ".beads", "issues.jsonl"))
	if err != nil {
		t.Fatalf("read released issues.jsonl: %v", err)
	}
	var exported map[string]any
	for _, line := range strings.Split(string(jsonl), "\n") {
		if !strings.Contains(line, `"id":"`+guardedID+`"`) {
			continue
		}
		if err := json.Unmarshal([]byte(line), &exported); err != nil {
			t.Fatalf("parse released JSONL row: %v", err)
		}
		break
	}
	assigneeValue, hasAssignee := exported["assignee"]
	if exported == nil || exported["status"] != "open" ||
		(hasAssignee && assigneeValue != nil && strings.TrimSpace(fmt.Sprint(assigneeValue)) != "") {
		t.Fatalf("released JSONL row=%v", exported)
	}
	if released, err := ReleaseBeadClaim(t.Context(), dir, guardedID, actor); err != nil || released {
		t.Fatalf("idempotent ReleaseBeadClaim() released=%v error=%v", released, err)
	}
}

func TestReleaseBeadClaimRetryFinalizesCommittedSQLiteMutation(t *testing.T) {
	requireRealBR(t)
	dir := t.TempDir()
	runRealBR(t, dir, "init", "--quiet")
	const actor = "ReleaseRetryActor"
	beadID := createRealBRBead(t, dir, "claim release retry finalization")
	if _, err := ClaimBead(context.Background(), dir, beadID, actor); err != nil {
		t.Fatalf("claim bead: %v", err)
	}
	databasePath := realBRDatabasePath(t, dir)

	// Simulate a process loss after the SQLite CAS commits but before `br sync`
	// and content-hash repair complete.
	txnResult, err := releaseBeadClaimTransaction(t.Context(), databasePath, beadID, actor)
	if err != nil || !txnResult.Released || !txnResult.NeedsFinalization || txnResult.Status != "open" {
		t.Fatalf("release transaction result=%+v error=%v", txnResult, err)
	}
	database, err := sql.Open(sqliteutil.DriverName, sqliteutil.FileDSN(databasePath, "busy_timeout(5000)", "foreign_keys(ON)"))
	if err != nil {
		t.Fatalf("open Beads database: %v", err)
	}
	defer database.Close()
	var dirty int
	var contentHash sql.NullString
	if err := database.QueryRow("SELECT EXISTS(SELECT 1 FROM dirty_issues WHERE issue_id = ?), content_hash FROM issues WHERE id = ?", beadID, beadID).Scan(&dirty, &contentHash); err != nil {
		t.Fatalf("inspect interrupted release: %v", err)
	}
	if dirty != 1 || contentHash.Valid {
		t.Fatalf("interrupted release dirty=%d content_hash=%q", dirty, contentHash.String)
	}

	released, err := ReleaseBeadClaim(t.Context(), dir, beadID, actor)
	if err != nil || released {
		t.Fatalf("recovery ReleaseBeadClaim() released=%v error=%v", released, err)
	}
	if err := database.QueryRow("SELECT EXISTS(SELECT 1 FROM dirty_issues WHERE issue_id = ?), content_hash FROM issues WHERE id = ?", beadID, beadID).Scan(&dirty, &contentHash); err != nil {
		t.Fatalf("inspect finalized release: %v", err)
	}
	if dirty != 0 || !contentHash.Valid || strings.TrimSpace(contentHash.String) == "" {
		t.Fatalf("finalized release dirty=%d content_hash=%q", dirty, contentHash.String)
	}
	assertRealBRStatusAndAssignee(t, dir, beadID, "open", "")
}

func TestReleaseBeadClaimLeavesDifferentOwnerAndUnassignsTerminalOwner(t *testing.T) {
	requireRealBR(t)
	dir := t.TempDir()
	runRealBR(t, dir, "init", "--quiet")
	const actor = "OriginalActor"

	ownedID := createRealBRBead(t, dir, "different owner release")
	if _, err := ClaimBead(context.Background(), dir, ownedID, actor); err != nil {
		t.Fatalf("claim owned bead: %v", err)
	}
	if released, err := ReleaseBeadClaim(t.Context(), dir, ownedID, "DifferentActor"); err != nil || released {
		t.Fatalf("different-owner release=%v error=%v", released, err)
	}
	assertRealBRStatusAndAssignee(t, dir, ownedID, "in_progress", actor)

	terminalID := createRealBRBead(t, dir, "terminal release")
	if _, err := ClaimBead(context.Background(), dir, terminalID, actor); err != nil {
		t.Fatalf("claim terminal bead: %v", err)
	}
	runRealBR(t, dir, "close", terminalID, "--reason", "terminal release test", "--json")
	if released, err := ReleaseBeadClaim(t.Context(), dir, terminalID, actor); err != nil || !released {
		t.Fatalf("terminal release=%v error=%v", released, err)
	}
	assertRealBRStatusAndAssignee(t, dir, terminalID, "closed", "")
	if released, err := ReleaseBeadClaim(t.Context(), dir, terminalID, actor); err != nil || released {
		t.Fatalf("terminal idempotent release=%v error=%v", released, err)
	}
}

func assertRealBRStatusAndAssignee(t *testing.T, dir, beadID, wantStatus, wantAssignee string) {
	t.Helper()
	output := runRealBR(t, dir, "show", beadID, "--json")
	var rows []struct {
		Status   string `json:"status"`
		Assignee string `json:"assignee"`
	}
	if err := json.Unmarshal(output, &rows); err != nil || len(rows) != 1 {
		t.Fatalf("parse br show for %s: rows=%v error=%v output=%s", beadID, rows, err, output)
	}
	if rows[0].Status != wantStatus || rows[0].Assignee != wantAssignee {
		t.Fatalf("bead %s status=%q assignee=%q, want %q/%q", beadID, rows[0].Status, rows[0].Assignee, wantStatus, wantAssignee)
	}
}

func requireRealBR(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("br"); err != nil {
		t.Skip("br is not installed")
	}
}

func runRealBR(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("br", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("br %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func createRealBRBead(t *testing.T, dir, title string) string {
	t.Helper()
	output := runRealBR(t, dir, "create", "--title", title, "--type", "task", "--priority", "2", "--json")
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &created); err != nil {
		t.Fatalf("parse br create output: %v\n%s", err, output)
	}
	if created.ID == "" {
		t.Fatalf("br create returned no ID: %s", output)
	}
	return created.ID
}

func realBRDatabasePath(t *testing.T, dir string) string {
	t.Helper()
	output := runRealBR(t, dir, "info", "--json", "--no-auto-import", "--no-auto-flush")
	var info beadsWorkspaceInfo
	if err := json.Unmarshal(output, &info); err != nil {
		t.Fatalf("parse br info output: %v\n%s", err, output)
	}
	if info.DatabasePath == "" {
		t.Fatalf("br info returned no database path: %s", output)
	}
	return info.DatabasePath
}
