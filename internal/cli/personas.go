package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Dicklesworthstone/ntm/internal/persona"
	"github.com/Dicklesworthstone/ntm/internal/tui/theme"
)

func newPersonasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "personas",
		Short: "Manage agent personas",
		Long: `List and inspect available agent personas.

Personas define agent characteristics including agent type, model,
system prompts, and behavioral settings.

Persona sources (later overrides earlier):
  1. Built-in: Compiled into ntm
  2. User: ~/.config/ntm/personas.toml
  3. Project: .ntm/personas.toml

Examples:
  ntm personas list              # List all personas
  ntm personas list --json       # JSON output
  ntm personas show architect    # Show persona details
  ntm personas show architect --json`,
	}

	cmd.AddCommand(
		newPersonasListCmd(),
		newPersonasShowCmd(),
	)

	return cmd
}

func newPersonasListCmd() *cobra.Command {
	var (
		filterAgent string
		filterTag   string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available personas",
		Long:  `List all available personas from built-in, user, and project sources.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPersonasList(filterAgent, filterTag)
		},
	}

	cmd.Flags().StringVar(&filterAgent, "agent", "", "Filter by agent type (claude, codex, gemini)")
	cmd.Flags().StringVar(&filterTag, "tag", "", "Filter by tag")

	return cmd
}

func runPersonasList(filterAgent, filterTag string) error {
	// Get project directory (current working directory)
	cwd, _ := os.Getwd()

	registry, err := persona.LoadRegistry(cwd)
	if err != nil {
		return err
	}

	personas := registry.List()

	// Sort by name
	sort.Slice(personas, func(i, j int) bool {
		return personas[i].Name < personas[j].Name
	})

	// Apply filters
	filtered := make([]*persona.Persona, 0, len(personas))
	for _, p := range personas {
		// Agent type filter
		if filterAgent != "" {
			if !strings.EqualFold(p.AgentTypeFlag(), filterAgent) &&
				!strings.EqualFold(p.AgentType, filterAgent) {
				continue
			}
		}

		// Tag filter
		if filterTag != "" {
			hasTag := false
			for _, tag := range p.Tags {
				if strings.EqualFold(tag, filterTag) {
					hasTag = true
					break
				}
			}
			if !hasTag {
				continue
			}
		}

		filtered = append(filtered, p)
	}

	if jsonOutput {
		// Ensure profile sets is never null in JSON output
		sets := registry.ListSets()
		if sets == nil {
			sets = []*persona.PersonaSet{}
		}
		// Include both personas and profile sets in JSON output
		output := struct {
			Personas    []*persona.Persona    `json:"personas"`
			ProfileSets []*persona.PersonaSet `json:"profile_sets"`
		}{
			Personas:    filtered,
			ProfileSets: sets,
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	// Count sources
	builtinCount := len(persona.BuiltinPersonas())
	userCount := 0
	projectCount := 0
	// These counts are approximations since we don't track source in registry

	t := theme.Current()

	if len(filtered) == 0 {
		fmt.Println("No personas found matching filters.")
		return nil
	}

	// Print header
	fmt.Printf("%s%-14s %-8s %-8s %s%s\n",
		colorize(t.Primary), "NAME", "AGENT", "MODEL", "DESCRIPTION", "\033[0m")
	fmt.Println(strings.Repeat("─", 70))

	// Print personas
	for _, p := range filtered {
		desc := truncateRunes(p.Description, 32, "...")

		model := p.Model
		if model == "" {
			model = "(default)"
		}
		model = truncateRunes(model, 6, "..")

		fmt.Printf("%-14s %-8s %-8s %s\n",
			p.Name,
			p.AgentTypeFlag(),
			model,
			desc,
		)
	}

	fmt.Println(strings.Repeat("─", 70))
	fmt.Printf("Total: %d personas (%d built-in)\n", len(filtered), builtinCount)

	// Show profile sets
	sets := registry.ListSets()
	if len(sets) > 0 {
		fmt.Println()
		fmt.Printf("%sPROFILE SETS:%s\n", colorize(t.Primary), "\033[0m")
		sort.Slice(sets, func(i, j int) bool {
			return sets[i].Name < sets[j].Name
		})
		for _, s := range sets {
			members := truncateRunes(strings.Join(s.Personas, ", "), 37, "...")
			desc := s.Description
			if desc == "" {
				desc = members
			}
			fmt.Printf("  %-16s %s\n", s.Name, desc)
		}
	}

	// Show source hint if user/project files exist
	if _, err := os.Stat(persona.DefaultUserPath()); err == nil {
		userCount++
	}
	projectPath := ".ntm/personas.toml"
	if _, err := os.Stat(projectPath); err == nil {
		projectCount++
	}
	if userCount > 0 || projectCount > 0 {
		sources := []string{}
		if userCount > 0 {
			sources = append(sources, "user")
		}
		if projectCount > 0 {
			sources = append(sources, "project")
		}
		fmt.Printf("Custom sources: %s\n", strings.Join(sources, ", "))
	}

	return nil
}

func newPersonasShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show persona details",
		Long:  `Show detailed information about a specific persona.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPersonasShow(args[0])
		},
	}

	return cmd
}

func runPersonasShow(name string) error {
	cwd, _ := os.Getwd()

	registry, err := persona.LoadRegistry(cwd)
	if err != nil {
		return err
	}

	p, ok := registry.Get(name)
	if !ok {
		if jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"error": fmt.Sprintf("persona %q not found", name),
			})
		}
		return fmt.Errorf("persona %q not found", name)
	}

	if jsonOutput {
		// Add source field for JSON output
		type personaWithSource struct {
			*persona.Persona
			Source string `json:"source"`
		}
		output := personaWithSource{
			Persona: p,
			Source:  determineSource(p.Name),
		}
		return json.NewEncoder(os.Stdout).Encode(output)
	}

	t := theme.Current()

	// Header
	fmt.Printf("%sPersona: %s%s\n", colorize(t.Primary), p.Name, "\033[0m")
	fmt.Println(strings.Repeat("─", 60))

	// Basic info
	fmt.Printf("Agent Type:   %s\n", p.AgentType)
	fmt.Printf("Model:        %s\n", valueOrDefault(p.Model, "(default)"))

	if p.Temperature != nil {
		fmt.Printf("Temperature:  %.1f\n", *p.Temperature)
	}

	if p.Description != "" {
		fmt.Printf("Description:  %s\n", p.Description)
	}

	if len(p.Tags) > 0 {
		fmt.Printf("Tags:         %s\n", strings.Join(p.Tags, ", "))
	}

	fmt.Printf("Source:       %s\n", determineSource(p.Name))

	// System prompt
	if p.SystemPrompt != "" {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("%sSystem Prompt:%s\n\n", colorize(t.Primary), "\033[0m")

		// Indent and wrap the system prompt
		lines := strings.Split(p.SystemPrompt, "\n")
		for _, line := range lines {
			fmt.Printf("  %s\n", line)
		}
	}

	// Context files
	if len(p.ContextFiles) > 0 {
		fmt.Println(strings.Repeat("─", 60))
		fmt.Printf("%sContext Files:%s\n", colorize(t.Primary), "\033[0m")
		for _, cf := range p.ContextFiles {
			fmt.Printf("  - %s\n", cf)
		}
	}

	fmt.Println(strings.Repeat("─", 60))

	return nil
}

// determineSource returns the source of a persona (built-in, user, or project)
func determineSource(name string) string {
	// Check if it's a built-in persona
	for _, bp := range persona.BuiltinPersonas() {
		if strings.EqualFold(bp.Name, name) {
			// Could be overridden, so check for user/project files
			// For simplicity, we'll report based on file existence
			cwd, _ := os.Getwd()
			projectPath := cwd + "/.ntm/personas.toml"
			if cfg, err := persona.LoadFromFile(projectPath); err == nil {
				for _, p := range cfg.Personas {
					if strings.EqualFold(p.Name, name) {
						return "project (.ntm/personas.toml)"
					}
				}
			}
			if cfg, err := persona.LoadFromFile(persona.DefaultUserPath()); err == nil {
				for _, p := range cfg.Personas {
					if strings.EqualFold(p.Name, name) {
						return "user (~/.config/ntm/personas.toml)"
					}
				}
			}
			return "built-in"
		}
	}

	// Not a builtin name - check user and project
	cwd, _ := os.Getwd()
	projectPath := cwd + "/.ntm/personas.toml"
	if cfg, err := persona.LoadFromFile(projectPath); err == nil {
		for _, p := range cfg.Personas {
			if strings.EqualFold(p.Name, name) {
				return "project (.ntm/personas.toml)"
			}
		}
	}
	if cfg, err := persona.LoadFromFile(persona.DefaultUserPath()); err == nil {
		for _, p := range cfg.Personas {
			if strings.EqualFold(p.Name, name) {
				return "user (~/.config/ntm/personas.toml)"
			}
		}
	}

	return "unknown"
}

func valueOrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// truncateRunes truncates a string to maxRunes runes plus suffix.
// This is UTF-8 safe unlike byte slicing.
func truncateRunes(s string, maxRunes int, suffix string) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + suffix
}

// newProfilesCmd creates an alias for 'personas' command as 'profiles'
// This provides user-friendly naming that aligns with the spawn --profiles flag
func newProfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "Manage agent profiles (alias for 'personas')",
		Long: `List and inspect available agent profiles.

This is an alias for 'ntm personas'. Profiles define agent characteristics
including agent type, model, system prompts, and behavioral settings.

Profile sources (later overrides earlier):
  1. Built-in: Compiled into ntm
  2. User: ~/.config/ntm/personas.toml
  3. Project: .ntm/personas.toml

Examples:
  ntm profiles list              # List all profiles
  ntm profiles list --json       # JSON output
  ntm profiles show architect    # Show profile details
  ntm profiles show architect --json`,
	}

	cmd.AddCommand(
		newPersonasListCmd(),
		newPersonasShowCmd(),
	)

	return cmd
}

func init() {
	rootCmd.AddCommand(newPersonasCmd())
	rootCmd.AddCommand(newProfilesCmd())
}
