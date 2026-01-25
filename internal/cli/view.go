package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/kernel"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

// SessionViewInput is the kernel input for sessions.view.
type SessionViewInput struct {
	Session string `json:"session"`
}

func init() {
	kernel.MustRegister(kernel.Command{
		Name:        "sessions.view",
		Description: "Apply tiled layout to a session",
		Category:    "sessions",
		Input: &kernel.SchemaRef{
			Name: "SessionViewInput",
			Ref:  "cli.SessionViewInput",
		},
		Output: &kernel.SchemaRef{
			Name: "SuccessResponse",
			Ref:  "output.SuccessResponse",
		},
		REST: &kernel.RESTBinding{
			Method: "POST",
			Path:   "/sessions/{sessionId}/view",
		},
		Examples: []kernel.Example{
			{
				Name:        "view",
				Description: "Apply tiled layout to a session",
				Command:     "ntm view myproject",
			},
		},
		SafetyLevel: kernel.SafetySafe,
		Idempotent:  true,
	})
	kernel.MustRegisterHandler("sessions.view", func(ctx context.Context, input any) (any, error) {
		opts := SessionViewInput{}
		switch value := input.(type) {
		case SessionViewInput:
			opts = value
		case *SessionViewInput:
			if value != nil {
				opts = *value
			}
		}
		if strings.TrimSpace(opts.Session) == "" {
			return nil, fmt.Errorf("session is required")
		}
		return buildViewResponse(opts.Session)
	})
}

func newViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "view [session-name]",
		Aliases: []string{"v", "tile"},
		Short:   "View all panes in a session (unzoom, tile, attach)",
		Long: `View all panes in a tmux session by:
1. Unzooming any zoomed panes
2. Applying tiled layout to all windows
3. Attaching/switching to the session

If no session is specified:
- If inside tmux, operates on the current session
- Otherwise, shows a session selector

Examples:
  ntm view myproject
  ntm view                 # Select session or use current
  ntm tile myproject       # Alias`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var session string
			if len(args) > 0 {
				session = args[0]
			}
			return runView(cmd.OutOrStdout(), session)
		},
	}

	cmd.ValidArgsFunction = completeSessionArgs

	return cmd
}

func runView(w io.Writer, session string) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	t := theme.Current()

	res, err := ResolveSession(session, w)
	if err != nil {
		return err
	}
	if res.Session == "" {
		return nil
	}
	res.ExplainIfInferred(os.Stderr)
	session = res.Session

	if !tmux.SessionExists(session) {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(fmt.Sprintf("session '%s' not found", session)))
		}
		cliErr := output.SessionNotFoundError(session)
		output.PrintCLIError(cliErr)
		return cliErr
	}

	result, err := kernel.Run(context.Background(), "sessions.view", SessionViewInput{Session: session})
	if err != nil {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}

	if IsJSONOutput() {
		resp, err := coerceSuccessResponse(result, "sessions.view")
		if err != nil {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return output.PrintJSON(resp)
	}

	fmt.Printf("%s✓%s Tiled layout applied to '%s'\n",
		colorize(t.Success), colorize(t.Text), session)

	// Attach or switch to session
	return tmux.AttachOrSwitch(session)
}

func coerceSuccessResponse(result any, command string) (output.SuccessResponse, error) {
	switch value := result.(type) {
	case output.SuccessResponse:
		return value, nil
	case *output.SuccessResponse:
		if value != nil {
			return *value, nil
		}
		return output.SuccessResponse{}, fmt.Errorf("%s returned nil response", command)
	default:
		return output.SuccessResponse{}, fmt.Errorf("%s returned unexpected type %T", command, result)
	}
}

func buildViewResponse(session string) (output.SuccessResponse, error) {
	if err := tmux.EnsureInstalled(); err != nil {
		return output.SuccessResponse{}, err
	}
	if !tmux.SessionExists(session) {
		return output.SuccessResponse{}, fmt.Errorf("session '%s' not found", session)
	}
	if err := tmux.ApplyTiledLayout(session); err != nil {
		return output.SuccessResponse{}, fmt.Errorf("failed to apply layout: %w", err)
	}
	return output.NewSuccess(fmt.Sprintf("tiled layout applied to '%s'", session)), nil
}
