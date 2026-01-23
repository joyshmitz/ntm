package swarm

import (
	"testing"
	"time"
)

func TestNewAccountRotator(t *testing.T) {
	rotator := NewAccountRotator()

	if rotator == nil {
		t.Fatal("NewAccountRotator returned nil")
	}

	if rotator.caamPath != "caam" {
		t.Errorf("expected caamPath 'caam', got %q", rotator.caamPath)
	}

	if rotator.CommandTimeout != 5*time.Second {
		t.Errorf("expected CommandTimeout 5s, got %v", rotator.CommandTimeout)
	}

	if rotator.Logger == nil {
		t.Error("expected non-nil Logger")
	}

	if rotator.rotationHistory == nil {
		t.Error("expected rotationHistory to be initialized")
	}
}

func TestAccountRotatorWithMethods(t *testing.T) {
	rotator := NewAccountRotator()

	// Test WithCaamPath
	result := rotator.WithCaamPath("/custom/path/caam")
	if result != rotator {
		t.Error("WithCaamPath should return the same rotator for chaining")
	}
	if rotator.caamPath != "/custom/path/caam" {
		t.Errorf("expected caamPath '/custom/path/caam', got %q", rotator.caamPath)
	}

	// Test WithLogger
	result = rotator.WithLogger(nil)
	if result != rotator {
		t.Error("WithLogger should return the same rotator for chaining")
	}

	// Test WithCommandTimeout
	customTimeout := 10 * time.Second
	result = rotator.WithCommandTimeout(customTimeout)
	if result != rotator {
		t.Error("WithCommandTimeout should return the same rotator for chaining")
	}
	if rotator.CommandTimeout != customTimeout {
		t.Errorf("expected CommandTimeout %v, got %v", customTimeout, rotator.CommandTimeout)
	}
}

func TestNormalizeProvider(t *testing.T) {
	tests := []struct {
		agentType string
		expected  string
	}{
		{"cc", "claude"},
		{"claude", "claude"},
		{"claude-code", "claude"},
		{"cod", "openai"},
		{"codex", "openai"},
		{"gmi", "google"},
		{"gemini", "google"},
		{"unknown", "unknown"},
		{"claude", "claude"},
		{"openai", "openai"},
		{"google", "google"},
	}

	for _, tt := range tests {
		t.Run(tt.agentType, func(t *testing.T) {
			result := normalizeProvider(tt.agentType)
			if result != tt.expected {
				t.Errorf("normalizeProvider(%q) = %q, want %q", tt.agentType, result, tt.expected)
			}
		})
	}
}

func TestAccountRotatorLogger(t *testing.T) {
	rotator := NewAccountRotator()
	logger := rotator.logger()

	if logger == nil {
		t.Error("expected non-nil logger from logger()")
	}
}

func TestAccountRotatorRotationHistory(t *testing.T) {
	rotator := NewAccountRotator()

	// Initially empty
	history := rotator.GetRotationHistory(10)
	if len(history) != 0 {
		t.Errorf("expected empty history, got %d records", len(history))
	}

	if rotator.RotationCount() != 0 {
		t.Errorf("expected 0 rotation count, got %d", rotator.RotationCount())
	}

	// Add some records manually for testing
	rotator.mu.Lock()
	rotator.rotationHistory = append(rotator.rotationHistory, RotationRecord{
		Provider:    "claude",
		FromAccount: "account1",
		ToAccount:   "account2",
		RotatedAt:   time.Now(),
		TriggeredBy: "limit_hit",
	})
	rotator.rotationHistory = append(rotator.rotationHistory, RotationRecord{
		Provider:    "openai",
		FromAccount: "work",
		ToAccount:   "personal",
		RotatedAt:   time.Now(),
		TriggeredBy: "manual",
	})
	rotator.mu.Unlock()

	// Check count
	if rotator.RotationCount() != 2 {
		t.Errorf("expected 2 rotation count, got %d", rotator.RotationCount())
	}

	// Get all history
	history = rotator.GetRotationHistory(10)
	if len(history) != 2 {
		t.Errorf("expected 2 records, got %d", len(history))
	}

	// Get limited history
	history = rotator.GetRotationHistory(1)
	if len(history) != 1 {
		t.Errorf("expected 1 record with limit, got %d", len(history))
	}

	// Clear history
	rotator.ClearRotationHistory()
	if rotator.RotationCount() != 0 {
		t.Errorf("expected 0 after clear, got %d", rotator.RotationCount())
	}
}

func TestAccountRotatorGetRotationHistoryZeroLimit(t *testing.T) {
	rotator := NewAccountRotator()

	// Add a record
	rotator.mu.Lock()
	rotator.rotationHistory = append(rotator.rotationHistory, RotationRecord{
		Provider:    "claude",
		FromAccount: "a",
		ToAccount:   "b",
		RotatedAt:   time.Now(),
	})
	rotator.mu.Unlock()

	// Zero limit should return all
	history := rotator.GetRotationHistory(0)
	if len(history) != 1 {
		t.Errorf("expected all records with 0 limit, got %d", len(history))
	}

	// Negative limit should return all
	history = rotator.GetRotationHistory(-5)
	if len(history) != 1 {
		t.Errorf("expected all records with negative limit, got %d", len(history))
	}
}

func TestAccountRotatorIsAvailableWithInvalidPath(t *testing.T) {
	rotator := NewAccountRotator().WithCaamPath("/nonexistent/path/to/caam")

	// Should return false for invalid path
	if rotator.IsAvailable() {
		t.Error("expected IsAvailable to return false for invalid path")
	}

	// Should cache the result
	if rotator.IsAvailable() {
		t.Error("expected cached result to be false")
	}
}

func TestAccountRotatorResetAvailabilityCheck(t *testing.T) {
	rotator := NewAccountRotator().WithCaamPath("/nonexistent/path/to/caam")

	// Check availability (will be false and cached)
	_ = rotator.IsAvailable()

	// Reset and check internal state
	rotator.ResetAvailabilityCheck()

	if rotator.availabilityChecked {
		t.Error("expected availabilityChecked to be false after reset")
	}
	if rotator.availabilityResult {
		t.Error("expected availabilityResult to be false after reset")
	}
}

func TestAccountRotatorGracefulDegradation(t *testing.T) {
	rotator := NewAccountRotator().WithCaamPath("/nonexistent/path/to/caam")

	// GetCurrentAccount should return error
	_, err := rotator.GetCurrentAccount("cc")
	if err == nil {
		t.Error("expected error when caam is unavailable")
	}

	// ListAccounts should return error
	_, err = rotator.ListAccounts("cc")
	if err == nil {
		t.Error("expected error when caam is unavailable")
	}

	// SwitchAccount should return error
	_, err = rotator.SwitchAccount("cc")
	if err == nil {
		t.Error("expected error when caam is unavailable")
	}

	// SwitchToAccount should return error
	_, err = rotator.SwitchToAccount("cc", "test")
	if err == nil {
		t.Error("expected error when caam is unavailable")
	}

	// RotateAccount should return error
	_, err = rotator.RotateAccount("cc")
	if err == nil {
		t.Error("expected error when caam is unavailable")
	}

	// CurrentAccount should return empty string
	account := rotator.CurrentAccount("cc")
	if account != "" {
		t.Errorf("expected empty account when caam unavailable, got %q", account)
	}
}

func TestAccountInfoFields(t *testing.T) {
	now := time.Now()
	info := AccountInfo{
		Provider:    "claude",
		AccountName: "personal",
		IsActive:    true,
		LastUsed:    now,
	}

	if info.Provider != "claude" {
		t.Errorf("unexpected Provider: %s", info.Provider)
	}
	if info.AccountName != "personal" {
		t.Errorf("unexpected AccountName: %s", info.AccountName)
	}
	if !info.IsActive {
		t.Error("expected IsActive to be true")
	}
	if !info.LastUsed.Equal(now) {
		t.Errorf("unexpected LastUsed: %v", info.LastUsed)
	}
}

func TestRotationRecordFields(t *testing.T) {
	now := time.Now()
	record := RotationRecord{
		Provider:    "openai",
		FromAccount: "work",
		ToAccount:   "personal",
		RotatedAt:   now,
		SessionPane: "test:1.1",
		TriggeredBy: "limit_hit",
	}

	if record.Provider != "openai" {
		t.Errorf("unexpected Provider: %s", record.Provider)
	}
	if record.FromAccount != "work" {
		t.Errorf("unexpected FromAccount: %s", record.FromAccount)
	}
	if record.ToAccount != "personal" {
		t.Errorf("unexpected ToAccount: %s", record.ToAccount)
	}
	if record.SessionPane != "test:1.1" {
		t.Errorf("unexpected SessionPane: %s", record.SessionPane)
	}
	if record.TriggeredBy != "limit_hit" {
		t.Errorf("unexpected TriggeredBy: %s", record.TriggeredBy)
	}
}

func TestAgentToProviderMap(t *testing.T) {
	// Verify the map contains expected entries
	expectedMappings := map[string]string{
		"cc":          "claude",
		"claude":      "claude",
		"claude-code": "claude",
		"cod":         "openai",
		"codex":       "openai",
		"gmi":         "google",
		"gemini":      "google",
	}

	for agent, expected := range expectedMappings {
		if provider, ok := agentToProvider[agent]; !ok {
			t.Errorf("agentToProvider missing entry for %q", agent)
		} else if provider != expected {
			t.Errorf("agentToProvider[%q] = %q, want %q", agent, provider, expected)
		}
	}
}

func TestAccountRotatorImplementsInterface(t *testing.T) {
	// Verify AccountRotator implements the AccountRotator interface used by AutoRespawner
	rotator := NewAccountRotator()

	// These methods must exist for the interface
	var _ = rotator.RotateAccount
	var _ = rotator.CurrentAccount
}

func TestCaamStatusStruct(t *testing.T) {
	status := caamStatus{
		Provider:      "claude",
		ActiveAccount: "personal",
		AccountCount:  3,
	}

	if status.Provider != "claude" {
		t.Errorf("unexpected Provider: %s", status.Provider)
	}
	if status.ActiveAccount != "personal" {
		t.Errorf("unexpected ActiveAccount: %s", status.ActiveAccount)
	}
	if status.AccountCount != 3 {
		t.Errorf("unexpected AccountCount: %d", status.AccountCount)
	}
}

func TestCaamAccountStruct(t *testing.T) {
	account := caamAccount{
		Name:   "work",
		Active: false,
	}

	if account.Name != "work" {
		t.Errorf("unexpected Name: %s", account.Name)
	}
	if account.Active {
		t.Error("expected Active to be false")
	}
}
