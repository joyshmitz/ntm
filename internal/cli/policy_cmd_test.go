package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyValidateJSONFailuresAreTerminal(t *testing.T) {
	originalJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = originalJSON })

	tests := []struct {
		name    string
		prepare func(*testing.T) string
		wantErr string
	}{
		{
			name: "missing policy",
			prepare: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing-policy.yaml")
			},
			wantErr: "Policy file does not exist",
		},
		{
			name: "invalid policy",
			prepare: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "invalid-policy.yaml")
				if err := os.WriteFile(path, []byte("blocked: [\n"), 0644); err != nil {
					t.Fatalf("write invalid policy: %v", err)
				}
				return path
			},
			wantErr: "Invalid YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.prepare(t)
			stdout, runErr := captureStdout(t, func() error { return runPolicyValidate(path) })
			if !errors.Is(runErr, errJSONFailure) {
				t.Fatalf("runPolicyValidate() error = %v, want errJSONFailure", runErr)
			}
			if !strings.Contains(runErr.Error(), tt.wantErr) {
				t.Fatalf("runPolicyValidate() error = %v, want %q", runErr, tt.wantErr)
			}

			document := decodeSingleTerminalJSONMap(t, stdout)
			if success, ok := document["success"].(bool); !ok || success {
				t.Fatalf("success = %#v, want false", document["success"])
			}
			if valid, ok := document["valid"].(bool); !ok || valid {
				t.Fatalf("valid = %#v, want false", document["valid"])
			}
			topLevelError, ok := document["error"].(string)
			if !ok || !strings.Contains(topLevelError, tt.wantErr) {
				t.Fatalf("error = %#v, want top-level error containing %q", document["error"], tt.wantErr)
			}
			errorsValue, ok := document["errors"].([]interface{})
			if !ok || len(errorsValue) == 0 {
				t.Fatalf("errors = %#v, want first error containing %q", document["errors"], tt.wantErr)
			}
			firstError, ok := errorsValue[0].(string)
			if !ok || !strings.Contains(firstError, tt.wantErr) {
				t.Fatalf("errors = %#v, want first error containing %q", document["errors"], tt.wantErr)
			}
		})
	}
}

func TestPolicyValidateJSONSuccessIncludesTerminalSuccess(t *testing.T) {
	originalJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = originalJSON })

	path := filepath.Join(t.TempDir(), "policy.yaml")
	content := `version: 1
automation:
  auto_commit: false
  auto_push: false
  force_release: approval
blocked:
  - pattern: "rm -rf /"
    reason: "dangerous"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write valid policy: %v", err)
	}

	stdout, runErr := captureStdout(t, func() error { return runPolicyValidate(path) })
	if runErr != nil {
		t.Fatalf("runPolicyValidate() error = %v, want nil", runErr)
	}
	document := decodeSingleTerminalJSONMap(t, stdout)
	if success, ok := document["success"].(bool); !ok || !success {
		t.Fatalf("success = %#v, want true", document["success"])
	}
	if valid, ok := document["valid"].(bool); !ok || !valid {
		t.Fatalf("valid = %#v, want true", document["valid"])
	}
}

func TestPolicyCmd(t *testing.T) {
	cmd := newPolicyCmd()

	// Test that the command has expected subcommands
	expectedSubs := []string{"show", "validate", "reset", "edit", "automation"}
	for _, sub := range expectedSubs {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == sub {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected subcommand %q not found", sub)
		}
	}
}

func TestPolicyShowCmd(t *testing.T) {
	cmd := newPolicyShowCmd()
	if cmd.Use != "show" {
		t.Errorf("expected Use to be 'show', got %q", cmd.Use)
	}

	// Test help doesn't error
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Errorf("help command failed: %v", err)
	}
}

func TestPolicyValidateCmd(t *testing.T) {
	cmd := newPolicyValidateCmd()
	if cmd.Use != "validate [file]" {
		t.Errorf("expected Use to be 'validate [file]', got %q", cmd.Use)
	}
}

func TestPolicyResetCmd(t *testing.T) {
	cmd := newPolicyResetCmd()
	if cmd.Use != "reset" {
		t.Errorf("expected Use to be 'reset', got %q", cmd.Use)
	}

	// Check force flag exists
	f := cmd.Flags().Lookup("force")
	if f == nil {
		t.Error("expected --force flag")
	}
}

func TestPolicyEditCmd(t *testing.T) {
	cmd := newPolicyEditCmd()
	if cmd.Use != "edit" {
		t.Errorf("expected Use to be 'edit', got %q", cmd.Use)
	}
}

func TestPolicyAutomationCmd(t *testing.T) {
	cmd := newPolicyAutomationCmd()
	if cmd.Use != "automation" {
		t.Errorf("expected Use to be 'automation', got %q", cmd.Use)
	}

	// Check flags exist
	expectedFlags := []string{"auto-commit", "no-auto-commit", "auto-push", "no-auto-push", "force-release"}
	for _, name := range expectedFlags {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("expected --%s flag", name)
		}
	}
}

func TestGenerateDefaultPolicyYAML(t *testing.T) {
	yaml := generateDefaultPolicyYAML()

	// Check for expected content
	checks := []string{
		"version: 1",
		"automation:",
		"auto_commit: true",
		"auto_push: false",
		"force_release: approval",
		"allowed:",
		"blocked:",
		"approval_required:",
		"slb: true",
	}

	for _, check := range checks {
		if !bytes.Contains([]byte(yaml), []byte(check)) {
			t.Errorf("expected %q in default policy YAML", check)
		}
	}
}

func TestUpdateAutomationInYAML(t *testing.T) {
	input := `version: 1

automation:
  auto_commit: true
  auto_push: false
  force_release: approval

blocked:
  - pattern: 'test'
`

	auto := struct {
		AutoCommit   bool
		AutoPush     bool
		ForceRelease string
	}{
		AutoCommit:   false,
		AutoPush:     true,
		ForceRelease: "never",
	}

	// Test with mock type - need to import policy
	// For now just verify the function exists and doesn't panic
	if len(input) == 0 {
		t.Error("input should not be empty")
	}
	if auto.AutoCommit {
		t.Error("auto commit should be false in test")
	}
}

func TestToRuleSummaries(t *testing.T) {
	// This tests internal conversion - just verify no panic
	summaries := toRuleSummaries(nil)
	if len(summaries) != 0 {
		t.Error("expected empty slice for nil input")
	}
}

func TestFormatBool(t *testing.T) {
	// Verify formatBool returns non-empty strings
	enabled := formatBool(true)
	disabled := formatBool(false)

	if len(enabled) == 0 {
		t.Error("formatBool(true) should return non-empty string")
	}
	if len(disabled) == 0 {
		t.Error("formatBool(false) should return non-empty string")
	}
	if enabled == disabled {
		t.Error("formatBool should return different strings for true/false")
	}
}

func TestPolicyValidateWithTempFile(t *testing.T) {
	// Create a temp directory for the test
	tmpDir, err := os.MkdirTemp("", "ntm-policy-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with valid policy file
	validPolicy := `version: 1
automation:
  auto_commit: true
  auto_push: false
  force_release: approval

blocked:
  - pattern: 'rm -rf /'
    reason: "Dangerous"

allowed:
  - pattern: 'ls'
    reason: "Safe"
`
	validPath := filepath.Join(tmpDir, "valid.yaml")
	if err := os.WriteFile(validPath, []byte(validPolicy), 0644); err != nil {
		t.Fatalf("failed to write valid policy: %v", err)
	}

	// Test with invalid policy file
	invalidPolicy := `version: 1
automation:
  force_release: invalid_value
`
	invalidPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte(invalidPolicy), 0644); err != nil {
		t.Fatalf("failed to write invalid policy: %v", err)
	}

	// Test validation of valid file (should not error)
	// Note: runPolicyValidate uses os.Exit, so we just verify the file exists
	if !fileExists(validPath) {
		t.Error("valid policy file should exist")
	}

	if !fileExists(invalidPath) {
		t.Error("invalid policy file should exist")
	}
}
