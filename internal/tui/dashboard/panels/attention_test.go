package panels

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/ntm/internal/robot"
)

func TestNewAttentionPanel(t *testing.T) {
	panel := NewAttentionPanel()
	if panel == nil {
		t.Fatal("NewAttentionPanel returned nil")
	}

	cfg := panel.Config()
	if cfg.ID != "attention" {
		t.Fatalf("Config().ID = %q, want %q", cfg.ID, "attention")
	}
	if cfg.Title != "Attention" {
		t.Fatalf("Config().Title = %q, want %q", cfg.Title, "Attention")
	}
}

func TestAttentionPanelViewFeedInactive(t *testing.T) {
	panel := NewAttentionPanel()
	panel.SetSize(80, 18)
	panel.SetData(nil, false)

	view := panel.View()
	if !strings.Contains(view, "Feed not active") {
		t.Fatalf("expected inactive attention panel title, got %q", view)
	}
	if !strings.Contains(view, "Attention Feed not active") {
		t.Fatalf("expected inactive attention panel description, got %q", view)
	}
}

func TestAttentionPanelViewAllClear(t *testing.T) {
	panel := NewAttentionPanel()
	panel.SetSize(80, 18)
	panel.SetData(nil, true)

	view := panel.View()
	if !strings.Contains(view, "All clear") {
		t.Fatalf("expected all-clear title, got %q", view)
	}
	if !strings.Contains(view, "No attention items") {
		t.Fatalf("expected empty-state description, got %q", view)
	}
}

func TestAttentionPanelSelectedItemAndCursorClamp(t *testing.T) {
	panel := NewAttentionPanel()
	panel.SetData([]AttentionItem{
		{Summary: "first", Actionability: robot.ActionabilityInteresting, Timestamp: time.Now()},
	}, true)
	panel.cursor = 5
	panel.SetData(panel.items, true)

	item := panel.SelectedItem()
	if item == nil || item.Summary != "first" {
		t.Fatalf("SelectedItem() = %+v, want first item", item)
	}
}

func TestAttentionPanelNavigationMovesSelection(t *testing.T) {
	panel := NewAttentionPanel()
	panel.SetSize(80, 18)
	panel.SetData([]AttentionItem{
		{Summary: "first", Actionability: robot.ActionabilityInteresting, Timestamp: time.Now().Add(-2 * time.Minute)},
		{Summary: "second", Actionability: robot.ActionabilityActionRequired, Timestamp: time.Now()},
	}, true)
	panel.Focus()

	updated, cmd := panel.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatalf("expected nil command from navigation update, got %v", cmd)
	}

	got := updated.(*AttentionPanel).SelectedItem()
	if got == nil || got.Summary != "second" {
		t.Fatalf("selected item after down key = %+v, want second", got)
	}
}

func TestAttentionPanelKeybindingsIncludeZoomToSource(t *testing.T) {
	panel := NewAttentionPanel()
	bindings := panel.Keybindings()

	found := false
	for _, binding := range bindings {
		if binding.Action == "zoom_to_source" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected zoom_to_source keybinding")
	}
}
