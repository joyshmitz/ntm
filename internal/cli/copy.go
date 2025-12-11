package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/codeblock"
	"github.com/Dicklesworthstone/ntm/internal/palette"
	"github.com/Dicklesworthstone/ntm/internal/tmux"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
	"github.com/spf13/cobra"
)

func newCopyCmd() *cobra.Command {
	var (
		lines   int
		pattern string
		allFlag bool
		ccFlag  bool
		codFlag bool
		gmiFlag bool
		codeFlag bool
	)

	cmd := &cobra.Command{
		Use:     "copy [session-name]",
		Aliases: []string{"cp", "yank"},
		Short:   "Copy pane output to clipboard",
		Long: `Copy the output from one or more panes to the system clipboard.

By default, captures the last 1000 lines from each pane.
Use filters to target specific agent types.

Examples:
  ntm copy myproject            # Copy from current/selected pane
  ntm copy myproject --all      # Copy from all panes
  ntm copy myproject --cc       # Copy from Claude panes only
  ntm copy myproject -l 500     # Copy last 500 lines
  ntm copy myproject --code     # Copy only code blocks
  ntm copy myproject --pattern "ERROR" # Copy lines matching regex`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var session string
			if len(args) > 0 {
				session = args[0]
			}

			filter := AgentFilter{
				All:    allFlag,
				Claude: ccFlag,
				Codex:  codFlag,
				Gemini: gmiFlag,
			}

			options := CopyOptions{
				Lines:   lines,
				Pattern: pattern,
				Code:    codeFlag,
			}

			return runCopy(cmd.OutOrStdout(), session, filter, options)
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "l", 1000, "Number of lines to capture")
	cmd.Flags().StringVarP(&pattern, "pattern", "p", "", "Regex pattern to filter lines")
	cmd.Flags().BoolVar(&codeFlag, "code", false, "Copy only code blocks")
	cmd.Flags().BoolVar(&allFlag, "all", false, "Copy from all panes")
	cmd.Flags().BoolVar(&ccFlag, "cc", false, "Copy from Claude panes")
	cmd.Flags().BoolVar(&codFlag, "cod", false, "Copy from Codex panes")
	cmd.Flags().BoolVar(&gmiFlag, "gmi", false, "Copy from Gemini panes")

	return cmd
}

// AgentFilter specifies which agent types to target
type AgentFilter struct {
	All    bool
	Claude bool
	Codex  bool
	Gemini bool
}

func (f AgentFilter) IsEmpty() bool {
	return !f.All && !f.Claude && !f.Codex && !f.Gemini
}

func (f AgentFilter) Matches(agentType tmux.AgentType) bool {
	if f.All {
		return true
	}
	switch agentType {
	case tmux.AgentClaude:
		return f.Claude
	case tmux.AgentCodex:
		return f.Codex
	case tmux.AgentGemini:
		return f.Gemini
	default:
		return false
	}
}

// CopyOptions defines options for the copy command
type CopyOptions struct {
	Lines   int
	Pattern string
	Code    bool
}

func runCopy(w io.Writer, session string, filter AgentFilter, opts CopyOptions) error {
	if err := tmux.EnsureInstalled(); err != nil {
		return err
	}

	t := theme.Current()

	// Determine target session
	if session == "" {
		if tmux.InTmux() {
			session = tmux.GetCurrentSession()
		} else {
			if !IsInteractive(w) {
				return fmt.Errorf("non-interactive environment: session name is required")
			}
			sessions, err := tmux.ListSessions()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				return fmt.Errorf("no tmux sessions found")
			}

			selected, err := palette.RunSessionSelector(sessions)
			if err != nil {
				return err
			}
			if selected == "" {
				return nil
			}
			session = selected
		}
	}

	if !tmux.SessionExists(session) {
		return fmt.Errorf("session '%s' not found", session)
	}

	panes, err := tmux.GetPanes(session)
	if err != nil {
		return err
	}

	// Filter panes
	var targetPanes []tmux.Pane
	if filter.IsEmpty() {
		// No filter: copy from active pane or first pane
		for _, p := range panes {
			if p.Active {
				targetPanes = []tmux.Pane{p}
				break
			}
		}
		if len(targetPanes) == 0 && len(panes) > 0 {
			targetPanes = []tmux.Pane{panes[0]}
		}
	} else {
		for _, p := range panes {
			if filter.Matches(p.Type) {
				targetPanes = append(targetPanes, p)
			}
		}
	}

	if len(targetPanes) == 0 {
		return fmt.Errorf("no matching panes found")
	}

	// Compile regex if provided
	var regex *regexp.Regexp
	if opts.Pattern != "" {
		var err error
		regex, err = regexp.Compile(opts.Pattern)
		if err != nil {
			return fmt.Errorf("invalid pattern regex: %w", err)
		}
	}

	// Capture output from all target panes
	var outputs []string
	for _, p := range targetPanes {
		output, err := tmux.CapturePaneOutput(p.ID, opts.Lines)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to capture pane %d: %v\n", p.Index, err)
			continue
		}

		// Filter content
		if opts.Code {
			// Extract code blocks
			blocks := codeblock.ExtractFromText(output)
			var blockContents []string
			for _, b := range blocks {
				// We include the language hint in the copied block for clarity?
				// Or just the content? The goal says "extracts code blocks only".
				// Let's include the fence for context if there are multiple.
				// But maybe users just want the code to paste into a file.
				// If multiple blocks, separating them is good.
				if b.Content != "" {
					blockContents = append(blockContents, b.Content)
				}
			}
			if len(blockContents) > 0 {
				output = strings.Join(blockContents, "\n\n")
			} else {
				output = "" // No code blocks found
			}
		} else if regex != nil {
			// Filter lines by regex
			lines := strings.Split(output, "\n")
			var filtered []string
			for _, line := range lines {
				if regex.MatchString(line) {
					filtered = append(filtered, line)
				}
			}
			output = strings.Join(filtered, "\n")
		}

		if strings.TrimSpace(output) == "" {
			continue
		}

		// Add header for each pane if copying multiple or using filters
		// (Always adding header helps context, but if user wants raw code, header might annoy.
		// If --code is used, maybe skip header? Or keep it?)
		// Let's keep it consistent with previous behavior, but maybe suppress if only 1 pane and no filters?
		// Existing behavior: always added header.
		// "ntm copy myproject:1 --code" -> copies: "function authenticate(token) { ... }"
		// If I add header, it breaks direct pasting into code editor.
		// If --code is active, I should probably SKIP the header.
		if opts.Code {
			outputs = append(outputs, output)
		} else {
			header := fmt.Sprintf("═══ %s (pane %d) ═══", p.Title, p.Index)
			outputs = append(outputs, header, output, "")
		}
	}

	if len(outputs) == 0 {
		return fmt.Errorf("no content captured (check filters)")
	}

	combined := strings.Join(outputs, "\n")

	// Copy to clipboard
	if err := copyToClipboard(combined); err != nil {
		return fmt.Errorf("failed to copy to clipboard: %w", err)
	}

	lineCount := strings.Count(combined, "\n")
	fmt.Printf("%s✓%s Copied %d lines from %d pane(s) to clipboard\n",
		colorize(t.Success), colorize(t.Text), lineCount, len(targetPanes))

	return nil
}

// copyToClipboard copies text to the system clipboard
func copyToClipboard(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		// Try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			// Wayland support
			cmd = exec.Command("wl-copy")
		} else {
			// Check for WSL
			if _, err := exec.LookPath("clip.exe"); err == nil {
				cmd = exec.Command("clip.exe")
			} else {
				return fmt.Errorf("no clipboard utility found (install xclip, xsel, or wl-copy)")
			}
		}
	default:
		// Fallback for Windows if built natively, though NTM is primarily POSIX
		if runtime.GOOS == "windows" {
			cmd = exec.Command("clip")
		} else {
			return fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
		}
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
