package checkpoint

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Helpers for building test archives
// =============================================================================

// buildTarGz creates a tar.gz archive from a map of filename→content.
func buildTarGz(t *testing.T, destPath string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	for name, data := range files {
		hdr := &tar.Header{
			Name:    name,
			Mode:    0644,
			Size:    int64(len(data)),
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}
}

// buildZip creates a zip archive from a map of filename→content.
func buildZip(t *testing.T, destPath string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
}

// validCheckpointJSON returns a minimal valid checkpoint JSON blob.
func validCheckpointJSON(t *testing.T, sessionName, cpID string) []byte {
	t.Helper()
	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		WorkingDir:  "/tmp/test",
		CreatedAt:   time.Now(),
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		t.Fatalf("marshal checkpoint: %v", err)
	}
	return data
}

// =============================================================================
// Import: unknown format
// =============================================================================

func TestImport_UnknownFormat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	// Create a dummy file with unsupported extension
	archivePath := filepath.Join(tmpDir, "archive.rar")
	if err := os.WriteFile(archivePath, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := storage.Import(archivePath, ImportOptions{})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "unknown archive format") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Export: unsupported format
// =============================================================================

func TestExport_UnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "test-cp",
		SessionName: "test-session",
		WorkingDir:  "/tmp/test",
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(cp); err != nil {
		t.Fatal(err)
	}

	_, err := storage.Export("test-session", "test-cp", "out.bad", ExportOptions{
		Format: ExportFormat("unsupported"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported export format") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Export: auto-generated dest path
// =============================================================================

func TestExport_AutoDestPath_TarGz(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "auto-dest-session"
	cpID := "auto-dest-cp"
	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(cp); err != nil {
		t.Fatal(err)
	}

	// Change to tmpDir so the auto-generated file goes there
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	manifest, err := storage.Export(sessionName, cpID, "", DefaultExportOptions())
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if manifest == nil {
		t.Fatal("manifest is nil")
	}

	// Check that auto-generated file was created with .tar.gz extension
	autoPath := filepath.Join(tmpDir, sessionName+"_"+cpID+".tar.gz")
	if _, err := os.Stat(autoPath); err != nil {
		t.Errorf("auto-generated archive not found at %s: %v", autoPath, err)
	}
}

func TestExport_AutoDestPath_Zip(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "auto-dest-zip"
	cpID := "auto-dest-zip-cp"
	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(cp); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	opts := DefaultExportOptions()
	opts.Format = FormatZip
	manifest, err := storage.Export(sessionName, cpID, "", opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}
	if manifest == nil {
		t.Fatal("manifest is nil")
	}

	autoPath := filepath.Join(tmpDir, sessionName+"_"+cpID+".zip")
	if _, err := os.Stat(autoPath); err != nil {
		t.Errorf("auto-generated zip not found at %s: %v", autoPath, err)
	}
}

// =============================================================================
// Import tar.gz: missing metadata.json
// =============================================================================

func TestImportTarGz_MissingMetadata(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "no-meta.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		"MANIFEST.json": []byte(`{"version":1}`),
		"other.txt":     []byte("data"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
	if !strings.Contains(err.Error(), "archive missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import zip: missing metadata.json
// =============================================================================

func TestImportZip_MissingMetadata(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "no-meta.zip")
	buildZip(t, archive, map[string][]byte{
		"MANIFEST.json": []byte(`{"version":1}`),
		"other.txt":     []byte("data"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for missing metadata")
	}
	if !strings.Contains(err.Error(), "archive missing") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import tar.gz: checksum mismatch
// =============================================================================

func TestImportTarGz_ChecksumMismatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "chk-session", "chk-cp-id")

	// Build manifest with wrong checksum
	manifest := &ExportManifest{
		Version:     1,
		SessionName: "chk-session",
		Checksums: map[string]string{
			MetadataFile: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	manifestJSON, _ := json.Marshal(manifest)

	archive := filepath.Join(tmpDir, "bad-checksum.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile:    cpJSON,
		"MANIFEST.json": manifestJSON,
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: true})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import zip: checksum mismatch
// =============================================================================

func TestImportZip_ChecksumMismatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "zip-chk-session", "zip-chk-cp")

	manifest := &ExportManifest{
		Version:     1,
		SessionName: "zip-chk-session",
		Checksums: map[string]string{
			MetadataFile: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
	}
	manifestJSON, _ := json.Marshal(manifest)

	archive := filepath.Join(tmpDir, "bad-checksum.zip")
	buildZip(t, archive, map[string][]byte{
		MetadataFile:    cpJSON,
		"MANIFEST.json": manifestJSON,
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: true})
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import: manifest lists file not in archive
// =============================================================================

func TestImportTarGz_ManifestListsMissingFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "miss-session", "miss-cp")

	manifest := &ExportManifest{
		Version:     1,
		SessionName: "miss-session",
		Checksums: map[string]string{
			MetadataFile:       sha256sum(cpJSON),
			"nonexistent.file": "deadbeef",
		},
	}
	manifestJSON, _ := json.Marshal(manifest)

	archive := filepath.Join(tmpDir, "missing-file.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile:    cpJSON,
		"MANIFEST.json": manifestJSON,
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: true})
	if err == nil {
		t.Fatal("expected error for missing file referenced in manifest")
	}
	if !strings.Contains(err.Error(), "manifest lists missing file") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import tar.gz: overwrite protection
// =============================================================================

func TestImportTarGz_OverwriteProtection(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "ow-session"
	cpID := "ow-cp-id"

	// Save a checkpoint first so the directory exists
	existing := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(existing); err != nil {
		t.Fatal(err)
	}

	// Build an archive for the same checkpoint
	cpJSON := validCheckpointJSON(t, sessionName, cpID)
	archive := filepath.Join(tmpDir, "overwrite.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile: cpJSON,
	})

	// Import without AllowOverwrite should fail
	_, err := storage.Import(archive, ImportOptions{
		VerifyChecksums: false,
		AllowOverwrite:  false,
	})
	if err == nil {
		t.Fatal("expected error when overwriting without AllowOverwrite")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}

	// Import with AllowOverwrite should succeed
	_, err = storage.Import(archive, ImportOptions{
		VerifyChecksums: false,
		AllowOverwrite:  true,
	})
	if err != nil {
		t.Fatalf("import with AllowOverwrite failed: %v", err)
	}
}

// =============================================================================
// Import zip: overwrite protection and AllowOverwrite
// =============================================================================

func TestImportZip_OverwriteProtection(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "ow-zip-session"
	cpID := "ow-zip-cp"

	existing := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(existing); err != nil {
		t.Fatal(err)
	}

	cpJSON := validCheckpointJSON(t, sessionName, cpID)
	archive := filepath.Join(tmpDir, "overwrite.zip")
	buildZip(t, archive, map[string][]byte{
		MetadataFile: cpJSON,
	})

	// Without AllowOverwrite
	_, err := storage.Import(archive, ImportOptions{
		VerifyChecksums: false,
		AllowOverwrite:  false,
	})
	if err == nil {
		t.Fatal("expected error when overwriting without AllowOverwrite")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}

	// With AllowOverwrite
	_, err = storage.Import(archive, ImportOptions{
		VerifyChecksums: false,
		AllowOverwrite:  true,
	})
	if err != nil {
		t.Fatalf("import with AllowOverwrite failed: %v", err)
	}
}

// =============================================================================
// Import tar.gz: path traversal protection
// =============================================================================

func TestImportTarGz_PathTraversal(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "trav-session", "trav-cp")

	archive := filepath.Join(tmpDir, "traversal.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile:                  cpJSON,
		"../../../etc/evil-file.conf": []byte("pwned"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import zip: path traversal protection
// =============================================================================

func TestImportZip_PathTraversal(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "ztrav-session", "ztrav-cp")

	archive := filepath.Join(tmpDir, "traversal.zip")
	buildZip(t, archive, map[string][]byte{
		MetadataFile:                  cpJSON,
		"../../../etc/evil-file.conf": []byte("pwned"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for path traversal in zip")
	}
	if !strings.Contains(err.Error(), "path traversal") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import tar.gz: corrupt checkpoint JSON
// =============================================================================

func TestImportTarGz_CorruptCheckpointJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "corrupt.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile: []byte("{invalid json"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse checkpoint") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import zip: corrupt checkpoint JSON
// =============================================================================

func TestImportZip_CorruptCheckpointJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "corrupt.zip")
	buildZip(t, archive, map[string][]byte{
		MetadataFile: []byte("{invalid json"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for corrupt JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse checkpoint") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import tar.gz: corrupt manifest JSON
// =============================================================================

func TestImportTarGz_CorruptManifestJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "mf-session", "mf-cp")

	archive := filepath.Join(tmpDir, "corrupt-manifest.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile:    cpJSON,
		"MANIFEST.json": []byte("{bad json"),
	})

	_, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err == nil {
		t.Fatal("expected error for corrupt manifest")
	}
	if !strings.Contains(err.Error(), "failed to parse manifest") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import: WorkingDir placeholder expansion
// =============================================================================

func TestImportTarGz_WorkingDirPlaceholder(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "wd-cp",
		SessionName: "wd-session",
		WorkingDir:  "${WORKING_DIR}",
		CreatedAt:   time.Now(),
	}
	cpJSON, _ := json.MarshalIndent(cp, "", "  ")

	archive := filepath.Join(tmpDir, "working-dir.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile: cpJSON,
	})

	imported, err := storage.Import(archive, ImportOptions{VerifyChecksums: false})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	cwd, _ := os.Getwd()
	if imported.WorkingDir != cwd {
		t.Errorf("WorkingDir = %q, want current dir %q", imported.WorkingDir, cwd)
	}
}

// =============================================================================
// Import tar.gz: TargetSession override
// =============================================================================

func TestImportTarGz_TargetSession(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "orig-session", "ts-cp")

	archive := filepath.Join(tmpDir, "target-session.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile: cpJSON,
	})

	imported, err := storage.Import(archive, ImportOptions{
		VerifyChecksums: false,
		TargetSession:   "new-session",
		TargetDir:       "/new/dir",
	})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	if imported.SessionName != "new-session" {
		t.Errorf("SessionName = %q, want new-session", imported.SessionName)
	}
	if imported.WorkingDir != "/new/dir" {
		t.Errorf("WorkingDir = %q, want /new/dir", imported.WorkingDir)
	}
}

// =============================================================================
// Import: session name from manifest takes precedence
// =============================================================================

func TestImportTarGz_SessionNameFromManifest(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cpJSON := validCheckpointJSON(t, "cp-session", "mn-cp")

	manifest := &ExportManifest{
		Version:     1,
		SessionName: "manifest-session",
		Checksums:   map[string]string{MetadataFile: sha256sum(cpJSON)},
	}
	manifestJSON, _ := json.Marshal(manifest)

	archive := filepath.Join(tmpDir, "manifest-name.tar.gz")
	buildTarGz(t, archive, map[string][]byte{
		MetadataFile:    cpJSON,
		"MANIFEST.json": manifestJSON,
	})

	imported, err := storage.Import(archive, ImportOptions{VerifyChecksums: true})
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// The checkpoint dir should use the manifest session name
	cpDir := storage.CheckpointDir("manifest-session", "mn-cp")
	if _, err := os.Stat(cpDir); err != nil {
		t.Errorf("checkpoint not stored under manifest session name: %v", err)
	}
	_ = imported
}

// =============================================================================
// checkFiles: missing metadata.json and session.json
// =============================================================================

func TestCheckFiles_MissingMetadataAndSession(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	// Create an empty checkpoint directory (no files inside)
	cpDir := filepath.Join(tmpDir, "test-session", "test-cp")
	if err := os.MkdirAll(cpDir, 0755); err != nil {
		t.Fatal(err)
	}

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "test-cp",
		SessionName: "test-session",
	}

	result := &IntegrityResult{
		FilesPresent: true,
		Errors:       []string{},
		Details:      make(map[string]string),
	}
	cp.checkFiles(storage, cpDir, result)

	if result.FilesPresent {
		t.Error("FilesPresent should be false when metadata.json and session.json are missing")
	}
	if len(result.Errors) < 2 {
		t.Errorf("expected at least 2 errors, got %d: %v", len(result.Errors), result.Errors)
	}

	foundMeta := false
	foundSession := false
	for _, e := range result.Errors {
		if strings.Contains(e, "metadata.json") {
			foundMeta = true
		}
		if strings.Contains(e, "session.json") {
			foundSession = true
		}
	}
	if !foundMeta {
		t.Error("expected error about missing metadata.json")
	}
	if !foundSession {
		t.Error("expected error about missing session.json")
	}
}

// =============================================================================
// checkFiles: missing git patch
// =============================================================================

func TestCheckFiles_MissingGitPatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "patch-session"
	cpID := "patch-cp"

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          cpID,
		SessionName: sessionName,
		CreatedAt:   time.Now(),
		Git: GitState{
			PatchFile: "changes.patch",
		},
	}

	if err := storage.Save(cp); err != nil {
		t.Fatal(err)
	}

	result := &IntegrityResult{
		FilesPresent: true,
		Errors:       []string{},
		Details:      make(map[string]string),
	}
	dir := storage.CheckpointDir(sessionName, cpID)
	cp.checkFiles(storage, dir, result)

	if result.FilesPresent {
		t.Error("FilesPresent should be false with missing git patch")
	}

	foundPatch := false
	for _, e := range result.Errors {
		if strings.Contains(e, "missing git patch") {
			foundPatch = true
		}
	}
	if !foundPatch {
		t.Errorf("expected error about missing git patch, got: %v", result.Errors)
	}
}

// =============================================================================
// validateConsistency: dirty git with zero changes
// =============================================================================

func TestValidateConsistency_DirtyGitZeroChanges(t *testing.T) {
	t.Parallel()

	cp := &Checkpoint{
		Session: SessionState{
			Panes:           []PaneState{{ID: "%0", Index: 0, Width: 80, Height: 24}},
			ActivePaneIndex: 0,
		},
		PaneCount: 1,
		Git: GitState{
			IsDirty:        true,
			StagedCount:    0,
			UnstagedCount:  0,
			UntrackedCount: 0,
		},
	}

	result := &IntegrityResult{
		ConsistencyValid: true,
		Errors:           []string{},
		Warnings:         []string{},
		Details:          make(map[string]string),
	}
	cp.validateConsistency(result)

	if !result.ConsistencyValid {
		t.Error("should still be valid (warning only)")
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "dirty but no changes") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about dirty with no changes, got: %v", result.Warnings)
	}
}

// =============================================================================
// validateConsistency: pane with invalid dimensions
// =============================================================================

func TestValidateConsistency_InvalidPaneDimensions(t *testing.T) {
	t.Parallel()

	cp := &Checkpoint{
		Session: SessionState{
			Panes: []PaneState{
				{ID: "%0", Index: 0, Width: 0, Height: 0},
			},
			ActivePaneIndex: 0,
		},
		PaneCount: 1,
	}

	result := &IntegrityResult{
		ConsistencyValid: true,
		Errors:           []string{},
		Warnings:         []string{},
		Details:          make(map[string]string),
	}
	cp.validateConsistency(result)

	foundWarning := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "invalid dimensions") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Errorf("expected warning about invalid dimensions, got: %v", result.Warnings)
	}
}

// =============================================================================
// VerifyManifest: nil manifest
// =============================================================================

func TestVerifyManifest_NilManifest(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "nil-mf-cp",
		SessionName: "nil-mf-session",
	}

	result := cp.VerifyManifest(storage, nil)
	if !result.Valid {
		t.Error("should be valid with nil manifest")
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warning about no manifest")
	}
}

// =============================================================================
// VerifyManifest: empty manifest
// =============================================================================

func TestVerifyManifest_EmptyManifest(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "em-mf-cp",
		SessionName: "em-mf-session",
	}

	result := cp.VerifyManifest(storage, &FileManifest{Files: map[string]string{}})
	if !result.Valid {
		t.Error("should be valid with empty manifest")
	}
}

// =============================================================================
// VerifyManifest: missing file on disk
// =============================================================================

func TestVerifyManifest_MissingFileOnDisk(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          "miss-disk-cp",
		SessionName: "miss-disk-session",
	}

	manifest := &FileManifest{
		Files: map[string]string{
			"nonexistent.json": "abcdef1234567890",
		},
	}

	result := cp.VerifyManifest(storage, manifest)
	if result.Valid {
		t.Error("should be invalid with missing file")
	}
	if result.ChecksumsValid {
		t.Error("ChecksumsValid should be false")
	}

	foundMissing := false
	for _, e := range result.Errors {
		if strings.Contains(e, "file missing") {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("expected 'file missing' error, got: %v", result.Errors)
	}
}

// =============================================================================
// hashFile: nonexistent file
// =============================================================================

func TestHashFile_Nonexistent(t *testing.T) {
	t.Parallel()

	_, err := hashFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got: %v", err)
	}
}

// =============================================================================
// QuickCheck: multiple errors
// =============================================================================

func TestQuickCheck_MultipleErrors(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	cp := &Checkpoint{
		Version:     0,  // invalid
		ID:          "", // missing
		SessionName: "", // missing
	}

	err := cp.QuickCheck(storage)
	if err == nil {
		t.Fatal("expected error with multiple failures")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "unsupported version") {
		t.Error("expected version error in message")
	}
	if !strings.Contains(errMsg, "missing checkpoint ID") {
		t.Error("expected missing ID error in message")
	}
	if !strings.Contains(errMsg, "missing session_name") {
		t.Error("expected missing session_name error in message")
	}
}

// =============================================================================
// gzipDecompress: invalid data
// =============================================================================

func TestGzipDecompress_InvalidInput(t *testing.T) {
	t.Parallel()

	_, err := gzipDecompress([]byte("not gzip data"))
	if err == nil {
		t.Fatal("expected error for invalid gzip data")
	}
}

// =============================================================================
// LoadCompressedScrollback: nonexistent session
// =============================================================================

func TestLoadCompressedScrollback_NoFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	_, err := storage.LoadCompressedScrollback("no-session", "no-cp", "%0")
	if err == nil {
		t.Fatal("expected error for nonexistent scrollback")
	}
}

// =============================================================================
// LoadCompressedScrollback: corrupt compressed file
// =============================================================================

func TestLoadCompressedScrollback_CorruptGzipFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	sessionName := "corrupt-session"
	cpID := "corrupt-cp"
	paneID := "%0"

	// Create the panes directory and a corrupt .gz file
	panesDir := storage.PanesDirPath(sessionName, cpID)
	if err := os.MkdirAll(panesDir, 0755); err != nil {
		t.Fatal(err)
	}

	filename := "pane__0.txt.gz"
	corruptPath := filepath.Join(panesDir, filename)
	if err := os.WriteFile(corruptPath, []byte("not valid gzip"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := storage.LoadCompressedScrollback(sessionName, cpID, paneID)
	if err == nil {
		t.Fatal("expected error for corrupt gzip file")
	}
	if !strings.Contains(err.Error(), "decompressing") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// rotateAutoCheckpoints
// =============================================================================

func TestRotateAutoCheckpoints(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	auto := &AutoCheckpointer{storage: storage}
	sessionName := "rotate-session"

	// Create 5 auto-checkpoints
	for i := 0; i < 5; i++ {
		cp := &Checkpoint{
			Version:     CurrentVersion,
			ID:          GenerateID("test"),
			Name:        AutoCheckpointPrefix + "-test",
			SessionName: sessionName,
			CreatedAt:   time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := storage.Save(cp); err != nil {
			t.Fatal(err)
		}
	}

	// Verify we have 5
	checkpoints, _ := storage.List(sessionName)
	if len(checkpoints) != 5 {
		t.Fatalf("expected 5 checkpoints, got %d", len(checkpoints))
	}

	// Rotate to keep max 3
	if err := auto.rotateAutoCheckpoints(sessionName, 3); err != nil {
		t.Fatalf("rotateAutoCheckpoints failed: %v", err)
	}

	// Verify we have 3 left
	remaining, _ := storage.List(sessionName)
	if len(remaining) != 3 {
		t.Errorf("expected 3 remaining after rotation, got %d", len(remaining))
	}
}

// =============================================================================
// rotateAutoCheckpoints: under limit does nothing
// =============================================================================

func TestRotateAutoCheckpoints_UnderLimit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	auto := &AutoCheckpointer{storage: storage}
	sessionName := "under-limit-session"

	cp := &Checkpoint{
		Version:     CurrentVersion,
		ID:          GenerateID("test"),
		Name:        AutoCheckpointPrefix + "-test",
		SessionName: sessionName,
		CreatedAt:   time.Now(),
	}
	if err := storage.Save(cp); err != nil {
		t.Fatal(err)
	}

	if err := auto.rotateAutoCheckpoints(sessionName, 5); err != nil {
		t.Fatalf("rotateAutoCheckpoints failed: %v", err)
	}

	remaining, _ := storage.List(sessionName)
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining (under limit), got %d", len(remaining))
	}
}

// =============================================================================
// isPathWithinDir: edge cases
// =============================================================================

func TestIsPathWithinDir_AdditionalCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseDir string
		target  string
		want    bool
	}{
		{"deep nested valid", "/base", "a/b/c/d/e.txt", true},
		{"dot-dot in valid position", "/base", "sub/../sub/file.txt", true},
		{"empty target", "/base", "", true},
		{"root traversal", "/base", "/../../../etc/shadow", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isPathWithinDir(tc.baseDir, tc.target)
			if got != tc.want {
				t.Errorf("isPathWithinDir(%q, %q) = %v, want %v", tc.baseDir, tc.target, got, tc.want)
			}
		})
	}
}

// =============================================================================
// Import tar.gz: not a gzip file
// =============================================================================

func TestImportTarGz_NotGzip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "not-gzip.tar.gz")
	if err := os.WriteFile(archive, []byte("plaintext not gzip"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := storage.Import(archive, ImportOptions{})
	if err == nil {
		t.Fatal("expected error for non-gzip file")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Import zip: not a zip file
// =============================================================================

func TestImportZip_NotZip(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	archive := filepath.Join(tmpDir, "not-a-zip.zip")
	if err := os.WriteFile(archive, []byte("plaintext not zip"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := storage.Import(archive, ImportOptions{})
	if err == nil {
		t.Fatal("expected error for non-zip file")
	}
	if !strings.Contains(err.Error(), "zip") {
		t.Errorf("unexpected error: %v", err)
	}
}

// =============================================================================
// Export: nonexistent checkpoint
// =============================================================================

func TestExport_NonexistentCheckpoint(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	storage := NewStorageWithDir(tmpDir)

	_, err := storage.Export("no-session", "no-cp", filepath.Join(tmpDir, "out.tar.gz"), DefaultExportOptions())
	if err == nil {
		t.Fatal("expected error for nonexistent checkpoint")
	}
	if !strings.Contains(err.Error(), "failed to load checkpoint") {
		t.Errorf("unexpected error: %v", err)
	}
}
