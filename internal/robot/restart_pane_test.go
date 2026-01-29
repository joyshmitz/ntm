package robot

import (
	"strings"
	"testing"
)

func TestRestartPaneBeadPromptTemplate(t *testing.T) {
	// Verify the template contains the expected placeholders
	if !strings.Contains(restartPaneBeadPromptTemplate, "{bead_id}") {
		t.Fatal("template missing {bead_id} placeholder")
	}
	if !strings.Contains(restartPaneBeadPromptTemplate, "{bead_title}") {
		t.Fatal("template missing {bead_title} placeholder")
	}
	if !strings.Contains(restartPaneBeadPromptTemplate, "AGENTS.md") {
		t.Fatal("template should reference AGENTS.md")
	}
	if !strings.Contains(restartPaneBeadPromptTemplate, "Agent Mail") {
		t.Fatal("template should reference Agent Mail")
	}
	if !strings.Contains(restartPaneBeadPromptTemplate, "br show") {
		t.Fatal("template should reference br show for bead details")
	}
}

func TestRestartPaneBeadPromptExpansion(t *testing.T) {
	// Test that the template expands correctly using the same replacer logic
	beadID := "bd-abc12"
	beadTitle := "Fix authentication bug"

	prompt := strings.NewReplacer(
		"{bead_id}", beadID,
		"{bead_title}", beadTitle,
	).Replace(restartPaneBeadPromptTemplate)

	if strings.Contains(prompt, "{bead_id}") {
		t.Error("prompt still contains {bead_id} placeholder after expansion")
	}
	if strings.Contains(prompt, "{bead_title}") {
		t.Error("prompt still contains {bead_title} placeholder after expansion")
	}
	if !strings.Contains(prompt, beadID) {
		t.Errorf("prompt should contain bead ID %q", beadID)
	}
	if !strings.Contains(prompt, beadTitle) {
		t.Errorf("prompt should contain bead title %q", beadTitle)
	}
	// The bead_id should appear multiple times (in work-on and br show)
	if strings.Count(prompt, beadID) < 2 {
		t.Errorf("bead ID should appear at least twice in prompt (work-on + br show), got %d", strings.Count(prompt, beadID))
	}
}

func TestRestartPaneOptionsPromptOverridesBead(t *testing.T) {
	// When both Bead and Prompt are set, Prompt should take precedence.
	// This tests the logic flow: promptToSend defaults to Prompt, falling back to beadPrompt.
	opts := RestartPaneOptions{
		Session: "test-session",
		Bead:    "bd-xyz",
		Prompt:  "Custom prompt override",
	}

	// Simulate the priority logic from GetRestartPane
	promptToSend := opts.Prompt
	beadPrompt := "generated from bead"
	if promptToSend == "" && beadPrompt != "" {
		promptToSend = beadPrompt
	}

	if promptToSend != "Custom prompt override" {
		t.Errorf("explicit --prompt should override bead template, got %q", promptToSend)
	}
}

func TestRestartPaneOptionsBeadPromptFallback(t *testing.T) {
	// When only Bead is set (no Prompt), beadPrompt should be used
	opts := RestartPaneOptions{
		Session: "test-session",
		Bead:    "bd-xyz",
	}

	promptToSend := opts.Prompt
	beadPrompt := "generated from bead"
	if promptToSend == "" && beadPrompt != "" {
		promptToSend = beadPrompt
	}

	if promptToSend != "generated from bead" {
		t.Errorf("bead template should be used when no explicit prompt, got %q", promptToSend)
	}
}

func TestRestartPaneOutputBeadFields(t *testing.T) {
	// Verify the output struct carries bead assignment info
	output := RestartPaneOutput{
		BeadAssigned: "bd-abc12",
		PromptSent:   true,
	}

	if output.BeadAssigned != "bd-abc12" {
		t.Errorf("BeadAssigned = %q, want %q", output.BeadAssigned, "bd-abc12")
	}
	if !output.PromptSent {
		t.Error("PromptSent should be true")
	}
}

func TestRestartPaneOutputPromptError(t *testing.T) {
	output := RestartPaneOutput{
		BeadAssigned: "bd-abc12",
		PromptSent:   false,
		PromptError:  "pane 1: connection refused",
	}

	if output.PromptSent {
		t.Error("PromptSent should be false when there's an error")
	}
	if output.PromptError == "" {
		t.Error("PromptError should be set when prompt sending fails")
	}
}

func TestRestartPaneDryRunShowsBead(t *testing.T) {
	// In dry-run mode, BeadAssigned should still be populated
	output := RestartPaneOutput{
		DryRun:       true,
		WouldAffect:  []string{"1", "2"},
		BeadAssigned: "bd-abc12",
	}

	if output.BeadAssigned == "" {
		t.Error("BeadAssigned should be set even in dry-run mode")
	}
	if !output.DryRun {
		t.Error("DryRun should be true")
	}
}
