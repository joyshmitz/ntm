package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CAAMAdapter provides integration with the CAAM (Coding Agent Account Manager) tool.
// CAAM manages multiple accounts for AI coding agents and handles automatic
// account rotation when rate limits are hit.
type CAAMAdapter struct {
	*BaseAdapter
}

// NewCAAMAdapter creates a new CAAM adapter
func NewCAAMAdapter() *CAAMAdapter {
	return &CAAMAdapter{
		BaseAdapter: NewBaseAdapter(ToolCAAM, "caam"),
	}
}

// Detect checks if caam is installed
func (a *CAAMAdapter) Detect() (string, bool) {
	path, err := exec.LookPath(a.BinaryName())
	if err != nil {
		return "", false
	}
	return path, true
}

// Version returns the installed caam version
func (a *CAAMAdapter) Version(ctx context.Context) (Version, error) {
	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, a.BinaryName(), "--version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return Version{}, fmt.Errorf("failed to get caam version: %w", err)
	}

	return parseVersion(stdout.String())
}

// Capabilities returns the list of caam capabilities
func (a *CAAMAdapter) Capabilities(ctx context.Context) ([]Capability, error) {
	caps := []Capability{}

	path, installed := a.Detect()
	if !installed {
		return caps, nil
	}

	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "help")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	_ = cmd.Run() // Ignore error, just check output

	output := stdout.String()

	// Check for known capabilities
	if strings.Contains(output, "--json") || strings.Contains(output, "robot") {
		caps = append(caps, CapRobotMode)
	}

	return caps, nil
}

// Health checks if caam is functioning correctly
func (a *CAAMAdapter) Health(ctx context.Context) (*HealthStatus, error) {
	start := time.Now()

	path, installed := a.Detect()
	if !installed {
		return &HealthStatus{
			Healthy:     false,
			Message:     "caam not installed",
			LastChecked: time.Now(),
		}, nil
	}

	// Try to get version as a basic health check
	_, err := a.Version(ctx)
	latency := time.Since(start)

	if err != nil {
		return &HealthStatus{
			Healthy:     false,
			Message:     fmt.Sprintf("caam at %s not responding", path),
			Error:       err.Error(),
			LastChecked: time.Now(),
			Latency:     latency,
		}, nil
	}

	return &HealthStatus{
		Healthy:     true,
		Message:     "caam is healthy",
		LastChecked: time.Now(),
		Latency:     latency,
	}, nil
}

// HasCapability checks if caam has a specific capability
func (a *CAAMAdapter) HasCapability(ctx context.Context, cap Capability) bool {
	caps, err := a.Capabilities(ctx)
	if err != nil {
		return false
	}
	for _, c := range caps {
		if c == cap {
			return true
		}
	}
	return false
}

// Info returns complete caam tool information
func (a *CAAMAdapter) Info(ctx context.Context) (*ToolInfo, error) {
	return a.BaseAdapter.Info(ctx, a)
}

// CAAM-specific types and methods

// CAAMAccount represents an account managed by CAAM
type CAAMAccount struct {
	ID            string    `json:"id"`
	Provider      string    `json:"provider"`
	Email         string    `json:"email,omitempty"`
	Name          string    `json:"name,omitempty"`
	Active        bool      `json:"active"`
	RateLimited   bool      `json:"rate_limited,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
}

// CAAMStatus represents the current CAAM status
type CAAMStatus struct {
	Available     bool          `json:"available"`
	Version       string        `json:"version,omitempty"`
	AccountsCount int           `json:"accounts_count"`
	Providers     []string      `json:"providers,omitempty"`
	ActiveAccount *CAAMAccount  `json:"active_account,omitempty"`
	Accounts      []CAAMAccount `json:"accounts,omitempty"`
}

// Cached status to avoid repeated lookups
var (
	caamStatusOnce   sync.Once
	caamStatusCache  CAAMStatus
	caamStatusExpiry time.Time
	caamStatusMutex  sync.RWMutex
	caamCacheTTL     = 5 * time.Minute
)

// GetStatus returns the current CAAM status with caching
func (a *CAAMAdapter) GetStatus(ctx context.Context) (*CAAMStatus, error) {
	// Check cache first
	caamStatusMutex.RLock()
	if time.Now().Before(caamStatusExpiry) {
		status := caamStatusCache
		caamStatusMutex.RUnlock()
		return &status, nil
	}
	caamStatusMutex.RUnlock()

	// Fetch fresh status
	status, err := a.fetchStatus(ctx)
	if err != nil {
		return nil, err
	}

	// Update cache
	caamStatusMutex.Lock()
	caamStatusCache = *status
	caamStatusExpiry = time.Now().Add(caamCacheTTL)
	caamStatusMutex.Unlock()

	return status, nil
}

// fetchStatus fetches fresh status from caam
func (a *CAAMAdapter) fetchStatus(ctx context.Context) (*CAAMStatus, error) {
	status := &CAAMStatus{}

	// Check if caam is installed
	path, installed := a.Detect()
	if !installed {
		status.Available = false
		return status, nil
	}
	status.Available = true

	// Get version
	version, err := a.Version(ctx)
	if err == nil {
		status.Version = version.String()
	}

	// Get accounts list
	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "list", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// caam might not have accounts configured - that's ok
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrTimeout
		}
		// Return status without accounts
		return status, nil
	}

	output := stdout.Bytes()
	if len(output) == 0 || !json.Valid(output) {
		return status, nil
	}

	// Parse accounts list
	var accounts []CAAMAccount
	if err := json.Unmarshal(output, &accounts); err != nil {
		// Try parsing as a status object instead
		var statusResp struct {
			Accounts []CAAMAccount `json:"accounts"`
		}
		if err := json.Unmarshal(output, &statusResp); err == nil {
			accounts = statusResp.Accounts
		}
	}

	status.Accounts = accounts
	status.AccountsCount = len(accounts)

	// Extract unique providers
	providerSet := make(map[string]bool)
	for _, acc := range accounts {
		if acc.Provider != "" {
			providerSet[acc.Provider] = true
		}
		if acc.Active {
			status.ActiveAccount = &acc
		}
	}
	for p := range providerSet {
		status.Providers = append(status.Providers, p)
	}

	return status, nil
}

// GetAccounts returns the list of configured accounts
func (a *CAAMAdapter) GetAccounts(ctx context.Context) ([]CAAMAccount, error) {
	status, err := a.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	return status.Accounts, nil
}

// GetActiveAccount returns the currently active account
func (a *CAAMAdapter) GetActiveAccount(ctx context.Context) (*CAAMAccount, error) {
	status, err := a.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	return status.ActiveAccount, nil
}

// SwitchAccount switches to a different account
func (a *CAAMAdapter) SwitchAccount(ctx context.Context, accountID string) error {
	path, installed := a.Detect()
	if !installed {
		return ErrToolNotInstalled
	}

	ctx, cancel := context.WithTimeout(ctx, a.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "switch", accountID)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ErrTimeout
		}
		return fmt.Errorf("failed to switch account: %w: %s", err, stderr.String())
	}

	// Invalidate cache after switch
	caamStatusMutex.Lock()
	caamStatusExpiry = time.Time{}
	caamStatusMutex.Unlock()

	return nil
}

// InvalidateCache forces the next GetStatus call to fetch fresh data
func (a *CAAMAdapter) InvalidateCache() {
	caamStatusMutex.Lock()
	caamStatusExpiry = time.Time{}
	caamStatusMutex.Unlock()
}

// IsAvailable is a convenience method that returns true if caam is installed
// and has at least one account configured
func (a *CAAMAdapter) IsAvailable(ctx context.Context) bool {
	status, err := a.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.Available && status.AccountsCount > 0
}

// HasMultipleAccounts returns true if caam has more than one account configured
func (a *CAAMAdapter) HasMultipleAccounts(ctx context.Context) bool {
	status, err := a.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.AccountsCount > 1
}

// RateLimitDetector monitors agent output for rate limit signals and triggers CAAM account switching.
// It uses pattern matching to detect rate limit messages from different AI providers.
type RateLimitDetector struct {
	adapter        *CAAMAdapter
	patterns       map[string][]*rateLimitPattern // provider -> patterns
	onLimitDetect  RateLimitCallback              // Called when rate limit is detected
	mu             sync.RWMutex
	lastDetection  map[string]time.Time // provider -> last detection time
	cooldownPeriod time.Duration        // Minimum time between detections per provider
}

// rateLimitPattern holds a compiled regex pattern and its metadata
type rateLimitPattern struct {
	pattern *regexp.Regexp
	name    string // Human-readable name for logging
}

// RateLimitCallback is called when a rate limit is detected
type RateLimitCallback func(provider string, paneID string, patterns []string)

// RateLimitEvent contains information about a detected rate limit
type RateLimitEvent struct {
	Provider      string    `json:"provider"`       // claude, openai, gemini
	PaneID        string    `json:"pane_id"`        // Pane where limit was detected
	DetectedAt    time.Time `json:"detected_at"`    // When limit was detected
	Patterns      []string  `json:"patterns"`       // Which patterns matched
	WaitSeconds   int       `json:"wait_seconds"`   // Suggested wait time (if detected)
	AccountBefore string    `json:"account_before"` // Account before switch (if any)
	AccountAfter  string    `json:"account_after"`  // Account after switch (if any)
	SwitchSuccess bool      `json:"switch_success"` // Whether account switch succeeded
}

// NewRateLimitDetector creates a new rate limit detector with default patterns
func NewRateLimitDetector(adapter *CAAMAdapter) *RateLimitDetector {
	d := &RateLimitDetector{
		adapter:        adapter,
		patterns:       make(map[string][]*rateLimitPattern),
		lastDetection:  make(map[string]time.Time),
		cooldownPeriod: 30 * time.Second, // Minimum 30 seconds between detections
	}

	// Initialize patterns for each provider
	d.initClaudePatterns()
	d.initOpenAIPatterns()
	d.initGeminiPatterns()

	return d
}

// initClaudePatterns sets up rate limit patterns for Claude
func (d *RateLimitDetector) initClaudePatterns() {
	patterns := []string{
		`(?i)you['']?ve\s+hit\s+your\s+limit`,
		`(?i)please\s+wait`,
		`(?i)try\s+again\s+later`,
		`(?i)too\s+many\s+requests`,
		`(?i)usage\s+limit`,
		`(?i)request\s+limit`,
		`(?i)limit.*exceeded`,
		`(?i)api\s+limit`,
		`(?i)anthropic.*limit`,
		`(?i)claude.*limit`,
	}
	d.patterns["claude"] = compilePatterns(patterns)
}

// initOpenAIPatterns sets up rate limit patterns for OpenAI/Codex
func (d *RateLimitDetector) initOpenAIPatterns() {
	patterns := []string{
		`(?i)you['']?ve\s+reached\s+your\s+usage\s+limit`,
		`(?i)rate[\s_-]*limit`,
		`(?i)quota\s+exceeded`,
		`(?i)capacity\s+reached`,
		`(?i)maximum\s+requests`,
		`(?i)429`,
		`(?i)tokens?\s+per\s+min`,
		`(?i)requests?\s+per\s+min`,
		`(?i)openai.*limit`,
		`(?i)gpt.*limit`,
		`(?i)codex.*limit`,
	}
	d.patterns["openai"] = compilePatterns(patterns)
}

// initGeminiPatterns sets up rate limit patterns for Gemini
func (d *RateLimitDetector) initGeminiPatterns() {
	patterns := []string{
		`(?i)resource[\s_-]*exhausted`,
		`(?i)RESOURCE_EXHAUSTED`,
		`(?i)limit\s+reached`,
		`(?i)gemini.*limit`,
		`(?i)google.*limit`,
		`(?i)bard.*limit`,
	}
	d.patterns["gemini"] = compilePatterns(patterns)
}

// compilePatterns compiles string patterns into regex
func compilePatterns(patterns []string) []*rateLimitPattern {
	result := make([]*rateLimitPattern, 0, len(patterns))
	for _, p := range patterns {
		compiled, err := regexp.Compile(p)
		if err != nil {
			continue // Skip invalid patterns
		}
		result = append(result, &rateLimitPattern{
			pattern: compiled,
			name:    p,
		})
	}
	return result
}

// SetCallback sets the callback function for rate limit detection
func (d *RateLimitDetector) SetCallback(cb RateLimitCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onLimitDetect = cb
}

// SetCooldownPeriod sets the minimum time between detections per provider
func (d *RateLimitDetector) SetCooldownPeriod(period time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cooldownPeriod = period
}

// Check analyzes output for rate limit signals and triggers callback if found.
// It returns the detected event if a rate limit was found, nil otherwise.
// Providers are checked in order: claude, openai, gemini (deterministic).
func (d *RateLimitDetector) Check(output string, paneID string) *RateLimitEvent {
	// Check providers in deterministic order
	providerOrder := []string{"claude", "openai", "gemini"}
	for _, provider := range providerOrder {
		patterns := d.patterns[provider]
		if patterns == nil {
			continue
		}
		matched := d.matchPatterns(output, patterns)
		if len(matched) > 0 {
			// Check cooldown
			d.mu.RLock()
			lastTime := d.lastDetection[provider]
			cooldown := d.cooldownPeriod
			d.mu.RUnlock()

			if time.Since(lastTime) < cooldown {
				continue // Still in cooldown
			}

			// Update last detection time
			d.mu.Lock()
			d.lastDetection[provider] = time.Now()
			callback := d.onLimitDetect
			d.mu.Unlock()

			event := &RateLimitEvent{
				Provider:   provider,
				PaneID:     paneID,
				DetectedAt: time.Now(),
				Patterns:   matched,
			}

			// Parse wait time if present
			event.WaitSeconds = parseWaitTimeFromOutput(output)

			// Invoke callback if set
			if callback != nil {
				callback(provider, paneID, matched)
			}

			return event
		}
	}
	return nil
}

// matchPatterns returns names of all patterns that matched in the output
func (d *RateLimitDetector) matchPatterns(output string, patterns []*rateLimitPattern) []string {
	var matched []string
	for _, p := range patterns {
		if p.pattern.MatchString(output) {
			matched = append(matched, p.name)
		}
	}
	return matched
}

// parseWaitTimeFromOutput extracts wait time from rate limit messages
func parseWaitTimeFromOutput(output string) int {
	waitPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)try\s+again\s+in\s+(\d+)\s*s`),
		regexp.MustCompile(`(?i)wait\s+(\d+)\s*(?:second|sec|s)`),
		regexp.MustCompile(`(?i)retry\s+(?:after|in)\s+(\d+)\s*(?:s|sec)`),
		regexp.MustCompile(`(?i)(\d+)\s*(?:second|sec)s?\s+(?:cooldown|delay|wait)`),
		regexp.MustCompile(`(?i)rate.?limit.*?(\d+)\s*s`),
	}

	for _, pattern := range waitPatterns {
		if matches := pattern.FindStringSubmatch(output); len(matches) > 1 {
			var seconds int
			if _, err := fmt.Sscanf(matches[1], "%d", &seconds); err == nil && seconds > 0 {
				return seconds
			}
		}
	}
	return 0
}

// TriggerAccountSwitch attempts to switch CAAM accounts when rate limit is detected.
// Returns the event with switch results populated.
func (d *RateLimitDetector) TriggerAccountSwitch(ctx context.Context, event *RateLimitEvent) *RateLimitEvent {
	if d.adapter == nil {
		return event
	}

	// Check if CAAM is available and has multiple accounts
	if !d.adapter.IsAvailable(ctx) {
		return event
	}
	if !d.adapter.HasMultipleAccounts(ctx) {
		return event
	}

	// Get current account before switch
	currentAccount, _ := d.adapter.GetActiveAccount(ctx)
	if currentAccount != nil {
		event.AccountBefore = currentAccount.ID
	}

	// Get available accounts and find next non-rate-limited one
	accounts, err := d.adapter.GetAccounts(ctx)
	if err != nil {
		return event
	}

	// Find next available account (not rate limited, not current)
	for _, acc := range accounts {
		if acc.Active {
			continue // Skip current account
		}
		if acc.RateLimited {
			continue // Skip rate limited accounts
		}
		if !acc.CooldownUntil.IsZero() && time.Now().Before(acc.CooldownUntil) {
			continue // Skip accounts in cooldown
		}

		// Try to switch to this account
		if err := d.adapter.SwitchAccount(ctx, acc.ID); err == nil {
			event.AccountAfter = acc.ID
			event.SwitchSuccess = true
			return event
		}
	}

	// No available account found
	event.SwitchSuccess = false
	return event
}

// DetectProvider attempts to determine the AI provider from pane output
func DetectProvider(output string) string {
	outputLower := strings.ToLower(output)

	// Claude indicators
	if strings.Contains(outputLower, "claude") ||
		strings.Contains(outputLower, "anthropic") ||
		strings.Contains(outputLower, "sonnet") ||
		strings.Contains(outputLower, "opus") ||
		strings.Contains(outputLower, "haiku") {
		return "claude"
	}

	// OpenAI/Codex indicators
	if strings.Contains(outputLower, "openai") ||
		strings.Contains(outputLower, "codex") ||
		strings.Contains(outputLower, "gpt-") ||
		strings.Contains(outputLower, "gpt4") ||
		strings.Contains(outputLower, "gpt5") {
		return "openai"
	}

	// Gemini indicators
	if strings.Contains(outputLower, "gemini") ||
		strings.Contains(outputLower, "google") ||
		strings.Contains(outputLower, "bard") {
		return "gemini"
	}

	return "unknown"
}
