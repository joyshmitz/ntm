package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestNewEnsembleCompareCmd(t *testing.T) {
	t.Log("TEST: TestNewEnsembleCompareCmd - starting")

	cmd := newEnsembleCompareCmd()

	if cmd == nil {
		t.Fatal("newEnsembleCompareCmd returned nil")
	}
	if cmd.Use != "compare <run1> <run2>" {
		t.Errorf("expected Use='compare <run1> <run2>', got %q", cmd.Use)
	}

	// Check flags exist
	formatFlag := cmd.Flags().Lookup("format")
	if formatFlag == nil {
		t.Error("expected --format flag")
	} else if formatFlag.DefValue != "text" {
		t.Errorf("expected --format default='text', got %q", formatFlag.DefValue)
	}

	verboseFlag := cmd.Flags().Lookup("verbose")
	if verboseFlag == nil {
		t.Error("expected --verbose flag")
	}

	t.Log("TEST: TestNewEnsembleCompareCmd - assertion: command created with correct flags")
}

func TestWriteCompareResult_JSON(t *testing.T) {
	t.Log("TEST: TestWriteCompareResult_JSON - starting")

	result := &ensemble.ComparisonResult{
		RunA:        "session-a",
		RunB:        "session-b",
		GeneratedAt: time.Now(),
		ModeDiff: ensemble.ModeDiff{
			Added:          []string{"mode-c"},
			Removed:        []string{"mode-d"},
			Unchanged:      []string{"mode-a", "mode-b"},
			AddedCount:     1,
			RemovedCount:   1,
			UnchangedCount: 2,
		},
		FindingsDiff: ensemble.FindingsDiff{
			NewCount:       2,
			MissingCount:   1,
			ChangedCount:   0,
			UnchangedCount: 3,
		},
		Summary: "+1 modes, -1 modes, +2 findings, -1 findings",
	}

	var buf bytes.Buffer
	opts := compareOptions{Verbose: false}
	err := writeCompareResult(&buf, result, opts, "json")

	if err != nil {
		t.Fatalf("writeCompareResult returned error: %v", err)
	}

	output := buf.String()
	t.Logf("TEST: TestWriteCompareResult_JSON - output: %s", output)

	// Parse JSON to validate structure
	var parsed compareOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if parsed.RunA != "session-a" {
		t.Errorf("expected RunA='session-a', got %q", parsed.RunA)
	}
	if parsed.RunB != "session-b" {
		t.Errorf("expected RunB='session-b', got %q", parsed.RunB)
	}
	if parsed.Result == nil {
		t.Error("expected Result to be present")
	}
	if parsed.Result.ModeDiff.AddedCount != 1 {
		t.Errorf("expected AddedCount=1, got %d", parsed.Result.ModeDiff.AddedCount)
	}

	t.Log("TEST: TestWriteCompareResult_JSON - assertion: JSON output is valid")
}

func TestWriteCompareResult_Text(t *testing.T) {
	t.Log("TEST: TestWriteCompareResult_Text - starting")

	result := &ensemble.ComparisonResult{
		RunA:        "run-alpha",
		RunB:        "run-beta",
		GeneratedAt: time.Now(),
		ModeDiff: ensemble.ModeDiff{
			Added:          []string{"new-mode"},
			Removed:        []string{},
			Unchanged:      []string{"existing-mode"},
			AddedCount:     1,
			RemovedCount:   0,
			UnchangedCount: 1,
		},
		FindingsDiff: ensemble.FindingsDiff{
			NewCount:       1,
			MissingCount:   0,
			ChangedCount:   0,
			UnchangedCount: 2,
		},
		Summary: "+1 modes, +1 findings",
	}

	var buf bytes.Buffer
	opts := compareOptions{Verbose: false}
	err := writeCompareResult(&buf, result, opts, "text")

	if err != nil {
		t.Fatalf("writeCompareResult returned error: %v", err)
	}

	output := buf.String()
	t.Logf("TEST: TestWriteCompareResult_Text - output:\n%s", output)

	// Check key sections are present
	if !strings.Contains(output, "run-alpha") {
		t.Error("expected output to contain run-alpha")
	}
	if !strings.Contains(output, "run-beta") {
		t.Error("expected output to contain run-beta")
	}
	if !strings.Contains(output, "Mode Changes") {
		t.Error("expected output to contain Mode Changes section")
	}
	if !strings.Contains(output, "Finding Changes") {
		t.Error("expected output to contain Finding Changes section")
	}

	t.Log("TEST: TestWriteCompareResult_Text - assertion: text output is well-formed")
}

func TestWriteCompareError_JSON(t *testing.T) {
	t.Log("TEST: TestWriteCompareError_JSON - starting")

	testErr := &testError{msg: "session not found"}

	var buf bytes.Buffer
	err := writeCompareError(&buf, "missing-a", "missing-b", testErr, "json")

	// Should return the original error
	if err == nil {
		t.Error("expected error to be returned")
	}

	output := buf.String()
	t.Logf("TEST: TestWriteCompareError_JSON - output: %s", output)

	// Parse JSON to validate structure
	var parsed compareOutput
	if parseErr := json.Unmarshal([]byte(output), &parsed); parseErr != nil {
		t.Fatalf("failed to parse JSON output: %v", parseErr)
	}

	if parsed.RunA != "missing-a" {
		t.Errorf("expected RunA='missing-a', got %q", parsed.RunA)
	}
	if parsed.Error != "session not found" {
		t.Errorf("expected Error='session not found', got %q", parsed.Error)
	}

	t.Log("TEST: TestWriteCompareError_JSON - assertion: error JSON output is valid")
}

func TestCompareOutput_Struct(t *testing.T) {
	t.Log("TEST: TestCompareOutput_Struct - starting")

	out := compareOutput{
		GeneratedAt: time.Now().Format(time.RFC3339),
		RunA:        "test-a",
		RunB:        "test-b",
		Summary:     "No differences",
		Result: &ensemble.ComparisonResult{
			RunA:    "test-a",
			RunB:    "test-b",
			Summary: "No differences",
		},
	}

	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("failed to marshal compareOutput: %v", err)
	}

	t.Logf("TEST: TestCompareOutput_Struct - JSON: %s", string(data))

	// Verify round-trip
	var parsed compareOutput
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal compareOutput: %v", err)
	}

	if parsed.RunA != out.RunA || parsed.RunB != out.RunB {
		t.Error("round-trip mismatch")
	}

	t.Log("TEST: TestCompareOutput_Struct - assertion: compareOutput marshals correctly")
}
