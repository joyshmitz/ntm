package adapters

import (
	"context"
	"sync"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/alerts"
	"github.com/Dicklesworthstone/ntm/internal/integrations/caut"
)

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
	Source       string
	CollectedAt  time.Time
	Work         *WorkSection
	Coordination *CoordinationSection
	Quota        *QuotaSection
	Alerts       *AlertsSection
	Health       *SourceHealthSection
}

// AggregatedSignals contains merged signals from all adapters
type AggregatedSignals struct {
	CollectedAt      time.Time
	Work             *WorkSection
	Coordination     *CoordinationSection
	Quota            *QuotaSection
	Alerts           *AlertsSection
	Health           *SourceHealthSection
	CollectionErrors []error
}

// QuotaSection normalized quota information
type QuotaSection struct {
	Accounts  []AccountQuota `json:"accounts"`
	Summary   *QuotaSummary  `json:"summary,omitempty"`
	Available bool           `json:"available"`
	Reason    string         `json:"reason,omitempty"`
}

// AccountQuota per-provider quota state
type AccountQuota struct {
	ID            string     `json:"id"`
	Provider      string     `json:"provider"`
	Model         string     `json:"model,omitempty"`
	TokensUsed    int64      `json:"tokens_used"`
	TokensLimit   int64      `json:"tokens_limit,omitempty"`
	RequestsUsed  int        `json:"requests_used"`
	RequestsLimit int        `json:"requests_limit,omitempty"`
	CostUSD       float64    `json:"cost_usd,omitempty"`
	Status        string     `json:"status"` // ok, warning, exceeded, suspended
	ReasonCode    ReasonCode `json:"reason_code"`
	ResetAt       string     `json:"reset_at,omitempty"`
	IsActive      bool       `json:"is_active"`
	IsPrimary     bool       `json:"is_primary,omitempty"`
}

// QuotaSummary aggregate quota state
type QuotaSummary struct {
	TotalAccounts         int    `json:"total_accounts"`
	HealthyAccounts       int    `json:"healthy_accounts"`
	WarningAccounts       int    `json:"warning_accounts"`
	ExceededAccounts      int    `json:"exceeded_accounts"`
	LowestTokensRemaining int64  `json:"lowest_tokens_remaining,omitempty"`
	NextReset             string `json:"next_reset,omitempty"`
}

// AlertsSection normalized alerts
type AlertsSection struct {
	Active          []AlertItem    `json:"active"`
	Summary         *AlertsSummary `json:"summary,omitempty"`
	RecentlyCleared []string       `json:"recently_cleared,omitempty"`
}

// AlertItem normalized alert
type AlertItem struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Severity    string                 `json:"severity"`
	Message     string                 `json:"message"`
	Session     string                 `json:"session,omitempty"`
	Pane        string                 `json:"pane,omitempty"`
	Agent       string                 `json:"agent,omitempty"`
	BeadID      string                 `json:"bead_id,omitempty"`
	ReasonCode  ReasonCode             `json:"reason_code"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Count       int                    `json:"count"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	DurationMs  int64                  `json:"duration_ms"`
	AutoClears  bool                   `json:"auto_clears"`
	Dismissable bool                   `json:"dismissable"`
}

// AlertsSummary aggregate alert state
type AlertsSummary struct {
	TotalActive int            `json:"total_active"`
	BySeverity  map[string]int `json:"by_severity"`
	ByType      map[string]int `json:"by_type"`
	OldestAlert string         `json:"oldest_alert,omitempty"`
}

// SourceHealthSection normalized source health
type SourceHealthSection struct {
	Sources  map[string]SourceInfo `json:"sources"`
	Degraded []string              `json:"degraded,omitempty"`
	AllFresh bool                  `json:"all_fresh"`
}

// SourceInfo per-source health state
type SourceInfo struct {
	Name           string     `json:"name"`
	Available      bool       `json:"available"`
	Fresh          bool       `json:"fresh"`
	ReasonCode     ReasonCode `json:"reason_code"`
	AgeMs          int64      `json:"age_ms,omitempty"`
	UpdatedAt      string     `json:"updated_at,omitempty"`
	Degraded       bool       `json:"degraded,omitempty"`
	DegradedSince  string     `json:"degraded_since,omitempty"`
	DegradedReason string     `json:"degraded_reason,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	RetryingAt     string     `json:"retrying_at,omitempty"`
}

// HealthSource represents a source's health state for normalization
type HealthSource struct {
	Available  bool
	Fresh      bool
	LastUpdate *time.Time
	DegradedAt time.Time
	Reason     string
	LastError  string
}

// FormatTimestamp formats time as RFC3339
func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// matchesPattern checks if value matches pattern using glob semantics.
// Supports * for any sequence and ? for single character.
func matchesPattern(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	if pattern == value {
		return true
	}
	if pattern == "*" {
		return true
	}

	// Simple glob matching
	pIdx, vIdx := 0, 0
	starIdx, matchIdx := -1, 0

	for vIdx < len(value) {
		if pIdx < len(pattern) && (pattern[pIdx] == '?' || pattern[pIdx] == value[vIdx]) {
			pIdx++
			vIdx++
		} else if pIdx < len(pattern) && pattern[pIdx] == '*' {
			starIdx = pIdx
			matchIdx = vIdx
			pIdx++
		} else if starIdx != -1 {
			pIdx = starIdx + 1
			matchIdx++
			vIdx = matchIdx
		} else {
			return false
		}
	}

	for pIdx < len(pattern) && pattern[pIdx] == '*' {
		pIdx++
	}

	return pIdx == len(pattern)
}

// NormalizeQuota transforms caut cache data to QuotaSection
func NormalizeQuota(poller *caut.UsagePoller) *QuotaSection {
	section := &QuotaSection{
		Accounts:  make([]AccountQuota, 0),
		Available: poller != nil,
	}

	if poller == nil {
		section.Reason = "caut unavailable"
		return section
	}

	cache := poller.GetCache()
	if cache == nil {
		section.Reason = "cache empty"
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
			CostUSD:      usage.Cost,
			Status:       "ok",
			ReasonCode:   ReasonQuotaOK,
			IsActive:     true,
		}

		// Compute status from usage if we have status info
		if status != nil {
			for _, p := range status.Providers {
				if p.Name == usage.Provider {
					account.ReasonCode = computeQuotaReasonCode(p.QuotaUsed)
					account.Status = reasonToStatus(account.ReasonCode)
					break
				}
			}
		}

		section.Accounts = append(section.Accounts, account)
	}

	section.Summary = computeQuotaSummary(section.Accounts)
	return section
}

func computeQuotaReasonCode(usagePercent float64) ReasonCode {
	switch {
	case usagePercent >= 100.0:
		return ReasonQuotaExceededTokens
	case usagePercent >= 95.0:
		return ReasonQuotaCriticalTokens
	case usagePercent >= 80.0:
		return ReasonQuotaWarningTokens
	default:
		return ReasonQuotaOK
	}
}

func reasonToStatus(code ReasonCode) string {
	switch code {
	case ReasonQuotaExceededTokens, ReasonQuotaExceededRequests,
		ReasonQuotaExceededCost, ReasonQuotaSuspended:
		return "exceeded"
	case ReasonQuotaCriticalTokens, ReasonQuotaCriticalRequests:
		return "critical"
	case ReasonQuotaWarningTokens, ReasonQuotaWarningRequests,
		ReasonQuotaWarningRateLimit:
		return "warning"
	default:
		return "ok"
	}
}

func computeQuotaSummary(accounts []AccountQuota) *QuotaSummary {
	summary := &QuotaSummary{
		TotalAccounts: len(accounts),
	}

	for _, a := range accounts {
		switch a.Status {
		case "ok":
			summary.HealthyAccounts++
		case "warning", "critical":
			summary.WarningAccounts++
		case "exceeded":
			summary.ExceededAccounts++
		}
	}

	return summary
}

// NormalizeAlerts transforms alerts.Alert slice to AlertsSection
func NormalizeAlerts(alertList []alerts.Alert) *AlertsSection {
	section := &AlertsSection{
		Active: make([]AlertItem, 0, len(alertList)),
		Summary: &AlertsSummary{
			BySeverity: make(map[string]int),
			ByType:     make(map[string]int),
		},
	}

	var oldest time.Time

	for _, a := range alertList {
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
			ReasonCode:  computeAlertReasonCode(a.Type),
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

		if oldest.IsZero() || a.CreatedAt.Before(oldest) {
			oldest = a.CreatedAt
		}
	}

	if !oldest.IsZero() {
		section.Summary.OldestAlert = FormatTimestamp(oldest)
	}

	return section
}

func mapAlertType(t alerts.AlertType) string {
	switch t {
	case alerts.AlertAgentStuck:
		return "agent_stuck"
	case alerts.AlertAgentCrashed:
		return "agent_crashed"
	case alerts.AlertAgentError:
		return "agent_error"
	case alerts.AlertHighCPU:
		return "system_cpu"
	case alerts.AlertDiskLow:
		return "system_disk"
	case alerts.AlertBeadStale:
		return "bead_stale"
	case alerts.AlertMailBacklog:
		return "mail_backlog"
	case alerts.AlertDependencyCycle:
		return "bead_cycle"
	case alerts.AlertRateLimit:
		return "rate_limit"
	case alerts.AlertFileConflict:
		return "file_conflict"
	case alerts.AlertContextWarning:
		return "context_warning"
	case alerts.AlertRotationStarted:
		return "rotation_started"
	case alerts.AlertRotationComplete:
		return "rotation_complete"
	case alerts.AlertRotationFailed:
		return "rotation_failed"
	case alerts.AlertCompactionTriggered:
		return "compaction_triggered"
	case alerts.AlertCompactionComplete:
		return "compaction_complete"
	case alerts.AlertCompactionFailed:
		return "compaction_failed"
	case alerts.AlertQuotaWarning:
		return "quota_warning"
	case alerts.AlertQuotaCritical:
		return "quota_critical"
	default:
		return string(t)
	}
}

func computeAlertReasonCode(t alerts.AlertType) ReasonCode {
	switch t {
	case alerts.AlertAgentStuck:
		return ReasonAlertAgentStuck
	case alerts.AlertAgentCrashed:
		return ReasonAlertAgentCrashed
	case alerts.AlertAgentError:
		return ReasonAlertAgentError
	case alerts.AlertDiskLow:
		return ReasonAlertSystemDiskLow
	case alerts.AlertHighCPU:
		return ReasonAlertSystemCPUHigh
	case alerts.AlertBeadStale:
		return ReasonAlertBeadStale
	case alerts.AlertMailBacklog:
		return ReasonAlertMailBacklog
	case alerts.AlertFileConflict:
		return ReasonAlertConflictFile
	case alerts.AlertRateLimit:
		return ReasonAlertAgentRateLimited
	case alerts.AlertContextWarning:
		return ReasonAlertAgentContext
	case alerts.AlertRotationStarted:
		return ReasonAlertRotationStarted
	case alerts.AlertRotationComplete:
		return ReasonAlertRotationComplete
	case alerts.AlertRotationFailed:
		return ReasonAlertRotationFailed
	case alerts.AlertCompactionTriggered:
		return ReasonAlertCompactionTriggered
	case alerts.AlertCompactionComplete:
		return ReasonAlertCompactionComplete
	case alerts.AlertCompactionFailed:
		return ReasonAlertCompactionFailed
	case alerts.AlertQuotaWarning:
		return ReasonQuotaWarningTokens
	case alerts.AlertQuotaCritical:
		return ReasonQuotaCriticalTokens
	default:
		return ReasonAlertAgentError
	}
}

func alertAutoClears(t alerts.AlertType) bool {
	switch t {
	case alerts.AlertAgentCrashed, alerts.AlertRotationFailed,
		alerts.AlertCompactionFailed:
		return false
	default:
		return true
	}
}

// NormalizeHealth transforms health sources to SourceHealthSection
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

func computeHealthReasonCode(source HealthSource) ReasonCode {
	if !source.Available {
		return ReasonHealthSourceUnavailable
	}
	if !source.Fresh {
		return ReasonHealthSourceStale
	}
	if source.Reason != "" {
		return ReasonHealthSourceDegraded
	}
	return ReasonHealthOK
}

// SignalAggregator combines signals from multiple adapters
type SignalAggregator struct {
	adapters []SignalAdapter
	mu       sync.RWMutex
	cache    map[string]*SignalBatch
	cacheTTL time.Duration
}

// NewSignalAggregator creates a new aggregator
func NewSignalAggregator(ttl time.Duration) *SignalAggregator {
	return &SignalAggregator{
		adapters: make([]SignalAdapter, 0),
		cache:    make(map[string]*SignalBatch),
		cacheTTL: ttl,
	}
}

// RegisterAdapter adds an adapter to the aggregator
func (a *SignalAggregator) RegisterAdapter(adapter SignalAdapter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.adapters = append(a.adapters, adapter)
}

// Collect aggregates signals from all adapters
func (a *SignalAggregator) Collect(ctx context.Context) (*AggregatedSignals, error) {
	result := &AggregatedSignals{
		CollectedAt:  time.Now(),
		Work:         NewWorkSection(),
		Coordination: NewCoordinationSection(),
		Quota:        &QuotaSection{Accounts: []AccountQuota{}},
		Alerts:       &AlertsSection{Active: []AlertItem{}, Summary: &AlertsSummary{BySeverity: make(map[string]int), ByType: make(map[string]int)}},
		Health:       &SourceHealthSection{Sources: make(map[string]SourceInfo)},
	}

	a.mu.RLock()
	adapters := a.adapters
	a.mu.RUnlock()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, adapter := range adapters {
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

func mergeSignalBatch(target *AggregatedSignals, batch *SignalBatch) {
	if batch == nil {
		return
	}

	if batch.Work != nil {
		target.Work = batch.Work
	}

	if batch.Coordination != nil {
		target.Coordination = batch.Coordination
	}

	if batch.Quota != nil {
		target.Quota.Accounts = append(target.Quota.Accounts, batch.Quota.Accounts...)
		if batch.Quota.Summary != nil && target.Quota.Summary == nil {
			target.Quota.Summary = batch.Quota.Summary
		}
	}

	if batch.Alerts != nil {
		target.Alerts.Active = append(target.Alerts.Active, batch.Alerts.Active...)
		if batch.Alerts.Summary != nil {
			target.Alerts.Summary.TotalActive += batch.Alerts.Summary.TotalActive
			for k, v := range batch.Alerts.Summary.BySeverity {
				target.Alerts.Summary.BySeverity[k] += v
			}
			for k, v := range batch.Alerts.Summary.ByType {
				target.Alerts.Summary.ByType[k] += v
			}
		}
	}

	if batch.Health != nil {
		for k, v := range batch.Health.Sources {
			target.Health.Sources[k] = v
		}
		target.Health.Degraded = append(target.Health.Degraded, batch.Health.Degraded...)
		if !batch.Health.AllFresh {
			target.Health.AllFresh = false
		}
	}
}
