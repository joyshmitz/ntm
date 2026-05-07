package pipeline

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var declaredParamPattern = regexp.MustCompile(`\*\*Parameters:\*\*\s*(.+)`)
var placeholderPattern = regexp.MustCompile(`<([A-Z][A-Z0-9_]*)>`)

// RenderTemplate substitutes <KEY> placeholders in content with values from
// params. Reserved placeholders (<TIMESTAMP_UTC>, <WORKSPACE_PATH>,
// <SESSION_ID>) are auto-populated from the provided reserved map. Args values
// are used as fallback when a key is not found in params.
//
// After substitution, any declared placeholder (listed on a **Parameters:**
// line) that remains unresolved causes an error. Instructional placeholders
// like <NNN> that are NOT declared in Parameters survive validation.
func RenderTemplate(content string, params, args map[string]interface{}, reserved map[string]string) (string, error) {
	merged := make(map[string]string)
	for k, v := range args {
		merged[strings.ToUpper(k)] = fmt.Sprintf("%v", v)
	}
	for k, v := range params {
		merged[strings.ToUpper(k)] = fmt.Sprintf("%v", v)
	}
	for k, v := range reserved {
		merged[strings.ToUpper(k)] = v
	}

	rendered := placeholderPattern.ReplaceAllStringFunc(content, func(match string) string {
		key := match[1 : len(match)-1]
		if val, ok := merged[key]; ok {
			return val
		}
		return match
	})

	declared := declaredPlaceholders(content)
	var unresolved []string
	for _, key := range declared {
		if _, ok := merged[key]; !ok {
			if strings.Contains(rendered, "<"+key+">") {
				unresolved = append(unresolved, key)
			}
		}
	}
	if len(unresolved) > 0 {
		return "", fmt.Errorf("unresolved declared placeholders: %s", strings.Join(unresolved, ", "))
	}

	return rendered, nil
}

// declaredPlaceholders extracts placeholder names from a **Parameters:** line.
func declaredPlaceholders(content string) []string {
	m := declaredParamPattern.FindStringSubmatch(content)
	if len(m) < 2 {
		return nil
	}
	var names []string
	for _, pm := range placeholderPattern.FindAllStringSubmatch(m[1], -1) {
		if len(pm) >= 2 {
			names = append(names, pm[1])
		}
	}
	return names
}

// ReservedPlaceholders returns the standard reserved placeholders for template
// rendering based on the current execution context.
func ReservedPlaceholders(projectDir, sessionID string) map[string]string {
	return map[string]string{
		"TIMESTAMP_UTC":  time.Now().UTC().Format(time.RFC3339),
		"WORKSPACE_PATH": projectDir,
		"SESSION_ID":     sessionID,
	}
}
