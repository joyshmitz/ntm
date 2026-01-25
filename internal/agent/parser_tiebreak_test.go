package agent

import (
	"testing"
)

// TestParser_DetectByPatternFrequency_TieBreak verifies that agent detection is deterministic
// even when multiple agents have matching patterns with the same score.
func TestParser_DetectByPatternFrequency_TieBreak(t *testing.T) {
	p := NewParser()

	// Scenario: Output contains "running " which is a working pattern for both Claude and Gemini.
	// Both should get a score of 1.
	// We want the detection to be deterministic (e.g., prefer Claude over Gemini if tied, or at least consistent).
	// Currently, map iteration randomization makes this flaky.
	output := `
running a command
`

	// Run multiple times to catch non-determinism
	firstResult := AgentTypeUnknown
	for i := 0; i < 100; i++ {
		// We can't access detectByPatternFrequency directly as it's private.
		// But Parse calls DetectAgentType which calls detectByPatternFrequency
		// if no explicit header is found.
		state, err := p.Parse(output)
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		if i == 0 {
			firstResult = state.Type
			t.Logf("Iteration 0: Detected %v", firstResult)
		} else {
			if state.Type != firstResult {
				t.Fatalf("Non-deterministic detection! Iteration %d gave %v, expected %v", i, state.Type, firstResult)
			}
		}
	}
}
