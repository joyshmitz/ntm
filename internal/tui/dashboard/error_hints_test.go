package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// hintForSessionFetchError — 41.7% → 100%
// ---------------------------------------------------------------------------

func TestHintForSessionFetchError(t *testing.T) {

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"deadline exceeded", context.DeadlineExceeded, "tmux is responding slowly"},
		{"tmux not installed", errors.New("tmux is not installed"), "Install tmux"},
		{"executable not found", errors.New("executable file not found in $PATH"), "Install tmux"},
		{"no server running", errors.New("no server running on /tmp/tmux-1000/default"), "Start tmux"},
		{"failed to connect", errors.New("failed to connect to server"), "Start tmux"},
		{"cant find session", errors.New("can't find session: myproject"), "Session may have ended"},
		{"session not found", errors.New("session not found: myproject"), "Session may have ended"},
		{"generic error", errors.New("something unexpected"), "Press r to retry"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hintForSessionFetchError(tc.err)
			if tc.want == "" {
				if got != "" {
					t.Errorf("expected empty hint, got %q", got)
				}
				return
			}
			if got == "" || !containsSubstring(got, tc.want) {
				t.Errorf("hintForSessionFetchError(%v) = %q, want to contain %q", tc.err, got, tc.want)
			}
		})
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// formatDuration — missing branch: <= 0 (87.5% → 100%)
// ---------------------------------------------------------------------------

func TestFormatDuration_Zero(t *testing.T) {
	if got := formatDuration(0); got != "now" {
		t.Errorf("formatDuration(0) = %q, want %q", got, "now")
	}
}

func TestFormatDuration_Negative(t *testing.T) {
	if got := formatDuration(-5 * time.Second); got != "now" {
		t.Errorf("formatDuration(-5s) = %q, want %q", got, "now")
	}
}
