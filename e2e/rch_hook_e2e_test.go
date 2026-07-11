//go:build e2e
// +build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/integrations/dcg"
)

func TestRCHHookStdinProtocol(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "rch.log")
	rchPath := filepath.Join(tempDir, "rch")

	script := fmt.Sprintf(`#!/bin/sh
input=$(cat)
printf '%%s\n' "$input" >> %q
printf '%%s\n' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"stubbed by test"}}'
`, logPath)
	if err := os.WriteFile(rchPath, []byte(script), 0755); err != nil {
		t.Fatalf("failed to write stub rch binary: %v", err)
	}

	entry, err := dcg.GenerateRCHHookEntry(dcg.RCHHookOptions{
		BinaryPath: rchPath,
		Patterns:   []string{"^go build"},
	})
	if err != nil {
		t.Fatalf("GenerateRCHHookEntry failed: %v", err)
	}
	if len(entry.Hooks) != 1 {
		t.Fatalf("expected one hook handler, got %d", len(entry.Hooks))
	}
	handler := entry.Hooks[0]
	if handler.Type != "command" {
		t.Fatalf("hook handler type = %q, want command", handler.Type)
	}
	if handler.Timeout != 5 {
		t.Fatalf("hook handler timeout = %d, want 5", handler.Timeout)
	}

	buildInput := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go build ./..."}}`
	cmd := exec.Command("sh", "-c", handler.Command)
	cmd.Stdin = strings.NewReader(buildInput)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("hook command failed: %v output: %s", err, string(output))
	}
	if !json.Valid(output) {
		t.Fatalf("hook command returned invalid JSON: %s", string(output))
	}
	if !strings.Contains(string(output), "\"permissionDecision\":\"deny\"") {
		t.Fatalf("expected permissionDecision deny in output, got: %s", string(output))
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading rch log: %v", err)
	}
	if !strings.Contains(string(logData), buildInput) {
		t.Fatalf("expected hook JSON on rch stdin, got: %s", string(logData))
	}

	listInput := `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls -la"}}`
	cmd = exec.Command("sh", "-c", handler.Command)
	cmd.Stdin = strings.NewReader(listInput)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("second hook command failed: %v output: %s", err, string(output))
	}

	logData, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed reading rch log after second command: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 hook invocations, got %d: %s", len(lines), string(logData))
	}
	if lines[1] != listInput {
		t.Fatalf("second hook stdin = %q, want %q", lines[1], listInput)
	}
}
