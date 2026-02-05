package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

type lockResult struct {
	Success bool   `json:"success"`
	Session string `json:"session"`
	Agent   string `json:"agent"`
	Granted []struct {
		PathPattern string `json:"path_pattern"`
		AgentName   string `json:"agent_name"`
	} `json:"granted,omitempty"`
	Conflicts []struct {
		Path    string   `json:"path"`
		Holders []string `json:"holders"`
	} `json:"conflicts,omitempty"`
	Error string `json:"error,omitempty"`
}

type unlockResult struct {
	Success  bool   `json:"success"`
	Session  string `json:"session"`
	Agent    string `json:"agent"`
	Released int    `json:"released"`
	Error    string `json:"error,omitempty"`
}

func TestE2ELockUnlockFileReservations(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)
	client := requireAgentMail(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	logger.LogSection("setup")

	// Isolate session agent files from the developer machine.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectDir := t.TempDir()
	logger.Log("project_dir=%s", projectDir)
	conflictPath := filepath.Join(projectDir, "internal", "cli", "send.go")
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0755); err != nil {
		t.Fatalf("mkdir lock path dir: %v", err)
	}
	if err := os.WriteFile(conflictPath, []byte("package cli\n"), 0644); err != nil {
		t.Fatalf("write lock path: %v", err)
	}
	otherPath := filepath.Join(projectDir, "internal", "cli", "other.go")
	if err := os.WriteFile(otherPath, []byte("package cli\n"), 0644); err != nil {
		t.Fatalf("write other path: %v", err)
	}
	patternConflict := filepath.ToSlash("internal/cli/send.go")
	patternOther := filepath.ToSlash("internal/cli/other.go")
	logger.Log("pattern_conflict=%s", patternConflict)
	logger.Log("pattern_other=%s", patternOther)

	sessionA := "lock_unlock_a"
	sessionB := "lock_unlock_b"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	infoA, err := client.RegisterSessionAgent(ctx, sessionA, projectDir)
	cancel()
	if err != nil {
		t.Fatalf("register session A agent: %v", err)
	}
	if infoA == nil || infoA.AgentName == "" {
		t.Fatalf("register session A agent: missing agent name")
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	infoB, errB := client.RegisterSessionAgent(ctx, sessionB, projectDir)
	cancel()
	if errB != nil {
		t.Fatalf("register session B agent: %v", errB)
	}
	if infoB == nil || infoB.AgentName == "" {
		t.Fatalf("register session B agent: missing agent name")
	}

	logger.Log("sessionA=%s agentA=%s", sessionA, infoA.AgentName)
	logger.Log("sessionB=%s agentB=%s", sessionB, infoB.AgentName)

	t.Cleanup(func() {
		// Best-effort cleanup so reservations don't leak into later tests/runs.
		_, _ = runCmdAllowFail(t, projectDir, "ntm", "--json", "unlock", sessionA, "--all")
		_, _ = runCmdAllowFail(t, projectDir, "ntm", "--json", "unlock", sessionB, "--all")
	})

	logger.LogSection("lock session A")
	out := runCmd(t, projectDir, "ntm", "--json", "lock", sessionA, patternConflict, "--ttl", "1h", "--reason", "e2e lock/unlock")
	logger.Log("expected: lock succeeds for sessionA")
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var lockA lockResult
	if err := json.Unmarshal(out, &lockA); err != nil {
		t.Fatalf("unmarshal lockA: %v\nout=%s", err, string(out))
	}
	if !lockA.Success {
		t.Fatalf("expected lockA.success=true, got false (error=%q)", lockA.Error)
	}
	if lockA.Agent == "" {
		t.Fatalf("expected lockA.agent to be set")
	}
	if lockA.Agent != infoA.AgentName {
		t.Fatalf("expected lockA.agent=%q, got %q", infoA.AgentName, lockA.Agent)
	}

	logger.LogSection("verify reservation exists")
	reservations := listReservations(t, client, projectDir)
	logger.Log("expected: reservation present for %s", patternConflict)
	logger.Log("actual: count=%d", len(reservations))
	if !hasReservation(reservations, patternConflict) {
		t.Fatalf("expected reservation for %s to exist", patternConflict)
	}

	logger.LogSection("lock session B (conflict expected)")
	out = runCmd(t, projectDir, "ntm", "--json", "lock", sessionB, patternConflict, "--ttl", "1h", "--reason", "e2e conflict")
	logger.Log("expected: lock conflicts for sessionB; holders include %s", infoA.AgentName)
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var lockB lockResult
	if err := json.Unmarshal(out, &lockB); err != nil {
		t.Fatalf("unmarshal lockB: %v\nout=%s", err, string(out))
	}
	if lockB.Success {
		t.Fatalf("expected lockB.success=false due to conflict, got true")
	}
	if len(lockB.Conflicts) == 0 {
		t.Fatalf("expected lockB.conflicts to be non-empty")
	}
	conflictFound := false
	holderFound := false
	for _, c := range lockB.Conflicts {
		if c.Path == patternConflict {
			conflictFound = true
			for _, holder := range c.Holders {
				if holder == infoA.AgentName {
					holderFound = true
				}
			}
		}
	}
	if !conflictFound {
		t.Fatalf("expected conflict entry for %s, got %+v", patternConflict, lockB.Conflicts)
	}
	if !holderFound {
		t.Fatalf("expected conflict holders to include %q, got %+v", infoA.AgentName, lockB.Conflicts)
	}

	logger.LogSection("lock session B (non-conflicting path)")
	out = runCmd(t, projectDir, "ntm", "--json", "lock", sessionB, patternOther, "--ttl", "1h", "--reason", "e2e non-conflict")
	logger.Log("expected: lock succeeds for sessionB on %s", patternOther)
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var lockBOther lockResult
	if err := json.Unmarshal(out, &lockBOther); err != nil {
		t.Fatalf("unmarshal lockBOther: %v\nout=%s", err, string(out))
	}
	if !lockBOther.Success {
		t.Fatalf("expected lockBOther.success=true, got false (error=%q)", lockBOther.Error)
	}
	if lockBOther.Agent != infoB.AgentName {
		t.Fatalf("expected lockBOther.agent=%q, got %q", infoB.AgentName, lockBOther.Agent)
	}

	logger.LogSection("verify reservations exist")
	reservations = listReservations(t, client, projectDir)
	logger.Log("expected: agentA holds %s; agentB holds %s", patternConflict, patternOther)
	logger.Log("actual: count=%d", len(reservations))

	hasAConflict := false
	hasBConflict := false
	hasBOther := false
	for _, r := range reservations {
		if r.ReleasedTS != nil {
			continue
		}
		switch r.PathPattern {
		case patternConflict:
			if r.AgentName == infoA.AgentName {
				hasAConflict = true
			}
			if r.AgentName == infoB.AgentName {
				hasBConflict = true
			}
		case patternOther:
			if r.AgentName == infoB.AgentName {
				hasBOther = true
			}
		}
	}
	if !hasAConflict || !hasBOther {
		t.Fatalf("expected agentA to hold %s and agentB to hold %s (hasAConflict=%v hasBOther=%v)", patternConflict, patternOther, hasAConflict, hasBOther)
	}

	logger.LogSection("unlock session A")
	out = runCmd(t, projectDir, "ntm", "--json", "unlock", sessionA, patternConflict)
	logger.Log("expected: unlock succeeds for sessionA")
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var unlockA unlockResult
	if err := json.Unmarshal(out, &unlockA); err != nil {
		t.Fatalf("unmarshal unlockA: %v\nout=%s", err, string(out))
	}
	if !unlockA.Success {
		t.Fatalf("expected unlockA.success=true, got false (error=%q)", unlockA.Error)
	}
	if unlockA.Agent != infoA.AgentName {
		t.Fatalf("expected unlockA.agent=%q, got %q", infoA.AgentName, unlockA.Agent)
	}
	if unlockA.Released != 1 {
		t.Fatalf("expected unlockA.released=1, got %d", unlockA.Released)
	}

	logger.LogSection("verify reservation released")
	reservations = listReservations(t, client, projectDir)
	logger.Log("expected: agentA no longer holds %s; agentB still holds %s", patternConflict, patternOther)
	logger.Log("actual: count=%d", len(reservations))

	hasAConflict = false
	hasBConflictAfter := false
	hasBOther = false
	for _, r := range reservations {
		if r.ReleasedTS != nil {
			continue
		}
		switch r.PathPattern {
		case patternConflict:
			if r.AgentName == infoA.AgentName {
				hasAConflict = true
			}
			if r.AgentName == infoB.AgentName {
				hasBConflictAfter = true
			}
		case patternOther:
			if r.AgentName == infoB.AgentName {
				hasBOther = true
			}
		}
	}
	if hasAConflict {
		t.Fatalf("expected agentA conflict reservation to be released for %s", patternConflict)
	}
	if !hasBOther {
		t.Fatalf("expected agentB to still hold %s", patternOther)
	}
	if hasBConflict && !hasBConflictAfter {
		t.Fatalf("expected agentB conflict reservation (if granted) to remain for %s", patternConflict)
	}

	logger.LogSection("unlock session B (cleanup)")
	out = runCmd(t, projectDir, "ntm", "--json", "unlock", sessionB, "--all")
	logger.Log("expected: unlock succeeds for sessionB")
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var unlockB unlockResult
	if err := json.Unmarshal(out, &unlockB); err != nil {
		t.Fatalf("unmarshal unlockB: %v\nout=%s", err, string(out))
	}
	if !unlockB.Success {
		t.Fatalf("expected unlockB.success=true, got false (error=%q)", unlockB.Error)
	}
}

func TestE2ELockRejectsTTLBelowOneMinute(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)
	client := requireAgentMail(t)

	// Isolate session agent files from the developer machine.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectDir := t.TempDir()
	lockPath := filepath.Join(projectDir, "file.txt")
	if err := os.WriteFile(lockPath, []byte("ok\n"), 0644); err != nil {
		t.Fatalf("write lock path: %v", err)
	}

	session := "lock_ttl_invalid"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	info, err := client.RegisterSessionAgent(ctx, session, projectDir)
	cancel()
	if err != nil {
		t.Fatalf("register session agent: %v", err)
	}
	if info == nil || info.AgentName == "" {
		t.Fatalf("register session agent: missing agent name")
	}

	out, cmdErr := runCmdAllowFail(t, projectDir, "ntm", "lock", session, "file.txt", "--ttl", "30s")
	if cmdErr == nil {
		t.Fatalf("expected ntm lock to fail for TTL < 1m, got success; out=%s", string(out))
	}
	if !strings.Contains(string(out), "TTL must be at least 1 minute") {
		t.Fatalf("expected error mentioning min TTL, got:\n%s", string(out))
	}
}

func TestE2EUnlockAllReleasesReservations(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireNTMBinary(t)
	client := requireAgentMail(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	logger.LogSection("setup")

	// Isolate session agent files from the developer machine.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	projectDir := t.TempDir()
	logger.Log("project_dir=%s", projectDir)
	pathOne := filepath.Join(projectDir, "file-one.txt")
	if err := os.WriteFile(pathOne, []byte("one\n"), 0644); err != nil {
		t.Fatalf("write file one: %v", err)
	}
	pathTwo := filepath.Join(projectDir, "file-two.txt")
	if err := os.WriteFile(pathTwo, []byte("two\n"), 0644); err != nil {
		t.Fatalf("write file two: %v", err)
	}

	session := "unlock_all"
	patternOne := "file-one.txt"
	patternTwo := "file-two.txt"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	info, err := client.RegisterSessionAgent(ctx, session, projectDir)
	cancel()
	if err != nil {
		t.Fatalf("register session agent: %v", err)
	}
	if info == nil || info.AgentName == "" {
		t.Fatalf("register session agent: missing agent name")
	}

	t.Cleanup(func() {
		// Best-effort cleanup so reservations don't leak into later tests/runs.
		_, _ = runCmdAllowFail(t, projectDir, "ntm", "--json", "unlock", session, "--all")
	})

	logger.LogSection("lock both patterns")
	runCmd(t, projectDir, "ntm", "--json", "lock", session, patternOne, "--ttl", "1h", "--reason", "e2e unlock --all")
	runCmd(t, projectDir, "ntm", "--json", "lock", session, patternTwo, "--ttl", "1h", "--reason", "e2e unlock --all")

	reservations := listReservations(t, client, projectDir)
	logger.Log("expected: reservations present for %s and %s", patternOne, patternTwo)
	logger.Log("actual: count=%d", len(reservations))
	if !hasReservation(reservations, patternOne) || !hasReservation(reservations, patternTwo) {
		t.Fatalf("expected reservations for %s and %s; got=%+v", patternOne, patternTwo, reservations)
	}

	logger.LogSection("unlock --all")
	out := runCmd(t, projectDir, "ntm", "--json", "unlock", session, "--all")
	logger.Log("expected: unlock --all succeeds for %s", info.AgentName)
	logger.Log("actual: %s", strings.TrimSpace(string(out)))

	var unlock unlockResult
	if err := json.Unmarshal(out, &unlock); err != nil {
		t.Fatalf("unmarshal unlock: %v\nout=%s", err, string(out))
	}
	if !unlock.Success {
		t.Fatalf("expected unlock.success=true, got false (error=%q)", unlock.Error)
	}
	if unlock.Agent != info.AgentName {
		t.Fatalf("expected unlock.agent=%q, got %q", info.AgentName, unlock.Agent)
	}
	if unlock.Released != -1 {
		t.Fatalf("expected unlock.released=-1 for --all, got %d", unlock.Released)
	}

	logger.LogSection("verify reservations released")
	reservations = listReservations(t, client, projectDir)
	logger.Log("expected: reservations absent for %s and %s", patternOne, patternTwo)
	logger.Log("actual: count=%d", len(reservations))
	if hasReservation(reservations, patternOne) || hasReservation(reservations, patternTwo) {
		t.Fatalf("expected reservations to be released; got=%+v", reservations)
	}

	logger.LogSection("re-lock after unlock")
	out = runCmd(t, projectDir, "ntm", "--json", "lock", session, patternOne, "--ttl", "1h")
	logger.Log("expected: lock succeeds after unlock --all")
	logger.Log("actual: %s", strings.TrimSpace(string(out)))
}
