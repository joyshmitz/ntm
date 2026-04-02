package ensemble

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsembleLoader_MergesSourcesWithPrecedence(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "user")
	projectDir := filepath.Join(tmp, "project")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".ntm"), 0o755); err != nil {
		t.Fatalf("mkdir project .ntm dir: %v", err)
	}

	userConfig := `
[[ensembles]]
name = "project-diagnosis"
display_name = "User Override"
description = "user override"
modes = [{id = "deductive"}, {id = "abductive"}]

[[ensembles]]
name = "custom-user"
display_name = "Custom User"
description = "user custom"
modes = [{id = "deductive"}, {id = "abductive"}]
`
	projectConfig := `
[[ensembles]]
name = "project-diagnosis"
display_name = "Project Override"
description = "project override"
modes = [{id = "deductive"}, {id = "abductive"}]

[[ensembles]]
name = "custom-project"
display_name = "Custom Project"
description = "project custom"
modes = [{id = "deductive"}, {id = "abductive"}]
`

	if err := os.WriteFile(filepath.Join(userDir, "ensembles.toml"), []byte(userConfig), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".ntm", "ensembles.toml"), []byte(projectConfig), 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	loader := &EnsembleLoader{
		UserConfigDir: userDir,
		ProjectDir:    projectDir,
		ModeCatalog:   nil, // skip validation for merge/precedence tests
	}

	presets, err := loader.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	projectOverride := findPreset(t, presets, "project-diagnosis")
	if projectOverride.DisplayName != "Project Override" {
		t.Fatalf("expected project override display name, got %q", projectOverride.DisplayName)
	}
	if projectOverride.Source != "project" {
		t.Fatalf("expected project override source, got %q", projectOverride.Source)
	}

	userPreset := findPreset(t, presets, "custom-user")
	if userPreset.Source != "user" {
		t.Fatalf("expected user preset source, got %q", userPreset.Source)
	}

	projectPreset := findPreset(t, presets, "custom-project")
	if projectPreset.Source != "project" {
		t.Fatalf("expected project preset source, got %q", projectPreset.Source)
	}
}

func TestEnsembleLoader_InvalidToml(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	badConfig := `[[ensembles
name = "broken"`
	if err := os.WriteFile(filepath.Join(userDir, "ensembles.toml"), []byte(badConfig), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	loader := &EnsembleLoader{
		UserConfigDir: userDir,
		ProjectDir:    tmp,
		ModeCatalog:   nil,
	}

	if _, err := loader.Load(); err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestEnsembleLoader_RejectsUnknownFields(t *testing.T) {
	tmp := t.TempDir()
	userDir := filepath.Join(tmp, "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("mkdir user dir: %v", err)
	}

	badConfig := `[[ensembles]]
name = "project-diagnosis"
description = "bad field"
legacy = true
modes = [{id = "deductive"}, {id = "abductive"}]
`
	if err := os.WriteFile(filepath.Join(userDir, "ensembles.toml"), []byte(badConfig), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	loader := &EnsembleLoader{
		UserConfigDir: userDir,
		ProjectDir:    tmp,
		ModeCatalog:   nil,
	}

	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for unknown TOML field")
	}
	if !strings.Contains(err.Error(), "unknown field(s): ensembles.legacy") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func findPreset(t *testing.T, presets []EnsemblePreset, name string) EnsemblePreset {
	t.Helper()
	for _, preset := range presets {
		if preset.Name == name {
			return preset
		}
	}
	t.Fatalf("preset %q not found", name)
	return EnsemblePreset{}
}
