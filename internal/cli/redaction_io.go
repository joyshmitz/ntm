package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/ntm/internal/redaction"
)

// RedactionSummary is a safe-to-print summary of redaction findings.
// It intentionally does NOT include the matched secret values.
type RedactionSummary struct {
	Mode       string         `json:"mode"`
	Findings   int            `json:"findings"`
	Categories map[string]int `json:"categories,omitempty"`
	Action     string         `json:"action,omitempty"` // warn|redact|block
}

type redactionBlockedError struct {
	summary RedactionSummary
}

func (e redactionBlockedError) Error() string {
	parts := formatRedactionCategoryCounts(e.summary.Categories)
	if parts == "" {
		return "refusing to proceed: potential secrets detected (redaction mode: block). Hint: re-run with --allow-secret to bypass, or use --redact=warn/--redact=redact"
	}
	return fmt.Sprintf("refusing to proceed: potential secrets detected (%s) (redaction mode: block). Hint: re-run with --allow-secret to bypass, or use --redact=warn/--redact=redact", parts)
}

// applyOutputRedaction scans input using cfg and returns the output that should be written/copied.
// If cfg is in block mode and findings exist, it returns a redactionBlockedError.
func applyOutputRedaction(input string, cfg redaction.Config) (string, *RedactionSummary, error) {
	result := redaction.ScanAndRedact(input, cfg)
	if len(result.Findings) == 0 {
		return result.Output, nil, nil
	}

	summary := summarizeRedactionResult(result)
	if result.Blocked {
		return "", &summary, redactionBlockedError{summary: summary}
	}

	return result.Output, &summary, nil
}

func summarizeRedactionResult(result redaction.Result) RedactionSummary {
	summary := RedactionSummary{
		Mode:     string(result.Mode),
		Findings: len(result.Findings),
	}

	cats := make(map[string]int, len(result.Findings))
	for _, f := range result.Findings {
		cats[string(f.Category)]++
	}
	if len(cats) > 0 {
		summary.Categories = cats
	}

	switch result.Mode {
	case redaction.ModeWarn:
		summary.Action = "warn"
	case redaction.ModeRedact:
		summary.Action = "redact"
	case redaction.ModeBlock:
		summary.Action = "block"
	}

	return summary
}

func formatRedactionCategoryCounts(categories map[string]int) string {
	if len(categories) == 0 {
		return ""
	}
	keys := make([]string, 0, len(categories))
	for k := range categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, categories[k]))
	}
	return strings.Join(parts, ", ")
}
