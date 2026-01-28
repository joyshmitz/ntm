package robot

import (
	"testing"
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
			name:     "unknown output",
			output:   "random text",
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
