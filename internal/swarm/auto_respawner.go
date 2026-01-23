package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ntmcontext "github.com/Dicklesworthstone/ntm/internal/context"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// AccountRotatorI provides account rotation for limit recovery (optional).
// If not provided, agents will be respawned with the same account.
// Note: The concrete AccountRotator struct in account_rotator.go implements this interface.
type AccountRotatorI interface {
	// RotateAccount switches to the next available account for the agent type.
	// Returns the new account identifier, or error if no accounts available.
	RotateAccount(agentType string) (newAccount string, err error)

	// CurrentAccount returns the current account for the agent type.
	CurrentAccount(agentType string) string
}

// RespawnResult tracks the result of a single respawn attempt.
type RespawnResult struct {
	Success         bool          `json:"success"`
	SessionPane     string        `json:"session_pane"`
	AgentType       string        `json:"agent_type"`
	AccountRotated  bool          `json:"account_rotated"`
	PreviousAccount string        `json:"previous_account,omitempty"`
	NewAccount      string        `json:"new_account,omitempty"`
	Duration        time.Duration `json:"duration"`
	Error           string        `json:"error,omitempty"`
	RespawnedAt     time.Time     `json:"respawned_at"`
}

// AutoRespawnerConfig holds configuration for the AutoRespawner.
type AutoRespawnerConfig struct {
	// GracefulExitDelay is how long to wait for the agent to exit gracefully.
	// Default: 2 seconds
	GracefulExitDelay time.Duration

	// AgentReadyDelay is how long to wait for a new agent to be ready.
	// Default: 5 seconds
	AgentReadyDelay time.Duration

	// MaxRetriesPerPane limits respawns per pane to prevent infinite loops.
	// Default: 3
	MaxRetriesPerPane int

	// RetryResetDuration resets the retry counter after this duration.
	// Default: 1 hour
	RetryResetDuration time.Duration

	// ClearPaneDelay is how long to wait after sending clear command.
	// Default: 100ms
	ClearPaneDelay time.Duration

	// ExitWaitTimeout is the maximum time to wait for agent exit verification.
	// Default: 5 seconds
	ExitWaitTimeout time.Duration

	// ExitPollInterval is the interval between exit verification checks.
	// Default: 500ms
	ExitPollInterval time.Duration

	// AutoRotateAccounts enables automatic account rotation on limit hit.
	// Default: false
	AutoRotateAccounts bool
}

// DefaultAutoRespawnerConfig returns sensible defaults.
func DefaultAutoRespawnerConfig() AutoRespawnerConfig {
	return AutoRespawnerConfig{
		GracefulExitDelay:  2 * time.Second,
		AgentReadyDelay:    5 * time.Second,
		MaxRetriesPerPane:  3,
		RetryResetDuration: 1 * time.Hour,
		ClearPaneDelay:     100 * time.Millisecond,
		ExitWaitTimeout:    5 * time.Second,
		ExitPollInterval:   500 * time.Millisecond,
		AutoRotateAccounts: false,
	}
}

// paneRetryState tracks retry attempts for a pane.
type paneRetryState struct {
	Count     int
	LastReset time.Time
}

// AutoRespawner handles automatic agent recovery when usage limits are hit.
type AutoRespawner struct {
	// Config holds respawn settings.
	Config AutoRespawnerConfig

	// LimitDetector provides limit events.
	LimitDetector *LimitDetector

	// AccountRotator switches accounts (optional).
	AccountRotator AccountRotatorI

	// PromptInjector re-sends marching orders.
	PromptInjector *PromptInjector

	// PaneSpawner from context package (REUSES EXISTING interface).
	PaneSpawner ntmcontext.PaneSpawner

	// TmuxClient for direct tmux operations.
	// If nil, the default tmux client is used.
	TmuxClient *tmux.Client

	// Logger for structured logging.
	Logger *slog.Logger

	// eventChan emits respawn events.
	eventChan chan RespawnEvent

	// mu protects internal state.
	mu sync.RWMutex

	// retryState tracks retries per pane.
	retryState map[string]*paneRetryState

	// ctx is the context for all respawn goroutines.
	ctx context.Context

	// cancel stops all respawn goroutines.
	cancel context.CancelFunc
}

// NewAutoRespawner creates a new AutoRespawner with default settings.
func NewAutoRespawner() *AutoRespawner {
	return &AutoRespawner{
		Config:         DefaultAutoRespawnerConfig(),
		LimitDetector:  nil,
		AccountRotator: nil,
		PromptInjector: nil,
		PaneSpawner:    nil,
		TmuxClient:     nil,
		Logger:         slog.Default(),
		eventChan:      make(chan RespawnEvent, 100),
		retryState:     make(map[string]*paneRetryState),
	}
}

// WithLimitDetector sets the limit detector.
func (r *AutoRespawner) WithLimitDetector(ld *LimitDetector) *AutoRespawner {
	r.LimitDetector = ld
	return r
}

// WithAccountRotator sets the account rotator (optional).
func (r *AutoRespawner) WithAccountRotator(ar AccountRotatorI) *AutoRespawner {
	r.AccountRotator = ar
	return r
}

// WithPromptInjector sets the prompt injector.
func (r *AutoRespawner) WithPromptInjector(pi *PromptInjector) *AutoRespawner {
	r.PromptInjector = pi
	return r
}

// WithPaneSpawner sets the pane spawner.
func (r *AutoRespawner) WithPaneSpawner(ps ntmcontext.PaneSpawner) *AutoRespawner {
	r.PaneSpawner = ps
	return r
}

// WithTmuxClient sets the tmux client.
func (r *AutoRespawner) WithTmuxClient(client *tmux.Client) *AutoRespawner {
	r.TmuxClient = client
	return r
}

// WithLogger sets a custom logger.
func (r *AutoRespawner) WithLogger(logger *slog.Logger) *AutoRespawner {
	r.Logger = logger
	return r
}

// WithConfig sets the configuration.
func (r *AutoRespawner) WithConfig(cfg AutoRespawnerConfig) *AutoRespawner {
	r.Config = cfg
	return r
}

// tmuxClient returns the configured tmux client or the default client.
func (r *AutoRespawner) tmuxClient() *tmux.Client {
	if r.TmuxClient != nil {
		return r.TmuxClient
	}
	return tmux.DefaultClient
}

// logger returns the configured logger or the default logger.
func (r *AutoRespawner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Events returns the channel that emits respawn events.
func (r *AutoRespawner) Events() <-chan RespawnEvent {
	return r.eventChan
}

// Start begins listening for limit events and auto-respawning agents.
func (r *AutoRespawner) Start(ctx context.Context) error {
	if r.LimitDetector == nil {
		return fmt.Errorf("LimitDetector is required")
	}

	r.mu.Lock()
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	r.logger().Info("[AutoRespawner] starting",
		"auto_rotate", r.Config.AutoRotateAccounts,
		"max_retries", r.Config.MaxRetriesPerPane)

	// Start the event processing goroutine
	go r.processLimitEvents()

	return nil
}

// Stop halts the auto-respawner.
func (r *AutoRespawner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}

	r.logger().Info("[AutoRespawner] stopped")
}

// processLimitEvents listens for limit events and triggers respawns.
func (r *AutoRespawner) processLimitEvents() {
	events := r.LimitDetector.Events()

	for {
		select {
		case <-r.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			r.logger().Info("[AutoRespawner] limit_event_received",
				"session_pane", event.SessionPane,
				"agent_type", event.AgentType,
				"pattern", event.Pattern)

			// Check retry limits
			if r.isRetryLimitExceeded(event.SessionPane) {
				r.logger().Warn("[AutoRespawner] retry_limit_exceeded",
					"session_pane", event.SessionPane,
					"max_retries", r.Config.MaxRetriesPerPane)
				continue
			}

			// Attempt respawn
			result := r.Respawn(event.SessionPane, event.AgentType)
			if result.Success {
				r.recordRetryAttempt(event.SessionPane)
			}
		}
	}
}

// isRetryLimitExceeded checks if a pane has exceeded its retry limit.
func (r *AutoRespawner) isRetryLimitExceeded(sessionPane string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, exists := r.retryState[sessionPane]
	if !exists {
		return false
	}

	// Reset counter if enough time has passed
	if time.Since(state.LastReset) > r.Config.RetryResetDuration {
		return false
	}

	return state.Count >= r.Config.MaxRetriesPerPane
}

// recordRetryAttempt increments the retry counter for a pane.
func (r *AutoRespawner) recordRetryAttempt(sessionPane string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, exists := r.retryState[sessionPane]
	if !exists || time.Since(state.LastReset) > r.Config.RetryResetDuration {
		r.retryState[sessionPane] = &paneRetryState{
			Count:     1,
			LastReset: time.Now(),
		}
		return
	}

	state.Count++
}

// Respawn performs the full respawn sequence for an agent.
func (r *AutoRespawner) Respawn(sessionPane, agentType string) *RespawnResult {
	start := time.Now()
	result := &RespawnResult{
		SessionPane: sessionPane,
		AgentType:   agentType,
		RespawnedAt: start,
	}

	r.logger().Info("[AutoRespawner] respawn_start",
		"session_pane", sessionPane,
		"agent_type", agentType)

	// Step 1: Kill the stuck agent
	if err := r.killAgent(sessionPane, agentType); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("kill agent failed: %v", err)
		result.Duration = time.Since(start)

		r.logger().Error("[AutoRespawner] respawn_failed",
			"session_pane", sessionPane,
			"stage", "kill",
			"error", err,
			"duration", result.Duration)

		return result
	}

	// Step 2: Wait for graceful exit with verification
	if !r.waitForExit(sessionPane) {
		// Agent didn't exit cleanly within timeout, wait a bit more
		r.logger().Warn("[AutoRespawner] agent_exit_not_verified",
			"session_pane", sessionPane,
			"continuing", true)
		time.Sleep(r.Config.GracefulExitDelay)
	}

	// Step 3: (Optional) Rotate account
	if r.Config.AutoRotateAccounts && r.AccountRotator != nil {
		previousAccount := r.AccountRotator.CurrentAccount(agentType)
		newAccount, err := r.AccountRotator.RotateAccount(agentType)
		if err != nil {
			r.logger().Warn("[AutoRespawner] account_rotation_failed",
				"session_pane", sessionPane,
				"agent_type", agentType,
				"error", err)
			// Continue without rotation - not fatal
		} else {
			result.AccountRotated = true
			result.PreviousAccount = previousAccount
			result.NewAccount = newAccount

			r.logger().Info("[AutoRespawner] account_rotated",
				"session_pane", sessionPane,
				"old", previousAccount,
				"new", newAccount)
		}
	}

	// Step 4: Clear the pane
	if err := r.clearPane(sessionPane); err != nil {
		r.logger().Warn("[AutoRespawner] clear_pane_failed",
			"session_pane", sessionPane,
			"error", err)
		// Continue anyway - not fatal
	}

	// Step 5: Respawn the agent
	if err := r.spawnAgent(sessionPane, agentType); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("spawn agent failed: %v", err)
		result.Duration = time.Since(start)

		r.logger().Error("[AutoRespawner] respawn_failed",
			"session_pane", sessionPane,
			"stage", "spawn",
			"error", err,
			"duration", result.Duration)

		return result
	}

	// Step 6: Wait for agent to be ready
	time.Sleep(r.Config.AgentReadyDelay)

	// Step 7: Re-inject marching orders
	if r.PromptInjector != nil {
		prompt := r.PromptInjector.GetTemplate("default")
		if _, err := r.PromptInjector.InjectPrompt(sessionPane, agentType, prompt); err != nil {
			r.logger().Warn("[AutoRespawner] injecting_prompt_failed",
				"session_pane", sessionPane,
				"error", err)
			// Continue - agent is running, just without marching orders
		} else {
			r.logger().Info("[AutoRespawner] injecting_prompt",
				"session_pane", sessionPane,
				"prompt_len", len(prompt))
		}
	}

	// Step 8: Success!
	result.Success = true
	result.Duration = time.Since(start)

	r.logger().Info("[AutoRespawner] respawn_complete",
		"session_pane", sessionPane,
		"success", true,
		"duration", result.Duration,
		"account_rotated", result.AccountRotated)

	// Step 9: Emit respawn event
	r.emitEvent(result)

	return result
}

// killAgent sends the appropriate kill sequence for the agent type.
func (r *AutoRespawner) killAgent(sessionPane, agentType string) error {
	r.logger().Info("[AutoRespawner] killing_agent",
		"session_pane", sessionPane,
		"method", agentType)

	client := r.tmuxClient()

	switch agentType {
	case "cc", "claude", "claude-code":
		// Claude: Double Ctrl+C with 100ms gap (CRITICAL timing)
		if err := client.SendKeys(sessionPane, "\x03", false); err != nil {
			return fmt.Errorf("send first ctrl-c: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
		if err := client.SendKeys(sessionPane, "\x03", false); err != nil {
			return fmt.Errorf("send second ctrl-c: %w", err)
		}

	case "cod", "codex":
		// Codex: /exit command
		if err := client.SendKeys(sessionPane, "/exit", true); err != nil {
			return fmt.Errorf("send /exit: %w", err)
		}

	case "gmi", "gemini":
		// Gemini: Escape then Ctrl+C
		if err := client.SendKeys(sessionPane, "\x1b", false); err != nil {
			return fmt.Errorf("send escape: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
		if err := client.SendKeys(sessionPane, "\x03", false); err != nil {
			return fmt.Errorf("send ctrl-c: %w", err)
		}

	default:
		// Default: Double Ctrl+C (safe fallback)
		if err := client.SendKeys(sessionPane, "\x03", false); err != nil {
			return fmt.Errorf("send first ctrl-c: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
		if err := client.SendKeys(sessionPane, "\x03", false); err != nil {
			return fmt.Errorf("send second ctrl-c: %w", err)
		}
	}

	return nil
}

// waitForExit waits for agent to terminate, returns true if exited.
// It checks the pane output for shell prompt indicators.
func (r *AutoRespawner) waitForExit(sessionPane string) bool {
	timeout := r.Config.ExitWaitTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	pollInterval := r.Config.ExitPollInterval
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C

		// Check if pane is back to shell prompt
		output := r.capturePaneOutput(sessionPane, 5)
		if output == "" {
			continue
		}

		if r.isShellPrompt(output) {
			r.logger().Info("[AutoRespawner] agent_exited",
				"session_pane", sessionPane)
			return true
		}
	}

	r.logger().Warn("[AutoRespawner] exit_timeout",
		"session_pane", sessionPane,
		"timeout", timeout)
	return false
}

// isShellPrompt checks if the output indicates a shell prompt.
func (r *AutoRespawner) isShellPrompt(output string) bool {
	if len(output) == 0 {
		return false
	}

	// Common shell prompt indicators
	prompts := []string{"$", "%", ">", "#", "➜", "❯"}

	// Check if the last non-whitespace line ends with a prompt
	lines := splitLines(output)
	for i := len(lines) - 1; i >= 0; i-- {
		line := trimWhitespace(lines[i])
		if line == "" {
			continue
		}
		// Check if line ends with a prompt character
		for _, prompt := range prompts {
			if len(line) > 0 && line[len(line)-1] == prompt[0] {
				return true
			}
		}
		// Also check for common prompt patterns anywhere in line
		if containsAny(line, prompts) {
			return true
		}
		// Only check the last non-empty line
		break
	}
	return false
}

// splitLines splits output into lines without importing strings.
func splitLines(s string) []string {
	var lines []string
	var current []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, string(current))
			current = current[:0]
		} else if s[i] != '\r' {
			current = append(current, s[i])
		}
	}
	if len(current) > 0 {
		lines = append(lines, string(current))
	}
	return lines
}

// trimWhitespace trims leading/trailing whitespace without importing strings.
func trimWhitespace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// capturePaneOutput captures the last N lines from a pane.
func (r *AutoRespawner) capturePaneOutput(sessionPane string, lines int) string {
	client := r.tmuxClient()

	output, err := client.CapturePaneOutput(sessionPane, lines)
	if err != nil {
		r.logger().Debug("[AutoRespawner] capture_failed",
			"session_pane", sessionPane,
			"error", err)
		return ""
	}

	return output
}

// clearPane sends the clear command to reset the terminal.
func (r *AutoRespawner) clearPane(sessionPane string) error {
	client := r.tmuxClient()

	if err := client.SendKeys(sessionPane, "clear", true); err != nil {
		return fmt.Errorf("send clear: %w", err)
	}

	time.Sleep(r.Config.ClearPaneDelay)
	return nil
}

// spawnAgent launches the agent command in the pane.
func (r *AutoRespawner) spawnAgent(sessionPane, agentType string) error {
	r.logger().Info("[AutoRespawner] spawning_agent",
		"session_pane", sessionPane,
		"agent_type", agentType)

	client := r.tmuxClient()

	// Get the agent command
	cmd := r.getAgentCommand(agentType)

	if err := client.SendKeys(sessionPane, cmd, true); err != nil {
		return fmt.Errorf("send agent command: %w", err)
	}

	return nil
}

// getAgentCommand returns the command to launch an agent.
func (r *AutoRespawner) getAgentCommand(agentType string) string {
	switch agentType {
	case "cc", "claude", "claude-code":
		return "cc"
	case "cod", "codex":
		return "cod"
	case "gmi", "gemini":
		return "gmi"
	default:
		return agentType
	}
}

// emitEvent sends a respawn event to the event channel.
func (r *AutoRespawner) emitEvent(result *RespawnResult) {
	event := RespawnEvent{
		SessionPane:     result.SessionPane,
		AgentType:       result.AgentType,
		RespawnedAt:     result.RespawnedAt,
		AccountRotated:  result.AccountRotated,
		PreviousAccount: result.PreviousAccount,
		NewAccount:      result.NewAccount,
	}

	// Non-blocking send
	select {
	case r.eventChan <- event:
		// Event sent successfully
	default:
		r.logger().Warn("[AutoRespawner] event_channel_full",
			"session_pane", result.SessionPane)
	}
}

// GetRetryCount returns the current retry count for a pane.
func (r *AutoRespawner) GetRetryCount(sessionPane string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	state, exists := r.retryState[sessionPane]
	if !exists {
		return 0
	}

	// Reset if expired
	if time.Since(state.LastReset) > r.Config.RetryResetDuration {
		return 0
	}

	return state.Count
}

// ResetRetryCount resets the retry counter for a pane.
func (r *AutoRespawner) ResetRetryCount(sessionPane string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.retryState, sessionPane)
}

// ResetAllRetryCounts clears all retry counters.
func (r *AutoRespawner) ResetAllRetryCounts() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.retryState = make(map[string]*paneRetryState)
}
