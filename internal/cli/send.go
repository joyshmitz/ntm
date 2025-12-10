package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/checkpoint"
	"github.com/Dicklesworthstone/ntm/internal/events"
	"github.com/Dicklesworthstone/ntm/internal/hooks"
	"github.com/Dicklesworthstone/ntm/internal/prompt"
	"github.com/Dicklesworthstone/ntm/internal/templates"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// SendResult is the JSON output for the send command.
type SendResult struct {
	Success       bool   `json:"success"`
	Session       string `json:"session"`
	PromptPreview string `json:"prompt_preview,omitempty"`
	Targets       []int  `json:"targets"`
	Delivered     int    `json:"delivered"`
	Failed        int    `json:"failed"`
	Error         string `json:"error,omitempty"`
}

// SendTarget represents a send target with optional variant filter.
// Used for --cc:opus style flags where variant filters to specific model/persona.
type SendTarget struct {
	Type    AgentType
	Variant string // Empty = all agents of type; non-empty = filter by variant
}

// SendTargets is a slice of SendTarget that implements pflag.Value for accumulating
type SendTargets []SendTarget

func (s *SendTargets) String() string {
	if s == nil || len(*s) == 0 {
		return ""
	}
	var parts []string
	for _, t := range *s {
		if t.Variant != "" {
			parts = append(parts, fmt.Sprintf("%s:%s", t.Type, t.Variant))
		} else {
			parts = append(parts, string(t.Type))
		}
	}
	return strings.Join(parts, ",")
}

func (s *SendTargets) Set(value string) error {
	// Parse value as optional variant: "cc" or "cc:opus"
	parts := strings.SplitN(value, ":", 2)
	target := SendTarget{}
	if len(parts) > 1 && parts[1] != "" {
		target.Variant = parts[1]
	}
	// Type is set by the flag registration, value is just the variant
	*s = append(*s, target)
	return nil
}

func (s *SendTargets) Type() string {
	return "[variant]"
}

// sendTargetValue wraps SendTargets with a specific agent type for flag parsing
type sendTargetValue struct {
	agentType AgentType
	targets   *SendTargets
}

func newSendTargetValue(agentType AgentType, targets *SendTargets) *sendTargetValue {
	return &sendTargetValue{
		agentType: agentType,
		targets:   targets,
	}
}

func (v *sendTargetValue) String() string {
	return v.targets.String()
}

func (v *sendTargetValue) Set(value string) error {
	// Value is the variant (after the colon), or empty for all
	target := SendTarget{
		Type:    v.agentType,
		Variant: value,
	}
	*v.targets = append(*v.targets, target)
	return nil
}

func (v *sendTargetValue) Type() string {
	return "[variant]"
}

// IsBoolFlag allows the flag to work with or without a value
// --cc sends to all Claude, --cc=opus sends to Claude with opus variant
func (v *sendTargetValue) IsBoolFlag() bool {
	return true
}

// HasTargetsForType checks if any targets match the given agent type
func (s SendTargets) HasTargetsForType(t AgentType) bool {
	for _, target := range s {
		if target.Type == t {
			return true
		}
	}
	return false
}

// MatchesPane checks if any target matches the given pane
func (s SendTargets) MatchesPane(pane tmux.Pane) bool {
	for _, target := range s {
		if matchesSendTarget(pane, target) {
			return true
		}
	}
	return false
}

// matchesSendTarget checks if a pane matches a send target
func matchesSendTarget(pane tmux.Pane, target SendTarget) bool {
	// Type must match
	if string(pane.Type) != string(target.Type) {
		return false
	}
	// If variant is specified, it must also match
	if target.Variant != "" && pane.Variant != target.Variant {
		return false
	}
	return true
}

func newSendCmd() *cobra.Command {
	var targets SendTargets
	var targetAll, skipFirst bool
	var paneIndex int
	var promptFile, prefix, suffix string
	var contextFiles []string
	var templateName string
	var templateVars []string

	cmd := &cobra.Command{
		Use:   "send <session> [prompt]",
		Short: "Send a prompt to agent panes",
		Long: `Send a prompt or command to agent panes in a session.

By default, sends to all agent panes. Use flags to target specific types.
Use --cc=variant to filter by model or persona (e.g., --cc=opus, --cc=architect).

Prompt can be provided as:
  - Command line argument (traditional)
  - From a file using --file
  - From stdin when piped/redirected
  - From a template using --template

Template Usage:
Use --template (-t) to use a named prompt template with variable substitution.
Templates support {{variable}} placeholders and {{#var}}...{{/var}} conditionals.
See 'ntm template list' for available templates.

File Context Injection:
Use --context (-c) to include file contents in the prompt. Files are prepended
with headers and code fences. Supports line ranges: path:10-50, path:10-, path:-50

When using --file or stdin, use --prefix and --suffix to wrap the content.

Examples:
  ntm send myproject "fix the linting errors"           # All agents
  ntm send myproject --cc "review the changes"          # All Claude agents
  ntm send myproject --cc=opus "review the changes"     # Only Claude Opus agents
  ntm send myproject --cc=opus --cc=sonnet "review"     # Opus and Sonnet
  ntm send myproject --cod --gmi "run the tests"        # Codex and Gemini
  ntm send myproject --all "git status"                 # All panes
  ntm send myproject --pane=2 "specific pane"           # Specific pane
  ntm send myproject --skip-first "restart"             # Skip user pane
  ntm send myproject --json "run tests"                 # JSON output
  ntm send myproject --file prompts/review.md           # From file
  cat error.log | ntm send myproject --cc               # From stdin
  git diff | ntm send myproject --all --prefix "Review these changes:"  # Stdin with prefix
  ntm send myproject -c src/auth.py "Refactor this"     # With file context
  ntm send myproject -c src/api.go:10-50 "Review lines" # With line range
  ntm send myproject -c a.go -c b.go "Compare these"    # Multiple files
  ntm send myproject -t code_review --file src/main.go  # Template with file
  ntm send myproject -t fix --var issue="null pointer" --file src/app.go  # Template with vars`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := args[0]

			// Handle template-based prompts
			if templateName != "" {
				return runSendWithTemplate(session, templateName, templateVars, promptFile, contextFiles, targets, targetAll, skipFirst, paneIndex)
			}

			promptText, err := getPromptContent(args[1:], promptFile, prefix, suffix)
			if err != nil {
				return err
			}

			// Inject file context if specified
			if len(contextFiles) > 0 {
				var specs []prompt.FileSpec
				for _, cf := range contextFiles {
					spec, err := prompt.ParseFileSpec(cf)
					if err != nil {
						return fmt.Errorf("invalid --context spec '%s': %w", cf, err)
					}
					specs = append(specs, spec)
				}

				promptText, err = prompt.InjectFiles(specs, promptText)
				if err != nil {
					return err
				}
			}

			return runSendWithTargets(session, promptText, targets, targetAll, skipFirst, paneIndex)
		},
	}

	// Use custom flag values that support --cc or --cc=variant syntax
	cmd.Flags().Var(newSendTargetValue(AgentTypeClaude, &targets), "cc", "send to Claude agents (optional :variant filter)")
	cmd.Flags().Var(newSendTargetValue(AgentTypeCodex, &targets), "cod", "send to Codex agents (optional :variant filter)")
	cmd.Flags().Var(newSendTargetValue(AgentTypeGemini, &targets), "gmi", "send to Gemini agents (optional :variant filter)")
	cmd.Flags().BoolVar(&targetAll, "all", false, "send to all panes (including user pane)")
	cmd.Flags().BoolVarP(&skipFirst, "skip-first", "s", false, "skip the first (user) pane")
	cmd.Flags().IntVarP(&paneIndex, "pane", "p", -1, "send to specific pane index")
	cmd.Flags().StringVarP(&promptFile, "file", "f", "", "read prompt from file (also used as {{file}} in templates)")
	cmd.Flags().StringVar(&prefix, "prefix", "", "text to prepend to file/stdin content")
	cmd.Flags().StringVar(&suffix, "suffix", "", "text to append to file/stdin content")
	cmd.Flags().StringArrayVarP(&contextFiles, "context", "c", nil, "file to include as context (repeatable, supports path:start-end)")
	cmd.Flags().StringVarP(&templateName, "template", "t", "", "use a named prompt template (see 'ntm template list')")
	cmd.Flags().StringArrayVar(&templateVars, "var", nil, "template variable in key=value format (repeatable)")

	return cmd
}

// getPromptContent resolves the prompt content from various sources:
// 1. If --file is specified, read from that file
// 2. If stdin has data (piped/redirected), read from stdin
// 3. Otherwise, use positional arguments
// The prefix and suffix are applied when reading from file or stdin.
func getPromptContent(args []string, promptFile, prefix, suffix string) (string, error) {
	var content string

	// Priority 1: Read from file if specified
	if promptFile != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return "", fmt.Errorf("reading prompt file: %w", err)
		}
		content = string(data)
		if strings.TrimSpace(content) == "" {
			return "", errors.New("prompt file is empty")
		}
		// Apply prefix/suffix for file content
		return buildPrompt(content, prefix, suffix), nil
	}

	// Priority 2: Read from stdin if piped/redirected AND we have no args
	// (If args are provided, they take priority over stdin)
	if len(args) == 0 && stdinHasData() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading from stdin: %w", err)
		}
		content = string(data)
		// Allow empty stdin if we have a prefix (e.g., just sending a command)
		if strings.TrimSpace(content) == "" && prefix == "" {
			return "", errors.New("stdin is empty and no prefix provided")
		}
		// Apply prefix/suffix for stdin content
		return buildPrompt(content, prefix, suffix), nil
	}

	// Priority 3: Use positional arguments
	if len(args) == 0 {
		return "", errors.New("no prompt provided (use argument, --file, or pipe to stdin)")
	}
	content = strings.Join(args, " ")
	// For positional args, prefix/suffix are ignored (they're for file/stdin)
	return content, nil
}

// stdinHasData checks if stdin has data available (is piped/redirected)
func stdinHasData() bool {
	// Check if stdin is a terminal - if it is, there's no piped data
	if isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return false
	}
	// Check if stdin has actual data using Stat
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check if it's a named pipe (FIFO) or has data waiting
	// ModeCharDevice is 0 when stdin is redirected/piped
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// buildPrompt combines prefix, content, and suffix into a single prompt string.
func buildPrompt(content, prefix, suffix string) string {
	var parts []string
	if prefix != "" {
		parts = append(parts, prefix)
	}
	parts = append(parts, strings.TrimSpace(content))
	if suffix != "" {
		parts = append(parts, suffix)
	}
	return strings.Join(parts, "\n")
}

// runSendWithTemplate handles template-based prompt generation and sending.
func runSendWithTemplate(session, templateName string, templateVars []string, promptFile string, contextFiles []string, targets SendTargets, targetAll, skipFirst bool, paneIndex int) error {
	// Load the template
	loader := templates.NewLoader()
	tmpl, err := loader.Load(templateName)
	if err != nil {
		return fmt.Errorf("loading template '%s': %w", templateName, err)
	}

	// Parse template variables from --var flags
	vars := make(map[string]string)
	for _, v := range templateVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --var format '%s' (expected key=value)", v)
		}
		vars[parts[0]] = parts[1]
	}

	// Build execution context
	ctx := templates.ExecutionContext{
		Variables: vars,
		Session:   session,
	}

	// Read file content if --file specified (used as {{file}} variable)
	if promptFile != "" {
		content, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("reading file '%s': %w", promptFile, err)
		}
		ctx.FileContent = string(content)
	}

	// Execute the template
	promptText, err := tmpl.Execute(ctx)
	if err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	// Inject additional file context if specified (via --context)
	if len(contextFiles) > 0 {
		var specs []prompt.FileSpec
		for _, cf := range contextFiles {
			spec, err := prompt.ParseFileSpec(cf)
			if err != nil {
				return fmt.Errorf("invalid --context spec '%s': %w", cf, err)
			}
			specs = append(specs, spec)
		}

		promptText, err = prompt.InjectFiles(specs, promptText)
		if err != nil {
			return err
		}
	}

	return runSendWithTargets(session, promptText, targets, targetAll, skipFirst, paneIndex)
}

// runSendWithTargets sends prompts using the new SendTargets filtering
func runSendWithTargets(session, prompt string, targets SendTargets, targetAll, skipFirst bool, paneIndex int) error {
	// Convert to the old signature for backwards compatibility
	targetCC := targets.HasTargetsForType(AgentTypeClaude)
	targetCod := targets.HasTargetsForType(AgentTypeCodex)
	targetGmi := targets.HasTargetsForType(AgentTypeGemini)

	return runSendInternal(session, prompt, targets, targetCC, targetCod, targetGmi, targetAll, skipFirst, paneIndex)
}

func runSendInternal(session, prompt string, targets SendTargets, targetCC, targetCod, targetGmi, targetAll, skipFirst bool, paneIndex int) error {
	// Helper for JSON error output
	outputError := func(err error) error {
		if jsonOutput {
			result := SendResult{
				Success: false,
				Session: session,
				Error:   err.Error(),
			}
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		return err
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return outputError(err)
	}

	if !tmux.SessionExists(session) {
		return outputError(fmt.Errorf("session '%s' not found", session))
	}

	// Initialize hook executor
	hookExec, err := hooks.NewExecutorFromConfig()
	if err != nil {
		// Log warning but continue - hooks are optional
		if !jsonOutput {
			fmt.Printf("⚠ Could not load hooks config: %v\n", err)
		}
		hookExec = hooks.NewExecutor(nil) // Use empty config
	}

	// Build target description for hook environment
	targetDesc := buildTargetDescription(targetCC, targetCod, targetGmi, targetAll, skipFirst, paneIndex)

	// Build execution context for hooks
	hookCtx := hooks.ExecutionContext{
		SessionName: session,
		ProjectDir:  getSessionWorkingDir(session),
		Message:     prompt,
		AdditionalEnv: map[string]string{
			"NTM_SEND_TARGETS": targetDesc,
			"NTM_TARGET_CC":    boolToStr(targetCC),
			"NTM_TARGET_COD":   boolToStr(targetCod),
			"NTM_TARGET_GMI":   boolToStr(targetGmi),
			"NTM_TARGET_ALL":   boolToStr(targetAll),
			"NTM_PANE_INDEX":   fmt.Sprintf("%d", paneIndex),
		},
	}

	// Run pre-send hooks
	if hookExec.HasHooksForEvent(hooks.EventPreSend) {
		if !jsonOutput {
			fmt.Println("Running pre-send hooks...")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		results, err := hookExec.RunHooksForEvent(ctx, hooks.EventPreSend, hookCtx)
		cancel()
		if err != nil {
			return outputError(fmt.Errorf("pre-send hook failed: %w", err))
		}
		if hooks.AnyFailed(results) {
			return outputError(fmt.Errorf("pre-send hook failed: %w", hooks.AllErrors(results)))
		}
		if !jsonOutput {
			success, _, _ := hooks.CountResults(results)
			fmt.Printf("✓ %d pre-send hook(s) completed\n", success)
		}
	}

	// Auto-checkpoint before broadcast sends
	isBroadcast := targetAll || (!targetCC && !targetCod && !targetGmi && paneIndex < 0)
	if isBroadcast && cfg != nil && cfg.Checkpoints.Enabled && cfg.Checkpoints.BeforeBroadcast {
		if !jsonOutput {
			fmt.Println("Creating auto-checkpoint before broadcast...")
		}
		autoCP := checkpoint.NewAutoCheckpointer()
		cp, err := autoCP.Create(checkpoint.AutoCheckpointOptions{
			SessionName:     session,
			Reason:          checkpoint.ReasonBroadcast,
			Description:     fmt.Sprintf("before sending to %s", targetDesc),
			ScrollbackLines: cfg.Checkpoints.ScrollbackLines,
			IncludeGit:      cfg.Checkpoints.IncludeGit,
			MaxCheckpoints:  cfg.Checkpoints.MaxAutoCheckpoints,
		})
		if err != nil {
			// Log warning but continue - auto-checkpoint is best-effort
			if !jsonOutput {
				fmt.Printf("⚠ Auto-checkpoint failed: %v\n", err)
			}
		} else if !jsonOutput {
			fmt.Printf("✓ Auto-checkpoint created: %s\n", cp.ID)
		}
	}

	panes, err := tmux.GetPanes(session)
	if err != nil {
		return outputError(err)
	}

	if len(panes) == 0 {
		return outputError(fmt.Errorf("no panes found in session '%s'", session))
	}

	// Track results for JSON output
	var targetPanes []int
	delivered := 0
	failed := 0

	// If specific pane requested
	if paneIndex >= 0 {
		for _, p := range panes {
			if p.Index == paneIndex {
				targetPanes = append(targetPanes, paneIndex)
				if err := tmux.SendKeys(p.ID, prompt, true); err != nil {
					failed++
					if jsonOutput {
						result := SendResult{
							Success:       false,
							Session:       session,
							PromptPreview: truncatePrompt(prompt, 50),
							Targets:       targetPanes,
							Delivered:     delivered,
							Failed:        failed,
							Error:         err.Error(),
						}
						return json.NewEncoder(os.Stdout).Encode(result)
					}
					return err
				}
				delivered++

				if jsonOutput {
					result := SendResult{
						Success:       true,
						Session:       session,
						PromptPreview: truncatePrompt(prompt, 50),
						Targets:       targetPanes,
						Delivered:     delivered,
						Failed:        failed,
					}
					return json.NewEncoder(os.Stdout).Encode(result)
				}
				fmt.Printf("Sent to pane %d\n", paneIndex)
				return nil
			}
		}
		return outputError(fmt.Errorf("pane %d not found", paneIndex))
	}

	// Determine which panes to target
	noFilter := !targetCC && !targetCod && !targetGmi && !targetAll
	hasVariantFilter := len(targets) > 0
	if noFilter {
		// Default: send to all agent panes (skip user panes)
		skipFirst = true
	}

	for i, p := range panes {
		// Skip first pane if requested
		if skipFirst && i == 0 {
			continue
		}

		// Apply type and variant filters
		if !targetAll && !noFilter {
			// Use variant-aware matching if we have targets with potential variants
			if hasVariantFilter {
				if !targets.MatchesPane(p) {
					continue
				}
			} else {
				// Fallback to simple type matching (shouldn't happen with new flags)
				match := false
				if targetCC && p.Type == tmux.AgentClaude {
					match = true
				}
				if targetCod && p.Type == tmux.AgentCodex {
					match = true
				}
				if targetGmi && p.Type == tmux.AgentGemini {
					match = true
				}
				if !match {
					continue
				}
			}
		} else if noFilter {
			// Default mode: skip non-agent panes
			if p.Type == tmux.AgentUser {
				continue
			}
		}

		targetPanes = append(targetPanes, p.Index)
		if err := tmux.SendKeys(p.ID, prompt, true); err != nil {
			failed++
			if !jsonOutput {
				return fmt.Errorf("sending to pane %d: %w", p.Index, err)
			}
		} else {
			delivered++
		}
	}

	// Update hook context with delivery results
	hookCtx.AdditionalEnv["NTM_DELIVERED_COUNT"] = fmt.Sprintf("%d", delivered)
	hookCtx.AdditionalEnv["NTM_FAILED_COUNT"] = fmt.Sprintf("%d", failed)
	hookCtx.AdditionalEnv["NTM_TARGET_PANES"] = fmt.Sprintf("%v", targetPanes)

	// Run post-send hooks
	if hookExec.HasHooksForEvent(hooks.EventPostSend) {
		if !jsonOutput {
			fmt.Println("Running post-send hooks...")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		results, postErr := hookExec.RunHooksForEvent(ctx, hooks.EventPostSend, hookCtx)
		cancel()
		if postErr != nil {
			// Log error but don't fail (send already succeeded)
			if !jsonOutput {
				fmt.Printf("⚠ Post-send hook error: %v\n", postErr)
			}
		} else if hooks.AnyFailed(results) {
			// Log failures but don't fail (send already succeeded)
			if !jsonOutput {
				fmt.Printf("⚠ Post-send hook failed: %v\n", hooks.AllErrors(results))
			}
		} else if !jsonOutput {
			success, _, _ := hooks.CountResults(results)
			fmt.Printf("✓ %d post-send hook(s) completed\n", success)
		}
	}

	// Emit prompt_send event
	if delivered > 0 {
		events.EmitPromptSend(session, delivered, len(prompt), "", buildTargetDescription(targetCC, targetCod, targetGmi, targetAll, skipFirst, paneIndex), len(hookCtx.AdditionalEnv) > 0)
	}

	// JSON output mode
	if jsonOutput {
		result := SendResult{
			Success:       failed == 0,
			Session:       session,
			PromptPreview: truncatePrompt(prompt, 50),
			Targets:       targetPanes,
			Delivered:     delivered,
			Failed:        failed,
		}
		if failed > 0 {
			result.Error = fmt.Sprintf("%d pane(s) failed", failed)
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}

	if len(targetPanes) == 0 {
		fmt.Println("No matching panes found")
	} else {
		fmt.Printf("Sent to %d pane(s)\n", delivered)
	}

	return nil
}

func newInterruptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "interrupt <session>",
		Short: "Send Ctrl+C to all agent panes",
		Long: `Send an interrupt signal (Ctrl+C) to all agent panes in a session.
User panes are not affected.

Examples:
  ntm interrupt myproject`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInterrupt(args[0])
		},
	}
}

func runInterrupt(session string) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	panes, err := tmux.GetPanes(session)
	if err != nil {
		return err
	}

	count := 0
	for _, p := range panes {
		// Only interrupt agent panes
		if p.Type == tmux.AgentClaude || p.Type == tmux.AgentCodex || p.Type == tmux.AgentGemini {
			if err := tmux.SendInterrupt(p.ID); err != nil {
				return fmt.Errorf("interrupting pane %d: %w", p.Index, err)
			}
			count++
		}
	}

	fmt.Printf("Sent Ctrl+C to %d agent pane(s)\n", count)
	return nil
}

func newKillCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "kill <session>",
		Short: "Kill a tmux session",
		Long: `Kill a tmux session and all its panes.

Examples:
  ntm kill myproject           # Prompts for confirmation
  ntm kill myproject --force   # No confirmation`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKill(args[0], force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")

	return cmd
}

func runKill(session string, force bool) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	if !force {
		panes, err := tmux.GetPanes(session)
		if err != nil {
			return err
		}

		if !confirm(fmt.Sprintf("Kill session '%s' with %d pane(s)?", session, len(panes))) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := tmux.KillSession(session); err != nil {
		return err
	}

	fmt.Printf("Killed session '%s'\n", session)
	return nil
}

// truncatePrompt truncates a prompt to the specified length for display
func truncatePrompt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// buildTargetDescription creates a human-readable description of send targets
func buildTargetDescription(targetCC, targetCod, targetGmi, targetAll, skipFirst bool, paneIndex int) string {
	if paneIndex >= 0 {
		return fmt.Sprintf("pane:%d", paneIndex)
	}
	if targetAll {
		return "all"
	}

	var targets []string
	if targetCC {
		targets = append(targets, "cc")
	}
	if targetCod {
		targets = append(targets, "cod")
	}
	if targetGmi {
		targets = append(targets, "gmi")
	}

	if len(targets) == 0 {
		if skipFirst {
			return "agents"
		}
		return "all-agents"
	}
	return strings.Join(targets, ",")
}

// getSessionWorkingDir returns the working directory for a session
func getSessionWorkingDir(session string) string {
	if cfg != nil {
		return cfg.GetProjectDir(session)
	}
	return ""
}

// boolToStr converts a boolean to "true" or "false" string
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
