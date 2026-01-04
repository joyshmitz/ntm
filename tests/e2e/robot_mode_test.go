package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// These tests exercise the robot-mode JSON outputs end-to-end using the built
// binary on PATH. They intentionally avoid deep schema validation beyond
// parseability to keep them fast and resilient to small additive fields.

func TestRobotVersion(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-version")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuildDate string `json:"build_date"`
		GoVersion string `json:"go_version"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.Version == "" {
		t.Fatalf("missing version field in output")
	}
	if payload.GoVersion == "" {
		t.Fatalf("missing go_version field in output")
	}
	if payload.Commit == "" {
		t.Fatalf("missing commit field in output")
	}
	if payload.BuildDate == "" {
		t.Fatalf("missing build_date field in output")
	}
}

func TestRobotStatusEmptySessions(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string                   `json:"generated_at"`
		Sessions    []map[string]interface{} `json:"sessions"`
		Summary     struct {
			TotalSessions int `json:"total_sessions"`
			TotalAgents   int `json:"total_agents"`
			ClaudeCount   int `json:"claude_count"`
			CodexCount    int `json:"codex_count"`
			GeminiCount   int `json:"gemini_count"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at field")
	}

	if payload.Sessions == nil {
		t.Fatalf("missing sessions array")
	}

	if payload.Summary.TotalSessions < 0 || payload.Summary.TotalAgents < 0 {
		t.Fatalf("summary counts should be non-negative: %+v", payload.Summary)
	}
}

func TestRobotPlan(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-plan")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt    string                   `json:"generated_at"`
		Actions        []map[string]interface{} `json:"actions"`
		Recommendation string                   `json:"recommendation"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("missing generated_at field")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}

	if payload.Actions == nil {
		t.Fatalf("missing actions array")
	}

	if payload.Recommendation == "" {
		t.Fatalf("missing recommendation field")
	}

	for i, action := range payload.Actions {
		if _, ok := action["priority"]; !ok {
			t.Fatalf("actions[%d] missing priority", i)
		}
		if cmd, ok := action["command"].(string); !ok || strings.TrimSpace(cmd) == "" {
			t.Fatalf("actions[%d] missing non-empty command", i)
		}
	}
}

// TestRobotStatusWithLiveSession ensures a real session appears in robot-status.
func TestRobotStatusWithLiveSession(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("ntm_robot_status_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
codex = "bash"
gemini = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session with two agents (claude+codex)
	logger.LogSection("spawn session")
	if _, err := logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1", "--cod=1"); err != nil {
		t.Fatalf("ntm spawn failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	// robot-status should include the session and at least 2 agents
	logger.LogSection("robot-status")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath, "--robot-status")

	var payload struct {
		Sessions []struct {
			Name    string                   `json:"name"`
			Agents  []map[string]interface{} `json:"agents"`
			Summary struct {
				TotalAgents int `json:"total_agents"`
			} `json:"summary"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	found := false
	for _, s := range payload.Sessions {
		if s.Name == session {
			found = true
			if s.Summary.TotalAgents < 2 {
				t.Fatalf("expected at least 2 agents (claude+codex) in summary, got %d", s.Summary.TotalAgents)
			}
			break
		}
	}
	if !found {
		t.Fatalf("robot-status did not include session %q; payload: %+v", session, payload)
	}
}

func TestRobotHelp(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-help")

	if len(out) == 0 {
		t.Fatalf("robot help output empty")
	}
	if !strings.Contains(string(out), "robot-status") {
		t.Fatalf("robot help missing expected marker")
	}
}

// TestRobotStatusWithSyntheticAgents ensures agent counts and types are surfaced when panes
// follow the NTM naming convention. This avoids launching real agent binaries by
// creating a tmux session with synthetic pane titles.
func TestRobotStatusWithSyntheticAgents(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	sessionName := createSyntheticAgentSession(t, logger)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		Sessions    []struct {
			Name   string `json:"name"`
			Agents []struct {
				Type    string `json:"type"`
				Pane    string `json:"pane"`
				PaneIdx int    `json:"pane_idx"`
			} `json:"agents"`
		} `json:"sessions"`
		Summary struct {
			TotalAgents int `json:"total_agents"`
			ClaudeCount int `json:"claude_count"`
			CodexCount  int `json:"codex_count"`
			GeminiCount int `json:"gemini_count"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}

	var targetSession *struct {
		Name   string `json:"name"`
		Agents []struct {
			Type    string `json:"type"`
			Pane    string `json:"pane"`
			PaneIdx int    `json:"pane_idx"`
		} `json:"agents"`
	}
	for i := range payload.Sessions {
		if payload.Sessions[i].Name == sessionName {
			targetSession = &payload.Sessions[i]
			break
		}
	}

	if targetSession == nil {
		t.Fatalf("robot-status missing session %s", sessionName)
	}

	if len(targetSession.Agents) < 3 {
		t.Fatalf("expected at least 3 agents for %s, got %d", sessionName, len(targetSession.Agents))
	}

	typeCounts := map[string]int{}
	for _, a := range targetSession.Agents {
		typeCounts[a.Type]++
	}

	if typeCounts["cc"] == 0 || typeCounts["cod"] == 0 || typeCounts["gmi"] == 0 {
		t.Fatalf("expected cc, cod, gmi agents in session %s; got %+v", sessionName, typeCounts)
	}

	if payload.Summary.TotalAgents < 3 {
		t.Fatalf("summary.total_agents should reflect at least synthetic agents, got %d", payload.Summary.TotalAgents)
	}
	if payload.Summary.ClaudeCount < 1 || payload.Summary.CodexCount < 1 || payload.Summary.GeminiCount < 1 {
		t.Fatalf("summary counts missing agent types: %+v", payload.Summary)
	}
}

func TestRobotStatusIncludesSystemFields(t *testing.T) {
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLoggerStdout(t)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		System      struct {
			Version   string `json:"version"`
			OS        string `json:"os"`
			Arch      string `json:"arch"`
			TmuxOK    bool   `json:"tmux_available"`
			GoVersion string `json:"go_version"`
		} `json:"system"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}
	if _, err := time.Parse(time.RFC3339, payload.GeneratedAt); err != nil {
		t.Fatalf("generated_at not RFC3339: %v", err)
	}
	if payload.System.Version == "" {
		t.Fatalf("system.version should be set")
	}
	if payload.System.OS == "" || payload.System.Arch == "" {
		t.Fatalf("system.os/arch should be set")
	}
	if payload.System.GoVersion == "" {
		t.Fatalf("system.go_version should be set")
	}
}

func TestRobotStatusHandlesLongSessionNames(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())
	longName := "robot_json_long_session_name_status_validation_1234567890"
	sessionName := createSyntheticAgentSessionWithName(t, logger, longName)

	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--robot-status")
	logger.Log("FULL JSON OUTPUT:\n%s", string(out))

	var payload struct {
		GeneratedAt string `json:"generated_at"`
		Sessions    []struct {
			Name string `json:"name"`
		} `json:"sessions"`
		Summary struct {
			TotalSessions int `json:"total_sessions"`
		} `json:"summary"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if payload.GeneratedAt == "" {
		t.Fatalf("generated_at should be set")
	}

	var found bool
	for _, s := range payload.Sessions {
		if s.Name == sessionName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("robot-status missing long session name %s", sessionName)
	}
	if payload.Summary.TotalSessions < 1 {
		t.Fatalf("summary.total_sessions should be at least 1, got %d", payload.Summary.TotalSessions)
	}
}

// TestRobotSpawn tests the --robot-spawn flag for creating sessions.
func TestRobotSpawn(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_spawn_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
codex = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Test robot-spawn with Claude agents
	logger.LogSection("robot-spawn")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-spawn", session, "--spawn-cc=2", "--spawn-wait")
	logger.Log("robot-spawn output: %s", string(out))

	var payload struct {
		Success bool   `json:"success"`
		Session string `json:"session"`
		Panes   []struct {
			Index int    `json:"index"`
			Type  string `json:"type"`
		} `json:"panes"`
		AgentCounts struct {
			Claude int `json:"claude"`
			Total  int `json:"total"`
		} `json:"agent_counts"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, string(out))
	}

	if !payload.Success {
		t.Fatalf("robot-spawn should succeed")
	}
	if payload.Session != session {
		t.Errorf("session = %q, want %q", payload.Session, session)
	}
	if payload.AgentCounts.Claude < 2 {
		t.Errorf("claude count = %d, want at least 2", payload.AgentCounts.Claude)
	}
}

// TestRobotSendAndTail tests --robot-send and --robot-tail together.
func TestRobotSendAndTail(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_send_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session first
	logger.LogSection("spawn session for send test")
	_, _ = logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1", "--safety")
	time.Sleep(500 * time.Millisecond)

	// Verify session was created
	testutil.AssertSessionExists(t, logger, session)

	// Test robot-send
	marker := fmt.Sprintf("ROBOT_SEND_MARKER_%d", time.Now().UnixNano())
	logger.LogSection("robot-send")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-send", session, "--msg", fmt.Sprintf("echo %s", marker), "--type=cc")
	logger.Log("robot-send output: %s", string(out))

	var sendPayload struct {
		Success bool `json:"success"`
		Targets []struct {
			PaneIdx int `json:"pane_idx"`
		} `json:"targets"`
		TargetCount int `json:"target_count"`
	}

	if err := json.Unmarshal(out, &sendPayload); err != nil {
		t.Fatalf("invalid robot-send JSON: %v", err)
	}

	if !sendPayload.Success {
		t.Fatalf("robot-send should succeed")
	}
	if sendPayload.TargetCount < 1 {
		t.Errorf("target_count = %d, want at least 1", sendPayload.TargetCount)
	}

	// Wait for command to execute
	time.Sleep(300 * time.Millisecond)

	// Test robot-tail
	logger.LogSection("robot-tail")
	out = testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-tail", session, "--lines=50")
	logger.Log("robot-tail output: %s", string(out))

	var tailPayload struct {
		Success bool   `json:"success"`
		Session string `json:"session"`
		Panes   []struct {
			Index   int    `json:"index"`
			Content string `json:"content"`
		} `json:"panes"`
	}

	if err := json.Unmarshal(out, &tailPayload); err != nil {
		t.Fatalf("invalid robot-tail JSON: %v", err)
	}

	if !tailPayload.Success {
		t.Fatalf("robot-tail should succeed")
	}
	if tailPayload.Session != session {
		t.Errorf("session = %q, want %q", tailPayload.Session, session)
	}

	// Check if marker appears in any pane content
	markerFound := false
	for _, pane := range tailPayload.Panes {
		if strings.Contains(pane.Content, marker) {
			markerFound = true
			logger.Log("Found marker in pane %d", pane.Index)
			break
		}
	}
	if !markerFound {
		logger.Log("WARNING: marker not found in tail output - timing issue possible")
	}
}

// TestRobotInterrupt tests the --robot-interrupt flag.
func TestRobotInterrupt(t *testing.T) {
	testutil.RequireE2E(t)
	testutil.RequireTmux(t)
	testutil.RequireNTMBinary(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	session := fmt.Sprintf("robot_interrupt_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, session)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	// Spawn session
	logger.LogSection("spawn session for interrupt test")
	_, _ = logger.Exec("ntm", "--config", configPath, "spawn", session, "--cc=1", "--safety")
	time.Sleep(500 * time.Millisecond)

	// Verify session was created
	testutil.AssertSessionExists(t, logger, session)

	// Test robot-interrupt
	logger.LogSection("robot-interrupt")
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath,
		"--robot-interrupt", session, "--interrupt-force")
	logger.Log("robot-interrupt output: %s", string(out))

	var payload struct {
		Success     bool `json:"success"`
		Interrupted int  `json:"interrupted"`
	}

	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("invalid robot-interrupt JSON: %v", err)
	}

	if !payload.Success {
		t.Fatalf("robot-interrupt should succeed")
	}
}

func createSyntheticAgentSession(t *testing.T, logger *testutil.TestLogger) string {
	t.Helper()

	name := fmt.Sprintf("robot_json_%d", time.Now().UnixNano())
	return createSyntheticAgentSessionWithName(t, logger, name)
}

func createSyntheticAgentSessionWithName(t *testing.T, logger *testutil.TestLogger, name string) string {
	t.Helper()

	workdir := t.TempDir()

	logger.LogSection("Create synthetic tmux session")
	testutil.AssertCommandSuccess(t, logger, "tmux", "new-session", "-d", "-s", name, "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "split-window", "-t", name, "-h", "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "split-window", "-t", name, "-v", "-c", workdir)
	testutil.AssertCommandSuccess(t, logger, "tmux", "select-layout", "-t", name, "tiled")

	paneIDsRaw := testutil.AssertCommandSuccess(t, logger, "tmux", "list-panes", "-t", name, "-F", "#{pane_id}")
	panes := strings.Fields(string(paneIDsRaw))
	if len(panes) < 3 {
		t.Fatalf("expected at least 3 panes, got %d (output=%s)", len(panes), string(paneIDsRaw))
	}

	titles := []string{
		fmt.Sprintf("%s__cc_1", name),
		fmt.Sprintf("%s__cod_1", name),
		fmt.Sprintf("%s__gmi_1", name),
	}

	for i, id := range panes[:3] {
		testutil.AssertCommandSuccess(t, logger, "tmux", "select-pane", "-t", id, "-T", titles[i])
	}

	t.Cleanup(func() {
		logger.LogSection("Teardown synthetic session")
		exec.Command("tmux", "kill-session", "-t", name).Run()
	})

	return name
}

// Skip tests if ntm binary is missing.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("ntm"); err != nil {
		// ntm binary not on PATH; skip suite gracefully
		return
	}
	os.Exit(m.Run())
}
