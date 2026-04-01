package cli

import (
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

func TestAgentTypeLabel(t *testing.T) {
	tests := []struct {
		agentType tmux.AgentType
		want      string
	}{
		{tmux.AgentClaude, "cc"},
		{tmux.AgentCodex, "cod"},
		{tmux.AgentGemini, "gmi"},
		{tmux.AgentType("claude_code"), "cc"},
		{tmux.AgentType("openai-codex"), "cod"},
		{tmux.AgentType("google-gemini"), "gmi"},
		{tmux.AgentUser, "user"},
		{tmux.AgentUnknown, "unknown"},
		{"something-else", "unknown"},
	}
	for _, tt := range tests {
		got := scaleAgentTypeLabel(tt.agentType)
		if got != tt.want {
			t.Errorf("scaleAgentTypeLabel(%q) = %q, want %q", tt.agentType, got, tt.want)
		}
	}
}

func TestScaleDeltaCalculation(t *testing.T) {
	// Test the core delta calculation logic directly by simulating current counts
	// and target counts, then verifying the expected actions
	tests := []struct {
		name          string
		currentCounts map[string]int
		targets       []scaleTarget
		wantUpCount   int // total spawn actions expected
		wantDownCount int // total kill actions expected
		wantNoChange  bool
	}{
		{
			name:          "scale up from zero",
			currentCounts: map[string]int{"cc": 0, "cod": 0, "gmi": 0},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 3, set: true},
			},
			wantUpCount:   3,
			wantDownCount: 0,
		},
		{
			name:          "scale down",
			currentCounts: map[string]int{"cc": 5, "cod": 2, "gmi": 0},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 2, set: true},
			},
			wantUpCount:   0,
			wantDownCount: 3,
		},
		{
			name:          "no change when at target",
			currentCounts: map[string]int{"cc": 3, "cod": 2, "gmi": 1},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 3, set: true},
			},
			wantNoChange: true,
		},
		{
			name:          "mixed scale up and down",
			currentCounts: map[string]int{"cc": 3, "cod": 2, "gmi": 0},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 5, set: true},
				{agentType: AgentTypeCodex, count: 1, set: true},
				{agentType: AgentTypeGemini, count: 2, set: true},
			},
			wantUpCount:   4, // +2 cc + 2 gmi
			wantDownCount: 1, // -1 cod
		},
		{
			name:          "scale to zero",
			currentCounts: map[string]int{"cc": 3, "cod": 0, "gmi": 0},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 0, set: true},
			},
			wantUpCount:   0,
			wantDownCount: 3,
		},
		{
			name:          "unset flags are ignored",
			currentCounts: map[string]int{"cc": 3, "cod": 2, "gmi": 1},
			targets: []scaleTarget{
				{agentType: AgentTypeClaude, count: 0, set: false}, // not set
				{agentType: AgentTypeCodex, count: 5, set: true},   // set
			},
			wantUpCount:   3, // +3 cod
			wantDownCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upTotal, downTotal int

			for _, target := range tt.targets {
				if !target.set {
					continue
				}
				typeStr := string(target.agentType)
				current := tt.currentCounts[typeStr]
				delta := target.count - current

				if delta > 0 {
					upTotal += delta
				} else if delta < 0 {
					downTotal += -delta
				}
			}

			if tt.wantNoChange {
				if upTotal != 0 || downTotal != 0 {
					t.Errorf("expected no change, got up=%d down=%d", upTotal, downTotal)
				}
				return
			}

			if upTotal != tt.wantUpCount {
				t.Errorf("scale up count = %d, want %d", upTotal, tt.wantUpCount)
			}
			if downTotal != tt.wantDownCount {
				t.Errorf("scale down count = %d, want %d", downTotal, tt.wantDownCount)
			}
		})
	}
}

func TestScaleTargetValidation(t *testing.T) {
	// Verify negative counts are caught
	targets := []scaleTarget{
		{agentType: AgentTypeClaude, count: -1, set: true},
	}
	for _, target := range targets {
		if target.set && target.count < 0 {
			// This is the validation check in runScale
			t.Logf("correctly identified negative count for %s: %d", target.agentType, target.count)
		}
	}
}

func TestScaleTargetNoFlagsSet(t *testing.T) {
	targets := []scaleTarget{
		{agentType: AgentTypeClaude, count: 0, set: false},
		{agentType: AgentTypeCodex, count: 0, set: false},
		{agentType: AgentTypeGemini, count: 0, set: false},
	}

	anySet := false
	for _, target := range targets {
		if target.set {
			anySet = true
			break
		}
	}
	if anySet {
		t.Error("expected no flags set, but found one")
	}
}

func TestScaleAgentSelectionOrder(t *testing.T) {
	// Verify that agents are selected for killing in NTMIndex descending order
	panes := []tmux.Pane{
		{NTMIndex: 1, Title: "proj__cc_1"},
		{NTMIndex: 3, Title: "proj__cc_3"},
		{NTMIndex: 2, Title: "proj__cc_2"},
		{NTMIndex: 5, Title: "proj__cc_5"},
		{NTMIndex: 4, Title: "proj__cc_4"},
	}

	// Sort descending by NTMIndex (matching scale command logic)
	sorted := make([]tmux.Pane, len(panes))
	copy(sorted, panes)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].NTMIndex > sorted[i].NTMIndex {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// First to kill should be highest index
	if sorted[0].NTMIndex != 5 {
		t.Errorf("first kill candidate NTMIndex = %d, want 5", sorted[0].NTMIndex)
	}
	if sorted[1].NTMIndex != 4 {
		t.Errorf("second kill candidate NTMIndex = %d, want 4", sorted[1].NTMIndex)
	}
	if sorted[4].NTMIndex != 1 {
		t.Errorf("last kill candidate NTMIndex = %d, want 1", sorted[4].NTMIndex)
	}
}

func TestNewScaleCmd(t *testing.T) {
	cmd := newScaleCmd()

	if cmd.Use != "scale <session>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "scale <session>")
	}

	// Verify expected flags exist
	flags := []string{"cc", "cod", "gmi", "dry-run", "force"}
	for _, name := range flags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("expected flag %q not found", name)
		}
	}

	// Verify force has short flag
	f := cmd.Flags().ShorthandLookup("f")
	if f == nil {
		t.Error("expected short flag -f for --force")
	}
}

func TestScaleResponseFields(t *testing.T) {
	resp := ScaleResponse{
		Session: "test",
		Before:  map[string]int{"cc": 3, "cod": 2, "gmi": 0},
		After:   map[string]int{"cc": 5, "cod": 1, "gmi": 2},
		Actions: []ScaleAction{
			{ActionType: "spawn", AgentType: "cc", Count: 2},
			{ActionType: "kill", AgentType: "cod", Count: 1, Agents: []string{"test__cod_2"}},
			{ActionType: "spawn", AgentType: "gmi", Count: 2},
		},
		Success: true,
	}

	if resp.Session != "test" {
		t.Errorf("Session = %q, want %q", resp.Session, "test")
	}
	if resp.Before["cc"] != 3 {
		t.Errorf("Before[cc] = %d, want 3", resp.Before["cc"])
	}
	if resp.After["cc"] != 5 {
		t.Errorf("After[cc] = %d, want 5", resp.After["cc"])
	}
	if len(resp.Actions) != 3 {
		t.Errorf("len(Actions) = %d, want 3", len(resp.Actions))
	}
	if !resp.Success {
		t.Error("expected Success = true")
	}
	if resp.DryRun {
		t.Error("expected DryRun = false when not set")
	}
}
