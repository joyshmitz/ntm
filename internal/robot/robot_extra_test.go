package robot

import (
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestPrintTerse(t *testing.T) {
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed")
	}

	cfg := config.Default()
	output, err := captureStdout(t, func() error { return PrintTerse(cfg) })
	if err != nil {
		t.Fatalf("PrintTerse failed: %v", err)
	}

	// Output format: S:...|... (may be empty if no sessions exist and ListSessions returns empty)
	// When there are no sessions (but tmux is running), output may be just a newline
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		// No sessions - this is valid, skip further checks
		t.Log("No sessions found, output is empty (valid)")
		return
	}
	// Check for S: prefix (session) if there is output
	if !strings.HasPrefix(trimmed, "S:") {
		t.Errorf("Expected output to start with 'S:', got %q", trimmed)
	}
}

func TestPrintTerseNoTmux(t *testing.T) {
	// Without mocking, we can only test the parsing logic helper if we extract it,
	// or rely on PrintTerse behavior in current env.

	cfg := config.Default()
	output, err := captureStdout(t, func() error { return PrintTerse(cfg) })
	if err != nil {
		t.Fatalf("PrintTerse failed: %v", err)
	}

	parts := parseTerseOutput(output)
	// If output is empty (e.g. no sessions and no alert config), parts might be nil or empty string
	if len(output) > 0 && len(parts) == 0 {
		// It might be just a newline?
		if strings.TrimSpace(output) != "" {
			t.Error("No terse parts found but output not empty")
		}
	}

	for _, part := range parts {
		state, err := ParseTerse(part)
		if err != nil {
			t.Errorf("Failed to parse terse part %q: %v", part, err)
		}
		if state.Session == "" {
			t.Error("Session is empty in parsed state")
		}
	}
}

func parseTerseOutput(output string) []string {
	// Strip newline
	output = stripNewline(output)
	if output == "" {
		return nil
	}
	return strings.Split(output, ";")
}

func stripNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}
