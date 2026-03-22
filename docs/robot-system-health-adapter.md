# Robot System, Quota, Alert, and Health Normalization Adapter

> **Authoritative reference** for normalizing non-tmux operational signals into the projection model.
> This adapter ensures coherent operational visibility across heterogeneous signal sources.

**Status:** AUTHORITATIVE
**Bead:** bd-j9jo3.3.3
**Created:** 2026-03-22

---

## 1. Purpose

This document defines how non-tmux operational signals are normalized into the canonical projection sections. The goal is to present a coherent operational picture from disparate sources without losing signal fidelity.

**Design Goals:**
1. All operational signals use stable section shapes and reason codes
2. Signal origins are traceable without exposing internal implementation
3. Signals can promote from transient (alert) to durable (incident) state
4. Machine consumers can act without parsing free-form text

**Non-Goals:**
1. Unifying internal data structures across packages
2. Replacing existing alert/quota/health subsystems
3. Adding new signal sources (those are separate tasks)

---

## 2. Signal Sources

### 2.1 Source Inventory

| Source | Package | Signals | Section Target |
|--------|---------|---------|----------------|
| Quota Monitor | `internal/quota` | Usage levels, rate limits, resets | `QuotaSection` |
| Alert Generator | `internal/alerts` | Agent stuck, crash, quota warning | `AlertsSection` |
| Health Checker | `internal/health` | Agent health, process status | `SourceHealthSection` |
| System Monitor | `internal/system` | Disk, CPU, memory pressure | `AlertsSection` |
| Caut Poller | `internal/integrations/caut` | Provider costs, quotas | `QuotaSection` |

### 2.2 Signal Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Quota Poller  │    │ Alert Generator │    │ Health Checker  │
│   (caut)        │    │ (alerts pkg)    │    │ (health pkg)    │
└────────┬────────┘    └────────┬────────┘    └────────┬────────┘
         │                      │                      │
         ▼                      ▼                      ▼
    ┌────────────────────────────────────────────────────────┐
    │                 NORMALIZATION ADAPTER                   │
    │  • Collect from sources                                 │
    │  • Apply reason codes                                   │
    │  • Compute severity/actionability                       │
    │  • Enrich with context                                  │
    └────────────────────────────────────────────────────────┘
         │                      │                      │
         ▼                      ▼                      ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  QuotaSection   │    │  AlertsSection  │    │ SourceHealth    │
│  (projection)   │    │  (projection)   │    │ Section         │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                      │                      │
         ▼                      ▼                      ▼
┌────────────────────────────────────────────────────────────┐
│                 INCIDENT PROMOTION LAYER                    │
│  • Escalate persistent alerts to incidents                  │
│  • Track resolution and root cause                          │
└────────────────────────────────────────────────────────────┘
```

---

## 3. Reason Codes

### 3.1 Reason Code Format

All signals carry machine-readable reason codes:

```
<domain>:<category>:<specific>

Examples:
  quota:exhausted:tokens
  quota:warning:rate_limit
  alert:agent:stuck
  alert:system:disk_low
  health:source:degraded
  health:agent:crashed
```

### 3.2 Quota Reason Codes

| Code | Meaning | Severity |
|------|---------|----------|
| `quota:ok` | Usage within limits | info |
| `quota:warning:tokens` | Token usage > 80% | warning |
| `quota:warning:requests` | Request usage > 80% | warning |
| `quota:warning:rate_limit` | Rate limiting detected | warning |
| `quota:critical:tokens` | Token usage > 95% | error |
| `quota:critical:requests` | Request usage > 95% | error |
| `quota:exceeded:tokens` | Token quota exhausted | critical |
| `quota:exceeded:requests` | Request limit reached | critical |
| `quota:exceeded:cost` | Cost ceiling exceeded | critical |
| `quota:suspended` | Account suspended | critical |
| `quota:unavailable` | Quota source unreachable | warning |

### 3.3 Alert Reason Codes

| Code | Meaning | Severity | Auto-Clears |
|------|---------|----------|-------------|
| `alert:agent:stuck` | No output for configured duration | warning | Yes |
| `alert:agent:crashed` | Agent process exited | error | No |
| `alert:agent:error` | Error detected in output | warning | Yes |
| `alert:agent:rate_limited` | Rate limit in output | warning | Yes |
| `alert:agent:context_warning` | Context usage > threshold | warning | Yes |
| `alert:system:disk_low` | Disk space below threshold | warning | Yes |
| `alert:system:cpu_high` | CPU usage sustained | warning | Yes |
| `alert:bead:stale` | In-progress bead inactive | info | Yes |
| `alert:mail:backlog` | Many unread messages | info | Yes |
| `alert:conflict:file` | File reservation conflict | warning | Yes |
| `alert:rotation:started` | Context rotation begun | info | Yes |
| `alert:rotation:complete` | Context rotation succeeded | info | Yes |
| `alert:rotation:failed` | Context rotation failed | error | No |
| `alert:compaction:triggered` | Proactive compaction started | info | Yes |
| `alert:compaction:complete` | Compaction succeeded | info | Yes |
| `alert:compaction:failed` | Compaction failed | error | No |

### 3.4 Health Reason Codes

| Code | Meaning | Severity |
|------|---------|----------|
| `health:ok` | All sources healthy | info |
| `health:source:degraded` | Source returning partial data | warning |
| `health:source:unavailable` | Source unreachable | error |
| `health:source:stale` | Source data outdated | warning |
| `health:agent:ok` | Agent healthy | info |
| `health:agent:idle` | Agent waiting for input | info |
| `health:agent:busy` | Agent actively working | info |
| `health:agent:stale` | Agent output stale | warning |
| `health:agent:crashed` | Agent process exited | error |
| `health:agent:rate_limited` | Agent hit rate limit | warning |

---

## 4. Severity Mapping

### 4.1 Severity Levels

| Level | Meaning | Example |
|-------|---------|---------|
| `debug` | Internal tracing | Source poll started |
| `info` | Normal state | Agent idle, quota ok |
| `warning` | Attention suggested | Quota > 80%, agent stale |
| `error` | Intervention needed | Agent crashed, quota exceeded |
| `critical` | Immediate action required | All quotas exhausted |

### 4.2 Actionability Classification

| Level | Meaning | Attention Feed |
|-------|---------|----------------|
| `background` | No action needed | Filtered unless requested |
| `interesting` | Worth knowing | Included in digest |
| `action_required` | Needs intervention | Always surfaced |

### 4.3 Severity to Actionability

```go
func severityToActionability(severity Severity, autoClears bool) Actionability {
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
```

---

## 5. Normalization Transforms

### 5.1 Quota Normalization

```go
// NormalizeQuota transforms caut cache data to QuotaSection
func NormalizeQuota(cache *caut.Cache) *QuotaSection {
    section := &QuotaSection{
        Accounts:  make([]AccountQuota, 0),
        Available: cache != nil,
    }

    if cache == nil {
        section.Reason = "caut unavailable"
        return section
    }

    status := cache.GetStatus()
    usages := cache.GetAllUsage()

    for _, usage := range usages {
        account := AccountQuota{
            ID:           usage.Provider,
            Provider:     usage.Provider,
            TokensUsed:   usage.TokensIn + usage.TokensOut,
            RequestsUsed: usage.RequestCount,
            Status:       computeQuotaStatus(usage),
            ReasonCode:   computeQuotaReasonCode(usage),
            IsActive:     true,
        }

        // Enrich from status if available
        if status != nil {
            enrichFromStatus(&account, status, usage.Provider)
        }

        section.Accounts = append(section.Accounts, account)
    }

    section.Summary = computeQuotaSummary(section.Accounts)
    return section
}
```

### 5.2 Alert Normalization

```go
// NormalizeAlerts transforms alerts.Alert slice to AlertsSection
func NormalizeAlerts(alerts []alerts.Alert) *AlertsSection {
    section := &AlertsSection{
        Active:  make([]AlertItem, 0, len(alerts)),
        Summary: &AlertsSummary{
            BySeverity: make(map[string]int),
            ByType:     make(map[string]int),
        },
    }

    for _, a := range alerts {
        if a.IsResolved() {
            continue
        }

        item := AlertItem{
            ID:          a.ID,
            Type:        mapAlertType(a.Type),
            Severity:    string(a.Severity),
            Message:     a.Message,
            Session:     a.Session,
            Pane:        a.Pane,
            BeadID:      a.BeadID,
            ReasonCode:  computeAlertReasonCode(a),
            Details:     a.Context,
            Count:       a.Count,
            CreatedAt:   FormatTimestamp(a.CreatedAt),
            UpdatedAt:   FormatTimestamp(a.LastSeenAt),
            DurationMs:  a.Duration().Milliseconds(),
            AutoClears:  alertAutoClears(a.Type),
            Dismissable: true,
        }

        section.Active = append(section.Active, item)
        section.Summary.TotalActive++
        section.Summary.BySeverity[item.Severity]++
        section.Summary.ByType[item.Type]++
    }

    return section
}
```

### 5.3 Health Normalization

```go
// NormalizeHealth transforms health.SessionHealth to SourceHealthSection
func NormalizeHealth(sources map[string]HealthSource) *SourceHealthSection {
    section := &SourceHealthSection{
        Sources:  make(map[string]SourceInfo),
        Degraded: []string{},
        AllFresh: true,
    }

    for name, source := range sources {
        info := SourceInfo{
            Name:       name,
            Available:  source.Available,
            Fresh:      source.Fresh,
            ReasonCode: computeHealthReasonCode(source),
        }

        if source.LastUpdate != nil {
            info.AgeMs = time.Since(*source.LastUpdate).Milliseconds()
            info.UpdatedAt = FormatTimestamp(*source.LastUpdate)
        }

        if !source.Available || !source.Fresh {
            section.AllFresh = false
            info.Degraded = true
            info.DegradedSince = FormatTimestamp(source.DegradedAt)
            info.DegradedReason = source.Reason
            section.Degraded = append(section.Degraded, name)
        }

        if source.LastError != "" {
            info.LastError = source.LastError
        }

        section.Sources[name] = info
    }

    return section
}
```

---

## 6. Incident Promotion

### 6.1 Promotion Rules

Alerts promote to incidents when:

| Condition | Promotion Trigger |
|-----------|-------------------|
| Duration | Alert active > 30 minutes |
| Severity | Severity is `critical` |
| Scope | Affects > 3 agents |
| Type | Type is `agent_crashed` or `rotation_failed` |
| Persistence | Alert cleared and re-raised 3+ times |

### 6.2 Promotion Transform

```go
// PromoteToIncident creates an incident from a qualifying alert
func PromoteToIncident(alert AlertItem, reason string) *IncidentItem {
    return &IncidentItem{
        ID:          GenerateIncidentID(),
        Type:        alert.Type,
        Severity:    alertToIncidentSeverity(alert.Severity),
        Title:       alert.Message,
        Description: fmt.Sprintf("Promoted from alert %s: %s", alert.ID, reason),
        Session:     alert.Session,
        Panes:       []string{alert.Pane},
        Status:      "investigating",
        CreatedAt:   FormatTimestamp(time.Now()),
        UpdatedAt:   FormatTimestamp(time.Now()),
        DetectedBy:  "alert_promotion",
        RelatedAlerts: []string{alert.ID},
    }
}

func alertToIncidentSeverity(alertSeverity string) string {
    switch alertSeverity {
    case "critical":
        return "P0"
    case "error":
        return "P1"
    default:
        return "P2"
    }
}
```

---

## 7. Go Implementation

### 7.1 Package Structure

```
internal/robot/adapters/
├── system_health.go     // SourceHealthSection adapter
├── quota.go             // QuotaSection adapter
├── alerts.go            // AlertsSection adapter
├── normalize.go         // Shared normalization utilities
├── reason_codes.go      // Reason code definitions
└── promotion.go         // Incident promotion logic
```

### 7.2 Reason Code Types

```go
package adapters

// ReasonCode is a machine-readable signal classification
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
    ReasonAlertAgentStuck        ReasonCode = "alert:agent:stuck"
    ReasonAlertAgentCrashed      ReasonCode = "alert:agent:crashed"
    ReasonAlertAgentError        ReasonCode = "alert:agent:error"
    ReasonAlertAgentRateLimited  ReasonCode = "alert:agent:rate_limited"
    ReasonAlertAgentContext      ReasonCode = "alert:agent:context_warning"
    ReasonAlertSystemDiskLow     ReasonCode = "alert:system:disk_low"
    ReasonAlertSystemCPUHigh     ReasonCode = "alert:system:cpu_high"
    ReasonAlertBeadStale         ReasonCode = "alert:bead:stale"
    ReasonAlertMailBacklog       ReasonCode = "alert:mail:backlog"
    ReasonAlertConflictFile      ReasonCode = "alert:conflict:file"
    ReasonAlertRotationStarted   ReasonCode = "alert:rotation:started"
    ReasonAlertRotationComplete  ReasonCode = "alert:rotation:complete"
    ReasonAlertRotationFailed    ReasonCode = "alert:rotation:failed"
    ReasonAlertCompactionTriggered ReasonCode = "alert:compaction:triggered"
    ReasonAlertCompactionComplete  ReasonCode = "alert:compaction:complete"
    ReasonAlertCompactionFailed    ReasonCode = "alert:compaction:failed"
)

// Health reason codes
const (
    ReasonHealthOK              ReasonCode = "health:ok"
    ReasonHealthSourceDegraded  ReasonCode = "health:source:degraded"
    ReasonHealthSourceUnavailable ReasonCode = "health:source:unavailable"
    ReasonHealthSourceStale     ReasonCode = "health:source:stale"
    ReasonHealthAgentOK         ReasonCode = "health:agent:ok"
    ReasonHealthAgentIdle       ReasonCode = "health:agent:idle"
    ReasonHealthAgentBusy       ReasonCode = "health:agent:busy"
    ReasonHealthAgentStale      ReasonCode = "health:agent:stale"
    ReasonHealthAgentCrashed    ReasonCode = "health:agent:crashed"
    ReasonHealthAgentRateLimited ReasonCode = "health:agent:rate_limited"
)
```

### 7.3 Adapter Interface

```go
// SignalAdapter normalizes a signal source to projection sections
type SignalAdapter interface {
    // Name returns the adapter identifier
    Name() string

    // Available returns true if the signal source is reachable
    Available(ctx context.Context) bool

    // Collect gathers current signal state
    Collect(ctx context.Context) (*SignalBatch, error)

    // LastError returns the most recent collection error
    LastError() error
}

// SignalBatch contains normalized signals from a source
type SignalBatch struct {
    Source     string
    CollectedAt time.Time
    Quota      *QuotaSection      // nil if not applicable
    Alerts     *AlertsSection     // nil if not applicable
    Health     *SourceHealthSection // nil if not applicable
}
```

### 7.4 Aggregator

```go
// SignalAggregator combines signals from multiple adapters
type SignalAggregator struct {
    adapters []SignalAdapter
    mu       sync.RWMutex
    cache    map[string]*SignalBatch
    cacheTTL time.Duration
}

// Collect aggregates signals from all adapters
func (a *SignalAggregator) Collect(ctx context.Context) (*AggregatedSignals, error) {
    result := &AggregatedSignals{
        CollectedAt: time.Now(),
        Quota:       &QuotaSection{Accounts: []AccountQuota{}},
        Alerts:      &AlertsSection{Active: []AlertItem{}},
        Health:      &SourceHealthSection{Sources: make(map[string]SourceInfo)},
    }

    var wg sync.WaitGroup
    var mu sync.Mutex
    var errs []error

    for _, adapter := range a.adapters {
        wg.Add(1)
        go func(ad SignalAdapter) {
            defer wg.Done()

            batch, err := ad.Collect(ctx)
            if err != nil {
                mu.Lock()
                errs = append(errs, err)
                mu.Unlock()
                return
            }

            mu.Lock()
            defer mu.Unlock()
            mergeSignalBatch(result, batch)
        }(adapter)
    }

    wg.Wait()

    if len(errs) > 0 {
        result.CollectionErrors = errs
    }

    return result, nil
}
```

---

## 8. Validation

### 8.1 Invariants

1. Every normalized signal carries a valid `ReasonCode`
2. Severity levels are always one of: debug, info, warning, error, critical
3. Timestamps are RFC3339 format
4. ID fields are non-empty for alerts and incidents

### 8.2 Test Cases

```go
func TestQuotaNormalization(t *testing.T) {
    // Test that quota status maps to correct reason codes
    cases := []struct {
        usage   float64
        want    ReasonCode
    }{
        {50.0, ReasonQuotaOK},
        {85.0, ReasonQuotaWarningTokens},
        {96.0, ReasonQuotaCriticalTokens},
        {100.0, ReasonQuotaExceededTokens},
    }
    // ...
}

func TestAlertReasonCodes(t *testing.T) {
    // Test that alert types map to correct reason codes
    cases := []struct {
        alertType alerts.AlertType
        want      ReasonCode
    }{
        {alerts.AlertAgentStuck, ReasonAlertAgentStuck},
        {alerts.AlertAgentCrashed, ReasonAlertAgentCrashed},
        {alerts.AlertDiskLow, ReasonAlertSystemDiskLow},
    }
    // ...
}
```

---

## 9. Non-Goals

1. **Real-time streaming** — This is batch normalization, not event streaming
2. **Alert deduplication** — That's the alert generator's responsibility
3. **Incident workflow** — Incident lifecycle is managed separately
4. **Cross-host aggregation** — Scope is single ntm instance

---

## 10. References

- [Robot Projection Sections](robot-projection-sections.md) — Section schemas
- [Robot Action Errors](robot-action-errors.md) — Error taxonomy
- [Robot Resource References](robot-resource-references.md) — Entity references

---

## Appendix: Changelog

- **2026-03-22:** Initial adapter specification (bd-j9jo3.3.3)

---

*Reference: bd-j9jo3.3.3*
