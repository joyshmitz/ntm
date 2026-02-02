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

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// BD-domc: E2E tests for history browsing and replay.
func TestHistoryBrowseAndReplay(t *testing.T) {
	testutil.E2ETestPrecheckThrottled(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	// Isolate all history/state writes so this test never touches the user's real ~/.local/share or ~/.ntm.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	sessionName := fmt.Sprintf("e2e_history_%d", time.Now().UnixNano())
	projectsBase := t.TempDir()
	projectDir := filepath.Join(projectsBase, sessionName)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	configContent := fmt.Sprintf(`
projects_base = %q

[agents]
claude = "bash"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	t.Cleanup(func() {
		exec.Command(tmux.BinaryPath(), "kill-session", "-t", sessionName).Run()
	})

	logger.LogSection("spawn session")
	out, err := logger.Exec("ntm", "--config", configPath, "spawn", sessionName, "--cc=1", "--json")
	logger.Log("spawn output: %s (err=%v)", string(out), err)
	if err != nil {
		t.Fatalf("ntm spawn failed: %v", err)
	}

	// Give tmux time to create panes.
	time.Sleep(750 * time.Millisecond)
	testutil.AssertSessionExists(t, logger, sessionName)
	testutil.AssertPaneCountAtLeast(t, logger, sessionName, 2) // user pane + 1 agent

	// Scenario 1: History recording
	marker1 := fmt.Sprintf("HISTORY_MARKER_1_%d", time.Now().UnixNano())
	prompt1 := fmt.Sprintf("echo %s", marker1)
	logger.LogSection("send 1 (records history)")
	out, err = logger.Exec("ntm", "--config", configPath, "send", sessionName, prompt1, "--cc", "--no-cass-check")
	logger.Log("send output: %s (err=%v)", string(out), err)
	if err != nil {
		t.Fatalf("ntm send failed: %v", err)
	}

	marker2 := fmt.Sprintf("HISTORY_MARKER_2_%d", time.Now().UnixNano())
	prompt2 := fmt.Sprintf("echo %s", marker2)
	logger.LogSection("send 2 (records history)")
	out, err = logger.Exec("ntm", "--config", configPath, "send", sessionName, prompt2, "--cc", "--no-cass-check")
	logger.Log("send output: %s (err=%v)", string(out), err)
	if err != nil {
		t.Fatalf("ntm send failed: %v", err)
	}

	agentPane := -1
	testutil.AssertEventually(t, logger, 10*time.Second, 200*time.Millisecond, "marker2 appears in an agent pane", func() bool {
		paneCount, err := testutil.GetSessionPaneCount(sessionName)
		if err != nil {
			return false
		}
		for i := 0; i < paneCount; i++ {
			content, err := testutil.CapturePane(sessionName, i)
			if err != nil {
				continue
			}
			if strings.Contains(content, marker2) {
				agentPane = i
				return true
			}
		}
		return false
	})
	if agentPane < 0 {
		t.Fatalf("could not determine agent pane (marker not observed)")
	}

	historyAll := getRobotHistory(t, logger, configPath, sessionName, nil)
	logger.Log("FULL HISTORY JSON:\n%s", historyAll.raw)

	entry1, ok := findHistoryEntryByMarker(historyAll, marker1)
	if !ok {
		t.Fatalf("expected history to contain marker1 prompt: %q", marker1)
	}
	entry2, ok := findHistoryEntryByMarker(historyAll, marker2)
	if !ok {
		t.Fatalf("expected history to contain marker2 prompt: %q", marker2)
	}

	if entry1.ID == "" || entry2.ID == "" {
		t.Fatalf("history entries must have non-empty IDs")
	}
	if entry1.Timestamp.IsZero() || entry2.Timestamp.IsZero() {
		t.Fatalf("history entries must have timestamps")
	}
	now := time.Now().UTC()
	for _, e := range []robotHistoryEntry{entry1, entry2} {
		if e.Timestamp.After(now.Add(10 * time.Second)) {
			t.Fatalf("history timestamp is unexpectedly in the future: %s", e.Timestamp.Format(time.RFC3339))
		}
		if now.Sub(e.Timestamp) > 2*time.Minute {
			t.Fatalf("history timestamp is unexpectedly old: %s", e.Timestamp.Format(time.RFC3339))
		}
		if !e.Success {
			t.Fatalf("history entry should be success=true (marker=%q, error=%q)", e.marker, e.Error)
		}
		if len(e.Targets) == 0 {
			t.Fatalf("history entry must include targets (marker=%q)", e.marker)
		}
	}

	// Scenario 2: History browse (filters)
	historyLast := getRobotHistory(t, logger, configPath, sessionName, []string{"--last=1"})
	if historyLast.Filtered != 1 || len(historyLast.Entries) != 1 {
		t.Fatalf("--last=1 should return exactly 1 entry (filtered=%d entries=%d)", historyLast.Filtered, len(historyLast.Entries))
	}
	if !strings.Contains(historyLast.Entries[0].Prompt, marker2) {
		t.Fatalf("--last=1 should return the most recent prompt (want marker2=%q, got prompt=%q)", marker2, historyLast.Entries[0].Prompt)
	}

	historySince := getRobotHistory(t, logger, configPath, sessionName, []string{"--since=1h"})
	if historySince.Filtered < 2 {
		t.Fatalf("--since=1h should include both entries (filtered=%d)", historySince.Filtered)
	}

	// Filter by pane index (history stores pane indices as strings in targets).
	paneFilter := entry2.Targets[0]
	historyPane := getRobotHistory(t, logger, configPath, sessionName, []string{"--pane=" + paneFilter})
	if !historyContainsID(historyPane, entry2.ID) {
		t.Fatalf("--pane=%s should include marker2 entry (id=%s)", paneFilter, entry2.ID)
	}

	nonTargetPane := "0"
	if containsString(entry2.Targets, nonTargetPane) {
		nonTargetPane = "999"
	}
	historyOtherPane := getRobotHistory(t, logger, configPath, sessionName, []string{"--pane=" + nonTargetPane})
	if historyOtherPane.Filtered != 0 {
		t.Fatalf("--pane=%s should exclude entries (filtered=%d)", nonTargetPane, historyOtherPane.Filtered)
	}

	// Scenario 3: Replay (robot replay delegates to robot send; output is SendOutput JSON)
	beforeReplay := captureOccurrences(t, logger, sessionName, agentPane, marker2)

	logger.LogSection("robot-replay")
	replayOut := testutil.AssertCommandSuccess(t, logger, "ntm", "--config", configPath, "--robot-replay="+sessionName, "--id="+entry2.ID)
	logger.Log("robot-replay output:\n%s", string(replayOut))

	var replaySend struct {
		Success        bool     `json:"success"`
		Session        string   `json:"session"`
		MessagePreview string   `json:"message_preview"`
		Successful     []string `json:"successful"`
	}
	if err := json.Unmarshal(replayOut, &replaySend); err != nil {
		t.Fatalf("robot-replay should output valid JSON (send output): %v", err)
	}
	if !replaySend.Success {
		t.Fatalf("robot-replay should succeed (send output success=false)")
	}
	if replaySend.Session != sessionName {
		t.Fatalf("robot-replay send output session=%q, want %q", replaySend.Session, sessionName)
	}
	if !strings.Contains(replaySend.MessagePreview, marker2) {
		t.Fatalf("robot-replay message_preview should include marker2 (preview=%q)", replaySend.MessagePreview)
	}
	if len(replaySend.Successful) == 0 {
		t.Fatalf("robot-replay should report at least one successful target")
	}

	testutil.AssertEventually(t, logger, 10*time.Second, 200*time.Millisecond, "marker2 appears twice after replay", func() bool {
		after := captureOccurrences(t, logger, sessionName, agentPane, marker2)
		return after >= beforeReplay+1
	})
}

type robotHistoryResponse struct {
	raw      string
	Success  bool                `json:"success"`
	Session  string              `json:"session"`
	Entries  []robotHistoryEntry `json:"entries"`
	Total    int                 `json:"total"`
	Filtered int                 `json:"filtered"`
}

type robotHistoryEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"ts"`
	Session   string    `json:"session"`
	Targets   []string  `json:"targets"`
	Prompt    string    `json:"prompt"`
	Source    string    `json:"source"`
	Success   bool      `json:"success"`
	Error     string    `json:"error"`

	marker string // test-only: which marker this entry matched
}

func getRobotHistory(t *testing.T, logger *testutil.TestLogger, configPath, session string, extraArgs []string) robotHistoryResponse {
	t.Helper()

	args := []string{"--config", configPath, "--robot-history=" + session}
	args = append(args, extraArgs...)
	out := testutil.AssertCommandSuccess(t, logger, "ntm", args...)

	var resp robotHistoryResponse
	resp.raw = string(out)
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON from --robot-history: %v", err)
	}
	if !resp.Success {
		t.Fatalf("--robot-history returned success=false: %s", resp.raw)
	}
	if resp.Session != session {
		t.Fatalf("--robot-history session=%q, want %q", resp.Session, session)
	}
	return resp
}

func findHistoryEntryByMarker(resp robotHistoryResponse, marker string) (robotHistoryEntry, bool) {
	for _, e := range resp.Entries {
		if strings.Contains(e.Prompt, marker) {
			e.marker = marker
			return e, true
		}
	}
	return robotHistoryEntry{}, false
}

func historyContainsID(resp robotHistoryResponse, id string) bool {
	for _, e := range resp.Entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func captureOccurrences(t *testing.T, logger *testutil.TestLogger, session string, paneIndex int, marker string) int {
	t.Helper()
	content, err := testutil.CapturePane(session, paneIndex)
	if err != nil {
		logger.Log("capture pane failed: %v", err)
		return 0
	}
	return strings.Count(content, marker)
}
