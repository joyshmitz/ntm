package cass

import (
	"fmt"
	"time"
)

// SearchHit represents a single search result from CASS
type SearchHit struct {
	SourcePath  string  `json:"source_path"`
	LineNumber  *int    `json:"line_number,omitempty"`
	Agent       string  `json:"agent"`
	Workspace   string  `json:"workspace"`
	Title       string  `json:"title"`
	Score       float64 `json:"score"`
	Snippet     string  `json:"snippet"`
	CreatedAt   *int64  `json:"created_at,omitempty"`
	MatchType   string  `json:"match_type"`
	Content     string  `json:"content,omitempty"`
	SessionID   string  `json:"session_id,omitempty"`
}

// CreatedAtTime returns the CreatedAt timestamp as a time.Time
func (h SearchHit) CreatedAtTime() time.Time {
	if h.CreatedAt == nil {
		return time.Time{}
	}
	return time.Unix(*h.CreatedAt, 0)
}

// Meta contains metadata about the search request
type Meta struct {
	TookMs           int64  `json:"took_ms"`
	IndexSize        int64  `json:"index_size"`
	Version          string `json:"version"`
	ElapsedMs        int64  `json:"elapsed_ms"`
	WildcardFallback bool   `json:"wildcard_fallback"`
	NextCursor       string `json:"next_cursor,omitempty"`
}

// HasMore returns true if there are more results available
func (m *Meta) HasMore() bool {
	if m == nil {
		return false
	}
	return m.NextCursor != ""
}

// Aggregations contains aggregated stats from search results
type Aggregations struct {
	Agents     map[string]int `json:"agents,omitempty"`
	Workspaces map[string]int `json:"workspaces,omitempty"`
	Tags       map[string]int `json:"tags,omitempty"`
}

// SearchResponse represents the full response from a CASS search
type SearchResponse struct {
	Query        string        `json:"query"`
	Limit        int           `json:"limit"`
	Offset       int           `json:"offset"`
	Count        int           `json:"count"`
	TotalMatches int           `json:"total_matches"`
	Hits         []SearchHit   `json:"hits"`
	Meta         *Meta         `json:"_meta,omitempty"`
	Aggregations *Aggregations `json:"aggregations,omitempty"`
}

// HasResults returns true if the response contains any hits
func (r SearchResponse) HasResults() bool {
	return len(r.Hits) > 0
}

// HasMore returns true if there are more results available (via cursor or count)
func (r SearchResponse) HasMore() bool {
	if r.Meta != nil && r.Meta.HasMore() {
		return true
	}
	return r.Offset+r.Count < r.TotalMatches
}

// IndexInfo provides details about the search index
type IndexInfo struct {
	DocCount    int64     `json:"doc_count"`
	SizeBytes   int64     `json:"size_bytes"`
	LastUpdated time.Time `json:"last_updated"`
	Healthy     bool      `json:"healthy"`
}

// SizeMB returns the index size in megabytes
func (i IndexInfo) SizeMB() float64 {
	return float64(i.SizeBytes) / (1024 * 1024)
}

// DBInfo provides details about the SQLite database
type DBInfo struct {
	Path         string `json:"path"`
	SizeBytes    int64  `json:"size_bytes"`
	Healthy      bool   `json:"healthy"`
	SessionCount int64  `json:"session_count"`
}

// SizeMB returns the DB size in megabytes
func (d DBInfo) SizeMB() float64 {
	return float64(d.SizeBytes) / (1024 * 1024)
}

// Pending tracks items waiting to be indexed
type Pending struct {
	Sessions int `json:"sessions"`
	Files    int `json:"files"`
}

// HasPending returns true if there are items waiting to be indexed
func (p Pending) HasPending() bool {
	return p.Sessions > 0 || p.Files > 0
}

// StatusResponse represents the response from CASS health check
type StatusResponse struct {
	Healthy           bool      `json:"healthy"`
	Version           string    `json:"version,omitempty"`
	RecommendedAction string    `json:"recommended_action,omitempty"`
	Index             IndexInfo `json:"index"`
	Database          DBInfo    `json:"database"`
	Pending           Pending   `json:"pending"`
	
	// Flattened fields for backwards compatibility/simpler parsing if needed
	IndexSize     int64     `json:"index_size,omitempty"`
	Conversations int64     `json:"conversations,omitempty"`
	Messages      int64     `json:"messages,omitempty"`
	LastIndexedAt time.Time `json:"last_indexed_at,omitempty"`
	StoragePath   string    `json:"storage_path,omitempty"`
}

// IsHealthy returns true if the overall status is healthy
func (s StatusResponse) IsHealthy() bool {
	return s.Healthy && s.Index.Healthy && s.Database.Healthy
}

// Limits defines API limits
type Limits struct {
	MaxQueryLength       int `json:"max_query_length"`
	MaxResults           int `json:"max_results"`
	MaxConcurrentQueries int `json:"max_concurrent_queries"`
	RateLimitPerMinute   int `json:"rate_limit_per_minute"`
}

// Capabilities describes the features supported by the CASS instance
type Capabilities struct {
	CrateVersion    string   `json:"crate_version"`
	APIVersion      int      `json:"api_version"`
	ContractVersion string   `json:"contract_version"`
	Features        []string `json:"features"`
	Connectors      []string `json:"connectors"`
	Limits          Limits   `json:"limits"`
}

// HasFeature checks if a feature is supported
func (c Capabilities) HasFeature(feature string) bool {
	for _, f := range c.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// HasConnector checks if a connector is supported
func (c Capabilities) HasConnector(connector string) bool {
	for _, conn := range c.Connectors {
		if conn == connector {
			return true
		}
	}
	return false
}

// CASSError represents an error returned by the CASS API
type CASSError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (e CASSError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s (hint: %s)", e.Message, e.Hint)
	}
	return e.Message
}

// Message represents a chat message in a timeline
type Message struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp *int64 `json:"timestamp,omitempty"`
}

// TimestampTime returns the message timestamp as time.Time
func (m Message) TimestampTime() time.Time {
	if m.Timestamp == nil {
		return time.Time{}
	}
	return time.Unix(*m.Timestamp, 0)
}

// TimelineEntry represents an event in the timeline
type TimelineEntry struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

// TimestampTime returns the entry timestamp as time.Time
func (e TimelineEntry) TimestampTime() time.Time {
	return time.Unix(e.Timestamp, 0)
}

// SearchOptions configures a search request
type SearchOptions struct {
	Query     string
	Limit     int
	Offset    int
	Agent     string
	Workspace string
	Since     string // e.g., "7d", "24h"
	Json      bool   // Always true for robot mode interactions
}
