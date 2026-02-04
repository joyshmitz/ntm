package ensemble

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsembleLoader_MergePrecedence(t *testing.T) {
	userDir := t.TempDir()
	projectDir := t.TempDir()

	userFile := filepath.Join(userDir, "ensembles.toml")
	importedFile := filepath.Join(userDir, "ensembles.imported.toml")
	projectFile := filepath.Join(projectDir, ".ntm", "ensembles.toml")
	if err := os.MkdirAll(filepath.Dir(projectFile), 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}

	importedToml := `[[ensembles]]
name = "diagnosis"
description = "imported override"

  [[ensembles.modes]]
  id = "deductive"

  [[ensembles.modes]]
  id = "abductive"

[[ensembles]]
name = "imported-only"
description = "imported only preset"

  [[ensembles.modes]]
  id = "deductive"

  [[ensembles.modes]]
  id = "practical"
`

	userToml := `[[ensembles]]
name = "diagnosis"
description = "user override"

  [[ensembles.modes]]
  id = "deductive"

  [[ensembles.modes]]
  id = "abductive"
`

	projectToml := `[[ensembles]]
name = "diagnosis"
description = "project override"

  [[ensembles.modes]]
  id = "deductive"

  [[ensembles.modes]]
  id = "practical"
`

	if err := os.WriteFile(importedFile, []byte(importedToml), 0o644); err != nil {
		t.Fatalf("write imported toml: %v", err)
	}
	if err := os.WriteFile(userFile, []byte(userToml), 0o644); err != nil {
		t.Fatalf("write user toml: %v", err)
	}
	if err := os.WriteFile(projectFile, []byte(projectToml), 0o644); err != nil {
		t.Fatalf("write project toml: %v", err)
	}

	loader := &EnsembleLoader{
		UserConfigDir: userDir,
		ProjectDir:    projectDir,
		ModeCatalog:   nil,
	}

	presets, err := loader.Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	found := false
	importedFound := false
	for _, preset := range presets {
		if preset.Name == "diagnosis" {
			found = true
			if preset.Source != "project" {
				t.Fatalf("preset source = %q, want project", preset.Source)
			}
			if preset.Description != "project override" {
				t.Fatalf("preset description = %q, want project override", preset.Description)
			}
		}
		if preset.Name == "imported-only" {
			importedFound = true
			if preset.Source != "imported" {
				t.Fatalf("imported preset source = %q, want imported", preset.Source)
			}
		}
	}
	if !found {
		t.Fatal("expected diagnosis preset in merged list")
	}
	if !importedFound {
		t.Fatal("expected imported-only preset in merged list")
	}
}

func TestEnsembleLoader_MissingFilesOk(t *testing.T) {
	loader := &EnsembleLoader{
		UserConfigDir: t.TempDir(),
		ProjectDir:    t.TempDir(),
		ModeCatalog:   nil,
	}

	if _, err := loader.Load(); err != nil {
		t.Fatalf("Load error: %v", err)
	}
}

func TestNewEnsembleLoader_Defaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	loader := NewEnsembleLoader(nil)
	if loader.UserConfigDir == "" {
		t.Fatal("expected UserConfigDir to be set")
	}
	if loader.ProjectDir == "" {
		t.Fatal("expected ProjectDir to be set")
	}
	if loader.ModeCatalog != nil {
		t.Fatal("expected ModeCatalog to be nil")
	}
}

func TestLoadEnsembles_Defaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	presets, err := LoadEnsembles(nil)
	if err != nil {
		t.Fatalf("LoadEnsembles error: %v", err)
	}
	if len(presets) == 0 {
		t.Fatal("expected embedded ensembles to be loaded")
	}
}

func TestGlobalEnsembleRegistry_Reset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ResetGlobalEnsembleRegistry()
	reg1, err := GlobalEnsembleRegistry()
	if err == nil && reg1 == nil {
		t.Fatal("expected registry or error from GlobalEnsembleRegistry")
	}
	ResetGlobalEnsembleRegistry()
	reg2, err := GlobalEnsembleRegistry()
	if err == nil && reg2 == nil {
		t.Fatal("expected registry or error from GlobalEnsembleRegistry")
	}
}
