package events

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewLogger(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	logger, err := NewLogger(LoggerOptions{
		Path:          logPath,
		RetentionDays: 30,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	if logger.file == nil {
		t.Error("Expected file to be opened")
	}
}

func TestNewLogger_Disabled(t *testing.T) {
	logger, err := NewLogger(LoggerOptions{
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	if logger.file != nil {
		t.Error("Expected file to be nil when disabled")
	}

	// Logging should be a no-op
	err = logger.Log(NewEvent(EventSessionCreate, "test", nil))
	if err != nil {
		t.Errorf("Log on disabled logger should not error: %v", err)
	}
}

func TestLogger_Log(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	logger, err := NewLogger(LoggerOptions{
		Path:    logPath,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	// Log an event
	event := NewEvent(EventSessionCreate, "myproject", map[string]interface{}{
		"claude_count": 2,
		"codex_count":  1,
	})
	if err := logger.Log(event); err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	// Close and read the file
	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	// Parse the logged event
	var logged Event
	if err := json.Unmarshal(data, &logged); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if logged.Type != EventSessionCreate {
		t.Errorf("Type = %q, want %q", logged.Type, EventSessionCreate)
	}

	if logged.Session != "myproject" {
		t.Errorf("Session = %q, want %q", logged.Session, "myproject")
	}
}

func TestLogger_LogEvent(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	logger, err := NewLogger(LoggerOptions{
		Path:    logPath,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	defer logger.Close()

	// Log using convenience method
	err = logger.LogEvent(EventPromptSend, "test-session", PromptSendData{
		TargetCount:  3,
		PromptLength: 100,
		Template:     "code_review",
	})
	if err != nil {
		t.Fatalf("LogEvent failed: %v", err)
	}

	logger.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var logged Event
	if err := json.Unmarshal(data, &logged); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if logged.Type != EventPromptSend {
		t.Errorf("Type = %q, want %q", logged.Type, EventPromptSend)
	}

	if tc, ok := logged.Data["target_count"].(float64); !ok || int(tc) != 3 {
		t.Errorf("target_count = %v, want 3", logged.Data["target_count"])
	}
}

func TestLogger_MultipleEvents(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	logger, err := NewLogger(LoggerOptions{
		Path:    logPath,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	// Log multiple events
	for i := 0; i < 5; i++ {
		logger.LogEvent(EventSessionCreate, "session-"+string(rune('a'+i)), nil)
	}
	logger.Close()

	// Read and count lines
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := bytes.Split(data, []byte("\n"))
	nonEmpty := 0
	for _, line := range lines {
		if len(line) > 0 {
			nonEmpty++
		}
	}

	if nonEmpty != 5 {
		t.Errorf("Got %d events, want 5", nonEmpty)
	}
}

func TestRotateOldEntries(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "events.jsonl")

	// Create file with old and new entries
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -35)   // 35 days ago
	recent := now.AddDate(0, 0, -5) // 5 days ago

	entries := []Event{
		{Timestamp: old, Type: EventSessionCreate, Session: "old"},
		{Timestamp: recent, Type: EventSessionCreate, Session: "recent"},
		{Timestamp: now, Type: EventSessionCreate, Session: "now"},
	}

	var data []byte
	for _, e := range entries {
		line, _ := json.Marshal(e)
		data = append(data, line...)
		data = append(data, '\n')
	}

	if err := os.WriteFile(logPath, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Create logger and trigger rotation
	logger, err := NewLogger(LoggerOptions{
		Path:          logPath,
		RetentionDays: 30,
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}

	// Force rotation
	logger.lastRotation = time.Time{} // Reset to trigger rotation
	if err := logger.rotateOldEntries(); err != nil {
		t.Fatalf("rotateOldEntries failed: %v", err)
	}
	logger.Close()

	// Read and verify
	data, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := bytes.Split(data, []byte("\n"))
	nonEmpty := 0
	for _, line := range lines {
		if len(line) > 0 {
			nonEmpty++
			var e Event
			json.Unmarshal(line, &e)
			if e.Session == "old" {
				t.Error("Old entry should have been rotated out")
			}
		}
	}

	if nonEmpty != 2 {
		t.Errorf("Got %d entries after rotation, want 2", nonEmpty)
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		got := expandPath(tt.input)
		if got != tt.want {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}

	// Test ~ expansion (can't test exact value since it depends on user)
	expanded := expandPath("~/test")
	if expanded == "~/test" {
		t.Error("expandPath should have expanded ~")
	}
}

func TestToMap(t *testing.T) {
	data := SessionCreateData{
		ClaudeCount: 2,
		CodexCount:  1,
		WorkDir:     "/path",
	}

	m := ToMap(data)

	if m["claude_count"] != 2 {
		t.Errorf("claude_count = %v, want 2", m["claude_count"])
	}

	if m["codex_count"] != 1 {
		t.Errorf("codex_count = %v, want 1", m["codex_count"])
	}
}

func TestToMap_AllTypes(t *testing.T) {
	t.Parallel()

	t.Run("SessionCreateData", func(t *testing.T) {
		t.Parallel()
		m := ToMap(SessionCreateData{
			ClaudeCount: 3,
			CodexCount:  2,
			GeminiCount: 1,
			WorkDir:     "/work",
			Recipe:      "code-review",
		})
		if m == nil {
			t.Fatal("ToMap returned nil")
		}
		if m["claude_count"] != 3 {
			t.Errorf("claude_count = %v, want 3", m["claude_count"])
		}
		if m["gemini_count"] != 1 {
			t.Errorf("gemini_count = %v, want 1", m["gemini_count"])
		}
		if m["work_dir"] != "/work" {
			t.Errorf("work_dir = %v, want /work", m["work_dir"])
		}
		if m["recipe"] != "code-review" {
			t.Errorf("recipe = %v, want code-review", m["recipe"])
		}
	})

	t.Run("AgentSpawnData", func(t *testing.T) {
		t.Parallel()
		m := ToMap(AgentSpawnData{
			AgentType: "claude",
			Model:     "opus-4.5",
			Variant:   "standard",
			PaneIndex: 2,
		})
		if m == nil {
			t.Fatal("ToMap returned nil")
		}
		if m["agent_type"] != "claude" {
			t.Errorf("agent_type = %v, want claude", m["agent_type"])
		}
		if m["model"] != "opus-4.5" {
			t.Errorf("model = %v, want opus-4.5", m["model"])
		}
		if m["variant"] != "standard" {
			t.Errorf("variant = %v, want standard", m["variant"])
		}
		if m["pane_index"] != 2 {
			t.Errorf("pane_index = %v, want 2", m["pane_index"])
		}
	})

	t.Run("PromptSendData", func(t *testing.T) {
		t.Parallel()
		m := ToMap(PromptSendData{
			TargetCount:     5,
			PromptLength:    1000,
			Template:        "debug",
			HasContext:      true,
			TargetTypes:     "claude,codex",
			EstimatedTokens: 500,
		})
		if m == nil {
			t.Fatal("ToMap returned nil")
		}
		if m["target_count"] != 5 {
			t.Errorf("target_count = %v, want 5", m["target_count"])
		}
		if m["prompt_length"] != 1000 {
			t.Errorf("prompt_length = %v, want 1000", m["prompt_length"])
		}
		if m["has_context"] != true {
			t.Errorf("has_context = %v, want true", m["has_context"])
		}
		if m["estimated_tokens"] != 500 {
			t.Errorf("estimated_tokens = %v, want 500", m["estimated_tokens"])
		}
	})

	t.Run("CheckpointData", func(t *testing.T) {
		t.Parallel()
		m := ToMap(CheckpointData{
			CheckpointID: "cp-123",
			Description:  "before refactor",
			IncludesGit:  true,
		})
		if m == nil {
			t.Fatal("ToMap returned nil")
		}
		if m["checkpoint_id"] != "cp-123" {
			t.Errorf("checkpoint_id = %v, want cp-123", m["checkpoint_id"])
		}
		if m["description"] != "before refactor" {
			t.Errorf("description = %v, want before refactor", m["description"])
		}
		if m["includes_git"] != true {
			t.Errorf("includes_git = %v, want true", m["includes_git"])
		}
	})

	t.Run("ErrorData", func(t *testing.T) {
		t.Parallel()
		m := ToMap(ErrorData{
			ErrorType: "rate_limit",
			Message:   "429 Too Many Requests",
			Stack:     "goroutine 1 [running]:\nmain.go:42",
		})
		if m == nil {
			t.Fatal("ToMap returned nil")
		}
		if m["error_type"] != "rate_limit" {
			t.Errorf("error_type = %v, want rate_limit", m["error_type"])
		}
		if m["message"] != "429 Too Many Requests" {
			t.Errorf("message = %v, want 429 Too Many Requests", m["message"])
		}
		if m["stack"] != "goroutine 1 [running]:\nmain.go:42" {
			t.Errorf("stack = %v", m["stack"])
		}
	})

	t.Run("map passthrough", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{"key": "value", "num": 42}
		m := ToMap(input)
		if m["key"] != "value" {
			t.Errorf("key = %v, want value", m["key"])
		}
		if m["num"] != 42 {
			t.Errorf("num = %v, want 42", m["num"])
		}
	})

	t.Run("unknown type returns nil", func(t *testing.T) {
		t.Parallel()
		m := ToMap("string value")
		if m != nil {
			t.Errorf("ToMap(string) = %v, want nil", m)
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()
		m := ToMap(nil)
		if m != nil {
			t.Errorf("ToMap(nil) = %v, want nil", m)
		}
	})
}

func TestNewEventWithCorrelation(t *testing.T) {
	t.Parallel()

	before := time.Now()
	event := NewEventWithCorrelation(EventAgentSpawn, "sess", "agent1", "corr-123",
		map[string]interface{}{"key": "val"})
	after := time.Now()

	if event.Type != EventAgentSpawn {
		t.Errorf("Type = %q, want %q", event.Type, EventAgentSpawn)
	}
	if event.Session != "sess" {
		t.Errorf("Session = %q, want %q", event.Session, "sess")
	}
	if event.AgentName != "agent1" {
		t.Errorf("AgentName = %q, want %q", event.AgentName, "agent1")
	}
	if event.CorrelationID != "corr-123" {
		t.Errorf("CorrelationID = %q, want %q", event.CorrelationID, "corr-123")
	}
	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Error("Timestamp should be between before and after")
	}
	if event.Data["key"] != "val" {
		t.Errorf("Data[key] = %v, want val", event.Data["key"])
	}
}

func TestNewEvent(t *testing.T) {
	before := time.Now()
	event := NewEvent(EventSessionCreate, "test", map[string]interface{}{"key": "value"})
	after := time.Now()

	if event.Type != EventSessionCreate {
		t.Errorf("Type = %q, want %q", event.Type, EventSessionCreate)
	}

	if event.Session != "test" {
		t.Errorf("Session = %q, want %q", event.Session, "test")
	}

	if event.Timestamp.Before(before) || event.Timestamp.After(after) {
		t.Error("Timestamp should be between before and after")
	}
}

func TestNewEvent_NilData(t *testing.T) {
	t.Parallel()

	event := NewEvent(EventSessionKill, "s", nil)
	if event.Type != EventSessionKill {
		t.Errorf("Type = %q, want %q", event.Type, EventSessionKill)
	}
	if event.Data != nil {
		t.Errorf("Data = %v, want nil", event.Data)
	}
}

func TestEventTypeConstants(t *testing.T) {
	t.Parallel()

	// Verify all event type constants are non-empty and unique
	types := []EventType{
		EventSessionCreate, EventSessionKill, EventSessionAttach,
		EventAgentSpawn, EventAgentAdd, EventAgentCrash, EventAgentRestart,
		EventPromptSend, EventPromptBroadcast, EventInterrupt,
		EventCheckpointCreate, EventCheckpointRestore, EventSessionSave, EventSessionRestore,
		EventTemplateUse, EventError,
	}

	seen := make(map[EventType]bool)
	for _, et := range types {
		if string(et) == "" {
			t.Errorf("EventType constant is empty")
		}
		if seen[et] {
			t.Errorf("Duplicate EventType: %q", et)
		}
		seen[et] = true
	}
}
