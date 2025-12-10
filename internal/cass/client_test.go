package cass

import (
	"context"
	"testing"
	"time"
)

func TestClient_IsInstalled(t *testing.T) {
	c := NewClient()
	// This test just verifies the method works, not whether cass is installed
	_ = c.IsInstalled()
}

func TestClient_WithOptions(t *testing.T) {
	c := NewClient(
		WithTimeout(5*time.Second),
		WithCASSPath("/custom/path/cass"),
	)

	if c.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %v", c.Timeout)
	}
	if c.cassPath != "/custom/path/cass" {
		t.Errorf("expected cassPath /custom/path/cass, got %s", c.cassPath)
	}
}

func TestSearchOptions(t *testing.T) {
	opts := SearchOptions{
		Query:     "authentication error",
		Limit:     5,
		Agent:     "claude-code",
		Workspace: "myproject",
		Days:      7,
		Fields:    "minimal",
	}

	if opts.Query != "authentication error" {
		t.Errorf("expected query 'authentication error', got %s", opts.Query)
	}
	if opts.Limit != 5 {
		t.Errorf("expected limit 5, got %d", opts.Limit)
	}
}

func TestTimelineOptions(t *testing.T) {
	opts := TimelineOptions{
		Limit:     10,
		Agent:     "codex",
		Workspace: "backend",
		Days:      30,
	}

	if opts.Limit != 10 {
		t.Errorf("expected limit 10, got %d", opts.Limit)
	}
	if opts.Agent != "codex" {
		t.Errorf("expected agent codex, got %s", opts.Agent)
	}
}

func TestClient_SearchEmptyQuery(t *testing.T) {
	c := NewClient()
	ctx := context.Background()

	_, err := c.Search(ctx, SearchOptions{})
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestClient_ViewEmptyPath(t *testing.T) {
	c := NewClient()
	ctx := context.Background()

	_, err := c.View(ctx, "", 0)
	if err == nil {
		t.Error("expected error for empty source path")
	}
}

func TestClient_ExpandValidation(t *testing.T) {
	c := NewClient()
	ctx := context.Background()

	// Empty path
	_, err := c.Expand(ctx, "", 10, 3)
	if err == nil {
		t.Error("expected error for empty source path")
	}

	// Zero line number
	_, err = c.Expand(ctx, "/path/to/file", 0, 3)
	if err == nil {
		t.Error("expected error for zero line number")
	}
}

func TestCASSError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      CASSError
		expected string
	}{
		{
			name: "without hint",
			err: CASSError{
				Code:    404,
				Kind:    "not_found",
				Message: "Session not found",
			},
			expected: "Session not found",
		},
		{
			name: "with hint",
			err: CASSError{
				Code:    429,
				Kind:    "rate_limit",
				Message: "Rate limit exceeded",
				Hint:    "Wait 60 seconds",
			},
			expected: "Rate limit exceeded (hint: Wait 60 seconds)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSearchResponse_HasResults(t *testing.T) {
	tests := []struct {
		name     string
		resp     SearchResponse
		expected bool
	}{
		{
			name:     "empty",
			resp:     SearchResponse{},
			expected: false,
		},
		{
			name: "with hits",
			resp: SearchResponse{
				Hits: []SearchHit{{SourcePath: "/path"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resp.HasResults(); got != tt.expected {
				t.Errorf("HasResults() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSearchResponse_HasMore(t *testing.T) {
	tests := []struct {
		name     string
		resp     SearchResponse
		expected bool
	}{
		{
			name:     "no more results",
			resp:     SearchResponse{Offset: 0, Count: 10, TotalMatches: 10},
			expected: false,
		},
		{
			name:     "more results by count",
			resp:     SearchResponse{Offset: 0, Count: 10, TotalMatches: 20},
			expected: true,
		},
		{
			name: "more results by cursor",
			resp: SearchResponse{
				Offset:       0,
				Count:        10,
				TotalMatches: 10,
				Meta:         &Meta{NextCursor: "abc123"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resp.HasMore(); got != tt.expected {
				t.Errorf("HasMore() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStatusResponse_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		status   StatusResponse
		expected bool
	}{
		{
			name: "all healthy",
			status: StatusResponse{
				Healthy:  true,
				Index:    IndexInfo{Healthy: true},
				Database: DBInfo{Healthy: true},
			},
			expected: true,
		},
		{
			name: "unhealthy overall",
			status: StatusResponse{
				Healthy:  false,
				Index:    IndexInfo{Healthy: true},
				Database: DBInfo{Healthy: true},
			},
			expected: false,
		},
		{
			name: "unhealthy index",
			status: StatusResponse{
				Healthy:  true,
				Index:    IndexInfo{Healthy: false},
				Database: DBInfo{Healthy: true},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsHealthy(); got != tt.expected {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCapabilities_HasFeature(t *testing.T) {
	caps := Capabilities{
		Features: []string{"search", "view", "expand"},
	}

	if !caps.HasFeature("search") {
		t.Error("expected HasFeature('search') = true")
	}
	if caps.HasFeature("missing") {
		t.Error("expected HasFeature('missing') = false")
	}
}

func TestCapabilities_HasConnector(t *testing.T) {
	caps := Capabilities{
		Connectors: []string{"claude-code", "codex", "cursor"},
	}

	if !caps.HasConnector("claude-code") {
		t.Error("expected HasConnector('claude-code') = true")
	}
	if caps.HasConnector("gemini") {
		t.Error("expected HasConnector('gemini') = false")
	}
}

func TestSearchHit_CreatedAtTime(t *testing.T) {
	// Nil timestamp
	hit := SearchHit{}
	if !hit.CreatedAtTime().IsZero() {
		t.Error("expected zero time for nil timestamp")
	}

	// Valid timestamp
	ts := int64(1704067200) // 2024-01-01 00:00:00 UTC
	hit = SearchHit{CreatedAt: &ts}
	expected := time.Unix(1704067200, 0)
	if !hit.CreatedAtTime().Equal(expected) {
		t.Errorf("CreatedAtTime() = %v, want %v", hit.CreatedAtTime(), expected)
	}
}

func TestIndexInfo_SizeMB(t *testing.T) {
	info := IndexInfo{SizeBytes: 10 * 1024 * 1024} // 10 MB
	if got := info.SizeMB(); got != 10.0 {
		t.Errorf("SizeMB() = %v, want 10.0", got)
	}
}

func TestPending_HasPending(t *testing.T) {
	tests := []struct {
		name     string
		pending  Pending
		expected bool
	}{
		{
			name:     "no pending",
			pending:  Pending{},
			expected: false,
		},
		{
			name:     "pending sessions",
			pending:  Pending{Sessions: 5},
			expected: true,
		},
		{
			name:     "pending files",
			pending:  Pending{Files: 3},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pending.HasPending(); got != tt.expected {
				t.Errorf("HasPending() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient()
	if c.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultTimeout, c.Timeout)
	}
	if c.cassPath != "" {
		t.Errorf("expected empty cassPath, got %s", c.cassPath)
	}
}

func TestStatusResponse_NeedsAction(t *testing.T) {
	tests := []struct {
		name     string
		status   StatusResponse
		expected bool
	}{
		{
			name:     "no action needed",
			status:   StatusResponse{RecommendedAction: ""},
			expected: false,
		},
		{
			name:     "action needed",
			status:   StatusResponse{RecommendedAction: "run 'cass reindex'"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.NeedsAction(); got != tt.expected {
				t.Errorf("NeedsAction() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestViewResponse(t *testing.T) {
	resp := ViewResponse{
		SourcePath: "/path/to/session.jsonl",
		Agent:      "claude-code",
		Workspace:  "myproject",
		Title:      "Debug session",
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	}

	if resp.Agent != "claude-code" {
		t.Errorf("Agent = %q, want %q", resp.Agent, "claude-code")
	}
	if len(resp.Messages) != 2 {
		t.Errorf("len(Messages) = %d, want 2", len(resp.Messages))
	}
}

func TestExpandResponse(t *testing.T) {
	resp := ExpandResponse{
		SourcePath:    "/path/to/session.jsonl",
		CenterLine:    42,
		ContextBefore: 5,
		ContextAfter:  5,
		Messages: []Message{
			{Role: "user", Content: "What's wrong?"},
		},
	}

	if resp.CenterLine != 42 {
		t.Errorf("CenterLine = %d, want 42", resp.CenterLine)
	}
}

func TestTimelineResponse(t *testing.T) {
	resp := TimelineResponse{
		Entries: []TimelineEntry{
			{Timestamp: 1702200000, Agent: "claude-code", MessageCount: 10},
			{Timestamp: 1702300000, Agent: "codex", MessageCount: 5},
		},
		Count: 2,
	}

	if resp.Count != 2 {
		t.Errorf("Count = %d, want 2", resp.Count)
	}
	if len(resp.Entries) != 2 {
		t.Errorf("len(Entries) = %d, want 2", len(resp.Entries))
	}
}

func TestAggregations(t *testing.T) {
	agg := Aggregations{
		ByAgent: []AgentCount{
			{Agent: "claude-code", Count: 50},
			{Agent: "codex", Count: 30},
		},
		ByWorkspace: []WorkspaceCount{
			{Workspace: "frontend", Count: 40},
			{Workspace: "backend", Count: 40},
		},
		TotalSessions: 80,
	}

	if len(agg.ByAgent) != 2 {
		t.Errorf("len(ByAgent) = %d, want 2", len(agg.ByAgent))
	}
	if agg.TotalSessions != 80 {
		t.Errorf("TotalSessions = %d, want 80", agg.TotalSessions)
	}
}

func TestDateRange(t *testing.T) {
	dr := DateRange{
		Earliest: "2024-01-01",
		Latest:   "2024-12-31",
	}

	if dr.Earliest != "2024-01-01" {
		t.Errorf("Earliest = %q, want %q", dr.Earliest, "2024-01-01")
	}
}

func TestLimits(t *testing.T) {
	limits := Limits{
		MaxQueryLength:       1000,
		MaxResults:           100,
		MaxConcurrentQueries: 10,
		RateLimitPerMinute:   60,
	}

	if limits.MaxResults != 100 {
		t.Errorf("MaxResults = %d, want 100", limits.MaxResults)
	}
}
