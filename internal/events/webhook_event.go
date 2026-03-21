package events

import "time"

// Webhook event types (shared with .ntm.yaml webhooks config).
const (
	WebhookSessionCreated = "session.created"
	WebhookSessionKilled  = "session.killed"
	WebhookSessionEnded   = "session.ended" // Alias for session.killed (legacy/alternate naming)
	WebhookAgentStarted   = "agent.started"
	WebhookAgentStopped   = "agent.stopped"
	WebhookAgentError     = "agent.error"
	WebhookAgentCrashed   = "agent.crashed"
	WebhookAgentRestarted = "agent.restarted"
	WebhookAgentIdle      = "agent.idle"
	WebhookAgentBusy      = "agent.busy"
	WebhookAgentRateLimit = "agent.rate_limit"
	WebhookAgentCompleted = "agent.completed"
	WebhookRotationNeeded = "rotation.needed"
	WebhookHealthDegraded = "health.degraded"
	WebhookBeadAssigned   = "bead.assigned"
	WebhookBeadCompleted  = "bead.completed"
	WebhookBeadFailed     = "bead.failed"
)

// WebhookEvent is a BusEvent intended for downstream dispatch to webhooks and
// for robot/server event streaming (SSE/WebSocket). It mirrors the core webhook
// payload shape while remaining lightweight and easy to emit throughout NTM.
type WebhookEvent struct {
	BaseEvent

	Pane    string            `json:"pane,omitempty"`
	Agent   string            `json:"agent,omitempty"`
	Message string            `json:"message,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

// NewWebhookEvent constructs a webhook event with UTC timestamp.
func NewWebhookEvent(eventType, session, pane, agent, message string, details map[string]string) WebhookEvent {
	return WebhookEvent{
		BaseEvent: BaseEvent{
			Type:      eventType,
			Timestamp: time.Now().UTC(),
			Session:   session,
		},
		Pane:    pane,
		Agent:   agent,
		Message: message,
		Details: cloneStringMap(details),
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
