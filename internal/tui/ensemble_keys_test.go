package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

func TestDefaultEnsembleKeyMap_AllBindingsNonEmpty(t *testing.T) {

	km := DefaultEnsembleKeyMap()

	bindings := map[string]key.Binding{
		"Quit":           km.Quit,
		"Help":           km.Help,
		"Refresh":        km.Refresh,
		"NextMode":       km.NextMode,
		"PrevMode":       km.PrevMode,
		"ZoomWindow":     km.ZoomWindow,
		"CycleFocus":     km.CycleFocus,
		"StartSynthesis": km.StartSynthesis,
		"ForceSynthesis": km.ForceSynthesis,
		"Export":         km.Export,
		"Copy":           km.Copy,
	}

	for name, binding := range bindings {
		keys := binding.Keys()
		if len(keys) == 0 {
			t.Errorf("%s binding has no keys assigned", name)
		}
		help := binding.Help()
		if help.Key == "" {
			t.Errorf("%s binding has empty help key", name)
		}
		if help.Desc == "" {
			t.Errorf("%s binding has empty help desc", name)
		}
	}
}

func TestDefaultEnsembleKeyMap_SpecificKeys(t *testing.T) {

	km := DefaultEnsembleKeyMap()

	tests := []struct {
		name string
		bind key.Binding
		want []string
	}{
		{"Quit", km.Quit, []string{"q"}},
		{"Help", km.Help, []string{"?"}},
		{"Refresh", km.Refresh, []string{"r"}},
		{"NextMode", km.NextMode, []string{"j", "down"}},
		{"PrevMode", km.PrevMode, []string{"k", "up"}},
		{"ZoomWindow", km.ZoomWindow, []string{"enter"}},
		{"CycleFocus", km.CycleFocus, []string{"tab"}},
		{"StartSynthesis", km.StartSynthesis, []string{"s"}},
		{"ForceSynthesis", km.ForceSynthesis, []string{"S"}},
		{"Export", km.Export, []string{"e"}},
		{"Copy", km.Copy, []string{"c"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keys := tc.bind.Keys()
			if len(keys) != len(tc.want) {
				t.Errorf("%s: got %d keys, want %d", tc.name, len(keys), len(tc.want))
				return
			}
			for i, k := range keys {
				if k != tc.want[i] {
					t.Errorf("%s key[%d] = %q, want %q", tc.name, i, k, tc.want[i])
				}
			}
		})
	}
}

func TestDefaultEnsembleKeyMap_JumpWindowBindings(t *testing.T) {

	km := DefaultEnsembleKeyMap()

	if len(km.JumpWindow) != 9 {
		t.Fatalf("JumpWindow has %d bindings, want 9", len(km.JumpWindow))
	}

	for i, binding := range km.JumpWindow {
		expected := string(rune('0' + i + 1))
		keys := binding.Keys()
		if len(keys) != 1 {
			t.Errorf("JumpWindow[%d] has %d keys, want 1", i, len(keys))
			continue
		}
		if keys[0] != expected {
			t.Errorf("JumpWindow[%d] key = %q, want %q", i, keys[0], expected)
		}
		help := binding.Help()
		if help.Key != expected {
			t.Errorf("JumpWindow[%d] help key = %q, want %q", i, help.Key, expected)
		}
		if help.Desc != "jump" {
			t.Errorf("JumpWindow[%d] help desc = %q, want %q", i, help.Desc, "jump")
		}
	}
}

func TestEnsembleKeyMap_ShortHelp(t *testing.T) {

	km := DefaultEnsembleKeyMap()
	short := km.ShortHelp()

	if len(short) != 9 {
		t.Fatalf("ShortHelp returned %d bindings, want 9", len(short))
	}

	// Verify order: Help, Refresh, NextMode, PrevMode, StartSynthesis, ForceSynthesis, Export, Copy, Quit
	expectedHelpKeys := []string{"?", "r", "j/↓", "k/↑", "s", "S", "e", "c", "q"}
	for i, binding := range short {
		help := binding.Help()
		if help.Key != expectedHelpKeys[i] {
			t.Errorf("ShortHelp[%d] key = %q, want %q", i, help.Key, expectedHelpKeys[i])
		}
	}
}

func TestEnsembleKeyMap_FullHelp(t *testing.T) {

	km := DefaultEnsembleKeyMap()
	full := km.FullHelp()

	if len(full) != 3 {
		t.Fatalf("FullHelp returned %d groups, want 3", len(full))
	}

	// Group 0: Navigation (NextMode, PrevMode, ZoomWindow, CycleFocus)
	if len(full[0]) != 4 {
		t.Errorf("FullHelp group 0 has %d bindings, want 4", len(full[0]))
	}

	// Group 1: Actions (StartSynthesis, ForceSynthesis, Export, Copy)
	if len(full[1]) != 4 {
		t.Errorf("FullHelp group 1 has %d bindings, want 4", len(full[1]))
	}

	// Group 2: General (Help, Refresh, Quit) + JumpWindow (9)
	if len(full[2]) != 12 {
		t.Errorf("FullHelp group 2 has %d bindings, want 12 (3 general + 9 jump)", len(full[2]))
	}
}

func TestEnsembleKeyMap_FullHelp_GroupContents(t *testing.T) {

	km := DefaultEnsembleKeyMap()
	full := km.FullHelp()

	// Group 0: Navigation bindings
	navExpected := []string{"j", "k", "enter", "tab"}
	for i, binding := range full[0] {
		keys := binding.Keys()
		if len(keys) == 0 {
			t.Errorf("FullHelp nav[%d] has no keys", i)
			continue
		}
		if keys[0] != navExpected[i] {
			t.Errorf("FullHelp nav[%d] first key = %q, want %q", i, keys[0], navExpected[i])
		}
	}

	// Group 1: Action bindings
	actExpected := []string{"s", "S", "e", "c"}
	for i, binding := range full[1] {
		keys := binding.Keys()
		if len(keys) == 0 {
			t.Errorf("FullHelp action[%d] has no keys", i)
			continue
		}
		if keys[0] != actExpected[i] {
			t.Errorf("FullHelp action[%d] first key = %q, want %q", i, keys[0], actExpected[i])
		}
	}

	// Group 2: first 3 are Help, Refresh, Quit
	genExpected := []string{"?", "r", "q"}
	for i, binding := range full[2][:3] {
		keys := binding.Keys()
		if len(keys) == 0 {
			t.Errorf("FullHelp general[%d] has no keys", i)
			continue
		}
		if keys[0] != genExpected[i] {
			t.Errorf("FullHelp general[%d] first key = %q, want %q", i, keys[0], genExpected[i])
		}
	}

	// Group 2[3:12]: jump window 1-9
	for i, binding := range full[2][3:] {
		expected := string(rune('0' + i + 1))
		keys := binding.Keys()
		if len(keys) == 0 || keys[0] != expected {
			t.Errorf("FullHelp jump[%d] key = %v, want %q", i, keys, expected)
		}
	}
}

func TestJumpWindowBindings_Count(t *testing.T) {

	bindings := jumpWindowBindings()
	if len(bindings) != 9 {
		t.Fatalf("jumpWindowBindings returned %d, want 9", len(bindings))
	}
}

func TestEnsembleKeyMap_NoDuplicateKeys(t *testing.T) {

	km := DefaultEnsembleKeyMap()

	// Collect all primary keys (first key of each binding)
	seen := make(map[string]string)
	bindings := map[string]key.Binding{
		"Quit":           km.Quit,
		"Help":           km.Help,
		"Refresh":        km.Refresh,
		"StartSynthesis": km.StartSynthesis,
		"ForceSynthesis": km.ForceSynthesis,
		"Export":         km.Export,
		"Copy":           km.Copy,
	}

	for name, binding := range bindings {
		for _, k := range binding.Keys() {
			if prev, exists := seen[k]; exists {
				t.Errorf("duplicate key %q: used by %s and %s", k, prev, name)
			}
			seen[k] = name
		}
	}
}
