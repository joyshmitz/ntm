// Package adapters provides normalization adapters for system, quota, alert, and health signals.
// It transforms heterogeneous signal sources into the canonical projection section model.
package adapters

// ReasonCode is a machine-readable signal classification.
// Format: <domain>:<category>:<specific>
type ReasonCode string

// Quota reason codes
const (
	ReasonQuotaOK               ReasonCode = "quota:ok"
	ReasonQuotaWarningTokens    ReasonCode = "quota:warning:tokens"
	ReasonQuotaWarningRequests  ReasonCode = "quota:warning:requests"
	ReasonQuotaWarningRateLimit ReasonCode = "quota:warning:rate_limit"
	ReasonQuotaCriticalTokens   ReasonCode = "quota:critical:tokens"
	ReasonQuotaCriticalRequests ReasonCode = "quota:critical:requests"
	ReasonQuotaExceededTokens   ReasonCode = "quota:exceeded:tokens"
	ReasonQuotaExceededRequests ReasonCode = "quota:exceeded:requests"
	ReasonQuotaExceededCost     ReasonCode = "quota:exceeded:cost"
	ReasonQuotaSuspended        ReasonCode = "quota:suspended"
	ReasonQuotaUnavailable      ReasonCode = "quota:unavailable"
)

// Alert reason codes
const (
	ReasonAlertAgentStuck          ReasonCode = "alert:agent:stuck"
	ReasonAlertAgentCrashed        ReasonCode = "alert:agent:crashed"
	ReasonAlertAgentError          ReasonCode = "alert:agent:error"
	ReasonAlertAgentRateLimited    ReasonCode = "alert:agent:rate_limited"
	ReasonAlertAgentContext        ReasonCode = "alert:agent:context_warning"
	ReasonAlertSystemDiskLow       ReasonCode = "alert:system:disk_low"
	ReasonAlertSystemCPUHigh       ReasonCode = "alert:system:cpu_high"
	ReasonAlertBeadStale           ReasonCode = "alert:bead:stale"
	ReasonAlertMailBacklog         ReasonCode = "alert:mail:backlog"
	ReasonAlertConflictFile        ReasonCode = "alert:conflict:file"
	ReasonAlertRotationStarted     ReasonCode = "alert:rotation:started"
	ReasonAlertRotationComplete    ReasonCode = "alert:rotation:complete"
	ReasonAlertRotationFailed      ReasonCode = "alert:rotation:failed"
	ReasonAlertCompactionTriggered ReasonCode = "alert:compaction:triggered"
	ReasonAlertCompactionComplete  ReasonCode = "alert:compaction:complete"
	ReasonAlertCompactionFailed    ReasonCode = "alert:compaction:failed"
)

// Health reason codes
const (
	ReasonHealthOK                ReasonCode = "health:ok"
	ReasonHealthSourceDegraded    ReasonCode = "health:source:degraded"
	ReasonHealthSourceUnavailable ReasonCode = "health:source:unavailable"
	ReasonHealthSourceStale       ReasonCode = "health:source:stale"
	ReasonHealthAgentOK           ReasonCode = "health:agent:ok"
	ReasonHealthAgentIdle         ReasonCode = "health:agent:idle"
	ReasonHealthAgentBusy         ReasonCode = "health:agent:busy"
	ReasonHealthAgentStale        ReasonCode = "health:agent:stale"
	ReasonHealthAgentCrashed      ReasonCode = "health:agent:crashed"
	ReasonHealthAgentRateLimited  ReasonCode = "health:agent:rate_limited"
)

// Work reason codes
const (
	ReasonWorkReadyTopRecommendation ReasonCode = "work:ready:top_recommendation"
)

// Coordination reason codes
const (
	ReasonCoordinationUrgentMail          ReasonCode = "coordination:mail:urgent_unread"
	ReasonCoordinationPendingAck          ReasonCode = "coordination:mail:pending_ack"
	ReasonCoordinationMailBacklog         ReasonCode = "coordination:mail:backlog"
	ReasonCoordinationReservationConflict ReasonCode = "coordination:reservation:conflict"
	ReasonCoordinationFileConflict        ReasonCode = "coordination:file:conflict"
	ReasonCoordinationHandoffBlocked      ReasonCode = "coordination:handoff:blocked"
)

// Severity levels
type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Actionability levels
type Actionability string

const (
	ActionabilityBackground  Actionability = "background"
	ActionabilityInteresting Actionability = "interesting"
	ActionabilityRequired    Actionability = "action_required"
)

// SeverityToActionability converts severity to actionability.
// Non-auto-clearing errors require action; warnings are interesting.
func SeverityToActionability(severity Severity, autoClears bool) Actionability {
	switch severity {
	case SeverityCritical:
		return ActionabilityRequired
	case SeverityError:
		if autoClears {
			return ActionabilityInteresting
		}
		return ActionabilityRequired
	case SeverityWarning:
		return ActionabilityInteresting
	default:
		return ActionabilityBackground
	}
}

// ReasonToSeverity extracts severity from a reason code
func ReasonToSeverity(code ReasonCode) Severity {
	switch code {
	// Critical
	case ReasonQuotaExceededTokens, ReasonQuotaExceededRequests,
		ReasonQuotaExceededCost, ReasonQuotaSuspended:
		return SeverityCritical
	// Error
	case ReasonQuotaCriticalTokens, ReasonQuotaCriticalRequests,
		ReasonAlertAgentCrashed, ReasonAlertRotationFailed,
		ReasonAlertCompactionFailed, ReasonHealthSourceUnavailable,
		ReasonHealthAgentCrashed, ReasonCoordinationUrgentMail:
		return SeverityError
	// Warning
	case ReasonQuotaWarningTokens, ReasonQuotaWarningRequests,
		ReasonQuotaWarningRateLimit, ReasonQuotaUnavailable,
		ReasonAlertAgentStuck, ReasonAlertAgentError,
		ReasonAlertAgentRateLimited, ReasonAlertAgentContext,
		ReasonAlertSystemDiskLow, ReasonAlertSystemCPUHigh,
		ReasonAlertConflictFile, ReasonHealthSourceDegraded,
		ReasonHealthSourceStale, ReasonHealthAgentStale,
		ReasonHealthAgentRateLimited, ReasonCoordinationPendingAck,
		ReasonCoordinationMailBacklog, ReasonCoordinationReservationConflict,
		ReasonCoordinationFileConflict, ReasonCoordinationHandoffBlocked:
		return SeverityWarning
	// Info
	case ReasonQuotaOK, ReasonAlertBeadStale, ReasonAlertMailBacklog,
		ReasonAlertRotationStarted, ReasonAlertRotationComplete,
		ReasonAlertCompactionTriggered, ReasonAlertCompactionComplete,
		ReasonHealthOK, ReasonHealthAgentOK, ReasonHealthAgentIdle,
		ReasonHealthAgentBusy, ReasonWorkReadyTopRecommendation:
		return SeverityInfo
	default:
		return SeverityInfo
	}
}
