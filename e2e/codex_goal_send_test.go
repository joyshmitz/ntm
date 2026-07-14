//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/tests/testutil"
)

type codexGoalSendProcessOutput struct {
	Success        bool   `json:"success"`
	ErrorCode      string `json:"error_code,omitempty"`
	Error          string `json:"error,omitempty"`
	Session        string `json:"session"`
	Pane           string `json:"pane"`
	PaneID         string `json:"pane_id"`
	TypedGoal      bool   `json:"typed_goal"`
	BodyInjected   bool   `json:"body_injected"`
	Submitted      bool   `json:"submitted"`
	SubmitAttempts int    `json:"submit_attempts"`
	State          string `json:"state"`
}

func TestE2ECodexGoalSendJSONContract(t *testing.T) {
	CommonE2EPrerequisites(t)
	testutil.RequireTmuxThrottled(t)

	t.Run("submits_one_goal", func(t *testing.T) {
		fixture := newCanonicalPaneFixture(t)
		target := fixture.panes["0.0"]
		fixture.sendPaneCommand(t, target.ID, "printf '\nOpenAI Codex\ncodex>\n'")
		fixture.waitForPaneContains(t, "0.0", "codex>")

		result := fixture.runNTM(t, nil,
			"--json", "send", fixture.session, "prove one goal", "--codex-goal", "--pane="+target.ID,
		)
		if result.exitCode != 0 || len(bytes.TrimSpace(result.stderr)) != 0 {
			t.Fatalf("goal send exit=%d stdout=%s stderr=%s", result.exitCode, result.stdout, result.stderr)
		}
		var output codexGoalSendProcessOutput
		if err := json.Unmarshal(result.stdout, &output); err != nil {
			t.Fatalf("goal send did not emit exactly one JSON document: %v output=%s", err, result.stdout)
		}
		if !output.Success || output.Session != fixture.session || output.PaneID != target.ID || output.Pane != "0.0" ||
			!output.TypedGoal || !output.BodyInjected || !output.Submitted || output.SubmitAttempts != 1 || output.State != "submitted" {
			t.Fatalf("goal send output=%+v", output)
		}
	})

	t.Run("cancel_no_late_enter", func(t *testing.T) {
		fixture := newCanonicalPaneFixture(t)
		target := fixture.panes["0.0"]
		fixture.sendPaneCommand(t, target.ID, "printf '\nOpenAI Codex\ncodex>\n'")
		fixture.waitForPaneContains(t, "0.0", "codex>")

		stageMarker := filepath.Join(fixture.runtimeRoot, "codex-goal-staged")
		postStageLog := filepath.Join(fixture.runtimeRoot, "codex-goal-post-stage.log")
		wrapper := filepath.Join(fixture.runtimeRoot, "bin", "tmux-codex-goal-cancel")
		wrapperBody := `#!/bin/sh
if [ "${1:-}" = "send-keys" ]; then
  literal=0
  enter=0
  goal=0
  for arg in "$@"; do
    if [ "$arg" = "-l" ]; then literal=1; fi
    if [ "$arg" = "Enter" ]; then enter=1; fi
    if [ "$arg" = "/goal " ]; then goal=1; fi
  done
  if [ "$goal" = "1" ]; then
    "$NTM_E2E_REAL_TMUX" "$@"
    rc=$?
    : > "$NTM_E2E_STAGE_MARKER"
    exit "$rc"
  fi
  if [ -f "$NTM_E2E_STAGE_MARKER" ] && { [ "$literal" = "1" ] || [ "$enter" = "1" ]; }; then
    printf '%s\n' "$*" >> "$NTM_E2E_POST_STAGE_LOG"
  fi
fi
if [ "${1:-}" = "capture-pane" ] && [ -f "$NTM_E2E_STAGE_MARKER" ]; then
  exec sleep 30
fi
exec "$NTM_E2E_REAL_TMUX" "$@"
`
		if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o700); err != nil {
			t.Fatalf("write goal cancellation wrapper: %v", err)
		}

		cmd := exec.Command(fixture.ntmPath,
			"--json", "send", fixture.session, "must not inject", "--codex-goal", "--pane="+target.ID,
		)
		cmd.Env = mergeProcessEnv(fixture.env, map[string]string{
			"NTM_TMUX_BINARY":        wrapper,
			"NTM_E2E_REAL_TMUX":      fixture.tmuxPath,
			"NTM_E2E_STAGE_MARKER":   stageMarker,
			"NTM_E2E_POST_STAGE_LOG": postStageLog,
		})
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start goal send: %v", err)
		}
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()

		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(stageMarker); err == nil {
				break
			}
			select {
			case waitErr := <-waited:
				t.Fatalf("goal send exited before palette stage: %v stdout=%s stderr=%s", waitErr, stdout.String(), stderr.String())
			default:
			}
			if time.Now().After(deadline) {
				_ = cmd.Process.Signal(os.Interrupt)
				t.Fatalf("goal send did not stage /goal: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			time.Sleep(20 * time.Millisecond)
		}

		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("interrupt goal send: %v", err)
		}
		exitCode := 0
		select {
		case waitErr := <-waited:
			var exitErr *exec.ExitError
			if !errors.As(waitErr, &exitErr) {
				t.Fatalf("canceled goal send wait=%v", waitErr)
			}
			exitCode = exitErr.ExitCode()
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Signal(os.Interrupt)
			t.Fatal("canceled goal send did not return")
		}
		if exitCode != 1 || len(bytes.TrimSpace(stderr.Bytes())) != 0 {
			t.Fatalf("canceled goal exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
		}
		var output codexGoalSendProcessOutput
		if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
			t.Fatalf("canceled goal send did not emit one JSON document: %v output=%s", err, stdout.String())
		}
		if output.Success || output.ErrorCode != robot.ErrCodeTimeout || output.Error == "" || !output.TypedGoal ||
			output.BodyInjected || output.Submitted || output.SubmitAttempts != 0 || output.State != "failed" {
			t.Fatalf("canceled goal output=%+v", output)
		}

		time.Sleep(tmux.DefaultEnterDelay + 250*time.Millisecond)
		postStage, err := os.ReadFile(postStageLog)
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read post-stage log: %v", err)
		}
		if strings.TrimSpace(string(postStage)) != "" {
			t.Fatalf("goal send actuated after cancellation: %s", postStage)
		}
		t.Logf("canceled goal send returned %s without post-stage actuation", fmt.Sprint(output.ErrorCode))
	})
}
