package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/policy"
)

func newSafetyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "safety",
		Short: "Manage destructive command protection",
		Long: `Manage NTM's destructive command protection system.

The safety system blocks or warns about dangerous commands like:
  - git reset --hard (loses uncommitted changes)
  - git push --force (overwrites remote history)
  - rm -rf / (catastrophic deletion)

Use 'ntm safety status' to see current protection status.
Use 'ntm safety blocked' to view blocked command history.
Use 'ntm safety check <command>' to test a command against the policy.`,
	}

	cmd.AddCommand(
		newSafetyStatusCmd(),
		newSafetyBlockedCmd(),
		newSafetyCheckCmd(),
		newSafetyInstallCmd(),
		newSafetyUninstallCmd(),
	)

	return cmd
}

func newSafetyStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show safety system status",
		RunE:  runSafetyStatus,
	}
}

// SafetyStatusResponse is the JSON output for safety status.
type SafetyStatusResponse struct {
	output.TimestampedResponse
	Installed     bool   `json:"installed"`
	PolicyPath    string `json:"policy_path,omitempty"`
	BlockedCount  int    `json:"blocked_rules"`
	ApprovalCount int    `json:"approval_rules"`
	AllowedCount  int    `json:"allowed_rules"`
	WrapperPath   string `json:"wrapper_path,omitempty"`
	HookInstalled bool   `json:"hook_installed"`
}

func runSafetyStatus(cmd *cobra.Command, args []string) error {
	home, _ := os.UserHomeDir()
	ntmDir := filepath.Join(home, ".ntm")
	wrapperDir := filepath.Join(ntmDir, "bin")

	// Check if wrappers are installed
	gitWrapper := filepath.Join(wrapperDir, "git")
	wrapperInstalled := fileExists(gitWrapper)

	// Check if Claude Code hook is installed
	hookPath := filepath.Join(home, ".claude", "hooks", "PreToolUse", "ntm-safety.sh")
	hookInstalled := fileExists(hookPath)

	// Load policy
	p, err := policy.LoadOrDefault()
	var blocked, approval, allowed int
	var policyPath string
	if err == nil {
		blocked, approval, allowed = p.Stats()
		// Check if custom policy exists
		customPath := filepath.Join(ntmDir, "policy.yaml")
		if fileExists(customPath) {
			policyPath = customPath
		}
	}

	if IsJSONOutput() {
		resp := SafetyStatusResponse{
			TimestampedResponse: output.NewTimestamped(),
			Installed:           wrapperInstalled || hookInstalled,
			PolicyPath:          policyPath,
			BlockedCount:        blocked,
			ApprovalCount:       approval,
			AllowedCount:        allowed,
			WrapperPath:         wrapperDir,
			HookInstalled:       hookInstalled,
		}
		return output.PrintJSON(resp)
	}

	// TUI output
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println()
	fmt.Println(titleStyle.Render("NTM Safety Status"))
	fmt.Println()

	// Wrapper status
	if wrapperInstalled {
		fmt.Printf("  %s PATH wrappers installed (%s)\n", okStyle.Render("✓"), wrapperDir)
	} else {
		fmt.Printf("  %s PATH wrappers not installed\n", warnStyle.Render("○"))
	}

	// Hook status
	if hookInstalled {
		fmt.Printf("  %s Claude Code hook installed\n", okStyle.Render("✓"))
	} else {
		fmt.Printf("  %s Claude Code hook not installed\n", warnStyle.Render("○"))
	}

	// Policy status
	fmt.Println()
	fmt.Println("  Policy rules:")
	fmt.Printf("    %s blocked patterns\n", mutedStyle.Render(fmt.Sprintf("%d", blocked)))
	fmt.Printf("    %s approval-required patterns\n", mutedStyle.Render(fmt.Sprintf("%d", approval)))
	fmt.Printf("    %s explicitly allowed patterns\n", mutedStyle.Render(fmt.Sprintf("%d", allowed)))

	if policyPath != "" {
		fmt.Printf("    %s\n", mutedStyle.Render("Custom: "+policyPath))
	} else {
		fmt.Printf("    %s\n", mutedStyle.Render("Using default policy"))
	}

	// Installation hint
	if !wrapperInstalled && !hookInstalled {
		fmt.Println()
		fmt.Printf("  %s\n", mutedStyle.Render("Run 'ntm safety install' to enable protection"))
	}

	fmt.Println()
	return nil
}

func newSafetyBlockedCmd() *cobra.Command {
	var hours int
	var limit int

	cmd := &cobra.Command{
		Use:   "blocked",
		Short: "Show blocked command history",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSafetyBlocked(hours, limit)
		},
	}

	cmd.Flags().IntVar(&hours, "hours", 24, "Show blocked commands from last N hours")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum entries to show")

	return cmd
}

// BlockedResponse is the JSON output for blocked commands.
type BlockedResponse struct {
	output.TimestampedResponse
	Entries []policy.BlockedEntry `json:"entries"`
	Count   int                   `json:"count"`
}

func runSafetyBlocked(hours, limit int) error {
	entries, err := policy.RecentBlocked("", hours)
	if err != nil {
		return fmt.Errorf("reading blocked log: %w", err)
	}

	// Limit entries
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	if IsJSONOutput() {
		return output.PrintJSON(BlockedResponse{
			TimestampedResponse: output.NewTimestamped(),
			Entries:             entries,
			Count:               len(entries),
		})
	}

	// TUI output
	if len(entries) == 0 {
		fmt.Println("No blocked commands in the last", hours, "hours")
		return nil
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println()
	fmt.Println(titleStyle.Render(fmt.Sprintf("Blocked Commands (last %d hours)", hours)))
	fmt.Println()

	for _, e := range entries {
		ts := e.Timestamp.Format("01/02 15:04")
		fmt.Printf("  %s %s\n", mutedStyle.Render(ts), errorStyle.Render(e.Command))
		if e.Reason != "" {
			fmt.Printf("           %s\n", mutedStyle.Render(e.Reason))
		}
		if e.Session != "" {
			fmt.Printf("           %s\n", mutedStyle.Render("Session: "+e.Session))
		}
	}

	fmt.Println()
	return nil
}

func newSafetyCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <command>",
		Short: "Check if a command would be blocked",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			command := strings.Join(args, " ")
			return runSafetyCheck(command)
		},
	}
}

// CheckResponse is the JSON output for safety check.
type CheckResponse struct {
	output.TimestampedResponse
	Command string `json:"command"`
	Action  string `json:"action"` // allow, block, approve
	Pattern string `json:"pattern,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

func runSafetyCheck(command string) error {
	p, err := policy.LoadOrDefault()
	if err != nil {
		return fmt.Errorf("loading policy: %w", err)
	}

	match := p.Check(command)

	if IsJSONOutput() {
		resp := CheckResponse{
			TimestampedResponse: output.NewTimestamped(),
			Command:             command,
			Action:              "allow",
		}
		if match != nil {
			resp.Action = string(match.Action)
			resp.Pattern = match.Pattern
			resp.Reason = match.Reason
		}
		return output.PrintJSON(resp)
	}

	// TUI output
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fmt.Println()
	fmt.Printf("  Command: %s\n", command)
	fmt.Println()

	if match == nil {
		fmt.Printf("  %s Allowed (no policy match)\n", okStyle.Render("✓"))
	} else {
		switch match.Action {
		case policy.ActionAllow:
			fmt.Printf("  %s Explicitly allowed\n", okStyle.Render("✓"))
		case policy.ActionBlock:
			fmt.Printf("  %s BLOCKED\n", errorStyle.Render("✗"))
		case policy.ActionApprove:
			fmt.Printf("  %s Requires approval\n", warnStyle.Render("⚠"))
		}
		if match.Reason != "" {
			fmt.Printf("    %s\n", mutedStyle.Render(match.Reason))
		}
		fmt.Printf("    %s\n", mutedStyle.Render("Pattern: "+match.Pattern))
	}

	fmt.Println()

	if match != nil && match.Action == policy.ActionBlock {
		os.Exit(1)
	}

	return nil
}

func newSafetyInstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install safety wrappers and hooks",
		Long: `Install PATH wrappers and Claude Code hooks for destructive command protection.

This installs:
  1. Shell wrappers in ~/.ntm/bin/ that intercept git and rm commands
  2. A Claude Code PreToolUse hook that validates Bash commands

After installation, add ~/.ntm/bin to your PATH (before /usr/bin) for wrapper protection.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSafetyInstall(force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files")

	return cmd
}

func runSafetyInstall(force bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	ntmDir := filepath.Join(home, ".ntm")
	binDir := filepath.Join(ntmDir, "bin")
	logsDir := filepath.Join(ntmDir, "logs")

	// Create directories
	for _, dir := range []string{ntmDir, binDir, logsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Install git wrapper
	gitWrapper := filepath.Join(binDir, "git")
	if err := installWrapper(gitWrapper, gitWrapperScript, force); err != nil {
		return err
	}

	// Install rm wrapper
	rmWrapper := filepath.Join(binDir, "rm")
	if err := installWrapper(rmWrapper, rmWrapperScript, force); err != nil {
		return err
	}

	// Install Claude Code hook
	hookDir := filepath.Join(home, ".claude", "hooks", "PreToolUse")
	if err := os.MkdirAll(hookDir, 0755); err != nil {
		return fmt.Errorf("creating hook directory: %w", err)
	}

	hookPath := filepath.Join(hookDir, "ntm-safety.sh")
	if err := installWrapper(hookPath, claudeHookScript, force); err != nil {
		return err
	}

	// Create default policy file if it doesn't exist
	policyPath := filepath.Join(ntmDir, "policy.yaml")
	if !fileExists(policyPath) || force {
		if err := writeDefaultPolicy(policyPath); err != nil {
			return err
		}
	}

	if !IsJSONOutput() {
		okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
		mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

		fmt.Println()
		fmt.Printf("  %s Installed git wrapper: %s\n", okStyle.Render("✓"), gitWrapper)
		fmt.Printf("  %s Installed rm wrapper: %s\n", okStyle.Render("✓"), rmWrapper)
		fmt.Printf("  %s Installed Claude Code hook: %s\n", okStyle.Render("✓"), hookPath)
		fmt.Printf("  %s Created policy file: %s\n", okStyle.Render("✓"), policyPath)
		fmt.Println()
		fmt.Printf("  %s\n", mutedStyle.Render("Add to your shell profile:"))
		fmt.Printf("    %s\n", "export PATH=\"$HOME/.ntm/bin:$PATH\"")
		fmt.Println()
	} else {
		return output.PrintJSON(map[string]interface{}{
			"success":     true,
			"timestamp":   time.Now(),
			"git_wrapper": gitWrapper,
			"rm_wrapper":  rmWrapper,
			"hook":        hookPath,
			"policy":      policyPath,
		})
	}

	return nil
}

func newSafetyUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove safety wrappers and hooks",
		RunE:  runSafetyUninstall,
	}
}

func runSafetyUninstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("getting home directory: %w", err)
	}

	var removed []string

	// Remove wrappers
	binDir := filepath.Join(home, ".ntm", "bin")
	for _, name := range []string{"git", "rm"} {
		path := filepath.Join(binDir, name)
		if fileExists(path) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			removed = append(removed, path)
		}
	}

	// Remove hook
	hookPath := filepath.Join(home, ".claude", "hooks", "PreToolUse", "ntm-safety.sh")
	if fileExists(hookPath) {
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("removing hook: %w", err)
		}
		removed = append(removed, hookPath)
	}

	if !IsJSONOutput() {
		if len(removed) == 0 {
			fmt.Println("Nothing to uninstall")
		} else {
			okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
			fmt.Println()
			for _, path := range removed {
				fmt.Printf("  %s Removed: %s\n", okStyle.Render("✓"), path)
			}
			fmt.Println()
		}
	} else {
		return output.PrintJSON(map[string]interface{}{
			"success":   true,
			"timestamp": time.Now(),
			"removed":   removed,
		})
	}

	return nil
}

func installWrapper(path, content string, force bool) error {
	if fileExists(path) && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}

	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

func writeDefaultPolicy(path string) error {
	content := `# NTM Safety Policy
# Patterns are regular expressions matched against commands

blocked:
  - pattern: 'git\s+reset\s+--hard'
    reason: "Hard reset loses uncommitted changes"
  - pattern: 'git\s+clean\s+-fd'
    reason: "Removes untracked files permanently"
  - pattern: 'git\s+push\s+.*--force(?!\s*-with-lease)'
    reason: "Force push can overwrite remote history"
  - pattern: 'git\s+push\s+.*-f(?:\s|$)'
    reason: "Force push can overwrite remote history"
  - pattern: 'rm\s+-rf\s+/'
    reason: "Recursive delete of root is catastrophic"
  - pattern: 'rm\s+-rf\s+~'
    reason: "Recursive delete of home directory"
  - pattern: 'rm\s+-rf\s+\*'
    reason: "Recursive delete of everything in current directory"
  - pattern: 'git\s+branch\s+-D'
    reason: "Force delete branch loses unmerged work"
  - pattern: 'git\s+stash\s+drop'
    reason: "Dropping stash loses saved work"
  - pattern: 'git\s+stash\s+clear'
    reason: "Clearing all stashes loses saved work"

approval_required:
  - pattern: 'git\s+rebase\s+-i'
    reason: "Interactive rebase rewrites history"
  - pattern: 'git\s+commit\s+--amend'
    reason: "Amending rewrites history"
  - pattern: 'rm\s+-rf'
    reason: "Recursive force delete"

allowed:
  - pattern: 'git\s+push\s+.*--force-with-lease'
    reason: "Safe force push"
  - pattern: 'git\s+reset\s+--soft'
    reason: "Soft reset preserves changes"
  - pattern: 'git\s+reset\s+HEAD~?\d*\s*$'
    reason: "Mixed reset preserves working directory"
`
	return os.WriteFile(path, []byte(content), 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Wrapper scripts

const gitWrapperScript = `#!/bin/bash
# NTM Safety Wrapper for git
# Intercepts destructive git commands

REAL_GIT=$(which -a git | grep -v "$HOME/.ntm/bin" | head -1)
if [ -z "$REAL_GIT" ]; then
    REAL_GIT="/usr/bin/git"
fi

# Check command against policy
check_result=$(ntm safety check "$@" --json 2>/dev/null)
if [ $? -eq 0 ]; then
    action=$(echo "$check_result" | jq -r '.action' 2>/dev/null)
    if [ "$action" = "block" ]; then
        reason=$(echo "$check_result" | jq -r '.reason' 2>/dev/null)
        echo "NTM Safety: Command blocked" >&2
        echo "  Reason: $reason" >&2
        echo "  Command: git $*" >&2

        # Log the blocked command
        mkdir -p "$HOME/.ntm/logs"
        echo "{\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"command\":\"git $*\",\"reason\":\"$reason\",\"action\":\"block\"}" >> "$HOME/.ntm/logs/blocked.jsonl"

        exit 1
    fi
fi

# Pass through to real git
exec "$REAL_GIT" "$@"
`

const rmWrapperScript = `#!/bin/bash
# NTM Safety Wrapper for rm
# Intercepts destructive rm commands

REAL_RM=$(which -a rm | grep -v "$HOME/.ntm/bin" | head -1)
if [ -z "$REAL_RM" ]; then
    REAL_RM="/bin/rm"
fi

# Check command against policy
check_result=$(ntm safety check "rm $*" --json 2>/dev/null)
if [ $? -eq 0 ]; then
    action=$(echo "$check_result" | jq -r '.action' 2>/dev/null)
    if [ "$action" = "block" ]; then
        reason=$(echo "$check_result" | jq -r '.reason' 2>/dev/null)
        echo "NTM Safety: Command blocked" >&2
        echo "  Reason: $reason" >&2
        echo "  Command: rm $*" >&2

        # Log the blocked command
        mkdir -p "$HOME/.ntm/logs"
        echo "{\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"command\":\"rm $*\",\"reason\":\"$reason\",\"action\":\"block\"}" >> "$HOME/.ntm/logs/blocked.jsonl"

        exit 1
    fi
fi

# Pass through to real rm
exec "$REAL_RM" "$@"
`

const claudeHookScript = `#!/bin/bash
# NTM Safety Hook for Claude Code
# PreToolUse hook that validates Bash commands

# Only process Bash tool calls
TOOL_NAME="${CLAUDE_TOOL_NAME:-}"
if [ "$TOOL_NAME" != "Bash" ]; then
    exit 0
fi

# Get the command from the tool input
COMMAND="${CLAUDE_TOOL_INPUT_command:-}"
if [ -z "$COMMAND" ]; then
    exit 0
fi

# Check against policy
check_result=$(ntm safety check "$COMMAND" --json 2>/dev/null)
if [ $? -eq 0 ]; then
    action=$(echo "$check_result" | jq -r '.action' 2>/dev/null)
    if [ "$action" = "block" ]; then
        reason=$(echo "$check_result" | jq -r '.reason' 2>/dev/null)

        # Log the blocked command
        mkdir -p "$HOME/.ntm/logs"
        session="${NTM_SESSION:-unknown}"
        agent="${CLAUDE_AGENT_TYPE:-claude}"
        echo "{\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"session\":\"$session\",\"agent\":\"$agent\",\"command\":\"$COMMAND\",\"reason\":\"$reason\",\"action\":\"block\"}" >> "$HOME/.ntm/logs/blocked.jsonl"

        # Return error to Claude Code
        echo "BLOCKED: $reason"
        exit 1
    fi
fi

exit 0
`
