package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/bundle"
	"github.com/Dicklesworthstone/ntm/internal/redaction"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

func newSupportBundleCmd() *cobra.Command {
	var (
		outputPath   string
		formatStr    string
		since        string
		lines        int
		maxSizeMB    int
		redactMode   string
		noRedact     bool
		includeAll   bool
		sessionName  string
	)

	cmd := &cobra.Command{
		Use:   "support-bundle [session]",
		Short: "Generate a support bundle for debugging",
		Long: `Generate a support bundle containing diagnostic information.

The bundle includes session state, pane scrollback, configuration,
and logs with sensitive content redacted by default.

Examples:
  ntm support-bundle                           # Generate bundle for all sessions
  ntm support-bundle myproject                 # Generate bundle for specific session
  ntm support-bundle myproject -o debug.zip    # Custom output path
  ntm support-bundle --format=tar.gz           # Use tar.gz format
  ntm support-bundle --since=1h                # Only include last hour of content
  ntm support-bundle --lines=500               # Limit scrollback to 500 lines per pane
  ntm support-bundle --no-redact               # Skip redaction (use with caution)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				sessionName = args[0]
			}

			// Determine format
			format := bundle.FormatZip
			if formatStr == "tar.gz" || formatStr == "tgz" {
				format = bundle.FormatTarGz
			}

			// Determine output path
			if outputPath == "" {
				outputPath = bundle.SuggestOutputPath(sessionName, format)
			}

			// Parse since duration
			var sinceTime *time.Time
			if since != "" {
				duration, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since duration: %w", err)
				}
				t := time.Now().Add(-duration)
				sinceTime = &t
			}

			// Determine redaction mode
			redactConfig := redaction.DefaultConfig()
			if noRedact {
				redactConfig.Mode = redaction.ModeOff
			} else {
				switch redactMode {
				case "warn":
					redactConfig.Mode = redaction.ModeWarn
				case "redact", "":
					redactConfig.Mode = redaction.ModeRedact
				case "block":
					redactConfig.Mode = redaction.ModeBlock
				default:
					return fmt.Errorf("invalid --redact mode: %s (use: warn, redact, block)", redactMode)
				}
			}

			// Create generator config
			genConfig := bundle.GeneratorConfig{
				Session:         sessionName,
				OutputPath:      outputPath,
				Format:          format,
				NTMVersion:      Version,
				Since:           sinceTime,
				Lines:           lines,
				MaxSizeBytes:    int64(maxSizeMB) * 1024 * 1024,
				RedactionConfig: redactConfig,
			}

			// Create generator and collect content
			gen := bundle.NewGenerator(genConfig)

			// Collect session data
			if sessionName != "" {
				if err := collectSessionData(gen, sessionName, lines); err != nil {
					return fmt.Errorf("collecting session data: %w", err)
				}
			} else if includeAll {
				sessions, err := tmux.ListSessions()
				if err == nil {
					for _, s := range sessions {
						if err := collectSessionData(gen, s.Name, lines); err != nil {
							// Record error but continue
							gen.AddFile(
								fmt.Sprintf("errors/%s.txt", s.Name),
								[]byte(fmt.Sprintf("Error collecting session data: %v", err)),
								bundle.ContentTypeLogs,
								time.Now(),
							)
						}
					}
				}
			}

			// Collect config files
			if err := collectConfigFiles(gen); err != nil {
				// Non-fatal
				gen.AddFile(
					"errors/config.txt",
					[]byte(fmt.Sprintf("Error collecting config: %v", err)),
					bundle.ContentTypeLogs,
					time.Now(),
				)
			}

			// Generate the bundle
			result, err := gen.Generate()
			if err != nil {
				return fmt.Errorf("generating bundle: %w", err)
			}

			// Output result
			if jsonOutput {
				return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
					"success":           true,
					"path":              result.Path,
					"format":            result.Format,
					"file_count":        result.FileCount,
					"total_size":        result.TotalSize,
					"redaction_summary": result.RedactionSummary,
					"errors":            result.Errors,
					"warnings":          result.Warnings,
				})
			}

			t := theme.Current()
			fmt.Printf("%s\u2713%s Bundle created: %s\n", colorize(t.Success), "\033[0m", result.Path)
			fmt.Printf("  Format: %s\n", result.Format)
			fmt.Printf("  Files: %d\n", result.FileCount)
			fmt.Printf("  Size: %s\n", formatBytes(result.TotalSize))

			if result.RedactionSummary != nil && result.RedactionSummary.TotalFindings > 0 {
				fmt.Printf("  Redacted: %d findings in %d files\n",
					result.RedactionSummary.TotalFindings,
					result.RedactionSummary.FilesRedacted)
			}

			if len(result.Errors) > 0 {
				fmt.Printf("  Warnings: %d (see bundle for details)\n", len(result.Errors))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output file path (default: auto-generated)")
	cmd.Flags().StringVar(&formatStr, "format", "zip", "archive format: zip or tar.gz")
	cmd.Flags().StringVar(&since, "since", "", "include content from this duration ago (e.g., 1h, 24h)")
	cmd.Flags().IntVarP(&lines, "lines", "l", 1000, "max scrollback lines per pane (0 = unlimited)")
	cmd.Flags().IntVar(&maxSizeMB, "max-size", 100, "max bundle size in MB (0 = unlimited)")
	cmd.Flags().StringVar(&redactMode, "redact", "redact", "redaction mode: warn, redact, block")
	cmd.Flags().BoolVar(&noRedact, "no-redact", false, "disable redaction (use with caution)")
	cmd.Flags().BoolVar(&includeAll, "all", false, "include all sessions when no session specified")

	return cmd
}

// collectSessionData adds session data to the bundle.
func collectSessionData(gen *bundle.Generator, session string, lines int) error {
	if !tmux.SessionExists(session) {
		return fmt.Errorf("session %q does not exist", session)
	}

	// Get panes
	panes, err := tmux.GetPanes(session)
	if err != nil {
		return fmt.Errorf("listing panes: %w", err)
	}

	// Add session metadata
	metadata := fmt.Sprintf("Session: %s\nPanes: %d\nCaptured: %s\n",
		session, len(panes), time.Now().Format(time.RFC3339))
	if err := gen.AddFile(
		filepath.Join("sessions", session, "metadata.txt"),
		[]byte(metadata),
		bundle.ContentTypeMetadata,
		time.Now(),
	); err != nil {
		return err
	}

	// Capture scrollback for each pane
	for _, pane := range panes {
		target := fmt.Sprintf("%s:%d", session, pane.Index)
		content, err := tmux.CapturePaneOutput(target, lines)
		if err != nil {
			// Record error and continue
			gen.AddFile(
				filepath.Join("sessions", session, "errors", fmt.Sprintf("pane_%d.txt", pane.Index)),
				[]byte(fmt.Sprintf("Error capturing pane: %v", err)),
				bundle.ContentTypeLogs,
				time.Now(),
			)
			continue
		}

		paneName := fmt.Sprintf("pane_%d", pane.Index)
		if pane.Title != "" {
			paneName = pane.Title
		}

		if err := gen.AddScrollback(
			filepath.Join("sessions", session, paneName),
			content,
			lines,
		); err != nil {
			// Continue even if one pane fails
			continue
		}
	}

	return nil
}

// collectConfigFiles adds relevant config files to the bundle.
func collectConfigFiles(gen *bundle.Generator) error {
	// Check for .ntm directory
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	ntmDir := filepath.Join(home, ".ntm")
	if info, err := os.Stat(ntmDir); err == nil && info.IsDir() {
		// Add select config files (not all - avoid sensitive data)
		configFiles := []string{"config.toml", "palettes.yaml", "themes.yaml"}
		for _, name := range configFiles {
			path := filepath.Join(ntmDir, name)
			if data, err := os.ReadFile(path); err == nil {
				gen.AddFile(filepath.Join("config", name), data, bundle.ContentTypeConfig, time.Now())
			}
		}
	}

	return nil
}
