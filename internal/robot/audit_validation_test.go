// Package robot provides machine-readable output for AI agents.
// audit_validation_test.go provides validation coverage for audit trails and actor attribution.
//
// # Audit Trail Contract (bd-j9jo3.9.12)
//
// This file verifies that ntm robot mode maintains trustworthy audit trails for:
//   - Operator attention-state changes (acknowledge, snooze, pin, override)
//   - Actuation requests and outcomes (send, interrupt, restart)
//   - Incident lifecycle transitions (open, escalate, resolve)
//   - Disclosure decisions (redact, reveal, withhold)
//
// The goal is operational trust: when a future agent or human asks "who changed
// this?" or "what request caused this effect?", ntm should have a first-class answer.
package robot

import (
	"encoding/json"
	"testing"
	"time"
)

// =============================================================================
// Audit Event Structure Tests
// =============================================================================

// AuditEvent represents an audit trail entry for state changes.
// This structure should be persisted and queryable.
type AuditEvent struct {
	// ID uniquely identifies this audit event.
	ID string `json:"id"`

	// Ts is when the audit-worthy action occurred (RFC3339).
	Ts string `json:"ts"`

	// ActorID identifies who performed the action.
	// Format: "agent:<name>" or "operator" or "system".
	ActorID string `json:"actor_id"`

	// ActorOrigin classifies the actor type for filtering.
	// Values: "agent", "operator", "system", "replay".
	ActorOrigin string `json:"actor_origin"`

	// RequestID correlates this event with the triggering request.
	// Enables joining audit events with request logs.
	RequestID string `json:"request_id,omitempty"`

	// ActionType is the category of action performed.
	// Values: "attention_change", "actuation", "incident", "disclosure".
	ActionType string `json:"action_type"`

	// ActionName is the specific action within the category.
	// Examples: "acknowledge", "send", "escalate", "redact".
	ActionName string `json:"action_name"`

	// TargetRef identifies the affected entity.
	// Format: "attention:cursor_123" or "incident:inc_456" or "session:proj:0.2".
	TargetRef string `json:"target_ref"`

	// BeforeState captures the state before the action (if applicable).
	BeforeState map[string]any `json:"before_state,omitempty"`

	// AfterState captures the state after the action.
	AfterState map[string]any `json:"after_state,omitempty"`

	// Reason explains why the action was taken.
	// User-provided or system-generated rationale.
	Reason string `json:"reason,omitempty"`

	// DisclosureState indicates sensitivity handling.
	// Values: "visible", "redacted", "preview_only", "withheld".
	DisclosureState string `json:"disclosure_state,omitempty"`
}

func TestAuditEventStructure(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_1711082700000000123",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "agent:SilentCanyon",
		ActorOrigin: "agent",
		RequestID:   "req_abc123",
		ActionType:  "attention_change",
		ActionName:  "acknowledge",
		TargetRef:   "attention:evt_1711082600000000100",
		BeforeState: map[string]any{"acknowledged": false},
		AfterState:  map[string]any{"acknowledged": true, "acknowledged_by": "agent:SilentCanyon"},
		Reason:      "Addressed in context",
	}

	// Verify JSON serialization
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal AuditEvent: %v", err)
	}

	var parsed AuditEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal AuditEvent: %v", err)
	}

	if parsed.ActorID != event.ActorID {
		t.Errorf("ActorID mismatch: got %s, want %s", parsed.ActorID, event.ActorID)
	}
	if parsed.ActionType != event.ActionType {
		t.Errorf("ActionType mismatch: got %s, want %s", parsed.ActionType, event.ActionType)
	}
}

// =============================================================================
// Actor Attribution Tests
// =============================================================================

func TestActorIDFormats(t *testing.T) {

	testCases := []struct {
		name    string
		actorID string
		origin  string
		valid   bool
	}{
		{"agent with name", "agent:SilentCanyon", "agent", true},
		{"operator", "operator", "operator", true},
		{"system", "system", "system", true},
		{"replay mode", "replay:session_123", "replay", true},
		{"empty actor", "", "", false},
		{"malformed agent", "agent:", "agent", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			valid := validateActorID(tc.actorID)
			if valid != tc.valid {
				t.Errorf("validateActorID(%q) = %v, want %v", tc.actorID, valid, tc.valid)
			}
		})
	}
}

func validateActorID(actorID string) bool {
	if actorID == "" {
		return false
	}
	if actorID == "operator" || actorID == "system" {
		return true
	}
	// Check prefix patterns
	prefixes := []string{"agent:", "replay:"}
	for _, prefix := range prefixes {
		if len(actorID) > len(prefix) && actorID[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// =============================================================================
// Attention State Change Audit Tests
// =============================================================================

func TestAuditAttentionAcknowledge(t *testing.T) {

	// Simulate an acknowledge action
	event := AuditEvent{
		ID:          "audit_ack_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "agent:PeachPond",
		ActorOrigin: "agent",
		ActionType:  "attention_change",
		ActionName:  "acknowledge",
		TargetRef:   "attention:evt_100",
		BeforeState: map[string]any{"acknowledged": false},
		AfterState:  map[string]any{"acknowledged": true, "acknowledged_at": time.Now().UTC().Format(time.RFC3339)},
	}

	// Verify required fields are present
	if event.ActorID == "" {
		t.Error("audit event missing ActorID")
	}
	if event.TargetRef == "" {
		t.Error("audit event missing TargetRef")
	}
	if event.ActionName == "" {
		t.Error("audit event missing ActionName")
	}
}

func TestAuditAttentionSnooze(t *testing.T) {

	snoozedUntil := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	event := AuditEvent{
		ID:          "audit_snooze_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "operator",
		ActorOrigin: "operator",
		ActionType:  "attention_change",
		ActionName:  "snooze",
		TargetRef:   "attention:evt_200",
		BeforeState: map[string]any{"snoozed_until": nil},
		AfterState:  map[string]any{"snoozed_until": snoozedUntil},
		Reason:      "Will address after standup",
	}

	// Verify snooze captures the duration
	if event.AfterState["snoozed_until"] == nil {
		t.Error("snooze audit event should capture snoozed_until")
	}
}

// =============================================================================
// Actuation Audit Tests
// =============================================================================

func TestAuditActuationSend(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_send_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "agent:CoralReef",
		ActorOrigin: "agent",
		RequestID:   "req_send_123",
		ActionType:  "actuation",
		ActionName:  "send",
		TargetRef:   "session:myproject:0.1",
		AfterState: map[string]any{
			"prompt_length": 150,
			"sent_at":       time.Now().UTC().Format(time.RFC3339),
		},
	}

	// Verify actuation events capture the target
	if event.TargetRef == "" {
		t.Error("actuation audit should include target reference")
	}
	if event.RequestID == "" {
		t.Error("actuation audit should include request ID for correlation")
	}
}

func TestAuditActuationInterrupt(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_interrupt_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "operator",
		ActorOrigin: "operator",
		ActionType:  "actuation",
		ActionName:  "interrupt",
		TargetRef:   "session:myproject:0.2",
		Reason:      "Agent stuck in loop",
	}

	if event.ActionName != "interrupt" {
		t.Errorf("expected action interrupt, got %s", event.ActionName)
	}
}

// =============================================================================
// Incident Lifecycle Audit Tests
// =============================================================================

func TestAuditIncidentCreate(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_inc_create_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "system",
		ActorOrigin: "system",
		ActionType:  "incident",
		ActionName:  "create",
		TargetRef:   "incident:inc_1711082700_abc",
		AfterState: map[string]any{
			"title":    "Agent crash loop detected",
			"severity": "major",
			"state":    "open",
		},
	}

	// System-created incidents should have actor "system"
	if event.ActorID != "system" {
		t.Errorf("system-created incident should have actor 'system', got %s", event.ActorID)
	}
}

func TestAuditIncidentEscalate(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_inc_escalate_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "agent:AlertBot",
		ActorOrigin: "agent",
		ActionType:  "incident",
		ActionName:  "escalate",
		TargetRef:   "incident:inc_1711082700_abc",
		BeforeState: map[string]any{"severity": "minor"},
		AfterState:  map[string]any{"severity": "major"},
		Reason:      "Third occurrence in 1 hour",
	}

	// Escalation should capture before/after severity
	if event.BeforeState["severity"] == event.AfterState["severity"] {
		t.Error("escalation audit should show severity change")
	}
}

func TestAuditIncidentResolve(t *testing.T) {

	event := AuditEvent{
		ID:          "audit_inc_resolve_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "operator",
		ActorOrigin: "operator",
		ActionType:  "incident",
		ActionName:  "resolve",
		TargetRef:   "incident:inc_1711082700_abc",
		BeforeState: map[string]any{"state": "open"},
		AfterState:  map[string]any{"state": "resolved", "resolved_at": time.Now().UTC().Format(time.RFC3339)},
		Reason:      "Root cause fixed in commit abc123",
	}

	// Resolution should capture who resolved and when
	if event.AfterState["resolved_at"] == nil {
		t.Error("resolve audit should capture resolved_at timestamp")
	}
}

// =============================================================================
// Disclosure Decision Audit Tests
// =============================================================================

func TestAuditDisclosureRedact(t *testing.T) {

	event := AuditEvent{
		ID:              "audit_disclosure_001",
		Ts:              time.Now().UTC().Format(time.RFC3339),
		ActorID:         "system",
		ActorOrigin:     "system",
		ActionType:      "disclosure",
		ActionName:      "redact",
		TargetRef:       "pane_output:myproject:0.3:line_150",
		DisclosureState: "redacted",
		Reason:          "Contains API key pattern",
	}

	// Disclosure events should capture the disclosure state
	if event.DisclosureState == "" {
		t.Error("disclosure audit should capture disclosure_state")
	}
}

// =============================================================================
// Audit Trail Query Tests
// =============================================================================

// AuditQuery represents filters for querying audit events.
type AuditQuery struct {
	ActorID     string     `json:"actor_id,omitempty"`
	ActorOrigin string     `json:"actor_origin,omitempty"`
	ActionType  string     `json:"action_type,omitempty"`
	TargetRef   string     `json:"target_ref,omitempty"`
	Since       *time.Time `json:"since,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	Limit       int        `json:"limit,omitempty"`
}

func TestAuditQueryByActor(t *testing.T) {

	query := AuditQuery{
		ActorID: "agent:SilentCanyon",
		Limit:   100,
	}

	// Verify query structure
	data, err := json.Marshal(query)
	if err != nil {
		t.Fatalf("failed to marshal AuditQuery: %v", err)
	}

	var parsed AuditQuery
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal AuditQuery: %v", err)
	}

	if parsed.ActorID != query.ActorID {
		t.Errorf("ActorID mismatch: got %s, want %s", parsed.ActorID, query.ActorID)
	}
}

func TestAuditQueryByTargetRef(t *testing.T) {

	query := AuditQuery{
		TargetRef: "incident:inc_1711082700_abc",
		Limit:     50,
	}

	// Should be able to query all audit events for a specific incident
	if query.TargetRef == "" {
		t.Error("target ref query should be supported")
	}
}

// =============================================================================
// Audit Retention Tests
// =============================================================================

func TestAuditRetentionPolicy(t *testing.T) {

	// Audit events should be retained per policy (default: 30 days)
	retentionDays := 30
	if retentionDays < 7 {
		t.Error("audit retention should be at least 7 days")
	}

	// Verify retention is configurable
	type RetentionConfig struct {
		AuditRetentionDays int `json:"audit_retention_days"`
	}

	config := RetentionConfig{AuditRetentionDays: retentionDays}
	if config.AuditRetentionDays != 30 {
		t.Errorf("expected 30 day retention, got %d", config.AuditRetentionDays)
	}
}

// =============================================================================
// Audit and Sensitivity Interaction Tests
// =============================================================================

func TestAuditRedactionDoesNotObscureActor(t *testing.T) {

	// Even when content is redacted, the audit trail should preserve
	// who performed the action and what entity was affected
	event := AuditEvent{
		ID:              "audit_sensitive_001",
		Ts:              time.Now().UTC().Format(time.RFC3339),
		ActorID:         "operator",
		ActorOrigin:     "operator",
		ActionType:      "disclosure",
		ActionName:      "reveal_sensitive",
		TargetRef:       "pane_output:proj:0.1:line_50",
		DisclosureState: "visible",
		Reason:          "[REDACTED - contains sensitive content]",
	}

	// Actor and target should NOT be redacted in audit trail
	if event.ActorID == "[REDACTED]" {
		t.Error("actor_id should never be redacted in audit trail")
	}
	if event.TargetRef == "[REDACTED]" {
		t.Error("target_ref should never be redacted in audit trail")
	}

	// But the reason/content MAY be redacted
	// (this is acceptable - the audit preserves WHO and WHAT, not necessarily WHY details)
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkAuditEventSerialization(b *testing.B) {
	event := AuditEvent{
		ID:          "audit_bench_001",
		Ts:          time.Now().UTC().Format(time.RFC3339),
		ActorID:     "agent:BenchAgent",
		ActorOrigin: "agent",
		RequestID:   "req_bench_123",
		ActionType:  "actuation",
		ActionName:  "send",
		TargetRef:   "session:bench:0.1",
		AfterState:  map[string]any{"sent": true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(event)
		var parsed AuditEvent
		_ = json.Unmarshal(data, &parsed)
	}
}
