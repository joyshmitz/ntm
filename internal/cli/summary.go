package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/robot"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/util"
)

func newSummaryCmd() *cobra.Command {
	var (
		since  string
		format string
	)

	cmd := &cobra.Command{
		Use:   "summary [session]",
		Short: "Show activity summary for agents in a session",
		Long: `Display a summary of what each agent accomplished in a session.

Shows per-agent:
  - Active time and output volume
  - Files modified
  - Key actions (created, fixed, added, etc.)
  - Error counts

The summary is useful after parallel agent work to understand
what each agent did and identify potential conflicts.

Examples:
  ntm summary                      # Auto-detect session
  ntm summary myproject            # Specific session
  ntm summary --since 1h           # Look back 1 hour
  ntm summary --json               # Output as JSON`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(args, since, format)
		},
	}

	cmd.Flags().StringVar(&since, "since", "30m", "Duration to look back (e.g., 30m, 1h)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")

	return cmd
}

func runSummary(args []string, sinceStr, format string) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	var session string
	if len(args) > 0 {
		session = args[0]
	}

	res, err := ResolveSession(session, os.Stdout)
	if err != nil {
		return err
	}
	if res.Session == "" {
		return nil
	}
	res.ExplainIfInferred(os.Stderr)
	session = res.Session

	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	since, err := util.ParseDurationWithDefault(sinceStr, 30*time.Minute, "since")
	if err != nil {
		return fmt.Errorf("invalid --since: %w", err)
	}

	// Get panes
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return fmt.Errorf("failed to get panes: %w", err)
	}

	// Build agent activity data
	var agentData []robot.AgentActivityData
	for _, pane := range panes {
		agentType := string(pane.Type)
		if agentType == "" || agentType == "unknown" {
			continue // Skip non-agent panes
		}

		// Capture output
		output, _ := tmux.CapturePaneOutput(pane.ID, 500)

		state := "idle"
		if pane.Active {
			state = "active"
		}

		data := robot.AgentActivityData{
			PaneID:    pane.ID,
			PaneTitle: pane.Title,
			AgentType: agentType,
			Output:    output,
			State:     state,
		}
		agentData = append(agentData, data)
	}

	// Generate summary
	wd, _ := os.Getwd()
	summary, err := robot.SummarizeSession(robot.SessionSummaryOptions{
		Session:   session,
		Since:     since,
		RepoPath:  wd,
		AgentData: agentData,
	})
	if err != nil {
		return err
	}

	// Output
	if IsJSONOutput() || format == "json" {
		resp := robot.NewSessionSummaryResponse(summary)
		return output.PrintJSON(resp)
	}

	// Human-readable output
	fmt.Print(robot.FormatSessionSummaryText(summary))
	return nil
}
