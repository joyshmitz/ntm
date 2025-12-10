package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/checkpoint"
	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	var agentSpecs AgentSpecs
	var personaSpecs PersonaSpecs

	cmd := &cobra.Command{
		Use:   "add <session-name>",
		Short: "Add more agents to an existing session",
		Long: `Add additional AI agents to an existing tmux session.

You can specify agent counts and optional model variants:
  ntm add myproject --cc=2           # Add 2 Claude agents (default model)
  ntm add myproject --cc=1:opus      # Add 1 Claude Opus agent
  ntm add myproject --cod=1 --gmi=1  # Add 1 Codex, 1 Gemini

Persona mode:
  Use --persona to add agents with predefined roles and system prompts.
  Built-in personas: architect, implementer, reviewer, tester, documenter
  ntm add myproject --persona=reviewer  # Add 1 reviewer agent

Agent count syntax: N or N:model where N is count and model is optional.
Multiple flags of the same type accumulate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionName := args[0]
			dir := cfg.GetProjectDir(sessionName)

			// Handle personas (they contribute to agentSpecs)
			if len(personaSpecs) > 0 {
				resolved, err := ResolvePersonas(personaSpecs, dir)
				if err != nil {
					return err
				}
				personaAgents := FlattenPersonas(resolved)

				// Add persona agents to agentSpecs with persona name as variant
				for _, pa := range personaAgents {
					agentSpecs = append(agentSpecs, AgentSpec{
						Type:  pa.AgentType,
						Count: 1,
						Model: pa.PersonaName, // Use persona name as variant
					})
				}

				if !IsJSONOutput() {
					fmt.Printf("Resolved %d persona agent(s)\n", len(personaAgents))
				}
			}

			return runAdd(sessionName, agentSpecs)
		},
	}

	cmd.Flags().Var(NewAgentSpecsValue(AgentTypeClaude, &agentSpecs), "cc", "Claude agents (N or N:model)")
	cmd.Flags().Var(NewAgentSpecsValue(AgentTypeCodex, &agentSpecs), "cod", "Codex agents (N or N:model)")
	cmd.Flags().Var(NewAgentSpecsValue(AgentTypeGemini, &agentSpecs), "gmi", "Gemini agents (N or N:model)")
	cmd.Flags().Var(&personaSpecs, "persona", "Persona-defined agents (name or name:count)")

	return cmd
}

func runAdd(session string, specs AgentSpecs) error {
	// Aggregate counts per type and keep per-agent model info
	ccSpecs := specs.ByType(AgentTypeClaude)
	codSpecs := specs.ByType(AgentTypeCodex)
	gmiSpecs := specs.ByType(AgentTypeGemini)
	ccCount, codCount, gmiCount := ccSpecs.TotalCount(), codSpecs.TotalCount(), gmiSpecs.TotalCount()
	totalAgents := specs.TotalCount()
	// Helper for JSON error output
	outputError := func(err error) error {
		if IsJSONOutput() {
			return output.PrintJSON(output.NewError(err.Error()))
		}
		return err
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return outputError(err)
	}

	if !tmux.SessionExists(session) {
		return outputError(fmt.Errorf("session '%s' does not exist (use 'ntm spawn' to create)", session))
	}

	if totalAgents == 0 {
		return outputError(fmt.Errorf("no agents specified (use --cc, --cod, or --gmi)"))
	}

	dir := cfg.GetProjectDir(session)
	if !IsJSONOutput() {
		fmt.Printf("Adding %d agent(s) to session '%s'...\n", totalAgents, session)
	}

	// Auto-checkpoint before adding many agents
	if cfg.Checkpoints.Enabled && cfg.Checkpoints.BeforeAddAgents > 0 && totalAgents >= cfg.Checkpoints.BeforeAddAgents {
		if !IsJSONOutput() {
			fmt.Println("Creating auto-checkpoint before adding agents...")
		}
		autoCP := checkpoint.NewAutoCheckpointer()
		cp, err := autoCP.Create(checkpoint.AutoCheckpointOptions{
			SessionName:     session,
			Reason:          checkpoint.ReasonAddAgents,
			Description:     fmt.Sprintf("before adding %d agents", totalAgents),
			ScrollbackLines: cfg.Checkpoints.ScrollbackLines,
			IncludeGit:      cfg.Checkpoints.IncludeGit,
			MaxCheckpoints:  cfg.Checkpoints.MaxAutoCheckpoints,
		})
		if err != nil {
			// Log warning but continue - auto-checkpoint is best-effort
			if !IsJSONOutput() {
				fmt.Printf("⚠ Auto-checkpoint failed: %v\n", err)
			}
		} else if !IsJSONOutput() {
			fmt.Printf("✓ Auto-checkpoint created: %s\n", cp.ID)
		}
	}

	// Track newly added panes for JSON output
	var newPanes []output.PaneResponse

	// Get existing panes to determine next indices
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return outputError(err)
	}

	maxCC, maxCod, maxGmi := 0, 0, 0

	// Helper to parse index from title (e.g., "myproject__cc_2" -> 2)
	parseIndex := func(title, suffix string) int {
		if idx := strings.LastIndex(title, suffix); idx != -1 {
			numPart := title[idx+len(suffix):]
			// Handle variants (e.g., "2_opus")
			if split := strings.SplitN(numPart, "_", 2); len(split) > 0 {
				if val, err := strconv.Atoi(split[0]); err == nil {
					return val
				}
			}
		}
		return 0
	}

	for _, p := range panes {
		if idx := parseIndex(p.Title, fmt.Sprintf("__%s_", tmux.AgentClaude)); idx > maxCC {
			maxCC = idx
		}
		if idx := parseIndex(p.Title, fmt.Sprintf("__%s_", tmux.AgentCodex)); idx > maxCod {
			maxCod = idx
		}
		if idx := parseIndex(p.Title, fmt.Sprintf("__%s_", tmux.AgentGemini)); idx > maxGmi {
			maxGmi = idx
		}
	}

	// Add Claude agents
	ccFlat := ccSpecs.Flatten()
	for _, agent := range ccFlat {
		paneID, err := tmux.SplitWindow(session, dir)
		if err != nil {
			return outputError(fmt.Errorf("creating pane: %w", err))
		}

		num := maxCC + agent.Index
		title := FormatPaneName(session, AgentTypeClaude, num, agent.Model)
		if err := tmux.SetPaneTitle(paneID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}

		// Generate command using template (respect model if provided)
		model := ResolveModel(AgentTypeClaude, agent.Model)
		agentCmd, err := config.GenerateAgentCommand(cfg.Agents.Claude, config.AgentTemplateVars{
			Model:       model,
			SessionName: session,
			PaneIndex:   num,
			AgentType:   "cc",
			ProjectDir:  dir,
		})
		if err != nil {
			return outputError(fmt.Errorf("generating command for Claude agent: %w", err))
		}

		safeAgentCmd, err := tmux.SanitizePaneCommand(agentCmd)
		if err != nil {
			return outputError(fmt.Errorf("invalid Claude agent command: %w", err))
		}

		cmd, err := tmux.BuildPaneCommand(dir, safeAgentCmd)
		if err != nil {
			return outputError(fmt.Errorf("building Claude agent command: %w", err))
		}

		if err := tmux.SendKeys(paneID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching agent: %w", err))
		}

		// Track for JSON output
		newPanes = append(newPanes, output.PaneResponse{
			Title:   title,
			Type:    "claude",
			Variant: agent.Model,
			Command: cmd,
		})
	}

	// Add Codex agents
	codFlat := codSpecs.Flatten()
	for _, agent := range codFlat {
		paneID, err := tmux.SplitWindow(session, dir)
		if err != nil {
			return outputError(fmt.Errorf("creating pane: %w", err))
		}

		num := maxCod + agent.Index
		title := FormatPaneName(session, AgentTypeCodex, num, agent.Model)
		if err := tmux.SetPaneTitle(paneID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}

		// Generate command using template (respect model if provided)
		model := ResolveModel(AgentTypeCodex, agent.Model)
		codexCmd, err := config.GenerateAgentCommand(cfg.Agents.Codex, config.AgentTemplateVars{
			Model:       model,
			SessionName: session,
			PaneIndex:   num,
			AgentType:   "cod",
			ProjectDir:  dir,
		})
		if err != nil {
			return outputError(fmt.Errorf("generating command for Codex agent: %w", err))
		}

		safeCodexCmd, err := tmux.SanitizePaneCommand(codexCmd)
		if err != nil {
			return outputError(fmt.Errorf("invalid Codex agent command: %w", err))
		}

		cmd, err := tmux.BuildPaneCommand(dir, safeCodexCmd)
		if err != nil {
			return outputError(fmt.Errorf("building Codex agent command: %w", err))
		}

		if err := tmux.SendKeys(paneID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching agent: %w", err))
		}

		// Track for JSON output
		newPanes = append(newPanes, output.PaneResponse{
			Title:   title,
			Type:    "codex",
			Variant: agent.Model,
			Command: cmd,
		})
	}

	// Add Gemini agents
	gmiFlat := gmiSpecs.Flatten()
	for _, agent := range gmiFlat {
		paneID, err := tmux.SplitWindow(session, dir)
		if err != nil {
			return outputError(fmt.Errorf("creating pane: %w", err))
		}

		num := maxGmi + agent.Index
		title := fmt.Sprintf("%s__gmi_%d", session, num)
		if err := tmux.SetPaneTitle(paneID, title); err != nil {
			return outputError(fmt.Errorf("setting pane title: %w", err))
		}

		// Generate command using template (respect model if provided)
		model := ResolveModel(AgentTypeGemini, agent.Model)
		geminiCmd, err := config.GenerateAgentCommand(cfg.Agents.Gemini, config.AgentTemplateVars{
			Model:       model,
			SessionName: session,
			PaneIndex:   num,
			AgentType:   "gmi",
			ProjectDir:  dir,
		})
		if err != nil {
			return outputError(fmt.Errorf("generating command for Gemini agent: %w", err))
		}

		safeGeminiCmd, err := tmux.SanitizePaneCommand(geminiCmd)
		if err != nil {
			return outputError(fmt.Errorf("invalid Gemini agent command: %w", err))
		}

		cmd, err := tmux.BuildPaneCommand(dir, safeGeminiCmd)
		if err != nil {
			return outputError(fmt.Errorf("building Gemini agent command: %w", err))
		}

		if err := tmux.SendKeys(paneID, cmd, true); err != nil {
			return outputError(fmt.Errorf("launching agent: %w", err))
		}

		// Track for JSON output
		newPanes = append(newPanes, output.PaneResponse{
			Title:   title,
			Type:    "gemini",
			Command: cmd,
		})
	}

	// JSON output mode
	if IsJSONOutput() {
		return output.PrintJSON(output.AddResponse{
			TimestampedResponse: output.NewTimestamped(),
			Session:             session,
			AddedClaude:         ccCount,
			AddedCodex:          codCount,
			AddedGemini:         gmiCount,
			TotalAdded:          totalAgents,
			NewPanes:            newPanes,
		})
	}

	fmt.Printf("✓ Added %dx cc, %dx cod, %dx gmi\n", ccCount, codCount, gmiCount)
	return nil
}
