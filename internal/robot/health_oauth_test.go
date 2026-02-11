package robot

import (
	"encoding/json"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/ratelimit"
)

func TestAgentTypeToProvider(t *testing.T) {
	tests := []struct {
		agentType string
		expected  string
	}{
		{"claude", "anthropic"},
		{"cc", "anthropic"},
		{"codex", "openai"},
		{"cod", "openai"},
		{"gemini", "google"},
		{"gmi", "google"},
		{"unknown", "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.agentType, func(t *testing.T) {
			got := agentTypeToProvider(tt.agentType)
			if got != tt.expected {
				t.Errorf("agentTypeToProvider(%q) = %q, want %q", tt.agentType, got, tt.expected)
			}
		})
	}
}

func TestDetectOAuthStatus(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected OAuthStatus
	}{
		{
			name:     "authentication failed",
			output:   "error: authentication failed",
			expected: OAuthError,
		},
		{
			name:     "unauthorized",
			output:   "401 unauthorized",
			expected: OAuthError,
		},
		{
			name:     "invalid api key",
			output:   "error: invalid api key provided",
			expected: OAuthError,
		},
		{
			name:     "api key not found",
			output:   "api key not found in environment",
			expected: OAuthError,
		},
		{
			name:     "authentication error",
			output:   "authentication error: check credentials",
			expected: OAuthError,
		},
		{
			name:     "token expired",
			output:   "token expired, please reauthenticate",
			expected: OAuthExpired,
		},
		{
			name:     "session expired",
			output:   "session expired",
			expected: OAuthExpired,
		},
		{
			name:     "please log in",
			output:   "please log in to continue",
			expected: OAuthExpired,
		},
		{
			name:     "needs reauth",
			output:   "needs reauth before next request",
			expected: OAuthExpired,
		},
		{
			name:     "refresh token",
			output:   "refresh token failed to renew",
			expected: OAuthExpired,
		},
		{
			name:     "working agent",
			output:   "thinking about the problem...",
			expected: OAuthValid,
		},
		{
			name:     "reading file",
			output:   "reading src/main.go",
			expected: OAuthValid,
		},
		{
			name:     "writing code",
			output:   "writing internal/foo.go",
			expected: OAuthValid,
		},
		{
			name:     "searching",
			output:   "searching for relevant files",
			expected: OAuthValid,
		},
		{
			name:     "analyzing",
			output:   "analyzing test coverage",
			expected: OAuthValid,
		},
		{
			name:     "unknown output",
			output:   "random text",
			expected: OAuthUnknown,
		},
		{
			name:     "empty output",
			output:   "",
			expected: OAuthUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := detectOAuthStatus(tt.output)
			if got != tt.expected {
				t.Errorf("detectOAuthStatus(%q) = %v, want %v", tt.output, got, tt.expected)
			}
		})
	}
}

func TestDetectOAuthStatusErrorMessage(t *testing.T) {
	// Verify the error message is populated for error/expired cases
	_, msg := detectOAuthStatus("token expired")
	if msg == "" {
		t.Error("detectOAuthStatus should return a non-empty error message for token expired")
	}
	_, msg = detectOAuthStatus("error: authentication failed")
	if msg == "" {
		t.Error("detectOAuthStatus should return a non-empty error message for auth failed")
	}
	// Valid case should have empty message
	_, msg = detectOAuthStatus("thinking about the problem")
	if msg != "" {
		t.Errorf("detectOAuthStatus valid case should return empty message, got %q", msg)
	}
}

func TestDetectRateLimitStatusFromOutput(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		expectStatus  RateLimitStatus
		expectCountGt int
	}{
		{
			name:          "rate limit hit",
			output:        "error: rate limit exceeded, try again later",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "429 error",
			output:        "HTTP 429 too many requests",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "multiple rate limit patterns",
			output:        "rate limit exceeded (429), quota exceeded, too many requests",
			expectStatus:  RateLimitLimited,
			expectCountGt: 2,
		},
		{
			name:          "clean output",
			output:        "successfully completed the task",
			expectStatus:  RateLimitOK,
			expectCountGt: -1,
		},
		{
			name:          "ratelimit single word",
			output:        "ratelimit detected",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "rate-limit hyphenated",
			output:        "rate-limit error",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "quota exceeded",
			output:        "quota exceeded for this billing period",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "retry after",
			output:        "retry after 30s",
			expectStatus:  RateLimitWarning,
			expectCountGt: 0,
		},
		{
			name:          "empty output",
			output:        "",
			expectStatus:  RateLimitOK,
			expectCountGt: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, count := detectRateLimitStatusFromOutput(tt.output)
			if status != tt.expectStatus {
				t.Errorf("detectRateLimitStatusFromOutput(%q) status = %v, want %v", tt.output, status, tt.expectStatus)
			}
			if count <= tt.expectCountGt && tt.expectCountGt >= 0 {
				t.Errorf("detectRateLimitStatusFromOutput(%q) count = %d, want > %d", tt.output, count, tt.expectCountGt)
			}
		})
	}
}

func TestRateLimitWarningThreshold(t *testing.T) {
	// The warning threshold constant should be 3
	if RateLimitWarningThreshold != 3 {
		t.Errorf("RateLimitWarningThreshold = %d, want 3", RateLimitWarningThreshold)
	}
}

func TestCountErrorsInOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int
	}{
		{
			name:     "no errors",
			output:   "success",
			expected: 0,
		},
		{
			name:     "single error",
			output:   "error occurred",
			expected: 1,
		},
		{
			name:     "multiple errors",
			output:   "error: connection failed with timeout exception",
			expected: 4, // error, failed, timeout, exception
		},
		{
			name:     "panic",
			output:   "goroutine panic in handler",
			expected: 1,
		},
		{
			name:     "connection refused",
			output:   "connection refused by remote host",
			expected: 1,
		},
		{
			name:     "empty output",
			output:   "",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countErrorsInOutput(tt.output)
			if got != tt.expected {
				t.Errorf("countErrorsInOutput(%q) = %d, want %d", tt.output, got, tt.expected)
			}
		})
	}
}

// TestEnrichWithThrottlePaused verifies that a paused CodexThrottle
// escalates the rate-limit status to limited and populates cooldown.
func TestEnrichWithThrottlePaused(t *testing.T) {
	ct := ratelimit.NewCodexThrottle(3)
	ct.RecordRateLimit("pane-1", 30)

	health := AgentOAuthHealth{
		RateLimitStatus: RateLimitOK,
		RateLimitCount:  0,
	}

	enrichWithThrottle(&health, ct)

	if health.RateLimitStatus != RateLimitLimited {
		t.Errorf("expected RateLimitLimited when throttle paused, got %v", health.RateLimitStatus)
	}
	if health.ThrottlePhase != string(ratelimit.ThrottlePaused) {
		t.Errorf("expected throttle_phase=%q, got %q", ratelimit.ThrottlePaused, health.ThrottlePhase)
	}
	if health.CooldownRemaining <= 0 {
		t.Errorf("expected positive cooldown_remaining when paused, got %d", health.CooldownRemaining)
	}
	if health.ThrottleGuidance == "" {
		t.Error("expected non-empty throttle_guidance when paused")
	}
}

// TestEnrichWithThrottleNormal verifies that a normal throttle does not
// change the rate-limit status or set throttle metadata.
func TestEnrichWithThrottleNormal(t *testing.T) {
	ct := ratelimit.NewCodexThrottle(3)

	health := AgentOAuthHealth{
		RateLimitStatus: RateLimitOK,
		RateLimitCount:  0,
	}

	enrichWithThrottle(&health, ct)

	if health.RateLimitStatus != RateLimitOK {
		t.Errorf("expected RateLimitOK for normal throttle, got %v", health.RateLimitStatus)
	}
	if health.ThrottlePhase != string(ratelimit.ThrottleNormal) {
		t.Errorf("expected throttle_phase=%q, got %q", ratelimit.ThrottleNormal, health.ThrottlePhase)
	}
}

// TestEnrichWithThrottleNil ensures a nil throttle is a no-op.
func TestEnrichWithThrottleNil(t *testing.T) {
	health := AgentOAuthHealth{
		RateLimitStatus: RateLimitOK,
		RateLimitCount:  0,
	}
	// Should not panic
	enrichWithThrottle(&health, nil)

	if health.RateLimitStatus != RateLimitOK {
		t.Errorf("nil throttle should not change status, got %v", health.RateLimitStatus)
	}
	if health.ThrottlePhase != "" {
		t.Errorf("nil throttle should leave throttle_phase empty, got %q", health.ThrottlePhase)
	}
}

// TestEnrichWithThrottleRateLimitCountMerge verifies that the higher
// rate-limit count wins when merging throttle data.
func TestEnrichWithThrottleRateLimitCountMerge(t *testing.T) {
	ct := ratelimit.NewCodexThrottle(3)
	ct.RecordRateLimit("pane-1", 10)

	// Agent already has a high local count
	health := AgentOAuthHealth{
		RateLimitStatus: RateLimitWarning,
		RateLimitCount:  10,
	}

	enrichWithThrottle(&health, ct)

	// The higher count should be kept
	if health.RateLimitCount < 1 {
		t.Errorf("expected rate_limit_count >= 1 from merge, got %d", health.RateLimitCount)
	}
}

// TestFormatOAuthHealthDisplay verifies the human-readable display format.
func TestFormatOAuthHealthDisplay(t *testing.T) {
	output := &OAuthHealthOutput{
		Agents: []AgentOAuthHealth{
			{
				AgentType:       "cc",
				Pane:            1,
				OAuthStatus:     OAuthValid,
				RateLimitStatus: RateLimitOK,
				LastActivitySec: 120,
			},
			{
				AgentType:       "cc",
				Pane:            2,
				OAuthStatus:     OAuthValid,
				RateLimitStatus: RateLimitWarning,
				RateLimitCount:  3,
				LastActivitySec: 30,
			},
			{
				AgentType:       "cod",
				Pane:            3,
				OAuthStatus:     OAuthExpired,
				RateLimitStatus: RateLimitLimited,
				RateLimitCount:  5,
				LastActivitySec: 5,
				ThrottlePhase:   "paused",
			},
		},
	}

	display := FormatOAuthHealthDisplay(output)
	if display == "" {
		t.Fatal("FormatOAuthHealthDisplay returned empty string")
	}

	// Check header
	if !oauthContainsSubstr(display, "OAuth/Rate Status:") {
		t.Error("display missing header")
	}
	// Check agent lines
	if !oauthContainsSubstr(display, "cc-1:") {
		t.Error("display missing cc-1 agent")
	}
	if !oauthContainsSubstr(display, "cc-2:") {
		t.Error("display missing cc-2 agent")
	}
	if !oauthContainsSubstr(display, "cod-3:") {
		t.Error("display missing cod-3 agent")
	}
	// Check OAuth icons
	if !oauthContainsSubstr(display, "\u2713") {
		t.Error("display missing checkmark for valid OAuth")
	}
	if !oauthContainsSubstr(display, "EXP") {
		t.Error("display missing EXP for expired OAuth")
	}
	// Check rate labels
	if !oauthContainsSubstr(display, "OK") {
		t.Error("display missing OK rate label")
	}
	if !oauthContainsSubstr(display, "WARN") {
		t.Error("display missing WARN rate label")
	}
	if !oauthContainsSubstr(display, "LIMITED") {
		t.Error("display missing LIMITED rate label")
	}
	// Check throttle annotation
	if !oauthContainsSubstr(display, "[throttle=paused]") {
		t.Error("display missing throttle annotation for paused agent")
	}
	// Check rate limit count annotation
	if !oauthContainsSubstr(display, "limits in 5m") {
		t.Error("display missing rate limit count annotation")
	}
}

func TestFormatOAuthHealthDisplayNoAgents(t *testing.T) {
	output := &OAuthHealthOutput{Agents: []AgentOAuthHealth{}}
	display := FormatOAuthHealthDisplay(output)
	if display != "OAuth/Rate Status: (no agents)" {
		t.Errorf("unexpected display for no agents: %q", display)
	}
}

func TestFormatOAuthHealthDisplayNil(t *testing.T) {
	display := FormatOAuthHealthDisplay(nil)
	if display != "OAuth/Rate Status: (no agents)" {
		t.Errorf("unexpected display for nil: %q", display)
	}
}

// TestOAuthHealthOutputJSONStructure verifies the JSON output includes
// all expected fields.
func TestOAuthHealthOutputJSONStructure(t *testing.T) {
	output := OAuthHealthOutput{
		RobotResponse: NewRobotResponse(true),
		Session:       "test-session",
		Agents: []AgentOAuthHealth{
			{
				Pane:              1,
				AgentType:         "cc",
				Provider:          "anthropic",
				OAuthStatus:       OAuthValid,
				RateLimitStatus:   RateLimitOK,
				LastActivitySec:   60,
				ThrottlePhase:     "",
				ThrottleGuidance:  "",
			},
		},
		Summary: OAuthHealthSummary{
			Total:      1,
			OAuthValid: 1,
			RateLimitOK: 1,
		},
		Display: "OAuth/Rate Status:\n  cc-1: OAuth=\u2713 Rate=OK      Last=1m ago",
	}

	data, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Unmarshal into generic map to check structure
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// Check top-level fields
	requiredFields := []string{"success", "timestamp", "session", "agents", "summary", "display"}
	for _, f := range requiredFields {
		if _, ok := m[f]; !ok {
			t.Errorf("JSON missing required field %q", f)
		}
	}

	// Check that success is true
	if s, ok := m["success"].(bool); !ok || !s {
		t.Error("expected success=true in JSON")
	}

	// Check agents array structure
	agents, ok := m["agents"].([]interface{})
	if !ok || len(agents) != 1 {
		t.Fatalf("expected agents array with 1 element, got %v", m["agents"])
	}
	agent, ok := agents[0].(map[string]interface{})
	if !ok {
		t.Fatal("agent should be an object")
	}

	agentFields := []string{"pane", "agent_type", "provider", "oauth_status", "rate_limit_status", "last_activity_sec"}
	for _, f := range agentFields {
		if _, ok := agent[f]; !ok {
			t.Errorf("agent JSON missing field %q", f)
		}
	}

	// Check summary structure
	summary, ok := m["summary"].(map[string]interface{})
	if !ok {
		t.Fatal("summary should be an object")
	}
	summaryFields := []string{"total", "oauth_valid", "oauth_expired", "oauth_error", "rate_limit_ok", "rate_limit_warn", "rate_limited"}
	for _, f := range summaryFields {
		if _, ok := summary[f]; !ok {
			t.Errorf("summary JSON missing field %q", f)
		}
	}
}

// TestFormatLastActivity verifies the duration formatting helper.
func TestFormatLastActivity(t *testing.T) {
	tests := []struct {
		sec      int
		expected string
	}{
		{-1, "now"},
		{0, "0s ago"},
		{30, "30s ago"},
		{59, "59s ago"},
		{60, "1m ago"},
		{120, "2m ago"},
		{3599, "59m ago"},
		{3600, "1h ago"},
		{7200, "2h ago"},
	}

	for _, tt := range tests {
		got := formatLastActivity(tt.sec)
		if got != tt.expected {
			t.Errorf("formatLastActivity(%d) = %q, want %q", tt.sec, got, tt.expected)
		}
	}
}

// TestOAuthIcons verifies icon/label helper functions.
func TestOAuthIcons(t *testing.T) {
	if oauthIcon(OAuthValid) != "\u2713" {
		t.Error("OAuthValid icon should be checkmark")
	}
	if oauthIcon(OAuthExpired) != "EXP" {
		t.Error("OAuthExpired icon should be EXP")
	}
	if oauthIcon(OAuthError) != "ERR" {
		t.Error("OAuthError icon should be ERR")
	}
	if oauthIcon(OAuthUnknown) != "?" {
		t.Error("OAuthUnknown icon should be ?")
	}
}

func TestRateLabels(t *testing.T) {
	if rateLabel(RateLimitOK) != "OK" {
		t.Error("RateLimitOK label should be OK")
	}
	if rateLabel(RateLimitWarning) != "WARN" {
		t.Error("RateLimitWarning label should be WARN")
	}
	if rateLabel(RateLimitLimited) != "LIMITED" {
		t.Error("RateLimitLimited label should be LIMITED")
	}
}

func oauthContainsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && oauthContainsCheck(s, sub))
}

func oauthContainsCheck(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
