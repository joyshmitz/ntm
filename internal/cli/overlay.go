package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

func newOverlayCmd() *cobra.Command {
	var overlayKey string

	cmd := &cobra.Command{
		Use:   "overlay [session-name]",
		Short: "Open dashboard as a floating overlay above agent panes",
		Long: `Open the NTM dashboard in a tmux popup that floats over your agent panes.

The overlay lets you monitor agents without leaving their terminal output.
Press Escape to dismiss the overlay and interact with panes directly.
Press Enter/z on a pane to dismiss the overlay AND zoom into that pane.

Use 'ntm bind --overlay' to set up F12 as a toggle key.

If no session is specified:
- Inside tmux: uses the current session
- Outside tmux: shows an error (overlay requires tmux)

Examples:
  ntm overlay myproject     # Open dashboard overlay for myproject
  ntm overlay               # Auto-detect session (must be inside tmux)
  ntm bind --overlay        # Set up F12 toggle key`,
		Aliases: []string{"ov", "hud"},
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tmux.InTmux() {
				return fmt.Errorf("overlay requires tmux — run from inside a tmux session")
			}

			var session string
			if len(args) > 0 {
				session = args[0]
			}

			res, err := ResolveSession(session, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if res.Session == "" {
				return nil
			}
			session = res.Session

			if !tmux.SessionExists(session) {
				return fmt.Errorf("session '%s' not found", session)
			}

			return launchOverlayPopup(session, overlayKey)
		},
	}

	cmd.Flags().StringVar(&overlayKey, "bind-key", "", "Also set up this key as a toggle (e.g., F12)")
	cmd.ValidArgsFunction = completeSessionArgs

	return cmd
}

// launchOverlayPopup opens the NTM dashboard inside a tmux display-popup.
func launchOverlayPopup(session, bindKey string) error {
	t := theme.Current()

	// Optionally set up the toggle keybinding
	if bindKey != "" {
		if err := setupOverlayBinding(bindKey); err != nil {
			fmt.Fprintf(os.Stderr, "%s⚠%s Could not set up %s binding: %v\n",
				colorize(t.Warning), colorize(t.Text), bindKey, err)
		}
	}

	// Build the ntm command to run inside the popup.
	// display-popup passes the command to /bin/sh -c, so quote paths
	// to handle spaces. Tmux session names can't contain single quotes.
	ntmBin, err := os.Executable()
	if err != nil {
		ntmBin = "ntm"
	}
	innerCmd := fmt.Sprintf("'%s' dashboard --popup '%s'", ntmBin, session)

	// Launch the popup — this blocks until the popup is dismissed
	tmuxArgs := []string{
		"display-popup",
		"-E",        // close popup when command exits
		"-w", "95%", // 95% of terminal width
		"-h", "95%", // 95% of terminal height
		innerCmd,
	}

	cmd := exec.Command(tmux.BinaryPath(), tmuxArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
