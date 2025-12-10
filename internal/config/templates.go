package config

import (
	"bytes"
	"strings"
	"text/template"
)

// AgentTemplateVars contains variables available for agent command templates
type AgentTemplateVars struct {
	Model       string // Resolved full model name (e.g., "claude-opus-4-20250514")
	ModelAlias  string // Original alias as specified (e.g., "opus")
	SessionName string // NTM session name
	PaneIndex   int    // Pane number (1-based)
	AgentType   string // Agent type: "cc", "cod", "gmi"
	ProjectDir  string // Project directory path
}

// templateFuncs contains custom functions available in templates
var templateFuncs = template.FuncMap{
	// default returns the fallback if value is empty
	"default": func(fallback, value string) string {
		if value == "" {
			return fallback
		}
		return value
	},
	// eq checks string equality
	"eq": func(a, b string) bool {
		return a == b
	},
	// ne checks string inequality
	"ne": func(a, b string) bool {
		return a != b
	},
	// contains checks if string contains substring
	"contains": func(s, substr string) bool {
		return strings.Contains(s, substr)
	},
	// hasPrefix checks if string has prefix
	"hasPrefix": func(s, prefix string) bool {
		return strings.HasPrefix(s, prefix)
	},
	// hasSuffix checks if string has suffix
	"hasSuffix": func(s, suffix string) bool {
		return strings.HasSuffix(s, suffix)
	},
	// lower converts to lowercase
	"lower": func(s string) string {
		return strings.ToLower(s)
	},
	// upper converts to uppercase
	"upper": func(s string) string {
		return strings.ToUpper(s)
	},
}

// GenerateAgentCommand renders an agent command template with the given variables.
// If the template contains no {{}} syntax, it's returned as-is (legacy mode).
// Returns an error if template parsing or execution fails.
func GenerateAgentCommand(tmpl string, vars AgentTemplateVars) (string, error) {
	// Fast path: if no template syntax, return as-is (legacy mode)
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil
	}

	t, err := template.New("agent").Funcs(templateFuncs).Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", err
	}

	// Clean up whitespace from template conditionals
	result := buf.String()
	// Remove multiple consecutive spaces that may result from conditionals
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	result = strings.TrimSpace(result)

	return result, nil
}

// IsTemplateCommand checks if a command string uses template syntax
func IsTemplateCommand(cmd string) bool {
	return strings.Contains(cmd, "{{")
}

// DefaultAgentTemplates returns default agent command templates with model injection support.
// These templates show the recommended format for model-aware agent commands.
func DefaultAgentTemplates() AgentConfig {
	return AgentConfig{
		Claude: `NODE_OPTIONS="--max-old-space-size=32768" ENABLE_BACKGROUND_TASKS=1 claude --dangerously-skip-permissions{{if .Model}} --model {{.Model}}{{end}}`,
		Codex:  `codex --dangerously-bypass-approvals-and-sandbox -m {{.Model | default "gpt-4"}} -c model_reasoning_effort="high" -c model_reasoning_summary_format=experimental --enable web_search_request`,
		Gemini: `gemini{{if .Model}} --model {{.Model}}{{end}} --yolo`,
	}
}
