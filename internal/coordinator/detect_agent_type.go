package coordinator

import "strings"

// detectAgentType extracts the canonical agent type from a pane title.
// Pane titles follow the pattern: <session>__<type>_<index>
func detectAgentType(title string) string {
	sep := strings.LastIndex(title, "__")
	if sep == -1 || sep+2 >= len(title) {
		return ""
	}
	typePart := title[sep+2:]
	if typePart == "" {
		return ""
	}

	agent := typePart
	if idx := strings.IndexRune(typePart, '_'); idx != -1 {
		agent = typePart[:idx]
	}

	switch strings.ToLower(agent) {
	case "cc", "claude":
		return "cc"
	case "cod", "codex":
		return "cod"
	case "gmi", "gemini":
		return "gmi"
	default:
		return ""
	}
}
