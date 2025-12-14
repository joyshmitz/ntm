package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/health"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [session]",
		Short: "Check health status of agents in a session",
		Long: `Check health status of all agents in a session.

Reports:
  - Process status (running/exited)
  - Activity level (active/idle/stale)
  - Detected issues (rate limits, crashes, errors)

Examples:
  ntm health myproject          # Check health of all agents
  ntm health myproject --json   # Output as JSON`,
		Args: cobra.MaximumNArgs(1),
		RunE: runHealth,
	}

	return cmd
}

func runHealth(cmd *cobra.Command, args []string) error {
	var session string

	if len(args) > 0 {
		session = args[0]
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	res, err := ResolveSession(session, cmd.OutOrStdout())
	if err != nil {
		return err
	}
	if res.Session == "" {
		return nil
	}
	res.ExplainIfInferred(cmd.ErrOrStderr())
	session = res.Session

	// Perform health check
	result, err := health.CheckSession(session)
	if err != nil {
		if _, ok := err.(*health.SessionNotFoundError); ok {
			if jsonOutput {
				return outputHealthJSON(&health.SessionHealth{
					Session: session,
					Summary: health.HealthSummary{},
					Agents:  []health.AgentHealth{},
				}, fmt.Errorf("session '%s' not found", session))
			}
			return fmt.Errorf("session '%s' not found", session)
		}
		return err
	}

	if jsonOutput {
		return outputHealthJSON(result, nil)
	}

	return renderHealthTUI(result)
}

func outputHealthJSON(result *health.SessionHealth, err error) error {
	type jsonOutput struct {
		*health.SessionHealth
		Error string `json:"error,omitempty"`
	}

	output := jsonOutput{SessionHealth: result}
	if err != nil {
		output.Error = err.Error()
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func renderHealthTUI(result *health.SessionHealth) error {
	// Define styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99"))

	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42"))     // Green
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))  // Orange
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // Gray

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	// Status icon helper
	statusIcon := func(s health.Status) string {
		switch s {
		case health.StatusOK:
			return okStyle.Render("✓ OK")
		case health.StatusWarning:
			return warnStyle.Render("⚠ WARN")
		case health.StatusError:
			return errorStyle.Render("✗ ERROR")
		default:
			return mutedStyle.Render("? UNKNOWN")
		}
	}

	// Activity indicator
	activityStr := func(a health.ActivityLevel) string {
		switch a {
		case health.ActivityActive:
			return okStyle.Render("active")
		case health.ActivityIdle:
			return mutedStyle.Render("idle")
		case health.ActivityStale:
			return warnStyle.Render("stale")
		default:
			return mutedStyle.Render("unknown")
		}
	}

	// Build header
	fmt.Println()
	fmt.Printf("%s %s\n", titleStyle.Render("Session:"), result.Session)
	fmt.Println()

	// Build table header
	header := fmt.Sprintf("%-6s │ %-10s │ %-10s │ %-10s │ %s",
		"Pane", "Agent", "Status", "Activity", "Issues")
	fmt.Println(mutedStyle.Render(header))
	fmt.Println(mutedStyle.Render(strings.Repeat("─", 70)))

	// Build table rows
	for _, agent := range result.Agents {
		// Format issues
		issueStr := "-"
		if len(agent.Issues) > 0 {
			var issueStrs []string
			for _, issue := range agent.Issues {
				issueStrs = append(issueStrs, issue.Message)
			}
			issueStr = strings.Join(issueStrs, ", ")
		}

		// Add stale timing if relevant
		if agent.Activity == health.ActivityStale && agent.IdleSeconds > 0 {
			mins := agent.IdleSeconds / 60
			if mins > 0 {
				issueStr = fmt.Sprintf("no output %dm", mins)
			}
		}

		row := fmt.Sprintf("%-6d │ %-10s │ %-10s │ %-10s │ %s",
			agent.Pane,
			agent.AgentType,
			statusIcon(agent.Status),
			activityStr(agent.Activity),
			issueStr)
		fmt.Println(row)
	}

	fmt.Println()

	// Summary box
	summary := fmt.Sprintf("Overall: %d healthy, %d warning, %d error",
		result.Summary.Healthy,
		result.Summary.Warning,
		result.Summary.Error)
	fmt.Println(boxStyle.Render(summary))
	fmt.Println()

	// Exit code reflects overall health
	if result.OverallStatus == health.StatusError {
		os.Exit(2)
	} else if result.OverallStatus == health.StatusWarning {
		os.Exit(1)
	}

	return nil
}
