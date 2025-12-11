package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

// TestCLISpawnSendAndStatus verifies that ntm CLI commands drive tmux correctly:
// - spawn creates a tmux session
// - we can add synthetic agent panes
// - send targets those agent panes
// - status reports the expected pane count.
func TestCLISpawnSendAndStatus(t *testing.T) {
	testutil.RequireNTMBinary(t)
	testutil.RequireTmux(t)

	logger := testutil.NewTestLogger(t, t.TempDir())

	// Use a temp config that stubs agent binaries to /bin/true to avoid external dependencies.
	projectsBase := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	configContents := fmt.Sprintf(`projects_base = "%s"

[agents]
claude = "/bin/true"
codex = "/bin/true"
gemini = "/bin/true"
`, projectsBase)
	if err := os.WriteFile(configPath, []byte(configContents), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	// Make config available to subsequent ntm commands.
	t.Setenv("NTM_PROJECTS_BASE", projectsBase)

	// Create a session with a single stubbed claude agent.
	session := testutil.CreateTestSession(t, logger, testutil.SessionConfig{
		Agents: testutil.AgentConfig{
			Claude: 1,
		},
		WorkDir:   projectsBase,
		ExtraArgs: []string{"--config", configPath},
	})
	testutil.AssertSessionExists(t, logger, session)

	// Discover panes and locate the spawned cc pane.
	panes, err := tmux.GetPanesWithActivity(session)
	if err != nil {
		t.Fatalf("failed to list panes: %v", err)
	}
	var ccPaneID string
	for _, p := range panes {
		if strings.HasPrefix(p.Pane.Title, session+"__cc_") {
			ccPaneID = p.Pane.ID
			break
		}
	}
	if ccPaneID == "" {
		t.Fatalf("cc pane not found in session %s", session)
	}

	// status should see user + cc = 2 panes.
	out := testutil.AssertCommandSuccess(t, logger, "ntm", "status", "--json", "--config", configPath, session)
	var status struct {
		Panes []struct {
			Type string `json:"type"`
		} `json:"panes"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		t.Fatalf("failed to parse status JSON: %v", err)
	}
	if len(status.Panes) != 2 {
		t.Fatalf("expected 2 panes (user + cc), got %d", len(status.Panes))
	}

	// Send a command to cc panes and verify it lands.
	// Use --no-cass-check to skip CASS duplicate detection (may not be available in all test environments).
	// Use --no-hooks to skip hook loading (may not be configured in test environments).
	// Note: Use "--cc=" (with empty value) to ensure the prompt is parsed as positional argument,
	// not as the value for --cc. With IsBoolFlag=true, "--cc value" is ambiguous.
	const marker = "INTEGRATION_CC_OK"
	testutil.AssertCommandSuccess(t, logger, "ntm", "send", "--config", configPath, "--no-cass-check", "--no-hooks", session, "--cc=", "echo "+marker)

	testutil.AssertEventually(t, logger, 5*time.Second, 150*time.Millisecond, "cc pane receives send payload", func() bool {
		out, err := tmux.CapturePaneOutput(ccPaneID, 200)
		if err != nil {
			return false
		}
		return strings.Contains(out, marker)
	})
}
