package robot

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPrintSave(t *testing.T) {
	// Need a session to save
	// But PrintSave checks session existence.
	// We can mock or use real session.
	// robot package calls tmux directly.

	// If no session, it should return error.
	originalFormat := GetOutputFormat()
	SetOutputFormat(FormatTOON)
	t.Cleanup(func() { SetOutputFormat(originalFormat) })

	opts := SaveOptions{Session: "nonexistent"}
	output, err := captureStdout(t, func() error { return PrintSave(opts) })
	var exitErr *ProcessExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !exitErr.JSONWritten() {
		t.Fatalf("PrintSave error = %T %v, want written exit-1 ProcessExitError", err, err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Should have error
	if resp["error"] == nil {
		t.Error("Expected error in response for nonexistent session")
	}
	if got := resp["output_format"]; got != string(FormatJSON) {
		t.Errorf("output_format = %v, want json", got)
	}
	if resp["error_code"] == nil || resp["timestamp"] == nil {
		t.Fatalf("missing canonical error fields: %v", resp)
	}
}

func TestPrintRestore(t *testing.T) {
	originalFormat := GetOutputFormat()
	SetOutputFormat(FormatTOON)
	t.Cleanup(func() { SetOutputFormat(originalFormat) })

	// Restore from non-existent file
	opts := RestoreOptions{SavedName: "nonexistent_file"}
	output, err := captureStdout(t, func() error { return PrintRestore(opts) })
	var exitErr *ProcessExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 || !exitErr.JSONWritten() {
		t.Fatalf("PrintRestore error = %T %v, want written exit-1 ProcessExitError", err, err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if resp["error"] == nil {
		t.Error("Expected error for nonexistent file")
	}
	if got := resp["output_format"]; got != string(FormatJSON) {
		t.Errorf("output_format = %v, want json", got)
	}
	if resp["error_code"] == nil || resp["timestamp"] == nil {
		t.Fatalf("missing canonical error fields: %v", resp)
	}
}
