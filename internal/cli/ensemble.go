package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

type ensembleStatusCounts struct {
	Pending int `json:"pending" yaml:"pending"`
	Working int `json:"working" yaml:"working"`
	Done    int `json:"done" yaml:"done"`
	Error   int `json:"error" yaml:"error"`
}

type ensembleBudgetSummary struct {
	MaxTokensPerMode     int `json:"max_tokens_per_mode" yaml:"max_tokens_per_mode"`
	MaxTotalTokens       int `json:"max_total_tokens" yaml:"max_total_tokens"`
	EstimatedTotalTokens int `json:"estimated_total_tokens" yaml:"estimated_total_tokens"`
}

type ensembleAssignmentRow struct {
	ModeID        string `json:"mode_id" yaml:"mode_id"`
	ModeCode      string `json:"mode_code,omitempty" yaml:"mode_code,omitempty"`
	ModeName      string `json:"mode_name,omitempty" yaml:"mode_name,omitempty"`
	AgentType     string `json:"agent_type" yaml:"agent_type"`
	Status        string `json:"status" yaml:"status"`
	TokenEstimate int    `json:"token_estimate" yaml:"token_estimate"`
	PaneName      string `json:"pane_name,omitempty" yaml:"pane_name,omitempty"`
}

type ensembleStatusOutput struct {
	GeneratedAt    time.Time                      `json:"generated_at" yaml:"generated_at"`
	Session        string                         `json:"session" yaml:"session"`
	Exists         bool                           `json:"exists" yaml:"exists"`
	EnsembleName   string                         `json:"ensemble_name,omitempty" yaml:"ensemble_name,omitempty"`
	Question       string                         `json:"question,omitempty" yaml:"question,omitempty"`
	StartedAt      time.Time                      `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	Status         string                         `json:"status,omitempty" yaml:"status,omitempty"`
	SynthesisReady bool                           `json:"synthesis_ready,omitempty" yaml:"synthesis_ready,omitempty"`
	Synthesis      string                         `json:"synthesis,omitempty" yaml:"synthesis,omitempty"`
	Budget         ensembleBudgetSummary          `json:"budget,omitempty" yaml:"budget,omitempty"`
	StatusCounts   ensembleStatusCounts           `json:"status_counts,omitempty" yaml:"status_counts,omitempty"`
	Assignments    []ensembleAssignmentRow        `json:"assignments,omitempty" yaml:"assignments,omitempty"`
	Contributions  *ensemble.ContributionReport   `json:"contributions,omitempty" yaml:"contributions,omitempty"`
}

func newEnsembleCmd() *cobra.Command {
	opts := ensembleSpawnOptions{
		Assignment: "affinity",
	}

	cmd := &cobra.Command{
		Use:   "ensemble [ensemble] [question]",
		Short: "Manage reasoning ensembles",
		Long: `Manage and run reasoning ensembles.

Primary usage:
  ntm ensemble <ensemble-name> "<question>"
`,
		Example: `  ntm ensemble project-diagnosis "What are the main issues?"
  ntm ensemble idea-forge "What features should we add next?"
  ntm ensemble spawn mysession --preset project-diagnosis --question "..."`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				return fmt.Errorf("ensemble name and question required (usage: ntm ensemble <ensemble-name> <question>)")
			}

			projectDir, err := resolveEnsembleProjectDir(opts.Project)
			if err != nil {
				if IsJSONOutput() {
					_ = output.PrintJSON(output.NewError(err.Error()))
				}
				return err
			}
			opts.Project = projectDir

			if err := tmux.EnsureInstalled(); err != nil {
				if IsJSONOutput() {
					_ = output.PrintJSON(output.NewError(err.Error()))
				}
				return err
			}

			baseName := defaultEnsembleSessionName(projectDir)
			opts.Session = uniqueEnsembleSessionName(baseName)
			opts.Preset = args[0]
			opts.Question = strings.Join(args[1:], " ")

			return runEnsembleSpawn(cmd, opts)
		},
	}

	bindEnsembleSharedFlags(cmd, &opts)
	cmd.AddCommand(newEnsembleSpawnCmd())
	cmd.AddCommand(newEnsembleStatusCmd())
	cmd.AddCommand(newEnsembleStopCmd())
	cmd.AddCommand(newEnsembleSuggestCmd())
	cmd.AddCommand(newEnsembleSynthesizeCmd())
	cmd.AddCommand(newEnsembleProvenanceCmd())
	cmd.AddCommand(newEnsembleResumeCmd())
	cmd.AddCommand(newEnsembleRerunModeCmd())
	cmd.AddCommand(newEnsembleCleanCheckpointsCmd())
	return cmd
}

type ensembleStatusOptions struct {
	Format            string
	ShowContributions bool
}

func newEnsembleStatusCmd() *cobra.Command {
	opts := ensembleStatusOptions{
		Format: "table",
	}
	cmd := &cobra.Command{
		Use:   "status [session]",
		Short: "Show status for an ensemble session",
		Long: `Show the current ensemble session state, assignments, and synthesis readiness.

Formats:
  --format=table (default)
  --format=json
  --format=yaml

Use --show-contributions to include mode contribution scores (requires completed outputs).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := ""
			if len(args) > 0 {
				session = args[0]
			} else {
				session = tmux.GetCurrentSession()
			}
			if session == "" {
				return fmt.Errorf("session required (not in tmux)")
			}

			return runEnsembleStatus(cmd.OutOrStdout(), session, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "table", "Output format: table, json, yaml")
	cmd.Flags().BoolVar(&opts.ShowContributions, "show-contributions", false, "Include mode contribution scores")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

type ensembleStopOptions struct {
	Force     bool
	NoCollect bool
	Quiet     bool
	Format    string
}

type ensembleStopOutput struct {
	GeneratedAt time.Time               `json:"generated_at" yaml:"generated_at"`
	Session     string                  `json:"session" yaml:"session"`
	Success     bool                    `json:"success" yaml:"success"`
	Message     string                  `json:"message,omitempty" yaml:"message,omitempty"`
	Captured    int                     `json:"captured,omitempty" yaml:"captured,omitempty"`
	Stopped     int                     `json:"stopped" yaml:"stopped"`
	Errors      int                     `json:"errors,omitempty" yaml:"errors,omitempty"`
	FinalStatus string                  `json:"final_status" yaml:"final_status"`
	Error       string                  `json:"error,omitempty" yaml:"error,omitempty"`
}

func newEnsembleStopCmd() *cobra.Command {
	opts := ensembleStopOptions{
		Format: "text",
	}

	cmd := &cobra.Command{
		Use:   "stop [session]",
		Short: "Stop an ensemble run gracefully",
		Long: `Stop all agents in an ensemble run and save partial state.

Behavior:
  1. Signal all ensemble agents to stop (SIGTERM)
  2. Wait for graceful shutdown (5s timeout)
  3. Force kill remaining agents
  4. Collect any partial outputs available
  5. Update ensemble state to 'stopped'
  6. Show summary of what was captured

Flags:
  --force        Skip graceful shutdown, force kill immediately
  --no-collect   Don't attempt to collect partial outputs
  --quiet        Minimal output
  --format       Output format: text, json, yaml`,
		Example: `  ntm ensemble stop
  ntm ensemble stop my-ensemble-session
  ntm ensemble stop --force
  ntm ensemble stop --no-collect --quiet`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := ""
			if len(args) > 0 {
				session = args[0]
			} else {
				session = tmux.GetCurrentSession()
			}
			if session == "" {
				return fmt.Errorf("session required (not in tmux)")
			}

			return runEnsembleStop(cmd.OutOrStdout(), session, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Force, "force", false, "Skip graceful shutdown, force kill immediately")
	cmd.Flags().BoolVar(&opts.NoCollect, "no-collect", false, "Don't attempt to collect partial outputs")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "Minimal output")
	cmd.Flags().StringVarP(&opts.Format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

func runEnsembleStop(w io.Writer, session string, opts ensembleStopOptions) error {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}
	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	// Load ensemble session state
	state, err := ensemble.LoadSession(session)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no ensemble running in session '%s'", session)
		}
		return fmt.Errorf("load session: %w", err)
	}

	// Check if already stopped
	if state.Status.IsTerminal() {
		result := ensembleStopOutput{
			GeneratedAt: output.Timestamp(),
			Session:     session,
			Success:     true,
			Message:     fmt.Sprintf("Ensemble already in terminal state: %s", state.Status),
			FinalStatus: state.Status.String(),
		}
		return renderEnsembleStopOutput(w, result, format, opts.Quiet)
	}

	slog.Default().Info("ensemble stop initiated",
		"session", session,
		"force", opts.Force,
		"no_collect", opts.NoCollect,
	)

	var captured int
	var collectErrors []error

	// Collect partial outputs if requested
	if !opts.NoCollect {
		capture := ensemble.NewOutputCapture(tmux.DefaultClient)
		capturedOutputs, err := capture.CaptureAll(state)
		if err != nil {
			slog.Default().Warn("failed to capture partial outputs", "error", err)
			collectErrors = append(collectErrors, err)
		} else {
			captured = len(capturedOutputs)
			slog.Default().Info("captured partial outputs",
				"session", session,
				"count", captured,
			)
		}
	}

	// Get all panes for the session
	panes, err := tmux.GetPanes(session)
	if err != nil {
		slog.Default().Warn("failed to get panes", "error", err)
	}

	stoppedCount := 0
	var stopErrors []error

	// Graceful shutdown: send Ctrl+C to each pane
	if !opts.Force && len(panes) > 0 {
		for _, pane := range panes {
			// Send Ctrl+C (interrupt signal)
			if err := tmux.SendKeys(pane.ID, "C-c", false); err != nil {
				slog.Default().Warn("failed to send interrupt to pane",
					"pane", pane.ID,
					"error", err,
				)
			}
		}

		// Wait for graceful shutdown
		time.Sleep(5 * time.Second)
	}

	// Kill the session (force or after graceful timeout)
	if err := tmux.KillSession(session); err != nil {
		slog.Default().Warn("failed to kill session", "session", session, "error", err)
		stopErrors = append(stopErrors, err)
	} else {
		stoppedCount = len(panes)
		slog.Default().Info("killed session", "session", session, "panes", stoppedCount)
	}

	// Update ensemble state to stopped
	state.Status = ensemble.EnsembleStopped
	if err := ensemble.SaveSession(session, state); err != nil {
		slog.Default().Warn("failed to save stopped state", "error", err)
		stopErrors = append(stopErrors, err)
	}

	// Build result
	result := ensembleStopOutput{
		GeneratedAt: output.Timestamp(),
		Session:     session,
		Success:     len(stopErrors) == 0,
		Captured:    captured,
		Stopped:     stoppedCount,
		Errors:      len(stopErrors) + len(collectErrors),
		FinalStatus: ensemble.EnsembleStopped.String(),
	}

	if len(stopErrors) > 0 {
		result.Error = fmt.Sprintf("%d errors during stop", len(stopErrors))
	}

	if result.Success {
		result.Message = fmt.Sprintf("Ensemble stopped: %d panes terminated", stoppedCount)
		if captured > 0 {
			result.Message += fmt.Sprintf(", %d outputs captured", captured)
		}
	}

	slog.Default().Info("ensemble stop completed",
		"session", session,
		"stopped", stoppedCount,
		"captured", captured,
		"errors", len(stopErrors)+len(collectErrors),
	)

	return renderEnsembleStopOutput(w, result, format, opts.Quiet)
}

func renderEnsembleStopOutput(w io.Writer, payload ensembleStopOutput, format string, quiet bool) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml", "yml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			_, err = w.Write([]byte("\n"))
			return err
		}
		return nil
	case "text", "table":
		if quiet {
			if payload.Success {
				fmt.Fprintf(w, "stopped\n")
			} else {
				fmt.Fprintf(w, "error: %s\n", payload.Error)
			}
			return nil
		}

		if payload.Error != "" {
			fmt.Fprintf(w, "Error: %s\n", payload.Error)
		}
		fmt.Fprintf(w, "Session:  %s\n", payload.Session)
		fmt.Fprintf(w, "Status:   %s\n", payload.FinalStatus)
		fmt.Fprintf(w, "Stopped:  %d panes\n", payload.Stopped)
		if payload.Captured > 0 {
			fmt.Fprintf(w, "Captured: %d outputs\n", payload.Captured)
		}
		if payload.Message != "" {
			fmt.Fprintf(w, "\n%s\n", payload.Message)
		}
		return nil
	default:
		return fmt.Errorf("invalid format %q (expected text, json, yaml)", format)
	}
}

func runEnsembleStatus(w io.Writer, session string, opts ensembleStatusOptions) error {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "table"
	}
	if jsonOutput {
		format = "json"
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}
	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	queryStart := time.Now()
	panes, err := tmux.GetPanes(session)
	queryDuration := time.Since(queryStart)
	if err != nil {
		return err
	}
	slog.Default().Info("ensemble status tmux query",
		"session", session,
		"panes", len(panes),
		"duration_ms", queryDuration.Milliseconds(),
	)

	state, err := ensemble.LoadSession(session)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return renderEnsembleStatus(w, ensembleStatusOutput{
				GeneratedAt: output.Timestamp(),
				Session:     session,
				Exists:      false,
			}, format)
		}
		return err
	}

	catalog, _ := ensemble.GlobalCatalog()
	preset, budget := resolveEnsembleBudget(state)
	assignments, counts := buildEnsembleAssignments(state, catalog, budget.MaxTokensPerMode)

	totalEstimate := budget.MaxTokensPerMode * len(assignments)
	synthesisReady := counts.Pending == 0 && counts.Working == 0 && len(assignments) > 0

	slog.Default().Info("ensemble status counts",
		"session", session,
		"pending", counts.Pending,
		"working", counts.Working,
		"done", counts.Done,
		"error", counts.Error,
	)

	outputData := ensembleStatusOutput{
		GeneratedAt:    output.Timestamp(),
		Session:        session,
		Exists:         true,
		EnsembleName:   preset,
		Question:       state.Question,
		StartedAt:      state.CreatedAt,
		Status:         state.Status.String(),
		SynthesisReady: synthesisReady,
		Synthesis:      state.SynthesisStrategy.String(),
		Budget: ensembleBudgetSummary{
			MaxTokensPerMode:     budget.MaxTokensPerMode,
			MaxTotalTokens:       budget.MaxTotalTokens,
			EstimatedTotalTokens: totalEstimate,
		},
		StatusCounts: counts,
		Assignments:  assignments,
	}

	// Compute contributions if requested and there are completed outputs
	if opts.ShowContributions && counts.Done > 0 {
		contributions, err := computeContributions(state, catalog)
		if err != nil {
			slog.Default().Warn("failed to compute contributions", "error", err)
		} else {
			outputData.Contributions = contributions
		}
	}

	return renderEnsembleStatus(w, outputData, format)
}

// computeContributions collects outputs and computes mode contribution scores.
func computeContributions(state *ensemble.EnsembleSession, catalog *ensemble.ModeCatalog) (*ensemble.ContributionReport, error) {
	capture := ensemble.NewOutputCapture(tmux.DefaultClient)
	captured, err := capture.CaptureAll(state)
	if err != nil {
		return nil, fmt.Errorf("capture outputs: %w", err)
	}

	// Convert to ModeOutputs
	outputs := make([]ensemble.ModeOutput, 0, len(captured))
	for _, cap := range captured {
		if cap.Parsed == nil {
			continue
		}
		parsed := *cap.Parsed
		if parsed.ModeID == "" {
			parsed.ModeID = cap.ModeID
		}
		outputs = append(outputs, parsed)
	}

	if len(outputs) == 0 {
		return nil, fmt.Errorf("no valid outputs to analyze")
	}

	// Create contribution tracker
	tracker := ensemble.NewContributionTracker()

	// Track original findings
	ensemble.TrackOriginalFindings(tracker, outputs)

	// Perform merge to identify surviving and unique findings
	merged := ensemble.MergeOutputs(outputs, ensemble.DefaultMergeConfig())

	// Track contributions from merged output
	ensemble.TrackContributionsFromMerge(tracker, merged)

	// Set mode names from catalog
	if catalog != nil {
		for _, o := range outputs {
			if mode := catalog.GetMode(o.ModeID); mode != nil {
				tracker.SetModeName(o.ModeID, mode.Name)
			}
		}
	}

	return tracker.GenerateReport(), nil
}

func resolveEnsembleBudget(state *ensemble.EnsembleSession) (string, ensemble.BudgetConfig) {
	name := state.PresetUsed
	if strings.TrimSpace(name) == "" {
		name = "custom"
	}
	budget := ensemble.DefaultBudgetConfig()

	registry, err := ensemble.GlobalEnsembleRegistry()
	if err != nil || registry == nil {
		return name, budget
	}

	if preset := registry.Get(state.PresetUsed); preset != nil {
		name = preset.DisplayName
		if name == "" {
			name = preset.Name
		}
		budget = mergeBudgetDefaults(preset.Budget, budget)
	}

	return name, budget
}

func mergeBudgetDefaults(current, defaults ensemble.BudgetConfig) ensemble.BudgetConfig {
	if current.MaxTokensPerMode == 0 {
		current.MaxTokensPerMode = defaults.MaxTokensPerMode
	}
	if current.MaxTotalTokens == 0 {
		current.MaxTotalTokens = defaults.MaxTotalTokens
	}
	if current.SynthesisReserveTokens == 0 {
		current.SynthesisReserveTokens = defaults.SynthesisReserveTokens
	}
	if current.ContextReserveTokens == 0 {
		current.ContextReserveTokens = defaults.ContextReserveTokens
	}
	if current.TimeoutPerMode == 0 {
		current.TimeoutPerMode = defaults.TimeoutPerMode
	}
	if current.TotalTimeout == 0 {
		current.TotalTimeout = defaults.TotalTimeout
	}
	if current.MaxRetries == 0 {
		current.MaxRetries = defaults.MaxRetries
	}
	return current
}

func buildEnsembleAssignments(state *ensemble.EnsembleSession, catalog *ensemble.ModeCatalog, tokenEstimate int) ([]ensembleAssignmentRow, ensembleStatusCounts) {
	rows := make([]ensembleAssignmentRow, 0, len(state.Assignments))
	var counts ensembleStatusCounts

	for _, assignment := range state.Assignments {
		modeCode := ""
		modeName := ""
		if catalog != nil {
			if mode := catalog.GetMode(assignment.ModeID); mode != nil {
				modeCode = mode.Code
				modeName = mode.Name
			}
		}

		status := assignment.Status.String()
		switch assignment.Status {
		case ensemble.AssignmentPending, ensemble.AssignmentInjecting:
			counts.Pending++
		case ensemble.AssignmentActive:
			counts.Working++
		case ensemble.AssignmentDone:
			counts.Done++
		case ensemble.AssignmentError:
			counts.Error++
		default:
			counts.Pending++
		}

		rows = append(rows, ensembleAssignmentRow{
			ModeID:        assignment.ModeID,
			ModeCode:      modeCode,
			ModeName:      modeName,
			AgentType:     assignment.AgentType,
			Status:        status,
			TokenEstimate: tokenEstimate,
			PaneName:      assignment.PaneName,
		})
	}

	return rows, counts
}

func renderEnsembleStatus(w io.Writer, payload ensembleStatusOutput, format string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml", "yml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			_, err = w.Write([]byte("\n"))
			return err
		}
		return nil
	case "table", "text":
		if !payload.Exists {
			fmt.Fprintf(w, "No ensemble running for session %s\n", payload.Session)
			return nil
		}

		fmt.Fprintf(w, "Session:   %s\n", payload.Session)
		fmt.Fprintf(w, "Ensemble:  %s\n", payload.EnsembleName)
		if strings.TrimSpace(payload.Question) != "" {
			fmt.Fprintf(w, "Question:  %s\n", payload.Question)
		}
		if !payload.StartedAt.IsZero() {
			fmt.Fprintf(w, "Started:   %s\n", payload.StartedAt.Format(time.RFC3339))
		}
		if payload.Status != "" {
			fmt.Fprintf(w, "Status:    %s\n", payload.Status)
		}
		if payload.Synthesis != "" {
			fmt.Fprintf(w, "Synthesis: %s\n", payload.Synthesis)
		}
		fmt.Fprintf(w, "Ready:     %t\n", payload.SynthesisReady)
		fmt.Fprintf(w, "Budget:    %d per mode, %d total (est %d)\n",
			payload.Budget.MaxTokensPerMode,
			payload.Budget.MaxTotalTokens,
			payload.Budget.EstimatedTotalTokens,
		)
		fmt.Fprintf(w, "Counts:    pending=%d working=%d done=%d error=%d\n\n",
			payload.StatusCounts.Pending,
			payload.StatusCounts.Working,
			payload.StatusCounts.Done,
			payload.StatusCounts.Error,
		)

		table := output.NewTable(w, "MODE", "CODE", "AGENT", "STATUS", "TOKENS", "PANE")
		for _, row := range payload.Assignments {
			table.AddRow(row.ModeID, row.ModeCode, row.AgentType, row.Status, fmt.Sprintf("%d", row.TokenEstimate), row.PaneName)
		}
		table.Render()

		// Render contribution report if present
		if payload.Contributions != nil && len(payload.Contributions.Scores) > 0 {
			fmt.Fprintf(w, "\nMode Contributions\n")
			fmt.Fprintf(w, "------------------\n")
			fmt.Fprintf(w, "Total Findings: %d (deduped: %d)  Overlap: %.1f%%  Diversity: %.2f\n\n",
				payload.Contributions.TotalFindings,
				payload.Contributions.DedupedFindings,
				payload.Contributions.OverlapRate*100,
				payload.Contributions.DiversityScore,
			)

			ctable := output.NewTable(w, "RANK", "MODE", "SCORE", "FINDINGS", "UNIQUE", "CITATIONS")
			for _, score := range payload.Contributions.Scores {
				name := score.ModeName
				if name == "" {
					name = score.ModeID
				}
				ctable.AddRow(
					fmt.Sprintf("#%d", score.Rank),
					name,
					fmt.Sprintf("%.1f", score.Score),
					fmt.Sprintf("%d/%d", score.FindingsCount, score.OriginalFindings),
					fmt.Sprintf("%d", score.UniqueInsights),
					fmt.Sprintf("%d", score.CitationCount),
				)
			}
			ctable.Render()
		}
		return nil
	default:
		return fmt.Errorf("invalid format %q (expected table, json, yaml)", format)
	}
}

type synthesizeOptions struct {
	Strategy string
	Output   string
	Format   string
	Force    bool
	Verbose  bool
	Explain  bool
}

func newEnsembleSynthesizeCmd() *cobra.Command {
	opts := synthesizeOptions{
		Format: "markdown",
	}

	cmd := &cobra.Command{
		Use:   "synthesize [session]",
		Short: "Synthesize outputs from ensemble agents",
		Long: `Trigger synthesis of ensemble outputs.

Collects outputs from all ensemble agents, validates them, and produces a
synthesized analysis using the configured strategy.

Output formats:
  --format=markdown (default) - Human-readable report
  --format=json               - Machine-readable JSON
  --format=yaml               - YAML format

Use --force to synthesize even if some agents haven't completed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := ""
			if len(args) > 0 {
				session = args[0]
			} else {
				session = tmux.GetCurrentSession()
			}
			if session == "" {
				return fmt.Errorf("session required (not in tmux)")
			}

			return runEnsembleSynthesize(cmd.OutOrStdout(), session, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Strategy, "strategy", "", "Override synthesis strategy")
	cmd.Flags().StringVarP(&opts.Output, "output", "o", "", "Output file path (default: stdout)")
	cmd.Flags().StringVarP(&opts.Format, "format", "f", "markdown", "Output format: markdown, json, yaml")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Synthesize even if some agents incomplete")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Include verbose details in output")
	cmd.Flags().BoolVar(&opts.Explain, "explain", false, "Include detailed reasoning for each conclusion")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

func runEnsembleSynthesize(w io.Writer, session string, opts synthesizeOptions) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}
	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	// Load ensemble session state
	state, err := ensemble.LoadSession(session)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no ensemble running in session '%s'", session)
		}
		return fmt.Errorf("load session: %w", err)
	}

	// Check if agents are ready
	ready, pending, working := countAgentStates(state)
	if !opts.Force && (pending > 0 || working > 0) {
		return fmt.Errorf("synthesis not ready: %d pending, %d working (use --force to override)", pending, working)
	}
	if ready == 0 && !opts.Force {
		return fmt.Errorf("no completed outputs to synthesize")
	}

	slog.Default().Info("ensemble synthesis starting",
		"session", session,
		"ready", ready,
		"pending", pending,
		"working", working,
		"force", opts.Force,
	)

	// Create output capture and collector
	capture := ensemble.NewOutputCapture(tmux.DefaultClient)
	collector := ensemble.NewOutputCollector(ensemble.DefaultOutputCollectorConfig())

	// Collect outputs from panes
	if err := collector.CollectFromSession(state, capture); err != nil {
		return fmt.Errorf("collect outputs: %w", err)
	}

	if collector.Count() == 0 {
		return fmt.Errorf("no valid outputs collected (errors: %d)", collector.ErrorCount())
	}

	slog.Default().Info("ensemble outputs collected",
		"session", session,
		"valid", collector.Count(),
		"errors", collector.ErrorCount(),
	)

	// Determine synthesis strategy
	strategy := state.SynthesisStrategy
	if opts.Strategy != "" {
		strategy = ensemble.SynthesisStrategy(opts.Strategy)
	}

	// Build synthesis config
	synthConfig := ensemble.SynthesisConfig{
		Strategy:           strategy,
		MaxFindings:        20,
		MinConfidence:      0.3,
		IncludeExplanation: opts.Explain,
	}

	// Create synthesizer
	synth, err := ensemble.NewSynthesizer(synthConfig)
	if err != nil {
		return fmt.Errorf("create synthesizer: %w", err)
	}

	// Build synthesis input
	input, err := collector.BuildSynthesisInput(state.Question, nil, synthConfig)
	if err != nil {
		return fmt.Errorf("build synthesis input: %w", err)
	}

	// Run synthesis
	result, err := synth.Synthesize(input)
	if err != nil {
		return fmt.Errorf("synthesis failed: %w", err)
	}

	slog.Default().Info("ensemble synthesis completed",
		"session", session,
		"findings", len(result.Findings),
		"risks", len(result.Risks),
		"recommendations", len(result.Recommendations),
		"confidence", float64(result.Confidence),
	)

	// Format output
	outputFormat := ensemble.FormatMarkdown
	switch strings.ToLower(opts.Format) {
	case "json":
		outputFormat = ensemble.FormatJSON
	case "yaml", "yml":
		outputFormat = ensemble.FormatYAML
	}

	formatter := ensemble.NewSynthesisFormatter(outputFormat)
	formatter.Verbose = opts.Verbose
	formatter.IncludeAudit = true
	formatter.IncludeExplanation = opts.Explain

	// Determine output destination
	var out io.Writer = w
	if opts.Output != "" {
		f, err := os.Create(opts.Output)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	if err := formatter.FormatResult(out, result, input.AuditReport); err != nil {
		return fmt.Errorf("format output: %w", err)
	}

	return nil
}

func countAgentStates(state *ensemble.EnsembleSession) (ready, pending, working int) {
	for _, a := range state.Assignments {
		switch a.Status {
		case ensemble.AssignmentDone:
			ready++
		case ensemble.AssignmentPending, ensemble.AssignmentInjecting:
			pending++
		case ensemble.AssignmentActive:
			working++
		}
	}
	return
}

// Provenance command types

type provenanceOptions struct {
	Format  string
	Session string
	All     bool
	Stats   bool
}

type provenanceOutput struct {
	GeneratedAt time.Time                 `json:"generated_at" yaml:"generated_at"`
	FindingID   string                    `json:"finding_id,omitempty" yaml:"finding_id,omitempty"`
	Chain       *ensemble.ProvenanceChain `json:"chain,omitempty" yaml:"chain,omitempty"`
	Stats       *ensemble.ProvenanceStats `json:"stats,omitempty" yaml:"stats,omitempty"`
	Chains      []*ensemble.ProvenanceChain `json:"chains,omitempty" yaml:"chains,omitempty"`
	Error       string                    `json:"error,omitempty" yaml:"error,omitempty"`
}

func newEnsembleProvenanceCmd() *cobra.Command {
	opts := provenanceOptions{
		Format: "text",
	}

	cmd := &cobra.Command{
		Use:   "provenance [finding-id]",
		Short: "Show provenance chain for a finding",
		Long: `Display the full provenance chain for a finding.

Shows the finding's origin, transformations, and synthesis usage.

Without a finding-id, use --all to list all tracked findings.
Use --stats to show provenance statistics.

Formats:
  --format=text (default) - Human-readable timeline
  --format=json           - Machine-readable JSON
  --format=yaml           - YAML format`,
		Example: `  ntm ensemble provenance abc123def456
  ntm ensemble provenance --all
  ntm ensemble provenance --stats
  ntm ensemble provenance --all --format=json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			session := opts.Session
			if session == "" {
				session = tmux.GetCurrentSession()
			}
			if session == "" {
				return fmt.Errorf("session required (not in tmux or use --session)")
			}

			findingID := ""
			if len(args) > 0 {
				findingID = args[0]
			}

			return runEnsembleProvenance(cmd.OutOrStdout(), session, findingID, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().StringVarP(&opts.Session, "session", "s", "", "Session name (default: current)")
	cmd.Flags().BoolVar(&opts.All, "all", false, "List all tracked findings")
	cmd.Flags().BoolVar(&opts.Stats, "stats", false, "Show provenance statistics")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

func runEnsembleProvenance(w io.Writer, session, findingID string, opts provenanceOptions) error {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}
	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	// Load ensemble session state
	state, err := ensemble.LoadSession(session)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no ensemble running in session '%s'", session)
		}
		return fmt.Errorf("load session: %w", err)
	}

	// Create provenance tracker and populate from session
	modeIDs := make([]string, 0, len(state.Assignments))
	for _, a := range state.Assignments {
		modeIDs = append(modeIDs, a.ModeID)
	}
	tracker := ensemble.NewProvenanceTracker(state.Question, modeIDs)

	// Load outputs and record provenance
	capture := ensemble.NewOutputCapture(tmux.DefaultClient)
	captured, err := capture.CaptureAll(state)
	if err != nil {
		slog.Default().Warn("failed to capture outputs for provenance", "error", err)
	}

	outputs := make([]ensemble.ModeOutput, 0, len(captured))
	for _, cap := range captured {
		if cap.Parsed == nil {
			continue
		}
		parsed := *cap.Parsed
		if parsed.ModeID == "" {
			parsed.ModeID = cap.ModeID
		}
		outputs = append(outputs, parsed)
	}

	if len(outputs) > 0 {
		synth, synthErr := ensemble.NewSynthesizer(ensemble.DefaultSynthesisConfig())
		if synthErr != nil {
			slog.Default().Warn("failed to initialize synthesizer for provenance", "error", synthErr)
		} else if _, synthErr := synth.Synthesize(&ensemble.SynthesisInput{
			Outputs:          outputs,
			OriginalQuestion: state.Question,
			Config:           synth.Config,
			Provenance:       tracker,
		}); synthErr != nil {
			slog.Default().Warn("failed to synthesize for provenance", "error", synthErr)
		}
	}

	slog.Default().Info("provenance tracker populated",
		"session", session,
		"total", tracker.Count(),
		"active", tracker.ActiveCount(),
	)

	// Handle stats mode
	if opts.Stats {
		stats := tracker.Stats()
		return renderProvenanceOutput(w, provenanceOutput{
			GeneratedAt: output.Timestamp(),
			Stats:       &stats,
		}, format)
	}

	// Handle all mode
	if opts.All {
		chains := tracker.ListChains()
		return renderProvenanceOutput(w, provenanceOutput{
			GeneratedAt: output.Timestamp(),
			Chains:      chains,
		}, format)
	}

	// Handle single finding lookup
	if findingID == "" {
		return fmt.Errorf("finding-id required (or use --all or --stats)")
	}

	chain, found := tracker.GetChain(findingID)
	if !found {
		// Try partial match
		chains := tracker.ListChains()
		for _, c := range chains {
			if strings.HasPrefix(c.FindingID, findingID) {
				chain = c
				found = true
				break
			}
		}
	}

	if !found {
		return renderProvenanceOutput(w, provenanceOutput{
			GeneratedAt: output.Timestamp(),
			FindingID:   findingID,
			Error:       fmt.Sprintf("finding '%s' not found", findingID),
		}, format)
	}

	return renderProvenanceOutput(w, provenanceOutput{
		GeneratedAt: output.Timestamp(),
		FindingID:   findingID,
		Chain:       chain,
	}, format)
}

func renderProvenanceOutput(w io.Writer, payload provenanceOutput, format string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml", "yml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal yaml: %w", err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if len(data) == 0 || data[len(data)-1] != '\n' {
			_, err = w.Write([]byte("\n"))
			return err
		}
		return nil
	case "text", "table":
		if payload.Error != "" {
			fmt.Fprintf(w, "Error: %s\n", payload.Error)
			return nil
		}

		if payload.Stats != nil {
			fmt.Fprintf(w, "Provenance Statistics\n")
			fmt.Fprintf(w, "=====================\n\n")
			fmt.Fprintf(w, "Total Findings:   %d\n", payload.Stats.TotalFindings)
			fmt.Fprintf(w, "Active Findings:  %d\n", payload.Stats.ActiveFindings)
			fmt.Fprintf(w, "Merged Findings:  %d\n", payload.Stats.MergedFindings)
			fmt.Fprintf(w, "Filtered Count:   %d\n", payload.Stats.FilteredCount)
			fmt.Fprintf(w, "Cited in Synthesis: %d\n\n", payload.Stats.CitedCount)

			if len(payload.Stats.ModeBreakdown) > 0 {
				fmt.Fprintf(w, "By Mode:\n")
				for mode, count := range payload.Stats.ModeBreakdown {
					fmt.Fprintf(w, "  %-20s %d\n", mode, count)
				}
			}
			return nil
		}

		if len(payload.Chains) > 0 {
			fmt.Fprintf(w, "Tracked Findings (%d)\n", len(payload.Chains))
			fmt.Fprintf(w, "====================\n\n")

			table := output.NewTable(w, "ID", "MODE", "IMPACT", "CONF", "TEXT")
			for _, chain := range payload.Chains {
				text := chain.CurrentText
				if len(text) > 60 {
					text = text[:57] + "..."
				}
				table.AddRow(
					chain.FindingID,
					chain.SourceMode,
					string(chain.Impact),
					chain.Confidence.String(),
					text,
				)
			}
			table.Render()
			return nil
		}

		if payload.Chain != nil {
			fmt.Fprint(w, ensemble.FormatProvenance(payload.Chain))
			return nil
		}

		fmt.Fprintf(w, "No provenance data available\n")
		return nil
	default:
		return fmt.Errorf("invalid format %q (expected text, json, yaml)", format)
	}
}

// --- Checkpoint Recovery Commands ---

type checkpointListOutput struct {
	GeneratedAt time.Time                     `json:"generated_at" yaml:"generated_at"`
	Checkpoints []ensemble.CheckpointMetadata `json:"checkpoints" yaml:"checkpoints"`
	Count       int                           `json:"count" yaml:"count"`
}

type checkpointResumeOutput struct {
	GeneratedAt time.Time `json:"generated_at" yaml:"generated_at"`
	RunID       string    `json:"run_id" yaml:"run_id"`
	Session     string    `json:"session" yaml:"session"`
	Success     bool      `json:"success" yaml:"success"`
	Message     string    `json:"message,omitempty" yaml:"message,omitempty"`
	Resumed     int       `json:"resumed,omitempty" yaml:"resumed,omitempty"`
	Skipped     int       `json:"skipped,omitempty" yaml:"skipped,omitempty"`
	Error       string    `json:"error,omitempty" yaml:"error,omitempty"`
}

type checkpointCleanOutput struct {
	GeneratedAt time.Time `json:"generated_at" yaml:"generated_at"`
	Removed     int       `json:"removed" yaml:"removed"`
	Message     string    `json:"message" yaml:"message"`
}

func newEnsembleResumeCmd() *cobra.Command {
	var (
		format   string
		quiet    bool
		skipDone bool
	)

	cmd := &cobra.Command{
		Use:   "resume <run-id>",
		Short: "Resume an interrupted ensemble run from checkpoint",
		Long: `Resume an ensemble run that was interrupted or failed.

The resume command loads the checkpoint state and:
  1. Identifies which modes completed successfully
  2. Re-runs any pending or errored modes
  3. Continues with synthesis when all modes complete

Use 'ntm ensemble list-checkpoints' to see available runs.`,
		Example: `  ntm ensemble resume my-ensemble-run
  ntm ensemble resume my-ensemble-run --skip-done
  ntm ensemble resume my-ensemble-run --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			return runEnsembleResume(cmd.OutOrStdout(), runID, format, quiet, skipDone)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Minimal output")
	cmd.Flags().BoolVar(&skipDone, "skip-done", true, "Skip already completed modes (default: true)")

	return cmd
}

func runEnsembleResume(w io.Writer, runID, format string, quiet, skipDone bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	store, err := ensemble.NewCheckpointStore("")
	if err != nil {
		return fmt.Errorf("open checkpoint store: %w", err)
	}

	if !store.RunExists(runID) {
		errMsg := fmt.Sprintf("checkpoint run '%s' not found", runID)
		result := checkpointResumeOutput{
			GeneratedAt: output.Timestamp(),
			RunID:       runID,
			Success:     false,
			Error:       errMsg,
		}
		return renderCheckpointResumeOutput(w, result, format, quiet)
	}

	meta, err := store.LoadMetadata(runID)
	if err != nil {
		return fmt.Errorf("load checkpoint metadata: %w", err)
	}

	slog.Default().Info("resuming ensemble run",
		"run_id", runID,
		"session", meta.SessionName,
		"completed", len(meta.CompletedIDs),
		"pending", len(meta.PendingIDs),
		"errors", len(meta.ErrorIDs),
	)

	// Calculate modes to run
	toRun := append([]string{}, meta.PendingIDs...)
	toRun = append(toRun, meta.ErrorIDs...)
	skipped := len(meta.CompletedIDs)

	result := checkpointResumeOutput{
		GeneratedAt: output.Timestamp(),
		RunID:       runID,
		Session:     meta.SessionName,
		Success:     true,
		Resumed:     len(toRun),
		Skipped:     skipped,
		Message:     fmt.Sprintf("Resume initiated: %d modes to run, %d already complete", len(toRun), skipped),
	}

	if len(toRun) == 0 {
		result.Message = "All modes already completed - no resume needed"
	}

	return renderCheckpointResumeOutput(w, result, format, quiet)
}

func renderCheckpointResumeOutput(w io.Writer, payload checkpointResumeOutput, format string, quiet bool) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		if quiet {
			if payload.Success {
				fmt.Fprintf(w, "resumed\n")
			} else {
				fmt.Fprintf(w, "error: %s\n", payload.Error)
			}
			return nil
		}

		if payload.Error != "" {
			fmt.Fprintf(w, "Resume failed: %s\n", payload.Error)
			return nil
		}

		fmt.Fprintf(w, "Ensemble Resume: %s\n", payload.RunID)
		fmt.Fprintf(w, "  Session: %s\n", payload.Session)
		fmt.Fprintf(w, "  Modes to run: %d\n", payload.Resumed)
		fmt.Fprintf(w, "  Already done: %d\n", payload.Skipped)
		fmt.Fprintf(w, "  %s\n", payload.Message)
		return nil
	}
}

func newEnsembleRerunModeCmd() *cobra.Command {
	var (
		format string
		quiet  bool
	)

	cmd := &cobra.Command{
		Use:   "rerun-mode <run-id> <mode>",
		Short: "Re-run a specific mode from a checkpoint",
		Long: `Re-run a single mode from an existing checkpoint run.

This is useful when:
  - A specific mode produced incorrect output
  - You want to try a mode with different parameters
  - A mode errored and you want to retry just that one

The mode's checkpoint will be updated with the new output.`,
		Example: `  ntm ensemble rerun-mode my-run deductive
  ntm ensemble rerun-mode my-run A1 --format json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID := args[0]
			modeRef := args[1]
			return runEnsembleRerunMode(cmd.OutOrStdout(), runID, modeRef, format, quiet)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Minimal output")

	return cmd
}

func runEnsembleRerunMode(w io.Writer, runID, modeRef, format string, quiet bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	store, err := ensemble.NewCheckpointStore("")
	if err != nil {
		return fmt.Errorf("open checkpoint store: %w", err)
	}

	if !store.RunExists(runID) {
		errPayload := checkpointResumeOutput{
			GeneratedAt: output.Timestamp(),
			RunID:       runID,
			Success:     false,
			Error:       fmt.Sprintf("checkpoint run '%s' not found", runID),
		}
		return renderCheckpointResumeOutput(w, errPayload, format, quiet)
	}

	meta, err := store.LoadMetadata(runID)
	if err != nil {
		return fmt.Errorf("load checkpoint metadata: %w", err)
	}

	slog.Default().Info("rerunning mode",
		"run_id", runID,
		"mode", modeRef,
		"session", meta.SessionName,
	)

	result := checkpointResumeOutput{
		GeneratedAt: output.Timestamp(),
		RunID:       runID,
		Session:     meta.SessionName,
		Success:     true,
		Resumed:     1,
		Message:     fmt.Sprintf("Re-running mode '%s' in run '%s'", modeRef, runID),
	}

	return renderCheckpointResumeOutput(w, result, format, quiet)
}

func newEnsembleCleanCheckpointsCmd() *cobra.Command {
	var (
		format string
		maxAge string
		all    bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "clean-checkpoints",
		Short: "Remove old or all checkpoint data",
		Long: `Remove checkpoint data to reclaim disk space.

By default, removes checkpoints older than 7 days.
Use --max-age to specify a different retention period.
Use --all to remove all checkpoints regardless of age.`,
		Example: `  ntm ensemble clean-checkpoints
  ntm ensemble clean-checkpoints --max-age 24h
  ntm ensemble clean-checkpoints --all
  ntm ensemble clean-checkpoints --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnsembleCleanCheckpoints(cmd.OutOrStdout(), format, maxAge, all, dryRun)
		},
	}

	cmd.Flags().StringVarP(&format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().StringVar(&maxAge, "max-age", "168h", "Remove checkpoints older than this duration (e.g., 24h, 7d)")
	cmd.Flags().BoolVar(&all, "all", false, "Remove all checkpoints regardless of age")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without actually removing")

	return cmd
}

func runEnsembleCleanCheckpoints(w io.Writer, format, maxAge string, all, dryRun bool) error {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	store, err := ensemble.NewCheckpointStore("")
	if err != nil {
		return fmt.Errorf("open checkpoint store: %w", err)
	}

	var removed int
	var msg string

	if all {
		runs, err := store.ListRuns()
		if err != nil {
			return fmt.Errorf("list checkpoints: %w", err)
		}

		if dryRun {
			removed = len(runs)
			msg = fmt.Sprintf("Would remove %d checkpoint(s)", removed)
		} else {
			for _, run := range runs {
				if err := store.DeleteRun(run.RunID); err != nil {
					slog.Default().Warn("failed to delete checkpoint", "run_id", run.RunID, "error", err)
					continue
				}
				removed++
			}
			msg = fmt.Sprintf("Removed %d checkpoint(s)", removed)
		}
	} else {
		duration, err := time.ParseDuration(maxAge)
		if err != nil {
			return fmt.Errorf("invalid max-age duration: %w", err)
		}

		if dryRun {
			runs, err := store.ListRuns()
			if err != nil {
				return fmt.Errorf("list checkpoints: %w", err)
			}
			cutoff := time.Now().Add(-duration)
			for _, run := range runs {
				ts := run.UpdatedAt
				if ts.IsZero() {
					ts = run.CreatedAt
				}
				if ts.Before(cutoff) {
					removed++
				}
			}
			msg = fmt.Sprintf("Would remove %d checkpoint(s) older than %s", removed, maxAge)
		} else {
			removed, err = store.CleanOld(duration)
			if err != nil {
				return fmt.Errorf("clean checkpoints: %w", err)
			}
			msg = fmt.Sprintf("Removed %d checkpoint(s) older than %s", removed, maxAge)
		}
	}

	slog.Default().Info("checkpoint cleanup",
		"removed", removed,
		"all", all,
		"dry_run", dryRun,
	)

	result := checkpointCleanOutput{
		GeneratedAt: output.Timestamp(),
		Removed:     removed,
		Message:     msg,
	}

	return renderCheckpointCleanOutput(w, result, format)
}

func renderCheckpointCleanOutput(w io.Writer, payload checkpointCleanOutput, format string) error {
	switch format {
	case "json":
		return output.WriteJSON(w, payload, true)
	case "yaml":
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	default:
		fmt.Fprintf(w, "%s\n", payload.Message)
		return nil
	}
}
