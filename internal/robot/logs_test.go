package robot

import (
	"testing"
)

func TestLogsOptionsDefaults(t *testing.T) {
	opts := LogsOptions{
		Session: "test",
	}

	if opts.Session != "test" {
		t.Errorf("expected session 'test', got %s", opts.Session)
	}

	if opts.Limit != 0 {
		t.Errorf("expected zero limit by default, got %d", opts.Limit)
	}
}

func TestShortAgentType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude", "cc"},
		{"codex", "cod"},
		{"gemini", "gmi"},
		{"unknown", "unk"},
		{"ab", "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := shortAgentType(tt.input)
			if result != tt.expected {
				t.Errorf("shortAgentType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatAggregatedLog(t *testing.T) {
	entry := AggregatedLogEntry{
		Pane:      2,
		AgentType: "claude",
		Line:      "test line",
	}

	result := FormatAggregatedLog(entry)
	expected := "[cc:2] test line"
	if result != expected {
		t.Errorf("FormatAggregatedLog() = %q, want %q", result, expected)
	}
}

func TestLogsOutput_EmptyPanes(t *testing.T) {
	output := &LogsOutput{
		RobotResponse: NewRobotResponse(true),
		Panes:         []PaneLogs{},
		Summary:       LogsSummary{},
	}

	if !output.Success {
		t.Error("expected Success to be true")
	}

	if len(output.Panes) != 0 {
		t.Errorf("expected empty panes, got %d", len(output.Panes))
	}
}

func TestDefaultLogsLimit(t *testing.T) {
	if DefaultLogsLimit != 100 {
		t.Errorf("DefaultLogsLimit = %d, want 100", DefaultLogsLimit)
	}
}

func TestLogsStreamer_Creation(t *testing.T) {
	opts := StreamLogsOptions{
		Session: "test",
	}

	streamer, err := NewLogsStreamer(opts)
	if err != nil {
		t.Fatalf("NewLogsStreamer failed: %v", err)
	}

	if streamer == nil {
		t.Error("expected non-nil streamer")
	}
}

func TestLogsStreamer_InvalidFilter(t *testing.T) {
	opts := StreamLogsOptions{
		Session: "test",
		Filter:  "[invalid",
	}

	_, err := NewLogsStreamer(opts)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}
