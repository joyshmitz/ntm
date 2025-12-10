package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents the main configuration
type Config struct {
	ProjectsBase string          `toml:"projects_base"`
	PaletteFile  string          `toml:"palette_file"` // Path to command_palette.md (optional)
	Agents       AgentConfig     `toml:"agents"`
	Palette      []PaletteCmd    `toml:"palette"`
	Tmux         TmuxConfig      `toml:"tmux"`
	AgentMail    AgentMailConfig `toml:"agent_mail"`
	Models       ModelsConfig    `toml:"models"`
}

// AgentConfig defines the commands for each agent type
type AgentConfig struct {
	Claude string `toml:"claude"`
	Codex  string `toml:"codex"`
	Gemini string `toml:"gemini"`
}

// PaletteCmd represents a command in the palette
type PaletteCmd struct {
	Key      string   `toml:"key"`
	Label    string   `toml:"label"`
	Prompt   string   `toml:"prompt"`
	Category string   `toml:"category,omitempty"`
	Tags     []string `toml:"tags,omitempty"`
}

// TmuxConfig holds tmux-specific settings
type TmuxConfig struct {
	DefaultPanes int    `toml:"default_panes"`
	PaletteKey   string `toml:"palette_key"`
}

// AgentMailConfig holds Agent Mail server settings
type AgentMailConfig struct {
	Enabled      bool   `toml:"enabled"`       // Master toggle
	URL          string `toml:"url"`           // Server endpoint
	Token        string `toml:"token"`         // Bearer token
	AutoRegister bool   `toml:"auto_register"` // Auto-register sessions as agents
	ProgramName  string `toml:"program_name"`  // Program identifier for registration
}

// ModelsConfig holds model alias configuration for each agent type
type ModelsConfig struct {
	DefaultClaude string            `toml:"default_claude"` // Default model for Claude
	DefaultCodex  string            `toml:"default_codex"`  // Default model for Codex
	DefaultGemini string            `toml:"default_gemini"` // Default model for Gemini
	Claude        map[string]string `toml:"claude"`         // Claude model aliases
	Codex         map[string]string `toml:"codex"`          // Codex model aliases
	Gemini        map[string]string `toml:"gemini"`         // Gemini model aliases
}

// DefaultModels returns the default model configuration with sensible aliases
func DefaultModels() ModelsConfig {
	return ModelsConfig{
		DefaultClaude: "claude-sonnet-4-20250514",
		DefaultCodex:  "gpt-4",
		DefaultGemini: "gemini-2.0-flash",
		Claude: map[string]string{
			"opus":      "claude-opus-4-20250514",
			"sonnet":    "claude-sonnet-4-20250514",
			"haiku":     "claude-haiku-3-20240307",
			"architect": "claude-opus-4-20250514",
			"fast":      "claude-sonnet-4-20250514",
		},
		Codex: map[string]string{
			"gpt4":  "gpt-4",
			"o1":    "o1",
			"turbo": "gpt-4-turbo",
			"max":   "gpt-5.1-codex-max",
		},
		Gemini: map[string]string{
			"pro":   "gemini-pro",
			"flash": "gemini-2.0-flash",
			"ultra": "gemini-ultra",
		},
	}
}

// GetModelName resolves a model alias to its full model name.
// Returns the alias itself if no mapping is found.
func (m *ModelsConfig) GetModelName(agentType, alias string) string {
	if alias == "" {
		// Return default if no alias specified
		switch strings.ToLower(agentType) {
		case "claude", "cc":
			return m.DefaultClaude
		case "codex", "cod":
			return m.DefaultCodex
		case "gemini", "gmi":
			return m.DefaultGemini
		}
		return ""
	}

	// Check agent-specific aliases
	var aliases map[string]string
	switch strings.ToLower(agentType) {
	case "claude", "cc":
		aliases = m.Claude
	case "codex", "cod":
		aliases = m.Codex
	case "gemini", "gmi":
		aliases = m.Gemini
	}

	if aliases != nil {
		if fullName, ok := aliases[strings.ToLower(alias)]; ok {
			return fullName
		}
	}

	// Return the alias as-is (assume it's a full model name)
	return alias
}

// DefaultPath returns the default config file path
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ntm", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ntm", "config.toml")
}

// DefaultProjectsBase returns the default projects directory
func DefaultProjectsBase() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Developer")
	}
	return "/data/projects"
}

// findPaletteMarkdown searches for a command_palette.md file in standard locations
// Search order: ~/.config/ntm/command_palette.md, then ./command_palette.md
func findPaletteMarkdown() string {
	// Check ~/.config/ntm/command_palette.md (user customization)
	configDir := filepath.Dir(DefaultPath())
	mdPath := filepath.Join(configDir, "command_palette.md")
	if _, err := os.Stat(mdPath); err == nil {
		return mdPath
	}

	// Check current working directory (project-specific)
	if cwd, err := os.Getwd(); err == nil {
		cwdPath := filepath.Join(cwd, "command_palette.md")
		if _, err := os.Stat(cwdPath); err == nil {
			return cwdPath
		}
	}

	return ""
}

// LoadPaletteFromMarkdown parses a command palette from markdown format.
// Format:
//
//	## Category Name
//	### command_key | Display Label
//	The prompt text (can be multiple lines)
//
// Lines starting with # (but not ## or ###) are treated as comments.
func LoadPaletteFromMarkdown(path string) ([]PaletteCmd, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var commands []PaletteCmd
	var currentCategory string
	var currentCmd *PaletteCmd
	var promptLines []string

	// Normalize line endings
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		// Check for category header: ## Category Name
		if strings.HasPrefix(line, "## ") {
			// Save previous command if exists
			if currentCmd != nil {
				currentCmd.Prompt = strings.TrimSpace(strings.Join(promptLines, "\n"))
				if currentCmd.Prompt != "" {
					commands = append(commands, *currentCmd)
				}
				currentCmd = nil
				promptLines = nil
			}
			currentCategory = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}

		// Check for command header: ### key | Label
		if strings.HasPrefix(line, "### ") {
			// Save previous command if exists
			if currentCmd != nil {
				currentCmd.Prompt = strings.TrimSpace(strings.Join(promptLines, "\n"))
				if currentCmd.Prompt != "" {
					commands = append(commands, *currentCmd)
				}
				promptLines = nil
			}

			// Parse key | label
			header := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			parts := strings.SplitN(header, "|", 2)
			if len(parts) != 2 {
				// Invalid format, skip this command
				currentCmd = nil
				continue
			}

			currentCmd = &PaletteCmd{
				Key:      strings.TrimSpace(parts[0]),
				Label:    strings.TrimSpace(parts[1]),
				Category: currentCategory,
			}
			continue
		}

		// Comment: starts with # but not ## or ###
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			continue
		}

		// Otherwise, it's prompt content
		if currentCmd != nil {
			promptLines = append(promptLines, line)
		}
	}

	// Don't forget the last command
	if currentCmd != nil {
		currentCmd.Prompt = strings.TrimSpace(strings.Join(promptLines, "\n"))
		if currentCmd.Prompt != "" {
			commands = append(commands, *currentCmd)
		}
	}

	return commands, nil
}

// DefaultAgentMailURL is the default Agent Mail server URL.
const DefaultAgentMailURL = "http://127.0.0.1:8765/mcp/"

// Default returns the default configuration.
// It tries to load the palette from a markdown file first, falling back to hardcoded defaults.
func Default() *Config {
	cfg := &Config{
		ProjectsBase: DefaultProjectsBase(),
		Agents: AgentConfig{
			Claude: `NODE_OPTIONS="--max-old-space-size=32768" ENABLE_BACKGROUND_TASKS=1 claude --dangerously-skip-permissions`,
			Codex:  `codex --dangerously-bypass-approvals-and-sandbox -m gpt-5.1-codex-max -c model_reasoning_effort="high" -c model_reasoning_summary_format=experimental --enable web_search_request`,
			Gemini: `gemini --yolo`,
		},
		Tmux: TmuxConfig{
			DefaultPanes: 10,
			PaletteKey:   "F6",
		},
		AgentMail: AgentMailConfig{
			Enabled:      true,
			URL:          DefaultAgentMailURL,
			Token:        "",
			AutoRegister: true,
			ProgramName:  "ntm",
		},
		Models: DefaultModels(),
	}

	// Try to load palette from markdown file
	if mdPath := findPaletteMarkdown(); mdPath != "" {
		if mdCmds, err := LoadPaletteFromMarkdown(mdPath); err == nil && len(mdCmds) > 0 {
			cfg.Palette = mdCmds
			return cfg
		}
	}

	// Fall back to hardcoded defaults
	cfg.Palette = defaultPaletteCommands()
	return cfg
}

func defaultPaletteCommands() []PaletteCmd {
	return []PaletteCmd{
		// Quick Actions
		{
			Key:      "fresh_review",
			Label:    "Fresh Eyes Review",
			Category: "Quick Actions",
			Prompt: `Take a step back and carefully reread the most recent code changes with fresh eyes.
Look for any obvious bugs, logical errors, or confusing patterns.
Fix anything you spot without waiting for direction.`,
		},
		{
			Key:      "fix_bug",
			Label:    "Fix the Bug",
			Category: "Quick Actions",
			Prompt: `Focus on diagnosing the root cause of the reported issue.
Don't just patch symptoms - find and fix the underlying problem.
Implement a real fix, not a workaround.`,
		},
		{
			Key:      "git_commit",
			Label:    "Commit Changes",
			Category: "Quick Actions",
			Prompt: `Commit all changed files with detailed, meaningful commit messages.
Group related changes logically. Push to the remote branch.`,
		},
		{
			Key:      "run_tests",
			Label:    "Run All Tests",
			Category: "Quick Actions",
			Prompt:   `Run the full test suite and fix any failing tests.`,
		},

		// Code Quality
		{
			Key:      "refactor",
			Label:    "Refactor Code",
			Category: "Code Quality",
			Prompt: `Review the current code for opportunities to improve:
- Extract reusable functions
- Simplify complex logic
- Improve naming
- Remove duplication
Make incremental improvements while preserving functionality.`,
		},
		{
			Key:      "add_types",
			Label:    "Add Type Annotations",
			Category: "Code Quality",
			Prompt: `Add comprehensive type annotations to the codebase.
Focus on function signatures, class attributes, and complex data structures.
Use generics where appropriate.`,
		},
		{
			Key:      "add_docs",
			Label:    "Add Documentation",
			Category: "Code Quality",
			Prompt: `Add comprehensive docstrings and comments to the codebase.
Document public APIs, complex algorithms, and non-obvious behavior.
Keep docs concise but complete.`,
		},

		// Coordination
		{
			Key:      "status_update",
			Label:    "Status Update",
			Category: "Coordination",
			Prompt: `Provide a brief status update:
1. What you just completed
2. What you're currently working on
3. Any blockers or questions
4. What you plan to do next`,
		},
		{
			Key:      "handoff",
			Label:    "Prepare Handoff",
			Category: "Coordination",
			Prompt: `Prepare a handoff document for another agent:
- Current state of the code
- What's working and what isn't
- Open issues and edge cases
- Recommended next steps`,
		},
		{
			Key:      "sync",
			Label:    "Sync with Main",
			Category: "Coordination",
			Prompt: `Pull latest changes from main branch and resolve any conflicts.
Run tests after merging to ensure nothing is broken.`,
		},

		// Investigation
		{
			Key:      "explain",
			Label:    "Explain This Code",
			Category: "Investigation",
			Prompt: `Explain how the current code works in detail.
Walk through the control flow, data transformations, and key design decisions.
Note any potential issues or areas for improvement.`,
		},
		{
			Key:      "find_issue",
			Label:    "Find the Issue",
			Category: "Investigation",
			Prompt: `Investigate the codebase to find potential issues:
- Logic errors
- Edge cases not handled
- Performance problems
- Security concerns
Report findings with specific file locations and line numbers.`,
		},
	}
}

// Load loads configuration from a file.
// Palette loading precedence:
//  1. Explicit palette_file from TOML config
//  2. Auto-discovered command_palette.md (~/.config/ntm/ or ./command_palette.md)
//  3. [[palette]] entries from TOML config
//  4. Hardcoded defaults
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Apply defaults for missing values
	if cfg.ProjectsBase == "" {
		cfg.ProjectsBase = DefaultProjectsBase()
	}
	if cfg.Agents.Claude == "" {
		cfg.Agents.Claude = Default().Agents.Claude
	}
	if cfg.Agents.Codex == "" {
		cfg.Agents.Codex = Default().Agents.Codex
	}
	if cfg.Agents.Gemini == "" {
		cfg.Agents.Gemini = Default().Agents.Gemini
	}
	if cfg.Tmux.DefaultPanes == 0 {
		cfg.Tmux.DefaultPanes = 10
	}
	if cfg.Tmux.PaletteKey == "" {
		cfg.Tmux.PaletteKey = "F6"
	}

	// Apply AgentMail defaults
	if cfg.AgentMail.URL == "" {
		cfg.AgentMail.URL = DefaultAgentMailURL
	}
	if cfg.AgentMail.ProgramName == "" {
		cfg.AgentMail.ProgramName = "ntm"
	}

	// Environment variable overrides for AgentMail
	if url := os.Getenv("AGENT_MAIL_URL"); url != "" {
		cfg.AgentMail.URL = url
	}
	if token := os.Getenv("AGENT_MAIL_TOKEN"); token != "" {
		cfg.AgentMail.Token = token
	}
	if enabled := os.Getenv("AGENT_MAIL_ENABLED"); enabled != "" {
		cfg.AgentMail.Enabled = enabled == "1" || enabled == "true"
	}

	// Try to load palette from markdown file
	// This takes precedence over TOML [[palette]] entries
	mdPath := cfg.PaletteFile
	if mdPath == "" {
		mdPath = findPaletteMarkdown()
	} else {
		// Expand ~/ in explicit path (e.g., ~/foo -> /home/user/foo)
		if strings.HasPrefix(mdPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				mdPath = filepath.Join(home, mdPath[2:])
			}
		}
	}

	if mdPath != "" {
		if mdCmds, err := LoadPaletteFromMarkdown(mdPath); err == nil && len(mdCmds) > 0 {
			cfg.Palette = mdCmds
			return &cfg, nil
		}
	}

	// If no palette commands from TOML, use defaults
	if len(cfg.Palette) == 0 {
		cfg.Palette = defaultPaletteCommands()
	}

	return &cfg, nil
}

// CreateDefault creates a default config file
func CreateDefault() (string, error) {
	path := DefaultPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}

	// Check if file already exists
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("config file already exists: %s", path)
	}

	// Write default config
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if err := Print(Default(), f); err != nil {
		return "", err
	}

	return path, nil
}

// Print writes config to a writer in TOML format
func Print(cfg *Config, w io.Writer) error {
	// Write a nicely formatted config file
	fmt.Fprintln(w, "# NTM (Named Tmux Manager) Configuration")
	fmt.Fprintln(w, "# https://github.com/Dicklesworthstone/ntm")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "# Base directory for projects\n")
	fmt.Fprintf(w, "projects_base = %q\n", cfg.ProjectsBase)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "# Path to command palette markdown file (optional)")
	fmt.Fprintln(w, "# If set, loads palette commands from this file instead of [[palette]] entries below")
	fmt.Fprintln(w, "# Searched automatically: ~/.config/ntm/command_palette.md, ./command_palette.md")
	if cfg.PaletteFile != "" {
		fmt.Fprintf(w, "palette_file = %q\n", cfg.PaletteFile)
	} else {
		fmt.Fprintln(w, "# palette_file = \"~/.config/ntm/command_palette.md\"")
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "[agents]")
	fmt.Fprintln(w, "# Commands used to launch each agent type")
	fmt.Fprintf(w, "claude = %q\n", cfg.Agents.Claude)
	fmt.Fprintf(w, "codex = %q\n", cfg.Agents.Codex)
	fmt.Fprintf(w, "gemini = %q\n", cfg.Agents.Gemini)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "[tmux]")
	fmt.Fprintln(w, "# Tmux-specific settings")
	fmt.Fprintf(w, "default_panes = %d\n", cfg.Tmux.DefaultPanes)
	fmt.Fprintf(w, "palette_key = %q\n", cfg.Tmux.PaletteKey)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "[agent_mail]")
	fmt.Fprintln(w, "# Agent Mail server settings for multi-agent coordination")
	fmt.Fprintln(w, "# Environment variables: AGENT_MAIL_URL, AGENT_MAIL_TOKEN, AGENT_MAIL_ENABLED")
	fmt.Fprintf(w, "enabled = %t\n", cfg.AgentMail.Enabled)
	fmt.Fprintf(w, "url = %q\n", cfg.AgentMail.URL)
	if cfg.AgentMail.Token != "" {
		fmt.Fprintf(w, "token = %q\n", cfg.AgentMail.Token)
	} else {
		fmt.Fprintln(w, "# token = \"\"  # Or set AGENT_MAIL_TOKEN env var")
	}
	fmt.Fprintf(w, "auto_register = %t\n", cfg.AgentMail.AutoRegister)
	fmt.Fprintf(w, "program_name = %q\n", cfg.AgentMail.ProgramName)
	fmt.Fprintln(w)

	// Write models configuration
	fmt.Fprintln(w, "[models]")
	fmt.Fprintln(w, "# Default models when no specifier given")
	fmt.Fprintf(w, "default_claude = %q\n", cfg.Models.DefaultClaude)
	fmt.Fprintf(w, "default_codex = %q\n", cfg.Models.DefaultCodex)
	fmt.Fprintf(w, "default_gemini = %q\n", cfg.Models.DefaultGemini)
	fmt.Fprintln(w)

	// Write Claude model aliases
	fmt.Fprintln(w, "[models.claude]")
	fmt.Fprintln(w, "# Claude model aliases (e.g., --cc=2:opus)")
	for alias, fullName := range cfg.Models.Claude {
		fmt.Fprintf(w, "%s = %q\n", alias, fullName)
	}
	fmt.Fprintln(w)

	// Write Codex model aliases
	fmt.Fprintln(w, "[models.codex]")
	fmt.Fprintln(w, "# Codex model aliases (e.g., --cod=2:max)")
	for alias, fullName := range cfg.Models.Codex {
		fmt.Fprintf(w, "%s = %q\n", alias, fullName)
	}
	fmt.Fprintln(w)

	// Write Gemini model aliases
	fmt.Fprintln(w, "[models.gemini]")
	fmt.Fprintln(w, "# Gemini model aliases (e.g., --gmi=1:flash)")
	for alias, fullName := range cfg.Models.Gemini {
		fmt.Fprintf(w, "%s = %q\n", alias, fullName)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "# Command Palette entries")
	fmt.Fprintln(w, "# Add your own prompts here")
	fmt.Fprintln(w)

	// Group by category, preserving order of first occurrence
	categories := make(map[string][]PaletteCmd)
	var categoryOrder []string
	seenCategories := make(map[string]bool)

	for _, cmd := range cfg.Palette {
		cat := cmd.Category
		if cat == "" {
			cat = "General"
		}
		categories[cat] = append(categories[cat], cmd)
		if !seenCategories[cat] {
			seenCategories[cat] = true
			categoryOrder = append(categoryOrder, cat)
		}
	}

	// Write categories in order of first occurrence
	for _, cat := range categoryOrder {
		cmds := categories[cat]
		fmt.Fprintf(w, "# %s\n", cat)
		for _, cmd := range cmds {
			fmt.Fprintln(w, "[[palette]]")
			fmt.Fprintf(w, "key = %q\n", cmd.Key)
			fmt.Fprintf(w, "label = %q\n", cmd.Label)
			if cmd.Category != "" {
				fmt.Fprintf(w, "category = %q\n", cmd.Category)
			}
			// Use multi-line string for prompts
			fmt.Fprintf(w, "prompt = \"\"\"\n%s\"\"\"\n", cmd.Prompt)
			fmt.Fprintln(w)
		}
	}

	return nil
}

// GetProjectDir returns the project directory for a session
func (c *Config) GetProjectDir(session string) string {
	// Expand ~/ in path (e.g., ~/Developer -> /home/user/Developer)
	base := c.ProjectsBase
	if strings.HasPrefix(base, "~/") {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, base[2:])
	}
	return filepath.Join(base, session)
}
