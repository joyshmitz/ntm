package cli

import (
	"testing"
)

// ---------------------------------------------------------------------------
// detectAgentTypeFromTitle — 0% → 100%
// ---------------------------------------------------------------------------

func TestDetectAgentTypeFromTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		// Claude detection
		{"claude via __cc prefix", "myproject__cc_1", "claude"},
		{"claude via keyword", "project Claude Code", "claude"},
		{"claude mixed case", "CLAUDE session", "claude"},

		// Codex detection
		{"codex via __cod prefix", "myproject__cod_2", "codex"},
		{"codex via keyword", "Codex agent running", "codex"},

		// Gemini detection
		{"gemini via __gmi prefix", "myproject__gmi_1", "gemini"},
		{"gemini via keyword", "gemini-pro session", "gemini"},

		// User detection
		{"user via __user prefix", "myproject__user_1", "user"},
		{"user via keyword", "user terminal", "user"},

		// Unknown
		{"unknown agent type", "random-pane-title", "unknown"},
		{"empty title", "", "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := detectAgentTypeFromTitle(tc.title)
			if got != tc.want {
				t.Errorf("detectAgentTypeFromTitle(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatTokenCount — 0% → 100%
// ---------------------------------------------------------------------------

func TestFormatTokenCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tokens int
		want   string
	}{
		{"zero", 0, "0"},
		{"small", 42, "42"},
		{"under 1K", 999, "999"},
		{"exactly 1K", 1000, "1.0K"},
		{"1500 tokens", 1500, "1.5K"},
		{"under 1M", 999999, "1000.0K"},
		{"exactly 1M", 1000000, "1.0M"},
		{"1.5M tokens", 1500000, "1.5M"},
		{"large", 10000000, "10.0M"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatTokenCount(tc.tokens)
			if got != tc.want {
				t.Errorf("formatTokenCount(%d) = %q, want %q", tc.tokens, got, tc.want)
			}
		})
	}
}
