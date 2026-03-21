package robot

import (
	"strings"
	"testing"
)

// =============================================================================
// categoryIndex tests
// =============================================================================

func TestCategoryIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cat  string
		want int
	}{
		{"state", "state", 0},
		{"ensemble", "ensemble", 1},
		{"control", "control", 2},
		{"spawn", "spawn", 3},
		{"beads", "beads", 4},
		{"bv", "bv", 5},
		{"cass", "cass", 6},
		{"pipeline", "pipeline", 7},
		{"utility", "utility", 8},
		{"unknown category", "nonexistent", len(categoryOrder)},
		{"empty string", "", len(categoryOrder)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := categoryIndex(tc.cat)
			if got != tc.want {
				t.Errorf("categoryIndex(%q) = %d, want %d", tc.cat, got, tc.want)
			}
		})
	}
}

// =============================================================================
// buildCommandRegistry tests
// =============================================================================

func TestBuildCommandRegistry(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()

	if len(commands) == 0 {
		t.Fatal("buildCommandRegistry() returned empty slice")
	}

	// Verify all commands have required fields
	for i, cmd := range commands {
		if cmd.Name == "" {
			t.Errorf("command[%d] has empty Name", i)
		}
		if cmd.Flag == "" {
			t.Errorf("command[%d] (%s) has empty Flag", i, cmd.Name)
		}
		if cmd.Category == "" {
			t.Errorf("command[%d] (%s) has empty Category", i, cmd.Name)
		}
		if cmd.Description == "" {
			t.Errorf("command[%d] (%s) has empty Description", i, cmd.Name)
		}
	}
}

func TestBuildCommandRegistryUniqueNames(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()
	seen := make(map[string]bool)

	for _, cmd := range commands {
		if seen[cmd.Name] {
			t.Errorf("duplicate command name: %q", cmd.Name)
		}
		seen[cmd.Name] = true
	}
}

func TestBuildCommandRegistryUniqueFlags(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()
	seen := make(map[string]bool)

	for _, cmd := range commands {
		if seen[cmd.Flag] {
			t.Errorf("duplicate command flag: %q", cmd.Flag)
		}
		seen[cmd.Flag] = true
	}
}

func TestBuildCommandRegistryValidCategories(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()
	validCategories := make(map[string]bool)
	for _, cat := range categoryOrder {
		validCategories[cat] = true
	}

	for _, cmd := range commands {
		if !validCategories[cmd.Category] {
			t.Errorf("command %q has invalid category %q", cmd.Name, cmd.Category)
		}
	}
}

func TestBuildCommandRegistryExamples(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()

	for _, cmd := range commands {
		if len(cmd.Examples) == 0 {
			t.Errorf("command %q has no examples", cmd.Name)
		}
	}
}

func TestBuildCommandRegistryParameterFields(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()

	for _, cmd := range commands {
		for j, param := range cmd.Parameters {
			if param.Name == "" {
				t.Errorf("command %q param[%d] has empty Name", cmd.Name, j)
			}
			if param.Flag == "" {
				t.Errorf("command %q param[%d] has empty Flag", cmd.Name, j)
			}
			if param.Type == "" {
				t.Errorf("command %q param[%d] has empty Type", cmd.Name, j)
			}
			if param.Description == "" {
				t.Errorf("command %q param[%d] has empty Description", cmd.Name, j)
			}
		}
	}
}

// =============================================================================
// GetCapabilities tests
// =============================================================================

func TestGetCapabilities(t *testing.T) {
	t.Parallel()

	output, err := GetCapabilities()
	if err != nil {
		t.Fatalf("GetCapabilities() error: %v", err)
	}
	if output == nil {
		t.Fatal("GetCapabilities() returned nil")
	}

	if len(output.Commands) == 0 {
		t.Error("expected non-empty Commands")
	}
	if len(output.Categories) != len(categoryOrder) {
		t.Errorf("Categories length = %d, want %d", len(output.Categories), len(categoryOrder))
	}
	if output.Attention == nil {
		t.Fatal("expected attention capabilities")
	}
	if output.Attention.ContractVersion != AttentionContractVersion {
		t.Errorf("Attention.ContractVersion = %q, want %q", output.Attention.ContractVersion, AttentionContractVersion)
	}
	beadOrphaned, ok := output.Attention.SignalAvailability[AttentionSignalBeadOrphaned]
	if !ok {
		t.Fatalf("expected %q signal availability entry", AttentionSignalBeadOrphaned)
	}
	if beadOrphaned.Status != CapabilityUnavailable {
		t.Errorf("bead_orphaned status = %q, want %q", beadOrphaned.Status, CapabilityUnavailable)
	}
}

func TestGetCapabilitiesSortOrder(t *testing.T) {
	t.Parallel()

	output, err := GetCapabilities()
	if err != nil {
		t.Fatalf("GetCapabilities() error: %v", err)
	}

	// Verify commands are sorted by category then name
	for i := 1; i < len(output.Commands); i++ {
		prev := output.Commands[i-1]
		curr := output.Commands[i]

		prevIdx := categoryIndex(prev.Category)
		currIdx := categoryIndex(curr.Category)

		if prevIdx > currIdx {
			t.Errorf("commands not sorted by category: %q (%s) before %q (%s)",
				prev.Name, prev.Category, curr.Name, curr.Category)
		}
		if prevIdx == currIdx && prev.Name > curr.Name {
			t.Errorf("commands not sorted by name within category %q: %q before %q",
				prev.Category, prev.Name, curr.Name)
		}
	}
}

func TestDefaultAttentionCapabilities_BeadOrphanedUnsupported(t *testing.T) {
	t.Parallel()

	caps := DefaultAttentionCapabilities()
	if caps == nil {
		t.Fatal("DefaultAttentionCapabilities() returned nil")
	}
	if caps.ContractVersion != AttentionContractVersion {
		t.Errorf("ContractVersion = %q, want %q", caps.ContractVersion, AttentionContractVersion)
	}
	signal, ok := caps.SignalAvailability[AttentionSignalBeadOrphaned]
	if !ok {
		t.Fatalf("expected %q in SignalAvailability", AttentionSignalBeadOrphaned)
	}
	if signal.Status != CapabilityUnavailable {
		t.Errorf("Status = %q, want %q", signal.Status, CapabilityUnavailable)
	}
	if signal.Note == "" {
		t.Error("expected unsupported note for bead_orphaned")
	}
}

func TestDefaultAttentionCapabilities_UnsupportedConditionsIncluded(t *testing.T) {
	t.Parallel()

	caps := DefaultAttentionCapabilities()
	if caps == nil {
		t.Fatal("DefaultAttentionCapabilities() returned nil")
	}
	if len(caps.UnsupportedConditions) == 0 {
		t.Fatal("expected UnsupportedConditions to be non-empty")
	}

	// Verify bead_orphaned is listed with full rationale
	found := false
	for _, uc := range caps.UnsupportedConditions {
		if uc.Name == AttentionSignalBeadOrphaned {
			found = true
			if uc.Status != CapabilityUnavailable {
				t.Errorf("bead_orphaned Status = %q, want %q", uc.Status, CapabilityUnavailable)
			}
			if uc.Reason == "" {
				t.Error("bead_orphaned Reason must not be empty")
			}
			if len(uc.ObservablesAvailable) == 0 {
				t.Error("bead_orphaned ObservablesAvailable must list what ntm CAN see")
			}
			if uc.WhatWouldChange == "" {
				t.Error("bead_orphaned WhatWouldChange must explain prerequisites for support")
			}
		}
	}
	if !found {
		t.Errorf("expected %q in UnsupportedConditions", AttentionSignalBeadOrphaned)
	}
}

func TestUnsupportedConditions_ConsistentWithSignalAvailability(t *testing.T) {
	t.Parallel()

	caps := DefaultAttentionCapabilities()

	// Every unsupported condition must also appear in SignalAvailability
	// with CapabilityUnavailable status — the two surfaces must agree.
	for _, uc := range caps.UnsupportedConditions {
		signal, ok := caps.SignalAvailability[uc.Name]
		if !ok {
			t.Errorf("UnsupportedCondition %q not found in SignalAvailability", uc.Name)
			continue
		}
		if signal.Status != CapabilityUnavailable {
			t.Errorf("SignalAvailability[%q].Status = %q, but UnsupportedConditions marks it unavailable",
				uc.Name, signal.Status)
		}
	}
}

func TestUnsupportedConditions_NotInAllWaitConditions(t *testing.T) {
	t.Parallel()

	// Unsupported conditions must NEVER appear in AllWaitConditions
	waitSet := make(map[string]bool)
	for _, wc := range AllWaitConditions {
		waitSet[wc] = true
	}

	for _, uc := range UnsupportedConditions() {
		if waitSet[uc.Name] {
			t.Errorf("unsupported condition %q must not be in AllWaitConditions", uc.Name)
		}
	}
}

func TestIsUnsupportedWaitCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		condition string
		want      bool
	}{
		{AttentionSignalBeadOrphaned, true},
		{"bead_orphaned", true},        // explicit string match
		{"idle", false},                // valid condition
		{"complete", false},            // valid condition
		{"nonexistent_garbage", false}, // unknown condition
		{"bead_orphaned,idle", true},   // unsupported in composed condition
	}

	for _, tt := range tests {
		got := isUnsupportedWaitCondition(tt.condition)
		if got != tt.want {
			t.Errorf("isUnsupportedWaitCondition(%q) = %v, want %v", tt.condition, got, tt.want)
		}
	}
}

func TestBuildCommandRegistryWaitCommandReflectsExtendedConditions(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()

	var waitCmd *RobotCommandInfo
	for i := range commands {
		if commands[i].Name == "wait" {
			waitCmd = &commands[i]
			break
		}
	}
	if waitCmd == nil {
		t.Fatal("expected wait command in registry")
	}

	if !strings.Contains(waitCmd.Description, "attention-feed conditions") {
		t.Fatalf("wait description %q should mention attention-feed conditions", waitCmd.Description)
	}

	var untilParam *RobotParameter
	var transitionParam *RobotParameter
	for i := range waitCmd.Parameters {
		param := &waitCmd.Parameters[i]
		switch param.Flag {
		case "--wait-until":
			untilParam = param
		case "--wait-transition":
			transitionParam = param
		}
	}
	if untilParam == nil {
		t.Fatal("expected --wait-until parameter")
	}
	if transitionParam == nil {
		t.Fatal("expected --wait-transition parameter")
	}

	for _, cond := range []string{
		"stalled",
		"rate_limited",
		"attention",
		"action_required",
		"mail_pending",
		"mail_ack_required",
		"context_hot",
		"reservation_conflict",
		"file_conflict",
		"session_changed",
		"pane_changed",
		"bead_orphaned",
	} {
		if !strings.Contains(untilParam.Description, cond) {
			t.Errorf("--wait-until description missing %q: %q", cond, untilParam.Description)
		}
	}

	if !strings.Contains(transitionParam.Description, "pane-state conditions") {
		t.Errorf("--wait-transition description should clarify pane-state scope, got %q", transitionParam.Description)
	}

	foundAttentionExample := false
	for _, example := range waitCmd.Examples {
		if strings.Contains(example, "--wait-until=action_required") {
			foundAttentionExample = true
			break
		}
	}
	if !foundAttentionExample {
		t.Error("expected wait command examples to include an action_required attention wait")
	}
}

func TestDocsContentMentionsAttentionWait(t *testing.T) {
	t.Parallel()

	commands := getCommandsContent()
	if commands == nil {
		t.Fatal("getCommandsContent() returned nil")
	}

	foundAgentControl := false
	for _, section := range commands.Sections {
		if section.Heading != "Agent Control" {
			continue
		}
		foundAgentControl = true
		if !strings.Contains(section.Body, "pane state or attention-feed condition") {
			t.Fatalf("Agent Control docs should mention attention-feed wait conditions, got %q", section.Body)
		}
	}
	if !foundAgentControl {
		t.Fatal("Agent Control section not found")
	}

	examples := getExamplesContent()
	if examples == nil {
		t.Fatal("getExamplesContent() returned nil")
	}

	found := false
	for _, example := range examples.Examples {
		if example.Name != "wait_for_attention" {
			continue
		}
		found = true
		if !strings.Contains(example.Command, "--wait-until=action_required") {
			t.Errorf("wait_for_attention command = %q, want action_required wait", example.Command)
		}
		if !strings.Contains(example.Notes, "operator-relevant wakeup") {
			t.Errorf("wait_for_attention notes = %q, want operator wakeup guidance", example.Notes)
		}
	}
	if !found {
		t.Fatal("expected wait_for_attention example")
	}
}

// =============================================================================
// categoryOrder tests
// =============================================================================

func TestCategoryOrderCompleteness(t *testing.T) {
	t.Parallel()

	commands := buildCommandRegistry()
	usedCategories := make(map[string]bool)
	for _, cmd := range commands {
		usedCategories[cmd.Category] = true
	}

	for cat := range usedCategories {
		found := false
		for _, c := range categoryOrder {
			if c == cat {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %q used in commands but not in categoryOrder", cat)
		}
	}
}
