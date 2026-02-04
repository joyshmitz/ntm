//go:build e2e
// +build e2e

// Package e2e contains end-to-end tests for NTM CLI commands.
// [E2E-STAGGER] Tests for staggered spawn and prompt delivery.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type spawnResponse struct {
	Session          string           `json:"session"`
	Created          bool             `json:"created"`
	WorkingDirectory string           `json:"working_directory,omitempty"`
	Panes            []spawnPane      `json:"panes"`
	AgentCounts      spawnAgentCounts `json:"agent_counts"`
	Stagger          *spawnStagger    `json:"stagger,omitempty"`
}

type spawnPane struct {
	Index         int    `json:"index"`
	Type          string `json:"type"`
	PromptDelayMs int64  `json:"prompt_delay_ms,omitempty"`
}

type spawnAgentCounts struct {
	Claude int `json:"claude"`
	Codex  int `json:"codex"`
	Gemini int `json:"gemini"`
	User   int `json:"user,omitempty"`
	Total  int `json:"total"`
}

type spawnStagger struct {
	Enabled    bool  `json:"enabled"`
	IntervalMs int64 `json:"interval_ms,omitempty"`
}

func TestE2E_StaggeredSpawnPromptDelivery(t *testing.T) {
	CommonE2EPrerequisites(t)
	SkipIfNoAgents(t)

	agentType := GetAvailableAgent()
	if agentType == "" {
		t.Skip("no agent CLI available")
	}

	logger := NewTestLogger(t, "stagger_spawn")
	defer logger.Close()

	baseDir, err := os.MkdirTemp("", "ntm-stagger-e2e-")
	if err != nil {
		t.Fatalf("[E2E-STAGGER] temp dir: %v", err)
	}
	defer os.RemoveAll(baseDir)

	session := fmt.Sprintf("e2e_stagger_%d", time.Now().UnixNano())
	projectDir := filepath.Join(baseDir, session)
	expectedDir := projectDir

	staggerDelay := 100 * time.Millisecond
	prompt := "Say hello."

	flag, ok := agentFlag(agentType)
	if !ok {
		t.Fatalf("[E2E-STAGGER] unsupported agent type: %s", agentType)
	}

	args := []string{
		"--json",
		"spawn",
		session,
		"--no-hooks",
		flag,
		"--prompt", prompt,
		fmt.Sprintf("--stagger=%s", staggerDelay),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ntm", args...)
	cmd.Env = append(os.Environ(), "NTM_PROJECTS_BASE="+baseDir)
	cmd.Dir = baseDir

	logger.Log("[E2E-STAGGER] Running: ntm %s", strings.Join(args, " "))
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Log("[E2E-STAGGER] spawn error: %v output=%s", err, string(out))
		t.Fatalf("[E2E-STAGGER] spawn failed: %v", err)
	}

	var resp spawnResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("[E2E-STAGGER] parse spawn JSON: %v output=%s", err, string(out))
	}
	logger.LogJSON("[E2E-STAGGER] spawn response", resp)

	if !resp.Created {
		t.Fatalf("[E2E-STAGGER] expected created=true")
	}
	if resp.Session != session {
		t.Fatalf("[E2E-STAGGER] session mismatch: %q", resp.Session)
	}
	if resp.WorkingDirectory != expectedDir {
		t.Fatalf("[E2E-STAGGER] working_directory mismatch: got %q want %q", resp.WorkingDirectory, expectedDir)
	}
	if resp.Stagger == nil || !resp.Stagger.Enabled {
		t.Fatalf("[E2E-STAGGER] expected stagger enabled in response")
	}
	if resp.Stagger.IntervalMs != staggerDelay.Milliseconds() {
		t.Fatalf("[E2E-STAGGER] interval mismatch: got %d want %d", resp.Stagger.IntervalMs, staggerDelay.Milliseconds())
	}

	hasDelayedPrompt := false
	for _, p := range resp.Panes {
		if p.PromptDelayMs > 0 {
			hasDelayedPrompt = true
			break
		}
	}
	if !hasDelayedPrompt {
		t.Fatalf("[E2E-STAGGER] expected at least one pane with prompt_delay_ms > 0")
	}

	killArgs := []string{"--json", "kill", session, "--force"}
	killCmd := exec.CommandContext(context.Background(), "ntm", killArgs...)
	killCmd.Env = append(os.Environ(), "NTM_PROJECTS_BASE="+baseDir)
	killCmd.Dir = baseDir
	if killOut, killErr := killCmd.CombinedOutput(); killErr != nil {
		logger.Log("[E2E-STAGGER] kill error: %v output=%s", killErr, string(killOut))
		t.Fatalf("[E2E-STAGGER] failed to kill session: %v", killErr)
	}
}
