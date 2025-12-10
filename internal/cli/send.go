package cli

import (
	"fmt"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	var targetCC, targetCod, targetGmi, targetAll, skipFirst bool
	var paneIndex int

	cmd := &cobra.Command{
		Use:   "send <session> <prompt>",
		Short: "Send a prompt to agent panes",
		Long: `Send a prompt or command to agent panes in a session.

By default, sends to all agent panes. Use flags to target specific types.

Examples:
  ntm send myproject "fix the linting errors"           # All agents
  ntm send myproject --cc "review the changes"          # Only Claude
  ntm send myproject --cod --gmi "run the tests"        # Codex and Gemini
  ntm send myproject --all "git status"                 # All panes
  ntm send myproject --pane=2 "specific pane"           # Specific pane
  ntm send myproject --skip-first "restart"             # Skip user pane`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := args[0]
			prompt := strings.Join(args[1:], " ")
			return runSend(session, prompt, targetCC, targetCod, targetGmi, targetAll, skipFirst, paneIndex)
		},
	}

	cmd.Flags().BoolVar(&targetCC, "cc", false, "send to Claude agents only")
	cmd.Flags().BoolVar(&targetCod, "cod", false, "send to Codex agents only")
	cmd.Flags().BoolVar(&targetGmi, "gmi", false, "send to Gemini agents only")
	cmd.Flags().BoolVar(&targetAll, "all", false, "send to all panes (including user pane)")
	cmd.Flags().BoolVarP(&skipFirst, "skip-first", "s", false, "skip the first (user) pane")
	cmd.Flags().IntVarP(&paneIndex, "pane", "p", -1, "send to specific pane index")

	return cmd
}

func runSend(session, prompt string, targetCC, targetCod, targetGmi, targetAll, skipFirst bool, paneIndex int) error {
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

	if len(panes) == 0 {
		return fmt.Errorf("no panes found in session '%s'", session)
	}

	// If specific pane requested
	if paneIndex >= 0 {
		for _, p := range panes {
			if p.Index == paneIndex {
				if err := tmux.SendKeys(p.ID, prompt, true); err != nil {
					return err
				}
				fmt.Printf("Sent to pane %d\n", paneIndex)
				return nil
			}
		}
		return fmt.Errorf("pane %d not found", paneIndex)
	}

	// Determine which panes to target
	noFilter := !targetCC && !targetCod && !targetGmi && !targetAll
	if noFilter {
		// Default: send to all agent panes (skip user panes)
		skipFirst = true
	}

	count := 0
	for i, p := range panes {
		// Skip first pane if requested
		if skipFirst && i == 0 {
			continue
		}

		// Apply type filters
		if !targetAll && !noFilter {
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
		} else if noFilter {
			// Default mode: skip non-agent panes
			if p.Type == tmux.AgentUser {
				continue
			}
		}

		if err := tmux.SendKeys(p.ID, prompt, true); err != nil {
			return fmt.Errorf("sending to pane %d: %w", p.Index, err)
		}
		count++
	}

	if count == 0 {
		fmt.Println("No matching panes found")
	} else {
		fmt.Printf("Sent to %d pane(s)\n", count)
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
