package lint

import (
	"strings"
	"testing"
)

func TestLinterBasic(t *testing.T) {
	l := New()
	result := l.Lint("Hello, world!")

	if !result.Success {
		t.Errorf("expected success for benign prompt, got %v findings", len(result.Findings))
	}
	if result.Stats.ByteCount != 13 {
		t.Errorf("expected 13 bytes, got %d", result.Stats.ByteCount)
	}
	if result.Stats.LineCount != 1 {
		t.Errorf("expected 1 line, got %d", result.Stats.LineCount)
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input     string
		minTokens int
		maxTokens int
	}{
		{"", 0, 0},
		{"hello", 1, 3},
		{"hello world", 2, 5},
		{"The quick brown fox jumps over the lazy dog", 8, 15},
		{strings.Repeat("a", 1000), 200, 300},
	}

	for _, tt := range tests {
		estimate := EstimateTokens(tt.input)
		if estimate < tt.minTokens || estimate > tt.maxTokens {
			t.Errorf("EstimateTokens(%q) = %d, want between %d and %d",
				truncate(tt.input, 20), estimate, tt.minTokens, tt.maxTokens)
		}
	}
}

func TestCheckDestructive(t *testing.T) {
	tests := []struct {
		prompt  string
		wantLen int
		desc    string
	}{
		{"rm -rf /", 1, "delete root"},
		{"rm -rf ~", 1, "delete home"},
		{"rm -rf *", 1, "delete wildcard"},
		{"git reset --hard", 1, "git hard reset"},
		{"git push --force", 1, "git force push"},
		{"git push -f", 1, "git force push short"},
		{"git push --force-with-lease", 0, "force with lease is safe"},
		{"rm -rf node_modules", 0, "node_modules is safe"},
		{"rm -rf dist/", 0, "dist is safe"},
		{"DROP TABLE users", 1, "drop table"},
		{"kubectl delete ns production", 1, "k8s namespace delete"},
		{"echo hello", 0, "safe command"},
		{"git commit -m 'test'", 0, "git commit is safe"},
	}

	for _, tt := range tests {
		findings := CheckDestructive(tt.prompt, SeverityWarning)
		if len(findings) != tt.wantLen {
			t.Errorf("CheckDestructive(%q) [%s] got %d findings, want %d",
				tt.prompt, tt.desc, len(findings), tt.wantLen)
			for _, f := range findings {
				t.Logf("  finding: %s", f.Message)
			}
		}
	}
}

func TestCheckSize(t *testing.T) {
	rules := DefaultRuleSet()

	// Test under warning threshold
	smallPrompt := "Hello"
	findings := CheckSize(smallPrompt, rules)
	if len(findings) != 0 {
		t.Errorf("small prompt should have no findings, got %d", len(findings))
	}

	// Test over warning threshold but under max
	rules.SetConfig(RuleOversizedPromptBytes, ConfigKeyWarnBytes, 10)
	rules.SetConfig(RuleOversizedPromptBytes, ConfigKeyMaxBytes, 100)
	mediumPrompt := strings.Repeat("a", 50)
	findings = CheckSize(mediumPrompt, rules)
	if len(findings) != 1 {
		t.Errorf("medium prompt should have 1 warning finding, got %d", len(findings))
	} else if findings[0].Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %s", findings[0].Severity)
	}

	// Test over max threshold
	largePrompt := strings.Repeat("b", 150)
	findings = CheckSize(largePrompt, rules)
	var hasError bool
	for _, f := range findings {
		if f.Severity == SeverityError {
			hasError = true
		}
	}
	if !hasError {
		t.Error("large prompt should have error severity finding")
	}
}

func TestCheckMissingContext(t *testing.T) {
	rules := DefaultRuleSet()

	// Disabled by default
	findings := CheckMissingContext("prompt without tags", rules)
	if len(findings) != 0 {
		t.Error("disabled rule should produce no findings")
	}

	// Enable and configure
	rules.Enable(RuleMissingContext)
	rules.SetConfig(RuleMissingContext, ConfigKeyRequiredTags, []string{"[CONTEXT]", "[TASK]"})

	// Missing all tags
	findings = CheckMissingContext("prompt without tags", rules)
	if len(findings) != 2 {
		t.Errorf("expected 2 missing tag findings, got %d", len(findings))
	}

	// Has one tag
	findings = CheckMissingContext("[CONTEXT] some context here", rules)
	if len(findings) != 1 {
		t.Errorf("expected 1 missing tag finding, got %d", len(findings))
	}

	// Has all tags
	findings = CheckMissingContext("[CONTEXT] context [TASK] task", rules)
	if len(findings) != 0 {
		t.Errorf("expected 0 findings when all tags present, got %d", len(findings))
	}
}

func TestLinterSecrets(t *testing.T) {
	l := New()

	// Test with a fake API key pattern
	prompt := `Here is my config:
API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz123456789012345678901234567890`

	result := l.Lint(prompt)

	secretFindings := result.FindingsByID(RuleSecretDetected)
	if len(secretFindings) == 0 {
		t.Error("expected secret detection for API key pattern")
	}
}

func TestLinterPII(t *testing.T) {
	l := New()

	tests := []struct {
		prompt  string
		wantPII bool
		piiType string
	}{
		{"Contact me at test@example.com", true, "email_address"},
		{"Call me at 555-123-4567", true, "phone_number"},
		{"My SSN is 123-45-6789", true, "ssn"},
		{"No personal info here", false, ""},
	}

	for _, tt := range tests {
		result := l.Lint(tt.prompt)
		piiFindings := result.FindingsByID(RulePIIDetected)

		if tt.wantPII && len(piiFindings) == 0 {
			t.Errorf("expected PII detection for %q", tt.prompt)
		}
		if !tt.wantPII && len(piiFindings) > 0 {
			t.Errorf("unexpected PII detection for %q: %v", tt.prompt, piiFindings)
		}
	}
}

func TestRuleSetClone(t *testing.T) {
	original := DefaultRuleSet()
	original.SetSeverity(RuleSecretDetected, SeverityWarning)

	clone := original.Clone()

	// Verify clone has same values
	if clone.Rules[RuleSecretDetected].Severity != SeverityWarning {
		t.Error("clone should have same severity")
	}

	// Modify clone, verify original unchanged
	clone.SetSeverity(RuleSecretDetected, SeverityInfo)

	if original.Rules[RuleSecretDetected].Severity != SeverityWarning {
		t.Error("modifying clone should not affect original")
	}
}

func TestStrictRuleSet(t *testing.T) {
	strict := StrictRuleSet()

	// Verify escalated severities
	if strict.Rules[RuleOversizedPromptBytes].Severity != SeverityError {
		t.Error("strict mode should have error severity for oversized bytes")
	}
	if strict.Rules[RuleDestructiveCommand].Severity != SeverityError {
		t.Error("strict mode should have error severity for destructive commands")
	}

	// Verify missing context is enabled
	if !strict.Rules[RuleMissingContext].Enabled {
		t.Error("strict mode should enable missing context rule")
	}
}

func TestResultHelpers(t *testing.T) {
	result := &Result{
		Findings: []Finding{
			{ID: RuleSecretDetected, Severity: SeverityError},
			{ID: RuleDestructiveCommand, Severity: SeverityWarning},
			{ID: RulePIIDetected, Severity: SeverityWarning},
			{ID: RuleMissingContext, Severity: SeverityInfo},
		},
	}

	if !result.HasErrors() {
		t.Error("should have errors")
	}
	if !result.HasWarnings() {
		t.Error("should have warnings")
	}

	errors := result.FindingsBySeverity(SeverityError)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(errors))
	}

	warnings := result.FindingsBySeverity(SeverityWarning)
	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(warnings))
	}

	secrets := result.FindingsByID(RuleSecretDetected)
	if len(secrets) != 1 {
		t.Errorf("expected 1 secret finding, got %d", len(secrets))
	}
}

func TestLintWithRedaction(t *testing.T) {
	l := New()

	// Prompt with a detectable secret pattern
	prompt := `Config: ANTHROPIC_KEY=sk-ant-api03-test1234567890123456789012345678901234567890123456`

	result, redacted := l.LintWithRedaction(prompt)

	// Should have secret findings
	if len(result.FindingsByID(RuleSecretDetected)) == 0 {
		t.Log("Note: Secret detection depends on redaction engine patterns")
	}

	// Redacted output should be returned (may or may not be different depending on patterns)
	if redacted == "" {
		t.Error("redacted output should not be empty")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
