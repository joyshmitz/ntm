package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/Dicklesworthstone/ntm/internal/ensemble"
)

func TestNewEnsemblePresetsCmd(t *testing.T) {
	t.Log("TEST: TestNewEnsemblePresetsCmd - starting")

	cmd := newEnsemblePresetsCmd()
	if cmd == nil {
		t.Fatal("newEnsemblePresetsCmd returned nil")
	}

	// Check command basics
	if cmd.Use != "presets" {
		t.Errorf("expected Use='presets', got %q", cmd.Use)
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "list" {
		t.Errorf("expected alias 'list', got %v", cmd.Aliases)
	}

	// Check flags exist
	if f := cmd.Flag("format"); f == nil {
		t.Error("missing --format flag")
	}
	if f := cmd.Flag("verbose"); f == nil {
		t.Error("missing --verbose flag")
	}
	if f := cmd.Flag("tag"); f == nil {
		t.Error("missing --tag flag")
	}
	if f := cmd.Flag("imported"); f == nil {
		t.Error("missing --imported flag")
	}

	t.Log("TEST: TestNewEnsemblePresetsCmd - assertion: command created with correct flags")
}

func TestRunEnsemblePresets_Table(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_Table - starting")

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:  "table",
		Verbose: false,
	}

	err := runEnsemblePresets(&buf, opts)
	if err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	output := buf.String()
	t.Logf("TEST: TestRunEnsemblePresets_Table - output: %s", output[:min(200, len(output))])

	// Verify table header
	if !strings.Contains(output, "NAME") {
		t.Error("table output missing NAME header")
	}
	if !strings.Contains(output, "DISPLAY") {
		t.Error("table output missing DISPLAY header")
	}

	// Verify embedded ensembles appear
	if !strings.Contains(output, "project-diagnosis") {
		t.Error("table output missing embedded ensemble 'project-diagnosis'")
	}
	if !strings.Contains(output, "idea-forge") {
		t.Error("table output missing embedded ensemble 'idea-forge'")
	}

	// Verify footer
	if !strings.Contains(output, "Total:") {
		t.Error("table output missing total count")
	}

	t.Log("TEST: TestRunEnsemblePresets_Table - assertion: table output is well-formed")
}

func TestRunEnsemblePresets_JSON(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_JSON - starting")

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:  "json",
		Verbose: false,
	}

	err := runEnsemblePresets(&buf, opts)
	if err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	var result ensemblePresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	t.Logf("TEST: TestRunEnsemblePresets_JSON - parsed %d presets", result.Count)

	// Verify count matches presets
	if result.Count != len(result.Presets) {
		t.Errorf("count mismatch: count=%d, presets=%d", result.Count, len(result.Presets))
	}

	// Verify at least embedded ensembles are present
	if result.Count < 9 {
		t.Errorf("expected at least 9 embedded ensembles, got %d", result.Count)
	}

	// Verify structure of first preset
	if len(result.Presets) > 0 {
		p := result.Presets[0]
		if p.Name == "" {
			t.Error("preset has empty name")
		}
		if p.ModeCount == 0 {
			t.Error("preset has zero modes")
		}
		if p.Source == "" {
			t.Error("preset has empty source")
		}
	}

	t.Log("TEST: TestRunEnsemblePresets_JSON - assertion: JSON output is valid and complete")
}

func TestRunEnsemblePresets_Verbose(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_Verbose - starting")

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:  "table",
		Verbose: true,
	}

	err := runEnsemblePresets(&buf, opts)
	if err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	output := buf.String()

	// Verbose output should contain more details
	if !strings.Contains(output, "Modes (") {
		t.Error("verbose output missing mode list")
	}
	if !strings.Contains(output, "Synthesis:") {
		t.Error("verbose output missing synthesis section")
	}
	if !strings.Contains(output, "Budget:") {
		t.Error("verbose output missing budget section")
	}
	if !strings.Contains(output, "Min Confidence:") {
		t.Error("verbose output missing min confidence")
	}
	if !strings.Contains(output, "Tokens/Mode:") {
		t.Error("verbose output missing tokens per mode")
	}

	t.Log("TEST: TestRunEnsemblePresets_Verbose - assertion: verbose output includes detailed sections")
}

func TestRunEnsemblePresets_TagFilter(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_TagFilter - starting")

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:  "json",
		Verbose: false,
		Tag:     "debugging",
	}

	err := runEnsemblePresets(&buf, opts)
	if err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	var result ensemblePresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	t.Logf("TEST: TestRunEnsemblePresets_TagFilter - found %d presets with tag 'debugging'", result.Count)

	// Should have at least bug-hunt and root-cause-analysis
	if result.Count < 2 {
		t.Errorf("expected at least 2 presets with 'debugging' tag, got %d", result.Count)
	}

	// All returned presets should have the debugging tag
	for _, p := range result.Presets {
		hasTag := false
		for _, tag := range p.Tags {
			if tag == "debugging" {
				hasTag = true
				break
			}
		}
		if !hasTag {
			t.Errorf("preset %q doesn't have 'debugging' tag: %v", p.Name, p.Tags)
		}
	}

	t.Log("TEST: TestRunEnsemblePresets_TagFilter - assertion: tag filter works correctly")
}

func TestRunEnsemblePresets_VerboseJSON(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_VerboseJSON - starting")

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:  "json",
		Verbose: true,
	}

	err := runEnsemblePresets(&buf, opts)
	if err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	var result ensemblePresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	// In verbose mode, Details should be populated, not Presets
	if len(result.Details) == 0 {
		t.Error("verbose JSON mode should populate details, not presets")
	}

	// Verify structure of first detail
	if len(result.Details) > 0 {
		d := result.Details[0]
		if len(d.Modes) == 0 {
			t.Error("detail has empty modes")
		}
		if d.Synthesis.Strategy == "" {
			t.Error("detail has empty synthesis strategy")
		}
		if d.Budget.MaxTotalTokens == 0 {
			t.Error("detail has zero max total tokens")
		}

		// Check mode details
		if len(d.Modes) > 0 {
			m := d.Modes[0]
			if m.ID == "" {
				t.Error("mode detail has empty ID")
			}
			// Should have resolved mode info
			if m.Code == "" || m.Name == "" {
				t.Log("TEST: mode details may not have full resolution if catalog unavailable")
			}
		}
	}

	t.Log("TEST: TestRunEnsemblePresets_VerboseJSON - assertion: verbose JSON includes full details")
}

func TestRenderPresetsTable_Empty(t *testing.T) {
	t.Log("TEST: TestRenderPresetsTable_Empty - starting")

	var buf bytes.Buffer
	err := renderPresetsTable(&buf, []ensemblePresetRow{})
	if err != nil {
		t.Fatalf("renderPresetsTable failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No ensemble presets found") {
		t.Error("empty table should show 'No ensemble presets found'")
	}

	t.Log("TEST: TestRenderPresetsTable_Empty - assertion: empty list handled correctly")
}

func TestRenderPresetsTableVerbose_Empty(t *testing.T) {
	t.Log("TEST: TestRenderPresetsTableVerbose_Empty - starting")

	var buf bytes.Buffer
	err := renderPresetsTableVerbose(&buf, []ensemblePresetDetail{})
	if err != nil {
		t.Fatalf("renderPresetsTableVerbose failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No ensemble presets found") {
		t.Error("empty verbose table should show 'No ensemble presets found'")
	}

	t.Log("TEST: TestRenderPresetsTableVerbose_Empty - assertion: empty list handled correctly")
}

func TestRunEnsemblePresets_ImportedOnly(t *testing.T) {
	t.Log("TEST: TestRunEnsemblePresets_ImportedOnly - starting")

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ensemble.ResetGlobalEnsembleRegistry()
	ensemble.ResetGlobalCatalog()

	importedPath := ensemble.ImportedEnsemblesPath("")
	if err := os.MkdirAll(filepath.Dir(importedPath), 0o755); err != nil {
		t.Fatalf("mkdir imported dir: %v", err)
	}

	imported := ensemble.EnsemblePreset{
		Name:        "imported-test",
		Description: "imported preset",
		Modes: []ensemble.ModeRef{
			ensemble.ModeRefFromID("systems-thinking"),
			ensemble.ModeRefFromID("worst-case"),
		},
	}
	var fileBuf bytes.Buffer
	type importFile struct {
		Ensembles []ensemble.EnsemblePreset `toml:"ensembles"`
	}
	if err := toml.NewEncoder(&fileBuf).Encode(importFile{Ensembles: []ensemble.EnsemblePreset{imported}}); err != nil {
		t.Fatalf("encode imported toml: %v", err)
	}
	if err := os.WriteFile(importedPath, fileBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("write imported file: %v", err)
	}

	var buf bytes.Buffer
	opts := ensemblePresetsOptions{
		Format:       "json",
		Verbose:      false,
		ImportedOnly: true,
	}
	if err := runEnsemblePresets(&buf, opts); err != nil {
		t.Fatalf("runEnsemblePresets failed: %v", err)
	}

	var result ensemblePresetsOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if result.Count != 1 {
		t.Fatalf("expected 1 imported preset, got %d", result.Count)
	}
	if len(result.Presets) != 1 || result.Presets[0].Name != "imported-test" {
		t.Fatalf("unexpected imported preset list: %+v", result.Presets)
	}

	t.Log("TEST: TestRunEnsemblePresets_ImportedOnly - assertion: imported filter works")
}

func TestEnsembleExportImport_RoundTrip(t *testing.T) {
	t.Log("TEST: TestEnsembleExportImport_RoundTrip - starting")

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ensemble.ResetGlobalEnsembleRegistry()
	ensemble.ResetGlobalCatalog()

	outputFile := filepath.Join(t.TempDir(), "project-diagnosis.toml")

	var exportBuf bytes.Buffer
	if err := runEnsembleExport(&exportBuf, "project-diagnosis", ensembleExportOptions{Output: outputFile, Force: true}); err != nil {
		t.Fatalf("runEnsembleExport failed: %v", err)
	}
	if _, err := os.Stat(outputFile); err != nil {
		t.Fatalf("expected export file to exist: %v", err)
	}

	adjustedFile := filepath.Join(t.TempDir(), "project-diagnosis-import.toml")
	var payload ensemble.EnsembleExport
	if _, err := toml.DecodeFile(outputFile, &payload); err != nil {
		t.Fatalf("decode export file: %v", err)
	}
	payload.Name = "project-diagnosis-import"
	f, err := os.Create(adjustedFile)
	if err != nil {
		t.Fatalf("create adjusted export: %v", err)
	}
	if err := toml.NewEncoder(f).Encode(payload); err != nil {
		_ = f.Close()
		t.Fatalf("encode adjusted export: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close adjusted export: %v", err)
	}

	var importBuf bytes.Buffer
	if err := runEnsembleImport(&importBuf, adjustedFile, ensembleImportOptions{}); err != nil {
		t.Fatalf("runEnsembleImport failed: %v", err)
	}

	ensemble.ResetGlobalEnsembleRegistry()
	registry, err := ensemble.GlobalEnsembleRegistry()
	if err != nil {
		t.Fatalf("GlobalEnsembleRegistry failed: %v", err)
	}
	preset := registry.Get("project-diagnosis-import")
	if preset == nil {
		t.Fatal("expected imported preset to be present")
	}
	if preset.Source != "imported" {
		t.Fatalf("expected imported source, got %q", preset.Source)
	}

	t.Log("TEST: TestEnsembleExportImport_RoundTrip - assertion: export/import works")
}

func TestRunEnsembleImport_RejectsUnknownFields(t *testing.T) {
	t.Log("TEST: TestRunEnsembleImport_RejectsUnknownFields - starting")

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ensemble.ResetGlobalEnsembleRegistry()
	ensemble.ResetGlobalCatalog()

	registry, err := ensemble.GlobalEnsembleRegistry()
	if err != nil {
		t.Fatalf("GlobalEnsembleRegistry failed: %v", err)
	}
	preset := registry.Get("project-diagnosis")
	if preset == nil {
		t.Fatal("expected embedded preset for export")
	}
	payload := ensemble.ExportFromPreset(*preset)
	payload.Name = "project-diagnosis-import"
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(payload); err != nil {
		t.Fatalf("encode export: %v", err)
	}
	data := append(buf.Bytes(), []byte("\nlegacy = true\n")...)
	path := filepath.Join(t.TempDir(), "bad-import.toml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	err = runEnsembleImport(&bytes.Buffer{}, path, ensembleImportOptions{})
	if err == nil {
		t.Fatal("expected error for unknown TOML field")
	}
	if !strings.Contains(err.Error(), "unknown field(s):") || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("expected unknown field error, got %v", err)
	}

	t.Log("TEST: TestRunEnsembleImport_RejectsUnknownFields - assertion: unknown fields fail early")
}

func TestRunEnsembleImport_RemoteChecksum(t *testing.T) {
	t.Log("TEST: TestRunEnsembleImport_RemoteChecksum - starting")

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ensemble.ResetGlobalEnsembleRegistry()
	ensemble.ResetGlobalCatalog()

	registry, err := ensemble.GlobalEnsembleRegistry()
	if err != nil {
		t.Fatalf("GlobalEnsembleRegistry failed: %v", err)
	}
	preset := registry.Get("project-diagnosis")
	if preset == nil {
		t.Fatal("expected embedded preset for export")
	}
	payload := ensemble.ExportFromPreset(*preset)
	payload.Name = "project-diagnosis-import"
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(payload); err != nil {
		t.Fatalf("encode export: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	checksum := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()

	if err := runEnsembleImport(&bytes.Buffer{}, server.URL, ensembleImportOptions{}); err == nil {
		t.Fatal("expected error when importing remote without allow-remote")
	}

	if err := runEnsembleImport(&bytes.Buffer{}, server.URL, ensembleImportOptions{AllowRemote: true, SHA256: "deadbeef"}); err == nil {
		t.Fatal("expected checksum mismatch error for remote import")
	}

	if err := runEnsembleImport(&bytes.Buffer{}, server.URL, ensembleImportOptions{AllowRemote: true, SHA256: checksum}); err != nil {
		t.Fatalf("remote import failed: %v", err)
	}

	t.Log("TEST: TestRunEnsembleImport_RemoteChecksum - assertion: checksum enforcement works")
}
