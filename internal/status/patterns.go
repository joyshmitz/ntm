package status

import (
	"regexp"
	"strings"
	"sync"
)

// ansiEscapeRegex matches ANSI escape sequences for stripping
// Includes CSI sequences (with private mode ?) and OSC sequences (title setting etc)
var ansiEscapeRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\a\x1b]*(\a|\x1b\\)`)

// PromptPattern defines a pattern for detecting idle state
type PromptPattern struct {
	// AgentType specifies which agent type this pattern applies to.
	// Empty string means it applies to all agent types.
	AgentType string
	// Regex is a compiled regular expression for matching (optional)
	Regex *regexp.Regexp
	// Literal is a simple string suffix to match (optional, faster than regex)
	Literal string
	// Description explains what this pattern matches (for debugging)
	Description string
}

// promptPatternsMu protects promptPatterns from concurrent access.
var promptPatternsMu sync.RWMutex

// promptPatterns contains all known prompt patterns for agent types
var promptPatterns = []PromptPattern{
	// Claude Code patterns
	{AgentType: "cc", Regex: regexp.MustCompile(`(?i)claude>?\s*$`), Description: "Claude prompt"},
	{AgentType: "cc", Regex: regexp.MustCompile(`>\s*$`), Description: "Claude simple prompt"},

	// Codex CLI patterns
	{AgentType: "cod", Regex: regexp.MustCompile(`(?i)codex>?\s*$`), Description: "Codex prompt"},

	// Gemini CLI patterns
	{AgentType: "gmi", Regex: regexp.MustCompile(`(?i)gemini>?\s*$`), Description: "Gemini prompt"},

	// Cursor patterns
	{AgentType: "cursor", Regex: regexp.MustCompile(`(?i)cursor>?\s*$`), Description: "Cursor prompt"},

	// Windsurf patterns
	{AgentType: "windsurf", Regex: regexp.MustCompile(`(?i)windsurf>?\s*$`), Description: "Windsurf prompt"},

	// Aider patterns
	{AgentType: "aider", Regex: regexp.MustCompile(`(?i)aider>?\s*$`), Description: "Aider prompt"},
	{AgentType: "aider", Regex: regexp.MustCompile(`>\s*$`), Description: "Aider simple prompt"},

	// Generic shell prompts (for user panes and fallback)
	{AgentType: "user", Regex: regexp.MustCompile(`[$%>]\s*$`), Description: "Standard shell prompt"},
	{AgentType: "user", Regex: regexp.MustCompile(`❯\s*$`), Description: "Fancy shell prompt (starship, etc)"},
	{AgentType: "user", Regex: regexp.MustCompile(`\$\s*$`), Description: "Dollar prompt"},

	// Generic patterns (apply to all types as fallback)
	{AgentType: "", Regex: regexp.MustCompile(`>\s*$`), Description: "Generic > prompt"},
	{AgentType: "", Regex: regexp.MustCompile(`[$%]\s*$`), Description: "Generic shell prompt"},
}

// StripANSI removes ANSI escape sequences from a string
func StripANSI(s string) string {
	return ansiEscapeRegex.ReplaceAllString(s, "")
}

// IsPromptLine checks if a line looks like a prompt.
// agentType can be empty to match any agent type.
func IsPromptLine(line string, agentType string) bool {
	// Strip ANSI codes first
	line = StripANSI(line)
	line = strings.TrimSpace(line)

	if line == "" {
		return false
	}

	// Try agent-specific patterns first, then generic ones
	promptPatternsMu.RLock()
	defer promptPatternsMu.RUnlock()

	for _, p := range promptPatterns {
		// Skip patterns for other agent types
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		// Skip generic shell prompt patterns for known agent types.
		// A shell $ prompt in a cc/cod/gmi pane means the agent exited,
		// not that it's idle at its prompt.
		if p.AgentType == "" && p.Description == "Generic shell prompt" && knownAgentTypes[agentType] {
			continue
		}

		if p.Regex != nil && p.Regex.MatchString(line) {
			return true
		}
		if p.Literal != "" && strings.HasSuffix(line, p.Literal) {
			return true
		}
	}

	return false
}

// knownAgentTypes are agent types that have their own specific prompt patterns.
// Generic shell prompt detection should not apply to these.
var knownAgentTypes = map[string]bool{
	"cc":       true, // Claude Code uses "claude>" or ">" prompts
	"cod":      true, // Codex uses "codex>" prompt
	"gmi":      true, // Gemini uses "gemini>" prompt
	"cursor":   true,
	"windsurf": true,
	"aider":    true,
}

// DetectIdleFromOutput analyzes output to determine if agent is idle.
// It checks up to 3 non-empty lines from the end for prompt patterns.
// This window allows detecting idle state even when there's trailing output.
func DetectIdleFromOutput(output string, agentType string) bool {
	// Strip ANSI first for cleaner processing
	clean := StripANSI(output)

	// Quick heuristic: if output ends with a shell prompt marker, treat as idle.
	// Only apply to user panes or unknown agent types, since known agent types
	// (cc, cod, gmi, etc.) have their own prompt patterns and a shell $ prompt
	// in those panes indicates the agent has exited, not that it's idle.
	// Check for both "$ " and just "$" since TrimSpace may remove trailing space.
	trimmed := strings.TrimSpace(clean)
	if !knownAgentTypes[agentType] {
		if strings.HasSuffix(trimmed, "$") {
			return true
		}
	}

	lines := strings.Split(clean, "\n")
	if len(lines) == 0 {
		return false
	}

	// Check up to 3 non-empty lines from the end for prompt patterns.
	// This window allows detecting idle state even with some trailing output.
	const maxLinesToCheck = 3
	linesChecked := 0
	for i := len(lines) - 1; i >= 0 && linesChecked < maxLinesToCheck; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		linesChecked++
		if IsPromptLine(line, agentType) {
			return true
		}
	}

	// If there was no output at all, treat user panes as idle (empty buffer, likely waiting at prompt)
	if linesChecked == 0 && (agentType == "" || agentType == "user") {
		return true
	}
	return false
}

// GetLastNonEmptyLine returns the last non-empty line from output
func GetLastNonEmptyLine(output string) string {
	output = StripANSI(output)
	lines := strings.Split(output, "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}

	return ""
}

// AddPromptPattern allows adding custom prompt patterns at runtime.
// It is thread-safe and can be called concurrently with detection functions.
func AddPromptPattern(agentType string, pattern string, description string) error {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}

	promptPatternsMu.Lock()
	defer promptPatternsMu.Unlock()

	promptPatterns = append(promptPatterns, PromptPattern{
		AgentType:   agentType,
		Regex:       regex,
		Description: description,
	})

	return nil
}
