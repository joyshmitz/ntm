package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
	"github.com/Dicklesworthstone/ntm/internal/output"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// compareOptions holds CLI flags for ensemble compare.
type compareOptions struct {
	Format  string
	Session string
	Verbose bool
}

// compareOutput is the JSON/YAML output structure.
type compareOutput struct {
	GeneratedAt string                     `json:"generated_at" yaml:"generated_at"`
	RunA        string                     `json:"run_a" yaml:"run_a"`
	RunB        string                     `json:"run_b" yaml:"run_b"`
	Summary     string                     `json:"summary" yaml:"summary"`
	Result      *ensemble.ComparisonResult `json:"result,omitempty" yaml:"result,omitempty"`
	Error       string                     `json:"error,omitempty" yaml:"error,omitempty"`
}

func newEnsembleCompareCmd() *cobra.Command {
	opts := compareOptions{
		Format: "text",
	}

	cmd := &cobra.Command{
		Use:   "compare <run1> <run2>",
		Short: "Compare two ensemble runs",
		Long: `Compare two ensemble runs side-by-side to see mode, finding, and synthesis differences.

The comparison uses stable finding IDs (hashes) for deterministic matching,
so findings from the same mode with the same text will be correctly aligned
even if order or other attributes differ.

Output sections:
  - Mode Changes: modes added, removed, or unchanged between runs
  - Finding Changes: new, missing, changed, and unchanged findings
  - Conclusion Changes: thesis and synthesis differences
  - Contribution Changes: mode contribution score deltas and rank changes

Formats:
  --format=text (default) - Human-readable report
  --format=json           - Machine-readable JSON
  --format=yaml           - YAML format`,
		Example: `  ntm ensemble compare session1 session2
  ntm ensemble compare run-20240101 run-20240102 --format=json
  ntm ensemble compare mysession-v1 mysession-v2 --verbose`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnsembleCompare(cmd.OutOrStdout(), args[0], args[1], opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Show detailed diff including unchanged items")
	cmd.ValidArgsFunction = completeSessionArgs
	return cmd
}

func runEnsembleCompare(w io.Writer, runAID, runBID string, opts compareOptions) error {
	format := strings.ToLower(strings.TrimSpace(opts.Format))
	if format == "" {
		format = "text"
	}
	if jsonOutput {
		format = "json"
	}

	slog.Info("comparing ensemble runs",
		"run_a", runAID,
		"run_b", runBID,
		"format", format,
	)

	if err := tmux.EnsureInstalled(); err != nil {
		return writeCompareError(w, runAID, runBID, err, format)
	}

	// Load run A
	inputA, err := loadCompareInput(runAID)
	if err != nil {
		return writeCompareError(w, runAID, runBID, fmt.Errorf("load run A (%s): %w", runAID, err), format)
	}

	// Load run B
	inputB, err := loadCompareInput(runBID)
	if err != nil {
		return writeCompareError(w, runAID, runBID, fmt.Errorf("load run B (%s): %w", runBID, err), format)
	}

	slog.Info("loaded ensemble runs for comparison",
		"run_a", runAID,
		"run_a_modes", len(inputA.ModeIDs),
		"run_a_outputs", len(inputA.Outputs),
		"run_b", runBID,
		"run_b_modes", len(inputB.ModeIDs),
		"run_b_outputs", len(inputB.Outputs),
	)

	// Compare
	result := ensemble.Compare(*inputA, *inputB)

	slog.Info("comparison complete",
		"run_a", runAID,
		"run_b", runBID,
		"summary", result.Summary,
		"modes_added", result.ModeDiff.AddedCount,
		"modes_removed", result.ModeDiff.RemovedCount,
		"findings_new", result.FindingsDiff.NewCount,
		"findings_missing", result.FindingsDiff.MissingCount,
		"findings_changed", result.FindingsDiff.ChangedCount,
	)

	return writeCompareResult(w, result, opts, format)
}

// loadCompareInput loads an ensemble session and constructs a CompareInput.
func loadCompareInput(runID string) (*ensemble.CompareInput, error) {
	// Try to load as session
	if !tmux.SessionExists(runID) {
		return nil, fmt.Errorf("session '%s' not found", runID)
	}

	state, err := ensemble.LoadSession(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no ensemble running in session '%s'", runID)
		}
		return nil, fmt.Errorf("load session: %w", err)
	}

	// Extract mode IDs from assignments
	modeIDs := make([]string, 0, len(state.Assignments))
	for _, a := range state.Assignments {
		modeIDs = append(modeIDs, a.ModeID)
	}
	sort.Strings(modeIDs)

	// Capture outputs
	capture := ensemble.NewOutputCapture(tmux.DefaultClient)
	captured, err := capture.CaptureAll(state)
	if err != nil {
		slog.Warn("failed to capture outputs for comparison",
			"session", runID,
			"error", err,
		)
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

	// Set up provenance tracker
	provenance := ensemble.NewProvenanceTracker(state.Question, modeIDs)

	// Try to get contributions
	var contributions *ensemble.ContributionReport
	if len(outputs) > 0 {
		tracker := ensemble.NewContributionTracker()
		ensemble.TrackOriginalFindings(tracker, outputs)
		contributions = tracker.GenerateReport()
	}

	// Try to get synthesis output
	var synthesisOutput string
	if len(outputs) > 0 {
		synth, synthErr := ensemble.NewSynthesizer(ensemble.DefaultSynthesisConfig())
		if synthErr == nil {
			input := &ensemble.SynthesisInput{
				Outputs:          outputs,
				OriginalQuestion: state.Question,
				Config:           synth.Config,
				Provenance:       provenance,
			}
			result, synthErr := synth.Synthesize(input)
			if synthErr == nil {
				synthesisOutput = result.Summary
			}
		}
	}

	slog.Debug("loaded compare input",
		"run_id", runID,
		"modes", len(modeIDs),
		"outputs", len(outputs),
		"has_contributions", contributions != nil,
		"has_synthesis", synthesisOutput != "",
	)

	return &ensemble.CompareInput{
		RunID:           runID,
		ModeIDs:         modeIDs,
		Outputs:         outputs,
		Provenance:      provenance,
		Contributions:   contributions,
		SynthesisOutput: synthesisOutput,
	}, nil
}

func writeCompareResult(w io.Writer, result *ensemble.ComparisonResult, opts compareOptions, format string) error {
	switch format {
	case "json":
		out := compareOutput{
			GeneratedAt: output.Timestamp().Format(time.RFC3339),
			RunA:        result.RunA,
			RunB:        result.RunB,
			Summary:     result.Summary,
			Result:      result,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)

	case "yaml":
		out := compareOutput{
			GeneratedAt: output.Timestamp().Format(time.RFC3339),
			RunA:        result.RunA,
			RunB:        result.RunB,
			Summary:     result.Summary,
			Result:      result,
		}
		return yaml.NewEncoder(w).Encode(out)

	default: // text
		formatted := ensemble.FormatComparison(result)
		_, err := fmt.Fprintln(w, formatted)
		return err
	}
}

func writeCompareError(w io.Writer, runAID, runBID string, err error, format string) error {
	switch format {
	case "json":
		out := compareOutput{
			GeneratedAt: output.Timestamp().Format(time.RFC3339),
			RunA:        runAID,
			RunB:        runBID,
			Error:       err.Error(),
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return err

	case "yaml":
		out := compareOutput{
			GeneratedAt: output.Timestamp().Format(time.RFC3339),
			RunA:        runAID,
			RunB:        runBID,
			Error:       err.Error(),
		}
		_ = yaml.NewEncoder(w).Encode(out)
		return err

	default:
		return err
	}
}
