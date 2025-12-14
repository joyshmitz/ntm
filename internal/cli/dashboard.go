package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/dashboard"
)

func newDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "dashboard [session-name]",
		Aliases: []string{"dash", "d"},
		Short:   "Open interactive session dashboard",
		Long: `Open a stunning interactive dashboard for a tmux session.

The dashboard shows:
- Visual grid of all panes with agent types
- Agent counts (Claude, Codex, Gemini)
- Quick actions for zooming and sending commands

If no session is specified:
- Inside tmux: uses the current session
- Outside tmux: shows a session selector

Examples:
  ntm dashboard myproject
  ntm dash                  # Auto-detect session`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var session string
			if len(args) > 0 {
				session = args[0]
			}
			return runDashboard(cmd.OutOrStdout(), cmd.ErrOrStderr(), session)
		},
	}
}

func runDashboard(w io.Writer, errW io.Writer, session string) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	res, err := ResolveSession(session, w)
	if err != nil {
		return err
	}
	if res.Session == "" {
		return nil
	}
	res.ExplainIfInferred(errW)
	session = res.Session

	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	projectDir := ""
	if cfg != nil {
		projectDir = cfg.GetProjectDir(session)
	} else {
		// Fallback to default if config not loaded
		projectDir = config.Default().GetProjectDir(session)
	}

	return dashboard.Run(session, projectDir)
}
